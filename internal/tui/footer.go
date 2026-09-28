package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (m Model) verifyingResources() bool {
	return m.assessmentPending || (m.loading && len(m.provisionalRows) > 0)
}

func renderFooter(m Model) string {
	if len(m.errs) > 0 {
		return renderErrorFooter(m)
	}
	var leftParts []string

	// Animated spinner + status indicator for in-flight work.
	// When no in-flight flags are set, the spinner is not rendered.
	if m.loading {
		status := " loading resources..."
		if m.verifyingResources() {
			status = " verifying resources..."
		}
		leftParts = append(leftParts, m.spinner.View()+styleFooterPending.Render(status))
	} else if m.syncing {
		leftParts = append(leftParts, m.spinner.View()+styleFooterPending.Render(" syncing..."))
	} else if m.refreshing {
		leftParts = append(leftParts, m.spinner.View()+styleFooterRefreshing.Render(" refreshing cache..."))
	} else if m.assessmentPending {
		leftParts = append(leftParts, m.spinner.View()+styleFooterPending.Render(" verifying resources..."))
	} else if m.driftAssessment != driftReady || m.hasUnavailableRows() {
		leftParts = append(leftParts, styleDriftPending.Render("~ drift unavailable"))
	} else if m.driftReport.InSync() {
		leftParts = append(leftParts, styleDriftOK.Render("~ in sync"))
	} else {
		leftParts = append(leftParts, styleDriftPending.Render(fmt.Sprintf("~ %d out of sync", m.driftReport.PendingCount())))
	}

	var staleEndpoints, truncatedEndpoints int
	for _, outcome := range m.discoveryOutcomes {
		if outcome.CacheState == "stale" {
			staleEndpoints++
		}
		if outcome.Truncated {
			truncatedEndpoints++
		}
	}
	if staleEndpoints > 0 {
		leftParts = append(leftParts, styleFooterWarning.Render(fmt.Sprintf("Marketplace: %d stale", staleEndpoints)))
	}
	if truncatedEndpoints > 0 {
		leftParts = append(leftParts, styleFooterPending.Render(fmt.Sprintf("Marketplace: %d limited", truncatedEndpoints)))
	}
	if len(m.discoveryErrs) > 0 {
		leftParts = append(leftParts, styleFooterWarning.Render(fmt.Sprintf("Marketplace: %d failed", len(m.discoveryErrs))))
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
		if m.failuresOrDrift() || len(m.discoveryErrs) > 0 {
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

func renderErrorFooter(m Model) string {
	actions := "e:errors · r:reload · q:quit"
	if m.filterActive {
		actions = "enter apply · esc cancel · q quit"
	}
	budget := m.width - ansi.StringWidth(actions) - 1
	status := fmt.Sprintf("! %d errors", len(m.errs))
	if m.verifyingResources() {
		status = m.spinner.View() + " verifying resources... · " + status
	}
	if budget > ansi.StringWidth(status)+2 {
		latest := ""
		if err := m.errs[len(m.errs)-1]; err != nil {
			latest = sanitizeTerminalData(fmt.Sprint(err), terminalSingleLine)
		}
		if latest != "" {
			status += ": " + latest
		}
	}
	if m.width < 1 {
		return ""
	}
	if budget < 0 {
		budget = 0
	}
	status = ansi.Truncate(status, budget, "…")
	if ansi.StringWidth(actions) >= m.width {
		return ansi.Truncate(actions, m.width, "…")
	}
	return styleFooterError.Render(status) + " " + actions
}

func rowsHaveUnavailable(rows []row) bool {
	for _, r := range rows {
		if r.UnavailableReason != "" {
			return true
		}
	}
	return false
}

func (m Model) hasUnavailableRows() bool {
	return rowsHaveUnavailable(m.rows)
}

func (m Model) failuresOrDrift() bool {
	if len(m.errs) > 0 {
		return true
	}
	return m.driftAssessment == driftFailed || m.hasUnavailableRows() || (m.driftAssessment == driftReady && !m.driftReport.InSync())
}

// fitFooterGroups adapts a separator-joined footer segment (actions joined
// by " · ", status groups joined by "  ") to the available width by keeping
// whole trailing groups that fit rather than cutting a partial word. When
// no single group fits, the last group is truncated with an ellipsis so the
// output stays bounded.
func fitFooterGroups(s, separator string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	groups := strings.Split(s, separator)
	sepWidth := ansi.StringWidth(separator)
	kept := make([]string, 0, len(groups))
	used := 0
	for i := len(groups) - 1; i >= 0; i-- {
		need := ansi.StringWidth(groups[i])
		if len(kept) > 0 {
			need += sepWidth
		}
		if used+need > width {
			continue
		}
		kept = append(kept, groups[i])
		used += need
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	if len(kept) == 0 {
		return ansi.Truncate(groups[len(groups)-1], width, "…")
	}
	return strings.Join(kept, separator)
}

func footerWithRight(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	right = fitFooterGroups(right, " · ", width)
	rightWidth := ansi.StringWidth(right)
	if rightWidth >= width {
		return right
	}
	left = fitFooterGroups(left, "  ", width-rightWidth-1)
	if left == "" {
		return right
	}
	return left + " " + right
}
