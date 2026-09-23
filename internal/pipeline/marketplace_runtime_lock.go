package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

type transientOrganizationMarketplaceLockCacheKey struct{}

type transientOrganizationMarketplaceLockCache struct {
	mu    sync.Mutex
	path  string
	entry cachedTransientOrganizationMarketplaceLock
}

type cachedTransientOrganizationMarketplaceLock struct {
	data  []byte
	reads []dependencytxn.ReadAssertion
}

type transientOrganizationMarketplaceLockUse uint8

const (
	transientOrganizationMarketplaceLockUnused transientOrganizationMarketplaceLockUse = iota
	transientOrganizationMarketplaceLockAcquired
	transientOrganizationMarketplaceLockReused
)

// WithTransientOrganizationMarketplaceLockCache reuses verified in-memory
// organization acquisition evidence while every policy read and lock absence
// assertion still matches. It never weakens a deployed lock.
func WithTransientOrganizationMarketplaceLockCache(ctx context.Context) context.Context {
	if transientOrganizationMarketplaceLockCacheFrom(ctx) != nil {
		return ctx
	}
	cache := &transientOrganizationMarketplaceLockCache{}
	return context.WithValue(ctx, transientOrganizationMarketplaceLockCacheKey{}, cache)
}

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
	switch mode {
	case ConfigLoadOnline, ConfigLoadCached:
		return loadEffectiveWithTransientOrganizationLock(ctx, opts, policy, mode == ConfigLoadOnline)
	case ConfigLoadStrict:
		return config.LoadEffective(ctx, opts), transientOrganizationMarketplaceLockUnused
	default:
		return effectiveConfigError(config.EffectiveConfig{}, fmt.Errorf("invalid config load mode: %d", mode)), transientOrganizationMarketplaceLockUnused
	}
}

func loadEffectiveWithTransientOrganizationLock(ctx context.Context, opts config.LoadEffectiveOptions, policy resolver.MarketplaceRevocationPolicy, allowAcquire bool) (config.EffectiveConfig, transientOrganizationMarketplaceLockUse) {
	initial := config.LoadEffective(ctx, opts)
	if !marketplacelock.HasRequiredError(initial.Errors) {
		return initial, transientOrganizationMarketplaceLockUnused
	}

	orgPath := opts.OrganizationPath
	if orgPath == "" {
		orgPath = config.OrganizationConfigPath()
	}
	layer, provisionalData, missing, err := marketplacelock.OrganizationProvisional(orgPath)
	if err != nil {
		return effectiveConfigError(initial, fmt.Errorf("inspect organization Marketplace lock: %w", err)), transientOrganizationMarketplaceLockUnused
	}
	if !missing {
		return initial, transientOrganizationMarketplaceLockUnused
	}
	cache := transientOrganizationMarketplaceLockCacheFrom(ctx)
	if cache != nil {
		if data, reads, ok := cache.load(layer.LockPath); ok {
			if err := dependencytxn.ValidateReadAssertions(reads); err == nil {
				cachedOpts := opts
				cachedOpts.OrganizationRuntimeLock = data
				cachedEC := config.LoadEffective(ctx, cachedOpts)
				if len(cachedEC.Errors) > 0 {
					return cachedEC, transientOrganizationMarketplaceLockUnused
				}
				if err := dependencytxn.ValidateReadAssertions(reads); err != nil {
					cache.delete(layer.LockPath)
					return effectiveConfigError(cachedEC, fmt.Errorf("%w: organization Marketplace inputs changed while reusing transient acquisition: %v", dependencytxn.ErrGenerationConflict, err)), transientOrganizationMarketplaceLockUnused
				}
				slog.Info("reused transient organization Marketplace lock acquisition", "config", orgPath)
				return cachedEC, transientOrganizationMarketplaceLockReused
			}
			cache.delete(layer.LockPath)
		}
	}
	if !allowAcquire {
		return initial, transientOrganizationMarketplaceLockUnused
	}

	provisionalOpts := opts
	provisionalOpts.OrganizationRuntimeLock = provisionalData
	provisionalEC := config.LoadEffective(ctx, provisionalOpts)
	if len(provisionalEC.Errors) > 0 {
		return provisionalEC, transientOrganizationMarketplaceLockUnused
	}
	reads, err := transientMarketplaceReadAssertions(provisionalEC, layer.LockPath)
	if err != nil {
		return effectiveConfigError(provisionalEC, fmt.Errorf("snapshot transient organization Marketplace inputs: %w", err)), transientOrganizationMarketplaceLockUnused
	}

	slog.Info("transient organization Marketplace lock acquisition started", "config", orgPath)
	mpResolver := marketplace.NewResolver()
	mpResolver.AllowedRegistries = provisionalEC.Allowed
	exact, err := marketplacelock.Resolve(ctx, mpResolver, layer, provisionalEC.TargetHarnesses, policy)
	if err != nil {
		slog.Error("transient organization Marketplace lock acquisition failed", "config", orgPath, "error", err)
		return effectiveConfigError(provisionalEC, fmt.Errorf("acquire transient organization Marketplace lock: %w", err)), transientOrganizationMarketplaceLockUnused
	}
	exactData, err := lockfile.Encode(exact)
	if err != nil {
		return effectiveConfigError(provisionalEC, fmt.Errorf("encode transient organization Marketplace lock: %w", err)), transientOrganizationMarketplaceLockUnused
	}
	if err := dependencytxn.ValidateReadAssertions(reads); err != nil {
		return effectiveConfigError(provisionalEC, fmt.Errorf("%w: organization Marketplace inputs changed during transient acquisition: %v", dependencytxn.ErrGenerationConflict, err)), transientOrganizationMarketplaceLockUnused
	}

	finalOpts := opts
	finalOpts.OrganizationRuntimeLock = exactData
	finalEC := config.LoadEffective(ctx, finalOpts)
	if len(finalEC.Errors) > 0 {
		return finalEC, transientOrganizationMarketplaceLockUnused
	}
	if err := dependencytxn.ValidateReadAssertions(reads); err != nil {
		return effectiveConfigError(finalEC, fmt.Errorf("%w: organization Marketplace inputs changed while validating transient acquisition: %v", dependencytxn.ErrGenerationConflict, err)), transientOrganizationMarketplaceLockUnused
	}
	if cache != nil {
		cache.store(layer.LockPath, exactData, reads)
	}
	slog.Info("transient organization Marketplace lock acquisition completed", "config", orgPath, "resources", marketplaceLockResourceCount(exact))
	return finalEC, transientOrganizationMarketplaceLockAcquired
}

func transientOrganizationMarketplaceLockCacheFrom(ctx context.Context) *transientOrganizationMarketplaceLockCache {
	if ctx == nil {
		return nil
	}
	cache, _ := ctx.Value(transientOrganizationMarketplaceLockCacheKey{}).(*transientOrganizationMarketplaceLockCache)
	return cache
}

func (c *transientOrganizationMarketplaceLockCache) load(path string) ([]byte, []dependencytxn.ReadAssertion, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path != path {
		return nil, nil, false
	}
	entry := c.entry
	return append([]byte(nil), entry.data...), append([]dependencytxn.ReadAssertion(nil), entry.reads...), true
}

func (c *transientOrganizationMarketplaceLockCache) store(path string, data []byte, reads []dependencytxn.ReadAssertion) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.path = path
	c.entry = cachedTransientOrganizationMarketplaceLock{
		data:  append([]byte(nil), data...),
		reads: append([]dependencytxn.ReadAssertion(nil), reads...),
	}
}

func (c *transientOrganizationMarketplaceLockCache) delete(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == path {
		c.path = ""
		c.entry = cachedTransientOrganizationMarketplaceLock{}
	}
}

type transientOrganizationMarketplaceResolver struct {
	delegate *marketplace.Resolver
}

func (r transientOrganizationMarketplaceResolver) Resolve(ctx context.Context, cfg *config.Config, targets []hcc.Target, mode resolver.MarketplaceResolveMode) ([]resolver.ResolvedArtifact, []error) {
	return r.ResolveWithPolicy(ctx, cfg, targets, mode, nil)
}

func (r transientOrganizationMarketplaceResolver) ResolveWithPolicy(ctx context.Context, cfg *config.Config, targets []hcc.Target, mode resolver.MarketplaceResolveMode, policy resolver.MarketplaceRevocationPolicy) ([]resolver.ResolvedArtifact, []error) {
	if cfg == nil {
		return nil, nil
	}
	var artifacts []resolver.ResolvedArtifact
	var errs []error
	for _, entry := range cfg.Marketplaces {
		entryCfg := *cfg
		entryCfg.Marketplaces = []config.MarketplaceEntry{entry}
		entryMode := mode
		if entry.Source == config.Organization {
			entryMode = resolver.MarketplaceResolveModeCachedNoFetch
		}
		resolved, resolveErrs := r.delegate.ResolveWithPolicy(ctx, &entryCfg, targets, entryMode, policy)
		artifacts = append(artifacts, resolved...)
		errs = append(errs, resolveErrs...)
	}
	return artifacts, errs
}

func transientMarketplaceReadAssertions(ec config.EffectiveConfig, lockPath string) ([]dependencytxn.ReadAssertion, error) {
	byRequested, err := configReadAssertions(ec, false)
	if err != nil {
		return nil, err
	}
	expected, err := dependencytxn.ReadGeneration(lockPath)
	if err != nil {
		return nil, err
	}
	if expected.Exists {
		return nil, fmt.Errorf("deployed organization Marketplace lock %q must remain absent", lockPath)
	}
	byRequested[lockPath] = dependencytxn.ReadAssertion{
		Path:          lockPath,
		RequestedPath: lockPath,
		Expected:      expected,
	}
	reads := make([]dependencytxn.ReadAssertion, 0, len(byRequested))
	for _, read := range byRequested {
		reads = append(reads, read)
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i].RequestedPath < reads[j].RequestedPath })
	return reads, nil
}

func effectiveConfigError(ec config.EffectiveConfig, err error) config.EffectiveConfig {
	ec.Errors = []error{err}
	return ec
}

func marketplaceLockResourceCount(doc lockfile.Document) int {
	total := 0
	for _, entry := range doc.Marketplaces {
		total += len(entry.Resources)
	}
	return total
}
