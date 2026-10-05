package session

import (
	"errors"
	"slices"
	"strings"

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

// CatalogLoadMode chooses whether catalog reads may refresh the registry
// cache or must stay cached-only.
type CatalogLoadMode int

const (
	CatalogLoadCached CatalogLoadMode = iota
	CatalogLoadRefresh
)

// CatalogInfo aliases the loaded-catalog record the resolver produces.
type CatalogInfo = catalog.LoadedCatalog

// sessionCatalogLoadMode converts a session catalog mode to the catalog
// package's load mode.
func sessionCatalogLoadMode(mode CatalogLoadMode) catalog.LoadMode {
	if mode == CatalogLoadRefresh {
		return catalog.LoadModeRefresh
	}
	return catalog.LoadModeCached
}

// LoadCatalogs loads every configured registry catalog through the shared
// loaders so tests can substitute fake backends.
func LoadCatalogs(cfg *config.Config, repoRoot string, mode CatalogLoadMode) (map[string]CatalogInfo, bool, error) {
	result := catalog.LoadRegistries(cfg, repoRoot, sessionCatalogLoadMode(mode), catalog.WithCloneOrPull(CloneOrPull), catalog.WithDiscoverFunc(DiscoverCatalog))
	if len(result.Errors) > 0 {
		return result.Catalogs, result.RefreshAfterLoad, errors.Join(result.Errors...)
	}
	return result.Catalogs, result.RefreshAfterLoad, nil
}

func LocalExcludes(cfg *config.LocalConfig) []string {
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

// RegistryIdentityKey builds the lookup key used to attribute a local
// registry entry to its catalog key. It mirrors the registry cache identity
// (normalized URL + ref + path) so entries that share a URL/path but differ by
// ref are not collapsed onto the same catalog key.
func RegistryIdentityKey(url, ref, path string) string {
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

func MarketplaceProtocolKind(kind ir.ResourceKind) string {
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

func MarketplaceCatalogKey(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) string {
	return config.MarketplaceCatalogKey(entry, pin)
}

func MarketplaceSelectionKey(catalogKey, kind, installedName string) string {
	return catalogKey + "\x00" + kind + "\x00" + installedName
}

func IndexMarketplaceSelections(cfg *config.Config) map[string]map[config.Source]MarketplaceSelection {
	if cfg == nil || len(cfg.Marketplaces) == 0 {
		return nil
	}
	indexed := make(map[string]map[config.Source]MarketplaceSelection)
	for _, entry := range cfg.Marketplaces {
		for _, pin := range entry.Resources {
			key := MarketplaceSelectionKey(MarketplaceCatalogKey(entry, pin), pin.Kind, marketplaceInstalledName(pin))
			if indexed[key] == nil {
				indexed[key] = make(map[config.Source]MarketplaceSelection)
			}
			indexed[key][entry.Source] = MarketplaceSelection{Entry: entry, Pin: pin}
		}
	}
	return indexed
}

func marketplaceScopeFlags(selections map[config.Source]MarketplaceSelection) (global, project, local bool) {
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

func MarketplaceCollectionOnly(selections map[config.Source]MarketplaceSelection) bool {
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

// BuildRows computes per-scope enablement by walking registries and asking
// each one which skill names it selects. Global vs project scope is
// distinguished by RegistryEntry.IsGlobal. Local enablement and excludes
// come from the local config directly.
func BuildRows(cfg *config.Config, catalogs map[string]CatalogInfo, resolved []resolver.ResolvedArtifact, localCfg *config.LocalConfig, driftReport materialize.DriftReport, harnessDefaults ...map[config.Source][]string) []Row {
	totalCatalogEntries := 0
	totalMalformed := 0
	for _, cat := range catalogs {
		totalCatalogEntries += len(cat.Entries)
		totalMalformed += len(cat.Malformed)
	}
	localEnabledCount, localExcludedCount := localSelectorCounts(localCfg)
	marketplaceSelectionsByArtifact := IndexMarketplaceSelections(cfg)

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
			k := RegistryIdentityKey(e.URL, e.Ref, e.Path)
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
			catalogKey := catalogKeyByIdentity[RegistryIdentityKey(re.URL, re.Ref, re.Path)]
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
	rows := make([]Row, 0, len(resolved)+totalCatalogEntries+totalMalformed)

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
		var marketplaceSelections map[config.Source]MarketplaceSelection
		if s.IsMarketplace {
			marketplaceSelections = marketplaceSelectionsByArtifact[MarketplaceSelectionKey(s.CatalogKey, MarketplaceProtocolKind(s.Kind), s.Name)]
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

		rows = append(rows, Row{
			Artifact:                  s,
			IsAvailable:               false,
			IsExcluded:                excludedLocally,
			DriftItem:                 driftItem,
			EnabledGlobal:             enabledGlobal,
			EnabledProject:            enabledProject,
			EnabledLocal:              enabledLocal,
			ExcludedLocally:           excludedLocally,
			RegistryURL:               s.RegistryURL,
			RegistryName:              RegistryDisplayName(s.RegistryURL),
			RegistryPath:              registryEntryPathFor(entry),
			RegistryRef:               registryEntryRefFor(entry),
			Harnesses:                 targetsToHarnesses(s.Harnesses),
			HarnessSource:             harnessSource,
			MarketplaceSelections:     marketplaceSelections,
			MarketplaceCollectionOnly: MarketplaceCollectionOnly(marketplaceSelections),
		})
	}

	forEachUserExcludedOrganizationPin(cfg, func(pin config.MarketplaceResourcePin, excluded Row) {
		seenKey := skill.ResourceKey(excluded.Artifact.Kind, excluded.Artifact.Name) + "\x01" + excluded.Artifact.CatalogKey
		if _, exists := seen[seenKey]; exists {
			return
		}
		seen[seenKey] = struct{}{}
		excluded.MarketplaceSelections = marketplaceSelectionsByArtifact[MarketplaceSelectionKey(excluded.Artifact.CatalogKey, pin.Kind, excluded.Artifact.Name)]
		excluded.MarketplaceCollectionOnly = MarketplaceCollectionOnly(excluded.MarketplaceSelections)
		rows = append(rows, excluded)
	})

	// Add available skills not in resolved, using the resolver's collision
	// winners so the catalog view matches materialization selection.
	for _, available := range resolver.ResolveCatalogView(cfg, EntriesFromCatalogs(catalogs), resolved) {
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

		rows = append(rows, Row{
			Artifact:        s,
			IsAvailable:     true,
			IsExcluded:      excludedLocally,
			ExcludedLocally: excludedLocally,
			RegistryURL:     s.RegistryURL,
			RegistryName:    RegistryDisplayName(s.RegistryURL),
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

			rows = append(rows, Row{
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
func forEachUserExcludedOrganizationPin(cfg *config.Config, visit func(config.MarketplaceResourcePin, Row)) {
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
			visit(pin, Row{
				Artifact: resolver.ResolvedArtifact{
					Entry: catalog.Entry{Kind: kind, Name: name}, RegistryURL: entry.URL,
					RegistryPath: pin.RegistryID, Ref: pin.Revision, Source: entry.Source,
					CatalogKey: MarketplaceCatalogKey(entry, pin), IsMarketplace: true, MarketplaceURL: entry.URL,
					MarketplaceInstanceID: entry.InstanceID, MarketplaceRegistryID: pin.RegistryID, MarketplaceResourceName: pin.Name,
				},
				IsAvailable: true, IsExcluded: true, ExcludedByUser: true, EnabledGlobal: true,
				RegistryURL: entry.URL, RegistryName: RegistryDisplayName(entry.URL),
			})
		}
	}
}

// WithCachedCollectionLeaves returns cfg with each Marketplace collection pin
// whose leaves are not pinned yet expanded from its locally verified cached
// manifest, so pending rows take the positions verified inventory will give
// them before acquisition. It never fetches, and the copy only feeds
// provisional rows.
func WithCachedCollectionLeaves(cfg *config.Config, cache *marketplace.Cache) *config.Config {
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

// provisionalLocalCatalog is the cached catalog content of one local
// registry entry, indexed by its position in the merged config.
type provisionalLocalCatalog struct {
	Entry   config.RegistryEntry
	Entries []catalog.Entry
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
	result := catalog.LoadRegistries(localOnly, workspace.RepoRoot(), catalog.LoadModeCachedNoFetch, catalog.WithLocalConfigDir(workspace.LocalConfigDir()), catalog.WithDiscoverFunc(DiscoverCatalog))
	catalogs := make(map[int]provisionalLocalCatalog, len(indexes))
	for pos, idx := range indexes {
		entry := localOnly.Registries[pos]
		catalogs[idx] = provisionalLocalCatalog{Entry: entry, Entries: result.Catalogs[entry.CatalogKey].Entries}
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
	entryIdentity := RegistryIdentityKey(entry.URL, entry.Ref, entry.Path)
	for i := range localCfg.Registries {
		re := &localCfg.Registries[i]
		scoped := re.SelectorForKind(kind)
		if !scoped.ExcludeAll && !slices.Contains(scoped.Exclude, trimmed) {
			continue
		}
		identity := RegistryIdentityKey(re.URL, re.Ref, re.Path)
		if identity == entryIdentity {
			return true
		}
	}
	return false
}

func BuildProvisionalRows(cfg *config.Config, localCfg *config.LocalConfig, locals map[int]provisionalLocalCatalog, harnessDefaults ...map[config.Source][]string) []Row {
	if cfg == nil {
		return nil
	}
	layerHarnesses := layerHarnessDefaults(cfg, harnessDefaults...)
	seen := make(map[string]int)
	var rows []Row
	add := func(key string, r Row) {
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
				entry = loaded.Entry
				catalogEntries = loaded.Entries
			}
		}
		for _, selected := range resolver.ResourcesSelected(entry, catalogEntries) {
			if provisionalLocallyExcluded(localCfg, selected.Kind, selected.Name, entry) {
				continue
			}
			add(selected.Kind.String()+"\x00"+selected.Name+"\x00"+entry.CatalogKey, Row{
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
				RegistryName:        RegistryDisplayName(entry.URL),
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
			catalogKey := MarketplaceCatalogKey(*entry, pin)
			add(MarketplaceSelectionKey(catalogKey, pin.Kind, name), Row{
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
				RegistryName:              RegistryDisplayName(entry.URL),
			})
		}
	}
	forEachUserExcludedOrganizationPin(cfg, func(pin config.MarketplaceResourcePin, excluded Row) {
		excluded.PendingVerification = true
		add(MarketplaceSelectionKey(excluded.Artifact.CatalogKey, pin.Kind, excluded.Artifact.Name), excluded)
	})
	return withoutShadowedCollectionMembers(rows)
}

// withoutShadowedCollectionMembers drops enabled, optional collection-only
// members whose resource name another enabled selection already provides;
// verified inventory resolves that collision to the configured selection.
func withoutShadowedCollectionMembers(rows []Row) []Row {
	configured := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		if !r.IsAvailable && !r.MarketplaceCollectionOnly {
			configured[skill.ResourceKey(r.Artifact.Kind, r.Artifact.Name)] = struct{}{}
		}
	}
	return slices.DeleteFunc(rows, func(r Row) bool {
		_, shadowed := configured[skill.ResourceKey(r.Artifact.Kind, r.Artifact.Name)]
		return shadowed && !r.IsAvailable && !r.Artifact.Required && r.MarketplaceCollectionOnly
	})
}

const UnavailableCatalogReason = "selection is unavailable because its catalog failed to load"

func AppendUnavailableSelectionRows(rows []Row, cfg *config.Config, localCfg *config.LocalConfig, resolveErrs []error, catalogs map[string]CatalogInfo) []Row {
	seen := make(map[string]struct{}, len(rows)+len(resolveErrs))
	available := make(map[string]struct{}, len(rows))
	for i := range rows {
		key := skill.ResourceKey(rows[i].Artifact.Kind, rows[i].Artifact.Name)
		seen[key+"\x01"+RegistryIdentityKey(rows[i].RegistryURL, rows[i].RegistryRef, rows[i].RegistryPath)] = struct{}{}
		if rows[i].IsAvailable {
			available[key] = struct{}{}
		}
	}
	layerHarnesses := layerHarnessDefaults(cfg)
	appendCandidate := func(kind ir.ResourceKind, name, url, ref, path string, required bool, reason string, soft bool) {
		key := skill.ResourceKey(kind, name)
		identity := RegistryIdentityKey(url, ref, path)
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
				if RegistryIdentityKey(entry.URL, entry.Ref, entry.Path) != identity {
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
				if RegistryIdentityKey(re.URL, re.Ref, re.Path) != identity {
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
		rows = append(rows, Row{
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
			UnavailableSoft:   soft && !required,
			DriftItem:         &materialize.DriftItem{Name: key, Reason: materialize.DriftBroken},
			IsExcluded:        excludedLocally,
			ExcludedLocally:   excludedLocally,
			EnabledGlobal:     enabledGlobal,
			EnabledProject:    enabledProject,
			EnabledLocal:      enabledLocal,
			RegistryURL:       meta.URL,
			RegistryName:      RegistryDisplayName(meta.URL),
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
				appendCandidate(selected.Kind, selected.Name, entry.URL, entry.Ref, entry.Path, entry.Required, UnavailableCatalogReason, false)
			}
			identity := RegistryIdentityKey(entry.URL, entry.Ref, entry.Path)
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
					appendCandidate(kind, name, entry.URL, entry.Ref, entry.Path, entry.Required, UnavailableCatalogReason, false)
				}
			}
		}
	}
	return rows
}

// EntriesFromCatalogs extracts the entry slices from a LoadedCatalog map
// for consumption by resolver.Resolve.
func EntriesFromCatalogs(catalogs map[string]catalog.LoadedCatalog) map[string][]catalog.Entry {
	result := make(map[string][]catalog.Entry, len(catalogs))
	for k, v := range catalogs {
		result[k] = v.Entries
	}
	return result
}

// RegistryDisplayName derives a short label from a registry URL.
// Strips trailing ".git" and returns the last path segment.
// Returns "" for empty input (e.g., malformed skills with no source).
func RegistryDisplayName(url string) string {
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
	globalCfg, _ := LoadGlobalConfig()
	repoCfg, _ := LoadRepoConfig()
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
