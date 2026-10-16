package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

type ConfigLoadMode uint8

const (
	ConfigLoadOnline ConfigLoadMode = iota
	ConfigLoadStrict
	ConfigLoadCached
)

// LoadEffective loads configuration with explicit organization-lock semantics:
// online acquisition, strict on-disk evidence, or reuse of verified cached evidence.
func LoadEffective(ctx context.Context, mode ConfigLoadMode, opts config.LoadEffectiveOptions, policy resolver.MarketplaceRevocationPolicy) config.EffectiveConfig {
	ec, _ := loadEffective(ctx, mode, opts, policy, nil, nil)
	return ec
}

// loadEffective also reports whether organization Marketplace state was acquired
// or reused. Strict loads never consult the transient cache; cached loads never
// contact the Marketplace.
func loadEffective(ctx context.Context, mode ConfigLoadMode, opts config.LoadEffectiveOptions, policy resolver.MarketplaceRevocationPolicy, browsingReady func(*config.Config), provisionalReady func(*config.Config, *config.LocalConfig)) (config.EffectiveConfig, transientOrganizationMarketplaceLockUse) {
	if mode != ConfigLoadOnline && mode != ConfigLoadStrict && mode != ConfigLoadCached {
		return effectiveConfigError(config.EffectiveConfig{}, fmt.Errorf("invalid config load mode: %d", mode)), transientOrganizationMarketplaceLockUnused
	}
	intent := readIntent(ctx, opts)
	if len(intent.Errors) > 0 {
		return config.Bind(intent, config.Evidence{}), transientOrganizationMarketplaceLockUnused
	}
	evidence, err := readDeployedLocks(ctx, intent, opts.FileOverlays)
	if err != nil && (mode != ConfigLoadOnline || !isMissingLockError(err)) {
		return effectiveConfigError(config.Bind(intent, evidence), err), transientOrganizationMarketplaceLockUnused
	}
	organization := intent.Layers[config.Organization]
	if mode == ConfigLoadStrict || !organization.Present() || !organization.NeedsMarketplaceLock() || evidence.Locks[config.Organization] != nil {
		return config.Bind(intent, evidence), transientOrganizationMarketplaceLockUnused
	}
	if err := MarketplaceAllowlistError(intent); err != nil {
		return effectiveConfigError(config.Bind(intent, evidence), err), transientOrganizationMarketplaceLockUnused
	}
	paths, err := config.ResolveConfigLockPaths(organization.Path)
	if err != nil {
		return effectiveConfigError(config.Bind(intent, evidence), err), transientOrganizationMarketplaceLockUnused
	}
	lockPath := paths.CanonicalLock
	inputs := transientOrganizationMarketplaceInputs(intent)
	if cache := transientOrganizationMarketplaceLockCacheFrom(ctx); cache != nil {
		if doc, reads, cachedInputs, ok := cache.load(lockPath); ok {
			if err := dependencytxn.ValidateReadAssertions(reads); err != nil || cachedInputs != inputs {
				cache.delete(lockPath)
			} else {
				evidence.Locks[config.Organization] = &doc
				cachedEC := config.Bind(intent, evidence)
				if len(cachedEC.Errors) > 0 {
					return cachedEC, transientOrganizationMarketplaceLockUnused
				}
				if err := dependencytxn.ValidateReadAssertions(reads); err != nil {
					cache.delete(lockPath)
					return effectiveConfigError(cachedEC, fmt.Errorf("%w: organization Marketplace inputs changed while reusing transient acquisition: %v", dependencytxn.ErrGenerationConflict, err)), transientOrganizationMarketplaceLockUnused
				}
				slog.Info("reused transient organization Marketplace lock acquisition", "config", organization.Path)
				return cachedEC, transientOrganizationMarketplaceLockReused
			}
		}
	}
	cacheReads, err := transientMarketplaceReadAssertions(intent, lockPath)
	if err != nil {
		return effectiveConfigError(config.Bind(intent, evidence), fmt.Errorf("snapshot transient organization Marketplace inputs: %w", err)), transientOrganizationMarketplaceLockUnused
	}
	reads, err := transientAcquisitionReadAssertions(intent, lockPath)
	if err != nil {
		return effectiveConfigError(config.Bind(intent, evidence), fmt.Errorf("snapshot transient organization Marketplace inputs: %w", err)), transientOrganizationMarketplaceLockUnused
	}
	if mode == ConfigLoadOnline && (provisionalReady != nil || browsingReady != nil) {
		provisional := config.Bind(intent, evidence)
		if provisional.Merged != nil {
			merged := provisionalMergedConfig(provisional.Merged, organization.MarketplaceSources())
			if provisionalReady != nil {
				provisionalReady(merged, provisional.Local)
			}
			if browsingReady != nil {
				browsingReady(browsingMergedConfig(ctx, merged))
			}
		}
	}
	var exact lockfile.Document
	if mode == ConfigLoadCached {
		exact, err = cachedOrganizationMarketplaceLock(ctx, organization)
		if err != nil {
			bound := config.Bind(intent, evidence)
			// Retain the missing-evidence diagnostic as well as its cache cause:
			// callers must not mistake a failed cached read for an acquired lock.
			cause := errors.Join(errors.Join(bound.Organization.Errors...), fmt.Errorf("reuse cached organization Marketplace lock evidence: %w", err))
			return effectiveConfigError(bound, cause), transientOrganizationMarketplaceLockUnused
		}
	} else {
		slog.Info("transient organization Marketplace lock acquisition started", "config", organization.Path)
		mpResolver := marketplace.NewResolver()
		mpResolver.AllowedRegistries = intent.AllowedRegistries()
		targets, _ := intent.TargetHarnesses()
		exact, err = marketplacelock.Resolve(ctx, mpResolver, organization, targets, policy)
		if err != nil {
			slog.Error("transient organization Marketplace lock acquisition failed", "config", organization.Path, "error", err)
			return effectiveConfigError(config.Bind(intent, evidence), fmt.Errorf("acquire transient organization Marketplace lock: %w", err)), transientOrganizationMarketplaceLockUnused
		}
	}
	if err := dependencytxn.ValidateReadAssertions(reads); err != nil {
		return effectiveConfigError(config.Bind(intent, evidence), fmt.Errorf("%w: organization Marketplace inputs changed during transient acquisition: %v", dependencytxn.ErrGenerationConflict, err)), transientOrganizationMarketplaceLockUnused
	}
	evidence.Locks[config.Organization] = &exact
	finalEC := config.Bind(intent, evidence)
	if len(finalEC.Errors) > 0 {
		return finalEC, transientOrganizationMarketplaceLockUnused
	}
	if err := dependencytxn.ValidateReadAssertions(reads); err != nil {
		return effectiveConfigError(finalEC, fmt.Errorf("%w: organization Marketplace inputs changed while validating transient acquisition: %v", dependencytxn.ErrGenerationConflict, err)), transientOrganizationMarketplaceLockUnused
	}
	if cache := transientOrganizationMarketplaceLockCacheFrom(ctx); cache != nil {
		cache.store(lockPath, exact, cacheReads, inputs)
	}
	if mode == ConfigLoadCached {
		slog.Info("reused verified cached organization Marketplace lock evidence", "config", organization.Path, "resources", marketplaceLockResourceCount(exact))
		return finalEC, transientOrganizationMarketplaceLockReused
	}
	slog.Info("transient organization Marketplace lock acquisition completed", "config", organization.Path, "resources", marketplaceLockResourceCount(exact))
	return finalEC, transientOrganizationMarketplaceLockAcquired
}

// cachedOrganizationMarketplaceLock reads only the exact coordinates authored in
// the organization configuration. Endpoint-scoped revision metadata supplies the
// previously observed instance identity, and VerifiedMarketplaceLockResource
// checks it against the requested coordinate/digest and verified artifact bytes.
// Never scan metadata/discovery namespaces or choose a revision, registry binding,
// or instance identity that the exact cache lookup cannot prove. Floating requests,
// slug-to-ID lookup, index-only occurrences, and collection expansion require a
// deployed lock or already-verified invocation evidence, not an offline bootstrap.
// The normal cached Marketplace resolver still enforces assessment and revocation
// policy before these bound resources can become a materialization desired set.
func cachedOrganizationMarketplaceLock(ctx context.Context, organization config.LayerIntent) (lockfile.Document, error) {
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeOrganization}
	cache := marketplace.DefaultCache()
	for _, source := range organization.MarketplaceSources() {
		if len(source.Requests) == 0 {
			// Source-only registrations have no preservation desired set and
			// cannot authorize discovering an identity from the cache.
			continue
		}
		locked := lockfile.MarketplaceLock{URL: source.URL}
		bindings := make(map[string]bool)
		for _, request := range source.Requests {
			if err := ctx.Err(); err != nil {
				return lockfile.Document{}, err
			}
			digest, err := lockfile.ParseDigest(request.Options.Revision)
			if err != nil {
				return lockfile.Document{}, fmt.Errorf("%s requires exact cached revision evidence: %w", request.Locator, err)
			}
			if request.MarketplaceKind() == "collection" {
				return lockfile.Document{}, fmt.Errorf("%s requires existing collection lock evidence", request.Locator)
			}
			revision, found, err := cache.ReadResourceRevision(source.URL, request.Registry, request.MarketplaceKind(), request.Name, digest.String())
			if err != nil {
				return lockfile.Document{}, fmt.Errorf("read %s cached revision evidence: %w", request.Locator, err)
			}
			if !found {
				return lockfile.Document{}, fmt.Errorf("%s has no exact cached revision evidence", request.Locator)
			}
			instanceID := string(revision.Coordinate.Marketplace)
			if instanceID == "" || (locked.InstanceID != "" && locked.InstanceID != instanceID) {
				return lockfile.Document{}, fmt.Errorf("%s has missing or conflicting cached Marketplace identity", request.Locator)
			}
			entry := config.MarketplaceEntry{URL: source.URL, InstanceID: instanceID, Source: config.Organization}
			pin := config.MarketplaceResourcePin{
				RegistryID: request.Registry, RegistrySlug: request.Registry,
				Kind: request.MarketplaceKind(), Name: request.Name,
				Revision: digest.String(), Digest: digest.String(),
				Acquisition: config.MarketplaceAcquisitionMirror,
			}
			record, err := cache.VerifiedMarketplaceLockResource(entry, pin)
			if err != nil {
				return lockfile.Document{}, fmt.Errorf("verify %s cached lock evidence: %w", request.Locator, err)
			}
			locked.InstanceID = instanceID
			locked.Resources = append(locked.Resources, record)
			if !bindings[request.Registry] {
				locked.RegistryBindings = append(locked.RegistryBindings, lockfile.MarketplaceRegistryBinding{Name: request.Registry, RegistryID: request.Registry})
				bindings[request.Registry] = true
			}
		}
		doc.Marketplaces = append(doc.Marketplaces, locked)
	}
	return lockfile.Normalize(doc)
}

const browsingIdentityTimeout = 5 * time.Second

func provisionalMergedConfig(merged *config.Config, sources []config.MarketplaceSource) *config.Config {
	out := *merged
	out.Marketplaces = append([]config.MarketplaceEntry(nil), merged.Marketplaces...)
	for _, source := range sources {
		entry := config.MarketplaceEntry{URL: source.URL, Source: config.Organization}
		for _, request := range source.Requests {
			entry.Resources = append(entry.Resources, config.MarketplaceResourcePin{
				RegistryID: request.Registry, Kind: request.MarketplaceKind(), Name: request.Name, Direct: true,
			})
		}
		out.Marketplaces = append(out.Marketplaces, entry)
	}
	return &out
}

func browsingMergedConfig(ctx context.Context, merged *config.Config) *config.Config {
	out := *merged
	entries := make([]config.MarketplaceEntry, 0, len(merged.Marketplaces))
	for _, entry := range merged.Marketplaces {
		if entry.Source == config.Organization {
			bound, ok := bindBrowsingInstanceID(ctx, entry)
			if !ok {
				slog.Warn("organization Marketplace endpoint excluded from browsing config: identity unavailable", "url", entry.URL)
				continue
			}
			entries = append(entries, bound)
			continue
		}
		entries = append(entries, entry)
	}
	out.Marketplaces = entries
	return &out
}

func bindBrowsingInstanceID(ctx context.Context, entry config.MarketplaceEntry) (config.MarketplaceEntry, bool) {
	client, err := marketplace.NewClient(entry.URL, marketplace.WithTimeout(browsingIdentityTimeout))
	if err != nil {
		return entry, false
	}
	wkCtx, cancel := context.WithTimeout(ctx, browsingIdentityTimeout)
	defer cancel()
	wk, err := client.WellKnown(wkCtx)
	if err != nil {
		return entry, false
	}
	entry.InstanceID = wk.InstanceID
	entry.Resources = nil
	return entry, true
}
