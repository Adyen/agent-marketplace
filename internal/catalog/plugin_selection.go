package catalog

import (
	"path"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// PluginSelectionEdge is one resolved selection edge of a plugin: a direct
// request or a collection-to-plugin binding. Type carries the resolver's
// edge kind ("direct" or "collection"). Scope is the configuration layer
// that authored the edge; SourceUnset behaves like RepoCommitted, matching
// Source's own convention.
type PluginSelectionEdge struct {
	Type       string
	Collection string
	Scope      config.Source
}

// PlannedPlugin is the resolver's own selection of one plugin by one
// configuration layer: the format source entry that provides it, that
// layer's own selection edges, its member kind/name keys after that layer's
// request exclusions, and the catalog entry it resolved to (which carries
// the external source requirement for catalog entries). Layer is the
// configuration layer that authored the selection, the layer whose own
// acquisition lock commits it. Exclusions carries authored member
// exclusions an external entry's decoded package validates at lock time;
// in-repository entries match them during resolution.
type PlannedPlugin struct {
	Source     config.RegistryEntry
	Name       string
	Layer      config.Source
	Edges      []PluginSelectionEdge
	Members    []string
	Exclusions []string
	Entry      Entry
}

// PluginSelectionPlan is the resolver's plugin selection for a
// configuration; every plugin-v1 manifest input derives from it. The plan
// carries one record per (plugin, owning source location, authoring layer)
// and never deduplicates across layers.
type PluginSelectionPlan struct {
	Plugins []PlannedPlugin
}

// ForEntry returns the plan records the given layer authored for the given
// format source entry's location: plugins whose source resolves to the same
// canonical URL or directory, ref, and root path, selected by that layer's
// own configuration. Each record carries only that layer's edges, so a
// layer's plugin-v1 manifest commits exactly the selections authored in
// that layer's own configuration.
func (p PluginSelectionPlan) ForEntry(entry config.RegistryEntry, scope config.Source) []PlannedPlugin {
	if scope == config.SourceUnset {
		scope = config.RepoCommitted
	}
	out := []PlannedPlugin{}
	for _, plugin := range p.Plugins {
		if plugin.Layer == scope && sameSourceLocation(plugin.Source, entry) {
			out = append(out, plugin)
		}
	}
	return out
}

// ExternalEntries returns the selected catalog entries that resolve to an
// external repository. Records identical across authoring layers collapse
// to one external entry; selections that differ (for example in member
// exclusions) stay distinct.
func (p PluginSelectionPlan) ExternalEntries() []PlannedPlugin {
	var out []PlannedPlugin
	seen := map[string]bool{}
	for _, plugin := range p.Plugins {
		if plugin.Entry.Plugin == nil || plugin.Entry.Plugin.Source.Kind != "git" {
			continue
		}
		source := plugin.Entry.Plugin.Source
		key := plugin.Name + "\x00" + source.URL + "\x00" + source.Ref + "\x00" + source.Subdirectory +
			"\x00" + plugin.Entry.Plugin.DefinitionDigest + "\x00" + strings.Join(plugin.Exclusions, "\x01")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, plugin)
	}
	return out
}

// sameSourceLocation reports whether two registry entries describe the same
// source location: the same canonical git URL or directory, ref, and root
// path.
func sameSourceLocation(a, b config.RegistryEntry) bool {
	if a.IsLocal() || b.IsLocal() {
		if a.IsLocal() != b.IsLocal() {
			return false
		}
	} else {
		aURL, aErr := lockfile.CanonicalGitConfigURL(a.URL)
		bURL, bErr := lockfile.CanonicalGitConfigURL(b.URL)
		if aErr != nil || bErr != nil || aURL != bURL {
			return false
		}
	}
	if a.IsLocal() && a.URL != b.URL {
		return false
	}
	if a.Ref != b.Ref {
		return false
	}
	return normalizedRootPath(a.Path) == normalizedRootPath(b.Path)
}

func normalizedRootPath(rootPath string) string {
	return path.Clean(rootPath)
}
