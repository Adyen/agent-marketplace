package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

// writeMissingSelectionFixture builds a project whose loaded directory
// registry provides "audit" but not "missing-skill". The registry is marked
// required when requiredRegistry is set, which turns the missing selection
// into a hard failure.
func writeMissingSelectionFixture(t *testing.T, requiredRegistry bool) (repoRoot, homeDir string) {
	t.Helper()
	repoRoot = t.TempDir()
	homeDir = t.TempDir()
	t.Setenv("HOME", homeDir)

	registry := "  - url: ./local-skills\n    path: skills\n"
	if requiredRegistry {
		registry += "    required: true\n"
	}
	projectConfig := repoV2Header + "registries:\n" + registry + "skills:\n  - audit\n  - missing-skill\n"
	auditDir := filepath.Join(repoRoot, "local-skills", "skills", "audit")
	for _, dir := range []string{filepath.Join(repoRoot, ".agents"), filepath.Join(homeDir, ".agents"), auditDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	mustWriteFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), []byte(projectConfig))
	mustWriteFile(t, filepath.Join(homeDir, ".agents", "config.yaml"), []byte(v2Header))
	mustWriteFile(t, filepath.Join(auditDir, "SKILL.md"), []byte("# Audit\n"))

	t.Cleanup(swapCloneOrPull(func(url, ref string) (string, error) { return url, nil }))
	t.Cleanup(swapDiscoverSkills(func(dir string) ([]catalog.Entry, []skill.MalformedSkill, error) {
		return []catalog.Entry{{Name: "audit", Path: filepath.Join(dir, "audit")}}, nil, nil
	}))
	t.Cleanup(swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil }))
	t.Cleanup(swapLoadGlobalConfig(func() (*config.Config, error) { return newMockGlobalConfig() }))
	t.Cleanup(swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	}))
	t.Cleanup(swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	}))
	return repoRoot, homeDir
}

// spec: resource-selection/optional-entry-ordered-before-a-required-entry-for-the-same-missing-resource
func TestOptionalEntryBeforeRequiredEntryKeepsMissAsHard(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	registryRoot := filepath.Join(repoRoot, "local-skills")
	auditDir := filepath.Join(registryRoot, "skills", "audit")
	for _, dir := range []string{filepath.Join(repoRoot, ".agents"), filepath.Join(homeDir, ".agents"), auditDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	mustWriteFile(t, filepath.Join(auditDir, "SKILL.md"), []byte("# Audit\n"))
	orgPath := filepath.Join(homeDir, "org.yaml")
	mustWriteFile(t, orgPath, []byte(repoV2Header+"registries:\n  - url: "+registryRoot+"\n    path: skills\nskills:\n  - missing-skill\n"))
	mustWriteFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"),
		[]byte(repoV2Header+"registries:\n  - url: "+registryRoot+"\n    path: skills\n    required: true\nskills:\n  - audit\n  - missing-skill\n"))
	mustWriteFile(t, filepath.Join(homeDir, ".agents", "config.yaml"), []byte(v2Header))

	t.Cleanup(swapCloneOrPull(func(url, ref string) (string, error) { return url, nil }))
	t.Cleanup(swapDiscoverSkills(func(dir string) ([]catalog.Entry, []skill.MalformedSkill, error) {
		return []catalog.Entry{{Name: "audit", Path: filepath.Join(dir, "audit")}}, nil, nil
	}))
	t.Cleanup(swapLoadOrgConfig(func() (*config.Config, error) { return config.LoadConfigFromPath(orgPath) }))
	t.Cleanup(swapLoadGlobalConfig(func() (*config.Config, error) { return newMockGlobalConfig() }))
	t.Cleanup(swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	}))
	t.Cleanup(swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	}))

	snapshot := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	if snapshot.driftAssessment == driftReady {
		t.Fatal("required miss shared across layers left drift assessed as ready")
	}
	missing := findSnapshotRow(t, snapshot, "missing-skill")
	if missing.UnavailableReason == "" || missing.UnavailableSoft {
		t.Fatalf("missing row reason %q soft %v, want hard unavailable", missing.UnavailableReason, missing.UnavailableSoft)
	}
	if err := snapshotAcceptanceError(snapshot); err == nil {
		t.Fatal("required miss shared across layers was accepted")
	}

	m := beginSyncOperation(t)
	next, _ := m.Update(syncDoneMsg{warnings: snapshot.warnings})
	m = next.(Model)
	snapshot.generation = m.requests.active(requestSnapshot)
	next, _ = m.Update(snapshot)
	m = next.(Model)
	if strings.Contains(m.toast, "Sync complete") {
		t.Fatalf("required miss shared across layers reported sync success: %q", m.toast)
	}
}

func findSnapshotRow(t *testing.T, snapshot snapshotLoadedMsg, name string) row {
	t.Helper()
	for _, r := range snapshot.rows {
		if r.Artifact.Name == name {
			return r
		}
	}
	t.Fatalf("snapshot has no row %q: %+v", name, snapshot.rows)
	return row{}
}

// spec: tui-startup/optional-missing-selection-keeps-the-assessment
func TestOptionalMissingSelectionKeepsAssessment(t *testing.T) {
	repoRoot, homeDir := writeMissingSelectionFixture(t, false)

	snapshot := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	if snapshot.driftAssessment != driftReady {
		t.Fatalf("drift assessment = %v, want ready", snapshot.driftAssessment)
	}
	if len(snapshot.errs) != 0 {
		t.Fatalf("optional missing selection produced errors: %v", snapshot.errs)
	}
	if !hasSelectedNotFound(snapshot.warnings, "missing-skill") {
		t.Fatalf("optional missing selection not reported as a warning: %v", snapshot.warnings)
	}
	missing := findSnapshotRow(t, snapshot, "missing-skill")
	if missing.UnavailableReason == "" || !missing.UnavailableSoft {
		t.Fatalf("missing row reason %q soft %v, want reason and soft", missing.UnavailableReason, missing.UnavailableSoft)
	}
	if err := snapshotAcceptanceError(snapshot); err != nil {
		t.Fatalf("snapshot acceptance rejected a soft missing selection: %v", err)
	}

	m := beginSyncOperation(t)
	next, _ := m.Update(syncDoneMsg{warnings: snapshot.warnings})
	m = next.(Model)
	snapshot.generation = m.requests.active(requestSnapshot)
	next, _ = m.Update(snapshot)
	m = next.(Model)
	if m.syncing || m.progress != nil {
		t.Fatalf("sync reload did not finish: syncing %t progress %v", m.syncing, m.progress)
	}
	if !strings.Contains(m.toast, "Sync complete with 1 warning(s)") {
		t.Fatalf("sync reload toast = %q, want success with one warning", m.toast)
	}
	for _, d := range m.diagnostics.operations {
		if d.operation == operationSync && d.severity == diagnosticError {
			t.Fatalf("sync reload recorded a failure: %+v", d)
		}
	}
	if strings.Contains(m.driftStatusSegment(), "drift unavailable") {
		t.Fatalf("footer drift segment = %q, want the assessed drift state", m.driftStatusSegment())
	}
}

// spec: tui-startup/unverifiable-selections-still-fail-the-assessment
func TestUnverifiableSelectionsStillFailAssessment(t *testing.T) {
	repoRoot, homeDir := writeMissingSelectionFixture(t, true)

	snapshot := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	if snapshot.driftAssessment == driftReady {
		t.Fatal("required missing selection left drift assessed as ready")
	}
	if err := snapshotAcceptanceError(snapshot); err == nil {
		t.Fatal("required missing selection was accepted")
	}

	m := beginSyncOperation(t)
	next, _ := m.Update(syncDoneMsg{})
	m = next.(Model)
	snapshot.generation = m.requests.active(requestSnapshot)
	next, _ = m.Update(snapshot)
	m = next.(Model)
	if strings.Contains(m.toast, "Sync complete") {
		t.Fatalf("required missing selection reported sync success: %q", m.toast)
	}
}

func TestCatalogUnavailableRowsStillBlockDriftFooter(t *testing.T) {
	catalogRow := row{UnavailableReason: unavailableCatalogReason}
	if !rowsHaveUnavailable([]row{catalogRow}) {
		t.Fatal("catalog-unavailable row does not count as unavailable")
	}
	softRow := row{UnavailableReason: "not found", UnavailableSoft: true}
	if rowsHaveUnavailable([]row{softRow}) {
		t.Fatal("soft unavailable row counts as unavailable")
	}
	m := Model{driftAssessment: driftReady, rows: []row{softRow}}
	if strings.Contains(m.driftStatusSegment(), "drift unavailable") {
		t.Fatal("soft unavailable row blocks the drift footer")
	}
	m.rows = []row{catalogRow}
	if !strings.Contains(m.driftStatusSegment(), "drift unavailable") {
		t.Fatal("catalog-unavailable row does not block the drift footer")
	}
}

func hasSelectedNotFound(errs []error, name string) bool {
	for _, err := range errs {
		var notFound *resolver.SelectedNotFoundError
		if errors.As(err, &notFound) && notFound.Name == name {
			return true
		}
	}
	return false
}
