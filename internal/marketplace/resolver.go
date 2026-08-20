package marketplace

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

// Resolver resolves Marketplace entries into resolver.ResolvedArtifact values.
type Resolver struct {
	HTTPClient *http.Client
	Cache      *Cache
}

// NewResolver creates a Marketplace resolver with a default cache.
func NewResolver() *Resolver {
	return &Resolver{Cache: DefaultCache()}
}

// Resolve implements resolver.MarketplaceResolver.
func (r *Resolver) Resolve(ctx context.Context, cfg *config.Config, targetSet []hcc.Target, mode resolver.MarketplaceResolveMode) ([]resolver.ResolvedArtifact, []error) {
	if cfg == nil || len(cfg.Marketplaces) == 0 {
		return nil, nil
	}

	var allowed []string
	if cfg.AllowedMarketplaces != nil {
		allowed = *cfg.AllowedMarketplaces
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
		client, err := r.clientForEntry(entry)
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
		for _, pin := range entry.Resources {
			resolved, err := r.resolvePin(ctx, client, cache, entry, pin, targetSet, mode)
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

func (r *Resolver) resolvePin(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, targetSet []hcc.Target, mode resolver.MarketplaceResolveMode) (resolver.ResolvedArtifact, error) {
	artifact, _, err := r.resolvePinWithArtifact(ctx, client, cache, entry, pin, targetSet, mode)
	return artifact, err
}

// resolvePinWithArtifact returns the same resolved artifact as resolvePin and
// also retains the CanonicalArtifact decoded and verified during resolution.
// Callers that need metadata for a prepared install must use this value rather
// than rereading the cache, since the cache is intentionally disposable.
func (r *Resolver) resolvePinWithArtifact(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, targetSet []hcc.Target, mode resolver.MarketplaceResolveMode) (resolver.ResolvedArtifact, hccartifact.CanonicalArtifact, error) {
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

	artifact := resolver.ResolvedArtifact{
		Entry: catalog.Entry{
			Kind: kind,
			Name: name,
		},
		RegistryURL:           entry.URL,
		RegistryPath:          pin.SourceID,
		Ref:                   pin.Revision,
		Source:                entry.Source,
		Required:              false,
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

	if rev.Revocation != nil {
		return artifact, hccartifact.CanonicalArtifact{}, fmt.Errorf("revision is revoked")
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

func (r *Resolver) getResourceRevision(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, mode resolver.MarketplaceResolveMode) (store.ResourceRevision, error) {
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
	if rev.Revocation == nil && rev.ArchiveDigest == "" {
		return fmt.Errorf("revision archive digest is required")
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
