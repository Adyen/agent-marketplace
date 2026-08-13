package pipeline

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// TestDiagWarningNamesTheResource is a reporting-side consequence of bead
// agent-marketplace-pu6b.
//
// Before that bead, an allowed-tools restriction Devin could not represent was
// an ERROR: the resource was missing from disk and counted in `Errors N`, so the
// user had two independent signals even though the message never named the
// resource. It is now a WARNING attached to a resource that installs
// successfully, and the printed line was `warning: <code>: <message>` — the
// message is deliberately generic so it cannot echo the offending specifier, so
// nothing in the output said WHICH resource lost its restriction.
//
// With three openspec skills in one registry that produced three identical,
// unattributable lines. The resource name is not sensitive (it is printed all
// over sync output already), and it is the only part that makes a widening
// actionable, so it belongs in the warning.
// Ported to the unified diagIssue promotion when agent-marketplace-oc2r
// replaced newDiagWarning: oc2r renders the resource AND its source location
// for both severities, which supersedes pu6b's narrower "(resource: x)" suffix.
// The property under test is unchanged - a widening must name what it widened.
func TestDiagWarningNamesTheResource(t *testing.T) {
	warning := PromoteDiagnostic(diag.Diagnostic{
		Code:     diag.CodeSkillDevinToolUnknown,
		Severity: diag.SeverityWarning,
		Resource: "openspec-apply-change",
		Message:  "an allowed-tools entry carries a scoped tool specifier, which has no Devin equivalent; the allowed-tools key was omitted and Devin's default tool set applies",
	})
	text := warning.Error()
	if !strings.Contains(text, diag.CodeSkillDevinToolUnknown) {
		t.Errorf("warning %q dropped the diagnostic code", text)
	}
	if !strings.Contains(text, "openspec-apply-change") {
		t.Errorf("warning %q does not name the resource whose tool restriction was dropped; "+
			"a user with several affected skills sees identical, unattributable lines", text)
	}
}

// TestDiagWarningWithoutAResourceIsUnchanged keeps the target-level diagnostics
// (which carry no resource) formatted exactly as before.
func TestDiagWarningWithoutAResourceIsUnchanged(t *testing.T) {
	warning := PromoteDiagnostic(diag.Diagnostic{Code: "materialize.example", Message: "something target-wide"})
	if got, want := warning.Error(), "materialize.example: something target-wide"; got != want {
		t.Errorf("warning = %q, want %q", got, want)
	}
}
