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

func TestReportLevelDiagnosticPreservesSourceAndOnlyUsesUniqueRoot(t *testing.T) {
	tests := []struct {
		name       string
		reports    []TargetReport
		diagnostic diag.Diagnostic
		wantPath   string
		wantScope  string
		wantRoot   string
	}{
		{
			name: "unique project resource",
			reports: []TargetReport{{
				Target: harnesscompilation.TargetClaude, RootID: "project-claude",
				ResourceByPath: map[string]string{"skills/a/SKILL.md": "alpha"},
			}},
			diagnostic: diag.Diagnostic{Code: "materialize.write.error", Target: string(harnesscompilation.TargetClaude), Resource: "alpha", Source: diag.SourceRef{File: "skills/alpha/SKILL.md"}},
			wantPath:   "skills/alpha/SKILL.md", wantScope: "project", wantRoot: "project-claude",
		},
		{
			name: "ambiguous project and machine resource",
			reports: []TargetReport{
				{Target: harnesscompilation.TargetClaude, RootID: "project-claude", ResourceByPath: map[string]string{"skills/a/SKILL.md": "alpha"}},
				{Target: harnesscompilation.TargetClaude, MachineScope: true, RootID: "machine-claude", ResourceByPath: map[string]string{"skills/a/SKILL.md": "alpha"}},
			},
			diagnostic: diag.Diagnostic{Code: "materialize.write.error", Target: string(harnesscompilation.TargetClaude), Resource: "alpha", Source: diag.SourceRef{File: "skills/alpha/SKILL.md"}},
			wantPath:   "skills/alpha/SKILL.md",
		},
		{
			name: "target-wide resource-less failure",
			reports: []TargetReport{
				{Target: harnesscompilation.TargetClaude, RootID: "project-claude"},
				{Target: harnesscompilation.TargetClaude, MachineScope: true, RootID: "machine-claude"},
			},
			diagnostic: diag.Diagnostic{Code: "materialize.destination.error", Target: string(harnesscompilation.TargetClaude), Source: diag.SourceRef{File: "mcp.json"}},
			wantPath:   "mcp.json",
		},
		{
			name: "unique machine resource",
			reports: []TargetReport{{
				Target: harnesscompilation.TargetClaude, MachineScope: true, RootID: "machine-claude",
				ResourceByPath: map[string]string{"skills/a/SKILL.md": "alpha"},
			}},
			diagnostic: diag.Diagnostic{Code: "materialize.write.error", Target: string(harnesscompilation.TargetClaude), Resource: "alpha", Source: diag.SourceRef{File: "skills/alpha/SKILL.md"}},
			wantPath:   "skills/alpha/SKILL.md", wantScope: "machine", wantRoot: "machine-claude",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &Report{PerRoot: tt.reports, Diags: []diag.Diagnostic{tt.diagnostic}}
			report.rebuildIssues()
			if len(report.Issues) != 1 {
				t.Fatalf("issues = %#v, want one report-level issue", report.Issues)
			}
			issue := report.Issues[0]
			if issue.Path != tt.wantPath || issue.Scope != tt.wantScope || issue.RootID != tt.wantRoot {
				t.Fatalf("issue identity = path %q scope %q root %q, want %q/%q/%q", issue.Path, issue.Scope, issue.RootID, tt.wantPath, tt.wantScope, tt.wantRoot)
			}
		})
	}
}

func TestStructuredIssueFieldsUseSharedSafeSanitizer(t *testing.T) {
	long := strings.Repeat("x", maxMaterializationIssueField+20)
	unsafe := MaterializationIssue{
		Resource: "resource\n\x1b[31m",
		Target:   harnesscompilation.Target("target\u202ehidden"), Scope: "project\u2028machine", RootID: "root\x7f",
		Path: "source\x00file", Operation: MaterializationOperation("write\runsafe"), Outcome: MaterializationOutcome("failed\u0085unsafe"),
		Code: "materialize.bad\x1b[31m", Severity: diag.SeverityError, Message: long + "\ufffd\u2029tail",
	}
	report := &Report{Issues: []MaterializationIssue{unsafe}}
	surfaced := report.SurfaceIssues()
	if len(surfaced) != 1 {
		t.Fatalf("surface issues = %#v, want one sanitized error", surfaced)
	}
	got := report.Issues[0]
	for name, field := range map[string]string{
		"resource": got.Resource, "target": string(got.Target), "scope": got.Scope, "root": got.RootID,
		"path": got.Path, "operation": string(got.Operation), "outcome": string(got.Outcome), "code": got.Code, "message": got.Message,
	} {
		if strings.ContainsAny(field, "\x00\x01\x1b\r\n\x7f") || strings.Contains(field, "\u202e") || strings.Contains(field, "\u2028") || strings.Contains(field, "\u2029") || strings.Contains(field, "\u0085") || strings.Contains(field, "\ufffd") {
			t.Errorf("%s retained unsafe text: %q", name, field)
		}
	}
	if len(got.Message) > maxMaterializationIssueMessage || len(got.Code) > maxMaterializationIssueField {
		t.Fatalf("sanitized fields were not bounded: message=%d code=%d", len(got.Message), len(got.Code))
	}
	formatted := FormatIssue(unsafe)
	if strings.ContainsAny(formatted, "\x00\x01\x1b\r\n\x7f") || strings.Contains(formatted, "\u202e") || strings.Contains(formatted, "\u2028") || strings.Contains(formatted, "\u2029") || strings.Contains(formatted, "\u0085") || strings.Contains(formatted, "\ufffd") {
		t.Fatalf("formatted issue retained unsafe text: %q", formatted)
	}
	if len(formatted) > maxMaterializationIssueMessage+8*maxMaterializationIssueField {
		t.Fatalf("formatted issue was not bounded: %d bytes", len(formatted))
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
