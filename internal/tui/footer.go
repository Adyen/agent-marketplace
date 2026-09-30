package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m Model) verifyingResources() bool {
	return m.assessmentPending || (m.loading && len(m.provisionalRows) > 0)
}

// driftStatusSegment renders the drift portion of the status bar.
func (m Model) driftStatusSegment() string {
	if m.driftAssessment != driftReady || m.hasUnavailableRows() {
		return styleDriftPending.Render("~ drift unavailable")
	}
	if m.driftReport.InSync() {
		return styleDriftOK.Render("~ in sync")
	}
	return styleDriftPending.Render(fmt.Sprintf("~ %d out of sync", m.driftReport.PendingCount()))
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
		status := " syncing..."
		if m.progress != nil {
			phase := m.operationPhase
			if phase == "" {
				phase = "running"
			}
			status = " " + m.progressLabel + ": " + phase + "..."
		}
		leftParts = append(leftParts, m.spinner.View()+styleFooterPending.Render(status))
	} else if m.progress != nil && m.operations.active != nil {
		phase := m.operationPhase
		if phase == "" {
			phase = "running"
		}
		leftParts = append(leftParts, m.spinner.View()+styleFooterPending.Render(" "+m.progressLabel+": "+phase+"..."))
	} else if m.refreshing {
		leftParts = append(leftParts, m.spinner.View()+styleFooterRefreshing.Render(" refreshing cache..."))
	} else if m.assessmentPending {
		leftParts = append(leftParts, m.spinner.View()+styleFooterPending.Render(" verifying resources..."))
	} else {
		leftParts = append(leftParts, m.driftStatusSegment())
	}
	// An active operation replaces the leading status but drift remains a
	// separate segment, so the status bar still shows drift, Marketplace
	// health, the operation, and diagnostic counts at once.
	if m.syncing || (m.progress != nil && m.operations.active != nil) {
		leftParts = append(leftParts, m.driftStatusSegment())
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

	priority := ""
	if m.verifyingResources() {
		priority, leftParts = leftParts[0], leftParts[1:]
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
		return statusBar(priority, status, actions, m.width)
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
	return statusBar(priority, strings.Join(leftParts, "  "), right, m.width)
}

func renderErrorFooter(m Model) string {
	actions := "e:errors · r:reload · q:quit"
	if m.filterActive {
		actions = "enter apply · esc cancel · q quit"
	}
	status := fmt.Sprintf("! %d errors", len(m.errs))
	if m.verifyingResources() {
		status = m.spinner.View() + " verifying resources... · " + status
		actions = fitStatusGroups(actions, " · ", max(0, m.width-ansi.StringWidth(status)-1))
	}
	budget := m.width - ansi.StringWidth(actions) - 1
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

// fitStatusGroups adapts a separator-joined status bar segment (actions
// joined by " · ", status groups joined by "  ") to the available width by
// keeping whole trailing groups that fit rather than cutting a partial word.
// When no single group fits, the last group is truncated with an ellipsis so
// the output stays bounded.
func fitStatusGroups(s, separator string, width int) string {
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

// statusBar composes the footer's priority status, status segments, and
// action hints into one bounded bar: the priority segment wins first, then
// whole separator-joined groups are kept while they fit.
func statusBar(priority, left, right string, width int) string {
	if priority == "" {
		return statusBarSegments(left, right, width)
	}
	priority = ansi.Truncate(priority, max(0, width), "…")
	rest := statusBarSegments(left, right, width-ansi.StringWidth(priority)-1)
	if rest == "" {
		return priority
	}
	return lipgloss.JoinHorizontal(lipgloss.Bottom, priority, " ", rest)
}

func statusBarSegments(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	right = fitStatusGroups(right, " · ", width)
	rightWidth := ansi.StringWidth(right)
	if rightWidth >= width {
		return right
	}
	left = fitStatusGroups(left, "  ", width-rightWidth-1)
	if left == "" {
		return right
	}
	return lipgloss.JoinHorizontal(lipgloss.Bottom, left, " ", right)
}
