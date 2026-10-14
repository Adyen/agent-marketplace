package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/redact"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// prerequisiteState is the single marker a declared prerequisite carries in
// the detail pane's "Requires" section.
type prerequisiteState int

const (
	prerequisiteMet prerequisiteState = iota
	prerequisiteImplied
	prerequisitePreview
	prerequisiteExcluded
	prerequisiteUnmet
)

func (s prerequisiteState) marker() string {
	switch s {
	case prerequisiteMet:
		return "met"
	case prerequisiteImplied:
		return "implied"
	case prerequisitePreview:
		return "preview"
	case prerequisiteExcluded:
		return "excluded"
	default:
		return "unmet"
	}
}

// prerequisiteEntry is one valid declared prerequisite of a row's resource,
// precomputed when the snapshot rows are built.
type prerequisiteEntry struct {
	kind  ir.ResourceKind
	name  string
	hint  string // redacted provider hint; empty when undeclared
	state prerequisiteState
}

func prerequisiteKey(kind ir.ResourceKind, name string) string {
	return kind.String() + "\x00" + name
}

// requirementHint returns the redacted provider hint declared on req, if any.
// Registry-authored hints may carry URL credentials, which are stripped before
// the text ever reaches the terminal.
func requirementHint(req catalog.Requirement) string {
	var hints []string
	if strings.TrimSpace(req.Registry) != "" {
		hints = append(hints, redact.URL(req.Registry))
	}
	if strings.TrimSpace(req.Path) != "" {
		hints = append(hints, redact.URL(req.Path))
	}
	return strings.Join(hints, " ")
}

// annotatePrerequisiteRows precomputes each resolved row's prerequisite
// state: the Requires entries the detail pane lists in declaration order and
// the unmet flag the list warning marker reads. Available and malformed rows
// carry no prerequisite state. Satisfaction is by kind and name, matching the
// resolver's own rule; unmet reasons come from the typed warnings the
// resolver emitted for this resolution.
func annotatePrerequisiteRows(rows []row, resolved []resolver.ResolvedArtifact, preview []resolver.ResolvedArtifact, errs []error) {
	selected := make(map[string]bool, len(resolved)) // key → implied
	for _, a := range resolved {
		key := prerequisiteKey(a.Kind, a.Name)
		if implied, seen := selected[key]; !seen || !implied {
			selected[key] = a.Implied
		}
	}
	previewed := make(map[string]struct{}, len(preview))
	for _, a := range preview {
		previewed[prerequisiteKey(a.Kind, a.Name)] = struct{}{}
	}
	var unmet map[string]map[string]resolver.UnmetRequirementReason
	for _, err := range errs {
		var warning *resolver.UnmetRequirementWarning
		if !errors.As(err, &warning) {
			continue
		}
		if unmet == nil {
			unmet = map[string]map[string]resolver.UnmetRequirementReason{}
		}
		requirer := prerequisiteKey(warning.ResourceKind, warning.ResourceName)
		if unmet[requirer] == nil {
			unmet[requirer] = map[string]resolver.UnmetRequirementReason{}
		}
		unmet[requirer][prerequisiteKey(warning.Requirement.Kind, warning.Requirement.Name)] = warning.Reason
	}
	for i := range rows {
		r := &rows[i]
		if r.IsAvailable || r.IsMalformed {
			continue
		}
		requirements := catalog.RequirementsFromResource(r.Artifact.Resource)
		if len(requirements) == 0 {
			continue
		}
		requirer := prerequisiteKey(r.Artifact.Kind, r.Artifact.Name)
		for _, req := range requirements {
			name := strings.TrimSpace(req.Name)
			if name == "" {
				continue
			}
			key := prerequisiteKey(req.Kind, name)
			entry := prerequisiteEntry{kind: req.Kind, name: name, hint: requirementHint(req)}
			if implied, ok := selected[key]; ok {
				if implied {
					entry.state = prerequisiteImplied
				}
			} else if reason, isUnmet := unmet[requirer][key]; isUnmet {
				entry.state = prerequisiteUnmet
				if reason == resolver.UnmetReasonExcluded {
					entry.state = prerequisiteExcluded
				}
			} else if _, ok := previewed[key]; ok {
				entry.state = prerequisitePreview
			} else {
				entry.state = prerequisiteUnmet
			}
			r.Prerequisites = append(r.Prerequisites, entry)
			if entry.state == prerequisiteUnmet || entry.state == prerequisiteExcluded {
				r.HasUnmetPrerequisite = true
			}
		}
	}
}

// impliedSelection reports whether the row is an expansion-selected implied
// resource, as opposed to a directly selected or preview-only one.
func impliedSelection(r row) bool {
	return r.Artifact.Implied && !r.Artifact.PreviewOnly
}

// prerequisiteBadges renders the row-level prerequisite markers: `req` for an
// expansion-selected implied resource and ⚠ for a resource with an unmet
// prerequisite. Rows without prerequisite relationships get no marker, so
// their rendered output stays byte-identical.
func prerequisiteBadges(r row) string {
	var b strings.Builder
	if impliedSelection(r) {
		b.WriteString(" req")
	}
	if r.HasUnmetPrerequisite {
		b.WriteString(" ⚠")
	}
	return b.String()
}

// impliedToggleScope reports which scope's toggle key turns an
// expansion-selected implied row off: the layer that selected its requiring
// resource, mirroring the CLI remove scope inference. The second result is
// false for rows that are not implied selections.
func impliedToggleScope(r row) (scope, bool) {
	if !impliedSelection(r) {
		return 0, false
	}
	switch r.Artifact.Source {
	case config.RepoPersonal:
		return scopeLocal, true
	case config.RepoCommitted:
		return scopeProject, true
	default:
		// Machine-scope (org/user) requirers select the implied resource into
		// the machine view; the effective exclusion lives in the user layer.
		return scopeGlobal, true
	}
}

// preexistingImpliedKeys is the set of implied prerequisite kind/name keys
// present before a toggle starts: resources already selected through
// expansion (resolved) or previewed (impliedPreview) did not come along with
// the toggle and must not be reported again.
func preexistingImpliedKeys(resolved, preview []resolver.ResolvedArtifact) map[string]struct{} {
	keys := make(map[string]struct{}, len(resolved)+len(preview))
	for _, artifacts := range [][]resolver.ResolvedArtifact{resolved, preview} {
		for _, a := range artifacts {
			if a.Implied {
				keys[prerequisiteKey(a.Kind, a.Name)] = struct{}{}
			}
		}
	}
	return keys
}

// prerequisiteToggleNotes computes the footer lines a completed toggle owes:
// for a toggle-on, each prerequisite that came along (`also selected` under
// expansion, `would also select` in advisory mode); for a toggle-off, each
// requiring resource's resulting unmet prerequisite warning. msg is the
// authoritative snapshot accepted after the write; preexisting holds the
// implied prerequisite keys captured before the toggle ran.
func prerequisiteToggleNotes(act *toggleAction, msg snapshotLoadedMsg, preexisting map[string]struct{}) []string {
	if act == nil {
		return nil
	}
	if act.disablesResource() {
		var notes []string
		for _, errs := range [][]error{msg.warnings, msg.errs} {
			for _, err := range errs {
				var warning *resolver.UnmetRequirementWarning
				if !errors.As(err, &warning) {
					continue
				}
				if warning.Requirement.Kind == act.kind && strings.TrimSpace(warning.Requirement.Name) == act.skillName {
					notes = append(notes, warning.Error())
				}
			}
		}
		return notes
	}
	expansion := msg.mergedCfg != nil && msg.mergedCfg.RequiresExpansionEnabled()
	verb := "would also select"
	candidates := msg.impliedPreview
	if expansion {
		verb = "also selected"
		candidates = msg.resolved
	}
	// Walk the RequiredBy closure rooted at the toggled resource so a chain
	// (parent -> child -> grandchild) reports every prerequisite that came
	// along, each note naming its immediate requirer.
	requirers := map[string]struct{}{prerequisiteKey(act.kind, act.skillName): {}}
	reported := make(map[string]struct{}, len(candidates))
	var notes []string
	for progress := true; progress; {
		progress = false
		for _, a := range candidates {
			key := prerequisiteKey(a.Kind, a.Name)
			if !a.Implied {
				continue
			}
			if _, done := reported[key]; done {
				continue
			}
			if _, already := preexisting[key]; already {
				reported[key] = struct{}{}
				continue
			}
			for _, ref := range a.RequiredBy {
				if _, ok := requirers[prerequisiteKey(ref.Kind, ref.Name)]; ok {
					notes = append(notes, fmt.Sprintf("%s %s %q (required by %s %q)",
						verb, skill.KindLabel(a.Kind), a.Name, skill.KindLabel(ref.Kind), ref.Name))
					reported[key] = struct{}{}
					requirers[key] = struct{}{}
					progress = true
					break
				}
			}
		}
	}
	return notes
}
