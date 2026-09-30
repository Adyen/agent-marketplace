package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type harnessScope int

const (
	harnessScopeProject harnessScope = iota
	harnessScopeGlobal
)

type harnessOption struct {
	ID    string
	Label string
}

type harnessModalState struct {
	scope      harnessScope
	cursor     int
	selected   map[string]bool
	form       *huh.Form
	multi      *huh.MultiSelect[string]
	multiValue *[]string
	optionIDs  []string
}

type harnessWrittenMsg struct {
	operation  operationID
	scopeLabel string
	err        error
}

var knownHarnessOptions = func() []harnessOption {
	definitions := targetmeta.Definitions()
	sort.SliceStable(definitions, func(i, j int) bool {
		return definitions[i].UIOrder < definitions[j].UIOrder
	})
	options := make([]harnessOption, 0, len(definitions))
	for _, definition := range definitions {
		if definition.UI {
			options = append(options, harnessOption{ID: string(definition.Target), Label: definition.Label})
		}
	}
	return options
}()

func (m *Model) openHarnessModal() {
	m.pane = paneHarnessModal
	m.harnessModal = harnessModalState{
		scope:    harnessScopeGlobal,
		selected: harnessSelection(m.globalHarnesses),
	}
	m.buildHarnessForm()
}

func harnessSelection(harnesses []string) map[string]bool {
	selected := make(map[string]bool, len(harnesses))
	for _, h := range harnesses {
		h = strings.TrimSpace(h)
		if h != "" {
			selected[h] = true
		}
	}
	return selected
}

func selectedHarnessIDs(selected map[string]bool) []string {
	var harnesses []string
	var unknown []string
	for _, opt := range knownHarnessOptions {
		if selected[opt.ID] {
			harnesses = append(harnesses, opt.ID)
		}
	}
	for id, ok := range selected {
		if ok && !knownHarnessID(id) {
			unknown = append(unknown, id)
		}
	}
	slices.Sort(unknown)
	return append(harnesses, unknown...)
}

func knownHarnessID(id string) bool {
	for _, opt := range knownHarnessOptions {
		if opt.ID == id {
			return true
		}
	}
	return false
}

func (s harnessScope) label() string {
	if s == harnessScopeGlobal {
		return "global"
	}
	return "project"
}

func (m *Model) setHarnessScope(scope harnessScope) {
	m.harnessModal.scope = scope
	if scope == harnessScopeGlobal {
		m.harnessModal.selected = harnessSelection(m.globalHarnesses)
	} else {
		m.harnessModal.selected = harnessSelection(m.projectHarnesses)
	}
	m.buildHarnessForm()
}

func renderHarnessModal(m Model) string {
	m.ensureHarnessForm()
	modal := m.harnessModal
	if modal.selected == nil {
		modal.selected = harnessSelection(m.globalHarnesses)
	}

	title := titleStyle(m.currentTheme()).Render("Target harnesses")
	keyStyle := markKeyStyle(m.currentTheme())
	activeStyle := okBoldStyle(m.currentTheme())

	compact := compactTerminalLayout(m)
	modalStyle := compactModalStyle(modalBoxStyle(m.currentTheme(), lipgloss.RoundedBorder(), m.currentTheme().accent), m)
	var b strings.Builder
	if compact {
		contentRows := max(1, m.height-modalStyle.GetVerticalFrameSize())
		visibleOptions := max(1, contentRows-4)
		if modal.multi != nil {
			modal.multi.Height(visibleOptions)
		}
		b.WriteString(title + "\n")
		fmt.Fprintf(&b, "%s · %s\n", activeStyle.Render(modal.scope.label()), harnessListLabel(effectiveHarnesses(m), ", "))
	} else {
		b.WriteString(title + "\n\n")
		fmt.Fprintf(&b, "  Editing:   %s\n", activeStyle.Render(modal.scope.label()))
		fmt.Fprintf(&b, "  Effective: %s\n", harnessListLabel(effectiveHarnesses(m), ", "))
		fmt.Fprintf(&b, "  Global:    %s\n", harnessListLabel(m.globalHarnesses, ", "))
		fmt.Fprintf(&b, "  Project:   %s\n\n", harnessListLabel(m.projectHarnesses, ", "))
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
	b.WriteString(keyStyle.Render("[g]") + " global  ")
	b.WriteString(keyStyle.Render("[p]") + " project\n")
	if !compact {
		b.WriteString("  ")
	}
	b.WriteString(keyStyle.Render("[space]") + " toggle  ")
	b.WriteString(keyStyle.Render("[enter]") + " save  ")
	b.WriteString(keyStyle.Render("[esc]") + " cancel")

	return modalStyle.Render(b.String())
}

func unknownSelectedHarnesses(selected map[string]bool) []string {
	var unknown []string
	for id, ok := range selected {
		if ok && !knownHarnessID(id) {
			unknown = append(unknown, id)
		}
	}
	slices.Sort(unknown)
	return unknown
}

func effectiveHarnesses(m Model) []string {
	if len(m.projectHarnesses) > 0 {
		return m.projectHarnesses
	}
	return m.globalHarnesses
}

func harnessListLabel(harnesses []string, separator string) string {
	if len(harnesses) == 0 {
		return "none"
	}
	labels := make([]string, len(harnesses))
	for i, id := range harnesses {
		definition, ok := targetmeta.Lookup(id)
		if !ok {
			labels[i] = id
			continue
		}
		labels[i] = definition.Label
	}
	return sanitizeTerminalData(strings.Join(labels, separator), terminalSingleLine)
}

func (m Model) handleHarnessModalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.ensureHarnessForm()
	switch {
	case key.Matches(msg, m.keys.HarnessCancel):
		m.pane = paneList
		return m, nil
	case key.Matches(msg, m.keys.HarnessProject):
		m.setHarnessScope(harnessScopeProject)
		return m, nil
	case key.Matches(msg, m.keys.HarnessGlobal):
		m.setHarnessScope(harnessScopeGlobal)
		return m, nil
	case key.Matches(msg, m.keys.HarnessSave):
		m.delegateHarnessForm(msg)
		if m.harnessModal.form == nil || m.harnessModal.form.State != huh.StateCompleted {
			return m, nil
		}
		harnesses := selectedHarnessIDs(m.harnessModal.selected)
		if len(harnesses) == 0 {
			// Rebuild so the completed form resets and the user can
			// keep toggling options after the rejected save.
			m.buildHarnessForm()
			return m, func() tea.Msg {
				return toastMsg{text: "select at least one harness", duration: 2 * time.Second}
			}
		}
		operation, ok := m.beginOperation(operationTargets)
		if !ok {
			return m, operationBlockedCmd()
		}
		scope := m.harnessModal.scope
		m.pane = paneList
		return m, correlateOperation(runHarnessSaveCmd(m.repoRoot, scope, harnesses), operation, false)
	default:
		m.delegateHarnessForm(msg)
		return m, nil
	}
}

// delegateHarnessForm forwards a message to the modal's Huh multiselect
// and syncs the resulting selection and cursor back to the modal state.
func (m *Model) delegateHarnessForm(msg tea.Msg) {
	var selected map[string]bool
	m.harnessModal.form, selected, m.harnessModal.cursor = updateHarnessMulti(
		m.harnessModal.form, m.harnessModal.multi, m.harnessModal.multiValue,
		m.harnessModal.optionIDs, m.harnessModal.cursor, msg)
	if selected != nil {
		m.harnessModal.selected = selected
	}
}

func runHarnessSaveCmd(repoRoot string, scope harnessScope, harnesses []string) tea.Cmd {
	return func() tea.Msg {
		var path string
		var err error
		if scope == harnessScopeGlobal {
			path, err = config.GlobalConfigPath()
		} else {
			path = config.ProjectConfigPath(repoRoot)
		}
		if err == nil {
			err = config.SetHarnessesInConfig(path, harnesses)
		}
		return harnessWrittenMsg{scopeLabel: scope.label(), err: err}
	}
}
