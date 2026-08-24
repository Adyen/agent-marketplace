package marketplace

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

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
		for _, pin := range entry.Resources {
			resolved, err := r.resolvePinWithPolicy(ctx, client, cache, entry, pin, targetSet, mode, policy, allowedRegistries)
			if err != nil {
				errs = append(errs, fmt.Errorf("marketplace %s pin %s/%s/%s: %w", entry.URL, pin.SourceID, pin.Kind, pin.Name, err))
				continue
			}
			out = append(out, resolved)
		}
	}
	return out, errs
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
	return fmt.Sprintf("marketplace:%s:%s:%s:%s:%s", entry.URL, entry.InstanceID, pin.SourceID, pin.Digest, pin.Revision)
}

// marketplacePinRequired derives the resolver policy for an exact pin from the
// configuration layer that owns it. Organization pins are mandatory policy;
// pins from writable user/repo layers remain optional and can be excluded.
func marketplacePinRequired(source config.Source) bool {
	return source == config.Organization
}

func enforceMarketplaceRevocation(rev store.ResourceRevision, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, policy resolver.MarketplaceRevocationPolicy) error {
	if rev.Revocation == nil {
		return nil
	}
	identity := resolver.MarketplaceRevocationIdentity{
		Marketplace:   entry.InstanceID,
		Source:        pin.SourceID,
		Kind:          pin.Kind,
		Name:          pin.Name,
		ContentDigest: pin.Digest,
	}
	if policy == nil || !policy.AllowRevoked(identity) {
		return fmt.Errorf("revision is revoked")
	}
	slog.Warn("marketplace revoked revision explicitly allowed for this invocation", "marketplace", entry.InstanceID, "source", pin.SourceID, "kind", pin.Kind, "name", pin.Name, "content_digest", pin.Digest)
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
	required := marketplacePinRequired(entry.Source)
	slog.Debug("marketplace pin required decision",
		"url", entry.URL,
		"instance_id", entry.InstanceID,
		"source_id", pin.SourceID,
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
		RegistryURL:           entry.URL,
		RegistryPath:          pin.SourceID,
		Ref:                   pin.Revision,
		Source:                entry.Source,
		Required:              required,
		CatalogKey:            marketplaceCatalogKey(entry, pin),
		Harnesses:             targetSet,
		IsMarketplace:         true,
		MarketplaceURL:        entry.URL,
		MarketplaceInstanceID: entry.InstanceID,
		MarketplaceAlias:      entry.Alias,
		MarketplaceSourceID:   pin.SourceID,
		InstalledName:         pin.InstalledName,
		Revoked:               rev.Revocation != nil,
	}

	if err := enforceMarketplaceRevocation(rev, entry, pin, policy); err != nil {
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
		slog.Debug("marketplace fetching artifact", "url", entry.URL, "source", pin.SourceID, "kind", pin.Kind, "name", pin.Name, "archive_digest", rev.ArchiveDigest)
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

func (r *Resolver) resolveIndexOnlyPin(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, targetSet []hcc.Target, mode resolver.MarketplaceResolveMode, policy resolver.MarketplaceRevocationPolicy, allowedRegistries []string) (resolver.ResolvedArtifact, hccartifact.CanonicalArtifact, error) {
	if pin.Kind == "collection" {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("index-only collection resolution is not supported yet")
	}
	if client == nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("index-only resolution requires a Marketplace client")
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
	artifact := resolver.ResolvedArtifact{
		Entry:       catalog.Entry{Kind: mustIRKind(pin.Kind), Name: name},
		RegistryURL: entry.URL, RegistryPath: pin.SourceID, Ref: pin.Revision,
		Source: entry.Source, Required: marketplacePinRequired(entry.Source),
		CatalogKey: marketplaceCatalogKey(entry, pin), Harnesses: targetSet,
		IsMarketplace: true, MarketplaceURL: entry.URL, MarketplaceInstanceID: entry.InstanceID,
		MarketplaceAlias: entry.Alias, MarketplaceSourceID: pin.SourceID,
		InstalledName: pin.InstalledName, Revoked: rev.Revocation != nil,
	}
	if err := enforceMarketplaceRevocation(rev, entry, pin, policy); err != nil {
		return artifact, hccartifact.CanonicalArtifact{}, err
	}
	occ, err := client.GetSourceOccurrence(ctx, pin.SourceID, pin.SourceCommit)
	if err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("fetch exact source occurrence: %w", err)
	}
	if occ.SourceID != store.SourceID(pin.SourceID) || occ.Commit != pin.SourceCommit || occ.Acquisition != store.AcquisitionIndexOnly {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("source occurrence identity or acquisition mismatch")
	}
	if occ.SourceRevisionManifestDigest != store.RevisionDigest(pin.SourceManifestDigest) {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("source manifest digest mismatch: occurrence %q, pin %q", occ.SourceRevisionManifestDigest, pin.SourceManifestDigest)
	}
	if rev.Provenance == nil || rev.Provenance.SourceID != occ.SourceID || rev.Provenance.SourceID != store.SourceID(pin.SourceID) || rev.Provenance.Commit != occ.Commit || rev.Provenance.ManifestDigest != occ.SourceRevisionManifestDigest {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("resource revision provenance does not match exact source occurrence")
	}
	origin := strings.TrimSpace(occ.AcquisitionOrigin)
	if err := validateAcquisitionOrigin(origin); err != nil {
		slog.Warn("index-only acquisition origin rejected before allowed_registries", "origin", redact.URL(origin), "source_id", pin.SourceID, "error", err)
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("invalid acquisition origin before allowed_registries: %w", err)
	}
	if err := config.ValidateRegistryURLs([]config.RegistryEntry{{URL: origin, Ref: occ.Ref}}, allowedRegistries); err != nil {
		slog.Warn("index-only acquisition origin rejected before Git access", "origin", redact.URL(origin), "source_id", pin.SourceID, "error", err)
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("acquisition origin rejected by allowed_registries before Git access: %w", err)
	}
	slog.Info("index-only exact source acquisition approved", "origin", redact.URL(origin), "source_id", pin.SourceID, "commit", occ.Commit, "manifest_digest", occ.SourceRevisionManifestDigest)
	artifact.RegistryURL = origin
	artifact.Ref = occ.Commit
	if mode == resolver.MarketplaceResolveModeInspect {
		artifact.Unresolved = true
		return artifact, hccartifact.CanonicalArtifact{}, nil
	}

	source, err := client.GetSource(ctx, pin.SourceID)
	if err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("fetch source for exact acquisition: %w", err)
	}
	if source.ID != store.SourceID(pin.SourceID) || source.ID != occ.SourceID {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("source metadata identity does not match exact source occurrence")
	}
	source.Origins = []string{origin}
	source.Ref = occ.Ref
	source.Acquisition = store.AcquisitionIndexOnly
	acquired, err := acquire.CloneAtCommit(ctx, origin, occ.Ref, occ.Commit, acquire.Credentials{}, acquire.Limits{RepositoryBytes: 100 * 1024 * 1024, FileCount: 1024, FileBytes: 2 * 1024 * 1024}, 5*time.Minute)
	if err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("acquire exact source commit: %w", err)
	}
	defer func() { _ = acquired.Release() }()
	buildCfg := marketplaceconfig.Config{Policy: marketplaceconfig.Policy{HiddenContent: "warn", Limits: marketplaceconfig.Limits{IngestionDuration: marketplaceconfig.Duration(2 * time.Minute), ArtifactBytes: 16 * 1024 * 1024}}}
	built, err := ingest.BuildSourceRevision(ctx, buildCfg, store.MarketplaceID(entry.InstanceID), source, acquired.Dir, occ.Commit)
	if err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("re-ingest exact source commit: %w", err)
	}
	if built.ManifestDigest != occ.SourceRevisionManifestDigest {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("source manifest digest mismatch after re-ingestion: got %q, want %q", built.ManifestDigest, occ.SourceRevisionManifestDigest)
	}
	key := pin.Kind + "/" + pin.Name
	raw := built.ResourceArchives[key]
	if len(raw) == 0 {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("exact source commit did not contain an accepted %s %q", pin.Kind, pin.Name)
	}
	archiveDigest := built.ResourceArchiveDigest[key]
	if archiveDigest == "" {
		archiveDigest = hccartifact.ArchiveDigest(raw)
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
	if err := ca.VerifyContentDigest(); err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("verify re-ingested content: %w", err)
	}
	if err := validateReconstructedArtifactIdentity(pin, ca); err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, err
	}
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("write reconstructed index-only artifact cache: %w", err)
	}
	// Keep the cache untrusted until the payload has been decoded as the
	// requested resource. Archive/content digests alone do not prove that the
	// payload is a usable resource or that its identity matches this pin.
	resource, err := hccartifact.DecodeResource(pin.Kind, ca.Payload)
	if err != nil {
		cache.InvalidateArtifactVerified(archiveDigest)
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("decode re-ingested resource: %w", err)
	}
	if resource.Kind().String() != pin.Kind || resource.Name() != pin.Name {
		cache.InvalidateArtifactVerified(archiveDigest)
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("decoded resource identity does not match pin: got %s/%q, want %s/%q", resource.Kind(), resource.Name(), pin.Kind, pin.Name)
	}
	var supportFiles []string
	if skill, ok := resource.(*ir.Skill); ok {
		supportFiles = skill.SupportFiles
	}
	resourcePath, err := stageReconstructedResource(cache, archiveDigest, acquired.Dir, source, pin.Kind, pin.Name, supportFiles)
	if err != nil {
		cache.InvalidateArtifactVerified(archiveDigest)
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("stage reconstructed resource support files: %w", err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		return resolver.ResolvedArtifact{}, hccartifact.CanonicalArtifact{}, fmt.Errorf("mark index-only artifact verified: %w", err)
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
func stageReconstructedResource(cache *Cache, archiveDigest, checkoutDir string, source store.Source, kind, name string, supportFiles []string) (string, error) {
	var sourceDir string
	for _, root := range source.Roots {
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

	stagedDir := filepath.Join(filepath.Dir(cache.root), "compiled", "marketplace-sources", safeDigest(archiveDigest))
	if err := os.RemoveAll(stagedDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(stagedDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(stagedDir, "SKILL.md"), nil, 0o644); err != nil {
		return "", err
	}
	for _, supportFile := range supportFiles {
		clean := filepath.Clean(filepath.FromSlash(supportFile))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) || clean == "SKILL.md" {
			return "", fmt.Errorf("unsafe reconstructed support path %q", supportFile)
		}
		sourcePath := filepath.Join(sourceDir, clean)
		if err := validateCheckoutPath(checkoutDir, sourcePath, true); err != nil {
			return "", fmt.Errorf("unsafe support file %q: %w", supportFile, err)
		}
		info, err := os.Lstat(sourcePath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("unsupported support file %q", supportFile)
		}
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			return "", err
		}
		destinationPath := filepath.Join(stagedDir, clean)
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(destinationPath, data, info.Mode().Perm()); err != nil {
			return "", err
		}
	}
	slog.Debug("staged reconstructed resource support files", "archive_digest", archiveDigest, "source_dir", sourceDir, "files", len(supportFiles), "path", stagedDir)
	return filepath.Join(stagedDir, "SKILL.md"), nil
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

func (r *Resolver) getResourceRevision(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, mode resolver.MarketplaceResolveMode) (store.ResourceRevision, error) {
	if err := ctx.Err(); err != nil {
		return store.ResourceRevision{}, err
	}
	if mode != resolver.MarketplaceResolveModeRefresh {
		rev, ok, err := cache.ReadResourceRevision(entry.URL, pin.SourceID, pin.Kind, pin.Name, pin.Digest)
		if err != nil {
			return store.ResourceRevision{}, fmt.Errorf("read cached revision: %w", err)
		}
		if ok {
			return rev, nil
		}
		if mode == resolver.MarketplaceResolveModeCached {
			return store.ResourceRevision{}, fmt.Errorf("resource revision not in cache")
		}
		if mode == resolver.MarketplaceResolveModeCachedNoFetch {
			return store.ResourceRevision{}, fmt.Errorf("resource revision metadata is missing from cache")
		}
	}

	if client == nil {
		return store.ResourceRevision{}, fmt.Errorf("cached no-fetch resolver has no Marketplace client")
	}
	rev, err := client.GetResourceRevision(ctx, pin.SourceID, pin.Kind, pin.Name, pin.Digest)
	if err != nil {
		return store.ResourceRevision{}, fmt.Errorf("fetch resource revision: %w", err)
	}
	if err := validateResourceRevision(entry, pin, rev); err != nil {
		return store.ResourceRevision{}, err
	}
	if _, err := cache.WriteResourceRevision(entry.URL, pin.SourceID, pin.Kind, pin.Name, pin.Digest, rev); err != nil {
		return store.ResourceRevision{}, fmt.Errorf("write revision cache: %w", err)
	}
	return rev, nil
}

func validatePin(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) error {
	if strings.TrimSpace(entry.InstanceID) == "" {
		return fmt.Errorf("configured marketplace instance ID is required")
	}
	if strings.TrimSpace(pin.SourceID) == "" || strings.TrimSpace(pin.Kind) == "" || strings.TrimSpace(pin.Name) == "" {
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
		if !mpapi.IsCanonicalGitCommit(pin.SourceCommit) {
			return fmt.Errorf("index-only pin source commit must be a full lowercase Git commit")
		}
		if err := validateSHA256Digest(pin.SourceManifestDigest); err != nil {
			return fmt.Errorf("index-only source manifest digest %q: %w", pin.SourceManifestDigest, err)
		}
	}
	return nil
}

func validateResourceRevision(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, rev store.ResourceRevision) error {
	want := store.ResourceCoordinate{
		Marketplace: store.MarketplaceID(entry.InstanceID),
		Source:      store.SourceID(pin.SourceID),
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
	if rev.ArchiveDigest != "" {
		if err := validateSHA256Digest(string(rev.ArchiveDigest)); err != nil {
			return fmt.Errorf("revision archive digest %q: %w", rev.ArchiveDigest, err)
		}
	}
	if rev.Provenance != nil && rev.Provenance.SourceID != "" && rev.Provenance.SourceID != store.SourceID(pin.SourceID) {
		return fmt.Errorf("revision provenance source ID mismatch: metadata %q, pin %q", rev.Provenance.SourceID, pin.SourceID)
	}
	acquisition := pin.Acquisition
	if acquisition == "" {
		acquisition = config.MarketplaceAcquisitionMirror
	}
	if rev.Revocation == nil && rev.ArchiveDigest == "" && pin.Kind != "collection" && acquisition != config.MarketplaceAcquisitionIndexOnly {
		return fmt.Errorf("revision archive digest is required")
	}
	return nil
}

func validateReconstructedArtifactIdentity(pin config.MarketplaceResourcePin, artifact hccartifact.CanonicalArtifact) error {
	manifest := artifact.Manifest
	if manifest.SourceID != pin.SourceID {
		return fmt.Errorf("re-ingested resource provenance source ID mismatch: got %q, want %q", manifest.SourceID, pin.SourceID)
	}
	if manifest.ContentDigest != pin.Digest {
		return fmt.Errorf("re-ingested content digest mismatch: got %q, want %q", manifest.ContentDigest, pin.Digest)
	}
	if manifest.Name != pin.Name || manifest.Kind != pin.Kind {
		return fmt.Errorf("re-ingested resource identity mismatch: got %s/%q, want %s/%q", manifest.Kind, manifest.Name, pin.Kind, pin.Name)
	}
	return nil
}

func validateArtifactManifest(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, rev store.ResourceRevision, artifact hccartifact.CanonicalArtifact) error {
	manifest := artifact.Manifest
	if manifest.Kind != pin.Kind {
		return fmt.Errorf("artifact manifest kind mismatch: got %q, want %q", manifest.Kind, pin.Kind)
	}
	if manifest.Name != pin.Name {
		return fmt.Errorf("artifact manifest name mismatch: got %q, want %q", manifest.Name, pin.Name)
	}
	if manifest.SourceID != pin.SourceID {
		return fmt.Errorf("artifact manifest source ID mismatch: got %q, want %q", manifest.SourceID, pin.SourceID)
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
