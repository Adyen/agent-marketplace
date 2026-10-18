package catalog

import (
	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
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
// independent of the acquired catalog content: the entry's declared selection
// (evaluated exactly like the resolver evaluates it), the authored member
// exclusions of its direct plugin requests, and its collection edges. The
// plugin-v1 source-revision manifest commits to exactly this shape.
type PluginSelectionSpec struct {
	Entry       config.RegistryEntry
	Explicit    []PluginExplicitSelection
	Collections []PluginCollectionEdge
}

// PluginSelectedNames resolves the declared selection against the decoded
// entries through the shared selection predicate: the plugins the entry
// itself selects, plus every plugin named by a collection edge.
func PluginSelectedNames(spec *PluginSelectionSpec, entries []Entry) map[string]bool {
	selected := map[string]bool{}
	if spec == nil {
		return selected
	}
	for _, sel := range SelectedResources(spec.Entry, entries) {
		if sel.Kind == ir.KindPlugin {
			selected[sel.Name] = true
		}
	}
	for _, edge := range spec.Collections {
		for _, name := range edge.Entries {
			selected[name] = true
		}
	}
	return selected
}

// PluginSelectionSpecFor derives the selection spec of one format source
// entry from its ordered v4 decisions. collections are the collections
// discovered in the same configuration layer's sources, and selected names
// the collections the layer selects.
func PluginSelectionSpecFor(entry config.RegistryEntry, collections []collection.Collection, selected map[string]bool) PluginSelectionSpec {
	spec := PluginSelectionSpec{Entry: entry}
	for _, decision := range entry.V4Decisions {
		if decision.Action != config.V4SelectionExplicit || decision.Request == nil || decision.Request.Kind != "plugin" {
			continue
		}
		spec.Explicit = append(spec.Explicit, PluginExplicitSelection{Name: decision.Request.Name, Exclude: append([]string(nil), decision.Request.Options.Exclude...)})
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
