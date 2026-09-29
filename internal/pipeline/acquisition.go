package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

var readIntent = config.ReadIntent

func NeedsLockBootstrap(errs []error) bool {
	for _, err := range errs {
		var validation *config.V4ValidationError
		if errors.As(err, &validation) && (validation.Code == "v4.marketplace-lock-required" || validation.Code == "v4.marketplace-lock-mismatch") {
			return true
		}
		var lifecycle *LockViewError
		if errors.As(err, &lifecycle) && lifecycle.Code == "lock.missing" {
			return true
		}
	}
	return false
}

func ReadLayerLock(path string, scope lockfile.Scope) (*lockfile.Document, dependencytxn.Generation, bool, error) {
	data, err := lockfile.ReadBytes(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, dependencytxn.Generation{}, false, nil
	}
	if errors.Is(err, lockfile.ErrUnsafeFile) {
		return nil, dependencytxn.Generation{}, false, fmt.Errorf("%w: %w", dependencytxn.ErrUnsafePath, err)
	}
	if err != nil {
		return nil, dependencytxn.Generation{}, false, err
	}
	generation := dependencytxn.Generation{Exists: true, Digest: dependencytxn.Digest(data)}
	doc, err := decodeLayerLock(data, path, scope)
	if err != nil {
		return nil, generation, true, err
	}
	return doc, generation, true, nil
}

func decodeLayerLock(data []byte, path string, scope lockfile.Scope) (*lockfile.Document, error) {
	doc, err := lockfile.Decode(data)
	if err == nil {
		err = lockfile.ValidateScope(doc, scope)
	}
	if err != nil {
		return nil, &LockViewError{Code: "lock.invalid", Scope: scope, Path: path, Err: err}
	}
	if doc.APIVersion == lockfile.APIVersionV1 {
		slog.Warn("lock file uses the deprecated v1 schema; the next writable reconciliation upgrades it to v2",
			slog.String("code", "lock.version-deprecated"),
			slog.String("path", path),
			slog.String("scope", string(scope)),
		)
	}
	return &doc, nil
}

func readLayerLockWithOverlays(paths config.ConfigLockPaths, scope lockfile.Scope, overlays map[string][]byte) (*lockfile.Document, dependencytxn.Generation, bool, bool, error) {
	for _, path := range []string{paths.CanonicalLock, paths.RequestedLock} {
		data, ok := overlays[path]
		if !ok {
			continue
		}
		doc, err := decodeLayerLock(data, paths.CanonicalLock, scope)
		if err != nil {
			return nil, dependencytxn.Generation{}, true, true, err
		}
		generation := dependencytxn.Generation{Exists: true, Digest: dependencytxn.Digest(data)}
		return doc, generation, true, true, nil
	}
	doc, generation, exists, err := ReadLayerLock(paths.CanonicalLock, scope)
	return doc, generation, exists, false, err
}

var deployedLockOrder = []config.Source{config.Organization, config.UserPersonal, config.RepoCommitted, config.RepoPersonal}

func ReadDeployedLocks(ctx context.Context, intent config.Intent) (config.Evidence, error) {
	return readDeployedLocks(ctx, intent, nil)
}

func readDeployedLocks(ctx context.Context, intent config.Intent, overlays map[string][]byte) (config.Evidence, error) {
	evidence := config.Evidence{}
	var missing error
	for _, source := range deployedLockOrder {
		if err := ctx.Err(); err != nil {
			return evidence, err
		}
		layer := intent.Layers[source]
		if !layer.Present() || layer.Err() != nil {
			continue
		}
		if source == config.Organization {
			if err := config.ValidateOrganizationTrustUnit(layer.Path, true, true); err != nil {
				return evidence, err
			}
		}
		if !layer.NeedsLock() {
			continue
		}
		scope := lockfile.Scope(source.LockScope())
		paths, err := config.ResolveConfigLockPaths(layer.Path)
		if err != nil {
			var pathErr *lockfile.Error
			invalidPath := errors.As(err, &pathErr) && pathErr.Code == lockfile.CodeInvalidPath
			if !errors.Is(err, os.ErrNotExist) && !invalidPath {
				return evidence, err
			}
			requested := source.LockPath(layer.Path)
			canonical, canonicalErr := dependencytxn.CanonicalConfigDestination(requested)
			if canonicalErr != nil {
				return evidence, canonicalErr
			}
			paths = config.ConfigLockPaths{RequestedLock: requested, CanonicalLock: canonical}
		}
		doc, generation, exists, overlay, err := readLayerLockWithOverlays(paths, scope, overlays)
		if err != nil {
			return evidence, err
		}
		if !exists {
			if source != config.Organization && missing == nil {
				missing = &LockViewError{Code: "lock.missing", Scope: scope, Path: paths.CanonicalLock, Err: fmt.Errorf("required %s layer lock %q is missing: %w", source, paths.CanonicalLock, os.ErrNotExist)}
			}
			continue
		}
		if source == config.Organization && !readOnlyGitDocumentMatches(*doc, GitRegistryEntries(layer.Sources())) {
			return evidence, &LockViewError{Code: "lock.stale", Scope: scope, Path: paths.CanonicalLock, Err: errors.New("lock does not match configuration")}
		}
		evidence.RecordDeployedLock(source, doc, config.ConfigRead{
			Path:            paths.CanonicalLock,
			RequestedPath:   paths.RequestedLock,
			Digest:          generation.Digest,
			Overlay:         overlay,
			AcquisitionLock: true,
		})
	}
	return evidence, missing
}

func isMissingLockError(err error) bool {
	var lockErr *LockViewError
	return errors.As(err, &lockErr) && lockErr.Code == "lock.missing"
}

func MarketplaceAllowlistError(intent config.Intent) error {
	var entries []config.MarketplaceEntry
	for _, source := range deployedLockOrder {
		layer := intent.Layers[source]
		if !layer.Present() || layer.Err() != nil {
			continue
		}
		for _, url := range layer.MarketplaceEndpoints() {
			entries = append(entries, config.MarketplaceEntry{URL: url})
		}
	}
	return config.ValidateMarketplaceURLs(entries, intent.AllowedMarketplaces())
}

func GitRegistryEntries(decls []config.SourceDecl) []config.RegistryEntry {
	var entries []config.RegistryEntry
	for _, decl := range decls {
		if decl.Kind != config.V4SourceGit {
			continue
		}
		entries = append(entries, config.RegistryEntry{URL: decl.URL, Ref: decl.Ref, Path: decl.Path, SourceHarness: decl.SourceHarness, V4Kind: decl.Kind})
	}
	return entries
}

func intentLayerResults(intent config.Intent) []config.LayerResult {
	layers := make([]config.LayerResult, 0, len(intent.Layers))
	for _, source := range deployedLockOrder {
		layer := intent.Layers[source]
		layers = append(layers, config.LayerResult{Source: source, Path: layer.Path, Reads: layer.Reads})
	}
	return layers
}

func sortedReadAssertions(byRequested map[string]dependencytxn.ReadAssertion) []dependencytxn.ReadAssertion {
	reads := make([]dependencytxn.ReadAssertion, 0, len(byRequested))
	for _, read := range byRequested {
		reads = append(reads, read)
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i].RequestedPath < reads[j].RequestedPath })
	return reads
}

func IntentReadAssertions(intent config.Intent) ([]dependencytxn.ReadAssertion, error) {
	byRequested, err := configReadAssertions(intentLayerResults(intent), false)
	if err != nil {
		return nil, err
	}
	return sortedReadAssertions(byRequested), nil
}

func transientOrganizationMarketplaceInputs(intent config.Intent) string {
	targets, _ := intent.TargetHarnesses()
	allowed, marketplaces := intent.AllowedRegistries(), intent.AllowedMarketplaces()
	return fmt.Sprintf("harnesses=%q allowed-nil=%t allowed=%q allowed-marketplaces-nil=%t allowed-marketplaces=%q", targets, allowed == nil, allowed, marketplaces == nil, marketplaces)
}

func transientMarketplaceReadAssertions(intent config.Intent, lockPath string) ([]dependencytxn.ReadAssertion, error) {
	organization := intent.Layers[config.Organization]
	byRequested, err := configReadAssertions([]config.LayerResult{{Source: config.Organization, Path: organization.Path, Reads: organization.Reads}}, false)
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
	return sortedReadAssertions(byRequested), nil
}

type transientOrganizationMarketplaceLockCacheKey struct{}

type transientOrganizationMarketplaceLockCache struct {
	mu    sync.Mutex
	path  string
	entry cachedTransientOrganizationMarketplaceLock
}

type cachedTransientOrganizationMarketplaceLock struct {
	doc    lockfile.Document
	reads  []dependencytxn.ReadAssertion
	inputs string
}

type transientOrganizationMarketplaceLockUse uint8

const (
	transientOrganizationMarketplaceLockUnused transientOrganizationMarketplaceLockUse = iota
	transientOrganizationMarketplaceLockAcquired
	transientOrganizationMarketplaceLockReused
)

func WithTransientOrganizationMarketplaceLockCache(ctx context.Context) context.Context {
	if transientOrganizationMarketplaceLockCacheFrom(ctx) != nil {
		return ctx
	}
	cache := &transientOrganizationMarketplaceLockCache{}
	return context.WithValue(ctx, transientOrganizationMarketplaceLockCacheKey{}, cache)
}

func transientOrganizationMarketplaceLockCacheFrom(ctx context.Context) *transientOrganizationMarketplaceLockCache {
	if ctx == nil {
		return nil
	}
	cache, _ := ctx.Value(transientOrganizationMarketplaceLockCacheKey{}).(*transientOrganizationMarketplaceLockCache)
	return cache
}

func cloneLockDocument(doc lockfile.Document) (lockfile.Document, error) {
	data, err := lockfile.Encode(doc)
	if err != nil {
		return lockfile.Document{}, err
	}
	return lockfile.Decode(data)
}

func (c *transientOrganizationMarketplaceLockCache) load(path string) (lockfile.Document, []dependencytxn.ReadAssertion, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path != path {
		return lockfile.Document{}, nil, "", false
	}
	doc, err := cloneLockDocument(c.entry.doc)
	if err != nil {
		return lockfile.Document{}, nil, "", false
	}
	return doc, append([]dependencytxn.ReadAssertion(nil), c.entry.reads...), c.entry.inputs, true
}

func (c *transientOrganizationMarketplaceLockCache) store(path string, doc lockfile.Document, reads []dependencytxn.ReadAssertion, inputs string) {
	cloned, err := cloneLockDocument(doc)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.path = path
	c.entry = cachedTransientOrganizationMarketplaceLock{
		doc:    cloned,
		reads:  append([]dependencytxn.ReadAssertion(nil), reads...),
		inputs: inputs,
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
