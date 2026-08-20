package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestHarnessKeyOpensGlobalHarnessModal(t *testing.T) {
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.globalHarnesses = []string{"cursor"}
	m.userHarnesses = []string{"cursor"}
	m.targetHarnesses = []string{"cursor"}
	m.targetHarnessSource = config.UserPersonal

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
	m = mi.(Model)
	if cmd != nil {
		t.Fatalf("opening harness modal should not emit command")
	}
	if m.pane != paneHarnessModal {
		t.Fatalf("expected harness modal, got %v", m.pane)
	}
	if m.harnessModal.scope != harnessScopeGlobal {
		t.Fatalf("expected global scope by default")
	}
	if !m.harnessModal.selected["cursor"] || m.harnessModal.selected["factory"] {
		t.Fatalf("expected global harness selection, got %#v", m.harnessModal.selected)
	}
}

func TestRenderHarnessModalShowsProvenanceAndActions(t *testing.T) {
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneHarnessModal
	m.targetHarnesses = []string{"factory"}
	m.targetHarnessSource = config.Organization
	m.openHarnessModal()

	out := renderHarnessModal(m)
	for _, want := range []string{"Target harnesses", "Editing:", "Effective:", "Factory Droid", "organization fallback (read-only)", "Global:", "Project:", "Cursor", "[g]", "[p]", "[space]", "[enter]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("modal missing %q in output:\n%s", want, out)
		}
	}
}

func TestRenderHarnessModalUsesRegistryLabelsInSummaries(t *testing.T) {
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.globalHarnesses = []string{"cursor"}
	m.projectHarnesses = []string{"codex"}
	m.openHarnessModal()

	out := renderHarnessModal(m)
	for _, want := range []string{"Effective: Codex", "Global:    Cursor", "Project:   Codex"} {
		if !strings.Contains(out, want) {
			t.Fatalf("modal missing registry label %q in summary:\n%s", want, out)
		}
	}
}

// TestHarnessModalOffersCodexAfterDevinAsAnOptInChoice pins the stable UI order
// and keeps both later-added targets opt-in.
func TestHarnessModalOffersCodexAfterDevinAsAnOptInChoice(t *testing.T) {
	ids := make([]string, 0, len(knownHarnessOptions))
	for _, opt := range knownHarnessOptions {
		ids = append(ids, opt.ID)
	}
	want := []string{"factory", "claude", "cursor", "windsurf", "gemini", "devin", "codex"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("harness modal options = %#v, want %#v", ids, want)
	}

	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneHarnessModal
	m.globalHarnesses = []string{"factory"}
	m.userHarnesses = []string{"factory"}
	m.targetHarnesses = []string{"factory"}
	m.targetHarnessSource = config.UserPersonal
	m.openHarnessModal()
	if m.harnessModal.selected["devin"] {
		t.Fatal("Devin must not be pre-selected: it is opt-in")
	}
	if m.harnessModal.selected["codex"] {
		t.Fatal("Codex must not be pre-selected: it is opt-in")
	}

	out := renderHarnessModal(m)
	if !strings.Contains(out, "Devin CLI") || !strings.Contains(out, "devin") {
		t.Fatalf("harness modal does not offer Devin:\n%s", out)
	}
	if !strings.Contains(out, "[ ] Devin CLI") {
		t.Fatalf("Devin should render as an unchecked option:\n%s", out)
	}
	if !strings.Contains(out, "[ ] Codex") || !strings.Contains(out, "codex") {
		t.Fatalf("Codex should render as an unchecked option after Devin:\n%s", out)
	}
}

// TestHarnessModalTogglesDevinIntoTheSavedList pins that the opt-in actually
// lands in the config the user is editing, and that it is written as the
// canonical target name.
func TestHarnessModalTogglesDevinIntoTheSavedList(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	configPath := filepath.Join(tmpHome, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\nharnesses:\n  - factory\nregistries: []\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	m := NewModel("/repo", tmpHome)
	seedTestTerminalSize(&m)
	m.globalHarnesses = []string{"factory"}
	m.userHarnesses = []string{"factory"}
	m.targetHarnesses = []string{"factory"}
	m.targetHarnessSource = config.UserPersonal
	m.openHarnessModal()
	// Walk to Devin and toggle it on without assuming it remains last.
	for knownHarnessOptions[m.harnessModal.cursor].ID != "devin" {
		mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = mi.(Model)
	}
	if got := knownHarnessOptions[m.harnessModal.cursor].ID; got != "devin" {
		t.Fatalf("cursor landed on %q, want devin", got)
	}
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mi.(Model)
	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(Model)
	if cmd == nil {
		t.Fatal("saving harnesses should emit a command")
	}
	if hw, ok := cmd().(harnessWrittenMsg); !ok || hw.err != nil {
		t.Fatalf("save failed: %#v", hw)
	}

	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"factory", "devin"}) {
		t.Fatalf("saved harnesses = %#v, want factory+devin", cfg.Harnesses)
	}
}

func TestHarnessModalSwitchScopeToggleAndSaveProject(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\nharnesses:\n  - factory\nregistries: []\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	m := NewModel(repoRoot, "/home")
	seedTestTerminalSize(&m)
	m.projectHarnesses = []string{"factory"}
	m.targetHarnesses = []string{"factory"}
	m.targetHarnessSource = config.RepoCommitted
	m.openHarnessModal()

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = mi.(Model)
	if cmd != nil {
		t.Fatalf("switching scope should not emit command")
	}
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mi.(Model)
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mi.(Model)
	mi, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("saving project harnesses should emit command")
	}
	hw, ok := cmd().(harnessWrittenMsg)
	if !ok || hw.err != nil {
		t.Fatalf("project harness save failed: %#v", hw)
	}
	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load project config: %v", err)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"factory", "claude"}) {
		t.Fatalf("expected project harnesses [factory claude], got %#v", cfg.Harnesses)
	}
}

func TestHarnessModalCreatesStrictV3UserOverrideFromOrganizationFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := NewModel("/repo", home)
	seedTestTerminalSize(&m)
	m.targetHarnesses = []string{"factory"}
	m.targetHarnessSource = config.Organization
	m.openHarnessModal()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("saving organization fallback did not emit a user override command")
	}
	msg := cmd().(harnessWrittenMsg)
	if msg.err != nil {
		t.Fatalf("save user override: %v", msg.err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "apiVersion: agent-marketplace/v3") || !strings.Contains(string(data), "- factory") {
		t.Fatalf("organization fallback override was not strict user v3:\n%s", data)
	}
}

func TestHarnessModalRejectsEmptySelection(t *testing.T) {
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.openHarnessModal()
	m.harnessModal.selected = map[string]bool{}

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("expected toast command for empty harness selection")
	}
	msg := cmd()
	toast, ok := msg.(toastMsg)
	if !ok {
		t.Fatalf("expected toastMsg, got %T", msg)
	}
	if toast.text != "select at least one harness" {
		t.Fatalf("unexpected toast: %q", toast.text)
	}
	if m.pane != paneHarnessModal {
		t.Fatalf("empty save should keep modal open")
	}
}

func TestHarnessModalTogglesAndSavesGlobalTargets(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	configPath := filepath.Join(tmpHome, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\nharnesses:\n  - factory\nregistries: []\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	m := NewModel("/repo", tmpHome)
	seedTestTerminalSize(&m)
	m.globalHarnesses = []string{"factory"}
	m.userHarnesses = []string{"factory"}
	m.targetHarnesses = []string{"factory"}
	m.targetHarnessSource = config.UserPersonal
	m.openHarnessModal()

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mi.(Model)
	if cmd != nil {
		t.Fatalf("moving cursor should not emit command")
	}
	mi, cmd = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mi.(Model)
	if cmd != nil {
		t.Fatalf("toggling harness should not emit command")
	}
	mi, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("saving global harnesses should emit command")
	}
	msg := cmd()
	hw, ok := msg.(harnessWrittenMsg)
	if !ok {
		t.Fatalf("expected harnessWrittenMsg, got %T", msg)
	}
	if hw.err != nil {
		t.Fatalf("save failed: %v", hw.err)
	}
	if hw.scopeLabel != "global" {
		t.Fatalf("expected global scope label, got %q", hw.scopeLabel)
	}

	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"factory", "claude"}) {
		t.Fatalf("expected global factory+claude harnesses, got %#v", cfg.Harnesses)
	}
}
