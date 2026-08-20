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
	} else {
		switch m.driftAssessment {
		case driftReady:
			if m.driftReport.InSync() {
				leftParts = append(leftParts, styleDriftOK.Render("~ in sync"))
			} else {
				leftParts = append(leftParts, styleDriftPending.Render(fmt.Sprintf("~ %d out of sync", m.driftReport.PendingCount())))
			}
		case driftFailed:
			leftParts = append(leftParts, styleFooterError.Render("~ drift check failed"))
		default:
			leftParts = append(leftParts, styleFooterPending.Render("~ drift unavailable"))
		}
	}

	// Error count
	if count := m.diagnostics.count(diagnosticError); count > 0 {
		leftParts = append(leftParts, styleFooterError.Render(fmt.Sprintf("! %d errors", count)))
	}
	if count := m.diagnostics.count(diagnosticWarning); count > 0 {
		leftParts = append(leftParts, styleFooterWarning.Render(fmt.Sprintf("⚠ %d warnings", count)))
	}

	status := strings.Join(leftParts, "  ")
	if compactTerminalLayout(m) {
		// In the compact layout only a few actions fit: while failures or
		// unresolved drift exist, the recovery actions (inspect errors,
		// reload from disk) displace the discovery actions (help, sync) so
		// the path out of a broken state is always visible.
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

	// Shortcuts: driven from the centralized keyMap via help.Model.
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

// failuresOrDrift reports whether unacknowledged error diagnostics or
// unresolved/failed drift exist — the states in which the compact footer
// prioritizes recovery actions over discovery actions.
func (m Model) failuresOrDrift() bool {
	if m.diagnostics.count(diagnosticError) > 0 {
		return true
	}
	switch m.driftAssessment {
	case driftFailed:
		return true
	case driftReady:
		return !m.driftReport.InSync()
	}
	return false
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
