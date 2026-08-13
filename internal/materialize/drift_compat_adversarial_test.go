package materialize

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// ADVERSARIAL: "regression in the opposite direction". The severity fix made
// the drift report quieter; these tests check nothing REAL went quiet.

// TestDriftReportStillBreaksOnPathShapedErrorResource is the counterpart to the
// `status` hole. DriftReportFromReport maps d.Resource through resolvedByName
// but FALLS BACK to the raw value, so a path-shaped Resource still produces a
// Broken item (named by its path) and still makes DriftReport.InSync() false.
// `status` drops it entirely. The two surfaces disagree, and drift is the
// correct one.
func TestDriftReportStillBreaksOnPathShapedErrorResource(t *testing.T) {
	resourceKey := skill.ResourceKey(ir.KindMcpServer, "some-mcp")
	report := &Report{
		Diags: []diag.Diagnostic{{
			Code:     "materialize.write.error",
			Severity: diag.SeverityError,
			Resource: ".windsurf/mcp_config.json",
			Message:  "prune managed entries: permission denied",
		}},
	}

	dr := DriftReportFromReport(report, []string{resourceKey})
	if len(dr.Broken) != 1 {
		t.Fatalf("Broken = %+v, want exactly one item: a real error must never vanish from the drift report", dr.Broken)
	}
	if dr.Broken[0].Name != ".windsurf/mcp_config.json" {
		t.Fatalf("Broken[0].Name = %q, want the raw path fallback", dr.Broken[0].Name)
	}
	if dr.InSync() {
		t.Fatal("DriftReport.InSync() = true with a SeverityError diagnostic present")
	}
	// And the resource itself is still (wrongly) listed as synced, because the
	// broken key never matched it. That is the cosmetic half of the same
	// name/path mismatch.
	if len(dr.Synced) != 1 || dr.Synced[0].Name != resourceKey {
		t.Fatalf("Synced = %+v, want the resource key: documenting the double-count", dr.Synced)
	}
}

// TestReportInSyncCatchesEveryErrorRegardlessOfResource: Report.InSync() has no
// Resource filter, so no error-severity diagnostic can escape `sync --check`,
// whatever its Resource is. This is the load-bearing "did not go too quiet"
// assertion.
func TestReportInSyncCatchesEveryErrorRegardlessOfResource(t *testing.T) {
	for _, resource := range []string{"", "some-skill", ".windsurf/mcp_config.json", "skills/nested/name"} {
		report := &Report{Diags: []diag.Diagnostic{{
			Code:     "materialize.write.error",
			Severity: diag.SeverityError,
			Resource: resource,
		}}}
		if report.InSync() {
			t.Fatalf("InSync() = true for an error diagnostic with Resource=%q: sync --check would exit 0", resource)
		}
	}
}

// TestReportInSyncStillFalseForOtherReasons guards against InSync() being
// short-circuited by the diagnostics change: the non-diagnostic drift signals
// must still work.
func TestReportInSyncStillFalseForOtherReasons(t *testing.T) {
	if (&Report{}).InSync() != true {
		t.Fatal("empty report must be in sync")
	}
	// A mixed report: a warning must not flip it, an error must.
	warn := &Report{Diags: []diag.Diagnostic{{Code: diag.CodeRuleDevinUnsupported, Severity: diag.SeverityWarning, Resource: "r"}}}
	if !warn.InSync() {
		t.Fatal("a warning-only report must be in sync")
	}
	mixed := &Report{Diags: []diag.Diagnostic{
		{Code: diag.CodeRuleDevinUnsupported, Severity: diag.SeverityWarning, Resource: "r"},
		{Code: "materialize.compile.error", Severity: diag.SeverityError, Resource: "r"},
	}}
	if mixed.InSync() {
		t.Fatal("a report containing an error must not be in sync")
	}
}

// TestDriftReportBrokenBeatsSynced: a resource that is broken must not also be
// counted as synced. This is the shape the pre-fix bug hid (everything was
// broken), so it needs an explicit pin now that only errors break.
func TestDriftReportBrokenBeatsSynced(t *testing.T) {
	resourceKey := skill.ResourceKey(ir.KindSkill, "some-skill")
	report := &Report{Diags: []diag.Diagnostic{{
		Code:     "materialize.compile.error",
		Severity: diag.SeverityError,
		Resource: "some-skill",
	}}}
	dr := DriftReportFromReport(report, []string{resourceKey})
	if len(dr.Broken) != 1 || dr.Broken[0].Name != resourceKey {
		t.Fatalf("Broken = %+v, want %q", dr.Broken, resourceKey)
	}
	for _, item := range dr.Synced {
		if item.Name == resourceKey {
			t.Fatalf("%q is both broken and synced", resourceKey)
		}
	}
}
