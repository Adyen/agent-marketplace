package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/cellbuf"
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

	b.WriteString(titleStyle(m.currentTheme()).Render("agent-marketplace — Keyboard Shortcuts"))
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

	writeRecovery := func(style lipgloss.Style, err error, category diagnosticCategory) {
		if hint := diagnosticRemediation(err, category); hint != "" {
			writeDiagnostic(style, "  → ", hint)
		}
	}
	categories := m.diagnosticCategories()
	categoryFor := func(severity diagnosticSeverity, err error) diagnosticCategory {
		key := diagnosticCategoryKey{severity: severity, stableKey: stableDiagnosticKey(err)}
		queue := categories[key]
		if len(queue) == 0 {
			return ""
		}
		categories[key] = queue[1:]
		return queue[0]
	}

	if len(m.errs) > 0 {
		b.WriteString(styleErrorTitle.Render(fmt.Sprintf("Errors (%d)", len(m.errs))))
		b.WriteString("\n\n")
		for _, err := range m.errs {
			writeDiagnostic(textStyle(m.currentTheme()), "• ", err.Error())
			writeRecovery(textStyle(m.currentTheme()), err, categoryFor(diagnosticError, err))
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
			writeRecovery(styleWarningMsg, discoveryErr, diagnosticCatalog)
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
			writeRecovery(styleWarningMsg, w, categoryFor(diagnosticWarning, w))
		}
	}

	b.WriteString("\n")
	b.WriteString(styleDim.Render("Press esc to close"))

	return b.String()
}

func renderConfirm(m Model) string {
	t := m.currentTheme()
	m.ensureSyncQuitForm()
	body := []string{sanitizeTerminalData(m.confirmMsg, terminalSingleLine)}
	if m.syncQuitForm != nil {
		body = append(body, "", m.syncQuitForm.View())
	}
	return modalFrame(m, t.warn, warnBoldStyle(t).Render("Confirm"), body, []footerHint{
		{"y", "sync+quit"}, {"n", "quit"}, {"esc", "cancel"},
	})
}
