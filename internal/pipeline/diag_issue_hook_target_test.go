package pipeline

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func hookTargetDiagnostic(target string) diag.Diagnostic {
	return diag.Diagnostic{
		Code:     diag.CodeHookMatcherUntranslatable,
		Severity: diag.SeverityError,
		Target:   target,
		Resource: "PreToolUse-command",
		Message:  "The target tool catalog cannot exactly cover the source matcher; hook suppressed",
		Hint:     "Author a target-specific hook or explicitly narrow the selected harnesses",
		Source:   diag.SourceRef{File: ".devin/hooks.v1.json", Line: 3},
	}
}

// spec: hook-sync-diagnostics/hook-errors-retain-target-and-source
func TestDiagIssueHookErrorNamesTargetResourceAndSource(t *testing.T) {
	want := `hook.matcher-untranslatable: The target tool catalog cannot exactly cover the source matcher; hook suppressed (target factory, resource "PreToolUse-command", .devin/hooks.v1.json:3)`
	if got := newDiagIssue(hookTargetDiagnostic("factory")).Error(); got != want {
		t.Fatalf("Error() =\n%s\nwant\n%s", got, want)
	}

	warning := hookTargetDiagnostic("factory")
	warning.Severity = diag.SeverityWarning
	wantWarning := `hook.matcher-untranslatable: The target tool catalog cannot exactly cover the source matcher; hook suppressed (resource "PreToolUse-command", .devin/hooks.v1.json:3)`
	if got := newDiagIssue(warning).Error(); got != wantWarning {
		t.Fatalf("warning Error() =\n%s\nwant\n%s", got, wantWarning)
	}
}

// spec: hook-sync-diagnostics/different-target-failures-remain-distinguishable
func TestDiagIssueHookErrorTargetsStayApart(t *testing.T) {
	factory := newDiagIssue(hookTargetDiagnostic("factory"))
	cursor := newDiagIssue(hookTargetDiagnostic("cursor"))
	factoryAgain := newDiagIssue(hookTargetDiagnostic("factory"))

	deduped := DedupIssues([]error{factory, cursor, factoryAgain})
	if len(deduped) != 2 {
		t.Fatalf("DedupIssues kept %d issues, want 2 (factory and cursor): %v", len(deduped), deduped)
	}
	if !strings.Contains(deduped[0].Error(), "target factory") || !strings.Contains(deduped[1].Error(), "target cursor") {
		t.Fatalf("deduped errors lost their targets: %v", deduped)
	}
}

// spec: hook-sync-diagnostics/hook-errors-retain-target-and-source
func TestDiagIssueHookTargetIsSanitizedAndBounded(t *testing.T) {
	hostile := "factory\x1b[31m\n\r\u202eFORGED" + strings.Repeat("x", 600)
	got := newDiagIssue(hookTargetDiagnostic(hostile)).Error()
	if strings.ContainsAny(got, "\x1b\n\r\u202e") {
		t.Fatalf("rendered hook error carries terminal controls: %q", got)
	}
	if !strings.Contains(got, "target factory") || !strings.Contains(got, diagTruncatedMark) {
		t.Fatalf("rendered hook error = %q, want the target qualifier with a truncation mark", got)
	}
	if len(got) > 2048 {
		t.Fatalf("rendered hook error is %d bytes, want it bounded", len(got))
	}
}
