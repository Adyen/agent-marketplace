package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
)

// initModalState holds the first-run harness-selection UI. It mirrors the
// harness modal but is always scoped to the user-personal layer and
// pre-selects any harness installation roots detected on the machine.
type initModalState struct {
	cursor     int
	selected   map[string]bool
	detected   bool
	form       *huh.Form
	multi      *huh.MultiSelect[string]
	multiValue *[]string
	optionIDs  []string
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
	m.ensureInitForm()
	modal := m.initModal

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.currentTheme().accent)
	keyStyle := lipgloss.NewStyle().
		Foreground(m.currentTheme().mark).
		Bold(true)

	compact := compactTerminalLayout(m)
	modalStyle := compactModalStyle(modalBoxStyle(m.currentTheme(), lipgloss.RoundedBorder(), m.currentTheme().accent), m)
	var b strings.Builder
	if compact {
		contentRows := max(1, m.height-modalStyle.GetVerticalFrameSize())
		visibleOptions := max(1, contentRows-3)
		if modal.multi != nil {
			modal.multi.Height(visibleOptions)
		}
		b.WriteString(titleStyle.Render("Initialize target harnesses") + "\n")
		if modal.detected {
			b.WriteString("Detected targets are pre-selected.\n")
		} else {
			b.WriteString("No installed targets detected.\n")
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

	if modal.form != nil {
		b.WriteString(modal.form.View())
		b.WriteString("\n")
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

	return modalStyle.Render(b.String())
}

func (m Model) handleInitModalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.ensureInitForm()
	switch {
	case key.Matches(msg, m.keys.HarnessCancel):
		m.pane = paneList
		return m, nil
	case key.Matches(msg, m.keys.HarnessSave):
		m.delegateInitForm(msg)
		if m.initModal.form == nil || m.initModal.form.State != huh.StateCompleted {
			return m, nil
		}
		harnesses := selectedHarnessIDs(m.initModal.selected)
		if len(harnesses) == 0 {
			m.initModal.form, m.initModal.multi, m.initModal.multiValue = newHarnessSelectForm(m.initModal.selected)
			return m, func() tea.Msg {
				return toastMsg{text: "select at least one harness", duration: 2 * time.Second}
			}
		}
		operation, ok := m.beginOperation(operationTargets)
		if !ok {
			return m, operationBlockedCmd()
		}
		m.pane = paneList
		return m, correlateOperation(runInitSaveCmd(harnesses), operation, false)
	default:
		m.delegateInitForm(msg)
		return m, nil
	}
}

func (m *Model) delegateInitForm(msg tea.Msg) {
	var selected map[string]bool
	m.initModal.form, selected, m.initModal.cursor = updateHarnessMulti(
		m.initModal.form, m.initModal.multi, m.initModal.multiValue,
		m.initModal.optionIDs, m.initModal.cursor, msg)
	if selected != nil {
		m.initModal.selected = selected
	}
}

func runInitSaveCmd(harnesses []string) tea.Cmd {
	return func() tea.Msg {
		_, err := config.InitializeUserPersonal(harnesses)
		// The TUI treats an already-initialized file as a no-op with a
		// success toast so repeated opens are harmless.
		if errors.Is(err, config.ErrAlreadyInitialized) {
			err = nil
		}
		return harnessWrittenMsg{scopeLabel: "user", err: err}
	}
}
