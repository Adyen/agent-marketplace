package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	scope    harnessScope
	cursor   int
	selected map[string]bool
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

var styleHarnessModal = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"}).
	Background(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1A1A1A"}).
	Foreground(lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#F8F8F2"}).
	Padding(1, 2)

func (m *Model) openHarnessModal() {
	m.pane = paneHarnessModal
	m.harnessModal = harnessModalState{
		scope:    harnessScopeGlobal,
		selected: harnessSelection(m.globalHarnesses),
	}
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
		return
	}
	m.harnessModal.selected = harnessSelection(m.projectHarnesses)
}

func renderHarnessModal(m Model) string {
	modal := m.harnessModal
	if modal.selected == nil {
		modal.selected = harnessSelection(m.globalHarnesses)
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"}).
		Render("Target harnesses")
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#A6A000", Dark: "#A6E22E"}).Bold(true)
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#5C8A1E", Dark: "#A6E22E"})
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"})

	compact := compactTerminalLayout(m)
	modalStyle := compactModalStyle(styleHarnessModal, m)
	optionStart, optionEnd := 0, len(knownHarnessOptions)
	var b strings.Builder
	if compact {
		contentRows := max(1, m.height-modalStyle.GetVerticalFrameSize())
		visibleOptions := max(1, contentRows-4)
		optionStart, optionEnd = harnessOptionWindow(len(knownHarnessOptions), modal.cursor, visibleOptions)
		b.WriteString(title + "\n")
		fmt.Fprintf(&b, "%s · %s\n", activeStyle.Render(modal.scope.label()), harnessListLabel(effectiveHarnesses(m), ", "))
	} else {
		b.WriteString(title + "\n\n")
		fmt.Fprintf(&b, "  Editing:   %s\n", activeStyle.Render(modal.scope.label()))
		fmt.Fprintf(&b, "  Effective: %s\n", harnessListLabel(effectiveHarnesses(m), ", "))
		fmt.Fprintf(&b, "  Global:    %s\n", harnessListLabel(m.globalHarnesses, ", "))
		fmt.Fprintf(&b, "  Project:   %s\n\n", harnessListLabel(m.projectHarnesses, ", "))
	}

	for i := optionStart; i < optionEnd; i++ {
		opt := knownHarnessOptions[i]
		cursor := "  "
		if i == modal.cursor {
			cursor = cursorStyle.Render("> ")
		}
		box := "[ ]"
		if modal.selected[opt.ID] {
			box = "[x]"
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

func harnessOptionWindow(total, cursor, limit int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	limit = min(max(1, limit), total)
	cursor = min(max(0, cursor), total-1)
	start := 0
	if cursor >= limit {
		start = cursor - limit + 1
	}
	if start+limit > total {
		start = total - limit
	}
	return start, start + limit
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

func (m Model) handleHarnessModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case key.Matches(msg, m.keys.Up):
		if m.harnessModal.cursor > 0 {
			m.harnessModal.cursor--
		}
		return m, nil
	case key.Matches(msg, m.keys.Down):
		if m.harnessModal.cursor < len(knownHarnessOptions)-1 {
			m.harnessModal.cursor++
		}
		return m, nil
	case key.Matches(msg, m.keys.Space):
		opt := knownHarnessOptions[m.harnessModal.cursor]
		if m.harnessModal.selected == nil {
			m.harnessModal.selected = map[string]bool{}
		}
		m.harnessModal.selected[opt.ID] = !m.harnessModal.selected[opt.ID]
		return m, nil
	case key.Matches(msg, m.keys.HarnessSave):
		harnesses := selectedHarnessIDs(m.harnessModal.selected)
		if len(harnesses) == 0 {
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
	}
	return m, nil
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
