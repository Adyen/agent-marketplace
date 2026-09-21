package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	tea "github.com/charmbracelet/bubbletea"
)

type snapshotLoadedMsg struct {
	generation           requestGeneration
	rows                 []row
	driftReport          materialize.DriftReport
	driftAssessment      driftAssessment
	catalogs             map[string][]catalog.Entry
	resolved             []resolver.ResolvedArtifact
	mergedCfg            *config.Config
	configuredRegistries []config.RegistryEntry
	inventoryResult      *pipeline.ResolveResult
	materializationModes config.MaterializationModes
	localExcludes        []string
	globalHarnesses      []string
	projectHarnesses     []string
	targetHarnesses      []hcc.Target
	targetHarnessSource  config.Source
	errs                 []error
	warnings             []error
	diagnostics          []tuiDiagnostic
	refreshAfterLoad     bool
	pendingMigration     migrationPrompt
}

type reloadMsg snapshotLoadedMsg
type refreshLoadedMsg struct {
	snapshotLoadedMsg
}

type toastMsg struct {
	text     string
	duration time.Duration
}

// toastExpiredMsg is sent by a timer to clear an expired toast.
type toastExpiredMsg struct{}

// toastExpiryCmd returns a tea.Cmd that fires a toastExpiredMsg after d.
func toastExpiryCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return toastExpiredMsg{}
	})
}

func invalidConfigToastCmd() tea.Cmd {
	return func() tea.Msg {
		return toastMsg{text: "configuration is invalid; press E to edit config", duration: 3 * time.Second}
	}
}

type configWrittenMsg struct {
	operation  operationID
	followUp   bool
	skillName  string
	enabled    bool
	selectNext bool
	err        error
}

type syncDoneMsg struct {
	err error
}

const (
	updateStageRefresh         = "refresh"
	updateStageMaterialization = "materialization"
	updateStageReload          = "state reload"
)

type updateDoneMsg struct {
	generation requestGeneration
	snapshot   snapshotLoadedMsg
	stage      string
	err        error
}

// detailContentMsg carries the glamour-rendered resource content for the
// full-screen detail view. Produced by loadDetailContent (async tea.Cmd)
// and consumed in Update to populate the viewport.
type detailContentMsg struct {
	generation requestGeneration
	content    string
	width      int
	keepScroll bool
	path       string
}

type catalogLoadMode int

const (
	catalogLoadCached catalogLoadMode = iota
	catalogLoadRefresh
)

var (
	loadOrgConfig         = config.LoadOrganizationConfig
	loadGlobalConfig      = config.LoadGlobalConfig
	loadRepoConfig        = config.LoadRepoConfig
	loadLocalConfig       = config.LoadLocalConfig
	cloneOrPull           = cache.CloneOrPull
	discoverCatalog       = catalog.Discover
	runMaterialize        = materialize.Run
	runPipelineSync       = pipeline.Sync
	readSnapshotForUpdate = readSnapshot
)

func loadSnapshot(ctx context.Context, repoRoot, homeDir string, generation requestGeneration) tea.Cmd {
	return func() tea.Msg {
		snapshot := readSnapshot(ctx, repoRoot, homeDir, catalogLoadCached)
		snapshot.generation = generation
		return snapshot
	}
}

func refreshSnapshot(ctx context.Context, repoRoot, homeDir string, generation requestGeneration) tea.Cmd {
	return func() tea.Msg {
		snapshot := readSnapshot(ctx, repoRoot, homeDir, catalogLoadRefresh)
		snapshot.generation = generation
		return refreshLoadedMsg{snapshotLoadedMsg: snapshot}
	}
}

// tuiWorkspace preserves the previous empty-LocalConfigDir fallback: the
// cwd-discovered local-config root applies when it lives inside repoRoot;
// otherwise the repository-root workspace keeps explicit construction intact.
// When even the repository root cannot construct a Workspace, the zero value
// is returned deliberately so Resolve and Sync reject it with
// ErrInvalidWorkspace rather than acting on unvalidated roots.
func tuiWorkspace(repoRoot string) pipeline.Workspace {
	ws, err := pipeline.NewWorkspace(repoRoot, config.FindLocalConfigDir())
	if err == nil {
		return ws
	}
	slog.Debug("workspace construction with the discovered local-config root failed; falling back to the repository root", "error", err)
	fallback, err := pipeline.NewWorkspace(repoRoot, "")
	if err != nil {
		slog.Debug("workspace construction failed; returning the zero Workspace for downstream rejection", "error", err)
		return pipeline.Workspace{}
	}
	return fallback
}

func readSnapshot(ctx context.Context, repoRoot, homeDir string, mode catalogLoadMode) snapshotLoadedMsg {
	ws := tuiWorkspace(repoRoot)
	resolvedResult, releaseExactLease := pipeline.ResolveWithExactLockLease(ctx, pipeline.ResolveOptions{
		Workspace:     ws,
		CatalogMode:   tuiCatalogLoadMode(mode),
		SkipInventory: true,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: config.OrganizationConfigPath(),
			GlobalPath:       filepath.Join(homeDir, ".agents", "config.yaml"),
			RepoPath:         config.ProjectConfigPath(repoRoot),
			LocalPath:        config.LocalConfigPath(repoRoot),
		},
		Loaders: pipeline.Loaders{
			Org:    loadOrgConfig,
			Global: loadGlobalConfig,
			Repo:   loadRepoConfig,
			Local:  loadLocalConfig,
		},
		CatalogOption: []catalog.LoadOption{
			catalog.WithCloneOrPull(cloneOrPull),
			catalog.WithDiscoverFunc(discoverCatalog),
		},
		ReadOnlyExactLocks:        true,
		AllowMissingReadOnlyLocks: true,
	})
	defer releaseExactLease()

	base := snapshotLoadedMsg{
		catalogs:             entriesFromCatalogs(resolvedResult.CatalogLoad.Catalogs),
		resolved:             resolvedResult.Resolved,
		mergedCfg:            resolvedResult.LoadedCfg,
		inventoryResult:      &resolvedResult,
		materializationModes: resolvedResult.MaterializationModes,
		localExcludes:        localExcludes(resolvedResult.LocalCfg),
		globalHarnesses:      configHarnesses(resolvedResult.GlobalCfg),
		projectHarnesses:     configHarnesses(resolvedResult.RepoCfg),
		targetHarnesses:      resolvedResult.TargetHarnesses,
		targetHarnessSource:  resolvedResult.TargetHarnessSource,
		pendingMigration:     pendingMigrationFor(repoRoot, homeDir),
		driftAssessment:      driftFailed,
		refreshAfterLoad:     resolvedResult.CatalogLoad.RefreshAfterLoad,
	}
	if resolvedResult.MergedCfg != nil {
		base.configuredRegistries = append([]config.RegistryEntry(nil), resolvedResult.MergedCfg.Registries...)
	}

	var diagnostics []tuiDiagnostic
	diagnostics = appendSnapshotDiagnostics(diagnostics, diagnosticWarning, diagnosticConfig, resolvedResult.Warnings...)
	if len(resolvedResult.ConfigErrors) > 0 {
		diagnostics = appendSnapshotDiagnostics(diagnostics, diagnosticError, diagnosticConfig, resolvedResult.ConfigErrors...)
		base.diagnostics = deduplicateDiagnostics(diagnostics)
		base.errs = diagnosticValues(base.diagnostics, diagnosticError)
		base.warnings = diagnosticValues(base.diagnostics, diagnosticWarning)
		return base
	}

	var catalogOverlapWarnings []error
	var hardCatalogFailure bool
	if len(resolvedResult.CatalogErrors) > 0 {
		var hardErrs []error
		for _, err := range resolvedResult.CatalogErrors {
			if catalog.IsOverlapWarning(err) {
				catalogOverlapWarnings = append(catalogOverlapWarnings, err)
			} else {
				hardErrs = append(hardErrs, err)
			}
		}
		diagnostics = appendSnapshotDiagnostics(diagnostics, diagnosticWarning, diagnosticCatalog, catalogOverlapWarnings...)
		if len(hardErrs) > 0 {
			diagnostics = appendSnapshotDiagnostics(diagnostics, diagnosticError, diagnosticCatalog, hardErrs...)
			hardCatalogFailure = true
		}
	}

	var resolvedNames []string
	// Copy detail bytes before any downstream work can yield long enough for a
	// waiting GC to run immediately after the resolution lease is released.
	detailSources := captureSnapshotDetailSources(resolvedResult.Resolved, resolvedResult.CatalogLoad.Catalogs)
	for _, resolved := range resolvedResult.Resolved {
		resolvedNames = append(resolvedNames, skill.ResourceKey(resolved.Kind, resolved.Name))
	}
	dryRunReport, dryRunErr := runMaterialize(ctx, repoRoot, resolvedResult.Resolved, materialize.WithDryRun(), materialize.WithMaterializationModes(resolvedResult.MaterializationModes))
	driftReport := materialize.DriftReportFromReport(dryRunReport, resolvedNames)
	driftState := driftReady
	if hardCatalogFailure || dryRunReport == nil || dryRunErr != nil {
		driftState = driftFailed
	}

	for _, malformed := range resolvedResult.CatalogLoad.Malformed {
		diagnostics = appendSnapshotDiagnostics(diagnostics, diagnosticWarning, diagnosticCatalog, fmt.Errorf("malformed skill %q (%s): %w", malformed.DirName, malformed.Path, malformed.Error))
	}
	for _, err := range resolvedResult.ResolveErrors {
		if isSoftResolutionIssue(err) {
			diagnostics = appendSnapshotDiagnostics(diagnostics, diagnosticWarning, diagnosticResolution, err)
		} else {
			diagnostics = appendSnapshotDiagnostics(diagnostics, diagnosticError, diagnosticResolution, err)
			driftState = driftFailed
		}
	}
	if dryRunReport != nil {
		for _, diagnostic := range dryRunReport.Diags {
			severity := diagnosticWarning
			if diagnostic.IsError() {
				severity = diagnosticError
				driftState = driftFailed
			}
			diagnostics = appendSnapshotDiagnostics(diagnostics, severity, diagnosticMaterialization, pipeline.PromoteDiagnostic(diagnostic))
		}
	}
	if dryRunErr != nil {
		diagnostics = appendSnapshotDiagnostics(diagnostics, diagnosticError, diagnosticMaterialization, dryRunErr)
	}

	rowCfg := resolvedResult.LoadedCfg
	recoveryCfg := rowCfg
	if hardCatalogFailure && resolvedResult.MergedCfg != nil {
		recoveryCfg = resolvedResult.MergedCfg
	}
	base.rows = buildRows(rowCfg, resolvedResult.CatalogLoad.Catalogs, resolvedResult.Resolved, resolvedResult.LocalCfg, driftReport, harnessDefaultsBySource(resolvedResult.GlobalCfg, resolvedResult.RepoCfg))
	base.rows = appendUnavailableSelectionRows(base.rows, recoveryCfg, resolvedResult.LocalCfg, resolvedResult.ResolveErrors, resolvedResult.CatalogLoad.Catalogs)
	if rowsHaveUnavailable(base.rows) {
		driftState = driftFailed
	}
	attachDetailSources(base.rows, detailSources)
	base.driftReport = driftReport
	base.driftAssessment = driftState
	base.diagnostics = deduplicateDiagnostics(diagnostics)
	base.errs = diagnosticValues(base.diagnostics, diagnosticError)
	base.warnings = diagnosticValues(base.diagnostics, diagnosticWarning)
	base.refreshAfterLoad = resolvedResult.CatalogLoad.RefreshAfterLoad
	return base
}

func captureSnapshotDetailSources(resolved []resolver.ResolvedArtifact, catalogs map[string]catalogInfo) map[string]*detailSourceSnapshot {
	detailSources := make(map[string]*detailSourceSnapshot, len(resolved))
	var detailBudget detailCaptureBudget
	capturePath := func(key, sourcePath string) {
		if key == "" {
			return
		}
		if _, ok := detailSources[key]; !ok {
			detailSources[key] = detailBudget.capture(sourcePath)
		}
	}
	captureEntry := func(entry catalog.Entry) {
		capturePath(entry.Path, catalog.SourcePath(entry))
	}
	for _, artifact := range resolved {
		captureEntry(artifact.Entry)
	}
	for _, key := range slices.Sorted(maps.Keys(catalogs)) {
		loaded := catalogs[key]
		entries := slices.Clone(loaded.Entries)
		slices.SortFunc(entries, func(a, b catalog.Entry) int {
			if order := strings.Compare(a.Kind.String(), b.Kind.String()); order != 0 {
				return order
			}
			if order := strings.Compare(a.Name, b.Name); order != 0 {
				return order
			}
			if order := strings.Compare(a.Path, b.Path); order != 0 {
				return order
			}
			return strings.Compare(catalog.SourcePath(a), catalog.SourcePath(b))
		})
		for _, entry := range entries {
			captureEntry(entry)
		}
		malformedEntries := slices.Clone(loaded.Malformed)
		slices.SortFunc(malformedEntries, func(a, b skill.MalformedSkill) int {
			if order := strings.Compare(a.DirName, b.DirName); order != 0 {
				return order
			}
			return strings.Compare(a.Path, b.Path)
		})
		for _, malformed := range malformedEntries {
			capturePath(malformed.Path, malformed.Path)
		}
	}
	return detailSources
}

func isSoftResolutionIssue(err error) bool {
	return resolver.IsSoftSelectedNotFound(err) || resolver.IsSoftSelectedMalformed(err) || resolver.IsNameCollision(err) || resolver.IsWarning(err)
}

func configHarnesses(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	return append([]string(nil), cfg.Harnesses...)
}

func harnessDefaultsBySource(globalCfg, repoCfg *config.Config) map[config.Source][]string {
	defaults := make(map[config.Source][]string)
	if globalCfg != nil {
		defaults[config.UserPersonal] = configHarnesses(globalCfg)
	}
	if repoCfg != nil {
		defaults[config.RepoCommitted] = configHarnesses(repoCfg)
	}
	return defaults
}

func loadCatalogsForTUI(cfg *config.Config, repoRoot string, mode catalogLoadMode) (map[string]catalogInfo, bool, error) {
	result := catalog.LoadRegistries(cfg, repoRoot, tuiCatalogLoadMode(mode), catalog.WithCloneOrPull(cloneOrPull), catalog.WithDiscoverFunc(discoverCatalog))
	if len(result.Errors) > 0 {
		return result.Catalogs, result.RefreshAfterLoad, errors.Join(result.Errors...)
	}
	return result.Catalogs, result.RefreshAfterLoad, nil
}

type catalogInfo = catalog.LoadedCatalog

func tuiCatalogLoadMode(mode catalogLoadMode) catalog.LoadMode {
	if mode == catalogLoadRefresh {
		return catalog.LoadModeRefresh
	}
	return catalog.LoadModeCached
}

func localExcludes(cfg *config.LocalConfig) []string {
	if cfg == nil {
		return nil
	}
	return cfg.AllExcludes()
}

func addLocalKindSelections(dst map[string]struct{}, kind ir.ResourceKind, names []string) {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		dst[skill.ResourceKey(kind, name)] = struct{}{}
	}
}

// registryIdentityKey builds the lookup key used to attribute a local
// registry entry to its catalog key. It mirrors the registry cache identity
// (normalized URL + ref + path) so entries that share a URL/path but differ by
// ref are not collapsed onto the same catalog key.
func registryIdentityKey(url, ref, path string) string {
	return config.NormalizeGitURL(strings.TrimSpace(url)) + "\x01" + strings.TrimSpace(ref) + "\x01" + config.NormalizeRegistryPath(path)
}

// addScopedLocalSelection records a single registry-scoped local selection,
// keyed by resource key → set of catalog keys (registry identity).
func addScopedLocalSelection(dst map[string]map[string]struct{}, resourceKey, catalogKey string) {
	if dst[resourceKey] == nil {
		dst[resourceKey] = make(map[string]struct{})
	}
	dst[resourceKey][catalogKey] = struct{}{}
}

// addScopedLocalKindSelections records registry-scoped local selections for a
// kind, keyed by the providing registry's catalog key so identical resource
// names from different registries stay distinct.
func addScopedLocalKindSelections(dst map[string]map[string]struct{}, kind ir.ResourceKind, names []string, catalogKey string) {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		addScopedLocalSelection(dst, skill.ResourceKey(kind, name), catalogKey)
	}
}

// localScopedContains reports whether a resource is locally selected, checking
// both the non-registry-scoped *Any set (top-level local selectors) and the
// registry-scoped map keyed by the resource's catalog key.
func localScopedContains(scoped map[string]map[string]struct{}, any map[string]struct{}, resourceKey, catalogKey string) bool {
	if _, ok := any[resourceKey]; ok {
		return true
	}
	if set, ok := scoped[resourceKey]; ok {
		if _, ok := set[catalogKey]; ok {
			return true
		}
	}
	return false
}

func localSelectorCounts(cfg *config.LocalConfig) (enabledCount int, excludedCount int) {
	if cfg == nil {
		return 0, 0
	}
	enabledCount += len(cfg.Skills.Include) + len(cfg.Commands.Include) + len(cfg.Subagents.Include) + len(cfg.Hooks.Include) + len(cfg.MCP.Include)
	excludedCount += len(cfg.Skills.Exclude) + len(cfg.Commands.Exclude) + len(cfg.Subagents.Exclude) + len(cfg.Hooks.Exclude) + len(cfg.MCP.Exclude)
	return enabledCount, excludedCount
}

func marketplaceProtocolKind(kind ir.ResourceKind) string {
	if kind == ir.KindMcpServer {
		return "mcpserver"
	}
	return kind.String()
}

func marketplaceInstalledName(pin config.MarketplaceResourcePin) string {
	if pin.InstalledName != "" {
		return pin.InstalledName
	}
	return pin.Name
}

func marketplaceCatalogKey(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) string {
	return config.MarketplaceCatalogKey(entry, pin)
}

func marketplaceSelectionKey(catalogKey, kind, installedName string) string {
	return catalogKey + "\x00" + kind + "\x00" + installedName
}

func indexMarketplaceSelections(cfg *config.Config) map[string]map[config.Source]marketplaceSelection {
	if cfg == nil || len(cfg.Marketplaces) == 0 {
		return nil
	}
	indexed := make(map[string]map[config.Source]marketplaceSelection)
	for _, entry := range cfg.Marketplaces {
		for _, pin := range entry.Resources {
			key := marketplaceSelectionKey(marketplaceCatalogKey(entry, pin), pin.Kind, marketplaceInstalledName(pin))
			if indexed[key] == nil {
				indexed[key] = make(map[config.Source]marketplaceSelection)
			}
			indexed[key][entry.Source] = marketplaceSelection{Entry: entry, Pin: pin}
		}
	}
	return indexed
}

func marketplaceScopeFlags(selections map[config.Source]marketplaceSelection) (global, project, local bool) {
	for source := range selections {
		switch source {
		case config.Organization, config.UserPersonal:
			global = true
		case config.RepoCommitted:
			project = true
		case config.RepoPersonal:
			local = true
		}
	}
	return global, project, local
}

func marketplaceCollectionOnly(selections map[config.Source]marketplaceSelection) bool {
	if len(selections) == 0 {
		return false
	}
	for _, selection := range selections {
		if selection.Pin.Direct || len(selection.Pin.SelectedBy.List()) == 0 {
			return false
		}
	}
	return true
}

func buildRows(cfg *config.Config, catalogs map[string]catalogInfo, resolved []resolver.ResolvedArtifact, localCfg *config.LocalConfig, driftReport materialize.DriftReport, harnessDefaults ...map[config.Source][]string) []row {
	// Compute per-scope enablement by walking registries and asking each one
	// which skill names it selects. Global vs project scope is distinguished
	// by RegistryEntry.IsGlobal. Local enablement and excludes come from the
	// local config directly.
	totalCatalogEntries := 0
	totalMalformed := 0
	for _, cat := range catalogs {
		totalCatalogEntries += len(cat.Entries)
		totalMalformed += len(cat.Malformed)
	}
	localEnabledCount, localExcludedCount := localSelectorCounts(localCfg)
	marketplaceSelectionsByArtifact := indexMarketplaceSelections(cfg)

	globalEnabled := make(map[string]map[string]struct{}, len(resolved))
	projectEnabled := make(map[string]map[string]struct{}, len(resolved))
	// Local selections are registry-scoped (resource key → set of catalog
	// keys) just like global/project, so the same resource name from two
	// registries does not badge both rows as local. Top-level local selectors
	// (config.local.yaml skills.include, etc.) are NOT registry-scoped — they
	// apply to whichever registry provides the resource — so they live in the
	// *Any sets and match any catalog key.
	localEnabled := make(map[string]map[string]struct{}, localEnabledCount)
	localExcluded := make(map[string]map[string]struct{}, localExcludedCount)
	localEnabledAny := make(map[string]struct{}, localEnabledCount)
	localExcludedAny := make(map[string]struct{}, localExcludedCount)
	// catalogKeyByIdentity maps a registry's (normalized URL, ref, path) to its
	// catalog key so registry-scoped local entries (which carry no catalog key
	// of their own) can be attributed to the right registry identity. Ref is
	// part of the key because the cache (and thus the catalog key) is per-ref:
	// the same URL+path at two refs are distinct registries and must not
	// collapse onto a single catalog key.
	catalogKeyByIdentity := make(map[string]string, totalCatalogEntries)
	if cfg != nil {
		for i := range cfg.Registries {
			e := &cfg.Registries[i]
			k := registryIdentityKey(e.URL, e.Ref, e.Path)
			if _, ok := catalogKeyByIdentity[k]; !ok {
				catalogKeyByIdentity[k] = e.CatalogKey
			}
		}
	}
	exactEntryBySeenKey := make(map[string]*config.RegistryEntry, totalCatalogEntries)
	entryByKeyURL := make(map[string]*config.RegistryEntry, totalCatalogEntries)
	harnessSourceBySeenKey := make(map[string]string, totalCatalogEntries)
	harnessSourceByKeyURL := make(map[string]string, totalCatalogEntries)

	layerHarnesses := layerHarnessDefaults(cfg)
	for _, defaults := range harnessDefaults {
		for source, harnesses := range defaults {
			layerHarnesses[source] = append([]string(nil), harnesses...)
		}
	}

	if cfg != nil {
		for i := range cfg.Registries {
			entry := &cfg.Registries[i]
			cat, ok := catalogs[entry.CatalogKey]
			if !ok {
				continue
			}
			sourceLabel := harnessSourceLabel(*entry, layerHarnesses[entry.Source])
			selected := resolver.ResourcesSelected(*entry, cat.Entries)
			if entry.Source == config.RepoPersonal {
				// Registry-scoped local entries (config.local.yaml) are local
				// enablement, not project. Without this they'd be mis-attributed
				// as project (P badge) and the local toggle could not see them.
				// Key by catalog key so a same-name resource from another
				// registry does not get the local badge too.
				for _, resource := range selected {
					addScopedLocalSelection(localEnabled, skill.ResourceKey(resource.Kind, resource.Name), entry.CatalogKey)
				}
			} else {
				target := projectEnabled
				if entry.Source.IsMachineScope() {
					target = globalEnabled
				}
				for _, resource := range selected {
					key := skill.ResourceKey(resource.Kind, resource.Name)
					if target[key] == nil {
						target[key] = make(map[string]struct{})
					}
					target[key][entry.CatalogKey] = struct{}{}
				}
			}
			for _, s := range cat.Entries {
				key := skill.ResourceKey(s.Kind, s.Name)
				seenKey := key + "\x01" + entry.CatalogKey
				if _, ok := exactEntryBySeenKey[seenKey]; !ok {
					exactEntryBySeenKey[seenKey] = entry
				}
				urlKey := key + "\x01" + entry.URL
				if _, ok := entryByKeyURL[urlKey]; !ok {
					entryByKeyURL[urlKey] = entry
				}
				if _, ok := harnessSourceBySeenKey[seenKey]; !ok {
					harnessSourceBySeenKey[seenKey] = sourceLabel
				}
				if _, ok := harnessSourceByKeyURL[urlKey]; !ok {
					harnessSourceByKeyURL[urlKey] = sourceLabel
				}
			}
		}
	}

	if localCfg != nil {
		// Top-level local selectors are not registry-scoped: they apply to
		// whichever registry provides the resource, so track them in the *Any
		// sets (matched against any catalog key).
		addLocalKindSelections(localEnabledAny, ir.KindSkill, localCfg.Skills.Include)
		addLocalKindSelections(localEnabledAny, ir.KindCommand, localCfg.Commands.Include)
		addLocalKindSelections(localEnabledAny, ir.KindSubagent, localCfg.Subagents.Include)
		addLocalKindSelections(localEnabledAny, ir.KindHook, localCfg.Hooks.Include)
		addLocalKindSelections(localEnabledAny, ir.KindMcpServer, localCfg.MCP.Include)
		addLocalKindSelections(localExcludedAny, ir.KindSkill, localCfg.Skills.Exclude)
		addLocalKindSelections(localExcludedAny, ir.KindCommand, localCfg.Commands.Exclude)
		addLocalKindSelections(localExcludedAny, ir.KindSubagent, localCfg.Subagents.Exclude)
		addLocalKindSelections(localExcludedAny, ir.KindHook, localCfg.Hooks.Exclude)
		addLocalKindSelections(localExcludedAny, ir.KindMcpServer, localCfg.MCP.Exclude)

		// Registry-scoped local selections (install --scope local and the TUI
		// now write these instead of the top-level selector). Key them by the
		// providing registry's catalog identity so the TUI recognizes locally
		// enabled/excluded resources without conflating same-name resources
		// across registries. When the local registry's catalog is not present
		// in the merged config, fall back to the *Any sets so the selection is
		// still recognized (just not registry-disambiguated).
		for i := range localCfg.Registries {
			re := &localCfg.Registries[i]
			catalogKey := catalogKeyByIdentity[registryIdentityKey(re.URL, re.Ref, re.Path)]
			for _, kind := range config.AllKinds {
				sel := re.SelectorForKind(kind)
				if catalogKey != "" {
					addScopedLocalKindSelections(localEnabled, kind, sel.Include, catalogKey)
					addScopedLocalKindSelections(localExcluded, kind, sel.Exclude, catalogKey)
				} else {
					addLocalKindSelections(localEnabledAny, kind, sel.Include)
					addLocalKindSelections(localExcludedAny, kind, sel.Exclude)
				}
			}
		}
	}

	driftByName := make(map[string]materialize.DriftItem, len(driftReport.Missing)+len(driftReport.Broken)+len(driftReport.Orphaned))
	for _, item := range driftReport.Missing {
		driftByName[item.Name] = item
	}
	for _, item := range driftReport.Broken {
		driftByName[item.Name] = item
	}
	for _, item := range driftReport.Orphaned {
		driftByName[item.Name] = item
	}

	// seen is keyed by (kind, name, catalogKey) so same-name skills from
	// different registries each get their own row.
	seen := make(map[string]struct{}, len(resolved)+totalCatalogEntries+totalMalformed)
	rows := make([]row, 0, len(resolved)+totalCatalogEntries+totalMalformed)

	// Add resolved skills first
	for _, s := range resolved {
		key := skill.ResourceKey(s.Kind, s.Name)
		seenKey := key + "\x01" + s.CatalogKey
		if _, dup := seen[seenKey]; dup {
			continue
		}
		seen[seenKey] = struct{}{}

		var driftItem *materialize.DriftItem
		if item, hasDrift := driftByName[key]; hasDrift {
			driftItem = &item
		}

		_, enabledGlobal := globalEnabled[key][s.CatalogKey]
		_, enabledProject := projectEnabled[key][s.CatalogKey]
		enabledLocal := localScopedContains(localEnabled, localEnabledAny, key, s.CatalogKey)
		excludedLocally := localScopedContains(localExcluded, localExcludedAny, key, s.CatalogKey)
		var marketplaceSelections map[config.Source]marketplaceSelection
		if s.IsMarketplace {
			marketplaceSelections = marketplaceSelectionsByArtifact[marketplaceSelectionKey(s.CatalogKey, marketplaceProtocolKind(s.Kind), s.Name)]
			enabledGlobal, enabledProject, enabledLocal = marketplaceScopeFlags(marketplaceSelections)
		}

		// When a resource comes from a collection (Via is set), it inherits
		// the collection's per-scope enablement. The collection itself is
		// what the entry explicitly selects; the expanded members are implicit.
		if s.Via != "" {
			// Extract the collection name from the Via annotation
			// (format: "collection:<name>" or "collection:<name>@<ver>").
			collectionKey := skill.ResourceKey(ir.KindCollection, collectionNameFromVia(s.Via))
			if len(globalEnabled[collectionKey]) > 0 && !enabledGlobal {
				enabledGlobal = true
			}
			if len(projectEnabled[collectionKey]) > 0 && !enabledProject {
				enabledProject = true
			}
			// Locally selected collections must light up their members too,
			// otherwise local-scope collection installs render as disabled.
			if !enabledLocal && localScopedContains(localEnabled, localEnabledAny, collectionKey, s.CatalogKey) {
				enabledLocal = true
			}
		}

		entry := exactEntryBySeenKey[seenKey]
		if entry == nil {
			entry = entryByKeyURL[key+"\x01"+s.RegistryURL]
		}
		var harnessSource string
		if val, ok := harnessSourceBySeenKey[seenKey]; ok {
			harnessSource = val
		} else {
			harnessSource = harnessSourceByKeyURL[key+"\x01"+s.RegistryURL]
		}

		rows = append(rows, row{
			Artifact:                  s,
			IsAvailable:               false,
			IsExcluded:                excludedLocally,
			DriftItem:                 driftItem,
			EnabledGlobal:             enabledGlobal,
			EnabledProject:            enabledProject,
			EnabledLocal:              enabledLocal,
			ExcludedLocally:           excludedLocally,
			RegistryURL:               s.RegistryURL,
			RegistryName:              registryDisplayName(s.RegistryURL),
			RegistryPath:              registryEntryPathFor(entry),
			RegistryRef:               registryEntryRefFor(entry),
			Harnesses:                 targetsToHarnesses(s.Harnesses),
			HarnessSource:             harnessSource,
			MarketplaceSelections:     marketplaceSelections,
			MarketplaceCollectionOnly: marketplaceCollectionOnly(marketplaceSelections),
		})
	}

	if cfg != nil && len(cfg.MarketplaceExclusions) > 0 {
		for _, entry := range cfg.Marketplaces {
			if entry.Source != config.Organization || entry.Required {
				continue
			}
			for _, pin := range entry.Resources {
				kind, ok := mpapi.ToIR(pin.Kind)
				if !ok || kind == ir.KindCollection {
					continue
				}
				name := marketplaceInstalledName(pin)
				key := skill.ResourceKey(kind, name)
				catalogKey := marketplaceCatalogKey(entry, pin)
				seenKey := key + "\x01" + catalogKey
				if !slices.ContainsFunc(cfg.MarketplaceExclusions, func(exclusion config.MarketplaceExclusion) bool {
					return exclusion.Source == config.UserPersonal && exclusion.Matches(entry, pin)
				}) {
					continue
				}
				if _, exists := seen[seenKey]; exists {
					continue
				}
				seen[seenKey] = struct{}{}
				selections := marketplaceSelectionsByArtifact[marketplaceSelectionKey(catalogKey, pin.Kind, name)]
				rows = append(rows, row{
					Artifact: resolver.ResolvedArtifact{
						Entry: catalog.Entry{Kind: kind, Name: name}, RegistryURL: entry.URL,
						RegistryPath: pin.RegistryID, Ref: pin.Revision, Source: entry.Source,
						CatalogKey: catalogKey, IsMarketplace: true, MarketplaceURL: entry.URL,
						MarketplaceInstanceID: entry.InstanceID, MarketplaceRegistryID: pin.RegistryID, MarketplaceResourceName: pin.Name,
					},
					IsAvailable: true, IsExcluded: true, ExcludedByUser: true, EnabledGlobal: true,
					RegistryURL: entry.URL, RegistryName: registryDisplayName(entry.URL),
					MarketplaceSelections: selections, MarketplaceCollectionOnly: marketplaceCollectionOnly(selections),
				})
			}
		}
	}

	// Add available skills not in resolved, using the resolver's collision
	// winners so the catalog view matches materialization selection.
	for _, available := range resolver.ResolveCatalogView(cfg, entriesFromCatalogs(catalogs), resolved) {
		if available.Selected {
			continue
		}
		s := available.ResolvedArtifact
		key := skill.ResourceKey(s.Kind, s.Name)
		seenKey := key + "\x01" + s.CatalogKey
		if _, dup := seen[seenKey]; dup {
			continue
		}
		seen[seenKey] = struct{}{}

		excludedLocally := localScopedContains(localExcluded, localExcludedAny, key, s.CatalogKey)

		var harnessSource string
		if val, ok := harnessSourceBySeenKey[seenKey]; ok {
			harnessSource = val
		} else {
			harnessSource = harnessSourceByKeyURL[key+"\x01"+s.RegistryURL]
		}

		rows = append(rows, row{
			Artifact:        s,
			IsAvailable:     true,
			IsExcluded:      excludedLocally,
			ExcludedLocally: excludedLocally,
			RegistryURL:     s.RegistryURL,
			RegistryName:    registryDisplayName(s.RegistryURL),
			RegistryPath:    s.RegistryPath,
			RegistryRef:     s.Ref,
			Harnesses:       targetsToHarnesses(s.Harnesses),
			HarnessSource:   harnessSource,
		})
	}

	// Add malformed skills. Deduplicate by (catalogKey, DirName) so
	// same-name malformed entries from different catalogs each appear.
	for catalogKey, cat := range catalogs {
		for _, m := range cat.Malformed {
			name := m.DirName
			seenKey := name + "\x01" + catalogKey
			if _, dup := seen[seenKey]; dup {
				continue
			}
			seen[seenKey] = struct{}{}

			rows = append(rows, row{
				IsMalformed:  true,
				MalformedMsg: m.Error.Error(),
				Artifact: resolver.ResolvedArtifact{
					Entry: catalog.Entry{Name: name, Path: m.Path},
				},
			})
		}
	}

	return rows
}

const unavailableCatalogReason = "selection is unavailable because its catalog failed to load"

func appendUnavailableSelectionRows(rows []row, cfg *config.Config, localCfg *config.LocalConfig, resolveErrs []error, catalogs map[string]catalogInfo) []row {
	seen := make(map[string]struct{}, len(rows)+len(resolveErrs))
	available := make(map[string]struct{}, len(rows))
	for i := range rows {
		key := skill.ResourceKey(rows[i].Artifact.Kind, rows[i].Artifact.Name)
		seen[key+"\x01"+registryIdentityKey(rows[i].RegistryURL, rows[i].RegistryRef, rows[i].RegistryPath)] = struct{}{}
		if rows[i].IsAvailable {
			available[key] = struct{}{}
		}
	}
	layerHarnesses := layerHarnessDefaults(cfg)
	appendCandidate := func(kind ir.ResourceKind, name, url, ref, path string, required bool, reason string) {
		key := skill.ResourceKey(kind, name)
		identity := registryIdentityKey(url, ref, path)
		seenKey := key + "\x01" + identity
		if _, dup := seen[seenKey]; dup {
			return
		}
		var first, firstSelecting *config.RegistryEntry
		var enabledGlobal, enabledProject, enabledLocal bool
		direct := false
		if cfg != nil {
			for i := range cfg.Registries {
				entry := &cfg.Registries[i]
				if registryIdentityKey(entry.URL, entry.Ref, entry.Path) != identity {
					continue
				}
				if first == nil {
					first = entry
				}
				entrySelected, localSelected := resolver.EffectiveDirectSelectionSources(*entry, cfg.Registries, localCfg, kind, name)
				if entrySelected {
					if firstSelecting == nil {
						firstSelecting = entry
					}
					direct = true
					required = required || entry.Required
					switch {
					case entry.Source.IsMachineScope():
						enabledGlobal = true
					case entry.Source == config.RepoCommitted:
						enabledProject = true
					case entry.Source == config.RepoPersonal:
						enabledLocal = true
					}
				}
				if localSelected {
					direct = true
					enabledLocal = true
				}
			}
		}
		excludedLocally := false
		if localCfg != nil {
			if slices.Contains(localCfg.SelectorForKind(kind).Exclude, name) {
				excludedLocally = true
			}
			for i := range localCfg.Registries {
				re := &localCfg.Registries[i]
				if registryIdentityKey(re.URL, re.Ref, re.Path) != identity {
					continue
				}
				if slices.Contains(re.SelectorForKind(kind).Exclude, name) {
					excludedLocally = true
				}
			}
		}
		meta := firstSelecting
		if meta == nil {
			meta = first
		}
		if !direct {
			return
		}
		seen[seenKey] = struct{}{}
		harnesses := meta.Harnesses
		if len(harnesses) == 0 {
			harnesses = layerHarnesses[meta.Source]
		}
		rows = append(rows, row{
			Artifact: resolver.ResolvedArtifact{
				Entry:          catalog.Entry{Kind: kind, Name: name, SourceHarness: meta.SourceHarness},
				RegistryURL:    meta.URL,
				RegistryPath:   meta.Path,
				Ref:            meta.Ref,
				V4Kind:         meta.V4Kind,
				Source:         meta.Source,
				FilePrecedence: meta.FilePrecedence,
				Required:       required,
				CatalogKey:     meta.CatalogKey,
			},
			UnavailableReason: reason,
			DriftItem:         &materialize.DriftItem{Name: key, Reason: materialize.DriftBroken},
			IsExcluded:        excludedLocally,
			ExcludedLocally:   excludedLocally,
			EnabledGlobal:     enabledGlobal,
			EnabledProject:    enabledProject,
			EnabledLocal:      enabledLocal,
			RegistryURL:       meta.URL,
			RegistryName:      registryDisplayName(meta.URL),
			RegistryPath:      meta.Path,
			RegistryRef:       meta.Ref,
			Harnesses:         append([]string(nil), harnesses...),
			HarnessSource:     harnessSourceLabel(*meta, layerHarnesses[meta.Source]),
		})
	}
	for _, err := range resolveErrs {
		var snf *resolver.SelectedNotFoundError
		if !errors.As(err, &snf) || snf.Via != "" {
			continue
		}
		appendCandidate(snf.Kind, snf.Name, snf.URL, snf.Ref, snf.Path, snf.Required, snf.Error())
	}
	if cfg != nil {
		failedIdentities := make(map[string]struct{})
		var failedRepresentative *config.RegistryEntry
		for i := range cfg.Registries {
			entry := &cfg.Registries[i]
			if _, loaded := catalogs[entry.CatalogKey]; loaded {
				continue
			}
			for _, selected := range resolver.ResourcesSelected(*entry, nil) {
				appendCandidate(selected.Kind, selected.Name, entry.URL, entry.Ref, entry.Path, entry.Required, unavailableCatalogReason)
			}
			identity := registryIdentityKey(entry.URL, entry.Ref, entry.Path)
			if _, dup := failedIdentities[identity]; !dup {
				failedIdentities[identity] = struct{}{}
				failedRepresentative = entry
			}
		}
		if localCfg != nil && len(failedIdentities) == 1 {
			entry := failedRepresentative
			for _, kind := range config.AllKinds {
				for _, name := range localCfg.SelectorForKind(kind).Include {
					if _, covered := available[skill.ResourceKey(kind, name)]; covered {
						continue
					}
					appendCandidate(kind, name, entry.URL, entry.Ref, entry.Path, entry.Required, unavailableCatalogReason)
				}
			}
		}
	}
	return rows
}

// registryDisplayName derives a short label from a registry URL.
// Strips trailing ".git" and returns the last path segment.
// Returns "" for empty input (e.g., malformed skills with no source).
// entriesFromCatalogs extracts the entry slices from a LoadedCatalog map
// for consumption by resolver.Resolve.
func entriesFromCatalogs(catalogs map[string]catalog.LoadedCatalog) map[string][]catalog.Entry {
	result := make(map[string][]catalog.Entry, len(catalogs))
	for k, v := range catalogs {
		result[k] = v.Entries
	}
	return result
}

func registryDisplayName(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}
	url = strings.TrimSuffix(url, ".git")
	if idx := strings.LastIndexAny(url, "/:"); idx >= 0 {
		url = url[idx+1:]
	}
	return url
}

// findRegistryEntry finds the registry entry matching the given resource key
// that also matches both the provided registry URL and catalogKey. Falls back
// to URL-only match, then to the first entry for the key. CatalogKey is
// needed because the same URL can appear with different Path values (e.g.
// registryEntryPathFor returns the path subdirectory for the given entry.
// Returns "" if the entry is nil.
func registryEntryPathFor(entry *config.RegistryEntry) string {
	if entry != nil {
		return entry.Path
	}
	return ""
}

// registryEntryRefFor returns the git ref for the given entry.
// Returns "" if the entry is nil.
func registryEntryRefFor(entry *config.RegistryEntry) string {
	if entry != nil {
		return entry.Ref
	}
	return ""
}

func targetsToHarnesses(targets []hcc.Target) []string {
	if targets == nil {
		return nil
	}
	harnesses := make([]string, 0, len(targets))
	for _, target := range targets {
		h := strings.TrimSpace(string(target))
		if h != "" && !slices.Contains(harnesses, h) {
			harnesses = append(harnesses, h)
		}
	}
	return harnesses
}

func layerHarnessDefaults(cfg *config.Config) map[config.Source][]string {
	defaults := make(map[config.Source][]string)
	if cfg == nil {
		return defaults
	}
	for _, entry := range cfg.Registries {
		if _, ok := defaults[entry.Source]; ok {
			continue
		}
		defaults[entry.Source] = append([]string(nil), entry.Harnesses...)
	}
	return defaults
}

func harnessSourceLabel(entry config.RegistryEntry, layerDefault []string) string {
	if len(entry.Harnesses) == 0 {
		return ""
	}
	if sameStringSet(entry.Harnesses, layerDefault) {
		if entry.Source.IsMachineScope() {
			return "global default"
		}
		return "project default"
	}
	return "registry override"
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, av := range a {
		if !slices.Contains(b, av) {
			return false
		}
	}
	return true
}

// ReadSkillMarkdown reads the SKILL.md content for a given resolved skill path.
func ReadSkillMarkdown(path string) (string, error) {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read resource source: %w", err)
		}
		return string(data), nil
	}
	mdPath := filepath.Join(path, "SKILL.md")
	data, err := os.ReadFile(mdPath)
	if err != nil {
		return "", fmt.Errorf("read SKILL.md: %w", err)
	}
	return string(data), nil
}

// loadDetailContent reads and glamour-renders a skill's SKILL.md in a
// background goroutine so the TUI can transition to the detail pane
// immediately without blocking on file I/O or markdown rendering.
func loadDetailContent(r row, generation requestGeneration, width int, keepScroll bool) tea.Cmd {
	return func() tea.Msg {
		return detailContentMsg{
			generation: generation,
			content:    renderDetailFullContent(r, width),
			width:      width,
			keepScroll: keepScroll,
		}
	}
}

func tuiPipelineOptions(repoRoot string) pipeline.ResolveOptions {
	ws := tuiWorkspace(repoRoot)
	return pipeline.ResolveOptions{
		Workspace: ws,
		Loaders: pipeline.Loaders{
			Org:    loadOrgConfig,
			Global: loadGlobalConfig,
			Repo:   loadRepoConfig,
			Local:  loadLocalConfig,
		},
		CatalogOption: []catalog.LoadOption{
			catalog.WithCloneOrPull(cloneOrPull),
			catalog.WithDiscoverFunc(discoverCatalog),
		},
	}
}

// runSyncCmd runs the authoritative sync pipeline as a tea.Cmd.
// It is a package-level var so tests can override it with a no-op to
// avoid real I/O when inspecting command composition in assertions.
var runSyncCmd = func(ctx context.Context, repoRoot string) tea.Cmd {
	return func() tea.Msg {
		result := runPipelineSync(ctx, tuiPipelineOptions(repoRoot))
		return syncDoneMsg{err: errors.Join(result.Errors...)}
	}
}

func runDependencySyncCmd(ctx context.Context, repoRoot string, sync DependencyLifecycleFunc) tea.Cmd {
	return func() tea.Msg { return syncDoneMsg{err: sync(ctx, repoRoot)} }
}

func runDependencyUpdateCmd(ctx context.Context, repoRoot, homeDir string, generation requestGeneration, update DependencyLifecycleFunc) tea.Cmd {
	return func() tea.Msg {
		lifecycleErr := update(ctx, repoRoot)
		current := readSnapshotForUpdate(ctx, repoRoot, homeDir, catalogLoadCached)
		current.generation = generation
		result := updateDoneMsg{generation: generation, snapshot: current, stage: updateStageRefresh}
		if lifecycleErr != nil {
			result.err = fmt.Errorf("update %s failed: %w", result.stage, lifecycleErr)
		} else if reloadErr := errors.Join(diagnosticErrors(current.diagnostics)...); reloadErr != nil {
			result.stage = updateStageReload
			result.err = fmt.Errorf("update %s failed: %w", result.stage, reloadErr)
		}
		return result
	}
}

func (m Model) dependencySyncCmd() tea.Cmd {
	if m.dependencyLifecycle.Sync != nil {
		return runDependencySyncCmd(ctxOrBackground(m.ctx), m.repoRoot, m.dependencyLifecycle.Sync)
	}
	return runSyncCmd(ctxOrBackground(m.ctx), m.repoRoot)
}

func (m Model) dependencyUpdateCmd(generation requestGeneration) tea.Cmd {
	if m.dependencyLifecycle.Update != nil {
		return runDependencyUpdateCmd(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation, m.dependencyLifecycle.Update)
	}
	return runUpdateCmd(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation)
}

func runUpdateCmd(ctx context.Context, repoRoot, homeDir string, generation requestGeneration) tea.Cmd {
	return func() tea.Msg {
		syncResult := runPipelineSync(ctx, tuiPipelineOptions(repoRoot))
		current := readSnapshotForUpdate(ctx, repoRoot, homeDir, catalogLoadCached)
		current.generation = generation
		for _, warning := range syncResult.Warnings {
			current.diagnostics = appendSnapshotDiagnostics(current.diagnostics, diagnosticWarning, diagnosticUpdate, warning)
		}
		current.diagnostics = deduplicateDiagnostics(current.diagnostics)
		current.errs = diagnosticValues(current.diagnostics, diagnosticError)
		current.warnings = diagnosticValues(current.diagnostics, diagnosticWarning)

		result := updateDoneMsg{
			generation: generation,
			snapshot:   current,
			stage:      updateStageMaterialization,
		}
		switch {
		case len(syncResult.CatalogTrustErrors) > 0:
			result.stage = updateStageRefresh
			result.err = fmt.Errorf("update %s failed: %w", result.stage, errors.Join(syncResult.CatalogTrustErrors...))
		case len(syncResult.MaterializeErrors) > 0:
			result.err = fmt.Errorf("update %s failed: %w", result.stage, errors.Join(syncResult.MaterializeErrors...))
		case len(diagnosticErrors(current.diagnostics)) > 0:
			result.stage = updateStageReload
			result.err = fmt.Errorf("update %s failed: %w", result.stage, errors.Join(diagnosticErrors(current.diagnostics)...))
		}
		return result
	}
}

// promoteReportErrors turns a materialization report's ERROR diagnostics into
// error values, through the same promotion `sync` uses, and collapses repeats
// the same way.
//
// Two bugs used to live in the four lines this replaces (bead
// agent-marketplace-oc2r, adversarial review):
//
//   - It flattened the diagnostic with fmt.Errorf("%s: %s", d.Code, d.Message),
//     which is exactly the anonymous, resource-less, hint-less line the bead
//     removed from `sync`. `update` still showed three byte-identical lines for
//     three different failing skills, and deduplicated nothing.
//   - It selected diagnostics with `d.Severity >= diag.SeverityError`.
//     diag.Severity is an iota with SeverityError = 0, so that predicate is
//     `>= 0`: every warning and every info was promoted to an error and failed
//     the update. A `rule.devin.unsupported` warning — which AGENTS.md says
//     must never fail a run — failed the TUI's update.
func promoteReportErrors(report *materialize.Report) []error {
	if report == nil {
		return nil
	}
	var errs []error
	for _, d := range report.Diags {
		if !d.IsError() {
			continue
		}
		errs = append(errs, pipeline.PromoteDiagnostic(d))
	}
	return pipeline.DedupIssues(errs)
}

// collectionNameFromVia extracts the collection name from a Via annotation.
// Via format: "collection:<name>" or "collection:<name>@<version>" or
// "collection:<name> > collection:<name2>" (nested).
func collectionNameFromVia(via string) string {
	via = strings.TrimSpace(via)
	// Take only the first collection in the chain (the top-level one).
	if idx := strings.Index(via, " > "); idx >= 0 {
		via = via[:idx]
	}
	const prefix = "collection:"
	if !strings.HasPrefix(via, prefix) {
		return via
	}
	name := via[len(prefix):]
	// Strip version suffix.
	if atIdx := strings.Index(name, "@"); atIdx >= 0 {
		name = name[:atIdx]
	}
	return name
}
