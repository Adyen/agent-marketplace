package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestHarnessKeyOpensGlobalHarnessModal(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.projectHarnesses = []string{"factory"}
	m.globalHarnesses = []string{"cursor"}

	mi, cmd := m.Update(tea.KeyPressMsg{Code: 'H', Text: string('H')})
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
	m.pane = paneHarnessModal
	m.projectHarnesses = []string{"factory"}
	m.globalHarnesses = []string{"cursor"}
	m.openHarnessModal()

	out := renderHarnessModal(m)
	for _, want := range []string{"Target harnesses", "Effective:", "Project:", "Global:", "Factory Droid", "Cursor", "[p]", "[g]", "[space]", "[enter]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("modal missing %q in output:\n%s", want, out)
		}
	}
}

func TestRenderHarnessModalUsesRegistryLabelsInSummaries(t *testing.T) {
	m := NewModel("/repo", "/home")
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
	m.pane = paneHarnessModal
	m.globalHarnesses = []string{"factory"}
	m.projectHarnesses = nil
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
	m.globalHarnesses = []string{"factory"}
	m.openHarnessModal()
	// Walk to Devin and toggle it on without assuming it remains last.
	for knownHarnessOptions[m.harnessModal.cursor].ID != "devin" {
		mi, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m = mi.(Model)
	}
	if got := knownHarnessOptions[m.harnessModal.cursor].ID; got != "devin" {
		t.Fatalf("cursor landed on %q, want devin", got)
	}
	mi, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = mi.(Model)
	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\nregistries: []\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	m := NewModel(repoRoot, "/home")
	m.projectHarnesses = []string{"factory"}
	m.openHarnessModal()

	mi, cmd := m.Update(tea.KeyPressMsg{Code: 'p', Text: string('p')})
	m = mi.(Model)
	if cmd != nil {
		t.Fatalf("switching scope should not emit command")
	}
	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = mi.(Model)
	if cmd != nil {
		t.Fatalf("moving cursor should not emit command")
	}
	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = mi.(Model)
	if cmd != nil {
		t.Fatalf("toggling harness should not emit command")
	}
	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("saving harnesses should emit command")
	}
	msg := cmd()
	hw, ok := msg.(harnessWrittenMsg)
	if !ok {
		t.Fatalf("expected harnessWrittenMsg, got %T", msg)
	}
	if hw.err != nil {
		t.Fatalf("expected project harness save to succeed: %v", hw.err)
	}
	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load project config: %v", err)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"factory", "claude"}) {
		t.Fatalf("expected project harnesses [factory claude], got %#v", cfg.Harnesses)
	}
}

func TestHarnessModalRejectsEmptySelection(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.openHarnessModal()
	m.harnessModal.selected = map[string]bool{}

	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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

func TestHarnessModalSwitchScopeToggleAndSaveGlobal(t *testing.T) {
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
	m.projectHarnesses = []string{"cursor"}
	m.globalHarnesses = []string{"factory"}
	m.openHarnessModal()

	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = mi.(Model)
	if cmd != nil {
		t.Fatalf("moving cursor should not emit command")
	}
	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = mi.(Model)
	if cmd != nil {
		t.Fatalf("toggling harness should not emit command")
	}
	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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

// TestHarnessModalSavesGlobalHarnessesToV4Config pins the v4.writer-opt-in
// regression in the TUI harness modal: after init or the startup migration
// prompt converts ~/.agents/config.yaml to v4, saving from the harness
// modal must edit the v4 root instead of failing with "legacy config
// writers do not edit v4 files".
func TestHarnessModalSavesGlobalHarnessesToV4Config(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	configPath := filepath.Join(tmpHome, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\nsources:\n  agents:\n    git: https://example.test/agents.git\n    ref: main\nresources:\n  agents:\n    include:\n      - skill/review\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	m := NewModel("/repo", tmpHome)
	m.globalHarnesses = []string{"factory"}
	m.openHarnessModal()

	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = mi.(Model)
	if cmd != nil {
		t.Fatalf("moving cursor should not emit command")
	}
	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = mi.(Model)
	if cmd != nil {
		t.Fatalf("toggling harness should not emit command")
	}
	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("saving harnesses should emit command")
	}
	msg := cmd()
	hw, ok := msg.(harnessWrittenMsg)
	if !ok {
		t.Fatalf("expected harnessWrittenMsg, got %T", msg)
	}
	if hw.err != nil {
		t.Fatalf("v4 harness save failed: %v", hw.err)
	}

	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load v4 config after save: %v", err)
	}
	if cfg.APIVersion != config.APIVersionV4 {
		t.Fatalf("apiVersion = %q, want %q", cfg.APIVersion, config.APIVersionV4)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"factory", "claude"}) {
		t.Fatalf("expected global factory+claude harnesses, got %#v", cfg.Harnesses)
	}
	if len(cfg.Registries) != 1 || len(cfg.Registries[0].Skills.Include) == 0 || cfg.Registries[0].Skills.Include[0] != "review" {
		t.Fatalf("expected v4 sources preserved after harness save, got %#v", cfg.Registries)
	}
}
