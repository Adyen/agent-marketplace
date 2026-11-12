package tui

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/Adyen/agent-marketplace/internal/cache"
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

// footerItem is one footer segment. Items render in display order, but
// when space runs out they are admitted by ascending rank, so a lower-rank
// item is never dropped to make room for a higher-rank one.
type footerItem struct {
	text  string
	short string // optional shorter form tried when text does not fit
	rank  int
	hint  bool // hints are joined by " · ", other segments by two spaces
	clip  bool // may be truncated to fill the remaining width
}

const (
	rankCritical = iota // startup progress, error count
	rankQuit
	rankRecovery // error diagnostics and reload
	rankToast
	rankPalette
	rankHelp
	rankStatus // status segments, offset by their order
	rankAction = rankStatus + 20
)

// renderedHints memoizes styled hint text: the hint styles are fixed, and
// the footer re-renders the same hints on every frame.
var renderedHints sync.Map

func hintItem(key, desc string, rank int, floor bool) footerItem {
	cacheKey := key + "\x00" + desc
	if floor {
		cacheKey += "\x00floor"
	}
	text, ok := renderedHints.Load(cacheKey)
	if !ok {
		keyStyle := shortcutKeyStyle
		if floor {
			keyStyle = styleSearchPrompt
		}
		text, _ = renderedHints.LoadOrStore(cacheKey, keyStyle.Render(key)+" "+shortcutDescStyle.Render(desc))
	}
	return footerItem{text: text.(string), rank: rank, hint: true}
}

// footerFloor is the always-reachable entry points: the command palette,
// help and quit. While search input is focused they are query text, so
// the floor becomes how to apply or cancel the query instead.
func (m Model) footerFloor() []footerItem {
	if m.filterActive {
		return []footerItem{
			hintItem("enter", "apply", rankRecovery, true),
			hintItem("esc", "cancel", rankRecovery, true),
			hintItem("ctrl+c", "quit", rankQuit, true),
		}
	}
	palette := hintItem(":", "commands", rankPalette, true)
	palette.short = hintItem(":", "cmds", rankPalette, true).text
	return []footerItem{palette, hintItem("?", "help", rankHelp, true), hintItem("q", "quit", rankQuit, true)}
}

// footerActionHints lists the optional list-pane hints in priority order;
// they drop individually from the end when the footer runs out of room.
func (m Model) footerActionHints() []footerItem {
	actions := [][2]string{{"/", "search"}, {"space", "toggle"}, {"s", "sync"}, {"H", "harnesses"}, {"E", "config"}, {"v", "view"}, {"i", "install from source"}}
	if r, ok := m.selectedRow(); ok && rowToggleDenial(r) != toggleEligible {
		actions = slices.DeleteFunc(actions, func(a [2]string) bool { return a[0] == "space" })
	}
	if m.failuresOrDrift() || len(m.discoveryErrs) > 0 {
		actions = append([][2]string{{"r", "reload"}}, actions...)
	}
	if m.hasDiagnostics() {
		actions = append([][2]string{{"e", "errors"}}, actions...)
	}
	hints := make([]footerItem, len(actions))
	for i, a := range actions {
		hints[i] = hintItem(a[0], a[1], rankAction+i, false)
		if a[0] == "s" {
			for _, warning := range m.warnings {
				if errors.Is(warning, cache.ErrExactGitSnapshotUnavailable) {
					hints[i].rank = rankRecovery
					break
				}
			}
		}
	}
	return hints
}

func renderFooter(m Model) string {
	if len(m.errs) > 0 {
		return renderErrorFooter(m)
	}
	priority, status := m.footerStatus()
	var items []footerItem
	if priority != "" {
		items = append(items, footerItem{text: priority, rank: rankCritical, clip: true})
	}
	for i, s := range status {
		items = append(items, footerItem{text: s, rank: rankStatus + i})
	}
	if !m.filterActive {
		items = append(items, m.footerActionHints()...)
	}
	items = append(items, m.footerFloor()...)
	if m.toast != "" && !m.toastExp.IsZero() {
		items = append(items, footerItem{text: styleToast.Render(sanitizeTerminalData(m.toast, terminalSingleLine)), rank: rankToast, clip: true})
	}
	return layoutFooter(m.width, items)
}

// footerStatus returns the priority status (startup verification, which
// must never drop) and the remaining status segments in priority order.
func (m Model) footerStatus() (string, []string) {
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
	} else if m.progress != nil && m.operations.Active != nil {
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
	if m.syncing || (m.progress != nil && m.operations.Active != nil) {
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

	if m.verifyingResources() {
		return leftParts[0], leftParts[1:]
	}
	return "", leftParts
}

// renderErrorFooter keeps the persistent error count and its recovery
// hints ahead of every other segment, then spends any remaining width on
// the latest error message.
func renderErrorFooter(m Model) string {
	status := styleFooterError.Render(fmt.Sprintf("! %d errors", len(m.errs)))
	if m.verifyingResources() {
		status = m.spinner.View() + styleFooterError.Render(" verifying resources... · "+ansi.Strip(status))
	}
	items := []footerItem{{text: status, rank: rankCritical}}
	if !m.filterActive {
		items = append(items, hintItem("e", "errors", rankRecovery, false), hintItem("r", "reload", rankRecovery, false))
	}
	items = append(items, m.footerFloor()...)
	footer := layoutFooter(m.width, items)
	room := m.width - ansi.StringWidth(footer) - 2
	if room < 4 {
		return footer
	}
	latest := ""
	if err := m.errs[len(m.errs)-1]; err != nil {
		latest = sanitizeTerminalData(diagnosticCause(err), terminalSingleLine)
	}
	if latest == "" {
		return footer
	}
	items[0].text = status + styleFooterError.Render(": "+ansi.Truncate(latest, room, "…"))
	return layoutFooter(m.width, items)
}

// layoutFooter admits items by ascending rank (display order breaks ties)
// while the joined line still fits width, trying each item's short form
// and then clipping clip-able items, and renders the admitted items in
// display order.
func layoutFooter(width int, items []footerItem) string {
	if width <= 0 {
		return ""
	}
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return items[order[a]].rank < items[order[b]].rank })
	chosen := make([]string, len(items))
	widths := make([]int, len(items)) // width of chosen[i]; 0 when not admitted
	for _, i := range order {
		for _, text := range []string{items[i].text, items[i].short} {
			if text == "" {
				continue
			}
			widths[i] = ansi.StringWidth(text)
			if footerWidth(items, widths) <= width {
				chosen[i] = text
				break
			}
			widths[i] = 0
		}
		if chosen[i] != "" || !items[i].clip {
			continue
		}
		widths[i] = 1
		if room := width - footerWidth(items, widths) + 1; room > 1 {
			chosen[i] = ansi.Truncate(items[i].text, room, "…")
			widths[i] = ansi.StringWidth(chosen[i])
		} else {
			widths[i] = 0
		}
	}
	return joinFooter(items, chosen)
}

// footerWidth is the display width joinFooter would produce for the items
// with non-zero widths, computed without building the string.
func footerWidth(items []footerItem, widths []int) int {
	total, prev := 0, -1
	for i, w := range widths {
		if w == 0 {
			continue
		}
		if prev >= 0 {
			total += ansi.StringWidth(footerSeparator(items[prev], items[i]))
		}
		total += w
		prev = i
	}
	return total
}

func footerSeparator(prev, next footerItem) string {
	if prev.hint && next.hint {
		return " · "
	}
	return "  "
}

// joinFooter joins the chosen item texts in display order: consecutive
// hints with " · ", anything else with two spaces.
func joinFooter(items []footerItem, chosen []string) string {
	var b strings.Builder
	prev := -1
	for i, text := range chosen {
		if text == "" {
			continue
		}
		if prev >= 0 {
			b.WriteString(footerSeparator(items[prev], items[i]))
		}
		b.WriteString(text)
		prev = i
	}
	return b.String()
}

func (m Model) hasUnavailableRows() bool {
	return rowsHaveUnavailable(m.rows)
}

// hasDiagnostics reports whether the errors view has anything to show.
func (m Model) hasDiagnostics() bool {
	if len(m.hookChanges) > 0 {
		return true
	}
	return len(m.errs) > 0 || len(m.warnings) > 0 || len(m.discoveryErrs) > 0
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
