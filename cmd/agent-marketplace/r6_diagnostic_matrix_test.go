package main

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// TestR6DiagnosticProjectionMatrix is independently authored against the
// externally visible diagnostic contract. It intentionally combines a hostile
// pre-populated issue, ownership diagnostics from both root and report seams,
// a generic compiler diagnostic, and diagnostics distinguished only by target
// and severity. Keeping these cases together prevents one projection from
// silently repairing a different projection's identity.
func TestR6DiagnosticProjectionMatrix(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	alphaPath := "registry/alpha/SKILL.md"
	report := &materialize.Report{
		PerRoot: []materialize.TargetReport{
			{
				Target: hcc.TargetClaude, RootID: "project-claude",
				ResourceByPath: map[string]string{"skills/alpha/SKILL.md": "alpha"},
			},
			{
				Target: hcc.TargetClaude, MachineScope: true, RootID: "machine-claude",
				ResourceByPath: map[string]string{"skills/alpha/SKILL.md": "alpha"},
			},
			{
				Target: hcc.TargetFactory, RootID: "project-factory",
				ResourceByPath: map[string]string{"skills/beta/SKILL.md": "beta"},
				Diags: []diag.Diagnostic{{
					Code: diag.CodeOwnershipStateCorrupt, Severity: diag.SeverityError,
					Resource: "beta", Message: "ownership state is corrupt", Source: diag.SourceRef{File: "registry/beta/SKILL.md"},
				}},
			},
		},
		Diags: []diag.Diagnostic{
			{
				Code: diag.CodeOwnershipStateCorrupt, Severity: diag.SeverityError,
				Target: string(hcc.TargetClaude), Resource: "alpha", Message: "ownership state is corrupt", Source: diag.SourceRef{File: alphaPath},
			},
			{
				Code: diag.CodeRuleDevinUnsupported, Severity: diag.SeverityWarning,
				Target: string(hcc.TargetDevin), Resource: "generic", Message: "generic compiler warning",
			},
		},
		Issues: []materialize.MaterializationIssue{{
			Resource: "hostile\nresource", Target: hcc.TargetClaude, Scope: "project", RootID: "hostile-root",
			Operation: materialize.MaterializationOperationSkip, Outcome: materialize.MaterializationOutcomeWarning,
			Code: diag.CodeOwnershipUnmanagedKey, Severity: diag.SeverityWarning,
			Message: "preserve\x00 this\u202e entry",
		}},
	}
	repeatedPrefix := strings.Repeat("x", 1100)
	report.Issues = append(report.Issues,
		materialize.MaterializationIssue{Resource: "long", Target: hcc.TargetClaude, Code: diag.CodeOwnershipEntryDrift, Severity: diag.SeverityError, Operation: materialize.MaterializationOperationFailure, Outcome: materialize.MaterializationOutcomeFailed, Message: repeatedPrefix + "A"},
		materialize.MaterializationIssue{Resource: "long", Target: hcc.TargetClaude, Code: diag.CodeOwnershipEntryDrift, Severity: diag.SeverityError, Operation: materialize.MaterializationOperationFailure, Outcome: materialize.MaterializationOutcomeFailed, Message: repeatedPrefix + "B"},
	)

	// The same compiler fact with a different target and severity is still two
	// public diagnostics. Its rendered message is intentionally identical.
	base := diag.Diagnostic{Code: "compiler.same", Resource: "identity", Message: "same fact"}
	identityDiagnostics := []error{
		pipeline.PromoteDiagnostic(withTargetAndSeverity(base, string(hcc.TargetClaude), diag.SeverityError)),
		pipeline.PromoteDiagnostic(withTargetAndSeverity(base, string(hcc.TargetFactory), diag.SeverityWarning)),
	}
	if got := len(pipeline.DedupIssues(identityDiagnostics)); got != 2 {
		t.Fatalf("DedupIssues collapsed target/severity distinctions: got %d, want 2", got)
	}

	surfaced := report.SurfaceIssues()
	if len(surfaced) != 5 {
		t.Fatalf("SurfaceIssues = %#v, want hostile, two long-identity, alpha, and beta issues", surfaced)
	}
	longIssues := 0
	var alpha, beta, hostile materialize.MaterializationIssue
	for _, issue := range surfaced {
		if issue.Resource == "long" {
			longIssues++
		}
		switch issue.Resource {
		case "alpha":
			alpha = issue
		case "beta":
			beta = issue
		case "hostile resource":
			hostile = issue
		}
	}
	if longIssues != 2 {
		t.Fatalf("long diagnostics collapsed after display bounding: got %d, want 2", longIssues)
	}
	if alpha.Path != alphaPath || alpha.Scope != "" || alpha.RootID != "" {
		t.Fatalf("ambiguous alpha attribution = %#v, want source path and no scope/root", alpha)
	}
	if beta.Path != "registry/beta/SKILL.md" || beta.Scope != "project" || beta.RootID != "project-factory" {
		t.Fatalf("unique beta attribution = %#v, want source path and unique root attribution", beta)
	}
	if strings.ContainsAny(hostile.Resource+hostile.Message, "\x00\n") || strings.Contains(hostile.Resource, "\u202e") || strings.Contains(hostile.Message, "\u202e") {
		t.Fatalf("pre-populated issue was not sanitized: %#v", hostile)
	}

	warnings, failures := materializationIssueErrors(report)
	if len(warnings) != 1 || len(failures) != 4 {
		t.Fatalf("materializationIssueErrors = %d warnings, %d failures, want 1/4", len(warnings), len(failures))
	}
	for _, issue := range append(warnings, failures...) {
		if strings.Contains(issue.Error(), diag.CodeRuleDevinUnsupported) {
			t.Fatalf("generic compiler diagnostic entered structured renderer: %v", issue)
		}
	}
	normal := reportDiagnosticErrors(report, diag.SeverityWarning)
	if len(normal) != 1 || !strings.Contains(normal[0].Error(), diag.CodeRuleDevinUnsupported) {
		t.Fatalf("generic compiler diagnostic did not remain on normal renderer: %#v", normal)
	}

	drift := materialize.DriftReportFromReport(report, []string{"skill\x00alpha", "skill\x00beta", "skill\x00gamma"})
	if !containsDrift(drift.Broken, "skill\x00alpha") || !containsDrift(drift.Broken, "skill\x00beta") {
		t.Fatalf("DriftReportFromReport lost ownership failures: %#v", drift)
	}
	if !containsDrift(drift.Synced, "skill\x00gamma") || len(drift.Missing) != 0 {
		t.Fatalf("DriftReportFromReport projection = %#v, want only gamma synced and no missing", drift)
	}
}

func withTargetAndSeverity(d diag.Diagnostic, target string, severity diag.Severity) diag.Diagnostic {
	d.Target, d.Severity = target, severity
	return d
}

func containsDrift(items []materialize.DriftItem, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}
