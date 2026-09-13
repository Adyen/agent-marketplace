package materialize

import (
	"strings"
	"testing"

	harnesscompilation "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func TestReportStructuredIssuesPreserveSubjectAndOperation(t *testing.T) {
	report := &Report{
		PerRoot: []TargetReport{
			{
				Target:       harnesscompilation.TargetClaude,
				MachineScope: false,
				RootID:       "project-claude",
				Written:      []string{"skills/review/SKILL.md"},
				Skipped:      []string{"skills/unchanged/SKILL.md"},
				Removed:      []string{"skills/old/SKILL.md"},
				ResourceByPath: map[string]string{
					"skills/review/SKILL.md":    "review",
					"skills/unchanged/SKILL.md": "unchanged",
					"skills/old/SKILL.md":       "old",
				},
				Diags: []diag.Diagnostic{{
					Code:     "materialize.write.error",
					Severity: diag.SeverityError,
					Resource: "review",
					Message:  strings.Repeat("x", maxMaterializationIssueMessage+20),
				}},
			},
		},
	}
	report.rebuildIssues()
	if len(report.Issues) != 4 {
		t.Fatalf("issues = %d, want one each for write/skip/remove/failure: %#v", len(report.Issues), report.Issues)
	}
	want := map[MaterializationOperation]MaterializationIssue{
		MaterializationOperationWrite:   {Resource: "review", Target: harnesscompilation.TargetClaude, Scope: "project", RootID: "project-claude", Path: "skills/review/SKILL.md", Operation: MaterializationOperationWrite, Outcome: MaterializationOutcomeWritten, Severity: diag.SeverityInfo},
		MaterializationOperationSkip:    {Resource: "unchanged", Target: harnesscompilation.TargetClaude, Scope: "project", RootID: "project-claude", Path: "skills/unchanged/SKILL.md", Operation: MaterializationOperationSkip, Outcome: MaterializationOutcomeSkipped, Severity: diag.SeverityInfo},
		MaterializationOperationRemove:  {Resource: "old", Target: harnesscompilation.TargetClaude, Scope: "project", RootID: "project-claude", Path: "skills/old/SKILL.md", Operation: MaterializationOperationRemove, Outcome: MaterializationOutcomeRemoved, Severity: diag.SeverityInfo},
		MaterializationOperationFailure: {Resource: "review", Target: harnesscompilation.TargetClaude, Scope: "project", RootID: "project-claude", Operation: MaterializationOperationFailure, Outcome: MaterializationOutcomeFailed, Code: "materialize.write.error", Severity: diag.SeverityError},
	}
	for _, issue := range report.Issues {
		expected, ok := want[issue.Operation]
		if !ok {
			t.Fatalf("unexpected operation: %#v", issue)
		}
		if issue.Resource != expected.Resource || issue.Target != expected.Target || issue.Scope != expected.Scope || issue.RootID != expected.RootID || issue.Path != expected.Path || issue.Outcome != expected.Outcome || issue.Code != expected.Code || issue.Severity != expected.Severity {
			t.Errorf("issue = %#v, want identity fields from %#v", issue, expected)
		}
		if issue.Operation == MaterializationOperationFailure && len(issue.Message) > maxMaterializationIssueMessage {
			t.Errorf("failure message length = %d, exceeds bounded limit", len(issue.Message))
		}
	}
}

func TestStructuredIssuesPreserveDistinctDiagnosticsAndSkippedPath(t *testing.T) {
	report := &Report{PerRoot: []TargetReport{{
		Target:  harnesscompilation.TargetDevin,
		Skipped: []string{"skills/rules/SKILL.md"},
		ResourceByPath: map[string]string{
			"skills/rules/SKILL.md": "rules",
		},
		Diags: []diag.Diagnostic{
			{Code: "materialize.collision.user-owned", Severity: diag.SeverityWarning, Resource: "rules", Message: "destination is user-owned"},
			{Code: "materialize.skill.user-owned-preserved", Severity: diag.SeverityWarning, Resource: "rules", Message: "skill directory is user-owned"},
			{Code: diag.CodeRuleDevinUnsupported, Severity: diag.SeverityWarning, Resource: "rules", Message: "rules are out of scope"},
		},
	}}}
	report.rebuildIssues()
	if len(report.Issues) != 3 {
		t.Fatalf("issues = %d, want skip plus two distinct materialization diagnostics: %#v", len(report.Issues), report.Issues)
	}
	var surfaced []MaterializationIssue
	for _, issue := range report.SurfaceIssues() {
		if issue.Resource == "rules" {
			surfaced = append(surfaced, issue)
		}
	}
	if len(surfaced) != 2 {
		t.Fatalf("surfaced diagnostics = %#v, want both distinct materialization-owned diagnostics", surfaced)
	}
	for _, issue := range surfaced {
		if issue.Path != "skills/rules/SKILL.md" {
			t.Fatalf("surfaced diagnostic = %#v, want exact skipped path", issue)
		}
	}
	codes := map[string]bool{}
	for _, issue := range surfaced {
		codes[issue.Code] = true
	}
	for _, code := range []string{"materialize.collision.user-owned", "materialize.skill.user-owned-preserved"} {
		if !codes[code] {
			t.Fatalf("surfaced diagnostics lost %q: %#v", code, surfaced)
		}
	}
}

func TestStructuredIssuesKeepTargetAndScopeDistinct(t *testing.T) {
	report := &Report{PerRoot: []TargetReport{
		{Target: harnesscompilation.TargetClaude, RootID: "project", Written: []string{"skills/review/SKILL.md"}, ResourceByPath: map[string]string{"skills/review/SKILL.md": "review"}},
		{Target: harnesscompilation.TargetClaude, MachineScope: true, RootID: "machine", Written: []string{"skills/review/SKILL.md"}, ResourceByPath: map[string]string{"skills/review/SKILL.md": "review"}},
		{Target: harnesscompilation.TargetFactory, RootID: "factory", Written: []string{"skills/review/SKILL.md"}, ResourceByPath: map[string]string{"skills/review/SKILL.md": "review"}},
	}}
	report.rebuildIssues()
	if len(report.Issues) != 3 {
		t.Fatalf("issues = %d, want three distinct target/scope records: %#v", len(report.Issues), report.Issues)
	}
	seen := make(map[string]bool)
	for _, issue := range report.Issues {
		seen[string(issue.Target)+"/"+issue.Scope+"/"+issue.RootID] = true
	}
	for _, key := range []string{"claude/project/project", "claude/machine/machine", "factory/project/factory"} {
		if !seen[key] {
			t.Errorf("missing structured target/scope/root identity %q in %#v", key, report.Issues)
		}
	}
}

func TestDriftUsesStructuredIssuesAndSuppressesDuplicates(t *testing.T) {
	resource := "skill\x00review"
	issue := MaterializationIssue{Resource: "review", Target: harnesscompilation.TargetClaude, Scope: "project", RootID: "project", Path: "not-a-resource-name", Operation: MaterializationOperationWrite, Outcome: MaterializationOutcomeWritten}
	warning := MaterializationIssue{Resource: "review", Target: harnesscompilation.TargetClaude, Scope: "project", RootID: "project", Path: "skills/review/SKILL.md", Operation: MaterializationOperationSkip, Outcome: MaterializationOutcomeWarning, Code: "materialize.collision.user-owned", Severity: diag.SeverityWarning}
	errorIssue := MaterializationIssue{Resource: "review", Target: harnesscompilation.TargetClaude, Scope: "project", RootID: "project", Operation: MaterializationOperationFailure, Outcome: MaterializationOutcomeFailed, Code: "materialize.write.error", Severity: diag.SeverityError}
	report := &Report{PerRoot: []TargetReport{{Issues: []MaterializationIssue{issue, warning, errorIssue, issue}}}}
	dr := DriftReportFromReport(report, []string{resource})
	if len(dr.Missing) != 0 {
		t.Fatalf("broken subject must not also be missing: %#v", dr.Missing)
	}
	if len(dr.Broken) != 1 || dr.Broken[0].Name != resource {
		t.Fatalf("broken = %#v, want one structured subject %q", dr.Broken, resource)
	}
	if len(dr.Synced) != 0 {
		t.Fatalf("broken subject must not be synced: %#v", dr.Synced)
	}
}

func TestDriftWarningDoesNotCreatePendingItem(t *testing.T) {
	report := &Report{Issues: []MaterializationIssue{{
		Resource: "review", Operation: MaterializationOperationSkip, Outcome: MaterializationOutcomeWarning,
		Code: "materialize.collision.user-owned", Severity: diag.SeverityWarning,
	}}}
	dr := DriftReportFromReport(report, []string{"skill\x00review"})
	if !dr.InSync() || len(dr.Synced) != 1 {
		t.Fatalf("warning-only structured issue drift = %#v, want in-sync with one synced subject", dr)
	}
}
