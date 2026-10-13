package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/session"
)

// spec: session-startup/registry-selections-carry-the-harness-override-badge
func TestGuiHostProvisionalRegistryHarnessOverrideBadge(t *testing.T) {
	paths := startupHostFixture(t, func(source string) string {
		return fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin, claude]\ntelemetry:\n  level: off\nsources:\n  local:\n    directory: %q\nresources:\n  local: {all: true, harnesses: [devin]}\n", source)
	})
	before := captureProtectedHostPaths(t, paths)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("create user-scope host GUI")
	}
	t.Cleanup(app.close)
	stages := readHostStartupStages(t, app)
	app.close()
	assertHostStartupStageOrder(t, stages)
	wantNames := []string{"alpha", "beta"}
	for stageIndex, event := range stages {
		pending := stageIndex < 2
		wantDrift := "pending"
		if stageIndex == 3 {
			wantDrift = "ready"
		}
		if event.Payload.Drift != wantDrift || len(event.Payload.Rows) != len(wantNames) {
			t.Fatal("startup stage drift or row count mismatch")
		}
		for rowIndex, row := range event.Payload.Rows {
			if row.ID == "" || row.Name != wantNames[rowIndex] || row.Kind != "skill" || row.Registry != "local" || row.Scope != "user" {
				t.Fatal("startup row identity or scope metadata mismatch")
			}
			if !row.Checked || row.PendingVerification != pending || !row.HarnessOverride || len(row.Harnesses) != 1 || row.Harnesses[0] != "devin" {
				t.Fatal("startup row selection or harness-override badge mismatch")
			}
			if pending && row.ResourceAction != "none" {
				t.Fatal("unverified startup row exposed a resource action")
			}
			if stageIndex > 0 && row.ID != stages[stageIndex-1].Payload.Rows[rowIndex].ID {
				t.Fatal("startup row identity changed between stages")
			}
		}
	}
	for _, diagnostic := range stages[3].Payload.Diagnostics {
		if diagnostic.Severity == "error" {
			t.Fatal("final startup assessment contained an error diagnostic")
		}
	}
	assertProtectedHostPathsUnchanged(t, paths, before)
}
