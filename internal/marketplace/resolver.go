package marketplace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	cachefs "github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/redact"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	marketplaceconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/ingest"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

var ErrArtifactIdentityMismatch = errors.New("marketplace artifact identity mismatch")
var ErrArtifactSourcePath = errors.New("marketplace artifact source path is invalid")

// Resolver resolves Marketplace entries into resolver.ResolvedArtifact values.
type Resolver struct {
	HTTPClient        *http.Client
	Cache             *Cache
	AllowedRegistries []string
}

// NewResolver creates a Marketplace resolver with a default cache.
func NewResolver() *Resolver {
	return &Resolver{Cache: DefaultCache()}
}

// Resolve implements resolver.MarketplaceResolver without a temporary
// revocation override.
func (r *Resolver) Resolve(ctx context.Context, cfg *config.Config, targetSet []hcc.Target, mode resolver.MarketplaceResolveMode) ([]resolver.ResolvedArtifact, []error) {
	return r.ResolveWithPolicy(ctx, cfg, targetSet, mode, nil)
}

// ResolveWithPolicy is the optional policy-aware Marketplace resolver path.
// The policy is intentionally supplied per call and is never persisted.
func (r *Resolver) ResolveWithPolicy(ctx context.Context, cfg *config.Config, targetSet []hcc.Target, mode resolver.MarketplaceResolveMode, policy resolver.MarketplaceRevocationPolicy) ([]resolver.ResolvedArtifact, []error) {
	if err := ctx.Err(); err != nil {
		return nil, []error{err}
	}
	if cfg == nil || len(cfg.Marketplaces) == 0 {
		return nil, nil
	}

	var allowed []string
	if cfg.AllowedMarketplaces != nil {
		allowed = *cfg.AllowedMarketplaces
	}
	var allowedRegistries []string
	if cfg.AllowedRegistries != nil {
		allowedRegistries = *cfg.AllowedRegistries
	}
	if err := config.ValidateMarketplaceURLs(cfg.Marketplaces, allowed); err != nil {
		return nil, []error{err}
	}

	cache := r.cache()
	var out []resolver.ResolvedArtifact
	var errs []error
	for _, configuredEntry := range cfg.Marketplaces {
		entry, err := canonicalMarketplaceEntry(configuredEntry)
		if err != nil {
			errs = append(errs, fmt.Errorf("marketplace %s: %w", configuredEntry.URL, err))
			continue
		}
		if strings.TrimSpace(entry.InstanceID) == "" {
			errs = append(errs, fmt.Errorf("marketplace %s: configured marketplace instance ID is required", entry.URL))
			continue
		}
		var client *Client
		if mode != resolver.MarketplaceResolveModeCachedNoFetch {
			client, err = r.clientForEntry(entry)
			if err != nil {
				errs = append(errs, fmt.Errorf("marketplace %s: %w", entry.URL, err))
				continue
			}
			wellKnown, err := client.WellKnown(ctx)
			if err != nil {
				errs = append(errs, fmt.Errorf("marketplace %s well-known document: %w", entry.URL, err))
				continue
			}
			if wellKnown.InstanceID != entry.InstanceID {
				errs = append(errs, fmt.Errorf("marketplace %s instance ID mismatch: endpoint %q, configured %q", entry.URL, wellKnown.InstanceID, entry.InstanceID))
				continue
			}
		} else {
			slog.Debug("marketplace cached no-fetch resolution", "url", entry.URL, "instance_id", entry.InstanceID)
		}
		// Expanded collection roots are persisted next to their flattened leaf
		// pins. Resolve every root first, regardless of config order, so a leaf
		// cannot materialize before its collection graph has passed revocation
		// checks.
		blockedCollections := make(map[string]error)
		for _, pin := range entry.Resources {
			if pin.Kind != "collection" || !hasExpandedCollectionLeaves(entry, pin) {
				continue
			}
			rev, revErr := r.getResourceRevision(ctx, client, cache, entry, pin, mode)
			if revErr == nil {
				revErr = validateResourceRevision(entry, pin, rev)
			}
			if revErr == nil {
				revErr = enforceMarketplaceUse(rev, policy)
			}
			if revErr != nil {
				errs = append(errs, fmt.Errorf("marketplace %s pin %s/%s/%s: %w", entry.URL, pin.RegistryID, pin.Kind, pin.Name, revErr))
				continue
			}
			if graphErr := r.enforceExpandedCollectionRevocations(ctx, client, cache, entry, pin, rev, mode, policy); graphErr != nil {
				errs = append(errs, fmt.Errorf("marketplace %s pin %s/%s/%s: %w", entry.URL, pin.RegistryID, pin.Kind, pin.Name, graphErr))
				blockedCollections[collectionSelectionKey(pin.RegistryID, pin.Name, pin.Revision)] = graphErr
			}
		}
		for _, pin := range entry.Resources {
			if pin.Kind == "collection" && hasExpandedCollectionLeaves(entry, pin) {
				continue
			}
			// Excluded pins are not acquired, so an unavailable or revoked
			// resource the user opted out of cannot fail the run.
			if cfg.MarketplacePinExcluded(entry, pin) {
				continue
			}
			if blocked, ok := blockedCollectionForPin(pin, blockedCollections); ok {
				slog.Warn("marketplace leaf materialization blocked by revoked collection graph", "url", entry.URL, "registry_id", pin.RegistryID, "kind", pin.Kind, "name", pin.Name, "error", blocked)
				continue
			}
			resolved, err := r.resolvePinWithPolicy(ctx, client, cache, entry, pin, targetSet, mode, policy, allowedRegistries)
			if err != nil {
				errs = append(errs, fmt.Errorf("marketplace %s pin %s/%s/%s: %w", entry.URL, pin.RegistryID, pin.Kind, pin.Name, err))
				continue
			}
			out = append(out, resolved)
		}
	}
	return out, errs
}

func hasExpandedCollectionLeaves(entry config.MarketplaceEntry, collectionPin config.MarketplaceResourcePin) bool {
	for _, pin := range entry.Resources {
		for _, selected := range pin.SelectedBy.List() {
			if selected.RegistryID == collectionPin.RegistryID && selected.Name == collectionPin.Name && selected.Revision == collectionPin.Revision {
				return true
			}
		}
	}
	return false
}

func collectionSelectionKey(registryID, name, revision string) string {
	return registryID + "\x00" + name + "\x00" + revision
}

func blockedCollectionForPin(pin config.MarketplaceResourcePin, blocked map[string]error) (error, bool) {
	for _, selected := range pin.SelectedBy.List() {
		if err, ok := blocked[collectionSelectionKey(selected.RegistryID, selected.Name, selected.Revision)]; ok {
			return err, true
		}
	}
	return nil, false
}

// enforceExpandedCollectionRevocations validates the manifest graph behind an
// already-expanded collection. Expanded leaves are persisted independently, so
// resolving only their pins would otherwise bypass an exact revocation of a
// nested collection revision.
func (r *Resolver) enforceExpandedCollectionRevocations(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, rootPin config.MarketplaceResourcePin, rootRevision store.AssessedResourceRevision, mode resolver.MarketplaceResolveMode, policy resolver.MarketplaceRevocationPolicy) error {
	manifest, _, err := r.collectionManifest(ctx, client, cache, string(rootRevision.ContentDigest), mode, rootPin)
	if err != nil {
		return fmt.Errorf("inspect collection manifest graph: %w", err)
	}
	if manifest.Name != rootPin.Name {
		return fmt.Errorf("collection manifest name mismatch: got %q, want %q", manifest.Name, rootPin.Name)
	}
	if manifest.ContentDigest != string(rootRevision.ContentDigest) {
		return fmt.Errorf("collection manifest digest mismatch: got %q, want %q", manifest.ContentDigest, rootRevision.ContentDigest)
	}

	nestedManifests := make(map[string]hccartifact.CollectionManifest)
	if err := r.enforceNestedCollectionRevocations(ctx, client, cache, entry, manifest, map[string]bool{manifest.ContentDigest: true}, nestedManifests, mode, policy); err != nil {
		return err
	}
	if _, err := hccartifact.ValidateAndExpandCollectionManifestGraph(manifest, nestedManifests); err != nil {
		return fmt.Errorf("validate collection graph: %w", err)
	}
	return nil
}

func (r *Resolver) collectionManifest(ctx context.Context, client *Client, cache *Cache, digest string, mode resolver.MarketplaceResolveMode, pin config.MarketplaceResourcePin) (hccartifact.CollectionManifest, []byte, error) {
	if err := ctx.Err(); err != nil {
		return hccartifact.CollectionManifest{}, nil, err
	}
	raw, verified, err := cache.ReadCollectionManifest(digest)
	if err != nil {
		return hccartifact.CollectionManifest{}, nil, fmt.Errorf("read cached collection manifest: %w", err)
	}
	if raw != nil && verified {
		manifest, payload, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(raw))
		if err != nil {
			return hccartifact.CollectionManifest{}, nil, fmt.Errorf("decode cached collection manifest: %w", err)
		}
		if err := hccartifact.VerifyCollectionManifest(manifest, payload); err != nil {
			return hccartifact.CollectionManifest{}, nil, fmt.Errorf("verify cached collection manifest: %w", err)
		}
		if manifest.ContentDigest != digest {
			return hccartifact.CollectionManifest{}, nil, fmt.Errorf("cached collection manifest digest mismatch: got %q, want %q", manifest.ContentDigest, digest)
		}
		if pin.LockCollectionManifestDigest != "" && manifest.ContentDigest != pin.LockCollectionManifestDigest {
			return hccartifact.CollectionManifest{}, nil, fmt.Errorf("collection manifest digest mismatch: got %q, want %q", manifest.ContentDigest, pin.LockCollectionManifestDigest)
		}
		return manifest, payload, nil
	}
	if mode == resolver.MarketplaceResolveModeCached || mode == resolver.MarketplaceResolveModeCachedNoFetch {
		return hccartifact.CollectionManifest{}, nil, fmt.Errorf("verified collection manifest is missing from cache")
	}
	if client == nil {
		return hccartifact.CollectionManifest{}, nil, fmt.Errorf("online collection resolution requires a Marketplace client")
	}
	manifest, payload, err := client.GetCollectionManifest(ctx, digest)
	if err != nil {
		return hccartifact.CollectionManifest{}, nil, err
	}
	if _, err := cache.WriteCollectionManifest(digest, payload); err != nil {
		return hccartifact.CollectionManifest{}, nil, fmt.Errorf("cache collection manifest: %w", err)
	}
	if err := cache.MarkCollectionVerified(digest); err != nil {
		return hccartifact.CollectionManifest{}, nil, fmt.Errorf("mark collection manifest verified: %w", err)
	}
	return manifest, payload, nil
}

func (r *Resolver) enforceNestedCollectionRevocations(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, manifest hccartifact.CollectionManifest, visiting map[string]bool, nestedManifests map[string]hccartifact.CollectionManifest, mode resolver.MarketplaceResolveMode, policy resolver.MarketplaceRevocationPolicy) error {
	for _, member := range manifest.Members {
		if member.Kind != "collection" {
			continue
		}
		if member.RegistryID == "" || member.Revision == "" {
			return fmt.Errorf("nested collection %q has an unresolved registry or revision", member.Name)
		}
		if visiting[member.Revision] {
			return fmt.Errorf("nested collection %q creates a cycle", member.Name)
		}
		nestedPin := config.MarketplaceResourcePin{RegistryID: member.RegistryID, Kind: "collection", Name: member.Name, Revision: member.Revision, Digest: member.Revision}
		nestedRevision, err := r.getResourceRevision(ctx, client, cache, entry, nestedPin, mode)
		if err != nil {
			return fmt.Errorf("nested collection %q revision: %w", member.Name, err)
		}
		if err := validateResourceRevision(entry, nestedPin, nestedRevision); err != nil {
			return fmt.Errorf("nested collection %q revision metadata: %w", member.Name, err)
		}
		if err := enforceMarketplaceUse(nestedRevision, policy); err != nil {
			return fmt.Errorf("nested collection %q revision: %w", member.Name, err)
		}
		nested, _, err := r.collectionManifest(ctx, client, cache, member.Revision, mode, config.MarketplaceResourcePin{})
		if err != nil {
			return fmt.Errorf("nested collection %q manifest: %w", member.Name, err)
		}
		if nested.Name != member.Name {
			return fmt.Errorf("nested collection %q manifest name mismatch: got %q", member.Name, nested.Name)
		}
		if nested.ContentDigest != member.Revision {
			return fmt.Errorf("nested collection %q manifest digest mismatch: got %q, want %q", member.Name, nested.ContentDigest, member.Revision)
		}
		nestedManifests[member.Revision] = nested
		visiting[member.Revision] = true
		if err := r.enforceNestedCollectionRevocations(ctx, client, cache, entry, nested, visiting, nestedManifests, mode, policy); err != nil {
			return err
		}
		delete(visiting, member.Revision)
	}
	return nil
}

func canonicalMarketplaceEntry(entry config.MarketplaceEntry) (config.MarketplaceEntry, error) {
	canonicalURL, err := urlutil.NormalizeEndpointURL(entry.URL)
	if err != nil {
		return config.MarketplaceEntry{}, err
	}
	entry.URL = canonicalURL
	return entry, nil
}

// marketplaceCatalogKey identifies the Marketplace occurrence as well as its
// endpoint and pin. The instance ID matters because an endpoint URL can host
// different Marketplace instances over time or behind different deployments.
func marketplaceCatalogKey(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) string {
	return config.MarketplaceCatalogKey(entry, pin)
}

// marketplacePinRequired derives the resolver policy for an exact pin from the
// explicit required policy of its configuration entry. Organization ownership
// alone does not make a Marketplace pin mandatory.
func marketplacePinRequired(entry config.MarketplaceEntry) bool {
	return entry.Required
}

func enforceMarketplaceAssessment(rev store.AssessedResourceRevision) error {
	if rev.Assessment.Decision == store.AssessmentDecisionReject && rev.EffectiveAvailability != string(mpapi.AvailabilityOperatorExposed) {
		return fmt.Errorf("assessment rejected revision")
	}
	return nil
}

func enforceMarketplaceUse(rev store.AssessedResourceRevision, policy resolver.MarketplaceRevocationPolicy) error {
	if err := enforceMarketplaceAssessment(rev); err != nil {
		return err
	}
	return enforceMarketplaceRevocation(rev, policy)
}

func enforceMarketplaceRevocation(rev store.AssessedResourceRevision, policy resolver.MarketplaceRevocationPolicy) error {
	if rev.Revocation == nil {
		return nil
	}
	// Build the authorization identity from verified response metadata, not the
	// requested pin. A stale pin must never turn an exact override into a broad
	// authorization for a different response revision.
	identity := resolver.MarketplaceRevocationIdentity{
		Marketplace:   string(rev.Coordinate.Marketplace),
		Registry:      string(rev.Coordinate.Registry),
		Kind:          rev.Coordinate.Kind,
		Name:          rev.Coordinate.Name,
		ContentDigest: string(rev.ContentDigest),
	}
	if policy == nil || !policy.AllowRevoked(identity) {
		return fmt.Errorf("revision is revoked")
	}
	slog.Warn("marketplace revoked revision explicitly allowed for this invocation", "marketplace", identity.Marketplace, "registry", identity.Registry, "kind", identity.Kind, "name", identity.Name, "content_digest", identity.ContentDigest)
	return nil
}

func (r *Resolver) cache() *Cache {
	if r.Cache != nil {
		return r.Cache
	}
	return DefaultCache()
}

func (r *Resolver) clientForEntry(entry config.MarketplaceEntry) (*Client, error) {
	opts := []Option{}
	if r.HTTPClient != nil {
		opts = append(opts, WithHTTPClient(r.HTTPClient))
	}
	return NewClient(entry.URL, opts...)
}

func (r *Resolver) resolvePinWithPolicy(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, targetSet []hcc.Target, mode resolver.MarketplaceResolveMode, policy resolver.MarketplaceRevocationPolicy, allowedRegistries []string) (resolver.ResolvedArtifact, error) {
	artifact, _, err := r.resolvePinWithArtifactPolicy(ctx, client, cache, entry, pin, targetSet, mode, policy, allowedRegistries)
	return artifact, err
}

// resolvePinWithArtifact returns the same resolved artifact as resolvePin and
// also retains the CanonicalArtifact decoded and verified during resolution.
// Callers that need metadata for a prepared install must use this value rather
// than rereading the cache, since the cache is intentionally disposable.
func (r *Resolver) resolvePinWithArtifact(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, targetSet []hcc.Target, mode resolver.MarketplaceResolveMode) (resolver.ResolvedArtifact, hccartifact.CanonicalArtifact, error) {
	return r.resolvePinWithArtifactPolicy(ctx, client, cache, entry, pin, targetSet, mode, nil, r.AllowedRegistries)
}

func (r *Resolver) resolvePinWithArtifactPolicy(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, targetSet []hcc.Target, mode resolver.MarketplaceResolveMode, policy resolver.MarketplaceRevocationPolicy, allowedRegistries []string) (resolver.ResolvedArtifact, hccartifact.CanonicalArtifact, error) {
	if err := ctx.Err(); err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, err
	}
	kind, err := irKind(pin.Kind)
	if err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, err
	}
	if err := validatePin(entry, pin); err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, err
	}

	acquisition := pin.Acquisition
	if acquisition == "" {
		acquisition = config.MarketplaceAcquisitionMirror
	}
	if acquisition == config.MarketplaceAcquisitionIndexOnly {
		return r.resolveIndexOnlyPin(ctx, client, cache, entry, pin, targetSet, mode, policy, allowedRegistries)
	}
	if acquisition != config.MarketplaceAcquisitionMirror {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("acquisition mode %q is not supported", acquisition)
	}

	if pin.Kind == "collection" {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("collection resolution is not supported yet")
	}

	rev, err := r.getResourceRevision(ctx, client, cache, entry, pin, mode)
	if err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, err
	}
	if err := validateResourceRevision(entry, pin, rev); err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, err
	}

	name := pin.Name
	if pin.InstalledName != "" {
		name = pin.InstalledName
	}
	required := marketplacePinRequired(entry)
	slog.Debug("marketplace pin required decision",
		"url", entry.URL,
		"instance_id", entry.InstanceID,
		"registry_id", pin.RegistryID,
		"kind", pin.Kind,
		"name", pin.Name,
		"config_source", entry.Source.String(),
		"required", required,
	)

	artifact := resolver.ResolvedArtifact{
		Entry: catalog.Entry{
			Kind: kind,
			Name: name,
		},
		RegistryURL:             entry.URL,
		RegistryPath:            pin.RegistryID,
		Ref:                     pin.Revision,
		Source:                  entry.Source,
		Required:                required,
		CatalogKey:              marketplaceCatalogKey(entry, pin),
		Harnesses:               targetSet,
		IsMarketplace:           true,
		MarketplaceURL:          entry.URL,
		MarketplaceInstanceID:   entry.InstanceID,
		MarketplaceAlias:        entry.Alias,
		MarketplaceRegistryID:   pin.RegistryID,
		MarketplaceResourceName: pin.Name,
		InstalledName:           pin.InstalledName,
		Revoked:                 rev.Revocation != nil,
	}
	if err := enforceMarketplaceUse(rev, policy); err != nil {
		return artifact, hccartifact.CanonicalArtifact{}, err
	}

	if mode == resolver.MarketplaceResolveModeInspect {
		artifact.Unresolved = true
		return artifact, hccartifact.CanonicalArtifact{}, nil
	}

	archiveDigest := string(rev.ArchiveDigest)
	raw, verified, err := cache.ReadArtifact(archiveDigest)
	if err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("read cached artifact: %w", err)
	}
	if mode == resolver.MarketplaceResolveModeCachedNoFetch && (!verified || raw == nil) {
		if raw == nil {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("verified artifact is missing from cache")
		}
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("cached artifact is not verified")
	}
	if !verified || raw == nil {
		if mode == resolver.MarketplaceResolveModeCached {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("artifact not in cache")
		}
		slog.Debug("marketplace fetching artifact", "url", entry.URL, "registry", pin.RegistryID, "kind", pin.Kind, "name", pin.Name, "archive_digest", rev.ArchiveDigest)
		raw, err = client.GetArtifact(ctx, archiveDigest)
		if err != nil {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("fetch artifact: %w", err)
		}
		if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("write artifact cache: %w", err)
		}
		verified = false
	}

	ca, caRaw, err := hccartifact.Decode(bytes.NewReader(raw))
	if err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("decode artifact: %w", err)
	}
	// Decode only parses the archive. Validate the complete decoded artifact
	// before any metadata can reach install consent/preview or the cache can be
	// marked verified; this includes schema, finding severity, and policy outcome.
	if err := ca.ValidateArchive(nil); err != nil {
		cache.InvalidateArtifactVerified(archiveDigest)
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("validate artifact archive: %w", err)
	}
	if err := ca.VerifyArchiveDigest(caRaw); err != nil {
		cache.InvalidateArtifactVerified(archiveDigest)
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("verify archive digest: %w", err)
	}
	if err := ca.VerifyContentDigest(); err != nil {
		cache.InvalidateArtifactVerified(archiveDigest)
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("verify content digest: %w", err)
	}
	if err := validateArtifactManifest(entry, pin, rev, ca); err != nil {
		cache.InvalidateArtifactVerified(archiveDigest)
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, err
	}

	resource, err := hccartifact.DecodeResource(pin.Kind, ca.Payload)
	if err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("decode resource: %w", err)
	}
	if err := validateDecodedArtifactIdentity(pin, resource); err != nil {
		cache.InvalidateArtifactVerified(archiveDigest)
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, err
	}
	if err := validateMirrorSourcePath(resource); err != nil {
		cache.InvalidateArtifactVerified(archiveDigest)
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, err
	}
	artifact.Resource = resource
	artifact.Description = descriptionFromResource(resource)
	artifact.Path = cache.ArtifactPath(archiveDigest)

	if !verified {
		if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("mark artifact verified: %w", err)
		}
	}
	return artifact, ca, nil
}

func (r *Resolver) resolveIndexOnlyPin(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, targetSet []hcc.Target, mode resolver.MarketplaceResolveMode, policy resolver.MarketplaceRevocationPolicy, allowedRegistries []string) (artifact resolver.ResolvedArtifact, canonical hccartifact.CanonicalArtifact, retErr error) {
	if pin.Kind == "collection" {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("index-only collection resolution is not supported yet")
	}
	identity := IndexOnlyIdentity{Marketplace: entry.InstanceID, RegistryID: pin.RegistryID, Commit: pin.RegistryCommit, ManifestDigest: pin.RegistryManifestDigest}
	cacheDigest := pin.Digest
	if mode != resolver.MarketplaceResolveModeInspect {
		defer func() {
			if retErr != nil {
				// An occurrence marker is a trust decision for this exact registry
				// occurrence. Any failed materialization attempt must not leave it
				// trusted, and must never touch the shared mirror marker.
				cache.InvalidateIndexOnlyArtifactVerified(identity, cacheDigest)
			}
		}()
	}
	rev, err := r.getIndexOnlyResourceRevision(ctx, client, cache, entry, pin, identity, mode)
	if err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, err
	}
	if err := validateResourceRevision(entry, pin, rev); err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, err
	}
	if mode != resolver.MarketplaceResolveModeRefresh && (rev.Occurrence.Coordinate.Registry != store.RegistryID(pin.RegistryID) || rev.Occurrence.Commit != pin.RegistryCommit || rev.Occurrence.ManifestDigest != store.RevisionDigest(pin.RegistryManifestDigest)) {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("cached registry occurrence digest mismatch: revision occurrence does not match pinned registry occurrence")
	}
	name := pin.Name
	if pin.InstalledName != "" {
		name = pin.InstalledName
	}
	artifact = resolver.ResolvedArtifact{
		Entry:       catalog.Entry{Kind: mustIRKind(pin.Kind), Name: name},
		RegistryURL: entry.URL, RegistryPath: pin.RegistryID, Ref: pin.Revision,
		Source: entry.Source, Required: marketplacePinRequired(entry),
		CatalogKey: marketplaceCatalogKey(entry, pin), Harnesses: targetSet,
		IsMarketplace: true, MarketplaceURL: entry.URL, MarketplaceInstanceID: entry.InstanceID,
		MarketplaceAlias: entry.Alias, MarketplaceRegistryID: pin.RegistryID,
		MarketplaceResourceName: pin.Name, InstalledName: pin.InstalledName, Revoked: rev.Revocation != nil,
	}
	artifact.Ref = pin.RegistryCommit
	if err := enforceMarketplaceUse(rev, policy); err != nil {
		return artifact, hccartifact.CanonicalArtifact{}, err
	}
	var origin string
	var archiveDigest string
	var raw []byte
	var registry store.Registry
	var checkoutDir string
	if mode == resolver.MarketplaceResolveModeRefresh || mode == resolver.MarketplaceResolveModeInspect {
		if client == nil {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("index-only online resolution requires a Marketplace client")
		}
		occ, err := client.GetRegistryOccurrence(ctx, pin.RegistryID, pin.RegistryCommit)
		if err != nil {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("exact registry commit unavailable: %w", err)
		}
		if occ.RegistryID != store.RegistryID(pin.RegistryID) || occ.Commit != pin.RegistryCommit || occ.Acquisition != store.AcquisitionIndexOnly {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("registry occurrence identity or acquisition mismatch")
		}
		if occ.RegistryRevisionManifestDigest != store.RevisionDigest(pin.RegistryManifestDigest) {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("registry manifest digest mismatch: occurrence %q, pin %q", occ.RegistryRevisionManifestDigest, pin.RegistryManifestDigest)
		}
		if rev.Occurrence.Coordinate.Registry != occ.RegistryID || rev.Occurrence.Coordinate.Registry != store.RegistryID(pin.RegistryID) || rev.Occurrence.Commit != occ.Commit || rev.Occurrence.ManifestDigest != occ.RegistryRevisionManifestDigest {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("resource revision occurrence does not match exact registry occurrence")
		}
		origin = strings.TrimSpace(occ.AcquisitionOrigin)
		if err := validateAcquisitionOrigin(origin); err != nil {
			slog.Warn("index-only acquisition origin rejected before allowed_registries", "origin", redact.URL(origin), "registry_id", pin.RegistryID, "error", err)
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("policy rejection: invalid acquisition origin before allowed_registries: %w", err)
		}
		if err := config.ValidateRegistryURLs([]config.RegistryEntry{{URL: origin, Ref: occ.Ref}}, allowedRegistries); err != nil {
			slog.Warn("index-only acquisition origin rejected before Git access", "origin", redact.URL(origin), "registry_id", pin.RegistryID, "error", err)
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("policy rejection: acquisition origin rejected by allowed_registries before Git access: %w", err)
		}
		slog.Info("index-only exact registry acquisition approved", "origin", redact.URL(origin), "registry_id", pin.RegistryID, "commit", occ.Commit, "manifest_digest", occ.RegistryRevisionManifestDigest)
		artifact.RegistryURL = origin
		artifact.Ref = occ.Commit
		if mode == resolver.MarketplaceResolveModeInspect {
			artifact.Unresolved = true
			return artifact, hccartifact.CanonicalArtifact{}, nil
		}

		registry, err = client.GetRegistry(ctx, pin.RegistryID)
		if err != nil {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("fetch registry for exact acquisition: %w", err)
		}
		if registry.ID != store.RegistryID(pin.RegistryID) || registry.ID != occ.RegistryID {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("registry metadata identity does not match exact registry occurrence")
		}
		registry.Origins = []string{origin}
		registry.Ref = occ.Ref
		if len(occ.Roots) > 0 {
			registry.Roots = append([]store.RegistryRoot(nil), occ.Roots...)
		}
		registry.Acquisition = store.AcquisitionIndexOnly
		roots := make([]string, len(occ.Roots))
		for i := range occ.Roots {
			roots[i] = occ.Roots[i].Path
		}
		acquired, err := acquire.CloneAtCommit(ctx, origin, occ.Ref, occ.Commit, acquire.Credentials{}, acquire.Limits{RepositoryBytes: 100 * 1024 * 1024, FileCount: 1024, FileBytes: 2 * 1024 * 1024}, 5*time.Minute, roots...)
		if err != nil {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("unavailable exact registry commit: %w", err)
		}
		defer func() { _ = acquired.Release() }()
		checkoutDir = acquired.Dir
		buildCfg := marketplaceconfig.Config{Policy: marketplaceconfig.Policy{HiddenContent: "warn", Limits: marketplaceconfig.Limits{IngestionDuration: marketplaceconfig.Duration(2 * time.Minute), ArtifactBytes: 16 * 1024 * 1024}}}
		built, err := ingest.BuildRegistryRevision(ctx, buildCfg, store.MarketplaceID(entry.InstanceID), registry, acquired.Dir, occ.Commit)
		if err != nil {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("re-ingest exact registry commit: %w", err)
		}
		if built.ManifestDigest != occ.RegistryRevisionManifestDigest {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("registry manifest digest mismatch after re-ingestion: got %q, want %q", built.ManifestDigest, occ.RegistryRevisionManifestDigest)
		}
		key := pin.Kind + "/" + pin.Name
		raw = built.ResourceArchives[key]
		if len(raw) == 0 {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("exact registry commit did not contain an accepted %s %q", pin.Kind, pin.Name)
		}
		archiveDigest = built.ResourceArchiveDigest[key]
		if archiveDigest == "" {
			archiveDigest = hccartifact.ArchiveDigest(raw)
		}
	} else {
		var verified bool
		raw, verified, err = cache.ReadIndexOnlyArtifact(identity, cacheDigest)
		if err != nil {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("read cached index-only artifact: %w", err)
		}
		if raw == nil {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("missing cached index-only artifact for exact registry occurrence")
		}
		if !verified {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("cached index-only artifact is not verified")
		}
	}
	ca, caRaw, err := hccartifact.Decode(bytes.NewReader(raw))
	if err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("decode re-ingested artifact: %w", err)
	}
	if err := ca.ValidateArchive(nil); err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("validate re-ingested artifact: %w", err)
	}
	if err := ca.VerifyArchiveDigest(caRaw); err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("verify re-ingested archive: %w", err)
	}
	if archiveDigest != "" && ca.Manifest.ArchiveDigest != archiveDigest {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("cached index-only artifact digest mismatch: metadata %q, archive %q", archiveDigest, ca.Manifest.ArchiveDigest)
	}
	if err := ca.VerifyContentDigest(); err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("verify re-ingested content: %w", err)
	}
	if err := validateReconstructedArtifactIdentity(pin, ca); err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, err
	}
	if mode == resolver.MarketplaceResolveModeRefresh {
		if _, err := cache.WriteIndexOnlyArtifact(identity, cacheDigest, raw); err != nil {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("write reconstructed index-only artifact cache: %w", err)
		}
	}
	// Keep the cache untrusted until the payload has been decoded as the
	// requested resource. Archive/content digests alone do not prove that the
	// payload is a usable resource or that its identity matches this pin.
	resource, err := hccartifact.DecodeResource(pin.Kind, ca.Payload)
	if err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("decode re-ingested resource: %w", err)
	}
	if err := validateDecodedArtifactIdentity(pin, resource); err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, err
	}
	var supportFiles []string
	if skill, ok := resource.(*ir.Skill); ok {
		supportFiles = skill.SupportFiles
	}
	var resourcePath string
	if checkoutDir != "" {
		resourcePath, err = stageReconstructedResource(cache, identity, checkoutDir, registry, pin.Kind, pin.Name, supportFiles)
	} else {
		dir := cachedReconstructedResourcePath(cache, identity, pin.Kind, pin.Name)
		if err = validateCachedReconstructedResource(cache, dir); err != nil {
			resourcePath = dir
		} else if pin.Kind == "hook" {
			if _, statErr := os.Stat(filepath.Join(dir, "hooks.json")); statErr == nil {
				resourcePath = filepath.Join(dir, "hooks.json")
			} else {
				resourcePath = filepath.Join(dir, "SKILL.md")
			}
		} else {
			resourcePath = filepath.Join(dir, "SKILL.md")
		}
		if err != nil {
			err = fmt.Errorf("cached reconstructed source is not within the Marketplace cache: %w", err)
		}
	}
	if err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("stage reconstructed resource support files: %w", err)
	}
	if mode == resolver.MarketplaceResolveModeRefresh {
		if err := cache.MarkIndexOnlyArtifactVerified(identity, cacheDigest); err != nil {
			return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("mark index-only artifact verified: %w", err)
		}
	}
	artifact.Resource = resource
	artifact.Description = descriptionFromResource(resource)
	artifact.Path = resourcePath
	return artifact, ca, nil
}

// stageReconstructedResource gives the materializer a source directory for the
// existing support-file copy seam. It copies only the exact resource directory
// from the checked-out commit; the artifact cache directory itself is never used
// as a source tree, so artifact.tar.gz and verified.marker cannot leak into an
// installed skill directory.
func stageReconstructedResource(cache *Cache, identity IndexOnlyIdentity, checkoutDir string, registry store.Registry, kind, name string, supportFiles []string) (string, error) {
	var sourceDir string
	for _, root := range registry.Roots {
		rootDir := filepath.Join(checkoutDir, filepath.Clean(root.Path))
		rel, err := filepath.Rel(checkoutDir, rootDir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		cat, err := catalog.Load(rootDir, root.SourceHarness)
		if err != nil {
			continue
		}
		for _, entry := range cat.Entries {
			if mpapi.FromIR(entry.Kind) != kind || entry.Name != name || entry.Path == "" {
				continue
			}
			resourcePath := entry.Path
			if !filepath.IsAbs(resourcePath) {
				resourcePath = filepath.Join(rootDir, filepath.FromSlash(resourcePath))
			}
			resourceRel, err := filepath.Rel(checkoutDir, resourcePath)
			if err != nil || resourceRel == ".." || strings.HasPrefix(resourceRel, ".."+string(filepath.Separator)) || filepath.IsAbs(resourceRel) {
				continue
			}
			if err := validateCheckoutPath(checkoutDir, resourcePath, false); err != nil {
				return "", fmt.Errorf("resource path is not a regular checkout path: %w", err)
			}
			info, err := os.Stat(resourcePath)
			if err != nil {
				return "", err
			}
			sourceDir = resourcePath
			if !info.IsDir() {
				sourceDir = filepath.Dir(resourcePath)
			}
			break
		}
		if sourceDir != "" {
			break
		}
	}
	if sourceDir == "" {
		return "", fmt.Errorf("resource %s/%q was not found in exact source checkout", kind, name)
	}

	if err := ir.ValidateName(name); err != nil {
		return "", err
	}
	stagedDir := cachedReconstructedResourcePath(cache, identity, kind, name)
	if err := validateCacheDirectory(cache.root, stagedDir); err != nil {
		return "", fmt.Errorf("validate reconstructed staging path: %w", err)
	}
	if err := os.RemoveAll(stagedDir); err != nil {
		return "", err
	}
	if err := ensureCacheDirectory(cache.root, stagedDir); err != nil {
		return "", err
	}
	if err := copyReconstructedTree(checkoutDir, sourceDir, stagedDir); err != nil {
		return "", err
	}
	// Skill support files are explicitly listed by the canonical IR. Copying
	// the source tree also preserves the original SKILL.md and gives hooks a
	// durable directory for their scripts when the next run is offline.
	for _, supportFile := range supportFiles {
		clean := filepath.Clean(filepath.FromSlash(supportFile))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) || clean == "SKILL.md" {
			return "", fmt.Errorf("unsafe reconstructed support path %q", supportFile)
		}
		if err := validateCheckoutPath(checkoutDir, filepath.Join(sourceDir, clean), true); err != nil {
			return "", fmt.Errorf("unsafe support file %q: %w", supportFile, err)
		}
	}
	slog.Debug("staged reconstructed resource support files", "source_dir", sourceDir, "files", len(supportFiles), "path", stagedDir)
	if kind == "hook" {
		if _, err := os.Stat(filepath.Join(stagedDir, "hooks.json")); err == nil {
			return filepath.Join(stagedDir, "hooks.json"), nil
		}
	}
	return filepath.Join(stagedDir, "SKILL.md"), nil
}

func cachedReconstructedResourcePath(cache *Cache, identity IndexOnlyIdentity, kind, name string) string {
	return filepath.Join(cache.root, "compiled", "marketplace-sources", IndexOnlyKey(identity), kind, name)
}

func validateCachedReconstructedResource(cache *Cache, resourcePath string) error {
	if err := cachefs.ValidateDirectoryAncestry(cache.root, resourcePath); err != nil {
		return err
	}
	info, err := os.Lstat(resourcePath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("staged resource directory is not a regular directory")
	}
	return nil
}

func copyReconstructedTree(checkoutDir, sourceDir, stagedDir string) error {
	return filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source tree contains symlink %q", path)
		}
		if path == sourceDir {
			return nil
		}
		if err := validateCheckoutPath(checkoutDir, path, false); err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return fmt.Errorf("staged source path escapes resource directory")
		}
		dst := filepath.Join(stagedDir, rel)
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported source file %q", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode().Perm())
	})
}

func validateCheckoutPath(checkoutDir, target string, allowMissing bool) error {
	root, err := filepath.Abs(checkoutDir)
	if err != nil {
		return err
	}
	path, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("path escapes exact source checkout")
	}

	current := root
	parts := []string{}
	if rel != "." {
		parts = strings.Split(rel, string(filepath.Separator))
	}
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if allowMissing {
				return nil
			}
			return err
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component %q is a symlink", part)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("path component %q is not a directory", part)
		}
	}
	return nil
}

func mustIRKind(kind string) ir.ResourceKind {
	value, _ := irKind(kind)
	return value
}

func (r *Resolver) getIndexOnlyResourceRevision(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, identity IndexOnlyIdentity, mode resolver.MarketplaceResolveMode) (store.AssessedResourceRevision, error) {
	if err := ctx.Err(); err != nil {
		return store.AssessedResourceRevision{}, err
	}
	if mode != resolver.MarketplaceResolveModeRefresh && mode != resolver.MarketplaceResolveModeInspect {
		rev, ok, err := cache.ReadIndexOnlyResourceRevision(identity, pin.RegistryID, pin.Kind, pin.Name, pin.Digest)
		if err != nil {
			return store.AssessedResourceRevision{}, fmt.Errorf("read cached index-only revision: %w", err)
		}
		if ok {
			return rev, nil
		}
		if mode == resolver.MarketplaceResolveModeCachedNoFetch {
			return store.AssessedResourceRevision{}, fmt.Errorf("missing cached index-only revision metadata for exact registry occurrence")
		}
		return store.AssessedResourceRevision{}, fmt.Errorf("index-only revision metadata is missing from cache")
	}
	if client == nil {
		return store.AssessedResourceRevision{}, fmt.Errorf("index-only online resolution requires a Marketplace client")
	}
	rev, err := client.GetResourceRevision(ctx, pin.RegistryID, pin.Kind, pin.Name, pin.Digest)
	if err != nil {
		return store.AssessedResourceRevision{}, fmt.Errorf("fetch resource revision: %w", err)
	}
	if err := validateResourceRevision(entry, pin, rev); err != nil {
		return store.AssessedResourceRevision{}, err
	}
	if _, err := cache.WriteIndexOnlyResourceRevision(identity, pin.RegistryID, pin.Kind, pin.Name, pin.Digest, rev); err != nil {
		return store.AssessedResourceRevision{}, fmt.Errorf("write index-only revision cache: %w", err)
	}
	return rev, nil
}

func (r *Resolver) getResourceRevision(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, mode resolver.MarketplaceResolveMode) (store.AssessedResourceRevision, error) {
	if err := ctx.Err(); err != nil {
		return store.AssessedResourceRevision{}, err
	}
	if mode != resolver.MarketplaceResolveModeRefresh {
		rev, ok, err := cache.ReadResourceRevision(entry.URL, pin.RegistryID, pin.Kind, pin.Name, pin.Digest)
		if err != nil {
			return store.AssessedResourceRevision{}, fmt.Errorf("read cached revision: %w", err)
		}
		if ok {
			return rev, nil
		}
		if mode == resolver.MarketplaceResolveModeCached {
			return store.AssessedResourceRevision{}, fmt.Errorf("resource revision not in cache")
		}
		if mode == resolver.MarketplaceResolveModeCachedNoFetch {
			return store.AssessedResourceRevision{}, fmt.Errorf("resource revision metadata is missing from cache")
		}
	}

	if client == nil {
		return store.AssessedResourceRevision{}, fmt.Errorf("cached no-fetch resolver has no Marketplace client")
	}
	rev, err := client.GetResourceRevision(ctx, pin.RegistryID, pin.Kind, pin.Name, pin.Digest)
	if err != nil {
		return store.AssessedResourceRevision{}, fmt.Errorf("fetch resource revision: %w", err)
	}
	if err := validateResourceRevision(entry, pin, rev); err != nil {
		return store.AssessedResourceRevision{}, err
	}
	if _, err := cache.WriteResourceRevision(entry.URL, pin.RegistryID, pin.Kind, pin.Name, pin.Digest, rev); err != nil {
		return store.AssessedResourceRevision{}, fmt.Errorf("write revision cache: %w", err)
	}
	return rev, nil
}

func validatePin(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) error {
	if strings.TrimSpace(entry.InstanceID) == "" {
		return fmt.Errorf("configured marketplace instance ID is required")
	}
	if strings.TrimSpace(pin.RegistryID) == "" || strings.TrimSpace(pin.Kind) == "" || strings.TrimSpace(pin.Name) == "" {
		return fmt.Errorf("resource pin coordinate is incomplete")
	}
	if strings.TrimSpace(pin.Revision) == "" {
		return fmt.Errorf("resource pin revision is required")
	}
	if err := validateSHA256Digest(pin.Digest); err != nil {
		return fmt.Errorf("content digest %q: %w", pin.Digest, err)
	}
	acquisition := pin.Acquisition
	if acquisition == "" {
		acquisition = config.MarketplaceAcquisitionMirror
	}
	if acquisition == config.MarketplaceAcquisitionIndexOnly {
		if !mpapi.IsCanonicalGitCommit(pin.RegistryCommit) {
			return fmt.Errorf("index-only pin registry commit must be a full lowercase Git commit")
		}
		if err := validateSHA256Digest(pin.RegistryManifestDigest); err != nil {
			return fmt.Errorf("index-only registry manifest digest %q: %w", pin.RegistryManifestDigest, err)
		}
	}
	return nil
}

func validateResourceRevision(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, rev store.AssessedResourceRevision) error {
	if err := validateAPIResourceRevision(rev); err != nil {
		return fmt.Errorf("invalid assessed resource revision: %w", err)
	}
	want := store.ResourceCoordinate{
		Marketplace: store.MarketplaceID(entry.InstanceID),
		Registry:    store.RegistryID(pin.RegistryID),
		Kind:        pin.Kind,
		Name:        pin.Name,
	}
	if rev.Coordinate != want {
		return fmt.Errorf("resource coordinate mismatch: got %q, want %q", rev.Coordinate, want)
	}
	if err := validateSHA256Digest(string(rev.ContentDigest)); err != nil {
		return fmt.Errorf("revision content digest %q: %w", rev.ContentDigest, err)
	}
	if rev.ContentDigest != store.RevisionDigest(pin.Digest) {
		return fmt.Errorf("revision content digest mismatch: metadata %q, pin %q", rev.ContentDigest, pin.Digest)
	}
	if pin.Kind == "collection" && rev.ArchiveDigest != "" {
		return fmt.Errorf("virtual collection resource revision must not have an archive digest")
	}
	if rev.ArchiveDigest != "" {
		if err := validateSHA256Digest(string(rev.ArchiveDigest)); err != nil {
			return fmt.Errorf("revision archive digest %q: %w", rev.ArchiveDigest, err)
		}
	}
	acquisition := pin.Acquisition
	if acquisition == "" {
		acquisition = config.MarketplaceAcquisitionMirror
	}
	if rev.Revocation == nil && rev.ArchiveDigest == "" && pin.Kind != "collection" && acquisition != config.MarketplaceAcquisitionIndexOnly {
		return fmt.Errorf("revision archive digest is required")
	}
	if pin.LockArchiveDigest != "" && string(rev.ArchiveDigest) != pin.LockArchiveDigest {
		return fmt.Errorf("locked archive digest mismatch: metadata %q, lock %q", rev.ArchiveDigest, pin.LockArchiveDigest)
	}
	if pin.LockSourcePath != "" && rev.Occurrence.SourcePath != pin.LockSourcePath {
		return fmt.Errorf("locked source path mismatch: metadata %q, lock %q", rev.Occurrence.SourcePath, pin.LockSourcePath)
	}
	return nil
}

func validateReconstructedArtifactIdentity(pin config.MarketplaceResourcePin, artifact hccartifact.CanonicalArtifact) error {
	manifest := artifact.Manifest
	if manifest.ContentDigest != pin.Digest {
		return fmt.Errorf("re-ingested content digest mismatch: got %q, want %q", manifest.ContentDigest, pin.Digest)
	}
	if manifest.Name != pin.Name || manifest.Kind != pin.Kind {
		return fmt.Errorf("re-ingested resource identity mismatch: got %s/%q, want %s/%q", manifest.Kind, manifest.Name, pin.Kind, pin.Name)
	}
	return nil
}

func validateDecodedArtifactIdentity(pin config.MarketplaceResourcePin, resource ir.Resource) error {
	if resource.Kind().String() != pin.Kind || resource.Name() != pin.Name {
		return fmt.Errorf("%w: decoded resource does not match pin", ErrArtifactIdentityMismatch)
	}
	return nil
}

func validateMirrorSourcePath(resource ir.Resource) error {
	var source string
	switch typed := resource.(type) {
	case *ir.Skill:
		source = typed.Source.File
	case *ir.Command:
		source = typed.Source.File
	case *ir.Subagent:
		source = typed.Source.File
	case *ir.Hook:
		source = typed.Source.File
	case *ir.McpServer:
		source = typed.Source.File
	case *ir.Rule:
		source = typed.Source.File
	case *ir.Collection:
		source = typed.Source.File
	}
	if source == "" {
		return nil
	}
	if filepath.IsAbs(source) || strings.Contains(source, "\\") || hccartifact.ValidatePath(source) != nil {
		return ErrArtifactSourcePath
	}
	return nil
}

func validateArtifactManifest(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, rev store.AssessedResourceRevision, artifact hccartifact.CanonicalArtifact) error {
	manifest := artifact.Manifest
	if manifest.Kind != pin.Kind {
		return fmt.Errorf("artifact manifest kind mismatch: got %q, want %q", manifest.Kind, pin.Kind)
	}
	if manifest.Name != pin.Name {
		return fmt.Errorf("artifact manifest name mismatch: got %q, want %q", manifest.Name, pin.Name)
	}
	if manifest.ContentDigest != pin.Digest {
		return fmt.Errorf("artifact manifest content digest mismatch: got %q, want %q", manifest.ContentDigest, pin.Digest)
	}
	if manifest.ArchiveDigest != string(rev.ArchiveDigest) {
		return fmt.Errorf("artifact manifest archive digest mismatch: got %q, want %q", manifest.ArchiveDigest, rev.ArchiveDigest)
	}
	if entry.InstanceID == "" {
		return fmt.Errorf("configured marketplace instance ID is required")
	}
	return nil
}

func irKind(kind string) (ir.ResourceKind, error) {
	switch kind {
	case "skill":
		return ir.KindSkill, nil
	case "command":
		return ir.KindCommand, nil
	case "subagent":
		return ir.KindSubagent, nil
	case "hook":
		return ir.KindHook, nil
	case "mcpserver":
		return ir.KindMcpServer, nil
	case "collection":
		return ir.KindCollection, nil
	}
	return 0, fmt.Errorf("unsupported resource kind %q", kind)
}

func descriptionFromResource(r ir.Resource) string {
	switch v := r.(type) {
	case *ir.Skill:
		return v.Description
	case *ir.Command:
		return v.Description
	case *ir.Subagent:
		return v.Description
	case *ir.Hook:
		return ""
	case *ir.McpServer:
		return ""
	}
	return ""
}
