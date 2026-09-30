package tui

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/cellbuf"

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

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(m.currentTheme().accent)
	b.WriteString(titleStyle.Render("agent-marketplace — Keyboard Shortcuts"))
	b.WriteString("\n\n")

	// Each section renders as one FullHelpView group stacked vertically so
	// the retained help viewport can scroll to every binding at any size.
	for _, sec := range m.helpSections() {
		b.WriteString(styleHelpSection.Render(sec.title))
		b.WriteString("\n")
		b.WriteString(m.help.FullHelpView([][]key.Binding{sec.bindings}))
		b.WriteString("\n\n")
	}

	b.WriteString(styleDim.Render("Press esc or ? to close"))

	return b.String()
}

func renderErrors(m Model) string {
	if len(m.errs) == 0 && len(m.warnings) == 0 && len(m.discoveryErrs) == 0 {
		return styleGlyphSynced.Render("No errors")
	}

	var b strings.Builder

	// Diagnostics render inside the bounded scrollable viewport: wrap each
	// message to the overlay width so complete text stays readable through
	// scrolling instead of being hard-cut by the terminal fit.
	writeDiagnostic := func(style lipgloss.Style, prefix, text string) {
		line := prefix + sanitizeTerminalData(text, terminalSingleLine)
		if m.width > 1 {
			line = cellbuf.Wrap(line, m.width, "")
		}
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}

	if len(m.errs) > 0 {
		b.WriteString(styleErrorTitle.Render(fmt.Sprintf("Errors (%d)", len(m.errs))))
		b.WriteString("\n\n")
		for _, err := range m.errs {
			writeDiagnostic(lipgloss.NewStyle().Foreground(m.currentTheme().text), "• ", err.Error())
			var r resolver.Remediable
			if errors.As(err, &r) {
				writeDiagnostic(lipgloss.NewStyle().Foreground(m.currentTheme().text), "  → ", r.Remediation())
			}
		}
	}

	if len(m.discoveryErrs) > 0 {
		if len(m.errs) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(styleWarningTitle.Render(fmt.Sprintf("Marketplace discovery (%d)", len(m.discoveryErrs))))
		b.WriteString("\n\n")
		for _, discoveryErr := range m.discoveryErrs {
			writeDiagnostic(styleWarningMsg, "• ", discoveryErr.Error())
		}
	}

	if len(m.warnings) > 0 {
		if len(m.errs) > 0 || len(m.discoveryErrs) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(styleWarningTitle.Render(fmt.Sprintf("Warnings (%d)", len(m.warnings))))
		b.WriteString("\n\n")
		for _, w := range m.warnings {
			writeDiagnostic(styleWarningMsg, "• ", w.Error())
			// Print remediation hint for remediable warnings
			var r resolver.Remediable
			if errors.As(w, &r) {
				writeDiagnostic(styleWarningMsg, "  → ", r.Remediation())
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(styleDim.Render("Press esc to close"))

	return b.String()
}

func renderConfirm(m Model) string {
	m.ensureSyncQuitForm()
	form := ""
	if m.syncQuitForm != nil {
		form = m.syncQuitForm.View() + "\n\n"
	}
	return styleHelpOverlay.Render(
		styleConfirmTitle.Render("Confirm") +
			"\n\n" + sanitizeTerminalData(m.confirmMsg, terminalSingleLine) +
			"\n\n" + form +
			"y = yes · n = no · esc = cancel",
	)
}
