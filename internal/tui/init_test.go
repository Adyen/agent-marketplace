package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain silences the first-run init modal for every test in the tui
// package by default. Tests that specifically exercise init behavior
// (below) flip testSkipInitModal off for the duration of their assertion.
func TestMain(m *testing.M) {
	testSkipInitModal = true
	lipgloss.SetColorProfile(termenv.Ascii)
	lipgloss.SetHasDarkBackground(true)
	// The runtime allowlist is now derived from effective configuration
	// layers. TUI tests use example.com URLs with no allowlist configured
	// (permitting all) unless they explicitly declare one.
	code := m.Run()
	os.Exit(code)
}

// withInitEnabled re-enables the init modal for a single test. Callers
// should defer the returned cleanup so later tests see the default
// (suppressed) behavior.
func withInitEnabled(t *testing.T) {
	t.Helper()
	prev := testSkipInitModal
	testSkipInitModal = false
	t.Cleanup(func() { testSkipInitModal = prev })
}

func TestNewModelShowsInitWhenGlobalConfigMissing(t *testing.T) {
	withInitEnabled(t)
	home := t.TempDir()

	m := NewModel("/repo", home)
	seedTestTerminalSize(&m)
	if m.pane != paneInitModal {
		t.Fatalf("expected paneInitModal when ~/.agents/config.yaml is missing, got %v", m.pane)
	}
}

func TestNewModelSkipsInitWhenOrganizationTargetsAreEffective(t *testing.T) {
	withInitEnabled(t)
	home := t.TempDir()
	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(orgPath, []byte("apiVersion: agent-marketplace/v3\nharnesses: [factory]\n"), 0o644); err != nil {
		t.Fatalf("write organization config: %v", err)
	}
	config.SetTestV3OrganizationConfigPath(t, orgPath)

	m := NewModel("/repo", home)
	seedTestTerminalSize(&m)
	if m.pane == paneInitModal {
		t.Fatal("init should not appear when organization targets are the effective fallback")
	}
}

func TestNewModelSkipsInitWhenGlobalConfigExists(t *testing.T) {
	withInitEnabled(t)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v2\nharnesses:\n  - factory\nregistries: []\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	m := NewModel("/repo", home)
	seedTestTerminalSize(&m)
	if m.pane == paneInitModal {
		t.Fatalf("init should not appear when config exists")
	}
}

func TestNewModelSkipsInitWhenHomeDirMissing(t *testing.T) {
	withInitEnabled(t)
	m := NewModel("/repo", "/nonexistent-agent-marketplace-test-home")
	seedTestTerminalSize(&m)
	if m.pane == paneInitModal {
		t.Fatalf("init should not appear when homeDir itself is missing")
	}
}

func TestNewModelSkipsInitWhenHomeDirEmpty(t *testing.T) {
	withInitEnabled(t)
	m := NewModel("/repo", "")
	seedTestTerminalSize(&m)
	if m.pane == paneInitModal {
		t.Fatalf("init should not appear when homeDir is empty")
	}
}

func TestInitModalDismissesOnEsc(t *testing.T) {
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneInitModal

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mi.(Model)
	if m.pane != paneList {
		t.Fatalf("esc should cancel init, got pane=%v", m.pane)
	}
	if cmd != nil {
		t.Fatalf("init cancel should not emit a cmd")
	}
}

func TestInitModalTogglesAndSavesHarnesses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	m := NewModel("/repo", home)
	seedTestTerminalSize(&m)
	m.pane = paneInitModal
	m.initModal = initModalState{selected: map[string]bool{"factory": true}}

	// Toggle claude on.
	m.initModal.cursor = 1 // claude
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mi.(Model)
	if !m.initModal.selected["claude"] {
		t.Fatal("space should select claude")
	}

	// Save.
	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(Model)
	if m.pane != paneList {
		t.Fatalf("enter should dismiss init, got pane=%v", m.pane)
	}
	if cmd == nil {
		t.Fatal("save should emit a command")
	}

	msg := cmd().(harnessWrittenMsg)
	if msg.err != nil {
		t.Fatalf("save failed: %v", msg.err)
	}

	cfgPath := filepath.Join(home, ".agents", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "apiVersion: agent-marketplace/v3") {
		t.Fatalf("expected v3 config, got:\n%s", content)
	}
	if !strings.Contains(content, "factory") || !strings.Contains(content, "claude") {
		t.Fatalf("expected factory and claude harnesses, got:\n%s", content)
	}
	if strings.Contains(content, "devin") {
		t.Fatalf("init added Devin without detection or selection:\n%s", content)
	}
}

func TestInitModalSavesDetectedDevinToUserV3Config(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".config", "devin"), 0o755); err != nil {
		t.Fatal(err)
	}
	state, err := newInitModalState(home)
	if err != nil {
		t.Fatalf("newInitModalState: %v", err)
	}
	m := NewModel("/repo", home)
	seedTestTerminalSize(&m)
	m.pane = paneInitModal
	m.initModal = state

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("saving detected Devin should emit a command")
	}
	msg := cmd().(harnessWrittenMsg)
	if msg.err != nil {
		t.Fatalf("save detected Devin: %v", msg.err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "apiVersion: agent-marketplace/v3") || !strings.Contains(content, "- devin") {
		t.Fatalf("detected Devin was not saved to strict user v3 config:\n%s", content)
	}
}

func TestSuccessfulFirstRunSaveReloadsPersistedUserState(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	t.Setenv("HOME", home)
	m := NewModel(repo, home)
	seedTestTerminalSize(&m)
	id, ok := m.beginOperation(operationTargets, nil)
	if !ok {
		t.Fatal("targets operation did not start")
	}

	_, cmd := m.Update(harnessWrittenMsg{operation: id, scopeLabel: "user-personal"})
	if cmd == nil {
		t.Fatal("successful first-run save did not schedule reload")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("successful first-run save returned %T, want tea.BatchMsg", cmd())
	}
	foundSnapshot := false
	for _, batchCmd := range batch {
		if _, ok := batchCmd().(snapshotLoadedMsg); ok {
			foundSnapshot = true
			break
		}
	}
	if !foundSnapshot {
		t.Fatal("successful first-run save did not reload a persisted snapshot")
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("first-run flow created or touched repository config: %v", err)
	}
}

func TestFirstRunSaveUpdatesExistingV3AndMigratesV2(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{name: "v3 without harnesses", content: "# preserve\napiVersion: agent-marketplace/v3\nregistries: []\n"},
		{name: "v2", content: "# preserve\napiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			path := filepath.Join(home, ".agents", "config.yaml")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			msg := runInitSaveCmd([]string{"cursor"}, 1)().(harnessWrittenMsg)
			if msg.err != nil {
				t.Fatalf("first-run save: %v", msg.err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "agent-marketplace/v3") || !strings.Contains(string(data), "cursor") || !strings.Contains(string(data), "# preserve") {
				t.Fatalf("persisted first-run state:\n%s", data)
			}
		})
	}
}

func TestInitModalRejectsEmptySelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	m := NewModel("/repo", home)
	seedTestTerminalSize(&m)
	m.pane = paneInitModal
	m.initModal = initModalState{selected: map[string]bool{}}

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(Model)
	if cmd == nil {
		t.Fatal("expected toast command for empty selection")
	}
	msg := cmd().(toastMsg)
	if msg.text == "" {
		t.Fatal("expected toast message")
	}
}

func TestRenderInitModalIncludesHints(t *testing.T) {
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneInitModal

	out := renderInitModal(m)
	for _, want := range []string{
		"Initialize target harnesses",
		"space",
		"enter",
		"esc",
		"factory",
		"claude",
		"Devin CLI",
		"Codex",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("init modal missing %q in output:\n%s", want, out)
		}
	}
}

func TestNewInitModalStatePreselectsDetectedHarnesses(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".factory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	state, err := newInitModalState(home)
	if err != nil {
		t.Fatalf("newInitModalState failed: %v", err)
	}
	if !state.selected["factory"] || !state.selected["claude"] {
		t.Fatalf("expected factory and claude to be pre-selected, got %v", state.selected)
	}
}

func TestNewInitModalStatePreselectsResolvedCodexHome(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}

	state, err := newInitModalState(home)
	if err != nil {
		t.Fatal(err)
	}
	if !state.selected["codex"] {
		t.Fatalf("resolved CODEX_HOME was not pre-selected: %v", state.selected)
	}
}

func TestNewInitModalStatePreselectsDevinInstallation(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".config", "devin"), 0o755); err != nil {
		t.Fatal(err)
	}

	state, err := newInitModalState(home)
	if err != nil {
		t.Fatalf("newInitModalState failed: %v", err)
	}
	if !state.selected["devin"] {
		t.Fatalf("expected detected Devin root to be pre-selected, got %v", state.selected)
	}
}

func TestNewInitModalStateIsEmptyWithNoDetections(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")

	state, err := newInitModalState(home)
	if err != nil {
		t.Fatalf("newInitModalState failed: %v", err)
	}
	if len(selectedHarnessIDs(state.selected)) != 0 {
		t.Fatalf("expected no pre-selected harnesses, got %v", state.selected)
	}
	if state.selected["codex"] {
		t.Fatal("Codex must remain unchecked when no install root is detected")
	}
}

func TestRenderInitModalShowsNoDetectionMessage(t *testing.T) {
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneInitModal
	m.initModal = initModalState{selected: map[string]bool{}}

	out := renderInitModal(m)
	if !strings.Contains(out, "No harness installation roots were detected") {
		t.Fatalf("expected no-detection message, got:\n%s", out)
	}
}
