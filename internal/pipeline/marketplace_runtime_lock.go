package pipeline

import (
	"context"
	"fmt"
	"log/slog"

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
	ec, _ := loadEffective(ctx, mode, opts, policy)
	return ec
}

// loadEffective also reports whether organization Marketplace state was acquired
// or reused. Strict loads never consult the transient cache; cached loads never
// contact the Marketplace.
func loadEffective(ctx context.Context, mode ConfigLoadMode, opts config.LoadEffectiveOptions, policy resolver.MarketplaceRevocationPolicy) (config.EffectiveConfig, transientOrganizationMarketplaceLockUse) {
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
