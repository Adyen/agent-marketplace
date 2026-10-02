package tui

import (
	"errors"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

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
	t := m.currentTheme()
	title := titleStyle(t).Render("Initialize target harnesses")
	hints := []footerHint{{"space", "toggle"}, {"enter", "save"}, {"esc", "cancel"}}

	var body []string
	if compactTerminalLayout(m) {
		if modal.multi != nil {
			modal.multi.Height(max(1, m.height-modalFrameRows-2))
		}
		if modal.detected {
			body = append(body, "Detected targets are pre-selected.")
		} else {
			body = append(body, "No installed targets detected.")
		}
		if modal.form != nil {
			body = append(body, modal.form.View())
		}
		return modalFrame(m, t.accent, title, body, hints)
	}

	body = append(body, "Select the harnesses agent-marketplace should materialize into.")
	if modal.detected {
		body = append(body, "Detected installation roots are pre-selected.", "")
	} else {
		body = append(body, "No harness installation roots were detected.", "")
	}
	if modal.form != nil {
		body = append(body, modal.form.View())
	}
	if unknown := unknownSelectedHarnesses(modal.selected); len(unknown) > 0 {
		body = append(body, "", "Other selected: "+sanitizeTerminalData(strings.Join(unknown, ", "), terminalSingleLine))
	}
	return modalFrame(m, t.accent, title, body, hints)
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
			m.initModal.form, m.initModal.multi, m.initModal.multiValue = newHarnessSelectForm(m.initModal.selected, m.currentTheme(), modalInnerWidth(m))
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
