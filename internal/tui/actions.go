package tui

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

type snapshotStage uint8

const (
	snapshotStageFinal snapshotStage = iota
	snapshotStageConfig
	snapshotStageInventory
)

type snapshotLoadedMsg struct {
	generation           requestGeneration
	stage                snapshotStage
	stages               <-chan snapshotLoadedMsg
	rows                 []row
	driftReport          materialize.DriftReport
	driftAssessment      driftAssessment
	catalogs             map[string][]catalog.Entry
	resolved             []resolver.ResolvedArtifact
	mergedCfg            *config.Config
	provisionalCfg       *config.Config
	provisionalRows      []row
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

// toastStackMax bounds the visible toast stack so transient notices can
// never grow unbounded or bury required controls.
const toastStackMax = 3

type queuedToast struct {
	text string
	exp  time.Time
}

// pushToast adds a toast to the visible stack, keeping the newest
// toastStackMax entries, and mirrors the newest one for the footer segment.
func (m *Model) pushToast(text string, duration time.Duration) {
	exp := time.Now().Add(duration)
	m.toast = text
	m.toastExp = exp
	m.toastQueue = append(m.toastQueue, queuedToast{text: text, exp: exp})
	if len(m.toastQueue) > toastStackMax {
		m.toastQueue = append([]queuedToast(nil), m.toastQueue[len(m.toastQueue)-toastStackMax:]...)
	}
}

// enqueueToast pushes text onto the visible toast stack and schedules its
// expiry tick.
func (m *Model) enqueueToast(text string, duration time.Duration) tea.Cmd {
	m.pushToast(text, duration)
	return toastExpiryCmd(duration)
}

// visibleToasts returns the live toast stack, oldest first. A footer toast
// set outside pushToast (legacy direct write) still renders as a
// single-entry stack.
func (m Model) visibleToasts() []queuedToast {
	now := time.Now()
	var out []queuedToast
	for _, t := range m.toastQueue {
		if now.Before(t.exp) {
			out = append(out, t)
		}
	}
	if len(out) == 0 && m.toast != "" && now.Before(m.toastExp) {
		out = append(out, queuedToast{text: m.toast, exp: m.toastExp})
	}
	return out
}

// layeredToasts returns the toasts rendered in the top-right layer. The
// footer already mirrors the newest toast, so the layer carries only the
// remaining stack and each notification appears exactly once. The error
// footer renders no toast, so there the whole stack goes to the layer.
func (m Model) layeredToasts() []queuedToast {
	toasts := m.visibleToasts()
	if len(m.errs) > 0 {
		return toasts
	}
	if len(toasts) <= 1 {
		return nil
	}
	return toasts[:len(toasts)-1]
}

// expireToast drops every toast whose timer elapsed, mirrors the newest
// survivor for the footer, and re-arms a tick for the next expiry.
func (m *Model) expireToast() tea.Cmd {
	now := time.Now()
	kept := m.toastQueue[:0]
	for _, t := range m.toastQueue {
		if now.Before(t.exp) {
			kept = append(kept, t)
		}
	}
	m.toastQueue = kept
	if !m.toastExp.IsZero() && !now.Before(m.toastExp) {
		m.toast = ""
		m.toastExp = time.Time{}
	}
	if len(m.toastQueue) == 0 {
		return nil
	}
	newest := m.toastQueue[len(m.toastQueue)-1]
	m.toast = newest.text
	m.toastExp = newest.exp
	return toastExpiryCmd(time.Until(m.toastQueue[0].exp))
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
	err      error
	warnings []error
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
	warnings   []error
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
	loadLocalConfig       = loadLocalConfigAt
	cloneOrPull           = cache.CloneOrPull
	discoverCatalog       = catalog.Discover
	runMaterialize        = materialize.Run
	runPipelineSync       = pipeline.Sync
	readSnapshotForUpdate = readSnapshotForWorkspace
)

type snapshotStreamClosedMsg struct{}

func loadSnapshot(ctx context.Context, workspace pipeline.Workspace, homeDir string, generation requestGeneration) tea.Cmd {
	return func() tea.Msg {
		snapshot := readSnapshotForWorkspace(ctx, workspace, homeDir, catalogLoadCached)
		snapshot.generation = generation
		return snapshot
	}
}

// loadSnapshotProgressive schedules the staged startup snapshot: a validated
// config stage (which starts cached Marketplace discovery), a verified
// inventory stage, and the final assessed snapshot, all under one generation.
func loadSnapshotProgressive(ctx context.Context, workspace pipeline.Workspace, homeDir string, generation requestGeneration) tea.Cmd {
	return waitSnapshotStages(readSnapshotStagesForWorkspace(ctx, workspace, homeDir, catalogLoadCached, generation))
}

func waitSnapshotStages(stages <-chan snapshotLoadedMsg) tea.Cmd {
	if stages == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-stages
		if !ok {
			return snapshotStreamClosedMsg{}
		}
		return msg
	}
}

func refreshSnapshot(ctx context.Context, workspace pipeline.Workspace, homeDir string, generation requestGeneration) tea.Cmd {
	return func() tea.Msg {
		snapshot := readSnapshotForWorkspace(ctx, workspace, homeDir, catalogLoadRefresh)
		snapshot.generation = generation
		return refreshLoadedMsg{snapshotLoadedMsg: snapshot}
	}
}

// readSnapshotStages runs one snapshot read and delivers its progressive
// stages on the returned channel: validated config, verified inventory, and
// the assessed result. Every delivery shares the caller's generation so stale
// streams are rejected by the normal request acceptance path. The channel is
// buffered past the maximum number of stages, so a reader that stops
// consuming after a superseded generation cannot block the writer.
func readSnapshotStagesForWorkspace(ctx context.Context, ws pipeline.Workspace, homeDir string, mode catalogLoadMode, generation requestGeneration) <-chan snapshotLoadedMsg {
	stages := make(chan snapshotLoadedMsg, 5)
	go func() {
		defer close(stages)
		emit := func(msg snapshotLoadedMsg) {
			msg.generation = generation
			msg.stages = stages
			stages <- msg
		}
		readSnapshotStaged(ctx, ws, homeDir, mode, true, emit)
	}()
	return stages
}

func readSnapshotStages(ctx context.Context, repoRoot, homeDir string, mode catalogLoadMode, generation requestGeneration) <-chan snapshotLoadedMsg {
	return readSnapshotStagesForWorkspace(ctx, defaultTUIWorkspace(repoRoot), homeDir, mode, generation)
}

func loadLocalConfigAt(localDir string) (*config.LocalConfig, error) {
	if config.IsHomeDir(localDir) {
		return nil, nil
	}
	return config.LoadLocalConfigFromPath(config.LocalConfigPath(localDir))
}

func validatedTUIWorkspace(ws pipeline.Workspace) (pipeline.Workspace, error) {
	return pipeline.NewWorkspace(ws.RepoRoot(), ws.LocalConfigDir())
}

func defaultTUIWorkspace(repoRoot string) pipeline.Workspace {
	ws, err := pipeline.NewWorkspace(repoRoot, "")
	if err != nil {
		return pipeline.Workspace{}
	}
	return ws
}

func invalidWorkspaceSnapshot(err error) snapshotLoadedMsg {
	diagnostics := appendSnapshotDiagnostics(nil, diagnosticError, diagnosticResolution, err)
	return snapshotLoadedMsg{
		driftAssessment: driftFailed,
		diagnostics:     diagnostics,
		errs:            diagnosticValues(diagnostics, diagnosticError),
	}
}

func readSnapshotForWorkspace(ctx context.Context, ws pipeline.Workspace, homeDir string, mode catalogLoadMode) snapshotLoadedMsg {
	var snapshot snapshotLoadedMsg
	readSnapshotStaged(ctx, ws, homeDir, mode, false, func(msg snapshotLoadedMsg) { snapshot = msg })
	return snapshot
}

func readSnapshotStaged(ctx context.Context, ws pipeline.Workspace, homeDir string, mode catalogLoadMode, progressive bool, emit func(snapshotLoadedMsg)) {
	ws, err := validatedTUIWorkspace(ws)
	if err != nil {
		emit(invalidWorkspaceSnapshot(err))
		return
	}
	repoRoot := ws.RepoRoot()
	makeBase := func(inspection pipeline.InspectionResult) snapshotLoadedMsg {
		resolvedResult := inspection.ResolveResult
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
			pendingMigration:     pendingMigrationFor(repoRoot, ws.LocalConfigDir(), homeDir),
			driftAssessment:      driftFailed,
			refreshAfterLoad:     resolvedResult.CatalogLoad.RefreshAfterLoad,
		}
		if resolvedResult.MergedCfg != nil {
			base.configuredRegistries = append([]config.RegistryEntry(nil), resolvedResult.MergedCfg.Registries...)
		}
		for _, issue := range inspection.Issues {
			severity := diagnosticWarning
			if issue.Severity == pipeline.IssueError {
				severity = diagnosticError
			}
			base.diagnostics = appendSnapshotDiagnostics(base.diagnostics, severity, diagnosticCategoryFor(issue.Category), issue.Err)
		}
		base.diagnostics = deduplicateDiagnostics(base.diagnostics)
		base.errs = diagnosticValues(base.diagnostics, diagnosticError)
		base.warnings = diagnosticValues(base.diagnostics, diagnosticWarning)
		return base
	}
	var inventoryRows []row
	var unavailable bool
	inspectOptions := pipeline.InspectOptions{
		Resolve: pipeline.ResolveOptions{
			Workspace:     ws,
			CatalogMode:   tuiCatalogLoadMode(mode),
			SkipInventory: true,
			EffectiveOpts: config.LoadEffectiveOptions{
				OrganizationPath: config.OrganizationConfigPath(),
				GlobalPath:       filepath.Join(homeDir, ".agents", "config.yaml"),
				RepoPath:         config.ProjectConfigPath(repoRoot),
				LocalPath:        config.LocalConfigPath(ws.LocalConfigDir()),
			},
			Loaders: pipeline.Loaders{
				Org:    loadOrgConfig,
				Global: loadGlobalConfig,
				Repo:   loadRepoConfig,
				Local: func() (*config.LocalConfig, error) {
					return loadLocalConfig(ws.LocalConfigDir())
				},
			},
			CatalogOption: []catalog.LoadOption{
				catalog.WithCloneOrPull(cloneOrPull),
				catalog.WithDiscoverFunc(discoverCatalog),
			},
			ReadOnlyExactLocks:        true,
			AllowMissingReadOnlyLocks: true,
		},
		Resolved: func(result pipeline.InspectionResult) {
			resolved := result.ResolveResult
			recoveryCfg := resolved.LoadedCfg
			for _, err := range resolved.CatalogErrors {
				if catalog.IsOverlapWarning(err) {
					continue
				}
				if resolved.MergedCfg != nil {
					recoveryCfg = resolved.MergedCfg
				}
				break
			}
			// The verified inventory is published before detail capture and the
			// materialization dry run: rows are navigable and config-only toggles are
			// eligible while drift stays explicitly pending.
			inventoryRows = buildRows(resolved.LoadedCfg, resolved.CatalogLoad.Catalogs, resolved.Resolved, resolved.LocalCfg, materialize.DriftReport{}, harnessDefaultsBySource(resolved.GlobalCfg, resolved.RepoCfg))
			inventoryRows = appendUnavailableSelectionRows(inventoryRows, recoveryCfg, resolved.LocalCfg, resolved.ResolveErrors, resolved.CatalogLoad.Catalogs)
			unavailable = rowsHaveUnavailable(inventoryRows)
			inventory := makeBase(result)
			inventory.stage = snapshotStageInventory
			inventory.rows = inventoryRows
			inventory.driftAssessment = driftUnknown
			inventory.inventoryResult = nil // the assessed stage owns inventory telemetry
			emit(inventory)
		},
		DryRun: runMaterialize,
	}
	if progressive {
		inspectOptions.Resolve.BrowsingConfigReady = func(merged *config.Config) {
			emit(snapshotLoadedMsg{stage: snapshotStageConfig, mergedCfg: merged})
		}
		inspectOptions.Resolve.ProvisionalConfigReady = func(merged *config.Config, local *config.LocalConfig) {
			emit(snapshotLoadedMsg{stage: snapshotStageConfig, provisionalCfg: merged, provisionalRows: buildProvisionalRows(withCachedCollectionLeaves(merged, marketplace.DefaultCache()), local, provisionalLocalCatalogs(merged, ws), provisionalHarnessDefaults())})
		}
		inspectOptions.Resolve.ConfigReady = func(merged *config.Config) {
			emit(snapshotLoadedMsg{stage: snapshotStageConfig, mergedCfg: merged})
		}
	}
	inspection, releaseExactLease := pipeline.Inspect(ctx, inspectOptions)
	defer releaseExactLease()
	resolvedResult := inspection.ResolveResult

	base := makeBase(inspection)

	if len(resolvedResult.ConfigErrors) > 0 {
		emit(base)
		return
	}

	var hardCatalogFailure bool
	for _, err := range resolvedResult.CatalogErrors {
		if !catalog.IsOverlapWarning(err) {
			hardCatalogFailure = true
			break
		}
	}

	// Any generic resolve failure — a zero Workspace, context cancellation,
	// an exact-lock acquisition error — must surface diagnostics and return
	// here, before detail capture or dry-run materialization can act on an
	// invalid root and mask the original error.
	if len(resolvedResult.Errors) > 0 && len(resolvedResult.CatalogErrors) == 0 && len(resolvedResult.ResolveErrors) == 0 {
		emit(base)
		return
	}

	var resolvedNames []string
	// Copy detail bytes before any downstream work can yield long enough for a
	// waiting GC to run immediately after the resolution lease is released.
	detailSources := captureSnapshotDetailSources(resolvedResult.Resolved, resolvedResult.CatalogLoad.Catalogs)
	for _, resolved := range resolvedResult.Resolved {
		resolvedNames = append(resolvedNames, skill.ResourceKey(resolved.Kind, resolved.Name))
	}
	dryRunReport := inspection.Report
	driftReport := materialize.DriftReportFromReport(dryRunReport, resolvedNames)
	driftState := driftReady
	if hardCatalogFailure || unavailable || dryRunReport == nil || !inspection.Healthy() {
		driftState = driftFailed
	}
	base.rows = applyDriftItems(inventoryRows, driftReport)
	attachDetailSources(base.rows, detailSources)
	base.driftReport = driftReport
	base.driftAssessment = driftState
	emit(base)
}

func applyDriftItems(rows []row, driftReport materialize.DriftReport) []row {
	out := slices.Clone(rows)
	if len(driftReport.Missing)+len(driftReport.Broken)+len(driftReport.Orphaned) == 0 {
		return out
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
	for i := range out {
		if out[i].IsAvailable || out[i].DriftItem != nil {
			continue
		}
		item, hasDrift := driftByName[skill.ResourceKey(out[i].Artifact.Kind, out[i].Artifact.Name)]
		if hasDrift {
			out[i].DriftItem = &item
		}
	}
	return out
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

func configHarnesses(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	return append([]string(nil), cfg.Harnesses...)
}

// provisionalHarnessDefaults reads the layer harness defaults verified
// inventory uses, so pending rows render the same harness badges. A layer that
// fails to load contributes nothing; verification reports the failure.
func provisionalHarnessDefaults() map[config.Source][]string {
	globalCfg, _ := loadGlobalConfig()
	repoCfg, _ := loadRepoConfig()
	return harnessDefaultsBySource(globalCfg, repoCfg)
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

	layerHarnesses := layerHarnessDefaults(cfg, harnessDefaults...)

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

	forEachUserExcludedOrganizationPin(cfg, func(pin config.MarketplaceResourcePin, excluded row) {
		seenKey := skill.ResourceKey(excluded.Artifact.Kind, excluded.Artifact.Name) + "\x01" + excluded.Artifact.CatalogKey
		if _, exists := seen[seenKey]; exists {
			return
		}
		seen[seenKey] = struct{}{}
		excluded.MarketplaceSelections = marketplaceSelectionsByArtifact[marketplaceSelectionKey(excluded.Artifact.CatalogKey, pin.Kind, excluded.Artifact.Name)]
		excluded.MarketplaceCollectionOnly = marketplaceCollectionOnly(excluded.MarketplaceSelections)
		rows = append(rows, excluded)
	})

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

// forEachUserExcludedOrganizationPin visits optional organization Marketplace
// leaves suppressed by a user exclusion. Both startup stages render them as
// unchecked Global rows so verification does not insert them mid-list.
func forEachUserExcludedOrganizationPin(cfg *config.Config, visit func(config.MarketplaceResourcePin, row)) {
	if cfg == nil || len(cfg.MarketplaceExclusions) == 0 {
		return
	}
	for _, entry := range cfg.Marketplaces {
		if entry.Source != config.Organization || entry.Required {
			continue
		}
		for _, pin := range entry.Resources {
			kind, ok := mpapi.ToIR(pin.Kind)
			if !ok || kind == ir.KindCollection || !slices.ContainsFunc(cfg.MarketplaceExclusions, func(exclusion config.MarketplaceExclusion) bool {
				return exclusion.Source == config.UserPersonal && exclusion.Matches(entry, pin)
			}) {
				continue
			}
			name := marketplaceInstalledName(pin)
			visit(pin, row{
				Artifact: resolver.ResolvedArtifact{
					Entry: catalog.Entry{Kind: kind, Name: name}, RegistryURL: entry.URL,
					RegistryPath: pin.RegistryID, Ref: pin.Revision, Source: entry.Source,
					CatalogKey: marketplaceCatalogKey(entry, pin), IsMarketplace: true, MarketplaceURL: entry.URL,
					MarketplaceInstanceID: entry.InstanceID, MarketplaceRegistryID: pin.RegistryID, MarketplaceResourceName: pin.Name,
				},
				IsAvailable: true, IsExcluded: true, ExcludedByUser: true, EnabledGlobal: true,
				RegistryURL: entry.URL, RegistryName: registryDisplayName(entry.URL),
			})
		}
	}
}

// withCachedCollectionLeaves returns cfg with each Marketplace collection pin
// whose leaves are not pinned yet expanded from its locally verified cached
// manifest, so pending rows take the positions verified inventory will give
// them before acquisition. It never fetches, and the copy only feeds
// provisional rows.
func withCachedCollectionLeaves(cfg *config.Config, cache *marketplace.Cache) *config.Config {
	if cfg == nil {
		return nil
	}
	out := cfg
	for i, entry := range cfg.Marketplaces {
		pinned := make(map[string]struct{})
		for _, pin := range entry.Resources {
			for _, selection := range pin.SelectedBy.List() {
				pinned[selection.RegistryID+"\x00"+selection.Name] = struct{}{}
			}
		}
		var leaves []config.MarketplaceResourcePin
		for _, pin := range entry.Resources {
			if _, ok := pinned[pin.RegistryID+"\x00"+pin.Name]; ok || pin.Kind != mpapi.KindCollection {
				continue
			}
			members, ok := cache.CachedCollectionLeaves(entry.URL, pin.RegistryID, pin.Name, pin.Revision)
			if !ok {
				continue
			}
			selectedBy := config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{RegistryID: pin.RegistryID, Name: pin.Name, Revision: pin.Revision}})
			for _, member := range members {
				slug := member.RegistryID
				if member.RegistryID == pin.RegistryID && pin.RegistrySlug != "" {
					slug = pin.RegistrySlug
				}
				leaves = append(leaves, config.MarketplaceResourcePin{
					RegistryID: member.RegistryID, RegistrySlug: slug, Kind: member.Kind, Name: member.Name,
					Revision: member.Revision, Digest: member.Revision, Acquisition: config.MarketplaceAcquisitionMirror, SelectedBy: selectedBy,
				})
			}
		}
		if len(leaves) == 0 {
			continue
		}
		if out == cfg {
			clone := *cfg
			clone.Marketplaces = slices.Clone(cfg.Marketplaces)
			out = &clone
		}
		out.Marketplaces[i].Resources = append(slices.Clone(entry.Resources), leaves...)
	}
	return out
}

type provisionalLocalCatalog struct {
	entry   config.RegistryEntry
	entries []catalog.Entry
}

func provisionalLocalCatalogs(cfg *config.Config, workspace pipeline.Workspace) map[int]provisionalLocalCatalog {
	if cfg == nil {
		return nil
	}
	var indexes []int
	localOnly := &config.Config{}
	for i := range cfg.Registries {
		if cfg.Registries[i].IsLocal() {
			indexes = append(indexes, i)
			localOnly.Registries = append(localOnly.Registries, cfg.Registries[i])
		}
	}
	if len(indexes) == 0 {
		return nil
	}
	result := catalog.LoadRegistries(localOnly, workspace.RepoRoot(), catalog.LoadModeCachedNoFetch, catalog.WithLocalConfigDir(workspace.LocalConfigDir()), catalog.WithDiscoverFunc(discoverCatalog))
	catalogs := make(map[int]provisionalLocalCatalog, len(indexes))
	for pos, idx := range indexes {
		entry := localOnly.Registries[pos]
		catalogs[idx] = provisionalLocalCatalog{entry: entry, entries: result.Catalogs[entry.CatalogKey].Entries}
	}
	return catalogs
}

func provisionalLocallyExcluded(localCfg *config.LocalConfig, kind ir.ResourceKind, name string, entry config.RegistryEntry) bool {
	if localCfg == nil || entry.Required || entry.Source.IsMachineScope() {
		return false
	}
	selector := localCfg.SelectorForKind(kind)
	if selector.ExcludeAll {
		return true
	}
	trimmed := strings.TrimSpace(name)
	if slices.Contains(selector.Exclude, trimmed) {
		return true
	}
	entryIdentity := registryIdentityKey(entry.URL, entry.Ref, entry.Path)
	for i := range localCfg.Registries {
		re := &localCfg.Registries[i]
		scoped := re.SelectorForKind(kind)
		if !scoped.ExcludeAll && !slices.Contains(scoped.Exclude, trimmed) {
			continue
		}
		identity := registryIdentityKey(re.URL, re.Ref, re.Path)
		if identity == entryIdentity {
			return true
		}
	}
	return false
}

func buildProvisionalRows(cfg *config.Config, localCfg *config.LocalConfig, locals map[int]provisionalLocalCatalog, harnessDefaults ...map[config.Source][]string) []row {
	if cfg == nil {
		return nil
	}
	layerHarnesses := layerHarnessDefaults(cfg, harnessDefaults...)
	seen := make(map[string]int)
	var rows []row
	add := func(key string, r row) {
		i, dup := seen[key]
		if !dup {
			i = len(rows)
			seen[key] = i
			rows = append(rows, r)
		}
		if r.IsAvailable {
			return
		}
		switch r.Artifact.Source {
		case config.Organization, config.UserPersonal:
			rows[i].EnabledGlobal = true
		case config.RepoCommitted:
			rows[i].EnabledProject = true
		case config.RepoPersonal:
			rows[i].EnabledLocal = true
		}
	}
	for i := range cfg.Registries {
		entry := cfg.Registries[i]
		var catalogEntries []catalog.Entry
		if entry.IsLocal() {
			if loaded, ok := locals[i]; ok {
				entry = loaded.entry
				catalogEntries = loaded.entries
			}
		}
		for _, selected := range resolver.ResourcesSelected(entry, catalogEntries) {
			if provisionalLocallyExcluded(localCfg, selected.Kind, selected.Name, entry) {
				continue
			}
			add(selected.Kind.String()+"\x00"+selected.Name+"\x00"+entry.CatalogKey, row{
				Artifact: resolver.ResolvedArtifact{
					Entry:        catalog.Entry{Kind: selected.Kind, Name: selected.Name, SourceHarness: entry.SourceHarness},
					RegistryURL:  entry.URL,
					RegistryPath: entry.Path,
					Ref:          entry.Ref,
					V4Kind:       entry.V4Kind,
					Source:       entry.Source,
					Required:     entry.Required,
					CatalogKey:   entry.CatalogKey,
				},
				// Verified inventory expands collections into members and
				// lists the collection itself as available.
				IsAvailable:         selected.Kind == ir.KindCollection,
				PendingVerification: true,
				RegistryURL:         entry.URL,
				RegistryName:        registryDisplayName(entry.URL),
				RegistryPath:        entry.Path,
				RegistryRef:         entry.Ref,
				Harnesses:           slices.Clone(entry.Harnesses),
				HarnessSource:       harnessSourceLabel(entry, layerHarnesses[entry.Source]),
			})
		}
	}
	for i := range cfg.Marketplaces {
		entry := &cfg.Marketplaces[i]
		for _, pin := range entry.Resources {
			if cfg.MarketplacePinExcluded(*entry, pin) {
				continue
			}
			kind, ok := mpapi.ToIR(pin.Kind)
			if !ok || kind == ir.KindCollection {
				continue
			}
			name := marketplaceInstalledName(pin)
			catalogKey := marketplaceCatalogKey(*entry, pin)
			add(marketplaceSelectionKey(catalogKey, pin.Kind, name), row{
				Artifact: resolver.ResolvedArtifact{
					Entry:                   catalog.Entry{Kind: kind, Name: name},
					RegistryURL:             entry.URL,
					RegistryPath:            pin.RegistryID,
					Ref:                     pin.Revision,
					Source:                  entry.Source,
					Required:                entry.Required,
					CatalogKey:              catalogKey,
					IsMarketplace:           true,
					MarketplaceURL:          entry.URL,
					MarketplaceInstanceID:   entry.InstanceID,
					MarketplaceAlias:        entry.Alias,
					MarketplaceRegistryID:   pin.RegistryID,
					MarketplaceResourceName: pin.Name,
				},
				PendingVerification:       true,
				MarketplaceCollectionOnly: !pin.Direct && pin.SelectedBy != "",
				RegistryURL:               entry.URL,
				RegistryName:              registryDisplayName(entry.URL),
			})
		}
	}
	forEachUserExcludedOrganizationPin(cfg, func(pin config.MarketplaceResourcePin, excluded row) {
		excluded.PendingVerification = true
		add(marketplaceSelectionKey(excluded.Artifact.CatalogKey, pin.Kind, excluded.Artifact.Name), excluded)
	})
	return withoutShadowedCollectionMembers(rows)
}

// withoutShadowedCollectionMembers drops enabled, optional collection-only
// members whose resource name another enabled selection already provides;
// verified inventory resolves that collision to the configured selection.
func withoutShadowedCollectionMembers(rows []row) []row {
	configured := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		if !r.IsAvailable && !r.MarketplaceCollectionOnly {
			configured[skill.ResourceKey(r.Artifact.Kind, r.Artifact.Name)] = struct{}{}
		}
	}
	return slices.DeleteFunc(rows, func(r row) bool {
		_, shadowed := configured[skill.ResourceKey(r.Artifact.Kind, r.Artifact.Name)]
		return shadowed && !r.IsAvailable && !r.Artifact.Required && r.MarketplaceCollectionOnly
	})
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
	appendCandidate := func(kind ir.ResourceKind, name, url, ref, path string, required bool, reason string, soft bool) {
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
			UnavailableSoft:   soft,
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
		appendCandidate(snf.Kind, snf.Name, snf.URL, snf.Ref, snf.Path, snf.Required, snf.Error(), resolver.IsSoftSelectedNotFound(err))
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
				appendCandidate(selected.Kind, selected.Name, entry.URL, entry.Ref, entry.Path, entry.Required, unavailableCatalogReason, false)
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
					appendCandidate(kind, name, entry.URL, entry.Ref, entry.Path, entry.Required, unavailableCatalogReason, false)
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

// layerHarnessDefaults returns each layer's default harness list, taken from
// its first registry entry unless an explicit layer default overrides it.
func layerHarnessDefaults(cfg *config.Config, overrides ...map[config.Source][]string) map[config.Source][]string {
	defaults := make(map[config.Source][]string)
	if cfg != nil {
		for _, entry := range cfg.Registries {
			if _, ok := defaults[entry.Source]; ok {
				continue
			}
			defaults[entry.Source] = append([]string(nil), entry.Harnesses...)
		}
	}
	for _, override := range overrides {
		for source, harnesses := range override {
			defaults[source] = append([]string(nil), harnesses...)
		}
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
func loadDetailContent(t theme, r row, generation requestGeneration, width int, keepScroll bool) tea.Cmd {
	return func() tea.Msg {
		return detailContentMsg{
			generation: generation,
			content:    renderDetailFullContent(t, r, width),
			width:      width,
			keepScroll: keepScroll,
		}
	}
}

func tuiPipelineOptionsForWorkspace(ws pipeline.Workspace) pipeline.ResolveOptions {
	return pipeline.ResolveOptions{
		Workspace: ws,
		Loaders: pipeline.Loaders{
			Org:    loadOrgConfig,
			Global: loadGlobalConfig,
			Repo:   loadRepoConfig,
			Local: func() (*config.LocalConfig, error) {
				return loadLocalConfig(ws.LocalConfigDir())
			},
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
var runSyncCmd = func(ctx context.Context, workspace pipeline.Workspace) tea.Cmd {
	return func() tea.Msg {
		ws, err := validatedTUIWorkspace(workspace)
		if err != nil {
			return syncDoneMsg{err: err}
		}
		result := runPipelineSync(ctx, tuiPipelineOptionsForWorkspace(ws))
		return syncDoneMsg{err: errors.Join(result.Errors...)}
	}
}

func runDependencySyncCmd(ctx context.Context, workspace pipeline.Workspace, sync DependencyLifecycleFunc) tea.Cmd {
	return func() tea.Msg {
		ws, err := validatedTUIWorkspace(workspace)
		if err != nil {
			return syncDoneMsg{err: err}
		}
		warnings, syncErr := sync(ctx, ws)
		return syncDoneMsg{err: syncErr, warnings: warnings}
	}
}

func runDependencyUpdateCmd(ctx context.Context, workspace pipeline.Workspace, homeDir string, generation requestGeneration, update DependencyLifecycleFunc) tea.Cmd {
	return func() tea.Msg {
		ws, err := validatedTUIWorkspace(workspace)
		if err != nil {
			return updateDoneMsg{generation: generation, stage: updateStageRefresh, err: err}
		}
		warnings, lifecycleErr := update(ctx, ws)
		pipeline.ObservePhase(ctx, pipeline.PhaseReload)
		current := readSnapshotForUpdate(ctx, ws, homeDir, catalogLoadCached)
		current.generation = generation
		for _, warning := range warnings {
			current.diagnostics = appendSnapshotDiagnostics(current.diagnostics, diagnosticWarning, diagnosticUpdate, warning)
		}
		current.diagnostics = deduplicateDiagnostics(current.diagnostics)
		current.errs = diagnosticValues(current.diagnostics, diagnosticError)
		current.warnings = diagnosticValues(current.diagnostics, diagnosticWarning)
		result := updateDoneMsg{generation: generation, snapshot: current, stage: updateStageRefresh, warnings: warnings}
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
	ctx := m.observedContext(ctxOrBackground(m.ctx))
	if m.dependencyLifecycle.Sync != nil {
		return runDependencySyncCmd(ctx, m.workspace, m.dependencyLifecycle.Sync)
	}
	return runSyncCmd(ctx, m.workspace)
}

func (m Model) dependencyUpdateCmd(generation requestGeneration) tea.Cmd {
	ctx := m.observedContext(ctxOrBackground(m.ctx))
	if m.dependencyLifecycle.Update != nil {
		return runDependencyUpdateCmd(ctx, m.workspace, m.homeDir, generation, m.dependencyLifecycle.Update)
	}
	return runUpdateCmdForWorkspace(ctx, m.workspace, m.homeDir, generation)
}

func runUpdateCmdForWorkspace(ctx context.Context, workspace pipeline.Workspace, homeDir string, generation requestGeneration) tea.Cmd {
	return func() tea.Msg {
		ws, err := validatedTUIWorkspace(workspace)
		if err != nil {
			return updateDoneMsg{generation: generation, stage: updateStageMaterialization, err: err}
		}
		syncResult := runPipelineSync(ctx, tuiPipelineOptionsForWorkspace(ws))
		pipeline.ObservePhase(ctx, pipeline.PhaseReload)
		current := readSnapshotForUpdate(ctx, ws, homeDir, catalogLoadCached)
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
