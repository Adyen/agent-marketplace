package catalog

import (
	"strings"

	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// PluginExplicitSelection is one direct plugin request: its name and the
// authored member exclusions.
type PluginExplicitSelection struct {
	Name    string
	Exclude []string
}

// PluginCollectionEdge binds a selected collection to the plugin entries of
// one format source: the collection label and the entry names it selects.
type PluginCollectionEdge struct {
	Collection string
	Entries    []string
}

// PluginSelectionSpec is the config-declared selection of one format source,
// independent of the acquired catalog content: direct requests, all-style
// selection, group-level plugin exclusions, and collection edges. The
// plugin-v1 source-revision manifest commits to exactly this shape.
type PluginSelectionSpec struct {
	Explicit    []PluginExplicitSelection
	All         bool
	Excluded    []string
	Collections []PluginCollectionEdge
}

// PluginSelectionSpecFor derives the selection spec of one format source
// entry from its ordered v4 decisions. collections are the collections
// discovered in the same configuration layer's sources, and selected names
// the collections the layer selects.
func PluginSelectionSpecFor(entry config.RegistryEntry, collections []collection.Collection, selected map[string]bool) PluginSelectionSpec {
	spec := PluginSelectionSpec{All: entry.V4All || entry.Required || entry.Default}
	for _, decision := range entry.V4Decisions {
		switch decision.Action {
		case config.V4SelectionExplicit:
			if decision.Request == nil || decision.Request.Kind != "plugin" {
				continue
			}
			spec.Explicit = append(spec.Explicit, PluginExplicitSelection{Name: decision.Request.Name, Exclude: append([]string(nil), decision.Request.Options.Exclude...)})
		case config.V4SelectionAll:
			spec.All = true
		case config.V4SelectionExclude:
			kind, name, ok := strings.Cut(decision.Exclusion, "/")
			if ok && kind == "plugin" {
				spec.Excluded = append(spec.Excluded, name)
			}
		}
	}
	canonical, err := lockfile.CanonicalGitConfigURL(entry.URL)
	if err != nil {
		canonical = ""
	}
	for i := range collections {
		col := collections[i]
		if !selected[col.Name] {
			continue
		}
		var names []string
		for _, member := range col.Resources {
			if member.Kind != "plugin" {
				continue
			}
			if !collectionMemberTargetsEntry(member, col, entry, canonical) {
				continue
			}
			names = append(names, member.Name)
		}
		if len(names) > 0 {
			spec.Collections = append(spec.Collections, PluginCollectionEdge{Collection: col.Label(), Entries: names})
		}
	}
	return spec
}

// collectionMemberTargetsEntry reports whether a collection member entry
// addresses the given format source: an empty registry means the
// collection's own source; otherwise the member's registry must match the
// entry's canonical URL or its directory URL.
func collectionMemberTargetsEntry(member collection.CollectionEntry, col collection.Collection, entry config.RegistryEntry, canonical string) bool {
	registry := member.Registry
	if registry == "" {
		registry = col.SourceRegistryURL
	}
	if registry == "" {
		return false
	}
	if canonical != "" {
		other, err := lockfile.CanonicalGitConfigURL(registry)
		if err == nil {
			if other != canonical {
				return false
			}
		} else if registry != entry.URL {
			return false
		}
	} else if registry != entry.URL {
		return false
	}
	path := member.Path
	if path == "" {
		path = col.SourcePath
	}
	if path == "" {
		path = "."
	}
	entryPath := entry.Path
	if entryPath == "" {
		entryPath = "."
	}
	return path == entryPath
}
