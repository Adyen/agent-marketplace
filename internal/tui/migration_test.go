package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestSnapshotV2ConfigDoesNotOpenMigrationModal(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	writeV2Config(t, filepath.Join(repoRoot, ".agents", "config.yaml"))

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreOrg()

	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreGlobal()

	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()

	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return nil, nil
	})
	defer restoreLocal()

	m := NewModel(repoRoot, homeDir)
	seedTestTerminalSize(&m)
	snapshot := readSnapshot(repoRoot, homeDir, catalogLoadCached)
	snapshot.generation = m.requests.active(requestSnapshot)
	mi, _ := m.Update(snapshot)
	m = mi.(Model)

	if m.pane != paneList {
		t.Fatalf("expected list pane for v2 config, got pane=%v", m.pane)
	}
}

func TestSnapshotV1ConfigDoesNotOpenMigrationModal(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	writeV1Config(t, filepath.Join(repoRoot, ".agents", "config.yaml"))

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreOrg()

	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreGlobal()

	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()

	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return nil, nil
	})
	defer restoreLocal()

	m := NewModel(repoRoot, homeDir)
	seedTestTerminalSize(&m)
	snapshot := readSnapshot(repoRoot, homeDir, catalogLoadCached)
	snapshot.generation = m.requests.active(requestSnapshot)
	mi, _ := m.Update(snapshot)
	m = mi.(Model)

	if m.pane == paneMigrationModal {
		t.Fatalf("did not expect migration modal for v1 config, got pane=%v", m.pane)
	}
	if len(m.migrationPrompt.configs) != 0 {
		t.Fatalf("expected no pending migration, got %d", len(m.migrationPrompt.configs))
	}
}

func TestMigrationModalLeaveKeepsConfigUnchanged(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeV2Config(t, configPath)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	m := NewModel(repoRoot, t.TempDir())
	seedTestTerminalSize(&m)
	m.pane = paneMigrationModal
	m.migrationPrompt = pendingMigrationFor(repoRoot, "")

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = mi.(Model)
	if cmd != nil {
		t.Fatalf("leave should not emit a migration command")
	}
	if m.pane != paneList {
		t.Fatalf("leave should close modal, got pane=%v", m.pane)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("leave changed config:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestMigrationModalRunMigratesConfig(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	globalAgentsDir := filepath.Join(homeDir, ".agents")
	if err := os.MkdirAll(globalAgentsDir, 0o755); err != nil {
		t.Fatalf("mkdir global agents: %v", err)
	}
	globalConfigPath := filepath.Join(globalAgentsDir, "config.yaml")
	writeV2Config(t, globalConfigPath)

	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeV2Config(t, configPath)

	m := NewModel(repoRoot, homeDir)
	seedTestTerminalSize(&m)
	m.pane = paneMigrationModal
	m.migrationPrompt = pendingMigrationFor(repoRoot, homeDir)

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("run should emit a migration command")
	}
	if m.pane != paneList {
		t.Fatalf("run should close modal before command completes, got pane=%v", m.pane)
	}

	msg := cmd()
	done, ok := msg.(migrationDoneMsg)
	if !ok {
		t.Fatalf("expected migrationDoneMsg, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("migration failed: %v", done.err)
	}
	if done.count != 1 {
		t.Fatalf("expected only the incomplete user config to be remediated, got %d migrations", done.count)
	}

	repoCfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load migrated repo config: %v", err)
	}
	if repoCfg.APIVersion != config.APIVersionV2 {
		t.Fatalf("expected repo apiVersion %q, got %q", config.APIVersionV2, repoCfg.APIVersion)
	}
	if len(repoCfg.Harnesses) != 0 {
		t.Fatalf("expected repo config to have no harnesses, got %v", repoCfg.Harnesses)
	}

	globalCfg, err := config.LoadConfigFromPath(globalConfigPath)
	if err != nil {
		t.Fatalf("load migrated global config: %v", err)
	}
	if !equalStrings(globalCfg.Harnesses, config.DefaultHarnesses()) {
		t.Fatalf("expected global harnesses %v, got %v", config.DefaultHarnesses(), globalCfg.Harnesses)
	}
}

func TestRenderMigrationModalIncludesExactPlan(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoPath, []byte("apiVersion: agent-marketplace/v2\nharnesses: [cursor]\nregistries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel(repoRoot, homeDir)
	seedTestTerminalSize(&m)
	m.height = 40
	m.pane = paneMigrationModal
	m.migrationPrompt = migrationPrompt{
		configs: []migrationConfig{{label: "Project", path: repoPath}},
		plan: &config.MigrationPlan{
			TargetHarnesses: []string{"cursor"},
			TargetSource:    config.MigrationTargetRepo,
			Files:           []config.MigrationPlannedFile{{Layer: "repo", Path: repoPath}},
		},
	}

	out := renderMigrationModal(m)
	for _, want := range []string{
		"Config remediation required",
		"agent-marketplace/v2 is canonical",
		"Exact target set: cursor",
		"Target source: repository",
		"migrate Project",
		repoPath,
		"source-file digest",
		"[m]",
		"[l]",
		"leave config unchanged",
	} {
		if !contains(out, want) {
			t.Fatalf("migration modal missing %q in output:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"factory, windsurf", "Fallback targets", "generic defaults"} {
		if contains(out, unwanted) {
			t.Fatalf("migration modal contains generic plan text %q:\n%s", unwanted, out)
		}
	}
}

func TestCatalogFailureSnapshotPreservesEffectiveConfigAndTargets(t *testing.T) {
	induced := errors.New("catalog unavailable")
	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return &config.Config{
			APIVersion: config.APIVersionV3,
			Harnesses:  []string{"factory"},
			Registries: []config.RegistryEntry{{URL: "https://example.com/org.git", Ref: "main", Default: true}},
		}, nil
	})
	defer restoreOrg()
	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) { return nil, nil })
	defer restoreGlobal()
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) { return nil, nil })
	defer restoreRepo()
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) { return nil, nil })
	defer restoreLocal()
	restoreClone := swapCloneOrPull(func(string, string) (string, error) { return "", induced })
	defer restoreClone()

	snapshot := readSnapshot(t.TempDir(), t.TempDir(), catalogLoadCached)
	if snapshot.targetHarnessSource != config.Organization || len(snapshot.targetHarnesses) != 1 || snapshot.targetHarnesses[0] != "factory" {
		t.Fatalf("catalog failure target state = %v from %v", snapshot.targetHarnesses, snapshot.targetHarnessSource)
	}
	if snapshot.mergedCfg == nil || len(snapshot.mergedCfg.Registries) != 1 {
		t.Fatalf("catalog failure lost effective config: %+v", snapshot.mergedCfg)
	}
	if len(diagnosticErrors(snapshot.diagnostics)) == 0 {
		t.Fatal("catalog failure was not reported")
	}
}

func TestCompactMigrationPreviewShowsExactPlan(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoPath, []byte("apiVersion: agent-marketplace/v2\nharnesses: [cursor]\nregistries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewModel(repoRoot, homeDir)
	seedTestTerminalSize(&m)
	m.width = 50
	m.height = 12
	m.migrationPrompt = migrationPrompt{
		configs: []migrationConfig{{label: "Project", path: repoPath}},
		plan: &config.MigrationPlan{
			TargetHarnesses: []string{"cursor"},
			TargetSource:    config.MigrationTargetRepo,
			Files:           []config.MigrationPlannedFile{{Layer: "repo", Path: repoPath}},
		},
	}
	out := renderMigrationModal(m)
	for _, want := range []string{"Targets: cursor (repository)", "migrate Project"} {
		if !contains(out, want) {
			t.Fatalf("compact preview missing %q:\n%s", want, out)
		}
	}
	if contains(out, "Fallback targets") || contains(out, "factory,windsurf") {
		t.Fatalf("compact preview showed generic defaults:\n%s", out)
	}
}

func writeV1Config(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := []byte("registries:\n  - url: ./skills\n    skills:\n      - audit\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write v1 config: %v", err)
	}
}

func writeV2Config(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := []byte("apiVersion: agent-marketplace/v2\nregistries:\n  - url: ./skills\n    skills:\n      - audit\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write v2 config: %v", err)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
