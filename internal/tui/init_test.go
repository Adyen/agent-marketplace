package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if m.pane != paneInitModal {
		t.Fatalf("expected paneInitModal when ~/.agents/config.yaml is missing, got %v", m.pane)
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
	if m.pane == paneInitModal {
		t.Fatalf("init should not appear when config exists")
	}
}

func TestNewModelSkipsInitWhenHomeDirMissing(t *testing.T) {
	withInitEnabled(t)
	m := NewModel("/repo", "/nonexistent-agent-marketplace-test-home")
	if m.pane == paneInitModal {
		t.Fatalf("init should not appear when homeDir itself is missing")
	}
}

func TestNewModelSkipsInitWhenHomeDirEmpty(t *testing.T) {
	withInitEnabled(t)
	m := NewModel("/repo", "")
	if m.pane == paneInitModal {
		t.Fatalf("init should not appear when homeDir is empty")
	}
}

func TestInitModalDismissesOnEsc(t *testing.T) {
	m := NewModel("/repo", "/home")
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
}

func TestInitModalRejectsEmptySelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	m := NewModel("/repo", home)
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
	m.pane = paneInitModal

	out := renderInitModal(m)
	for _, want := range []string{
		"Initialize target harnesses",
		"space",
		"enter",
		"esc",
		"factory",
		"claude",
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

func TestNewInitModalStateIsEmptyWithNoDetections(t *testing.T) {
	home := t.TempDir()

	state, err := newInitModalState(home)
	if err != nil {
		t.Fatalf("newInitModalState failed: %v", err)
	}
	if len(selectedHarnessIDs(state.selected)) != 0 {
		t.Fatalf("expected no pre-selected harnesses, got %v", state.selected)
	}
}

func TestRenderInitModalShowsNoDetectionMessage(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneInitModal
	m.initModal = initModalState{selected: map[string]bool{}}

	out := renderInitModal(m)
	if !strings.Contains(out, "No harness installation roots were detected") {
		t.Fatalf("expected no-detection message, got:\n%s", out)
	}
}
