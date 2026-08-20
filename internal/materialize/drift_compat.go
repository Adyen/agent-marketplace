package materialize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
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
	Name         string
	Reason       DriftReason
	Path         string
	Target       hcc.Target
	MachineScope bool
	OutDir       string
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

	resolvedByName := make(map[string]string, len(resolvedNames))
	for _, name := range resolvedNames {
		resolvedByName[resourceKeyName(name)] = name
	}

	// Artifacts that would be written = missing from disk (need sync)
	writtenNames := make(map[string]bool)
	for _, tr := range report.TargetReports() {
		for _, w := range tr.Written {
			writtenNames[resourceKeyForPath(w, resolvedByName)] = true
		}
	}
	for name := range writtenNames {
		dr.Missing = append(dr.Missing, DriftItem{Name: name, Reason: DriftMissing})
	}

	// Removed files = orphaned
	for _, tr := range report.TargetReports() {
		for _, r := range tr.Removed {
			dr.Orphaned = append(dr.Orphaned, DriftItem{
				Name:         resourceKeyForPath(r, resolvedByName),
				Reason:       DriftOrphaned,
				Path:         r,
				Target:       tr.Target,
				MachineScope: tr.MachineScope,
				OutDir:       tr.OutDir,
			})
		}
	}

	// Skills with error diagnostics = broken
	brokenSet := make(map[string]bool)
	for _, d := range report.Diags {
		if !d.IsError() || d.Resource == "" {
			continue
		}
		resource := d.Resource
		if key, ok := resolvedByName[d.Resource]; ok {
			resource = key
		}
		if !brokenSet[resource] {
			brokenSet[resource] = true
			dr.Broken = append(dr.Broken, DriftItem{Name: resource, Reason: DriftBroken})
		}
	}

	// Remaining resolved skills are in sync
	resolvedSet := make(map[string]bool, len(resolvedNames))
	for _, name := range resolvedNames {
		resolvedSet[name] = true
	}
	for name := range resolvedSet {
		if !writtenNames[name] && !brokenSet[name] {
			dr.Synced = append(dr.Synced, DriftItem{Name: name, Reason: DriftInSync})
		}
	}

	// The report is consumed by UI surfaces that render items in slice order,
	// and several derivations above iterate maps. Sort every bucket so two
	// runs over the same report produce byte-identical orderings (stable, so
	// report order survives inside an equal key).
	sort.SliceStable(dr.Missing, func(i, j int) bool { return dr.Missing[i].Name < dr.Missing[j].Name })
	sort.SliceStable(dr.Broken, func(i, j int) bool { return dr.Broken[i].Name < dr.Broken[j].Name })
	sort.SliceStable(dr.Synced, func(i, j int) bool { return dr.Synced[i].Name < dr.Synced[j].Name })
	sort.SliceStable(dr.Orphaned, func(i, j int) bool {
		return driftOrphanIdentityKey(dr.Orphaned[i]) < driftOrphanIdentityKey(dr.Orphaned[j])
	})

	return dr
}

// driftOrphanIdentityKey orders orphan outputs by their full
// (resource, target, scope, outDir, path) identity: that tuple is the only
// identity an unclaimed output has, and it is also the key TUI row-building
// deduplicates on, so the two can never disagree about whether two orphans
// are "the same".
func driftOrphanIdentityKey(item DriftItem) string {
	return strings.Join([]string{
		item.Name,
		string(item.Target),
		fmt.Sprint(item.MachineScope),
		item.OutDir,
		item.Path,
	}, "\x00")
}

func resourceKeyForPath(path string, resolvedByName map[string]string) string {
	name := filePathToSkillName(path)
	if key, ok := resolvedByName[name]; ok {
		return key
	}
	kind := filePathResourceKind(path)
	switch kind {
	case ir.KindSkill, ir.KindCommand, ir.KindSubagent, ir.KindHook, ir.KindMcpServer:
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
	parts := strings.Split(path, "/")
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
	default:
		return -1
	}
}

// filePathToSkillName extracts a skill name from an artifact path.
// For Claude target, paths look like "skills/<name>/<name>.md" or
// similar harness-specific layouts. We use the top-level directory name.
func filePathToSkillName(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) >= 2 {
		// If the path starts with a known directory like "skills/",
		// return the next component.
		for _, prefix := range []string{"skills", "agents", "commands"} {
			if parts[0] == prefix {
				return parts[1]
			}
		}
	}
	if len(parts) >= 1 {
		return parts[0]
	}
	return path
}
