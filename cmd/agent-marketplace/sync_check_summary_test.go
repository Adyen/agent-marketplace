package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/materialize"
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

// TestSyncCheckSummaryKeepsTheDriftWording preserves the existing, correct
// message for real drift (files to write / remove).
func TestSyncCheckSummaryKeepsTheDriftWording(t *testing.T) {
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

// TestSyncCheckSummarySilentWhenClean pins the in-sync contract: no output, no
// error. A warning-only report is in sync.
func TestSyncCheckSummarySilentWhenClean(t *testing.T) {
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
