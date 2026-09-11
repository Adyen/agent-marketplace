package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Adyen/agent-marketplace/internal/backup"
	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// syncPhaseHookContextKey is an internal deterministic concurrency-test seam.
// Production callers never install it.
type syncPhaseHookContextKey struct{}

type Loaders struct {
	Org    func() (*config.Config, error)
	Global func() (*config.Config, error)
	Repo   func() (*config.Config, error)
	Local  func() (*config.LocalConfig, error)
}

type ResolveOptions struct {
	RepoRoot         string
	MaterializeRoot  string // where to materialize harness files; defaults to RepoRoot if empty
	LocalConfigDir   string // base dir for resolving local-config entries' relative paths; defaults to RepoRoot
	CatalogMode      catalog.LoadMode
	Offline          bool
	Loaders          Loaders
	CatalogOption    []catalog.LoadOption
	Gitignore        bool
	NoBackup         bool
	RequireTargetSet bool
	// EffectiveOpts overrides the paths used by config.LoadEffective.
	EffectiveOpts config.LoadEffectiveOptions
	// IgnoreBlockedRegistries treats RegistryURLBlockedError from the allowlist
	// validation as non-fatal. Used by the remove path so a previously-allowed
	// registry can still be removed after the policy is tightened.
	IgnoreBlockedRegistries bool
	// SkipInventory suppresses recording for internal pre-mutation lookups. The
	// post-mutation Resolve performed by sync remains the reported generation.
	SkipInventory      bool
	ReadOnlyExactLocks bool
	// AllowMissingReadOnlyLocks lets read-only inspection fall back to the
	// mutable registry cache for layers that have no lockfile. Present locks
	// remain exact and malformed or stale locks still fail closed.
	AllowMissingReadOnlyLocks   bool
	ExactLockLease              func(context.Context) (func(), error)
	MarketplaceRevocationPolicy resolver.MarketplaceRevocationPolicy
	// MarketplaceMode overrides the mode derived from CatalogMode when set,
	// except that Offline always forces CachedNoFetch.
	MarketplaceMode    *resolver.MarketplaceResolveMode
	marketplaceInstall *marketplace.PreparedInstall
	// DisableTransientOrganizationMarketplaceLock preserves strictly
	// read-only/check-only command semantics when an organization lock is
	// absent. Normal online consumer paths acquire a verified in-memory lock.
	DisableTransientOrganizationMarketplaceLock bool
	// MarketplaceSourceFilter limits Marketplace resolution to entries owned by
	// the selected sources. It is applied only to the resolver's view and does
	// not change the effective configuration returned to callers.
	MarketplaceSourceFilter func(config.Source) bool
}

type ResolveResult struct {
	OrgCfg                *config.Config
	GlobalCfg             *config.Config
	RepoCfg               *config.Config
	LocalCfg              *config.LocalConfig
	MergedCfg             *config.Config
	LoadedCfg             *config.Config
	MaterializationModes  config.MaterializationModes
	CatalogLoad           catalog.LoadResult
	Resolved              []resolver.ResolvedArtifact
	LockMetadata          []LockMetadata
	MarketplaceStatuses   []marketplace.PinStatus
	MarketplaceInspection *marketplace.Inspection
	ConfigErrors          []error
	CatalogErrors         []error
	ResolveErrors         []error
	Errors                []error
	Warnings              []error
	TargetHarnesses       []hcc.Target
	TargetHarnessSource   config.Source
	MalformedPruneFences  []materialize.PruneFence
	Managed               bool
	Diagnostics           config.ManagedInstallationDiagnostics
}

// TargetHarnessSetMissingError is returned by Resolve/Sync when no effective
// target harness set is configured.
type TargetHarnessSetMissingError struct{}

func (e *TargetHarnessSetMissingError) Error() string {
	return "no machine-wide target harness set configured; add harnesses to ~/.agents/config.yaml"
}

// Remediation returns a user-facing hint about how to configure the target set.
func (e *TargetHarnessSetMissingError) Remediation() string {
	return "add 'harnesses: [factory, claude, ...]' to the project or user config"
}

type SyncResult struct {
	ResolveResult
	Report *materialize.Report

	// CatalogTrustErrors are the hard failures raised BEFORE materialization:
	// config load/validation, catalog load, and resolve. They are the errors
	// that make this run's view of the registries unreliable — while any of
	// them is present, "selected but not found" may mean "unreachable", not
	// "removed upstream".
	//
	// MaterializeErrors are the hard failures raised AFTER the catalog was read
	// and the selections resolved: backup failures, materialize diagnostics
	// promoted to errors, and the materialize.Run error itself. They say
	// nothing about whether the catalog was trustworthy.
	//
	// Both are disjoint subsets of Errors, which stays the single flat list
	// callers join for the exit code. The split exists because bead
	// agent-marketplace-tbjf found `--prune-stale` gated on len(Errors) == 0:
	// a compile failure in one unrelated resource silently disabled pruning of
	// selections that were verifiably gone. Callers that need the distinction
	// must read these fields rather than string-match or type-sniff Errors.
	CatalogTrustErrors []error
	MaterializeErrors  []error
}

// hccTargetsFromStrings converts a []string harness list into []hcc.Target.
func hccTargetsFromStrings(targets []string) []hcc.Target {
	if len(targets) == 0 {
		return nil
	}
	out := make([]hcc.Target, len(targets))
	for i, t := range targets {
		out[i] = hcc.Target(t)
	}
	return out
}

// targetHarnessSetFromEC returns the effective target harness set and its
// source from an EffectiveConfig. Project-level harnesses win, followed by
// user-personal and organization defaults.
func targetHarnessSetFromEC(ec config.EffectiveConfig) ([]hcc.Target, config.Source) {
	if len(ec.TargetHarnesses) > 0 {
		return hccTargetsFromStrings(ec.TargetHarnesses), ec.TargetHarnessSource
	}
	if ec.Repo.Config != nil && len(ec.Repo.Config.Harnesses) > 0 {
		return hccTargetsFromStrings(ec.Repo.Config.Harnesses), config.RepoCommitted
	}
	if ec.User.Config != nil && len(ec.User.Config.Harnesses) > 0 {
		return hccTargetsFromStrings(ec.User.Config.Harnesses), config.UserPersonal
	}
	if ec.Organization.Config != nil && len(ec.Organization.Config.Harnesses) > 0 {
		return hccTargetsFromStrings(ec.Organization.Config.Harnesses), ec.Organization.Source
	}
	return nil, config.SourceUnset
}

// CatalogTrusted reports whether this run's view of the registries is reliable
// enough to conclude that a selected-but-absent resource is genuinely gone
// upstream rather than merely unreachable. It is the gate `sync --prune-stale`
// consults before removing anything from a user's config.
func (r SyncResult) CatalogTrusted() bool { return len(r.CatalogTrustErrors) == 0 }

// addMaterializeError records a post-resolve failure in both MaterializeErrors
// (the category) and Errors (the flat list that drives the exit code). Every
// append after the catalog has been read goes through here so the two cannot
// drift apart, which is what would silently re-introduce the tbjf bug.
func (r *SyncResult) addMaterializeError(errs ...error) {
	r.MaterializeErrors = append(r.MaterializeErrors, errs...)
	r.Errors = append(r.Errors, errs...)
}

// malformedPruneFences translates soft malformed selections into narrow
// target/scope/kind fences for materialization.
func malformedPruneFences(errs []error) []materialize.PruneFence {
	seen := make(map[string]struct{})
	var fences []materialize.PruneFence
	for _, err := range errs {
		if !resolver.IsSoftSelectedMalformed(err) {
			continue
		}
		var malformed *resolver.SelectedMalformedError
		if !errors.As(err, &malformed) {
			continue
		}
		for _, partition := range resolver.MalformedSelectionPartitions(err) {
			fence := materialize.PruneFence{Target: partition.Target, MachineScope: partition.MachineScope, Kind: malformed.Kind}
			key := fmt.Sprintf("%s|%t|%s", fence.Target, fence.MachineScope, fence.Kind)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			fences = append(fences, fence)
		}
	}
	return fences
}

// SoftWarnings derives the advisory warnings implied by a ResolveResult's
// catalog and resolve errors: overlaps, name collisions, soft
// selected-not-found, soft malformed selections, collection-member ambiguity,
// and the other resolver warnings.
//
// Resolve deliberately leaves these in CatalogErrors/ResolveErrors rather than
// promoting them, because whether an unresolved selection is advisory or fatal
// is the CALLER's policy (`sync --strict` makes some of them fatal). This
// function is the one place that classification lives, so `sync`, `sync
// --check` and any future caller cannot drift apart: `sync --check` used to
// have no equivalent at all and silently showed none of them.
//
// The returned slice excludes ResolveResult.Warnings, which the caller already
// has; append it to that list rather than using it alone.
func SoftWarnings(resolved ResolveResult) []error {
	var warnings []error
	// Surface catalog overlap warnings as soft warnings for parity with the
	// status command's WARNINGS section.
	for _, catErr := range resolved.CatalogErrors {
		if catalog.IsOverlapWarning(catErr) {
			warnings = append(warnings, catErr)
		}
	}
	for _, resolveErr := range resolved.ResolveErrors {
		if resolver.IsNameCollision(resolveErr) || resolver.IsSoftSelectedNotFound(resolveErr) || resolver.IsWarning(resolveErr) {
			warnings = append(warnings, resolveErr)
		}
		// A path-less collection member that several configured locations of
		// the same repository provide is reported, not fatal: the resolver
		// still binds one deterministically.
		if resolver.IsCollectionMemberAmbiguity(resolveErr) {
			warnings = append(warnings, resolveErr)
		}
		// A present-but-malformed selection is reported at the same severity as
		// a stale one so an unfixable registry-side syntax error does not fail
		// every consumer's sync — but with the opposite remediation.
		if resolver.IsSoftSelectedMalformed(resolveErr) {
			warnings = append(warnings, resolveErr)
		}
	}
	return warnings
}

func DefaultLoaders() Loaders {
	return Loaders{Org: config.LoadOrganizationConfig, Global: config.LoadGlobalConfig, Repo: config.LoadRepoConfig, Local: config.LoadLocalConfig}
}

// filterBlockedRegistryErrors removes RegistryURLBlockedError values from errs,
// logging each ignored error as a warning. It returns the remaining errors.
func filterBlockedRegistryErrors(errs []error) ([]error, bool) {
	filtered := make([]error, 0, len(errs))
	ignored := false
	for _, err := range errs {
		if config.IsRegistryURLBlockedError(err) {
			slog.Warn("ignoring blocked registry error during remove", "error", err)
			ignored = true
			continue
		}
		filtered = append(filtered, err)
	}
	return filtered, ignored
}

// ResolveWithExactLockLease keeps the single read-only exact-cache lease used
// by resolution alive until release is called. It is intended for callers that
// consume resolved snapshot paths after Resolve returns.
func ResolveWithExactLockLease(ctx context.Context, opts ResolveOptions) (ResolveResult, func()) {
	return resolve(ctx, opts)
}

func Resolve(ctx context.Context, opts ResolveOptions) ResolveResult {
	result, release := resolve(ctx, opts)
	release()
	return result
}

func resolve(ctx context.Context, opts ResolveOptions) (ResolveResult, func()) {
	releaseExactLease := func() {}
	start := time.Now()
	if opts.Offline {
		opts.CatalogMode = catalog.LoadModeInspect
	}
	slog.Debug("pipeline resolve start", "repo_root", opts.RepoRoot, "catalog_mode", opts.CatalogMode, "offline", opts.Offline)
	var result ResolveResult
	// Every early return below must still hand back a valid policy: an unset
	// mode matches neither "symlink" nor "file" and would silently pick a
	// branch in materialize.Run for callers that materialize before checking
	// Errors.
	result.MaterializationModes = config.DefaultMaterializationModes()

	tel := telemetry.TelemetryFromContext(ctx)
	var resolveSpan telemetry.Span
	if tel != nil {
		ctx, resolveSpan = tel.StartSpan(ctx, "agent_marketplace.pipeline.resolve", nil)
	}
	defer func() {
		registries := 0
		if result.MergedCfg != nil {
			registries = len(result.MergedCfg.Registries)
		}
		slog.Debug("pipeline resolve end",
			"duration_ms", time.Since(start).Milliseconds(),
			"registries", registries,
			"resolved", len(result.Resolved),
			"config_errors", len(result.ConfigErrors),
			"catalog_errors", len(result.CatalogErrors),
			"resolve_errors", len(result.ResolveErrors),
			"errors", len(result.Errors),
		)
		if tel != nil && !opts.SkipInventory {
			recordResolvedInventory(ctx, result)
		}
		if resolveSpan != (telemetry.Span{}) {
			resolveSpan.SetAttributes(resolveResultAttrs(result, time.Since(start)))
			resolveSpan.End()
		}
	}()

	if ctx.Err() != nil {
		result.Errors = []error{ctx.Err()}
		return result, releaseExactLease
	}
	if opts.ExactLockLease != nil {
		release, err := opts.ExactLockLease(ctx)
		if err != nil {
			result.Errors = []error{fmt.Errorf("acquire and revalidate exact locks before resolution: %w", err)}
			return result, releaseExactLease
		}
		if release != nil {
			releaseExactLease = release
		}
		ctx = cache.ContextWithExactGitNamespaceLease(ctx)
	}
	var ec config.EffectiveConfig
	var transientOrganizationMarketplaceLock transientOrganizationMarketplaceLockUse

	var configLoadSpan telemetry.Span
	if tel != nil {
		_, configLoadSpan = tel.StartSpan(ctx, "agent_marketplace.pipeline.config_load", nil)
	}
	defer func() {
		if configLoadSpan != (telemetry.Span{}) {
			configLoadSpan.SetAttributes(configLoadAttrs(ec))
			configLoadSpan.End()
		}
	}()

	effectiveOpts := opts.EffectiveOpts
	if effectiveOpts.RepoRoot == "" {
		effectiveOpts.RepoRoot = opts.RepoRoot
	}
	if effectiveOpts.LocalConfigDir == "" {
		effectiveOpts.LocalConfigDir = opts.LocalConfigDir
	}
	allowTransientOrganizationLock := !opts.DisableTransientOrganizationMarketplaceLock && !opts.Offline && opts.CatalogMode != catalog.LoadModeCachedNoFetch
	useLoaders := opts.Loaders.Org != nil || opts.Loaders.Global != nil || opts.Loaders.Repo != nil || opts.Loaders.Local != nil
	if useLoaders {
		loaders := opts.Loaders
		if loaders.Org == nil {
			loaders.Org = config.LoadOrganizationConfig
		}
		if loaders.Global == nil {
			loaders.Global = config.LoadGlobalConfig
		}
		if loaders.Repo == nil {
			loaders.Repo = config.LoadRepoConfig
		}
		if loaders.Local == nil {
			loaders.Local = config.LoadLocalConfig
		}
		var errs []error
		orgCfg, err := loaders.Org()
		if err != nil {
			errs = append(errs, err)
		}
		globalCfg, err := loaders.Global()
		if err != nil {
			errs = append(errs, err)
		}
		repoCfg, err := loaders.Repo()
		if err != nil {
			errs = append(errs, err)
		}
		localCfg, err := loaders.Local()
		if err != nil {
			errs = append(errs, err)
		}
		ec = config.EffectiveConfig{
			Organization: config.LayerResult{Source: config.Organization, Path: opts.EffectiveOpts.OrganizationPath, Config: orgCfg},
			User:         config.LayerResult{Source: config.UserPersonal, Path: opts.EffectiveOpts.GlobalPath, Config: globalCfg},
			Repo:         config.LayerResult{Source: config.RepoCommitted, Path: opts.EffectiveOpts.RepoPath, Config: repoCfg},
			LocalLayer:   config.LayerResult{Source: config.RepoPersonal, Path: opts.EffectiveOpts.LocalPath, Local: localCfg},
			Local:        localCfg,
			Errors:       errs,
		}
		if allowTransientOrganizationLock && marketplacelock.HasRequiredError(ec.Errors) {
			ec, transientOrganizationMarketplaceLock = loadEffectiveWithTransientOrganizationLock(ctx, effectiveOpts, opts.MarketplaceRevocationPolicy)
		}
	} else {
		if allowTransientOrganizationLock {
			ec, transientOrganizationMarketplaceLock = loadEffectiveWithTransientOrganizationLock(ctx, effectiveOpts, opts.MarketplaceRevocationPolicy)
		} else {
			ec = config.LoadEffective(ctx, effectiveOpts)
		}
	}

	result.Diagnostics = ec.Diagnostics
	result.Warnings = ec.Warnings
	result.OrgCfg = ec.Organization.Config
	result.GlobalCfg = ec.User.Config
	result.RepoCfg = ec.Repo.Config
	result.LocalCfg = ec.Local
	result.Managed = ec.Managed

	config.LogManagedDiagnostics(ec.Diagnostics)

	var ignoredBlocked bool
	if opts.IgnoreBlockedRegistries {
		ec.Errors, ignoredBlocked = filterBlockedRegistryErrors(ec.Errors)
	}

	for _, err := range ec.Errors {
		slog.Error("effective config error", "error", err)
	}

	if opts.Gitignore {
		gitignoreDir := opts.LocalConfigDir
		if gitignoreDir == "" {
			gitignoreDir = opts.RepoRoot
		}
		EnsureLocalConfigGitignored(gitignoreDir)
	}
	if len(ec.Errors) > 0 {
		result.ConfigErrors = ec.Errors
		result.Errors = ec.Errors
		return result, releaseExactLease
	}

	result.TargetHarnesses, result.TargetHarnessSource = targetHarnessSetFromEC(ec)

	if opts.RequireTargetSet && len(result.TargetHarnesses) == 0 {
		result.Errors = []error{&TargetHarnessSetMissingError{}}
		return result, releaseExactLease
	}

	var mergedCfg *config.Config
	if ec.Merged != nil {
		// config.LoadEffective already merged the layers and validated the
		// registry URLs against the resolved allowlist.
		mergedCfg = ec.Merged
	} else {
		// Custom loaders bypass config.LoadEffective, so we still need to
		// merge the layers and enforce the allowlist here.
		layers := make([]config.Layer, 0, 4)
		if result.OrgCfg != nil {
			src := ec.Organization.Source
			if src == config.SourceUnset {
				src = config.Organization
			}
			layers = append(layers, config.Layer{Source: src, Config: result.OrgCfg})
		}
		if result.GlobalCfg != nil {
			layers = append(layers, config.Layer{Source: config.UserPersonal, Config: result.GlobalCfg})
		}
		if result.RepoCfg != nil {
			layers = append(layers, config.Layer{Source: config.RepoCommitted, Config: result.RepoCfg})
		}
		if l := config.LocalConfigAsLayer(result.LocalCfg); l != nil {
			layers = append(layers, *l)
		}
		mergedCfg = config.MergeLayers(layers)

		// Enforce registry URL allowlist before any network fetch. The allowlist
		// is resolved from the effective configuration layers; omission permits all
		// remote origins and an explicit empty list denies all remote origins.
		var effectiveAllowlist []string
		if mergedCfg.AllowedRegistries != nil {
			effectiveAllowlist = *mergedCfg.AllowedRegistries
		}
		if err := config.ValidateRegistryURLs(mergedCfg.Registries, effectiveAllowlist); err != nil {
			ec.Errors = append(ec.Errors, err)
			if opts.IgnoreBlockedRegistries {
				ec.Errors, ignoredBlocked = filterBlockedRegistryErrors(ec.Errors)
			}
			if len(ec.Errors) > 0 {
				result.ConfigErrors = ec.Errors
				result.Errors = ec.Errors
				return result, releaseExactLease
			}
		}
	}
	result.MergedCfg = mergedCfg
	if ignoredBlocked {
		opts.Offline = true
		opts.CatalogMode = catalog.LoadModeInspect
		slog.Debug("blocked-registry removal resolves cached inputs without fetching")
	}
	if opts.Offline && mergedCfg != nil && len(mergedCfg.Marketplaces) > 0 {
		result.Warnings = append(result.Warnings, errors.New("offline Marketplace sync cannot observe newer revocations; cached revocation metadata is used"))
	}
	materializationModes, modeErr := config.ResolveMaterializationModes(result.GlobalCfg, result.RepoCfg, result.LocalCfg)
	if modeErr != nil {
		result.ConfigErrors = append(result.ConfigErrors, modeErr)
		result.Errors = append(result.Errors, modeErr)
		return result, releaseExactLease
	}
	result.MaterializationModes = materializationModes
	registryCount := 0
	if mergedCfg != nil {
		registryCount = len(mergedCfg.Registries)
	}
	slog.Debug("config layers merged",
		"org", result.OrgCfg != nil,
		"user", result.GlobalCfg != nil,
		"repo", result.RepoCfg != nil,
		"local", result.LocalCfg != nil,
		"registries", registryCount,
	)

	var selectionSpan telemetry.Span
	if tel != nil {
		ctx, selectionSpan = tel.StartSpan(ctx, "agent_marketplace.pipeline.selection", nil)
	}

	catalogOpts := opts.CatalogOption
	var readOnlyView readOnlyExactView
	if opts.ReadOnlyExactLocks {
		ctx, readOnlyView = exactLockViewWithContext(ctx, ec, opts.AllowMissingReadOnlyLocks)
		previousRelease := releaseExactLease
		releaseExactLease = func() {
			readOnlyView.release()
			previousRelease()
		}
		catalogOpts = append(catalogOpts, readOnlyView.catalogOption)
		result.LockMetadata = readOnlyView.gitMetadata
		result.Warnings = append(result.Warnings, readOnlyView.warnings...)
	}
	if opts.LocalConfigDir != "" && opts.LocalConfigDir != opts.RepoRoot {
		catalogOpts = append(catalogOpts, catalog.WithLocalConfigDir(opts.LocalConfigDir))
	}
	loadResult := catalog.LoadRegistriesWithContext(ctx, mergedCfg, opts.RepoRoot, opts.CatalogMode, catalogOpts...)
	result.CatalogLoad = loadResult
	result.CatalogErrors = loadResult.Errors
	slog.Debug("catalogs loaded", "catalogs", len(loadResult.Catalogs), "errors", len(loadResult.Errors))
	// Overlap warnings are soft; don't add them to the hard error list.
	var errs []error
	for _, err := range loadResult.Errors {
		if !catalog.IsOverlapWarning(err) {
			errs = append(errs, err)
		}
	}

	loadedCfg := loadResult.Config

	// Malformed resources are dropped from Entries by the ingesters, so the
	// resolver needs them separately to tell "failed validation" apart from
	// "removed upstream" for a selection it cannot find.
	mpMode := marketplaceResolveMode(opts.CatalogMode, opts.Offline)
	if opts.MarketplaceMode != nil && !opts.Offline {
		mpMode = *opts.MarketplaceMode
	}
	if opts.Offline {
		if opts.MarketplaceMode != nil && mpMode != resolver.MarketplaceResolveModeCachedNoFetch {
			slog.Debug("offline Marketplace resolution overrides explicit mode", "requested_mode", mpMode, "effective_mode", resolver.MarketplaceResolveModeCachedNoFetch)
		}
		mpMode = resolver.MarketplaceResolveModeCachedNoFetch
	}
	marketplaceResolver := marketplace.NewResolver()
	// Keep the effective registry origin policy on the Marketplace resolver as
	// well as on the loaded config. PrepareLocator uses the resolver directly,
	// while normal sync passes the loaded config through the generic resolver.
	if mergedCfg != nil && mergedCfg.AllowedRegistries != nil {
		marketplaceResolver.AllowedRegistries = *mergedCfg.AllowedRegistries
	}
	var mpResolver resolver.MarketplaceResolver = marketplaceResolver
	if transientOrganizationMarketplaceLock == transientOrganizationMarketplaceLockAcquired && mpMode != resolver.MarketplaceResolveModeInspect {
		mpResolver = transientOrganizationMarketplaceResolver{delegate: marketplaceResolver}
	}
	if opts.CatalogMode == catalog.LoadModeInspect && mpMode == resolver.MarketplaceResolveModeInspect && loadedCfg != nil {
		inspection := marketplaceResolver.Inspect(ctx, loadedCfg, result.TargetHarnesses, mpMode)
		result.MarketplaceStatuses = inspection.Pins
		result.MarketplaceInspection = &inspection
		mpResolver = inspection
	}
	marketplaceCfg := loadedCfg
	if loadedCfg != nil && opts.MarketplaceSourceFilter != nil {
		filtered := *loadedCfg
		filtered.Marketplaces = make([]config.MarketplaceEntry, 0, len(loadedCfg.Marketplaces))
		for _, entry := range loadedCfg.Marketplaces {
			if opts.MarketplaceSourceFilter(entry.Source) {
				filtered.Marketplaces = append(filtered.Marketplaces, entry)
			}
		}
		marketplaceCfg = &filtered
	}
	resolved, resolveErrs := resolver.Resolve(
		marketplaceCfg,
		catalog.EntriesByKey(loadResult.Catalogs),
		result.LocalCfg,
		resolver.WithResources(loadResult.ResourceCatalogs),
		resolver.WithMalformed(catalog.MalformedByKey(loadResult.Catalogs)),
		resolver.WithContext(ctx),
		resolver.WithTargetHarnesses(result.TargetHarnesses),
		resolver.WithMarketplaceResolver(mpResolver),
		resolver.WithMarketplaceResolveMode(mpMode),
		resolver.WithMarketplaceRevocationPolicy(opts.MarketplaceRevocationPolicy),
	)
	result.LoadedCfg = loadedCfg
	result.Resolved = resolved
	result.ResolveErrors = append(resolveErrs, readOnlyView.errors...)
	result.MalformedPruneFences = malformedPruneFences(result.ResolveErrors)
	slog.Debug("resources resolved", "resolved", len(resolved), "errors", len(result.ResolveErrors), "malformed_prune_fences", len(result.MalformedPruneFences))
	for _, resolveErr := range result.ResolveErrors {
		if opts.Offline && (resolver.IsSoftSelectedNotFound(resolveErr) || resolver.IsSoftSelectedMalformed(resolveErr)) {
			errs = append(errs, resolveErr)
			continue
		}
		if resolver.IsWarning(resolveErr) {
			continue
		}
		if resolver.IsSoftSelectedNotFound(resolveErr) || resolver.IsSoftSelectedMalformed(resolveErr) {
			continue
		}
		errs = append(errs, resolveErr)
	}
	if selectionSpan != (telemetry.Span{}) {
		selectionSpan.SetAttributes(selectionAttrs(result))
		selectionSpan.End()
	}
	result.Errors = errs
	return result, releaseExactLease
}

func marketplaceResolveMode(mode catalog.LoadMode, offline bool) resolver.MarketplaceResolveMode {
	if offline {
		return resolver.MarketplaceResolveModeCachedNoFetch
	}
	switch mode {
	case catalog.LoadModeRefresh:
		return resolver.MarketplaceResolveModeRefresh
	case catalog.LoadModeInspect:
		return resolver.MarketplaceResolveModeInspect
	case catalog.LoadModeCachedNoFetch:
		return resolver.MarketplaceResolveModeCachedNoFetch
	default:
		return resolver.MarketplaceResolveModeCached
	}
}

func Sync(ctx context.Context, opts ResolveOptions) SyncResult {
	start := time.Now()
	if !opts.Offline {
		if opts.CatalogMode != catalog.LoadModeCached {
			opts.CatalogMode = catalog.LoadModeRefresh
		}
		opts.Gitignore = true
	}
	slog.Debug("pipeline sync start", "repo_root", opts.RepoRoot, "catalog_mode", opts.CatalogMode, "offline", opts.Offline)
	opts.RequireTargetSet = true

	tel := telemetry.TelemetryFromContext(ctx)
	var syncSpan telemetry.Span
	if tel != nil {
		ctx, syncSpan = tel.StartSpan(ctx, "agent_marketplace.pipeline.sync", nil)
	}
	var result SyncResult
	defer func() {
		written := 0
		removed := 0
		if result.Report != nil {
			for _, target := range result.Report.PerRoot {
				written += len(target.Written)
				removed += len(target.Removed)
			}
		}
		slog.Info("pipeline sync end",
			"duration_ms", time.Since(start).Milliseconds(),
			"resolved", len(result.Resolved),
			"written", written,
			"removed", removed,
			"warnings", len(result.Warnings),
			"errors", len(result.Errors),
			"catalog_trust_errors", len(result.CatalogTrustErrors),
			"materialize_errors", len(result.MaterializeErrors),
		)
		if syncSpan != (telemetry.Span{}) {
			syncSpan.SetAttributes(syncResultAttrs(result, time.Since(start)))
			syncSpan.End()
		}
	}()

	var releaseExactLocks func()
	if opts.ExactLockLease != nil {
		release, err := opts.ExactLockLease(ctx)
		if err != nil {
			wrapped := fmt.Errorf("acquire and revalidate exact locks before sync: %w", err)
			result = SyncResult{ResolveResult: ResolveResult{Errors: []error{wrapped}}, MaterializeErrors: []error{wrapped}}
			return result
		}
		releaseExactLocks = release
	} else if opts.ReadOnlyExactLocks {
		release, err := cache.AcquireExactGitNamespaceReadOnlyLeaseWithContext(ctx)
		if err != nil {
			wrapped := fmt.Errorf("acquire exact cache read lease before sync: %w", err)
			result = SyncResult{ResolveResult: ResolveResult{Errors: []error{wrapped}}, MaterializeErrors: []error{wrapped}}
			return result
		}
		releaseExactLocks = release
	}
	if releaseExactLocks != nil {
		defer releaseExactLocks()
		ctx = cache.ContextWithExactGitNamespaceLease(ctx)
	}

	resolveOpts := opts
	// Sync already holds and revalidated the caller-provided lease above; do not
	// acquire a second file description when delegating to resolution.
	resolveOpts.ExactLockLease = nil
	resolved := Resolve(ctx, resolveOpts)
	result.ResolveResult = resolved
	for _, warning := range SoftWarnings(resolved) {
		if opts.Offline && (resolver.IsSoftSelectedNotFound(warning) || resolver.IsSoftSelectedMalformed(warning)) {
			continue
		}
		result.Warnings = append(result.Warnings, warning)
	}

	// Compute the materialization root once and reuse it for both the
	// ancestor-exclude detection (below) and the actual materialize.Run
	// call further down. The warning root and the write root must always
	// agree, so deriving them from a single value avoids future drift.
	matRoot := opts.MaterializeRoot
	if matRoot == "" {
		matRoot = opts.RepoRoot
	}
	targets := resolved.TargetHarnesses
	result.Warnings = append(result.Warnings, detectAncestorMaterializedExcludes(
		matRoot,
		opts.RepoRoot,
		targets,
		skillExcludesFromLocal(resolved.LocalCfg),
		resolved.Resolved,
	)...)

	if len(resolved.Errors) > 0 {
		filteredErrors := make([]error, 0, len(resolved.Errors))
		for _, err := range resolved.Errors {
			if resolver.IsNameCollision(err) {
				continue
			}
			if !opts.Offline && resolver.IsSoftSelectedNotFound(err) {
				continue
			}
			if !opts.Offline && resolver.IsSoftSelectedMalformed(err) {
				continue
			}
			filteredErrors = append(filteredErrors, err)
		}
		result.Errors = filteredErrors
	}

	// Everything hard that is known at this point predates materialization, so
	// it is exactly the set that makes the catalog untrustworthy. Recording it
	// here — instead of re-deriving the config/catalog/resolve filters in a
	// second place — is what keeps the category honest as those filters change.
	// The copy keeps it independent of the appends below.
	result.CatalogTrustErrors = append([]error(nil), result.Errors...)

	if len(result.Errors) > 0 {
		return result
	}
	if opts.marketplaceInstall != nil {
		if err := checkResolvedMarketplaceInstallCollision(resolved.Resolved, opts.marketplaceInstall); err != nil {
			slog.Warn("Marketplace install refused after dependency resolution", "error", err)
			result.addMaterializeError(err)
			return result
		}
	}

	if !opts.NoBackup {
		backupCfg, err := resolved.MergedCfg.ToBackupConfig()
		if err != nil {
			result.addMaterializeError(err)
			return result
		}
		if backupCfg.Enabled {
			bCfg := backup.Config{
				Enabled:   backupCfg.Enabled,
				MaxDays:   backupCfg.MaxDays,
				MaxPerDay: backupCfg.MaxPerDay,
			}
			managed := CollectManagedPaths(resolved.Resolved)
			codexPartitions, codexErr := CollectCodexBackupPartitions(resolved.Resolved)
			if codexErr != nil {
				result.addMaterializeError(fmt.Errorf("backup: %w", codexErr))
				return result
			}
			if len(managed) > 0 || len(codexPartitions) > 0 {
				var backupSpan telemetry.Span
				if tel != nil {
					_, backupSpan = tel.StartSpan(ctx, "agent_marketplace.pipeline.backup", map[string]string{
						"target.count":          strconv.Itoa(len(managed)),
						"codex.partition.count": strconv.Itoa(len(codexPartitions)),
					})
				}
				slog.Debug("creating harness backups", "targets", len(managed), "codex_partitions", len(codexPartitions))
				if err := backup.Create(managed); err != nil {
					if backupSpan != (telemetry.Span{}) {
						backupSpan.SetAttributes(map[string]string{"error": "true"})
						backupSpan.End()
					}
					result.addMaterializeError(fmt.Errorf("backup: %w", err))
					return result
				}
				if len(codexPartitions) > 0 {
					if _, err := backup.CreateCodexSet(codexPartitions); err != nil {
						if backupSpan != (telemetry.Span{}) {
							backupSpan.SetAttributes(map[string]string{"error": "true"})
							backupSpan.End()
						}
						result.addMaterializeError(fmt.Errorf("backup: %w", err))
						return result
					}
				}
				slog.Debug("rotating harness backups", "max_days", bCfg.MaxDays, "max_per_day", bCfg.MaxPerDay)
				if err := backup.Rotate(bCfg); err != nil {
					if backupSpan != (telemetry.Span{}) {
						backupSpan.SetAttributes(map[string]string{"error": "true"})
						backupSpan.End()
					}
					result.addMaterializeError(fmt.Errorf("backup rotate: %w", err))
					return result
				}
				if backupSpan != (telemetry.Span{}) {
					backupSpan.End()
				}
			}
		}
	}

	if hook, ok := ctx.Value(syncPhaseHookContextKey{}).(func(string)); ok {
		hook("before-materialization")
	}

	var materializeSpan telemetry.Span
	if tel != nil {
		ctx, materializeSpan = tel.StartSpan(ctx, "agent_marketplace.pipeline.materialization", map[string]string{
			"target.count":   strconv.Itoa(len(targets)),
			"resolved.count": strconv.Itoa(len(resolved.Resolved)),
		})
	}
	report, err := materialize.Run(ctx, matRoot, resolved.Resolved,
		materialize.WithMaterializationModes(resolved.MaterializationModes),
		materialize.WithPruneFences(resolved.MalformedPruneFences))
	if materializeSpan != (telemetry.Span{}) {
		materializeSpan.SetAttributes(materializationResultAttrs(report, err))
		materializeSpan.End()
	}
	if report != nil {
		result.Report = report
		slog.Debug("materialization report received", "targets", len(report.PerRoot), "diagnostics", len(report.Diags))
		for _, d := range report.Diags {
			// Both severities are promoted through the SAME type so an error
			// keeps the resource, source location and hint that a warning has
			// always kept (bead agent-marketplace-oc2r).
			switch d.Severity {
			case diag.SeverityError:
				slog.Warn("materialize diagnostic promoted to error",
					"code", d.Code, "resource", d.Resource,
					"source_file", d.Source.File, "source_line", d.Source.Line)
				result.addMaterializeError(newDiagIssue(d))
			case diag.SeverityWarning:
				result.Warnings = append(result.Warnings, newDiagIssue(d))
			}
		}
	}
	if err != nil {
		result.addMaterializeError(err)
	}
	return result
}

// CollectManagedPaths returns the managed paths (component subdirectories and
// root config files) for each machine-scope target that has resolved artifacts.
//
// The IsMachineScope filter below is a DECISION, not an oversight, and bead
// agent-marketplace-5exj.30 re-affirmed it after bead .27 made
// harness.TargetInfo.AggregateFiles.Project truthful and a repo-scope collector
// became a two-line edit. Repo-scope aggregates are recovered through
// internal/ownership's mandatory per-mutation raw-byte backups — root-keyed, so
// they can tell two repositories apart, which the archive layout in
// internal/backup structurally cannot — as each target migrates onto that store.
// The full argument, including why the three alternatives are worse, is in the
// internal/backup package doc; read it before widening this filter.
//
// TestCollectManagedPathsIsMachineScopeOnlyByDecision pins the filter so
// widening it is a deliberate act that fails a test naming the decision.
func CollectManagedPaths(resolved []resolver.ResolvedArtifact) map[hcc.Target]backup.ManagedPaths {
	seen := make(map[hcc.Target]bool)
	for _, r := range resolved {
		if !r.Source.IsMachineScope() {
			continue
		}
		for _, t := range r.Harnesses {
			seen[t] = true
		}
	}

	result := make(map[hcc.Target]backup.ManagedPaths, len(seen))
	for t := range seen {
		if t == hcc.Target(targetmeta.TargetCodex) {
			continue
		}
		root, ok := harness.MachineRoot(t)
		if !ok {
			continue
		}
		info, ok := harness.TargetInfoFor(t)
		if !ok {
			continue
		}
		// Machine-scope component directories can diverge from the repo-scope
		// component names (e.g. Windsurf writes machine-wide workflows to
		// global_workflows/). ComponentsForScope is the same source of truth
		// materialization uses, so backup coverage cannot drift from the
		// directories that are actually written.
		machineComponents := info.ComponentsForScope(true)
		// Shared aggregate configuration files used to live in a hand-maintained
		// rootConfigFiles map here, which had no Devin entry and therefore left
		// hooks.v1.json, config.json, and mcp_config.json outside backup coverage.
		// TargetInfo.AggregateFilesForScope is now the single declaration both
		// materialization and this collector read, populated from the target's
		// native-layout descriptor where one is registered, so a target cannot
		// declare an aggregate file that backups do not cover. Bead
		// agent-marketplace-5exj.27 made that declaration truthful — until then
		// Gemini's settings.json was merged into on every sync with no backup at
		// all, because it is not inside any Gemini component directory.
		machineAggregates := info.AggregateFilesForScope(true)
		mappedFiles, mappingErr := harness.NativeAggregateBackupFiles(t)
		if mappingErr != nil {
			slog.Error("resolve native aggregate backup files", "target", t, "error", mappingErr)
			continue
		}
		if len(harness.NativeAggregateInstalls(t)) > 0 {
			destinations, err := harness.ResolveDestinations(t, true, ".")
			if err != nil {
				slog.Error("resolve native aggregate backup destinations", "target", t, "error", err)
				continue
			}
			machineAggregates = nil
			for _, destination := range destinations {
				if destination.Root == root {
					machineAggregates = append(machineAggregates, destination.Info.AggregateFilesForScope(true)...)
				}
			}
		}
		relPaths := make([]string, 0, len(machineComponents)+len(machineAggregates))
		relPaths = append(relPaths, machineComponents...)
		for _, rel := range machineAggregates {
			// An aggregate can live inside a component directory that is already
			// archived whole (Claude's hooks/hooks.json inside hooks/). Adding it
			// again would write the same bytes into the archive twice; the
			// coverage is identical either way, so keep the archive minimal.
			if coveredByAncestor(rel, relPaths) {
				continue
			}
			relPaths = append(relPaths, rel)
		}
		result[t] = backup.ManagedPaths{Root: root, RelPaths: relPaths, MappedFiles: mappedFiles}
	}
	return result
}

// CollectCodexBackupPartitions resolves the two native machine roots as one
// logical backup set. An empty partition is retained so publication and restore
// can prove that both roots participated in the same operation.
func CollectCodexBackupPartitions(resolved []resolver.ResolvedArtifact) ([]backup.CodexPartition, error) {
	codex := hcc.Target(targetmeta.TargetCodex)
	selected := false
	for _, resource := range resolved {
		if !resource.Source.IsMachineScope() {
			continue
		}
		for _, target := range resource.Harnesses {
			selected = selected || target == codex
		}
	}
	partitions, err := CodexBackupPartitions()
	if err != nil {
		return nil, err
	}
	if selected {
		return partitions, nil
	}
	for _, partition := range partitions {
		if len(partition.RelPaths) > 0 {
			return partitions, nil
		}
	}
	return nil, nil
}

// CodexBackupPartitions resolves the current native machine destinations.
func CodexBackupPartitions() ([]backup.CodexPartition, error) {
	codex := hcc.Target(targetmeta.TargetCodex)
	destinations, err := harness.ResolveDestinations(codex, true, ".")
	if err != nil {
		return nil, err
	}
	managed, err := materialize.CodexMachineManagedPaths(".")
	if err != nil {
		return nil, err
	}
	partitions := make([]backup.CodexPartition, 0, len(destinations))
	for _, destination := range destinations {
		partitions = append(partitions, backup.CodexPartition{ID: destination.ID, Root: destination.Root, RelPaths: managed[destination.ID]})
	}
	return partitions, nil
}

// coveredByAncestor reports whether rel is the same path as, or lives beneath,
// one of the already-collected managed paths. backup.Create archives a
// directory recursively, so a path covered this way is already recoverable.
//
// It is the predicate behind the backup-coverage contract asserted by
// TestCollectManagedPathsCoversEveryDeclaredAggregate: every declared
// machine-scope aggregate is recoverable, whether it is collected in its own
// right (Gemini's settings.json) or as part of a component directory (Claude's
// hooks/hooks.json).
func coveredByAncestor(rel string, collected []string) bool {
	rel = path.Clean(filepath.ToSlash(rel))
	for _, candidate := range collected {
		candidate = path.Clean(filepath.ToSlash(candidate))
		if rel == candidate || strings.HasPrefix(rel, candidate+"/") {
			return true
		}
	}
	return false
}

var errUnsafeGitignore = errors.New("gitignore is not a regular singly linked file")

func EnsureLocalConfigGitignored(repoRoot string) {
	localConfigPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	if _, err := os.Stat(localConfigPath); err != nil {
		return
	}
	root, err := os.OpenRoot(repoRoot)
	if err != nil {
		slog.Warn("failed to open gitignore root", "path", repoRoot, "code", "pipeline.gitignore.write-refused", "error", err)
		return
	}
	defer func() { _ = root.Close() }()
	flags := os.O_RDWR | os.O_APPEND | os.O_CREATE | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	f, err := root.OpenFile(".gitignore", flags, 0o644)
	if err != nil {
		slog.Warn("refused gitignore update", "path", filepath.Join(repoRoot, ".gitignore"), "code", "pipeline.gitignore.write-refused", "error", err)
		return
	}
	defer func() { _ = f.Close() }()
	if err := appendLocalConfigIgnoreEntries(f); err != nil {
		slog.Warn("refused gitignore update", "code", "pipeline.gitignore.write-refused", "error", err)
	}
}

func appendLocalConfigIgnoreEntries(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat gitignore: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 {
		return errUnsafeGitignore
	}
	if err := syscall.SetNonblock(int(f.Fd()), false); err != nil {
		return fmt.Errorf("restore blocking gitignore I/O: %w", err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("read gitignore: %w", err)
	}
	entries := []string{".agents/config.local.yaml", ".agents/lock.local.yaml"}
	present := make(map[string]bool, len(entries))
	for _, line := range strings.Split(string(data), "\n") {
		present[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, entry := range entries {
		if !present[entry] {
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	addition := strings.Join(missing, "\n") + "\n"
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		addition = "\n" + addition
	}
	if _, err := f.WriteString(addition); err != nil {
		return fmt.Errorf("append gitignore: %w", err)
	}
	return nil
}
