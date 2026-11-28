package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

// spec: target-profile-import/new-destination-paths-report-predicted-claims
func TestTargetDryRunRelativeXDGConfigUsesPlaceFallback(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	profile := strings.Replace(targetApproveProfileYAML, "override: INVENTED_APP_HOME, ", "", 1)
	profile = strings.Replace(
		profile,
		"macos: {base: home, path: .config/invented-app}",
		"macos: {base: xdg-config, path: invented-app}",
		1,
	)
	profile = strings.Replace(profile, "allowed_bases: [home]", "allowed_bases: [home, xdg-config]", 1)
	if !strings.Contains(profile, "macos: {base: xdg-config, path: invented-app}") ||
		!strings.Contains(profile, "allowed_bases: [home, xdg-config]") ||
		strings.Contains(profile, "override: INVENTED_APP_HOME") {
		t.Fatalf("profile does not use the allowed xdg-config base without an override:\n%s", profile)
	}

	fixture := newTargetDryRunFixture(t, []byte(profile), targetDryRunManifestYAML)
	t.Setenv("XDG_CONFIG_HOME", "relative-config")
	stdout, stderr, err := executeTargetDryRun(fixture, true)
	if err != nil {
		t.Fatalf("target dry-run failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report targetDryRunReport
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("decode target dry-run report: %v\nstdout:\n%s", err, stdout)
	}
	if report.Status != "valid" || report.DestinationCheck.Status != "checked" ||
		len(report.DestinationCheck.OwnershipChanges) != 2 {
		t.Fatalf("destination report = %#v", report.DestinationCheck)
	}
	for _, claim := range report.DestinationCheck.OwnershipChanges {
		if claim.Action != "claim" {
			t.Errorf("ownership change = %#v, want a predicted claim", claim)
		}
	}
}

// spec: target-profile-import/refusal-returns-diagnostics-and-a-nonzero-exit
// spec: target-profile-import/dry-run-leaves-all-existing-state-unchanged
// spec: target-profile-import/local-dry-run-needs-no-configuration-approval-acquisition-or-host-execution
func TestTargetDryRunPendingJournal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, test := range []struct {
		name            string
		withDestination bool
	}{
		{name: "destination-refuses", withDestination: true},
		{name: "local-validation-succeeds"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTargetDryRunFixture(t, []byte(targetApproveProfileYAML), targetDryRunManifestYAML)
			stateDir, journalPath := seedTargetDryRunPendingJournal(t, fixture)
			if err := dependencytxn.CheckPending(stateDir); !errors.Is(err, dependencytxn.ErrRecoveryRequired) {
				t.Fatalf("seeded transaction check = %v, want pending recovery", err)
			}
			before := snapshotTargetDryRunTree(t, fixture.root)

			stdout, stderr, runErr := executeTargetDryRun(fixture, test.withDestination)
			var report targetDryRunReport
			if err := json.Unmarshal(stdout, &report); err != nil {
				t.Fatalf("decode target dry-run report: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
			}
			if test.withDestination {
				if runErr == nil || report.SchemaVersion != 1 || report.Status != "refused" ||
					report.DestinationCheck.Status != "refused" ||
					len(report.DestinationCheck.OwnershipChanges) != 0 ||
					len(report.Diagnostics) != 1 ||
					report.Diagnostics[0].Code != "dependency.recovery-required" ||
					report.Diagnostics[0].Path != fixture.destination ||
					!strings.Contains(report.Diagnostics[0].Message, journalPath) {
					t.Fatalf("pending destination result: err=%v report=%#v\nstderr:\n%s", runErr, report, stderr)
				}
			} else if runErr != nil || report.SchemaVersion != 1 || report.Status != "valid" ||
				report.DestinationCheck.Status != "not-requested" ||
				len(report.DestinationCheck.OwnershipChanges) != 0 || len(report.Diagnostics) != 0 {
				t.Fatalf("local validation with pending state: err=%v report=%#v\nstderr:\n%s", runErr, report, stderr)
			}

			if after := snapshotTargetDryRunTree(t, fixture.root); !reflect.DeepEqual(after, before) {
				t.Fatalf("dry-run changed inputs, destination, or pending state:\nbefore: %#v\nafter: %#v", before, after)
			}
		})
	}
}

func seedTargetDryRunPendingJournal(t *testing.T, fixture targetDryRunFixture) (string, string) {
	t.Helper()
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(fixture.destination, "mods", "sample-links", "main.js")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("interrupt dependency publication")
	_, err = dependencytxn.Commit(context.Background(), stateDir, []dependencytxn.DesiredFile{{
		Path: destination, Bytes: []byte("pending publication\n"), Mode: 0o644,
	}}, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
		if point.Stage == dependencytxn.FaultAfterJournal {
			return interrupted
		}
		return nil
	}))
	if !errors.Is(err, interrupted) {
		t.Fatalf("seed pending transaction: %v", err)
	}
	journals, err := filepath.Glob(filepath.Join(stateDir, ".dependencytxn-*.json"))
	if err != nil || len(journals) != 1 {
		t.Fatalf("pending journals = %v, err=%v", journals, err)
	}
	return stateDir, journals[0]
}
