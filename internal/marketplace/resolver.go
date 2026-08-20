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
	for _, entry := range cfg.Marketplaces {
		client, err := r.clientForEntry(entry)
		if err != nil {
			errs = append(errs, fmt.Errorf("marketplace %s: %w", entry.URL, err))
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
	kind, err := irKind(pin.Kind)
	if err != nil {
		return resolver.ResolvedArtifact{}, err
	}

	acquisition := pin.Acquisition
	if acquisition == "" {
		acquisition = config.MarketplaceAcquisitionMirror
	}
	if acquisition != config.MarketplaceAcquisitionMirror {
		return resolver.ResolvedArtifact{}, fmt.Errorf("acquisition mode %q is not supported", acquisition)
	}

	if pin.Kind == "collection" {
		return resolver.ResolvedArtifact{}, fmt.Errorf("collection resolution is not supported yet")
	}

	rev, err := r.getResourceRevision(ctx, client, cache, entry, pin, mode)
	if err != nil {
		return resolver.ResolvedArtifact{}, err
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
		RegistryURL:           normalizeMarketplaceURL(entry.URL),
		RegistryPath:          pin.SourceID,
		Ref:                   pin.Revision,
		Source:                entry.Source,
		Required:              false,
		CatalogKey:            fmt.Sprintf("marketplace:%s:%s:%s:%s", entry.URL, pin.SourceID, pin.Digest, pin.Revision),
		Harnesses:             targetSet,
		IsMarketplace:         true,
		MarketplaceURL:        normalizeMarketplaceURL(entry.URL),
		MarketplaceInstanceID: entry.InstanceID,
		MarketplaceAlias:      entry.Alias,
		MarketplaceSourceID:   pin.SourceID,
		InstalledName:         pin.InstalledName,
		Revoked:               rev.Revocation != nil,
	}

	if rev.Revocation != nil {
		return artifact, fmt.Errorf("revision is revoked")
	}

	if mode == resolver.MarketplaceResolveModeInspect {
		artifact.Unresolved = true
		return artifact, nil
	}

	raw, verified, err := cache.ReadArtifact(pin.Digest)
	if err != nil {
		return resolver.ResolvedArtifact{}, fmt.Errorf("read cached artifact: %w", err)
	}
	if !verified || raw == nil {
		if mode == resolver.MarketplaceResolveModeCached {
			return resolver.ResolvedArtifact{}, fmt.Errorf("artifact not in cache")
		}
		slog.Debug("marketplace fetching artifact", "url", entry.URL, "source", pin.SourceID, "kind", pin.Kind, "name", pin.Name, "archive_digest", rev.ArchiveDigest)
		raw, err = client.GetArtifact(ctx, string(rev.ArchiveDigest))
		if err != nil {
			return resolver.ResolvedArtifact{}, fmt.Errorf("fetch artifact: %w", err)
		}
		if _, err := cache.WriteArtifact(pin.Digest, raw); err != nil {
			return resolver.ResolvedArtifact{}, fmt.Errorf("write artifact cache: %w", err)
		}
		verified = false
	}

	ca, caRaw, err := hccartifact.Decode(bytes.NewReader(raw))
	if err != nil {
		return resolver.ResolvedArtifact{}, fmt.Errorf("decode artifact: %w", err)
	}
	if err := ca.VerifyArchiveDigest(caRaw); err != nil {
		return resolver.ResolvedArtifact{}, fmt.Errorf("verify archive digest: %w", err)
	}
	if err := ca.VerifyContentDigest(); err != nil {
		return resolver.ResolvedArtifact{}, fmt.Errorf("verify content digest: %w", err)
	}
	if ca.Manifest.ContentDigest != pin.Digest {
		return resolver.ResolvedArtifact{}, fmt.Errorf("content digest mismatch: cache %q, pin %q", ca.Manifest.ContentDigest, pin.Digest)
	}
	if ca.Manifest.ArchiveDigest != string(rev.ArchiveDigest) {
		return resolver.ResolvedArtifact{}, fmt.Errorf("archive digest mismatch: cache %q, revision %q", ca.Manifest.ArchiveDigest, rev.ArchiveDigest)
	}

	resource, err := hccartifact.DecodeResource(pin.Kind, ca.Payload)
	if err != nil {
		return resolver.ResolvedArtifact{}, fmt.Errorf("decode resource: %w", err)
	}
	artifact.Resource = resource
	artifact.Description = descriptionFromResource(resource)
	artifact.Path = cache.ArtifactPath(pin.Digest)

	if !verified {
		if err := cache.MarkArtifactVerified(pin.Digest); err != nil {
			return resolver.ResolvedArtifact{}, fmt.Errorf("mark artifact verified: %w", err)
		}
	}
	return artifact, nil
}

func (r *Resolver) getResourceRevision(ctx context.Context, client *Client, cache *Cache, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, mode resolver.MarketplaceResolveMode) (store.ResourceRevision, error) {
	rev, ok, err := cache.ReadResourceRevision(entry.URL, pin.SourceID, pin.Kind, pin.Name, pin.Revision)
	if err != nil {
		return store.ResourceRevision{}, fmt.Errorf("read cached revision: %w", err)
	}
	if ok {
		return rev, nil
	}
	if mode == resolver.MarketplaceResolveModeCached {
		return store.ResourceRevision{}, fmt.Errorf("resource revision not in cache")
	}
	rev, err = client.GetResourceRevision(ctx, pin.SourceID, pin.Kind, pin.Name, pin.Revision)
	if err != nil {
		return store.ResourceRevision{}, fmt.Errorf("fetch resource revision: %w", err)
	}
	if _, err := cache.WriteResourceRevision(entry.URL, pin.SourceID, pin.Kind, pin.Name, pin.Revision, rev); err != nil {
		return store.ResourceRevision{}, fmt.Errorf("write revision cache: %w", err)
	}
	return rev, nil
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

func normalizeMarketplaceURL(u string) string {
	return strings.TrimRight(u, "/")
}
