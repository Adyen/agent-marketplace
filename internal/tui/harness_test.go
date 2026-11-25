package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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

	out := ansi.Strip(renderHarnessModal(m))
	for _, want := range []string{"Target harnesses", "Effective:", "Project:", "Global:", "Factory Droid", "Cursor", "p project", "g global", "space toggle", "enter save"} {
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
	want := []string{"factory", "claude", "cursor", "windsurf", "gemini", "devin", "codex", "opencode"}
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
	if m.harnessModal.selected["opencode"] {
		t.Fatal("OpenCode must remain opt-in")
	}
	if m.harnessModal.selected["codex"] {
		t.Fatal("Codex must not be pre-selected: it is opt-in")
	}

	out := ansi.Strip(renderHarnessModal(m))
	if !strings.Contains(out, "[ ] OpenCode") {
		t.Fatalf("OpenCode choice missing: %s", out)
	}
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
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\nsources: {}\nresources: {}\n"), 0o644); err != nil {
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

// spec: session-interactions/save-selected-harnesses
// spec: tui-interactions/enter-saves-selected-harness-targets
func TestHarnessModalSwitchScopeToggleAndSaveProject(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v4\nsources: {}\nresources: {}\n"), 0o644); err != nil {
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

// spec: session-interactions/empty-selection
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
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\nsources: {}\nresources: {}\n"), 0o644); err != nil {
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

// TestHarnessModalSavesGlobalHarnessesToV4Config pins that the harness modal
// edits the v4 root without losing its existing source/resource selections.
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

// spec: tui-interactions/harness-options-return-after-compact-resize
func TestHarnessDialogsRestoreAllOptionsAfterCompactResize(t *testing.T) {
	type dialog struct {
		name     string
		pane     pane
		open     func(*Model)
		ids      func(Model) []string
		selected func(Model) map[string]bool
		cursor   func(Model) int
		hovered  func(Model) string
	}
	selectedIDs := []string{"factory", "claude", "opencode"}
	selected := make(map[string]bool, len(selectedIDs))
	for _, id := range selectedIDs {
		selected[id] = true
	}
	dialogs := []dialog{
		{
			name: "targets",
			pane: paneHarnessModal,
			open: func(m *Model) {
				m.globalHarnesses = append([]string(nil), selectedIDs...)
				m.openHarnessModal()
			},
			ids: func(m Model) []string { return m.harnessModal.optionIDs },
			selected: func(m Model) map[string]bool {
				return m.harnessModal.selected
			},
			cursor: func(m Model) int { return m.harnessModal.cursor },
			hovered: func(m Model) string {
				id, _ := m.harnessModal.multi.Hovered()
				return id
			},
		},
		{
			name: "initialization",
			pane: paneInitModal,
			open: func(m *Model) {
				m.initModal = initModalState{selected: harnessSelection(selectedIDs), detected: true}
				m.ensureInitForm()
			},
			ids: func(m Model) []string { return m.initModal.optionIDs },
			selected: func(m Model) map[string]bool {
				return m.initModal.selected
			},
			cursor: func(m Model) int { return m.initModal.cursor },
			hovered: func(m Model) string {
				id, _ := m.initModal.multi.Hovered()
				return id
			},
		},
	}

	for _, dialog := range dialogs {
		t.Run(dialog.name, func(t *testing.T) {
			m := NewModel(t.TempDir(), t.TempDir())
			m.loading = false
			m.width, m.height = 80, 30
			m.pane = dialog.pane
			dialog.open(&m)
			ids := dialog.ids(m)
			if len(ids) < 2 {
				t.Fatalf("option fixture = %v, want multiple choices", ids)
			}
			for range len(ids) - 1 {
				updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
				m = updated.(Model)
			}
			lastSelected := selectedIDs[len(selectedIDs)-1]
			if got := dialog.cursor(m); got != len(ids)-1 || dialog.hovered(m) != lastSelected {
				t.Fatalf("initial cursor=%d hovered=%q, want selected supported option %q at %d", got, dialog.hovered(m), lastSelected, len(ids)-1)
			}
			if !reflect.DeepEqual(dialog.selected(m), selected) {
				t.Fatalf("initial selected IDs = %#v, want %#v", dialog.selected(m), selected)
			}

			fullView := ansi.Strip(m.viewContent())
			assertHarnessOptionsVisible(t, fullView, ids)
			cursorBefore, hoveredBefore := dialog.cursor(m), dialog.hovered(m)
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
			m = updated.(Model)
			compactView := ansi.Strip(m.viewContent())
			if visible := visibleHarnessOptionCount(compactView, ids); visible >= len(ids) {
				t.Fatalf("compact layout displayed all %d choices in 50x12", visible)
			}
			updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
			m = updated.(Model)
			_ = m.viewContent()
			updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			m = updated.(Model)
			if cmd != nil {
				t.Fatalf("no-op navigation at the last choice emitted a command: %v", cmd)
			}
			fullView = ansi.Strip(m.viewContent())
			assertHarnessOptionsVisible(t, fullView, ids)
			if !reflect.DeepEqual(dialog.selected(m), selected) || dialog.cursor(m) != cursorBefore || dialog.hovered(m) != hoveredBefore {
				t.Fatalf("resize changed selected IDs=%#v cursor=%d hovered=%q", dialog.selected(m), dialog.cursor(m), dialog.hovered(m))
			}
		})
	}
}

// A harness dialog scrolled while compact shows every choice again as soon
// as the terminal is restored, without another key press.
// spec: tui-interactions/harness-options-return-after-compact-resize
func TestHarnessDialogsUnscrollWhenRestoredAfterCompactScrolling(t *testing.T) {
	for _, dialog := range []struct {
		name    string
		open    func(*Model)
		ids     func(Model) []string
		hovered func(Model) string
	}{
		{
			name: "targets",
			open: func(m *Model) {
				m.globalHarnesses = []string{"factory"}
				m.pane = paneHarnessModal
				m.openHarnessModal()
			},
			ids: func(m Model) []string { return m.harnessModal.optionIDs },
			hovered: func(m Model) string {
				id, _ := m.harnessModal.multi.Hovered()
				return id
			},
		},
		{
			name: "initialization",
			open: func(m *Model) {
				m.pane = paneInitModal
				m.initModal = initModalState{selected: harnessSelection([]string{"claude"}), detected: true}
				m.ensureInitForm()
			},
			ids: func(m Model) []string { return m.initModal.optionIDs },
			hovered: func(m Model) string {
				id, _ := m.initModal.multi.Hovered()
				return id
			},
		},
	} {
		t.Run(dialog.name, func(t *testing.T) {
			m := NewModel(t.TempDir(), t.TempDir())
			m.loading = false
			m.width, m.height = 80, 24
			dialog.open(&m)
			ids := dialog.ids(m)
			_ = m.viewContent()
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
			m = updated.(Model)
			_ = m.viewContent()
			for range len(ids) - 1 {
				updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
				m = updated.(Model)
				_ = m.viewContent()
			}
			last := ids[len(ids)-1]
			if got := dialog.hovered(m); got != last {
				t.Fatalf("compact cursor hovers %q, want the last choice %q", got, last)
			}
			updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m = updated.(Model)
			assertHarnessOptionsVisible(t, ansi.Strip(m.viewContent()), ids)
			if got := dialog.hovered(m); got != last {
				t.Fatalf("restoring the size moved the cursor to %q, want %q", got, last)
			}
		})
	}
}

func assertHarnessOptionsVisible(t *testing.T, view string, ids []string) {
	t.Helper()
	if visible := visibleHarnessOptionCount(view, ids); visible != len(ids) {
		t.Fatalf("visible harness choices = %d, want %d:\n%s", visible, len(ids), view)
	}
}

func visibleHarnessOptionCount(view string, ids []string) int {
	lines := strings.Split(view, "\n")
	visible := 0
	for _, id := range ids {
		for _, line := range lines {
			if strings.Contains(line, id) && (strings.Contains(line, "[ ]") || strings.Contains(line, "[x]") || strings.Contains(line, "[•]")) {
				visible++
				break
			}
		}
	}
	return visible
}
