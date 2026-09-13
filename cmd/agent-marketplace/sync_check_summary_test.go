package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/materialize"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// ADVERSARIAL: the bead's headline symptom was "`sync --check` exited 1 with
// zero drift". The severity fix removed the WARNING half of that symptom, but
// the reporting half survives for a genuine error: Report.InSync() is false
// because of an error diagnostic, pendingCount is 0, and the user gets
//
//	sync: 0 out of sync
//	Error: 0 skills out of sync
//
// with the actual error diagnostic never printed. Exit 1 is correct; the
// message is not, and it is indistinguishable from the bug that was just fixed.

func TestSyncCheckSummaryNamesTheErrorInsteadOfClaimingZeroDrift(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	report := &materialize.Report{
		Diags: []diag.Diagnostic{
			{Code: diag.CodeRuleDevinUnsupported, Severity: diag.SeverityWarning, Resource: "some-rule", Message: "rules are out of scope for devin"},
			{Code: "materialize.write.error", Severity: diag.SeverityError, Resource: ".windsurf/mcp_config.json", Message: "prune managed entries: permission denied"},
		},
	}
	if report.InSync() {
		t.Fatal("precondition: the report must be out of sync")
	}

	var buf bytes.Buffer
	err := writeSyncCheckSummary(&buf, report, nil, nil)
	if err == nil {
		t.Fatal("writeSyncCheckSummary returned nil: sync --check must exit 1 on an error diagnostic")
	}
	out := buf.String()

	if strings.Contains(out, "sync: 0 out of sync") {
		t.Errorf("summary claims zero drift for an error-only report:\n%s", out)
	}
	if !strings.Contains(out, "materialize.write.error") {
		t.Errorf("summary never names the error diagnostic that caused the failure:\n%s", out)
	}
	if !strings.Contains(out, "permission denied") {
		t.Errorf("summary never shows the error message:\n%s", out)
	}
	if strings.Contains(err.Error(), "0 skills out of sync") {
		t.Errorf("returned error = %q, want it to describe the materialization error, not zero drift", err)
	}
	// A warning must not be reported as a failure cause.
	if strings.Contains(out, diag.CodeRuleDevinUnsupported) {
		t.Errorf("summary reported a WARNING as a failure cause:\n%s", out)
	}
}

func TestSyncCheckSummaryDeduplicatesStructuredAndGenericDiagnostics(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	longMessage := strings.Repeat("compiler detail ", 300)
	structured := materialize.MaterializationIssue{
		Resource: "materialized", Target: hcc.TargetClaude, Scope: "project", RootID: "root",
		Operation: materialize.MaterializationOperationFailure, Outcome: materialize.MaterializationOutcomeFailed,
		Code: "materialize.write.error", Severity: diag.SeverityError, Message: "permission denied",
	}
	generic := diag.Diagnostic{Code: "hcc.generic", Severity: diag.SeverityError, Resource: "compiled", Message: longMessage}
	report := &materialize.Report{
		Diags:  []diag.Diagnostic{generic, generic},
		Issues: []materialize.MaterializationIssue{structured, structured},
	}
	var buf bytes.Buffer
	if err := writeSyncCheckSummary(&buf, report, nil, nil); err == nil {
		t.Fatal("duplicate diagnostic report must return an error")
	}
	out := buf.String()
	if got := strings.Count(out, structured.Code); got != 1 {
		t.Fatalf("structured diagnostic rendered %d times, want one: %q", got, out)
	}
	if got := strings.Count(out, generic.Code); got != 1 {
		t.Fatalf("generic diagnostic rendered %d times, want one: %q", got, out)
	}
	if strings.Contains(out, longMessage) {
		t.Fatal("generic diagnostic emitted an unbounded raw message after normal rendering")
	}
	if !strings.Contains(out, "compiled") {
		t.Fatalf("generic diagnostic lost normal resource identity: %q", out)
	}
}

func TestSyncCheckSummaryKeepsTheDriftWording(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	report := driftOnlyReport(t)

	var buf bytes.Buffer
	err := writeSyncCheckSummary(&buf, report, nil, nil)
	if err == nil {
		t.Fatal("drifted report must return an error")
	}
	out := buf.String()
	if !strings.Contains(out, "out of sync") {
		t.Fatalf("drift summary lost its wording:\n%s", out)
	}
	if !strings.Contains(out, "missing") {
		t.Fatalf("drift summary lost the missing breakdown:\n%s", out)
	}
	if !strings.Contains(err.Error(), "out of sync") {
		t.Fatalf("returned error = %q, want the drift wording", err)
	}
}

func TestSyncCheckSummaryNamesCodexRootsAndArtifacts(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	report := &materialize.Report{PerRoot: []materialize.TargetReport{
		{RootID: "machine-agents", OutDir: "/home/.agents", Removed: []string{"skills/machine-skill"}},
		{RootID: "machine-codex", OutDir: "/opt/codex", Removed: []string{"agents/machine-agent.toml", "hooks.json", "config.toml"}},
	}}

	var buf bytes.Buffer
	if err := writeSyncCheckSummary(&buf, report, nil, nil); err == nil {
		t.Fatal("drifted report must return an error")
	}
	out := buf.String()
	for _, want := range []string{
		"machine-agents", "skills/machine-skill",
		"machine-codex", "agents/machine-agent.toml", "hooks.json", "config.toml",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("sync --check output missing %q:\n%s", want, out)
		}
	}
}

// driftOnlyReport builds a report whose only drift is a file to write.
func driftOnlyReport(t *testing.T) *materialize.Report {
	t.Helper()
	return &materialize.Report{
		PerRoot: []materialize.TargetReport{{
			OutDir:  "/repo/.windsurf",
			Written: []string{"skills/some-skill/SKILL.md"},
		}},
	}
}

func TestGenericCompilerWarningStaysOnNormalRenderer(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	report := &materialize.Report{Diags: []diag.Diagnostic{{
		Code: diag.CodeRuleDevinUnsupported, Severity: diag.SeverityWarning,
		Resource: "some-rule", Message: "rules are out of scope for devin",
	}}}
	if got := report.SurfaceIssues(); len(got) != 0 {
		t.Fatalf("generic compiler warning entered SurfaceIssues: %#v", got)
	}
	normal := reportDiagnosticErrors(report, diag.SeverityWarning)
	if len(normal) != 1 || !strings.Contains(normal[0].Error(), diag.CodeRuleDevinUnsupported) || !strings.Contains(normal[0].Error(), "some-rule") {
		t.Fatalf("generic compiler warning lost normal rendering: %#v", normal)
	}
}

func TestSyncCheckSummarySilentWhenClean(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	report := &materialize.Report{Diags: []diag.Diagnostic{
		{Code: diag.CodeRuleDevinUnsupported, Severity: diag.SeverityWarning, Resource: "some-rule", Message: "rules are out of scope for devin"},
	}}
	var buf bytes.Buffer
	if err := writeSyncCheckSummary(&buf, report, nil, nil); err != nil {
		t.Fatalf("warning-only report must be in sync, got %v", err)
	}
	if buf.String() != "" {
		t.Fatalf("warning-only report printed %q, want nothing", buf.String())
	}
}

func TestMaterializationSurfaceProjectionPreservesIdentityOnce(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	warning := materialize.MaterializationIssue{
		Resource: "review", Target: hcc.TargetClaude, Scope: "project", RootID: "project-claude",
		Path: "skills/review/SKILL.md", Operation: materialize.MaterializationOperationSkip,
		Outcome: materialize.MaterializationOutcomeWarning, Code: "materialize.collision.user-owned",
		Severity: diag.SeverityWarning, Message: "destination is user-owned",
	}
	errorIssue := materialize.MaterializationIssue{
		Resource: "broken", Target: hcc.TargetClaude, Scope: "project", RootID: "project-claude",
		Operation: materialize.MaterializationOperationFailure, Outcome: materialize.MaterializationOutcomeFailed,
		Code: "materialize.write.error", Severity: diag.SeverityError, Message: "permission denied",
	}
	report := &materialize.Report{Issues: []materialize.MaterializationIssue{warning, warning, errorIssue}}
	var out bytes.Buffer
	if err := writeMaterializationIssues(&out, report); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if got := strings.Count(text, warning.Code); got != 1 {
		t.Fatalf("warning code count = %d, want one: %q", got, text)
	}
	if got := strings.Count(text, errorIssue.Code); got != 1 {
		t.Fatalf("error code count = %d, want one: %q", got, text)
	}
	for _, want := range []string{
		"warning:", "severity=warning", `resource="review"`, "target=claude", "scope=project",
		"root=project-claude", `path="skills/review/SKILL.md"`, "operation=skip", "outcome=warning",
		"error:", "severity=error", `resource="broken"`, "operation=failure", "outcome=failed",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("surface output missing %q:\n%s", want, text)
		}
	}
}

func TestMaterializationSurfaceGroupingKeepsUsableStatusInputs(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	report := &materialize.Report{Issues: []materialize.MaterializationIssue{
		{Resource: "warn", Target: hcc.TargetClaude, Scope: "project", RootID: "root", Operation: materialize.MaterializationOperationSkip, Outcome: materialize.MaterializationOutcomeWarning, Code: "materialize.warn", Severity: diag.SeverityWarning, Message: "lossy"},
		{Resource: "fail", Target: hcc.TargetClaude, Scope: "project", RootID: "root", Operation: materialize.MaterializationOperationFailure, Outcome: materialize.MaterializationOutcomeFailed, Code: "materialize.fail", Severity: diag.SeverityError, Message: "blocked"},
	}}
	warnings, failures := materializationIssueErrors(report)
	if len(warnings) != 1 || len(failures) != 1 {
		t.Fatalf("grouped materialization issues = %d warnings, %d failures, want 1/1", len(warnings), len(failures))
	}
	if !strings.Contains(warnings[0].Error(), "materialize.warn") || !strings.Contains(warnings[0].Error(), "resource=\"warn\"") {
		t.Fatalf("warning lost structured identity: %v", warnings[0])
	}
	if !strings.Contains(failures[0].Error(), "materialize.fail") || !strings.Contains(failures[0].Error(), "resource=\"fail\"") {
		t.Fatalf("failure lost structured identity: %v", failures[0])
	}
}
