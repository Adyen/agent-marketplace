package materialize

import (
	"fmt"
	slashpath "path"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// The types below provide backward-compatible drift information for consumers
// (primarily the TUI) that were built around the internal/drift package.
// They are derived from a dry-run materialize.Report rather than computed
// independently. New code should use materialize.Report directly.

// DriftReason classifies why a skill is out of sync.
type DriftReason int

const (
	DriftMissing  DriftReason = iota // skill is configured but not materialized
	DriftOrphaned                    // file exists but no skill claims it
	DriftBroken                      // skill failed to compile/materialize
	DriftInSync                      // skill is properly materialized
)

func (r DriftReason) String() string {
	switch r {
	case DriftMissing:
		return "missing"
	case DriftOrphaned:
		return "orphaned"
	case DriftBroken:
		return "broken"
	case DriftInSync:
		return "in_sync"
	default:
		return fmt.Sprintf("unknown(%d)", r)
	}
}

// DriftItem describes a single artifact's drift status. It replaces the old
// drift.Item type for TUI consumers.
type DriftItem struct {
	Name   string
	Reason DriftReason
}

// DriftReport holds the result of a drift check derived from a dry-run
// materialize run. It replaces the old drift.Report for TUI consumers.
type DriftReport struct {
	Missing  []DriftItem
	Orphaned []DriftItem
	Broken   []DriftItem
	Synced   []DriftItem
}

// InSync reports whether there are no drift items (everything is materialized).
func (r DriftReport) InSync() bool {
	return len(r.Missing)+len(r.Orphaned)+len(r.Broken) == 0
}

// PendingCount returns the total number of items that need attention.
func (r DriftReport) PendingCount() int {
	return len(r.Missing) + len(r.Orphaned) + len(r.Broken)
}

// DriftReportFromReport derives a DriftReport from a dry-run materialize.Report.
// It uses the Written/Removed/Diagnostic fields to classify each skill.
func DriftReportFromReport(report *Report, resolvedNames []string) DriftReport {
	if report == nil {
		return DriftReport{}
	}

	var dr DriftReport

	resolvedByName, resolvedKeys := resolvedIndexes(resolvedNames)

	// Artifacts that would be written = missing from disk (need sync)
	writtenNames := make(map[string]bool)
	for _, tr := range report.TargetReports() {
		for _, w := range tr.Written {
			writtenNames[resourceKeyForPath(w, resolvedByName, resolvedKeys)] = true
		}
	}
	for name := range writtenNames {
		dr.Missing = append(dr.Missing, DriftItem{Name: name, Reason: DriftMissing})
	}

	// Removed files = orphaned
	for _, tr := range report.TargetReports() {
		for _, r := range tr.Removed {
			dr.Orphaned = append(dr.Orphaned, DriftItem{Name: resourceKeyForPath(r, resolvedByName, resolvedKeys), Reason: DriftOrphaned})
		}
	}

	// Skills with error diagnostics = broken
	brokenSet := make(map[string]bool)
	neutralBrokenNames := make(map[string]bool)
	for _, d := range report.Diags {
		if !d.IsError() || d.Resource == "" {
			continue
		}
		resource := diagnosticResourceKey(d.Resource, d.Source.File, resolvedByName, resolvedKeys)
		if resource == d.Resource {
			neutralBrokenNames[d.Resource] = true
		}
		if !brokenSet[resource] {
			brokenSet[resource] = true
			dr.Broken = append(dr.Broken, DriftItem{Name: resource, Reason: DriftBroken})
		}
	}

	// Remaining resolved skills are in sync
	for name := range resolvedKeys {
		if !writtenNames[name] && !brokenSet[name] && !neutralBrokenNames[resourceKeyName(name)] {
			dr.Synced = append(dr.Synced, DriftItem{Name: name, Reason: DriftInSync})
		}
	}

	return dr
}

func resolvedIndexes(resolvedNames []string) (map[string]string, map[string]bool) {
	unique := make(map[string]string, len(resolvedNames))
	ambiguous := make(map[string]bool)
	keys := make(map[string]bool, len(resolvedNames))
	for _, key := range resolvedNames {
		keys[key] = true
		name := resourceKeyName(key)
		if prior, ok := unique[name]; ok && prior != key {
			delete(unique, name)
			ambiguous[name] = true
			continue
		}
		if !ambiguous[name] {
			unique[name] = key
		}
	}
	return unique, keys
}

func diagnosticResourceKey(name, sourcePath string, resolvedByName map[string]string, resolvedKeys map[string]bool) string {
	if kind := filePathResourceKind(sourcePath); kind >= ir.KindSkill && kind <= ir.KindRule {
		key := skill.ResourceKey(kind, name)
		if resolvedKeys[key] {
			return key
		}
	}
	if key, ok := resolvedByName[name]; ok {
		return key
	}
	return name
}

func resourceKeyForPath(path string, resolvedByName map[string]string, resolvedKeys map[string]bool) string {
	name := filePathToSkillName(path)
	kind := filePathResourceKind(path)
	if kind >= ir.KindSkill && kind <= ir.KindRule {
		key := skill.ResourceKey(kind, name)
		if resolvedKeys[key] {
			return key
		}
	}
	if key, ok := resolvedByName[name]; ok {
		return key
	}
	switch kind {
	case ir.KindSkill, ir.KindCommand, ir.KindSubagent, ir.KindHook, ir.KindMcpServer, ir.KindRule:
		return skill.ResourceKey(kind, name)
	default:
		return name
	}
}

func resourceKeyName(key string) string {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return key
}

func filePathResourceKind(path string) ir.ResourceKind {
	parts := resourcePathParts(path)
	if len(parts) == 0 {
		return -1
	}
	switch parts[0] {
	case "skills":
		return ir.KindSkill
	case "commands":
		return ir.KindCommand
	case "agents":
		return ir.KindSubagent
	case "hooks":
		return ir.KindHook
	case "rules":
		return ir.KindRule
	default:
		return -1
	}
}

// filePathToSkillName extracts a skill name from an artifact path.
// For Claude target, paths look like "skills/<name>/<name>.md" or
// similar harness-specific layouts. We use the top-level directory name.
func filePathToSkillName(path string) string {
	parts := resourcePathParts(path)
	if len(parts) >= 2 {
		// If the path starts with a known directory like "skills/",
		// return the next component.
		for _, prefix := range []string{"skills", "agents", "commands", "rules"} {
			if parts[0] == prefix {
				if prefix == "rules" {
					return strings.TrimSuffix(parts[1], slashpath.Ext(parts[1]))
				}
				return parts[1]
			}
		}
	}
	if len(parts) >= 1 {
		return parts[0]
	}
	return path
}

func resourcePathParts(artifactPath string) []string {
	parts := strings.Split(strings.TrimPrefix(artifactPath, "./"), "/")
	if len(parts) >= 3 && (parts[0] == ".windsurf" || parts[0] == ".cursor" || parts[0] == ".factory") && parts[1] == "rules" {
		return parts[1:]
	}
	return parts
}
