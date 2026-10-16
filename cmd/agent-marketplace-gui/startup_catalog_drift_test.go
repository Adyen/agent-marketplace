package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/session"
)

func work51RowsByRegistry(t *testing.T, rows []work50HostRow) map[string]work50HostRow {
	t.Helper()
	out := make(map[string]work50HostRow)
	ids := make(map[string]bool)
	for _, row := range rows {
		if row.Registry == "other" {
			t.Fatal("hidden same-name catalog occurrence surfaced as a row")
		}
		if row.Name != "shared" {
			continue
		}
		if row.Kind != "skill" || row.ID == "" || ids[row.ID] {
			t.Fatal("catalog drift fixture lost same-name catalog identities")
		}
		if _, duplicate := out[row.Registry]; duplicate {
			t.Fatal("catalog drift fixture lost distinct registry labels")
		}
		out[row.Registry], ids[row.ID] = row, true
	}
	_, selectedExists := out["selected"]
	_, recoveryExists := out["recovery"]
	if len(out) != 2 || !selectedExists || !recoveryExists {
		t.Fatal("catalog drift fixture did not deliver exactly the selected and recovery shared rows")
	}
	return out
}

// spec: session-startup/drift-applies-only-to-rows-of-its-own-catalog-selection
func TestGuiHostOwnCatalogDriftPreservesRecoveryAndHiddenOccurrence(t *testing.T) {
	var fixturePaths []string
	paths := startupHostFixture(t, func(source string) string {
		selected := filepath.Join(source, "selected")
		other := filepath.Join(source, "other")
		recovery := filepath.Join(source, "recovery")
		body := strings.NewReplacer("solo", "shared", "Solo", "Shared").Replace(fixtureSkillBody)
		for _, directory := range []string{selected, other} {
			skillDir := filepath.Join(directory, "shared")
			skillPath := filepath.Join(skillDir, "SKILL.md")
			writeFile(t, skillPath, body)
			fixturePaths = append(fixturePaths, directory, skillDir, skillPath)
		}
		if err := os.MkdirAll(recovery, 0o700); err != nil {
			t.Fatal("create empty recovery catalog")
		}
		fixturePaths = append(fixturePaths, recovery)
		return fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\nsources:\n  selected: {directory: %q}\n  other: {directory: %q}\n  recovery: {directory: %q}\nresources:\n  selected: {include: [skill/shared]}\n  other: {}\n  recovery: {include: [skill/shared]}\n", selected, other, recovery)
	})
	paths = append(paths, fixturePaths...)
	for _, root := range []string{filepath.Join(os.Getenv("HOME"), ".config", "devin"), filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "devin")} {
		paths = append(paths, filepath.Join(root, "skills", "shared"), filepath.Join(root, "skills", "shared", "SKILL.md"))
	}
	before := captureProtectedHostPaths(t, paths)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("create desktop host for catalog drift fixture")
	}
	t.Cleanup(app.close)
	stages := readWork50HostStages(t, app)
	if len(stages) != 4 {
		t.Fatal("catalog drift fixture did not deliver four startup stages")
	}
	for i, stage := range stages {
		if stage.Payload.Stage != []string{"provisional", "config", "inventory", "final"}[i] || stage.Generation != stages[0].Generation || stage.Generation == 0 {
			t.Fatal("catalog drift fixture changed startup order or generation")
		}
	}
	if stages[2].Payload.Drift != "pending" || stages[3].Payload.Drift != "failed" {
		t.Fatal("catalog recovery assessment did not preserve pending then failed drift")
	}
	inventory := work51RowsByRegistry(t, stages[2].Payload.Rows)
	final := work51RowsByRegistry(t, stages[3].Payload.Rows)
	selected, recovery := inventory["selected"], inventory["recovery"]
	if !selected.Checked || selected.Group != "enabled" || selected.Status != "enabled" {
		t.Fatal("selected catalog row was not verified before assessment")
	}
	if !recovery.Checked || recovery.Group != "broken" || !strings.HasPrefix(recovery.Status, "unavailable: ") {
		t.Fatal("missing selected resource did not create an existing recovery row")
	}
	assessed := final["selected"]
	if assessed.ID != selected.ID || !assessed.Checked || assessed.Group != "pending" || assessed.Status != "enabled (pending sync)" {
		t.Fatal("selected catalog did not receive its own missing drift")
	}
	if after := final["recovery"]; recovery.ID != after.ID || recovery.Checked != after.Checked || recovery.Scope != after.Scope || recovery.Status != after.Status || recovery.Group != after.Group || recovery.PendingVerification != after.PendingVerification {
		t.Fatal("assessment changed another catalog or replaced existing recovery drift")
	}
	for _, row := range stages[3].Payload.Rows {
		if row.PendingVerification {
			t.Fatal("final catalog drift retained a pending verification row")
		}
	}
	app.close()
	assertProtectedHostPathsUnchanged(t, paths, before)
}
