package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// helpSection groups a titled set of key bindings for the help overlay.
type helpSection struct {
	title    string
	bindings []key.Binding
}

// helpSections returns the help overlay sections driven from the keyMap,
// providing a single source of truth for the full help display.
func (m Model) helpSections() []helpSection {
	return []helpSection{
		{title: "Navigation", bindings: m.keys.navigationBindings()},
		{title: "Actions", bindings: m.keys.actionBindings()},
		{title: "Toggle Modal", bindings: m.keys.toggleModalBindings()},
		{title: "Harness Modal", bindings: m.keys.harnessModalBindings()},
		{title: "Edit Config Modal", bindings: m.keys.editConfigModalBindings()},
		{title: "Other", bindings: m.keys.otherBindings()},
	}
}

func renderHelp(m Model) string {
	var b strings.Builder

	b.WriteString(styleHelpOverlay.Render(""))
	b.WriteString("\n")

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"})
	b.WriteString(titleStyle.Render("agent-marketplace — Keyboard Shortcuts"))
	b.WriteString("\n\n")

	// Render each section using the keyMap bindings with help.Model styles.
	for _, sec := range m.helpSections() {
		b.WriteString(styleHelpSection.Render(sec.title))
		b.WriteString("\n")
		for _, kb := range sec.bindings {
			if !kb.Enabled() {
				continue
			}
			b.WriteString("  ")
			b.WriteString(m.help.Styles.FullKey.Render(kb.Help().Key))
			b.WriteString(m.help.Styles.FullSeparator.Inline(true).Render("  "))
			b.WriteString(m.help.Styles.FullDesc.Render(kb.Help().Desc))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(styleDim.Render("Press esc or ? to close"))

	return wrapOverlayContent(b.String(), m.width)
}

func renderErrors(m Model) string {
	errs := m.diagnostics.bySeverity(diagnosticError)
	warnings := m.diagnostics.bySeverity(diagnosticWarning)
	if len(errs) == 0 && len(warnings) == 0 {
		return styleGlyphSynced.Render("No errors")
	}

	var b strings.Builder

	if len(errs) > 0 {
		b.WriteString(styleErrorTitle.Render(fmt.Sprintf("Errors (%d)", len(errs))))
		b.WriteString("\n\n")
		for _, diagnostic := range errs {
			b.WriteString(styleErrorMsg.Render("• [" + string(diagnostic.category) + "] " + sanitizeTerminalData(diagnostic.err.Error(), terminalSingleLine)))
			b.WriteString("\n")
			var r resolver.Remediable
			if errors.As(diagnostic.err, &r) {
				b.WriteString(styleErrorMsg.Render("  → " + sanitizeTerminalData(r.Remediation(), terminalSingleLine)))
				b.WriteString("\n")
			}
		}
	}

	if len(warnings) > 0 {
		if len(errs) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(styleWarningTitle.Render(fmt.Sprintf("Warnings (%d)", len(warnings))))
		b.WriteString("\n\n")
		for _, diagnostic := range warnings {
			b.WriteString(styleWarningMsg.Render("• [" + string(diagnostic.category) + "] " + sanitizeTerminalData(diagnostic.err.Error(), terminalSingleLine)))
			b.WriteString("\n")
			var r resolver.Remediable
			if errors.As(diagnostic.err, &r) {
				b.WriteString(styleWarningMsg.Render("  → " + sanitizeTerminalData(r.Remediation(), terminalSingleLine)))
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("\n")
	if len(m.diagnostics.operations) > 0 {
		b.WriteString(styleDim.Render("esc/e close · acknowledgement requires scrolling to the bottom (G)"))
	} else {
		b.WriteString(styleDim.Render("Press esc or e to close"))
	}

	return wrapOverlayContent(b.String(), m.width)
}

// wrapOverlayContent word-wraps overlay content at the frame width so long
// (sanitized) lines stay readable instead of being hard-clipped. Widths below
// one mean the size is unknown; the content is returned unwrapped.
func wrapOverlayContent(content string, width int) string {
	if width < 1 {
		return content
	}
	return lipgloss.NewStyle().Width(width).Render(content)
}

// helpPaneFooter is the persistent key line under the help overlay: scroll
// keys are always visible, alongside close and hard quit.
func helpPaneFooter() string {
	return "↑/↓ scroll · esc/? close · Ctrl+C quit"
}

// errorsPaneFooter is the persistent key line under the diagnostics overlay.
// It reports live acknowledgement state: esc acknowledges retained operation
// diagnostics only once the viewport has reached the bottom (i.e. every
// operation diagnostic has been visible); before that it only closes.
func errorsPaneFooter(m Model) string {
	if len(m.diagnostics.operations) == 0 {
		return "↑/↓ scroll · esc close · Ctrl+C quit"
	}
	if m.errorsViewport.AtBottom() {
		return "↑/↓ scroll · esc acknowledge · Ctrl+C quit"
	}
	return "↑/↓ scroll to bottom to acknowledge · esc close"
}

// errorsAcknowledgeable reports whether closing the diagnostics pane may
// acknowledge retained operation diagnostics: acknowledgement requires that
// all operation diagnostics have been visible, i.e. the viewport reached the
// bottom. Snapshot diagnostics are never acknowledged by closing; they are
// replaced by the next accepted snapshot.
func (m Model) errorsAcknowledgeable() bool {
	if len(m.diagnostics.operations) == 0 {
		return true
	}
	return m.errorsViewport.AtBottom()
}

// scrollHelpOverlay routes scroll keys (j/k, ↑/↓, pgup/pgdn, half-page, g/G,
// mouse wheel) into the help viewport. Any other key is swallowed while the
// overlay is active.
func (m Model) scrollHelpOverlay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Top):
		m.helpViewport.GotoTop()
		return m, nil
	case key.Matches(msg, m.keys.Bottom):
		m.helpViewport.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.helpViewport, cmd = m.helpViewport.Update(msg)
	return m, cmd
}

// scrollErrorsOverlay routes scroll keys into the diagnostics viewport. Any
// other key is swallowed while the overlay is active.
func (m Model) scrollErrorsOverlay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Top):
		m.errorsViewport.GotoTop()
		return m, nil
	case key.Matches(msg, m.keys.Bottom):
		m.errorsViewport.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.errorsViewport, cmd = m.errorsViewport.Update(msg)
	return m, cmd
}

// syncOverlayViewports keeps the help and diagnostics overlay viewports sized
// to the current frame and — while the corresponding pane is active — filled
// with freshly rendered content. Scroll offsets persist: SetContent retains
// YOffset. helpStickyBottom/errorsStickyBottom carry the pre-message
// sticky-bottom anchors sampled by Update; a pane that was at the bottom
// stays anchored to the bottom across content growth and resizes.
func (m *Model) syncOverlayViewports(helpStickyBottom, errorsStickyBottom bool) {
	if m.width < 1 || m.height < 1 {
		return
	}
	height := max(1, m.height-1)
	m.helpViewport.Width = m.width
	m.helpViewport.Height = height
	m.errorsViewport.Width = m.width
	m.errorsViewport.Height = height
	switch m.pane {
	case paneHelp:
		m.helpViewport.SetContent(renderHelp(*m))
		if helpStickyBottom {
			m.helpViewport.GotoBottom()
		}
	case paneErrors:
		m.errorsViewport.SetContent(renderErrors(*m))
		if errorsStickyBottom {
			m.errorsViewport.GotoBottom()
		}
	}
}

func renderConfirm(m Model) string {
	return styleHelpOverlay.Render(
		styleConfirmTitle.Render("Confirm") +
			"\n\n" + sanitizeTerminalData(m.confirmMsg, terminalSingleLine) +
			"\n\ny = yes · n = no · esc = cancel",
	)
}
