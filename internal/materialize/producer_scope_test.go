package materialize

import (
	"errors"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func TestAppendTargetDiagnosticKeepsGenericDiagnosticAndStructuredRoot(t *testing.T) {
	key := outputKey{target: hcc.TargetClaude, machineScope: true}
	report := &Report{}
	diagnostic := diag.Diagnostic{
		Code:     "materialize.write.error",
		Severity: diag.SeverityError,
		Target:   string(key.target),
		Resource: "settings",
		Source:   diag.SourceRef{File: "/home/user/.claude/settings.json"},
		Message:  "failed to write /home/user/.claude/settings.json",
	}
	appendTargetDiagnostic(report, key, "machine-claude", diagnostic, ".claude/settings.json")

	if len(report.Diags) != 1 || report.Diags[0].Source.File != diagnostic.Source.File {
		t.Fatalf("generic diagnostic was mutated: %#v", report.Diags)
	}
	issues := report.SurfaceIssues()
	if len(issues) != 1 {
		t.Fatalf("surface issues = %#v, want one structured producer issue", issues)
	}
	issue := issues[0]
	if issue.Scope != "machine" || issue.RootID != "machine-claude" || issue.Path != ".claude/settings.json" {
		t.Fatalf("structured attribution = %#v, want machine root and safe relative path", issue)
	}
	if strings.Contains(FormatIssue(issue), "/home/user/.claude") {
		t.Fatalf("structured output exposed an absolute root: %q", FormatIssue(issue))
	}
}

func TestAppendGroupReportFailureKeepsKnownScopeAndRoot(t *testing.T) {
	key := outputKey{target: hcc.TargetCodex, machineScope: true}
	report := &Report{}
	appendGroupReport(report, key, "/home/user/.codex", ".codex/skills", TargetReport{
		RootID: "machine-codex",
		OutDir: "/home/user/.codex/skills",
	}, errors.New("permission denied"), "materialize target %s to %s")

	issues := report.SurfaceIssues()
	if len(issues) != 1 {
		t.Fatalf("surface issues = %#v, want one report-level failure", issues)
	}
	issue := issues[0]
	if issue.Target != hcc.TargetCodex || issue.Scope != "machine" || issue.RootID != "machine-codex" {
		t.Fatalf("report-level scope/root attribution = %#v, want codex machine root", issue)
	}
	if issue.Code != "materialize.write.error" || issue.Severity != diag.SeverityError || issue.Operation != MaterializationOperationFailure || issue.Outcome != MaterializationOutcomeFailed {
		t.Fatalf("report-level identity = %#v, want write failure", issue)
	}
	if issue.Path != ".codex/skills" {
		t.Fatalf("report-level path = %q, want output-relative report prefix", issue.Path)
	}
	formatted := FormatIssue(issue)
	if strings.Contains(formatted, "/home/user/.codex") {
		t.Fatalf("structured issue exposed absolute output root: %q", formatted)
	}
	if len(report.PerRoot) != 0 {
		t.Fatalf("failure unexpectedly fabricated a TargetReport: %#v", report.PerRoot)
	}

	projectReport := &Report{}
	appendGroupReport(projectReport, outputKey{target: hcc.TargetCodex}, "/repo/.codex", ".codex/skills", TargetReport{
		RootID: "project-codex",
		OutDir: "/repo/.codex/skills",
	}, errors.New("permission denied"), "materialize target %s to %s")
	projectIssues := projectReport.SurfaceIssues()
	if len(projectIssues) != 1 || projectIssues[0].Scope != "project" || projectIssues[0].RootID != "project-codex" {
		t.Fatalf("project report-level scope/root attribution = %#v, want project/project-codex", projectIssues)
	}
}
