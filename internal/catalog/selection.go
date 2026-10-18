package catalog

import (
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// SelectedResource is one kind/name pair a registry entry's declared
// selection admits.
type SelectedResource struct {
	Kind ir.ResourceKind
	Name string
}

// SelectedResources applies one registry entry's declared selection to a
// decoded catalog: wholesale all/required/default selection, tag matching,
// the ordered v4 decision stream (explicit adds and exclusions matched by
// kind/name, kind/* and *, skipped for required groups), and the legacy
// selector include/exclude fallback. It is the single selection predicate the
// resolver and the plugin lock path share so both select the same set.
func SelectedResources(entry config.RegistryEntry, entries []Entry) []SelectedResource {
	selected := make(map[SelectedResource]struct{})

	// v4 all:true is additive, including when the group also has explicit
	// includes or tags. Legacy required/default selection retains its
	// historical explicit-selection guard.
	autoSelectAll := entry.V4All || ((entry.Required || entry.Default) && !HasExplicitSelection(entry))
	if autoSelectAll {
		for _, candidate := range entries {
			name := strings.TrimSpace(candidate.Name)
			if name == "" {
				continue
			}
			selected[SelectedResource{Kind: candidate.Kind, Name: name}] = struct{}{}
		}
	}

	// Tag selection participates in the ordered v4 stream: exclusions must be
	// able to suppress tagged resources just as they suppress explicit
	// includes.
	for _, candidate := range entries {
		name := strings.TrimSpace(candidate.Name)
		if name == "" {
			continue
		}
		if config.HasMatchingTag(candidate.Tags, entry.Tags) {
			selected[SelectedResource{Kind: candidate.Kind, Name: name}] = struct{}{}
		}
	}

	// V4 carries an ordered decision stream. Apply it after dynamic tag/all
	// selection so an exclusion can suppress inherited selections while a
	// later include can explicitly re-enable the same resource.
	if entry.V4Decisions != nil {
		for _, decision := range entry.V4Decisions {
			switch decision.Action {
			case config.V4SelectionExplicit:
				if decision.Request == nil {
					continue
				}
				selected[SelectedResource{Kind: DecisionKind(decision.Request.Kind), Name: decision.Request.Name}] = struct{}{}
			case config.V4SelectionExclude:
				if entry.Required {
					continue
				}
				for candidate := range selected {
					if DecisionExclusionMatches(decision.Exclusion, candidate.Kind, candidate.Name) {
						delete(selected, candidate)
					}
				}
			}
		}
	} else {
		for _, kind := range config.AllKinds {
			for _, name := range entry.SelectorForKind(kind).Include {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				selected[SelectedResource{Kind: kind, Name: name}] = struct{}{}
			}
		}
	}

	// required:true is immune to entry-level excludes; default:true and
	// explicit selections respect them. V4's ordered stream already applied
	// its excludes.
	if !entry.Required && entry.V4Decisions == nil {
		excludeAllKinds := EntryExcludeAllKinds(entry)
		for _, excluded := range EntryResourceExcludes(entry) {
			for candidate := range selected {
				if candidate.Kind == excluded.Kind && candidate.Name == strings.TrimSpace(excluded.Name) {
					delete(selected, candidate)
				}
			}
		}
		for kind := range excludeAllKinds {
			for candidate := range selected {
				if candidate.Kind == kind {
					delete(selected, candidate)
				}
			}
		}
	}

	out := make([]SelectedResource, 0, len(selected))
	for selected := range selected {
		out = append(out, selected)
	}
	slices.SortFunc(out, func(a, b SelectedResource) int {
		if a.Kind != b.Kind {
			return int(a.Kind) - int(b.Kind)
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// DecisionKind maps a v4 locator kind to its resource kind.
func DecisionKind(kind string) ir.ResourceKind {
	switch kind {
	case "skill":
		return ir.KindSkill
	case "command":
		return ir.KindCommand
	case "subagent":
		return ir.KindSubagent
	case "hook":
		return ir.KindHook
	case "mcp":
		return ir.KindMcpServer
	case "collection":
		return ir.KindCollection
	case "plugin":
		return ir.KindPlugin
	default:
		return ir.ResourceKind(-1)
	}
}

// DecisionExclusionMatches reports whether a v4 exclusion names the resource:
// "*", kind/*, kind/name, and grouped group/kind/name forms.
func DecisionExclusionMatches(exclusion string, kind ir.ResourceKind, name string) bool {
	if exclusion == "*" {
		return true
	}
	parts := strings.Split(strings.TrimSuffix(exclusion, "/*"), "/")
	if strings.HasSuffix(exclusion, "/*") {
		if len(parts) == 1 {
			return DecisionKind(parts[0]) == kind
		}
		if len(parts) == 2 {
			return DecisionKind(parts[1]) == kind
		}
		return false
	}
	if len(parts) == 2 {
		return DecisionKind(parts[0]) == kind && parts[1] == name
	}
	if len(parts) == 3 {
		return parts[2] == name && DecisionKind(parts[1]) == kind
	}
	return false
}

// HasExplicitSelection reports whether the entry's declared selection is
// explicit: a v4 decision stream is explicit only when it contains an
// include; a selector is explicit only with a non-empty Include list, not
// merely an Exclude. Exclude-only selectors do not disable autoSelectAll.
func HasExplicitSelection(entry config.RegistryEntry) bool {
	for _, decision := range entry.V4Decisions {
		if decision.Action == config.V4SelectionExplicit && decision.Request != nil {
			return true
		}
	}
	for _, kind := range config.AllKinds {
		if len(entry.SelectorForKind(kind).Include) > 0 {
			return true
		}
	}
	return entry.Tags != nil
}

// EntryResourceExcludes lists the per-kind selector exclusions of one entry.
func EntryResourceExcludes(entry config.RegistryEntry) []SelectedResource {
	var excludes []SelectedResource
	for _, kind := range config.AllKinds {
		for _, name := range entry.SelectorForKind(kind).Exclude {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			excludes = append(excludes, SelectedResource{Kind: kind, Name: name})
		}
	}
	return excludes
}

// EntryExcludeAllKinds returns the set of kinds for which the entry has
// ExcludeAll=true on its per-kind selector.
func EntryExcludeAllKinds(entry config.RegistryEntry) map[ir.ResourceKind]bool {
	kinds := make(map[ir.ResourceKind]bool)
	for _, kind := range config.AllKinds {
		if entry.SelectorForKind(kind).ExcludeAll {
			kinds[kind] = true
		}
	}
	return kinds
}
