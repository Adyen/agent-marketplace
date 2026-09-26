package codemode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type MarketplaceOrigin struct {
	Entry      config.MarketplaceEntry
	RegistryID string
	Kind       string
	Name       string
	Revision   string
}

func (r *Resource) MarketplaceOrigin() *MarketplaceOrigin {
	if r == nil || r.origin == nil {
		return nil
	}
	return r.origin.Marketplace
}

type SnapshotBuilder struct {
	CatalogMode catalog.LoadMode
	RepoRoot    string

	Discovery func(ec config.EffectiveConfig) ([]marketplace.DiscoveryOutcome, error)

	Cache *marketplace.Cache
}

func (b *SnapshotBuilder) BuildSnapshot(ctx context.Context, ec config.EffectiveConfig) (*Snapshot, []error) {
	snap := &Snapshot{AsOf: time.Now()}
	var errs []error
	mode := b.CatalogMode

	cfg := ec.Merged
	if cfg == nil {
		return &Snapshot{Stale: true, AsOf: time.Now()}, nil
	}

	if len(cfg.Registries) > 0 {
		result := catalog.LoadRegistriesWithContext(ctx, cfg, b.RepoRoot, mode)
		errs = append(errs, result.Errors...)
		for i := range cfg.Registries {
			entry := cfg.Registries[i]
			key, kerr := cache.RegistryKey(entry, b.RepoRoot)
			if kerr != nil {
				continue
			}
			loaded, ok := result.Catalogs[key]
			if !ok {
				continue
			}
			bundleBase := cache.BaseCachePath(key)
			registryLabel := registryLabel(entry)
			for i := range loaded.Entries {
				e := loaded.Entries[i]
				res := Resource{
					Kind:        normalizeKind(e.Kind.String()),
					Name:        e.Name,
					Description: e.Description,
					Tags:        e.Tags,
					Registry:    registryLabel,
					Revision:    entry.Ref,
					Assessment:  "allow",
				}
				res.Ref = registryLabel + "/" + res.Kind + "/" + res.Name
				res.origin = registryOrigin(bundleBase, e)
				if res.Kind == "mcp" {
					res.Tools = "unknown"
				}
				snap.Resources = append(snap.Resources, res)
			}
		}
	}

	if len(cfg.Marketplaces) > 0 {
		mc := b.Cache
		if mc == nil {
			mc = marketplace.DefaultCache()
		}
		discover := b.Discovery
		if discover == nil {
			discover = func(ec config.EffectiveConfig) ([]marketplace.DiscoveryOutcome, error) {
				outcomes, _, err := marketplace.CachedDiscovery(ec, "", marketplace.DiscoveryOptions{
					Limit: marketplace.DiscoveryPageLimit * 20,
					Cache: marketplace.DefaultDiscoveryCache(),
				})
				return outcomes, err
			}
		}
		outcomes, derr := discover(ec)
		if derr != nil {
			snap.Stale = true
			errs = append(errs, derr)
		}
		for _, oc := range outcomes {
			if oc.Err != nil || oc.CacheState == marketplace.DiscoveryCacheMiss {
				snap.Stale = true
				continue
			}
			if oc.CacheState == marketplace.DiscoveryCacheStale {
				snap.Stale = true
			}
			alias := oc.Endpoint.Alias
			if alias == "" {
				alias = oc.Endpoint.InstanceID
			}
			for _, item := range oc.Items {
				if excludedAvailability(item.Availability) {
					continue
				}
				slug := item.RegistrySlug
				if slug == "" {
					slug = item.RegistryID
				}
				kind := normalizeKind(item.Kind)
				res := Resource{
					Ref:         alias + ":" + slug + "/" + kind + "/" + item.Name,
					Kind:        kind,
					Name:        item.Name,
					Description: item.Description,
					Registry:    slug,
					Revision:    item.Revision,
					Assessment:  "allow",
				}
				res.origin = &Origin{Marketplace: &MarketplaceOrigin{
					Entry:      oc.Endpoint,
					RegistryID: item.RegistryID,
					Kind:       item.Kind,
					Name:       item.Name,
					Revision:   item.Revision,
				}}

				if !b.applyCachedGate(mc, oc.Endpoint.URL, item, &res) {
					continue
				}
				if item.Kind == "mcp" {
					res.Tools = "unknown"
				}
				snap.Resources = append(snap.Resources, res)
			}
		}
	}
	return snap, errs
}

func (b *SnapshotBuilder) applyCachedGate(mc *marketplace.Cache, endpointURL string, item mpapi.ResourceListItem, res *Resource) bool {
	if mc == nil {
		return true
	}
	rev, ok, err := mc.ReadResourceRevision(endpointURL, item.RegistryID, item.Kind, item.Name, item.Revision)
	if err != nil || !ok {
		return true
	}
	if rev.Revocation != nil {
		return false
	}
	switch rev.Assessment.Decision {
	case store.AssessmentDecisionReject:
		return false
	case store.AssessmentDecisionWarn:
		res.Assessment = "warn"
	}
	for _, f := range rev.Assessment.Findings {
		res.Findings = append(res.Findings, Finding{Rule: f.Code, Severity: f.Severity, Message: f.Message})
	}
	return true
}

func normalizeKind(kind string) string {
	if kind == "mcpserver" {
		return "mcp"
	}
	return kind
}

func excludedAvailability(a mpapi.Availability) bool {
	switch a {
	case "", mpapi.AvailabilityAvailable:
		return false
	default:
		return true
	}
}

func registryLabel(entry config.RegistryEntry) string {
	label := entry.URL
	if entry.IsLocal() {
		label = "local:" + filepath.Base(strings.TrimRight(entry.URL, "/"))
	}
	label = strings.TrimPrefix(label, "https://")
	label = strings.TrimPrefix(label, "http://")
	label = strings.TrimPrefix(label, "git@")
	label = strings.ReplaceAll(label, "/", "-")
	label = strings.TrimSuffix(label, ".git")
	return label
}

func registryOrigin(bundleBase string, e catalog.Entry) *Origin {
	dir := bundleBase
	file := ""
	if e.Path != "" {
		if filepath.IsAbs(e.Path) {
			dir = filepath.Dir(e.Path)
		} else {
			dir = filepath.Join(bundleBase, filepath.Dir(e.Path))
		}
		file = filepath.Base(e.Path)
	}
	o := &Origin{ContentDir: dir, File: file}
	if srv, ok := e.Resource.(*ir.McpServer); ok {
		o.Server = &ServerDef{
			Command: srv.Command,
			Args:    srv.Args,
			Env:     srv.Env,
			URL:     srv.URL,
			Headers: srv.Headers,
			Digest:  serverDefDigest(srv),
		}
	}
	return o
}

func serverDefDigest(srv *ir.McpServer) string {
	raw, _ := json.Marshal(map[string]any{
		"command": srv.Command, "args": srv.Args, "env": srv.Env,
		"url": srv.URL, "headers": srv.Headers,
	})
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
