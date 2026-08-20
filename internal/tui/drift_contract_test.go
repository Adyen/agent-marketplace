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
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	tea "github.com/charmbracelet/bubbletea"
)

func TestMaterializationDryRunFailureNeverRendersInSync(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m)
	updated, _ := m.Update(snapshotLoadedMsg{
		generation:      m.requests.active(requestSnapshot),
		driftAssessment: driftFailed,
		mergedCfg:       &config.Config{},
		diagnostics: []tuiDiagnostic{
			snapshotDiagnostic(diagnosticError, diagnosticMaterialization, errors.New("dry run failed")),
		},
	})
	m = updated.(Model)
	footer := renderFooter(m)
	if strings.Contains(footer, "in sync") || !strings.Contains(footer, "drift check failed") {
		t.Fatalf("dry-run failure rendered a clean drift verdict: %q", footer)
	}
}

func TestReadSnapshotPropagatesMaterializationDryRunError(t *testing.T) {
	induced := errors.New("induced dry-run failure")
	previous := runMaterialize
	runMaterialize = func(context.Context, string, []resolver.ResolvedArtifact, ...materialize.RunOption) (*materialize.Report, error) {
		return nil, induced
	}
	t.Cleanup(func() { runMaterialize = previous })
	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreOrg)
	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreGlobal)
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreRepo)
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) { return nil, nil })
	t.Cleanup(restoreLocal)

	snapshot := readSnapshot(t.TempDir(), t.TempDir(), catalogLoadCached)
	if snapshot.driftAssessment != driftFailed {
		t.Fatalf("drift assessment = %v, want failed", snapshot.driftAssessment)
	}
	errs := diagnosticErrors(snapshot.diagnostics)
	if len(errs) != 1 || !errors.Is(errs[0], induced) {
		t.Fatalf("dry-run failure not propagated: %v", errs)
	}
}

func TestFatalStartupNeverInfersInSyncFromZeroValueDrift(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m)
	updated, _ := m.Update(snapshotLoadedMsg{
		generation: m.requests.active(requestSnapshot),
		diagnostics: []tuiDiagnostic{
			snapshotDiagnostic(diagnosticError, diagnosticConfig, errors.New("invalid config")),
		},
	})
	m = updated.(Model)
	footer := renderFooter(m)
	if strings.Contains(footer, "in sync") || !strings.Contains(footer, "drift unavailable") {
		t.Fatalf("fatal startup rendered a clean drift verdict: %q", footer)
	}
}

func TestBuildRowsRepresentsOrphanWithoutResolvedCatalogRow(t *testing.T) {
	key := skill.ResourceKey(ir.KindSkill, "orphaned")
	rows := buildRows(nil, nil, nil, nil, materialize.DriftReport{
		Orphaned: []materialize.DriftItem{{
			Name: key, Reason: materialize.DriftOrphaned, Path: "skills/orphaned", Target: "factory", OutDir: "/repo/.factory",
		}},
	})
	if len(rows) != 1 || rows[0].Artifact.Name != "orphaned" || rows[0].DriftItem == nil {
		t.Fatalf("orphan-only drift was not represented: %+v", rows)
	}
	m := modelWithSelectedRow(t, rows[0])
	detail := renderDetail(m)
	for _, want := range []string{"skills/orphaned", "factory", "project", "/repo/.factory"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("orphan detail missing %q:\n%s", want, detail)
		}
	}
}

// TestBuildRowsRepresentsEveryOrphanOutputWithoutCollapsing pins the orphan
// identity contract: orphan outputs are distinct outputs identified by
// (resource, target, scope, outDir, path). An orphan that happens to share a
// resource name with a resolved or catalog resource is still orphaned output
// and must not be dropped, and the same resource name orphaned on two
// targets is two rows — never one collapsed row.
func TestBuildRowsRepresentsEveryOrphanOutputWithoutCollapsing(t *testing.T) {
	key := skill.ResourceKey(ir.KindSkill, "foo")
	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL: "git@example.com:org/reg.git", Ref: "main",
			Skills: config.KindSelector{Include: []string{"foo"}},
			Source: config.RepoCommitted, CatalogKey: "reg-key",
		}},
	}
	catalogs := map[string]catalogInfo{
		"reg-key": {Entries: []catalog.Entry{{Name: "foo", Path: "/cache/reg/foo"}}},
	}
	resolved := []resolver.ResolvedArtifact{{
		Entry:       catalog.Entry{Name: "foo", Path: "/cache/reg/foo"},
		RegistryURL: "git@example.com:org/reg.git", CatalogKey: "reg-key",
	}}
	drift := materialize.DriftReport{
		Orphaned: []materialize.DriftItem{
			// Same resource name as the resolved row, but orphaned output on
			// two different targets/roots: both outputs must survive.
			{Name: key, Reason: materialize.DriftOrphaned, Path: "skills/foo/SKILL.md", Target: "claude", OutDir: "/repo/.claude"},
			{Name: key, Reason: materialize.DriftOrphaned, Path: "skills/foo/SKILL.md", Target: "factory", OutDir: "/repo/.factory"},
			// A second, unrelated orphan output.
			{Name: skill.ResourceKey(ir.KindSkill, "stray"), Reason: materialize.DriftOrphaned, Path: "skills/stray", Target: "factory", OutDir: "/repo/.factory"},
		},
	}

	rows := buildRows(cfg, catalogs, resolved, nil, drift)

	var resolvedRow *row
	var orphanRows []row
	for i := range rows {
		r := rows[i]
		if r.isOrphanOutput() {
			orphanRows = append(orphanRows, r)
			continue
		}
		if r.Artifact.Name == "foo" && !r.IsAvailable {
			resolvedCopy := r
			resolvedRow = &resolvedCopy
		}
	}
	if resolvedRow == nil {
		t.Fatalf("resolved foo row missing: %+v", rows)
	}
	if resolvedRow.DriftItem != nil && resolvedRow.DriftItem.Reason == materialize.DriftOrphaned {
		t.Fatalf("orphan output was attached to the resolved row: %+v", resolvedRow.DriftItem)
	}
	if len(orphanRows) != 3 {
		t.Fatalf("expected 3 orphan rows (2 targets + stray), got %d: %+v", len(orphanRows), rows)
	}
	type orphanIdentity struct {
		name, target, outDir, path string
	}
	wantOrphans := map[orphanIdentity]bool{
		{"foo", "claude", "/repo/.claude", "skills/foo/SKILL.md"}:   false,
		{"foo", "factory", "/repo/.factory", "skills/foo/SKILL.md"}: false,
		{"stray", "factory", "/repo/.factory", "skills/stray"}:      false,
	}
	for _, orphan := range orphanRows {
		id := orphanIdentity{orphan.Artifact.Name, string(orphan.DriftItem.Target), orphan.DriftItem.OutDir, orphan.DriftItem.Path}
		if _, known := wantOrphans[id]; !known {
			t.Fatalf("unexpected orphan row identity: %+v", orphan.DriftItem)
		}
		wantOrphans[id] = true
	}
	for id, seen := range wantOrphans {
		if !seen {
			t.Fatalf("orphan output %+v was collapsed or dropped; rows: %+v", id, rows)
		}
	}
}

// TestReadSnapshotHardRequiredSelectionFailureFailsDrift pins that a HARD
// resolution error — a required registry selection that cannot be found —
// makes the drift assessment failed, surfaces an error diagnostic, and can
// never render a clean drift verdict in the footer.
func TestReadSnapshotHardRequiredSelectionFailureFailsDrift(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	registryRoot := t.TempDir()
	skillDir := filepath.Join(registryRoot, "skills", "present")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: present\ndescription: Present\n---\n# Present\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	globalConfig := `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: ` + registryRoot + `
    required: true
    skills:
      - ghost
`
	if err := os.MkdirAll(filepath.Join(homeDir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if err := os.WriteFile(globalPath, []byte(globalConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.yaml"), []byte(repoV2Header), 0o644); err != nil {
		t.Fatal(err)
	}

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })
	defer restoreOrg()
	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(globalPath)
	})
	defer restoreGlobal()
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) { return nil, nil })
	defer restoreLocal()

	snap := readSnapshot(repoRoot, homeDir, catalogLoadCached)
	if snap.driftAssessment != driftFailed {
		t.Fatalf("drift assessment = %v, want failed for a hard required-not-found error", snap.driftAssessment)
	}
	foundResolutionError := false
	for _, diagnostic := range snap.diagnostics {
		if diagnostic.severity == diagnosticError && diagnostic.category == diagnosticResolution {
			foundResolutionError = true
		}
	}
	if !foundResolutionError {
		t.Fatalf("hard required-not-found error missing from diagnostics: %+v", snap.diagnostics)
	}

	m := NewModel(repoRoot, homeDir)
	seedTestTerminalSize(&m)
	snap.generation = m.requests.active(requestSnapshot)
	updated, _ := m.Update(snap)
	m = updated.(Model)
	footer := renderFooter(m)
	if strings.Contains(footer, "in sync") || !strings.Contains(footer, "drift check failed") {
		t.Fatalf("hard resolution error rendered a clean drift verdict: %q", footer)
	}
}

// TestReadSnapshotHardCollectionSelectionFailureFailsDrift pins the same
// hard-error drift contract for collection selections: a collection that
// cannot be resolved is a user config error, never a soft warning, so drift
// must be failed.
func TestReadSnapshotHardCollectionSelectionFailureFailsDrift(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	registryRoot := t.TempDir()
	skillDir := filepath.Join(registryRoot, "skills", "member")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: member\ndescription: Member\n---\n# Member\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	globalConfig := `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: ` + registryRoot + `
    collections:
      - ghost-collection
`
	if err := os.MkdirAll(filepath.Join(homeDir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if err := os.WriteFile(globalPath, []byte(globalConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.yaml"), []byte(repoV2Header), 0o644); err != nil {
		t.Fatal(err)
	}

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })
	defer restoreOrg()
	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(globalPath)
	})
	defer restoreGlobal()
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) { return nil, nil })
	defer restoreLocal()

	snap := readSnapshot(repoRoot, homeDir, catalogLoadCached)
	if snap.driftAssessment != driftFailed {
		t.Fatalf("drift assessment = %v, want failed for a hard collection selection error", snap.driftAssessment)
	}
	foundResolutionError := false
	for _, diagnostic := range snap.diagnostics {
		if diagnostic.severity == diagnosticError && diagnostic.category == diagnosticResolution {
			foundResolutionError = true
		}
	}
	if !foundResolutionError {
		t.Fatalf("hard collection selection error missing from diagnostics: %+v", snap.diagnostics)
	}
}

// TestStaleRowsCannotBeMutated pins the mutation half of the drift contract:
// once an accepted snapshot carries hard errors, the displayed rows are
// stale and space must NOT open the toggle modal. A later clean snapshot
// restores mutability.
func TestStaleRowsCannotBeMutated(t *testing.T) {
	m := newTestModel()
	m.loading = false
	// Select a real row.
	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatArtifact {
			m.cursor = i
			break
		}
	}

	// Accept a snapshot carrying a hard error: rows become stale.
	updated, _ := m.Update(snapshotLoadedMsg{
		generation:      m.requests.active(requestSnapshot),
		rows:            m.rows,
		mergedCfg:       &config.Config{},
		driftAssessment: driftFailed,
		diagnostics: []tuiDiagnostic{
			snapshotDiagnostic(diagnosticError, diagnosticResolution, errors.New("required resource missing")),
		},
	})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if m.pane == paneToggleModal {
		t.Fatal("toggle modal opened on stale rows")
	}
	if cmd == nil {
		t.Fatal("stale-row mutation block should surface a toast")
	}
	if msg, ok := cmd().(toastMsg); !ok || !strings.Contains(msg.text, "error") {
		t.Fatalf("stale-row mutation block toast = %#v", cmd())
	}

	// A clean snapshot restores mutability.
	updated, _ = m.Update(snapshotLoadedMsg{
		generation:      m.requests.active(requestSnapshot),
		rows:            m.rows,
		mergedCfg:       &config.Config{},
		driftAssessment: driftReady,
	})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if m.pane != paneToggleModal {
		t.Fatalf("toggle modal did not reopen after a clean snapshot: pane=%v", m.pane)
	}
}
