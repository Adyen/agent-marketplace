package materialize

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// The types below provide backward-compatible drift information for consumers
// (primarily the TUI) that were built around the internal/drift package.
// They are derived from structured materialize.Report issues rather than
// guessing a resource from an output path.

type DriftReason int

const (
	DriftMissing DriftReason = iota
	DriftOrphaned
	DriftBroken
	DriftInSync
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

type DriftItem struct {
	Name   string
	Reason DriftReason
}

type DriftReport struct {
	Missing  []DriftItem
	Orphaned []DriftItem
	Broken   []DriftItem
	Synced   []DriftItem
}

func (r DriftReport) InSync() bool {
	return len(r.Missing)+len(r.Orphaned)+len(r.Broken) == 0
}

func (r DriftReport) PendingCount() int {
	return len(r.Missing) + len(r.Orphaned) + len(r.Broken)
}

// SurfaceIssues returns the report-owned materialization warnings and errors
// that command surfaces should display. Informational write/skip/remove
// records remain available through Issues for drift and TUI consumers, but do
// not become warnings merely because a command inspected the report.
//
// The projection is deduplicated and sorted here so sync, status, and list do
// not each reconstruct identity from paths or accidentally print one issue for
// both a root-local and target aggregate report.
func (r *Report) SurfaceIssues() []MaterializationIssue {
	if r == nil {
		return nil
	}
	r.rebuildIssues()
	seen := make(map[string]struct{}, len(r.Issues))
	issues := make([]MaterializationIssue, 0, len(r.Issues))
	for i := range r.Issues {
		r.Issues[i] = sanitizeMaterializationIssue(r.Issues[i])
		issue := r.Issues[i]
		if issue.Code == "" || !IsMaterializationDiagnosticCode(issue.Code) || (issue.Severity != diag.SeverityWarning && issue.Severity != diag.SeverityError) {
			continue
		}
		key := materializationIssueKey(issue)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		issues = append(issues, issue)
	}
	sort.Slice(issues, func(i, j int) bool {
		left, right := issues[i], issues[j]
		for _, pair := range [][2]string{
			{string(left.Target), string(right.Target)},
			{left.Scope, right.Scope},
			{left.RootID, right.RootID},
			{left.Resource, right.Resource},
			{left.Path, right.Path},
			{left.Code, right.Code},
			{string(left.Operation), string(right.Operation)},
			{string(left.Outcome), string(right.Outcome)},
			{left.Message, right.Message},
			{left.Severity.String(), right.Severity.String()},
		} {
			if pair[0] != pair[1] {
				return pair[0] < pair[1]
			}
		}
		return false
	})
	return issues
}

// FormatIssue renders one structured issue for command output. The identity
// fields are deliberately explicit: prose is bounded at the materialization
// seam, while code/severity and resource/target/scope/root remain stable.
func FormatIssue(issue MaterializationIssue) string {
	issue = sanitizeMaterializationIssue(issue)
	var b strings.Builder
	b.WriteString(issue.Code)
	b.WriteString(": ")
	b.WriteString(boundedMaterializationMessage(issue.Message))
	b.WriteString(" (severity=")
	b.WriteString(issue.Severity.String())
	if issue.Resource != "" {
		b.WriteString(" resource=")
		b.WriteString(strconv.Quote(boundedMaterializationMessage(issue.Resource)))
	}
	if issue.Target != "" {
		b.WriteString(" target=")
		b.WriteString(string(issue.Target))
	}
	if issue.Scope != "" {
		b.WriteString(" scope=")
		b.WriteString(issue.Scope)
	}
	if issue.RootID != "" {
		b.WriteString(" root=")
		b.WriteString(boundedMaterializationMessage(issue.RootID))
	}
	if issue.Path != "" {
		b.WriteString(" path=")
		b.WriteString(strconv.Quote(boundedMaterializationMessage(issue.Path)))
	}
	if issue.Operation != "" {
		b.WriteString(" operation=")
		b.WriteString(string(issue.Operation))
	}
	if issue.Outcome != "" {
		b.WriteString(" outcome=")
		b.WriteString(string(issue.Outcome))
	}
	b.WriteByte(')')
	return b.String()
}

// DriftReportFromReport derives drift from the report's structured issues.
// Warnings remain visible on the report but do not create pending drift;
// errors create one broken subject and suppress any missing/synced duplicate.
func DriftReportFromReport(report *Report, resolvedNames []string) DriftReport {
	if report == nil {
		return DriftReport{}
	}
	report.rebuildIssues()

	resolvedByName := make(map[string]string, len(resolvedNames))
	for _, name := range resolvedNames {
		resolvedByName[resourceKeyName(name)] = name
	}
	var dr DriftReport
	missing := make(map[string]struct{})
	orphaned := make(map[string]struct{})
	broken := make(map[string]struct{})
	add := func(items *[]DriftItem, seen map[string]struct{}, issue MaterializationIssue, reason DriftReason) {
		name := structuredIssueSubject(issue, resolvedByName)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		*items = append(*items, DriftItem{Name: name, Reason: reason})
	}
	for _, issue := range report.Issues {
		switch {
		case issue.Operation == MaterializationOperationWrite && issue.Outcome == MaterializationOutcomeWritten:
			add(&dr.Missing, missing, issue, DriftMissing)
		case issue.Operation == MaterializationOperationRemove && issue.Outcome == MaterializationOutcomeRemoved:
			add(&dr.Orphaned, orphaned, issue, DriftOrphaned)
		case issue.Operation == MaterializationOperationFailure && issue.Severity == diag.SeverityError:
			add(&dr.Broken, broken, issue, DriftBroken)
		}
	}
	// Generic compiler errors remain outside the materialization SurfaceIssues
	// projection, but still make their resource broken for drift consumers.
	for _, d := range report.Diags {
		if !d.IsError() || IsMaterializationDiagnostic(d) {
			continue
		}
		add(&dr.Broken, broken, MaterializationIssue{Resource: d.Resource, Target: hcc.Target(d.Target)}, DriftBroken)
	}
	// A broken resource is not also missing or in sync. Remove it from the
	// earlier projections without relying on output path parsing.
	if len(broken) > 0 {
		filtered := dr.Missing[:0]
		for _, item := range dr.Missing {
			if _, ok := broken[item.Name]; !ok {
				filtered = append(filtered, item)
			}
		}
		dr.Missing = filtered
	}
	for name := range resolvedByName {
		key := resolvedByName[name]
		if _, ok := broken[key]; ok {
			continue
		}
		if _, ok := missing[key]; ok {
			continue
		}
		dr.Synced = append(dr.Synced, DriftItem{Name: key, Reason: DriftInSync})
	}
	sort.Slice(dr.Synced, func(i, j int) bool { return dr.Synced[i].Name < dr.Synced[j].Name })
	return dr
}

func structuredIssueSubject(issue MaterializationIssue, resolvedByName map[string]string) string {
	subject := issue.Resource
	if subject == "" {
		// Path is already a structured field from the materialization seam. It is
		// a safe bounded fallback for target-wide issues without a resource.
		subject = issue.Path
	}
	if key, ok := resolvedByName[subject]; ok {
		return key
	}
	return subject
}

func resourceKeyName(key string) string {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return key
}
