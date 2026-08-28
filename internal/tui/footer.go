package tui

import (
	"fmt"
	"strings"

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
	} else if m.driftAssessment != driftReady {
		leftParts = append(leftParts, styleDriftPending.Render("~ drift unavailable"))
	} else if m.driftReport.InSync() {
		leftParts = append(leftParts, styleDriftOK.Render("~ in sync"))
	} else {
		leftParts = append(leftParts, styleDriftPending.Render(fmt.Sprintf("~ %d out of sync", m.driftReport.PendingCount())))
	}

	if len(m.errs) > 0 {
		leftParts = append(leftParts, styleFooterError.Render(fmt.Sprintf("! %d errors", len(m.errs))))
	}
	if len(m.warnings) > 0 {
		leftParts = append(leftParts, styleFooterWarning.Render(fmt.Sprintf("⚠ %d warnings", len(m.warnings))))
	}

	status := strings.Join(leftParts, "  ")
	if compactTerminalLayout(m) {
		actions := "?:help · s:sync · q:quit"
		if m.failuresOrDrift() {
			actions = "e:errors · r:reload · q:quit"
		}
		if m.filterActive {
			actions = "enter apply · esc cancel · q quit"
		}
		if m.toast != "" && !m.toastExp.IsZero() {
			status = styleToast.Render(sanitizeTerminalData(m.toast, terminalSingleLine))
		}
		return footerWithRight(status, actions, m.width)
	}

	bindings := m.keys.ShortHelp()
	if len(bindings) > 0 {
		bindings = bindings[:len(bindings)-1]
	}
	leftParts = append(leftParts, m.help.ShortHelpView(bindings))
	right := "q quit"
	if m.toast != "" && !m.toastExp.IsZero() {
		right += " · " + styleToast.Render(sanitizeTerminalData(m.toast, terminalSingleLine))
	}
	return footerWithRight(strings.Join(leftParts, "  "), right, m.width)
}

func (m Model) failuresOrDrift() bool {
	if len(m.errs) > 0 {
		return true
	}
	return m.driftAssessment == driftFailed || (m.driftAssessment == driftReady && !m.driftReport.InSync())
}

func footerWithRight(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	rightWidth := ansi.StringWidth(right)
	if rightWidth >= width {
		return ansi.Truncate(right, width, "")
	}
	left = ansi.Truncate(left, width-rightWidth-1, "")
	if left == "" {
		return right
	}
	return left + " " + right
}
