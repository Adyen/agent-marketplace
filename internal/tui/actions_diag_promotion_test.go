package tui

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/materialize"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func toolUnknownDiag(resource string) diag.Diagnostic {
	return diag.Diagnostic{
		Code:     diag.CodeSkillDevinToolUnknown,
		Severity: diag.SeverityError,
		Target:   "devin",
		Resource: resource,
		Message:  "an allowed-tools entry carries a scoped tool specifier, which has no Devin equivalent; artifact suppressed",
		Hint:     "Devin's allowed tools (read, edit, grep, glob, exec) cannot be scoped to a command; remove the specifier or drop the explicit allowed-tools restriction",
		Source:   diag.SourceRef{File: "skills/" + resource + "/SKILL.md", Line: 4},
	}
}

func TestAppendReportDiagnosticsUsesStructuredMaterializationProjection(t *testing.T) {
	report := &materialize.Report{
		Issues: []materialize.MaterializationIssue{{
			Code: diag.CodeOwnershipUnmanagedKey, Severity: diag.SeverityWarning,
			Resource: "server", Target: hcc.TargetDevin, Scope: "machine", RootID: "machine-devin",
			Path: "mcp_config.json", Operation: materialize.MaterializationOperationSkip,
			Outcome: materialize.MaterializationOutcomeWarning, Message: "preserved handwritten entry",
		}},
		Diags: []diag.Diagnostic{{
			Code: diag.CodeRuleDevinUnsupported, Severity: diag.SeverityWarning,
			Target: string(hcc.TargetDevin), Resource: "rules", Message: "generic compiler warning",
		}},
	}

	diagnostics := appendReportDiagnostics(nil, report)
	if len(diagnostics) != 2 {
		t.Fatalf("projected diagnostics = %d, want structured materialization plus generic compiler diagnostic: %#v", len(diagnostics), diagnostics)
	}
	var structured, generic bool
	for _, diagnostic := range diagnostics {
		text := diagnostic.err.Error()
		if strings.Contains(text, diag.CodeOwnershipUnmanagedKey) {
			structured = true
			for _, field := range []string{
				"severity=warning", `resource="server"`, "target=devin", "scope=machine",
				"root=machine-devin", `path="mcp_config.json"`, "operation=skip", "outcome=warning",
			} {
				if !strings.Contains(text, field) {
					t.Errorf("structured TUI projection omitted %s: %q", field, text)
				}
			}
			continue
		}
		if strings.Contains(text, diag.CodeRuleDevinUnsupported) && strings.Contains(text, `resource "rules"`) {
			generic = true
		}
	}
	if !structured || !generic {
		t.Fatalf("materialization/generic projection controls = structured %v, generic %v; diagnostics=%#v", structured, generic, diagnostics)
	}
}

// TestPromoteReportErrorsNamesEachResource is the TUI half of bead
// agent-marketplace-oc2r. `sync` was fixed to name the failing resource; the
// TUI's update path kept its own fmt.Errorf("%s: %s", d.Code, d.Message) and
// went on printing byte-identical anonymous lines for different skills, with no
// deduplication at all. One promotion, one dedup, both surfaces.
func TestPromoteReportErrorsNamesEachResource(t *testing.T) {
	report := &materialize.Report{Diags: []diag.Diagnostic{
		toolUnknownDiag("alpha"),
		toolUnknownDiag("beta"),
		// The same failure reported again for a second harness target must
		// still collapse.
		toolUnknownDiag("alpha"),
	}}

	errs := promoteReportErrors(report)
	if len(errs) != 2 {
		t.Fatalf("promoted %d errors, want 2 (alpha, beta) after dedup: %v", len(errs), errs)
	}
	for _, want := range []string{`resource "alpha"`, `resource "beta"`} {
		found := false
		for _, err := range errs {
			if strings.Contains(err.Error(), want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no promoted error names %s; the TUI cannot tell the user which skill failed: %v", want, errs)
		}
	}
}

// TestPromoteReportErrorsIgnoresNonErrors pins the severity predicate.
// diag.Severity is an iota with SeverityError = 0, so the previous
// `d.Severity >= diag.SeverityError` test was `>= 0` and matched EVERY
// diagnostic: a rule.devin.unsupported warning, which AGENTS.md says must never
// fail a run, failed the TUI's update.
func TestPromoteReportErrorsIgnoresNonErrors(t *testing.T) {
	report := &materialize.Report{Diags: []diag.Diagnostic{
		{Code: diag.CodeRuleDevinUnsupported, Severity: diag.SeverityWarning, Resource: "house-style", Message: "rules are out of scope for the Devin target"},
		{Code: diag.CodeIngestDevinManifestSynthesized, Severity: diag.SeverityInfo, Resource: "alpha", Message: "manifest synthesized"},
	}}

	if errs := promoteReportErrors(report); len(errs) != 0 {
		t.Fatalf("a warning and an info were promoted to errors and would fail the update: %v", errs)
	}
	if errs := promoteReportErrors(nil); len(errs) != 0 {
		t.Fatalf("a nil report produced %v", errs)
	}
}
