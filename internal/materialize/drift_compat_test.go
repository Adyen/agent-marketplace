package materialize

import "testing"

// spec: tui-feedback/broken-only-drift-does-not-loop-on-quit
func TestBrokenItemsRemainInFullDriftReport(t *testing.T) {
	report := DriftReport{
		Broken: []DriftItem{{Name: "skill/missing-upstream", Reason: DriftBroken}},
	}
	if report.InSync() {
		t.Fatal("Broken diagnostics must remain part of the full drift verdict")
	}
	if report.PendingCount() != 1 {
		t.Fatalf("PendingCount() = %d, want Broken diagnostics included", report.PendingCount())
	}
}

// spec: tui-feedback/broken-only-drift-does-not-loop-on-quit
func TestSyncableCountExcludesBrokenAndCountsMissingAndOrphaned(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report DriftReport
		want   int
	}{
		{
			name:   "broken only",
			report: DriftReport{Broken: []DriftItem{{Name: "skill/broken", Reason: DriftBroken}}},
			want:   0,
		},
		{
			name: "missing and orphaned",
			report: DriftReport{
				Missing:  []DriftItem{{Name: "skill/new", Reason: DriftMissing}},
				Orphaned: []DriftItem{{Name: "skill/old", Reason: DriftOrphaned}},
				Broken:   []DriftItem{{Name: "skill/broken", Reason: DriftBroken}},
			},
			want: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.report.SyncableCount(); got != tc.want {
				t.Fatalf("SyncableCount() = %d, want %d", got, tc.want)
			}
		})
	}
}
