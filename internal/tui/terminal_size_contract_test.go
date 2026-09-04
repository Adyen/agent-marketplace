package tui

import (
	"errors"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func assertTerminalBounds(t *testing.T, output string, width, height int) {
	t.Helper()
	lines := strings.Split(ansi.Strip(output), "\n")
	if len(lines) > height {
		t.Fatalf("rendered %d rows, want <= %d:\n%s", len(lines), height, ansi.Strip(output))
	}
	for _, line := range lines {
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("rendered %d columns, want <= %d: %q", got, width, line)
		}
	}
}

func TestSeedTerminalSizeDoesNotAssumeAValidFirstFrame(t *testing.T) {
	previous := probeTerminalSize
	t.Cleanup(func() { probeTerminalSize = previous })

	probeTerminalSize = func(*os.File) (int, int, bool) { return 40, 10, true }
	m := NewModel(t.TempDir(), t.TempDir())
	seedTerminalSize(&m, nil)
	if output := m.View(); !strings.Contains(output, "Resize required") {
		t.Fatalf("probed constrained first frame was not gated: %q", output)
	}

	probeTerminalSize = func(*os.File) (int, int, bool) { return 0, 0, false }
	m = NewModel(t.TempDir(), t.TempDir())
	seedTerminalSize(&m, nil)
	if !m.belowMinimumTerminal() || m.width != 0 || m.height != 0 {
		t.Fatalf("failed size probe assumed supported dimensions: %dx%d", m.width, m.height)
	}
}

func TestBelowMinimumTerminalGatesWorkflowButKeepsQuit(t *testing.T) {
	m := modelWithSelectedRow(t, mkRow("resource", withProject))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 49, Height: 12})
	m = updated.(Model)
	beforePane := m.pane

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if cmd != nil || m.pane != beforePane {
		t.Fatalf("below-minimum workflow input was accepted: pane=%v cmd=%v", m.pane, cmd)
	}
	output := m.View()
	assertTerminalBounds(t, output, 49, 12)
	if !strings.Contains(output, "Resize required") || !strings.Contains(output, "q quit") {
		t.Fatalf("resize recovery omitted guidance: %q", output)
	}

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !cmdProducesQuitMsg(cmd) {
		t.Fatal("below-minimum q did not quit")
	}
}

func TestHelpAndErrorsScrollAtSupportedMinimum(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.width = 50
	m.height = 12
	m.pane = paneHelp

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = updated.(Model)
	if m.helpViewport.YOffset == 0 {
		t.Fatal("ctrl+f did not page help overlay down")
	}
	pageOffset := m.helpViewport.YOffset
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	m = updated.(Model)
	if m.helpViewport.YOffset >= pageOffset {
		t.Fatalf("ctrl+b did not page help overlay up: before=%d after=%d", pageOffset, m.helpViewport.YOffset)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = updated.(Model)
	if m.helpViewport.YOffset == 0 {
		t.Fatal("help overlay did not scroll")
	}
	helpOffset := m.helpViewport.YOffset
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(Model)
	if m.helpViewport.YOffset != helpOffset {
		t.Fatalf("help offset changed across close/reopen: got %d want %d", m.helpViewport.YOffset, helpOffset)
	}

	m.pane = paneErrors
	m.errs = make([]error, 30)
	for i := range m.errs {
		m.errs[i] = errors.New("diagnostic line")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = updated.(Model)
	if m.errorsViewport.YOffset == 0 {
		t.Fatal("errors overlay did not scroll")
	}
	output := m.View()
	assertTerminalBounds(t, output, 50, 12)
	if !strings.Contains(output, "scroll") || !strings.Contains(output, "close") {
		t.Fatalf("scrollable errors footer missing: %s", output)
	}
}

func TestCoreViewsFitSupportedMinimumWithRecoveryActions(t *testing.T) {
	base := modelWithSelectedRow(t, mkRow("resource", withProject))
	base.width = 50
	base.height = 12
	base.driftAssessment = driftReady
	base.errs = []error{errors.New("failure")}
	base.warnings = []error{errors.New("warning")}
	base.detailViewport.Width = 50
	base.detailViewport.Height = 11
	base.detailViewport.SetContent(strings.Repeat("detail content\n", 30))

	for _, test := range []struct {
		name     string
		prepare  func(Model) Model
		required []string
	}{
		{name: "base", prepare: func(m Model) Model { return m }, required: []string{"q"}},
		{name: "help", prepare: func(m Model) Model { m.pane = paneHelp; return m }, required: []string{"esc/? close", "Ctrl+C quit"}},
		{name: "errors", prepare: func(m Model) Model { m.pane = paneErrors; return m }, required: []string{"esc/e close", "Ctrl+C quit"}},
		{name: "confirm", prepare: func(m Model) Model { m.pane = paneConfirm; m.confirmMsg = "Sync before quitting?"; return m }, required: []string{"y sync+quit", "n quit", "esc cancel"}},
		{name: "detail", prepare: func(m Model) Model { m.pane = paneDetail; return m }, required: []string{"esc/q back", "Ctrl+C quit"}},
		{name: "toggle", prepare: func(m Model) Model { m.pane = paneToggleModal; m.modalSkill = mkRow("resource", withProject); return m }, required: []string{"[g]", "[p]", "[l]", "[e]", "[esc]"}},
		{name: "harness", prepare: func(m Model) Model { m.pane = paneHarnessModal; m.openHarnessModal(); return m }, required: []string{"[space]", "[enter]", "[esc]"}},
		{name: "first run", prepare: func(m Model) Model {
			m.pane = paneInitModal
			m.initModal = initModalState{selected: map[string]bool{"factory": true}}
			return m
		}, required: []string{"[space]", "[enter]", "[esc]"}},
		{name: "edit config", prepare: func(m Model) Model { m.pane = paneEditConfigModal; return m }, required: []string{"[g]", "[p]", "[l]", "[esc]"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := ansi.Strip(test.prepare(base).View())
			assertTerminalBounds(t, output, 50, 12)
			for _, required := range test.required {
				if !strings.Contains(output, required) {
					t.Fatalf("required action %q is inaccessible:\n%s", required, output)
				}
			}
		})
	}
}
