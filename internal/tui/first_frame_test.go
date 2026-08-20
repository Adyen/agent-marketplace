package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The model must not assume a terminal size before the first real
// WindowSizeMsg: an unknown size is treated as below-minimum so the first
// frame is the bounded resize-required state rather than a full UI painted
// at a guessed 80x24 onto a smaller physical terminal.

func TestNewModelStartsWithUnknownTerminalSize(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	if !m.belowMinimumTerminal() {
		t.Fatalf("fresh model must treat unknown size %dx%d as below minimum", m.width, m.height)
	}
	// At 0x0 the frame cannot contain even the recovery guidance, so the
	// bounded state renders nothing rather than overflowing.
	if out := m.View(); out != "" {
		t.Fatalf("first frame at unknown size rendered %q, want empty bounded state", out)
	}
	// As soon as any real (below-minimum) size arrives, the bounded
	// resize-required state with quit guidance takes over.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 20, Height: 4})
	out := ansi.Strip(updated.(Model).View())
	if !strings.Contains(out, "Resize required") || !strings.Contains(out, "q quit") {
		t.Fatalf("first real frame must be the bounded resize state with quit guidance:\n%s", out)
	}
}

func TestWindowSizeMsgLiftsUnknownSizeToRealSize(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	if m.belowMinimumTerminal() {
		t.Fatal("real size did not lift the below-minimum state")
	}
	out := ansi.Strip(m.View())
	if strings.Contains(out, "Resize required") {
		t.Fatalf("supported size still renders the resize state:\n%s", out)
	}
}

func TestSeedTerminalSizeSkipsNonTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close(); _ = w.Close() }()

	m := NewModel(t.TempDir(), t.TempDir())
	seedTerminalSize(&m, w)
	if m.width != 0 || m.height != 0 {
		t.Fatalf("piped output seeded a size: %dx%d", m.width, m.height)
	}
	if !m.belowMinimumTerminal() {
		t.Fatal("non-terminal output must leave the size unknown/below minimum")
	}
}

func swapProbeTerminalSize(fn func(*os.File) (int, int, bool)) func() {
	prev := probeTerminalSize
	probeTerminalSize = fn
	return func() { probeTerminalSize = prev }
}

func TestSeedTerminalSizeAppliesProbedSize(t *testing.T) {
	restore := swapProbeTerminalSize(func(*os.File) (int, int, bool) { return 120, 40, true })
	t.Cleanup(restore)
	m := NewModel(t.TempDir(), t.TempDir())
	seedTerminalSize(&m, nil)
	if m.width != 120 || m.height != 40 {
		t.Fatalf("probed size not seeded: %dx%d, want 120x40", m.width, m.height)
	}
	if m.belowMinimumTerminal() {
		t.Fatal("seeded supported size still below minimum")
	}
}

func TestSeedTerminalSizeIgnoresFailedProbe(t *testing.T) {
	restore := swapProbeTerminalSize(func(*os.File) (int, int, bool) { return 0, 0, false })
	t.Cleanup(restore)
	m := NewModel(t.TempDir(), t.TempDir())
	seedTerminalSize(&m, nil)
	if !m.belowMinimumTerminal() {
		t.Fatal("failed probe must leave the size unknown/below minimum")
	}
}

func TestMouseInputIsGatedBelowMinimum(t *testing.T) {
	m := terminalContractModel(t)
	m.pane = paneDetail
	m.detailViewport.SetContent(strings.Repeat("scrollable line\n", 40))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m = updated.(Model)

	updated, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	m = updated.(Model)
	if m.detailViewport.YOffset != 0 {
		t.Fatalf("mouse wheel scrolled the detail viewport below minimum: offset %d", m.detailViewport.YOffset)
	}

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	m = updated.(Model)
	if m.detailViewport.YOffset == 0 {
		t.Fatal("mouse wheel did not reach the detail viewport at a supported size")
	}
}

func TestAllUserInputBelowMinimumExceptHardQuitIsGated(t *testing.T) {
	gated := []struct {
		name string
		msg  tea.KeyMsg
	}{
		{"up", tea.KeyMsg{Type: tea.KeyUp}},
		{"down", tea.KeyMsg{Type: tea.KeyDown}},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}},
		{"space", tea.KeyMsg{Type: tea.KeySpace}},
		{"tab", tea.KeyMsg{Type: tea.KeyTab}},
		{"sync s", keyPress('s')},
		{"update u", keyPress('u')},
		{"reload r", keyPress('r')},
		{"search /", keyPress('/')},
		{"fold z", keyPress('z')},
		{"harness H", keyPress('H')},
		{"edit E", keyPress('E')},
		{"help ?", keyPress('?')},
		{"errors e", keyPress('e')},
	}
	for _, tc := range gated {
		t.Run(tc.name, func(t *testing.T) {
			m := terminalContractModel(t)
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
			m = updated.(Model)
			beforePane, beforeCursor, beforeSyncing := m.pane, m.cursor, m.syncing
			updated, cmd := m.Update(tc.msg)
			m = updated.(Model)
			if cmd != nil {
				t.Fatalf("%s produced a command below minimum", tc.name)
			}
			if m.pane != beforePane || m.cursor != beforeCursor || m.syncing != beforeSyncing {
				t.Fatalf("%s changed gated state below minimum: pane %v→%v cursor %d→%d syncing %t→%t",
					tc.name, beforePane, m.pane, beforeCursor, m.cursor, beforeSyncing, m.syncing)
			}
		})
	}
}
