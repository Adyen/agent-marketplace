package materialize

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// diag.SeverityError is the ZERO value of diag.Severity (error 0, warning 1,
// info 2), so an ordinal `>=`/`<` comparison against it says the opposite of
// what it reads like. Every consumer of report diagnostics must ask
// diag.Diagnostic.IsError() instead. The tests below pin the observable
// behaviour of the report surfaces that got it wrong.

// TestReportInSyncIgnoresNonErrorDiagnostics covers Report.InSync(): a
// warnings-only or info-only report is in sync, so `sync --check` must not exit
// 1 for a lossy-but-successful materialization.
func TestReportInSyncIgnoresNonErrorDiagnostics(t *testing.T) {
	tests := []struct {
		name     string
		severity diag.Severity
		want     bool
	}{
		{name: "warning only is in sync", severity: diag.SeverityWarning, want: true},
		{name: "info only is in sync", severity: diag.SeverityInfo, want: true},
		{name: "error is out of sync", severity: diag.SeverityError, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &Report{
				Diags: []diag.Diagnostic{{
					Code:     diag.CodeSubagentDevinEffortDropped,
					Severity: tt.severity,
					Resource: "some-skill",
					Message:  "lossy but successful",
				}},
			}
			if got := report.InSync(); got != tt.want {
				t.Fatalf("InSync() with a %v-only report = %v, want %v", tt.severity, got, tt.want)
			}
		})
	}
}

// TestReportInSyncTrueWithNoDiagnostics is the control case.
func TestReportInSyncTrueWithNoDiagnostics(t *testing.T) {
	if got := (&Report{}).InSync(); !got {
		t.Fatalf("InSync() on an empty report = %v, want true", got)
	}
}

// TestReportInSyncAccountsForRetainedStructuredErrors covers issues retained
// by callers before the report projection is rebuilt. Both report-level and
// target-report issues must make sync --check report drift.
func TestReportInSyncAccountsForRetainedStructuredErrors(t *testing.T) {
	errorIssue := MaterializationIssue{
		Target:    "claude",
		Operation: MaterializationOperationFailure,
		Outcome:   MaterializationOutcomeFailed,
		Code:      "materialize.write.error",
		Severity:  diag.SeverityError,
		Message:   "pre-populated materialization failure",
	}
	tests := []struct {
		name   string
		report *Report
	}{
		{name: "report issue", report: &Report{Issues: []MaterializationIssue{errorIssue}}},
		{name: "target report issue", report: &Report{PerRoot: []TargetReport{{Issues: []MaterializationIssue{errorIssue}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.report.InSync(); got {
				t.Fatalf("InSync() = true for retained structured error, want false")
			}
		})
	}
}

// TestDriftReportFromReportOnlyErrorsAreBroken covers DriftReportFromReport:
// a resource carrying a warning is still in sync, only an error-severity
// diagnostic marks it broken.
func TestDriftReportFromReportOnlyErrorsAreBroken(t *testing.T) {
	resourceKey := skill.ResourceKey(ir.KindSkill, "some-skill")

	tests := []struct {
		name      string
		severity  diag.Severity
		wantBroke bool
	}{
		{name: "warning is not broken", severity: diag.SeverityWarning, wantBroke: false},
		{name: "info is not broken", severity: diag.SeverityInfo, wantBroke: false},
		{name: "error is broken", severity: diag.SeverityError, wantBroke: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &Report{
				Diags: []diag.Diagnostic{{
					Code:     diag.CodeSubagentDevinEffortDropped,
					Severity: tt.severity,
					Resource: "some-skill",
					Message:  "lossy but successful",
				}},
			}
			dr := DriftReportFromReport(report, []string{resourceKey})

			broken := false
			for _, item := range dr.Broken {
				if item.Name == resourceKey {
					broken = true
				}
			}
			if broken != tt.wantBroke {
				t.Fatalf("DriftReport.Broken for a %v diagnostic = %+v, want broken=%v", tt.severity, dr.Broken, tt.wantBroke)
			}

			synced := false
			for _, item := range dr.Synced {
				if item.Name == resourceKey {
					synced = true
				}
			}
			if wantSynced := !tt.wantBroke; synced != wantSynced {
				t.Fatalf("DriftReport.Synced for a %v diagnostic = %+v, want synced=%v", tt.severity, dr.Synced, wantSynced)
			}
		})
	}
}
