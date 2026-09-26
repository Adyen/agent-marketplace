package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
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
func loadEffective(ctx context.Context, mode ConfigLoadMode, opts config.LoadEffectiveOptions, policy resolver.MarketplaceRevocationPolicy, browsingReady, provisionalReady func(*config.Config)) (config.EffectiveConfig, transientOrganizationMarketplaceLockUse) {
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
	if mode == ConfigLoadCached {
		return config.Bind(intent, evidence), transientOrganizationMarketplaceLockUnused
	}

	cacheReads, err := transientMarketplaceReadAssertions(intent, lockPath)
	if err != nil {
		return effectiveConfigError(config.Bind(intent, evidence), fmt.Errorf("snapshot transient organization Marketplace inputs: %w", err)), transientOrganizationMarketplaceLockUnused
	}
	reads, err := transientAcquisitionReadAssertions(intent, lockPath)
	if err != nil {
		return effectiveConfigError(config.Bind(intent, evidence), fmt.Errorf("snapshot transient organization Marketplace inputs: %w", err)), transientOrganizationMarketplaceLockUnused
	}
	if provisionalReady != nil || browsingReady != nil {
		provisional := config.Bind(intent, evidence)
		if provisional.Merged != nil {
			merged := provisionalMergedConfig(provisional.Merged, organization.MarketplaceSources())
			if provisionalReady != nil {
				provisionalReady(merged)
			}
			if browsingReady != nil {
				browsingReady(browsingMergedConfig(ctx, merged))
			}
		}
	}
	slog.Info("transient organization Marketplace lock acquisition started", "config", organization.Path)
	mpResolver := marketplace.NewResolver()
	mpResolver.AllowedRegistries = intent.AllowedRegistries()
	targets, _ := intent.TargetHarnesses()
	exact, err := marketplacelock.Resolve(ctx, mpResolver, organization, targets, policy)
	if err != nil {
		slog.Error("transient organization Marketplace lock acquisition failed", "config", organization.Path, "error", err)
		return effectiveConfigError(config.Bind(intent, evidence), fmt.Errorf("acquire transient organization Marketplace lock: %w", err)), transientOrganizationMarketplaceLockUnused
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
	slog.Info("transient organization Marketplace lock acquisition completed", "config", organization.Path, "resources", marketplaceLockResourceCount(exact))
	return finalEC, transientOrganizationMarketplaceLockAcquired
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
