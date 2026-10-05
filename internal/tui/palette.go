package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// paletteActionID is the stable identity of one dispatchable list action.
// Both physical key bindings and the command palette dispatch through these
// identities so the palette can never bypass the guards the keyboard path
// applies; availability is computed from the model, never from
// key.Binding.Enabled().
type paletteActionID string

const (
	actionSearch      paletteActionID = "search"
	actionClearSearch paletteActionID = "clear-search"
	actionDetail      paletteActionID = "detail"
	actionToggle      paletteActionID = "toggle"
	actionReload      paletteActionID = "reload"
	actionSync        paletteActionID = "sync"
	actionUpdate      paletteActionID = "update"
	actionHarness     paletteActionID = "harness"
	actionEditConfig  paletteActionID = "edit-config"
	actionHelp        paletteActionID = "help"
	actionDiagnostics paletteActionID = "diagnostics"
	actionFoldToggle  paletteActionID = "fold-toggle"
	actionFoldAll     paletteActionID = "fold-all"
	actionExpandAll   paletteActionID = "expand-all"
	actionQuit        paletteActionID = "quit"
)

// paletteAction pairs a stable identity with the label the fuzzy matcher
// scores and the keyboard shortcut shown beside it. The identity is the
// deterministic tie break for equal scores.
type paletteAction struct {
	id       paletteActionID
	label    string
	shortcut string
}

// paletteActions is the complete palette-visible action set. It covers the
// list-pane actions including the z-prefix fold identities (dispatched
// directly, never as synthesized key sequences) and excludes every
// modal-only scope or confirmation key.
func paletteActions() []paletteAction {
	return []paletteAction{
		{actionSearch, "Search resources", "/"},
		{actionClearSearch, "Clear search", "esc"},
		{actionDetail, "View details / toggle fold", "enter"},
		{actionToggle, "Toggle resource / fold", "space"},
		{actionReload, "Reload from disk", "r"},
		{actionSync, "Sync pending changes", "s"},
		{actionUpdate, "Update caches", "u"},
		{actionHarness, "Harness defaults", "H"},
		{actionEditConfig, "Edit config", "E"},
		{actionHelp, "Help", "?"},
		{actionDiagnostics, "View errors and diagnostics", "e"},
		{actionFoldToggle, "Toggle current fold", "za"},
		{actionFoldAll, "Collapse all folds", "zM"},
		{actionExpandAll, "Expand all folds", "zR"},
		{actionQuit, "Quit", "q"},
	}
}

// paletteMatch is one eligible action plus its fuzzy score for the current
// query (score 0 when the query is empty).
type paletteMatch struct {
	action paletteAction
	score  int
}

// paletteMatchLess orders matches by descending score; equal scores break
// ties by the stable action identity.
func paletteMatchLess(a, b paletteMatch) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	return a.action.id < b.action.id
}

// paletteState is the command palette's transient overlay state: a focused
// text input that owns typing and paste while open, plus the eligible,
// fuzzy-filtered action list and its cursor.
type paletteState struct {
	open    bool
	input   textinput.Model
	matches []paletteMatch
	cursor  int
}

// actionEligible reports whether the action may be dispatched from the
// CURRENT model state. It is evaluated both when the palette filters its
// list and again on submission, so state that changed while the palette was
// open can never ride stale eligibility through the guards.
func (m Model) actionEligible(id paletteActionID) bool {
	switch id {
	case actionSync, actionUpdate:
		return !m.loading && !m.syncing && !m.configInvalid
	case actionHarness:
		return !m.configInvalid
	case actionDiagnostics:
		return len(m.errs) > 0 || len(m.warnings) > 0 || len(m.discoveryErrs) > 0
	case actionClearSearch:
		return m.filter != ""
	case actionToggle:
		return m.paletteToggleable()
	case actionDetail, actionFoldToggle, actionFoldAll, actionExpandAll:
		return len(m.flatItems) > 0
	}
	return true
}

// paletteToggleable reports whether the shared guarded toggle dispatch would
// reach the toggle modal for the current selection, evaluated through
// rowToggleEligibility — the same policy applyRowSpaceEligibility enforces.
// Section headers and denied rows stay detail-inspectable but are not
// toggle-mutable; fold state keeps its own action identities.
func (m Model) paletteToggleable() bool {
	r, ok := m.selectedRow()
	if !ok {
		return false
	}
	return m.rowToggleEligibility(r) == toggleEligible
}

// openPalette initializes the palette overlay. Callers guarantee the list
// context: paneList, no active search input, no modal/overlay, no pending
// fold prefix, and a terminal at or above the minimum size.
func (m *Model) openPalette() tea.Cmd {
	pi := textinput.New()
	pi.Prompt = "› "
	pi.Placeholder = "type to filter actions"
	pi.SetWidth(m.paletteInnerWidth() - 2)
	styles := textinput.DefaultStyles(m.isDark)
	styles.Cursor.Color = m.theme.accent
	pi.SetStyles(styles)
	cmd := pi.Focus()
	m.palette.input = pi
	m.palette.open = true
	m.palette.cursor = 0
	m.recomputePaletteMatches()
	return cmd
}

// closePalette dismisses the overlay and blurs its input without touching
// the search filter or the pending fold state.
func (m *Model) closePalette() {
	m.palette.open = false
	m.palette.input.Blur()
}

// recomputePaletteMatches filters the eligible actions through the same
// fuzzy matcher the resource search uses, sorted by score then action
// identity.
func (m *Model) recomputePaletteMatches() {
	query := m.palette.input.Value()
	matches := m.palette.matches[:0]
	for _, action := range paletteActions() {
		if !m.actionEligible(action.id) {
			continue
		}
		if query == "" {
			matches = append(matches, paletteMatch{action: action})
			continue
		}
		match, ok := fuzzyMatch(query, action.label+" "+action.shortcut)
		if !ok {
			continue
		}
		matches = append(matches, paletteMatch{action: action, score: match.Score})
	}
	if query != "" {
		sort.SliceStable(matches, func(i, j int) bool { return paletteMatchLess(matches[i], matches[j]) })
	}
	m.palette.matches = matches
	m.palette.cursor = min(max(0, m.palette.cursor), max(0, len(matches)-1))
}

func (m Model) selectedPaletteAction() (paletteAction, bool) {
	if m.palette.cursor < 0 || m.palette.cursor >= len(m.palette.matches) {
		return paletteAction{}, false
	}
	return m.palette.matches[m.palette.cursor].action, true
}

// handlePaletteKey owns every key while the palette is open: esc closes,
// enter re-evaluates eligibility against the current model and dispatches,
// arrows move the cursor, and everything else is query text for the input.
// Ctrl+C is handled before pane dispatch as a hard quit.
func (m Model) handlePaletteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Clear):
		m.closePalette()
		return m, nil
	case key.Matches(msg, m.keys.SearchConfirm):
		action, ok := m.selectedPaletteAction()
		if !ok {
			return m, nil
		}
		if !m.actionEligible(action.id) {
			// State changed while the palette was open; refresh the list
			// instead of dispatching a stale selection.
			m.recomputePaletteMatches()
			return m, nil
		}
		m.closePalette()
		return m.dispatchAction(action.id)
	case msg.String() == "up":
		if m.palette.cursor > 0 {
			m.palette.cursor--
		}
		return m, nil
	case msg.String() == "down":
		if m.palette.cursor < len(m.palette.matches)-1 {
			m.palette.cursor++
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.palette.input, cmd = m.palette.input.Update(msg)
	m.recomputePaletteMatches()
	return m, cmd
}

// handlePalettePaste routes bracketed paste to the palette input; the input
// owns pasted text exactly like typed runes and never dispatches actions.
func (m Model) handlePalettePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.palette.input, cmd = m.palette.input.Update(msg)
	m.recomputePaletteMatches()
	return m, cmd
}

// paletteVisibleRows bounds how many matched actions render at once.
const paletteVisibleRows = 8

// paletteTop is the row the palette layer hangs from: below the header and
// the search row when a filter is applied.
func (m Model) paletteTop() int {
	if m.searchRowVisible() {
		return 2
	}
	return 1
}

// paletteVisibleLimit bounds the visible action window to the terminal: the
// border, title, input and hint rows surround the actions, and the footer
// must stay uncovered. Actions outside the window stay reachable by
// scrolling.
func (m Model) paletteVisibleLimit() int {
	return min(paletteVisibleRows, max(1, m.height-m.paletteTop()-6))
}

// renderPalette builds the overlay layer: a titled box holding the query
// input, the eligible actions around the cursor with their shortcuts in a
// right-aligned column, and the palette's own key hints. It is bounded in
// both dimensions so it can never clip or cover the footer controls.
// Action labels are static; any user-typed query travels through the same
// textinput the search field uses.
// paletteInnerWidth is the palette's content width inside its border and
// padding.
func (m Model) paletteInnerWidth() int {
	return min(56, max(24, m.width-8)) - 4
}

func renderPalette(m Model) string {
	t := m.currentTheme()
	inner := m.paletteInnerWidth()
	row := func(left, right string, style lipgloss.Style) string {
		right = dimTextStyle(t).Render(right)
		left = ansi.Truncate(left, max(0, inner-ansi.StringWidth(right)-1), "…")
		gap := max(1, inner-ansi.StringWidth(left)-ansi.StringWidth(right))
		return style.Render(left+strings.Repeat(" ", gap)) + right
	}
	position := "0/0"
	if n := len(m.palette.matches); n > 0 {
		position = fmt.Sprintf("%d/%d", m.palette.cursor+1, n)
	}
	lines := []string{row("Command palette", position, titleStyle(t)), m.palette.input.View()}
	visible := m.paletteVisibleLimit()
	start := 0
	if m.palette.cursor >= visible {
		start = m.palette.cursor - visible + 1
	}
	end := min(len(m.palette.matches), start+visible)
	for i := start; i < end; i++ {
		action := m.palette.matches[i].action
		if i == m.palette.cursor {
			lines = append(lines, row("▸ "+action.label, action.shortcut, accentStyle(t).Bold(true)))
		} else {
			lines = append(lines, row("  "+action.label, action.shortcut, textStyle(t)))
		}
	}
	if len(m.palette.matches) == 0 {
		lines = append(lines, dimTextStyle(t).Render("  no matching actions"))
	}
	lines = append(lines, dimTextStyle(t).Render(ansi.Truncate("↑↓ move · enter run · esc close", inner, "…")))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.accent).
		Padding(0, 1).
		Width(inner + 4).
		Render(strings.Join(lines, "\n"))
}

// compositePalette overlays the palette below the header (and search row),
// horizontally centered, without hiding the list or the footer controls.
func compositePalette(base, layer string, width, height, top int) string {
	col := max(0, (width-lipgloss.Width(layer))/2)
	canvas := lipgloss.NewCanvas(max(1, width), max(1, height))
	return canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(layer).X(col).Y(top).Z(1),
	)).Render()
}
