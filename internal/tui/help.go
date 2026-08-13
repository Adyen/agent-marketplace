package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
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

	return b.String()
}

func renderErrors(m Model) string {
	if len(m.errs) == 0 && len(m.warnings) == 0 {
		return styleGlyphSynced.Render("No errors")
	}

	var b strings.Builder

	if len(m.errs) > 0 {
		b.WriteString(styleErrorTitle.Render(fmt.Sprintf("Errors (%d)", len(m.errs))))
		b.WriteString("\n\n")
		for _, err := range m.errs {
			b.WriteString(styleErrorMsg.Render("• " + err.Error()))
			b.WriteString("\n")
			var r resolver.Remediable
			if errors.As(err, &r) {
				b.WriteString(styleErrorMsg.Render("  → " + r.Remediation()))
				b.WriteString("\n")
			}
		}
	}

	if len(m.warnings) > 0 {
		if len(m.errs) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(styleWarningTitle.Render(fmt.Sprintf("Warnings (%d)", len(m.warnings))))
		b.WriteString("\n\n")
		for _, w := range m.warnings {
			b.WriteString(styleWarningMsg.Render("• " + w.Error()))
			b.WriteString("\n")
			// Print remediation hint for remediable warnings
			var r resolver.Remediable
			if errors.As(w, &r) {
				b.WriteString(styleWarningMsg.Render("  → " + r.Remediation()))
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(styleDim.Render("Press esc to close"))

	return b.String()
}

func renderConfirm(m Model) string {
	return styleHelpOverlay.Render(
		styleConfirmTitle.Render("Confirm") +
			"\n\n" + m.confirmMsg +
			"\n\ny = yes · n = no · esc = cancel",
	)
}
