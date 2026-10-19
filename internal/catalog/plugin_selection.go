package catalog

import (
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// PluginSelectionEdge is one resolved selection edge of a plugin: a direct
// request or a collection-to-plugin binding. Type carries the resolver's
// edge kind ("direct" or "collection").
type PluginSelectionEdge struct {
	Type       string
	Collection string
}

// PlannedPlugin is the resolver's own selection of one plugin: the format
// source entry that provides it, its selection edges, its member kind/name
// keys after request exclusions, and the catalog entry it resolved to (which
// carries the external source requirement for catalog entries).
type PlannedPlugin struct {
	Source  config.RegistryEntry
	Name    string
	Edges   []PluginSelectionEdge
	Members []string
	Entry   Entry
}

// PluginSelectionPlan is the resolver's plugin selection for a
// configuration; every plugin-v1 manifest input derives from it.
type PluginSelectionPlan struct {
	Plugins []PlannedPlugin
}

// ForEntry returns the plan items owned by the given format source entry:
// plugins whose source resolves to the same canonical URL or directory, ref,
// and root path.
func (p PluginSelectionPlan) ForEntry(entry config.RegistryEntry) []PlannedPlugin {
	out := []PlannedPlugin{}
	for _, plugin := range p.Plugins {
		if sameSourceLocation(plugin.Source, entry) {
			out = append(out, plugin)
		}
	}
	return out
}

// ExternalEntries returns the selected catalog entries that resolve to an
// external repository, one per planned plugin across every format source.
func (p PluginSelectionPlan) ExternalEntries() []PlannedPlugin {
	var out []PlannedPlugin
	for _, plugin := range p.Plugins {
		if plugin.Entry.Plugin != nil && plugin.Entry.Plugin.Source.Kind == "git" {
			out = append(out, plugin)
		}
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

func normalizedRootPath(path string) string {
	if path == "" {
		return "."
	}
	return path
}
