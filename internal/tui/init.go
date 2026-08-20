package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
)

// initModalState holds the first-run harness-selection UI. It mirrors the
// harness modal but is always scoped to the user-personal layer and
// pre-selects any harness installation roots detected on the machine.
type initModalState struct {
	cursor   int
	selected map[string]bool
	detected bool
}

// newInitModalState detects installed harnesses under homeDir and returns a
// modal state with those harnesses pre-selected.
func newInitModalState(homeDir string) (initModalState, error) {
	detected, err := harness.DetectInstalled(homeDir)
	if err != nil {
		return initModalState{}, err
	}
	return initModalState{selected: harnessSelection(detected), detected: len(detected) > 0}, nil
}

func renderInitModal(m Model) string {
	modal := m.initModal

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"})
	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#A6A000", Dark: "#A6E22E"}).
		Bold(true)
	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#5C8A1E", Dark: "#A6E22E"})
	cursorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"})

	compact := compactTerminalLayout(m)
	var b strings.Builder
	if compact {
		b.WriteString(titleStyle.Render("Initialize target harnesses") + "\n")
		if modal.detected {
			b.WriteString("Detected roots are pre-selected.\n")
		} else {
			b.WriteString("No installation roots detected.\n")
		}
	} else {
		b.WriteString(titleStyle.Render("Initialize target harnesses") + "\n\n")
		b.WriteString("Select the harnesses agent-marketplace should materialize into.\n")
		if modal.detected {
			b.WriteString("Detected installation roots are pre-selected.\n\n")
		} else {
			b.WriteString("No harness installation roots were detected.\n\n")
		}
	}

	for i, opt := range knownHarnessOptions {
		cursor := "  "
		if i == modal.cursor {
			cursor = cursorStyle.Render("> ")
		}
		box := "[ ]"
		if modal.selected[opt.ID] {
			box = activeStyle.Render("[x]")
		}
		fmt.Fprintf(&b, "%s%s %-14s %s\n", cursor, box, opt.Label, styleDim.Render(opt.ID))
	}

	unknown := unknownSelectedHarnesses(modal.selected)
	if len(unknown) > 0 && !compact {
		b.WriteString("\n")
		fmt.Fprintf(&b, "  Other selected: %s\n", sanitizeTerminalData(strings.Join(unknown, ", "), terminalSingleLine))
	}

	if !compact {
		b.WriteString("\n  ")
	}
	b.WriteString(keyStyle.Render("[space]") + " toggle  ")
	b.WriteString(keyStyle.Render("[enter]") + " save  ")
	b.WriteString(keyStyle.Render("[esc]") + " cancel")

	return compactModalStyle(styleHarnessModal, m).Render(b.String())
}

func (m Model) handleInitModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.HarnessCancel):
		m.pane = paneList
		return m, nil
	case key.Matches(msg, m.keys.Up):
		if m.initModal.cursor > 0 {
			m.initModal.cursor--
		}
		return m, nil
	case key.Matches(msg, m.keys.Down):
		if m.initModal.cursor < len(knownHarnessOptions)-1 {
			m.initModal.cursor++
		}
		return m, nil
	case key.Matches(msg, m.keys.Space):
		if m.initModal.cursor < 0 || m.initModal.cursor >= len(knownHarnessOptions) {
			return m, nil
		}
		opt := knownHarnessOptions[m.initModal.cursor]
		if m.initModal.selected == nil {
			m.initModal.selected = map[string]bool{}
		}
		m.initModal.selected[opt.ID] = !m.initModal.selected[opt.ID]
		return m, nil
	case key.Matches(msg, m.keys.HarnessSave):
		harnesses := selectedHarnessIDs(m.initModal.selected)
		if len(harnesses) == 0 {
			return m, func() tea.Msg {
				return toastMsg{text: "select at least one harness", duration: 2 * time.Second}
			}
		}
		id, ok := m.beginOperation(operationTargets, nil)
		if !ok {
			return m, operationInProgressCmd()
		}
		m.pane = paneList
		return m, runInitSaveCmd(harnesses, id)
	}
	return m, nil
}

func runInitSaveCmd(harnesses []string, operation operationID) tea.Cmd {
	return func() tea.Msg {
		_, err := config.PersistUserPersonalHarnesses(harnesses)
		return harnessWrittenMsg{operation: operation, scopeLabel: "user", err: err}
	}
}
