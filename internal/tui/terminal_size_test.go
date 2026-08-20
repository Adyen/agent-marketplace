package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func terminalContractModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m)
	m.loading = false
	m.driftAssessment = driftReady
	m.rows = []row{mkRow("selected", withProject, withRegistry("registry.git", "registry"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i, item := range m.flatItems {
		if item.Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	m.modalSkill = m.rows[0]
	m.initModal = initModalState{selected: map[string]bool{"factory": true}}
	m.targetHarnesses = []string{"factory"}
	m.targetHarnessSource = config.UserPersonal
	m.openHarnessModal()
	m.pane = paneList
	m.migrationPrompt = migrationPrompt{configs: []migrationConfig{{label: "Project", path: "/repo/.agents/config.yaml"}}}
	m.diagnostics.replaceSnapshot([]tuiDiagnostic{snapshotDiagnostic(diagnosticError, diagnosticConfig, errors.New("repair config"))})
	return m
}

func TestEveryPaneAndModalFitsSupportedMinimumWithRequiredActions(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*Model)
		tokens []string
	}{
		{name: "list", setup: func(m *Model) { m.pane = paneList }, tokens: []string{"q"}},
		{name: "search", setup: func(m *Model) { m.pane = paneList; m.filterActive = true; m.searchInput.Focus() }, tokens: []string{"enter", "esc"}},
		{name: "detail", setup: func(m *Model) { m.pane = paneDetail; m.detailViewport.SetContent(strings.Repeat("detail line\n", 20)) }, tokens: []string{"esc", "q"}},
		{name: "help", setup: func(m *Model) { m.pane = paneHelp }, tokens: []string{"esc"}},
		{name: "errors", setup: func(m *Model) { m.pane = paneErrors }, tokens: []string{"esc"}},
		{name: "confirm", setup: func(m *Model) { m.pane = paneConfirm; m.confirmMsg = "Sync and quit?" }, tokens: []string{"y", "n", "esc"}},
		{name: "toggle", setup: func(m *Model) { m.pane = paneToggleModal }, tokens: []string{"[g]", "[p]", "[l]", "[e]", "[esc]"}},
		{name: "init", setup: func(m *Model) { m.pane = paneInitModal }, tokens: []string{"[space]", "[enter]", "[esc]"}},
		{name: "migration", setup: func(m *Model) { m.pane = paneMigrationModal }, tokens: []string{"[m]", "[l]"}},
		{name: "harness", setup: func(m *Model) { m.openHarnessModal() }, tokens: []string{"[space]", "[enter]", "[esc]"}},
		{name: "edit config", setup: func(m *Model) { m.pane = paneEditConfigModal }, tokens: []string{"[g]", "[p]", "[l]", "[esc]"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := terminalContractModel(t)
			tc.setup(&m)
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
			m = updated.(Model)
			output := ansi.Strip(m.View())
			lines := strings.Split(output, "\n")
			if len(lines) > 12 {
				t.Fatalf("%s rendered %d rows at 50x12:\n%s", tc.name, len(lines), output)
			}
			for _, line := range lines {
				if width := ansi.StringWidth(line); width > 50 {
					t.Fatalf("%s rendered %d columns at 50x12: %q", tc.name, width, line)
				}
			}
			for _, token := range tc.tokens {
				if !strings.Contains(output, token) {
					t.Fatalf("%s hid required action %q at 50x12:\n%s", tc.name, token, output)
				}
			}
		})
	}
}

func TestModalActionsRemainVisibleAcrossConstrainedSupportedSizes(t *testing.T) {
	setups := []struct {
		name   string
		setup  func(*Model)
		tokens []string
	}{
		{name: "toggle", setup: func(m *Model) { m.pane = paneToggleModal }, tokens: []string{"[g]", "[p]", "[l]", "[e]", "[esc]"}},
		{name: "init", setup: func(m *Model) { m.pane = paneInitModal }, tokens: []string{"[space]", "[enter]", "[esc]"}},
		{name: "migration", setup: func(m *Model) { m.pane = paneMigrationModal }, tokens: []string{"[m]", "[l]"}},
		{name: "harness", setup: func(m *Model) { m.openHarnessModal() }, tokens: []string{"[space]", "[enter]", "[esc]"}},
		{name: "edit", setup: func(m *Model) { m.pane = paneEditConfigModal }, tokens: []string{"[g]", "[p]", "[l]", "[esc]"}},
	}
	for _, size := range []struct{ width, height int }{{51, 13}, {59, 24}, {80, 13}} {
		for _, setup := range setups {
			m := terminalContractModel(t)
			setup.setup(&m)
			updated, _ := m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
			output := ansi.Strip(updated.(Model).View())
			for _, token := range setup.tokens {
				if !strings.Contains(output, token) {
					t.Fatalf("%s hid %q at %dx%d:\n%s", setup.name, token, size.width, size.height, output)
				}
			}
		}
	}
}

func TestQuitRemainsVisibleAcrossSupportedWidths(t *testing.T) {
	for _, width := range []int{50, 80, 100, 200} {
		m := terminalContractModel(t)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 12})
		output := ansi.Strip(updated.(Model).View())
		if !strings.Contains(output, "q") || !strings.Contains(output, "quit") {
			t.Fatalf("quit guidance missing at %dx12:\n%s", width, output)
		}
	}
}

func TestBelowMinimumRendersOnlyBoundedResizeRecoveryState(t *testing.T) {
	for _, size := range []struct{ width, height int }{{49, 12}, {50, 11}, {20, 4}, {1, 1}} {
		m := terminalContractModel(t)
		m.pane = paneToggleModal
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		m = updated.(Model)
		output := ansi.Strip(m.View())
		if !strings.Contains(output, "q") || strings.Contains(output, "selected") || strings.Contains(output, "toggle global") {
			t.Fatalf("%dx%d did not render only resize/quit recovery state: %q", size.width, size.height, output)
		}
		lines := strings.Split(output, "\n")
		if len(lines) > size.height {
			t.Fatalf("%dx%d resize state used %d rows", size.width, size.height, len(lines))
		}
		for _, line := range lines {
			if got := ansi.StringWidth(line); got > size.width {
				t.Fatalf("%dx%d resize state used %d columns: %q", size.width, size.height, got, line)
			}
		}
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		if cmd == nil {
			t.Fatalf("%dx%d resize state hid functional quit", size.width, size.height)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%dx%d q produced %T, want tea.QuitMsg", size.width, size.height, cmd())
		}
	}
}

func TestResizeRecoveryPreservesWorkflowSelectionAndFocus(t *testing.T) {
	m := terminalContractModel(t)
	m.filterActive = true
	m.searchInput.SetValue("selected")
	m.searchInput.Focus()
	beforePane, beforeCursor, beforeFilter := m.pane, m.cursor, m.searchInput.Value()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.pane != beforePane || m.cursor != beforeCursor || m.searchInput.Value() != beforeFilter || !m.searchInput.Focused() {
		t.Fatalf("below-minimum input changed hidden workflow: pane=%v cursor=%d filter=%q focused=%t", m.pane, m.cursor, m.searchInput.Value(), m.searchInput.Focused())
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	m = updated.(Model)
	if m.pane != beforePane || m.cursor != beforeCursor || m.searchInput.Value() != beforeFilter || !m.searchInput.Focused() {
		t.Fatalf("resize recovery lost workflow: pane=%v cursor=%d filter=%q focused=%t", m.pane, m.cursor, m.searchInput.Value(), m.searchInput.Focused())
	}
	if output := ansi.Strip(m.View()); !strings.Contains(output, "selected") || !strings.Contains(output, "esc cancel") {
		t.Fatalf("recovered search workflow is inaccessible:\n%s", output)
	}
}

func TestSupportedSizeSeverityIsImpactBased(t *testing.T) {
	classify := func(output string, required []string, cosmeticClipping, workaround bool) string {
		for _, action := range required {
			if !strings.Contains(output, action) {
				return "High"
			}
		}
		if cosmeticClipping && workaround {
			return "Medium/Low"
		}
		return "None"
	}
	if got := classify("screen without recovery", []string{"[enter] recover"}, false, false); got != "High" {
		t.Fatalf("inaccessible required action severity = %s, want High", got)
	}

	m := terminalContractModel(t)
	m.rows[0].Artifact.Name = strings.Repeat("cosmetic-name-", 20)
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	output := ansi.Strip(updated.(Model).View())
	if strings.Contains(output, m.rows[0].Artifact.Name) {
		t.Fatal("fixture did not produce cosmetic clipping")
	}
	if got := classify(output, []string{"q:quit"}, true, true); got != "Medium/Low" {
		t.Fatalf("cosmetic clipping with workaround severity = %s, want Medium/Low", got)
	}
}

func TestResizeFromWideToSupportedMinimumPreservesWorkflowAndFocus(t *testing.T) {
	m := terminalContractModel(t)
	m.width = 120
	m.height = 40
	m.pane = paneHarnessModal
	m.harnessModal.cursor = len(knownHarnessOptions) - 1
	m.harnessModal.selected["devin"] = true
	beforePane := m.pane
	beforeCursor := m.harnessModal.cursor
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	m = updated.(Model)
	if m.pane != beforePane || m.harnessModal.cursor != beforeCursor || !m.harnessModal.selected["devin"] {
		t.Fatalf("resize changed active workflow: pane=%v cursor=%d selected=%v", m.pane, m.harnessModal.cursor, m.harnessModal.selected)
	}
	if output := ansi.Strip(m.View()); !strings.Contains(output, "Codex") || !strings.Contains(output, "[enter]") {
		t.Fatalf("focused constrained workflow is inaccessible:\n%s", output)
	}
}
