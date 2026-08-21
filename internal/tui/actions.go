package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/registryfs"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	tea "github.com/charmbracelet/bubbletea"
)

type snapshotLoadedMsg struct {
	rows                 []row
	driftReport          materialize.DriftReport
	catalogs             map[string][]catalog.Entry
	resolved             []resolver.ResolvedArtifact
	mergedCfg            *config.Config
	materializationModes config.MaterializationModes
	localExcludes        []string
	globalHarnesses      []string
	projectHarnesses     []string
	errs                 []error
	warnings             []error
	refreshAfterLoad     bool
	pendingMigration     migrationPrompt
}

type reloadMsg snapshotLoadedMsg
type refreshLoadedMsg struct {
	snapshotLoadedMsg
	refreshToken int
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

type configWrittenMsg struct {
	skillName string
	enabled   bool
	err       error
}

type syncDoneMsg struct {
	err error
}

type updateDoneMsg struct {
	err error
}

// detailContentMsg carries the glamour-rendered SKILL.md content for the
// full-screen detail view. Produced by loadDetailContent (async tea.Cmd)
// and consumed in Update to populate the viewport.
type detailContentMsg struct {
	content string
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
)

func loadSnapshot(repoRoot, homeDir string) tea.Cmd {
	return func() tea.Msg {
		return readSnapshot(repoRoot, homeDir, catalogLoadCached)
	}
}

func refreshSnapshot(repoRoot, homeDir string, refreshToken int) tea.Cmd {
	return func() tea.Msg {
		return refreshLoadedMsg{
			snapshotLoadedMsg: readSnapshot(repoRoot, homeDir, catalogLoadRefresh),
			refreshToken:      refreshToken,
		}
	}
}

func readSnapshot(repoRoot, homeDir string, mode catalogLoadMode) snapshotLoadedMsg {
	var catalogOverlapWarnings []error
	resolvedResult := pipeline.Resolve(context.Background(), pipeline.ResolveOptions{
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

	// Surface a v2 migration prompt when any config file still declares the
	// v2 schema. The modal must not pop for v1/empty/unknown API versions.
	pendingMigration := pendingMigrationFor(repoRoot, homeDir)

	if len(resolvedResult.ConfigErrors) > 0 {
		return snapshotLoadedMsg{errs: resolvedResult.ConfigErrors}
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
			return snapshotLoadedMsg{errs: hardErrs, warnings: overlapWarnings, pendingMigration: pendingMigration}
		}
		// No hard errors; carry overlap warnings forward to the final warnings.
		// Store them in a temp var since `warnings` is declared below.
		catalogOverlapWarnings = overlapWarnings
	}

	// Use materialize dry-run to compute drift instead of drift.Compute.
	var resolvedNames []string
	for _, r := range resolvedResult.Resolved {
		resolvedNames = append(resolvedNames, skill.ResourceKey(r.Kind, r.Name))
	}
	dryRunReport, _ := materialize.Run(context.Background(), repoRoot, resolvedResult.Resolved, materialize.WithDryRun(), materialize.WithMaterializationModes(resolvedResult.MaterializationModes))
	driftReport := materialize.DriftReportFromReport(dryRunReport, resolvedNames)

	rows := buildRows(resolvedResult.LoadedCfg, resolvedResult.CatalogLoad.Catalogs, resolvedResult.Resolved, resolvedResult.LocalCfg, driftReport, harnessDefaultsBySource(resolvedResult.GlobalCfg, resolvedResult.RepoCfg))

	var errs []error
	var warnings []error

	// Surface overlap warnings from catalog loading.
	warnings = append(warnings, catalogOverlapWarnings...)

	// Surface malformed skills as warnings in the errors overlay so the
	// user can see them when pressing 'e' without scrolling to the bottom
	// of the scope tree.
	for _, m := range resolvedResult.CatalogLoad.Malformed {
		warnings = append(warnings, fmt.Errorf("malformed skill %q (%s): %w", m.DirName, m.Path, m.Error))
	}

	for _, err := range resolvedResult.ResolveErrors {
		if resolver.IsSoftSelectedNotFound(err) {
			warnings = append(warnings, err)
			continue
		}
		if resolver.IsNameCollision(err) {
			warnings = append(warnings, err)
			continue
		}
		if resolver.IsWarning(err) {
			continue
		}
		errs = append(errs, err)
	}

	return snapshotLoadedMsg{
		rows:                 rows,
		driftReport:          driftReport,
		catalogs:             entriesFromCatalogs(resolvedResult.CatalogLoad.Catalogs),
		resolved:             resolvedResult.Resolved,
		mergedCfg:            resolvedResult.LoadedCfg,
		materializationModes: resolvedResult.MaterializationModes,
		localExcludes:        localExcludes(resolvedResult.LocalCfg),
		globalHarnesses:      configHarnesses(resolvedResult.GlobalCfg),
		projectHarnesses:     configHarnesses(resolvedResult.RepoCfg),
		errs:                 errs,
		warnings:             warnings,
		refreshAfterLoad:     resolvedResult.CatalogLoad.RefreshAfterLoad,
		pendingMigration:     pendingMigration,
	}
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
// The read is confined to registryRoot (the registry the resource was
// discovered in), so previewing a resource cannot render host content that a
// registry symlinked into place, and cannot block the TUI on a special file.
func ReadSkillMarkdown(registryRoot, path string) (string, error) {
	data, sourceFile, err := registryfs.ReadResourceFile(registryRoot, path)
	if err != nil {
		return "", fmt.Errorf("read resource source %s: %w", filepath.Base(sourceFile), err)
	}
	return string(data), nil
}

// loadDetailContent reads and glamour-renders a skill's SKILL.md in a
// background goroutine so the TUI can transition to the detail pane
// immediately without blocking on file I/O or markdown rendering.
func loadDetailContent(r row) tea.Cmd {
	return func() tea.Msg {
		return detailContentMsg{content: renderDetailFullContent(r)}
	}
}

// runSyncCmd runs the authoritative sync pipeline as a tea.Cmd.
// It is a package-level var so tests can override it with a no-op to
// avoid real I/O when inspecting command composition in assertions.
var runSyncCmd = func(repoRoot string) tea.Cmd {
	return func() tea.Msg {
		result := pipeline.Sync(context.Background(), pipeline.ResolveOptions{
			RepoRoot: repoRoot,
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
		return syncDoneMsg{err: errors.Join(result.Errors...)}
	}
}

// runUpdateCmd reloads catalogs (clone/pull), materializes refreshed resources, and reloads state.
func runUpdateCmd(repoRoot, homeDir string) tea.Cmd {
	return func() tea.Msg {
		snap := readSnapshot(repoRoot, homeDir, catalogLoadRefresh)
		if snap.mergedCfg == nil && len(snap.errs) > 0 {
			return updateDoneMsg{err: errors.Join(snap.errs...)}
		}
		report, err := materialize.Run(context.Background(), repoRoot, snap.resolved, materialize.WithMaterializationModes(snap.materializationModes))
		var errs []error
		errs = append(errs, snap.errs...)
		errs = append(errs, promoteReportErrors(report)...)
		if err != nil {
			errs = append(errs, err)
		}
		if len(errs) > 0 {
			return updateDoneMsg{err: errors.Join(errs...)}
		}
		// Preserve warning-level diagnostics so the TUI can display them and
		// tests can observe that a warning-only update really did emit a warning.
		if report != nil {
			for _, d := range report.Diags {
				if !d.IsError() {
					snap.warnings = append(snap.warnings, pipeline.PromoteDiagnostic(d))
				}
			}
		}
		return reloadMsg(snap)
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
