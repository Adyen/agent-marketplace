package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
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
	materializationModes config.MaterializationModes
	localExcludes        []string
	globalHarnesses      []string
	projectHarnesses     []string
	userHarnesses        []string
	targetHarnesses      []string
	targetHarnessSource  config.Source
	diagnostics          []tuiDiagnostic
	refreshAfterLoad     bool
	pendingMigration     migrationPrompt
}

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

// operationInProgressCmd surfaces the start-block of a second mutation-class
// operation while another is still active. The blocked action stays where it
// is (modal open) so it can be retried once the active operation completes.
func operationInProgressCmd() tea.Cmd {
	return func() tea.Msg {
		return toastMsg{text: "another operation is in progress", duration: 2 * time.Second}
	}
}

type configWrittenMsg struct {
	operation operationID
	skillName string
	enabled   bool
	err       error
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
	snapshot snapshotLoadedMsg
	stage    string
	// staleCaches carries the typed stale-cache fallback warnings observed
	// while registries were refreshed. Non-empty means the update completed
	// from stale data: a partial outcome that must never be reported with
	// the clean-refresh success copy.
	staleCaches []error
	err         error
}

// detailContentMsg carries the glamour-rendered source content for the
// full-screen detail view. Produced by loadDetailContent (async tea.Cmd)
// and consumed in Update to populate the viewport. width records the
// word-wrap width the content was rendered at so a resize can detect a
// stale-width render; keepScroll distinguishes a width re-render (keep the
// scroll anchor) from a fresh open (reset to the top).
type detailContentMsg struct {
	generation requestGeneration
	content    string
	width      int
	keepScroll bool
}

type catalogLoadMode int

const (
	catalogLoadCached catalogLoadMode = iota
	catalogLoadRefresh
)

var (
	loadOrgConfig    = config.LoadOrganizationConfig
	loadGlobalConfig = config.LoadGlobalConfig
	loadRepoConfig   = config.LoadRepoConfig
	loadLocalConfig  = config.LoadLocalConfig
	cloneOrPull      = cache.CloneOrPull
	discoverCatalog  = catalog.Discover
	runMaterialize   = materialize.Run
)

func loadSnapshot(ctx context.Context, repoRoot, homeDir string, generation requestGeneration) tea.Cmd {
	return func() tea.Msg {
		snapshot := readSnapshotWithContext(ctx, repoRoot, homeDir, catalogLoadCached)
		snapshot.generation = generation
		return snapshot
	}
}

func refreshSnapshot(ctx context.Context, repoRoot, homeDir string, generation requestGeneration) tea.Cmd {
	return func() tea.Msg {
		snapshot := readSnapshotWithContext(ctx, repoRoot, homeDir, catalogLoadRefresh)
		snapshot.generation = generation
		return refreshLoadedMsg{snapshotLoadedMsg: snapshot}
	}
}

func readSnapshot(repoRoot, homeDir string, mode catalogLoadMode) snapshotLoadedMsg {
	return readSnapshotWithContext(context.Background(), repoRoot, homeDir, mode)
}

func readSnapshotWithContext(ctx context.Context, repoRoot, homeDir string, mode catalogLoadMode) (result snapshotLoadedMsg) {
	started := time.Now()
	slog.Debug("tui snapshot started", "mode", mode)
	defer func() {
		slog.Debug("tui snapshot finished",
			"mode", mode,
			"duration_ms", time.Since(started).Milliseconds(),
			"rows", len(result.rows),
			"diagnostics", len(result.diagnostics),
			"drift_assessment", result.driftAssessment,
		)
	}()
	var catalogOverlapWarnings []error
	resolvedResult := pipeline.Resolve(ctx, pipeline.ResolveOptions{
		RepoRoot:    repoRoot,
		CatalogMode: tuiCatalogLoadMode(mode),
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
	})

	// v2 is canonical and v3 is a compatibility alias; schema versions do not
	// trigger a migration prompt in the TUI.
	pendingMigration := pendingMigrationFor(repoRoot, homeDir)
	snapshotConfig := resolvedResult.MergedCfg
	if len(resolvedResult.CatalogErrors) == 0 && resolvedResult.LoadedCfg != nil {
		snapshotConfig = resolvedResult.LoadedCfg
	}
	globalHarnesses := configHarnesses(resolvedResult.GlobalCfg)
	baseSnapshot := snapshotLoadedMsg{
		catalogs:             entriesFromCatalogs(resolvedResult.CatalogLoad.Catalogs),
		resolved:             resolvedResult.Resolved,
		mergedCfg:            snapshotConfig,
		materializationModes: resolvedResult.MaterializationModes,
		localExcludes:        localExcludes(resolvedResult.LocalCfg),
		globalHarnesses:      globalHarnesses,
		projectHarnesses:     configHarnesses(resolvedResult.RepoCfg),
		userHarnesses:        globalHarnesses,
		targetHarnesses:      targetsToHarnesses(resolvedResult.TargetHarnesses),
		targetHarnessSource:  resolvedResult.TargetHarnessSource,
		refreshAfterLoad:     resolvedResult.CatalogLoad.RefreshAfterLoad,
		pendingMigration:     pendingMigration,
	}

	if len(resolvedResult.ConfigErrors) > 0 {
		baseSnapshot.diagnostics = appendSnapshotDiagnostics(nil, diagnosticWarning, diagnosticConfig, resolvedResult.Warnings...)
		baseSnapshot.diagnostics = appendSnapshotDiagnostics(baseSnapshot.diagnostics, diagnosticError, diagnosticConfig, resolvedResult.ConfigErrors...)
		return baseSnapshot
	}
	if len(resolvedResult.CatalogErrors) > 0 {
		// Separate overlap warnings from hard errors.
		var hardErrs []error
		var overlapWarnings []error
		for _, err := range resolvedResult.CatalogErrors {
			if catalog.IsOverlapWarning(err) {
				overlapWarnings = append(overlapWarnings, err)
			} else {
				hardErrs = append(hardErrs, err)
			}
		}
		if len(hardErrs) > 0 {
			baseSnapshot.diagnostics = appendSnapshotDiagnostics(baseSnapshot.diagnostics, diagnosticWarning, diagnosticConfig, resolvedResult.Warnings...)
			baseSnapshot.diagnostics = appendSnapshotDiagnostics(baseSnapshot.diagnostics, diagnosticError, diagnosticCatalog, hardErrs...)
			baseSnapshot.diagnostics = appendSnapshotDiagnostics(baseSnapshot.diagnostics, diagnosticWarning, diagnosticCatalog, overlapWarnings...)
			return baseSnapshot
		}
		// No hard errors; carry overlap warnings into the accepted snapshot diagnostics.
		catalogOverlapWarnings = overlapWarnings
	}

	// Use materialize dry-run to compute drift instead of drift.Compute.
	var resolvedNames []string
	for _, r := range resolvedResult.Resolved {
		resolvedNames = append(resolvedNames, skill.ResourceKey(r.Kind, r.Name))
	}
	dryRunReport, dryRunErr := runMaterialize(ctx, repoRoot, resolvedResult.Resolved, materialize.WithDryRun(), materialize.WithMaterializationModes(resolvedResult.MaterializationModes))
	driftReport := materialize.DriftReportFromReport(dryRunReport, resolvedNames)
	driftState := driftReady
	if dryRunReport == nil || dryRunErr != nil {
		driftState = driftFailed
	}
	// Any hard resolution error (required selection not found, unresolved or
	// malformed collection, incompatible organization requirement) means the
	// resolved set is incomplete, so a dry-run verdict over it is not a
	// trustworthy drift signal: drift is failed, never "in sync".
	for _, err := range resolvedResult.ResolveErrors {
		if !isSoftResolutionIssue(err) {
			driftState = driftFailed
			break
		}
	}

	rows := buildRows(snapshotConfig, resolvedResult.CatalogLoad.Catalogs, resolvedResult.Resolved, resolvedResult.LocalCfg, driftReport, harnessDefaultsBySource(resolvedResult.GlobalCfg, resolvedResult.RepoCfg))

	var diagnostics []tuiDiagnostic
	diagnostics = appendSnapshotDiagnostics(diagnostics, diagnosticWarning, diagnosticConfig, resolvedResult.Warnings...)
	diagnostics = appendSnapshotDiagnostics(diagnostics, diagnosticWarning, diagnosticCatalog, catalogOverlapWarnings...)
	for _, malformed := range resolvedResult.CatalogLoad.Malformed {
		diagnostics = appendSnapshotDiagnostics(diagnostics, diagnosticWarning, diagnosticCatalog, fmt.Errorf("malformed skill %q (%s): %w", malformed.DirName, malformed.Path, malformed.Error))
	}
	for _, err := range resolvedResult.ResolveErrors {
		if isSoftResolutionIssue(err) {
			diagnostics = appendSnapshotDiagnostics(diagnostics, diagnosticWarning, diagnosticResolution, err)
			continue
		}
		diagnostics = appendSnapshotDiagnostics(diagnostics, diagnosticError, diagnosticResolution, err)
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
	if driftState == driftFailed {
		slog.Error("tui drift assessment failed", "returned_error", dryRunErr != nil, "diagnostic_errors", len(diagnosticErrors(diagnostics)))
	}

	return snapshotLoadedMsg{
		rows:                 rows,
		driftReport:          driftReport,
		driftAssessment:      driftState,
		catalogs:             entriesFromCatalogs(resolvedResult.CatalogLoad.Catalogs),
		resolved:             resolvedResult.Resolved,
		mergedCfg:            snapshotConfig,
		materializationModes: resolvedResult.MaterializationModes,
		localExcludes:        localExcludes(resolvedResult.LocalCfg),
		globalHarnesses:      globalHarnesses,
		projectHarnesses:     configHarnesses(resolvedResult.RepoCfg),
		userHarnesses:        globalHarnesses,
		targetHarnesses:      targetsToHarnesses(resolvedResult.TargetHarnesses),
		targetHarnessSource:  resolvedResult.TargetHarnessSource,
		diagnostics:          diagnostics,
		refreshAfterLoad:     resolvedResult.CatalogLoad.RefreshAfterLoad,
		pendingMigration:     pendingMigration,
	}
}

// isSoftResolutionIssue reports whether a resolver error is advisory for the
// TUI: soft selected-not-found/selected-malformed, name collisions, and the
// resolver's warning family. Everything else (required selections that are
// missing or malformed, unresolved collections, incompatible organization
// requirements) is hard: it fails the drift assessment and is reported as an
// error diagnostic. This predicate is the single place that classification
// lives so the drift gate and the diagnostic severities cannot drift apart.
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
	return config.NormalizeGitURL(strings.TrimSpace(url)) + "\x01" + strings.TrimSpace(ref) + "\x01" + strings.TrimSpace(path)
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

	// Missing and broken drift attach to the resolved row for the resource.
	// Orphaned output deliberately does NOT: an orphan is unclaimed output
	// whose only identity is (resource, target, scope, outDir, path), so
	// keying it by resource name would both mis-mark a resolved row whose
	// resource merely shares the name and hide the output's own identity.
	driftByName := make(map[string]materialize.DriftItem, len(driftReport.Missing)+len(driftReport.Broken))
	for _, item := range driftReport.Missing {
		driftByName[item.Name] = item
	}
	for _, item := range driftReport.Broken {
		driftByName[item.Name] = item
	}

	// seen is keyed by (kind, name, catalogKey) so same-name skills from
	// different registries each get their own row.
	seen := make(map[string]struct{}, len(resolved)+totalCatalogEntries+totalMalformed)
	rows := make([]row, 0, len(resolved)+totalCatalogEntries+totalMalformed+len(driftReport.Orphaned))

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
			driftItem = new(materialize.DriftItem)
			*driftItem = item
		}

		_, enabledGlobal := globalEnabled[key][s.CatalogKey]
		_, enabledProject := projectEnabled[key][s.CatalogKey]
		enabledLocal := localScopedContains(localEnabled, localEnabledAny, key, s.CatalogKey)
		excludedLocally := localScopedContains(localExcluded, localExcludedAny, key, s.CatalogKey)

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
			Artifact:        s,
			IsAvailable:     false,
			IsExcluded:      excludedLocally,
			DriftItem:       driftItem,
			EnabledGlobal:   enabledGlobal,
			EnabledProject:  enabledProject,
			EnabledLocal:    enabledLocal,
			ExcludedLocally: excludedLocally,
			RegistryURL:     s.RegistryURL,
			RegistryName:    registryDisplayName(s.RegistryURL),
			RegistryPath:    registryEntryPathFor(entry),
			RegistryRef:     registryEntryRefFor(entry),
			Harnesses:       targetsToHarnesses(s.Harnesses),
			HarnessSource:   harnessSource,
		})
	}

	// Add available skills not in resolved, iterating per-registry so each
	// occurrence gets its correct registry URL.
	if cfg != nil {
		for i := range cfg.Registries {
			entry := &cfg.Registries[i]
			cat, ok := catalogs[entry.CatalogKey]
			if !ok {
				continue
			}
			for _, s := range cat.Entries {
				key := skill.ResourceKey(s.Kind, s.Name)
				seenKey := key + "\x01" + entry.CatalogKey
				if _, dup := seen[seenKey]; dup {
					continue
				}
				seen[seenKey] = struct{}{}

				excludedLocally := localScopedContains(localExcluded, localExcludedAny, skill.ResourceKey(s.Kind, s.Name), entry.CatalogKey)

				var harnessSource string
				if val, ok := harnessSourceBySeenKey[seenKey]; ok {
					harnessSource = val
				} else {
					harnessSource = harnessSourceByKeyURL[key+"\x01"+entry.URL]
				}

				rows = append(rows, row{
					Artifact: resolver.ResolvedArtifact{
						Entry:       s,
						RegistryURL: entry.URL,
						CatalogKey:  entry.CatalogKey,
					},
					IsAvailable:     true,
					IsExcluded:      excludedLocally,
					ExcludedLocally: excludedLocally,
					RegistryURL:     entry.URL,
					RegistryName:    registryDisplayName(entry.URL),
					RegistryPath:    registryEntryPathFor(entry),
					RegistryRef:     registryEntryRefFor(entry),
					Harnesses:       append([]string(nil), entry.Harnesses...),
					HarnessSource:   harnessSource,
				})
			}
		}
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

	// Every orphan output gets its own row, keyed by its full
	// (resource, target, scope, outDir, path) identity: an orphan is output
	// no resolved artifact claims, so it can never be "represented" by a
	// resolved or catalog row that merely shares its resource name, and two
	// orphans collapse only when every identity field agrees.
	seenOrphans := make(map[string]struct{}, len(driftReport.Orphaned))
	for _, orphan := range driftReport.Orphaned {
		identity := orphan.Name +
			"\x00" + string(orphan.Target) +
			"\x00" + fmt.Sprint(orphan.MachineScope) +
			"\x00" + orphan.OutDir +
			"\x00" + orphan.Path
		if _, dup := seenOrphans[identity]; dup {
			continue
		}
		seenOrphans[identity] = struct{}{}
		kind, name := orphanResourceIdentity(orphan.Name)
		orphanCopy := orphan
		rows = append(rows, row{
			Artifact:  resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: kind, Name: name, Path: orphan.Path}},
			DriftItem: &orphanCopy,
		})
	}

	return rows
}

func orphanResourceIdentity(key string) (ir.ResourceKind, string) {
	kindName, name, found := strings.Cut(key, "\x00")
	if found {
		if kind, ok := skill.ParseKind(kindName); ok {
			return kind, name
		}
	}
	return ir.KindSkill, key
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

// loadDetailContent reads (bounded) and glamour-renders a resource's source
// at the given viewport width in a background goroutine so the TUI can
// transition to the detail pane immediately without blocking on file I/O or
// markdown rendering. keepScroll marks width-change re-renders: on acceptance
// the viewport keeps its scroll anchor instead of resetting to the top.
func loadDetailContent(r row, generation requestGeneration, width int, keepScroll bool) tea.Cmd {
	return func() tea.Msg {
		return detailContentMsg{generation: generation, content: renderDetailFullContent(r, width), width: width, keepScroll: keepScroll}
	}
}

// cloneOrPullDetailed mirrors cache.CloneOrPullDetailed behind a package var
// so tests can swap the fetch behavior AND its stale-fallback reporting. The
// TUI update path consumes the typed warning; readSnapshot and sync keep the
// plain two-result seam.
var cloneOrPullDetailed = cache.CloneOrPullDetailed

// tuiPipelineOptions builds the pipeline options shared by the TUI's sync
// and update operations so both run the identical authoritative behavior:
// strict config loaders, the catalog fetch/discovery seams, and
// pipeline.Sync's refresh/gitignore/target-set semantics. A nil cloneFn
// falls back to the package's plain fetch seam.
func tuiPipelineOptions(repoRoot string, cloneFn catalog.CloneOrPullFunc) pipeline.ResolveOptions {
	if cloneFn == nil {
		cloneFn = cloneOrPull
	}
	return pipeline.ResolveOptions{
		RepoRoot: repoRoot,
		Loaders: pipeline.Loaders{
			Org:    loadOrgConfig,
			Global: loadGlobalConfig,
			Repo:   loadRepoConfig,
			Local:  loadLocalConfig,
		},
		CatalogOption: []catalog.LoadOption{
			catalog.WithCloneOrPull(cloneFn),
			catalog.WithDiscoverFunc(discoverCatalog),
		},
	}
}

// staleCacheCollector records the typed stale-cache fallbacks observed by
// the update's fetch seam. catalog runs fetches concurrently, so the
// collector is mutex-guarded; warnings keep first-observed order.
type staleCacheCollector struct {
	mu       sync.Mutex
	warnings []error
}

func (c *staleCacheCollector) add(warning *cache.StaleCacheFallbackWarning) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.warnings = append(c.warnings, warning)
}

func (c *staleCacheCollector) all() []error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]error(nil), c.warnings...)
}

// wrap adapts the detailed fetch seam to catalog.CloneOrPullFunc, collecting
// stale-cache fallbacks as they happen.
func (c *staleCacheCollector) wrap() catalog.CloneOrPullFunc {
	return func(url, ref string) (string, error) {
		path, warning, err := cloneOrPullDetailed(url, ref)
		if warning != nil {
			c.add(warning)
		}
		return path, err
	}
}

// runSyncCmd runs the authoritative sync pipeline as a tea.Cmd.
// It is a package-level var so tests can override it with a no-op to
// avoid real I/O when inspecting command composition in assertions.
var runSyncCmd = func(ctx context.Context, repoRoot string) tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		slog.Info("tui sync started")
		result := pipeline.Sync(ctx, tuiPipelineOptions(repoRoot, nil))
		slog.Info("tui sync finished", "success", len(result.Errors) == 0, "error_count", len(result.Errors), "duration_ms", time.Since(started).Milliseconds())
		return syncDoneMsg{err: errors.Join(result.Errors...)}
	}
}

// runUpdateCmd refreshes pinned registries and materializes them through the
// same authoritative pipeline the CLI sync and update commands run. After
// success or partial failure it produces the authoritative post-operation
// snapshot from disk, so the applied state reflects what the update actually
// did rather than what it intended.
//
// It is a package-level var so tests can stub the operation when driving
// key-level state-machine transitions.
var runUpdateCmd = func(ctx context.Context, repoRoot, homeDir string, generations ...requestGeneration) tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		var generation requestGeneration
		if len(generations) > 0 {
			generation = generations[0]
		}
		slog.Info("tui update started", "generation", generation)
		result := updateDoneMsg{}
		defer func() {
			slog.Info("tui update finished",
				"generation", generation,
				"stage", result.stage,
				"success", result.err == nil && len(result.staleCaches) == 0,
				"partial", len(result.staleCaches) > 0,
				"duration_ms", time.Since(started).Milliseconds())
		}()

		stale := &staleCacheCollector{}
		syncResult := pipeline.Sync(ctx, tuiPipelineOptions(repoRoot, stale.wrap()))

		// The authoritative post-operation snapshot reflects whatever the
		// update left on disk. The sync run's warnings are carried into it
		// with the same category mapping readSnapshot derives, so the
		// accepted replacement deduplicates on stable identity rather than
		// doubling entries.
		current := readSnapshotWithContext(ctx, repoRoot, homeDir, catalogLoadCached)
		current.generation = generation
		for _, warning := range syncResult.Warnings {
			current.diagnostics = appendSnapshotDiagnostics(current.diagnostics, diagnosticWarning, pipelineWarningCategory(warning), warning)
		}
		result.staleCaches = stale.all()
		for _, warning := range result.staleCaches {
			current.diagnostics = appendSnapshotDiagnostics(current.diagnostics, diagnosticWarning, diagnosticCatalog, warning)
		}
		current.diagnostics = deduplicateDiagnostics(current.diagnostics)
		result.snapshot = current

		switch {
		case len(syncResult.CatalogTrustErrors) > 0:
			// Everything in the catalog-trust bucket predates
			// materialization: config, catalog, and resolution failures are
			// refresh-stage failures for update copy.
			result.stage = updateStageRefresh
			result.err = fmt.Errorf("update %s failed: %w", updateStageRefresh, errors.Join(pipeline.DedupIssues(syncResult.CatalogTrustErrors)...))
		case len(syncResult.MaterializeErrors) > 0:
			result.stage = updateStageMaterialization
			result.err = fmt.Errorf("update %s failed: %w", updateStageMaterialization, errors.Join(pipeline.DedupIssues(syncResult.MaterializeErrors)...))
		default:
			result.stage = updateStageMaterialization
			if errs := diagnosticErrors(current.diagnostics); len(errs) > 0 {
				result.stage = updateStageReload
				result.err = fmt.Errorf("update state reload failed after materialization: %w", errors.Join(errs...))
			}
		}
		return result
	}
}

// pipelineWarningCategory attributes a sync-run warning to the same
// diagnostic category readSnapshot derives for the same underlying issue, so
// carrying it into the post-operation snapshot deduplicates on stable
// identity instead of producing a second copy.
func pipelineWarningCategory(err error) diagnosticCategory {
	switch {
	case catalog.IsOverlapWarning(err):
		return diagnosticCatalog
	case isSoftResolutionIssue(err):
		return diagnosticResolution
	case pipeline.IsDiagIssue(err):
		return diagnosticMaterialization
	default:
		return diagnosticConfig
	}
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
