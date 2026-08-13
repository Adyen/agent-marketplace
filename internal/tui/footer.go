package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func renderFooter(m Model) string {
	var leftParts []string

	// Animated spinner + status indicator for in-flight work.
	// When no in-flight flags are set, the spinner is not rendered.
	if m.loading {
		leftParts = append(leftParts, m.spinner.View()+styleFooterPending.Render(" loading resources..."))
	} else if m.syncing {
		leftParts = append(leftParts, m.spinner.View()+styleFooterPending.Render(" syncing..."))
	} else if m.refreshing {
		leftParts = append(leftParts, m.spinner.View()+styleFooterRefreshing.Render(" refreshing cache..."))
	} else if m.driftReport.InSync() {
		leftParts = append(leftParts, styleDriftOK.Render("~ in sync"))
	} else {
		leftParts = append(leftParts, styleDriftPending.Render(fmt.Sprintf("~ %d out of sync", m.driftReport.PendingCount())))
	}

	// Error count
	if len(m.errs) > 0 {
		leftParts = append(leftParts, styleFooterError.Render(fmt.Sprintf("! %d errors", len(m.errs))))
	}
	if len(m.warnings) > 0 {
		leftParts = append(leftParts, styleFooterWarning.Render(fmt.Sprintf("⚠ %d warnings", len(m.warnings))))
	}

	// Shortcuts: driven from the centralized keyMap via help.Model.
	leftParts = append(leftParts, m.help.ShortHelpView(m.keys.ShortHelp()))

	left := strings.Join(leftParts, "  ")

	// Toast notification is rendered on the right side of the footer,
	// positioned in the bottom-right corner as a temporary notification.
	if m.toast != "" && !m.toastExp.IsZero() {
		toast := styleToast.Render(m.toast)
		// Keep the footer within the terminal width, even when the left
		// content and toast cannot both fit.
		if m.width <= 0 {
			return ""
		}
		toastWidth := lipgloss.Width(toast)
		if toastWidth >= m.width {
			return ansi.Truncate(toast, m.width, "")
		}
		leftWidth := m.width - toastWidth - 1
		if leftWidth < 0 {
			return ansi.Truncate(toast, m.width, "")
		}
		left = ansi.Truncate(left, leftWidth, "")
		return left + " " + toast
	}

	return left
}
