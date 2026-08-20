package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Adyen/agent-marketplace/internal/materialize"
)

// overlayTestModel returns a model at the supported minimum terminal size
// (50x12) with loading finished, driven through Update so overlay viewport
// dimensions are synchronized exactly as in production.
func overlayTestModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	updated, _ := m.Update(tea.WindowSizeMsg{Width: minimumTerminalWidth, Height: minimumTerminalHeight})
	return updated.(Model)
}

func assertFitsFrame(t *testing.T, name, output string, width, height int) {
	t.Helper()
	lines := strings.Split(output, "\n")
	if len(lines) > height {
		t.Fatalf("%s rendered %d rows at %dx%d:\n%s", name, len(lines), width, height, output)
	}
	for _, line := range lines {
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("%s rendered %d columns at %dx%d: %q", name, got, width, height, line)
		}
	}
}

func TestHelpPaneIsScrollableWrappedViewportAtMinimum(t *testing.T) {
	m := overlayTestModel(t)

	updated, _ := m.Update(keyPress('?'))
	m = updated.(Model)
	if m.pane != paneHelp {
		t.Fatalf("? did not open help pane: %v", m.pane)
	}

	// Precondition: the full help content cannot fit in the 11-row body at
	// the supported minimum, so scrolling must exist.
	if got := strings.Count(renderHelp(m), "\n") + 1; got <= minimumTerminalHeight-1 {
		t.Fatalf("precondition: help content (%d lines) fits without scrolling at %dx12", got, minimumTerminalWidth)
	}

	output := ansi.Strip(m.View())
	assertFitsFrame(t, "help", output, minimumTerminalWidth, minimumTerminalHeight)
	for _, token := range []string{"scroll", "esc"} {
		if !strings.Contains(output, token) {
			t.Fatalf("help pane at 50x12 hid visible scroll/close key %q:\n%s", token, output)
		}
	}

	// j/down scrolls the persistent offset.
	updated, _ = m.Update(keyPress('j'))
	m = updated.(Model)
	if m.helpViewport.YOffset == 0 {
		t.Fatal("j did not scroll the help viewport")
	}
	assertFitsFrame(t, "help scrolled", ansi.Strip(m.View()), minimumTerminalWidth, minimumTerminalHeight)

	// G jumps to the bottom; the last help line becomes visible.
	updated, _ = m.Update(keyPress('G'))
	m = updated.(Model)
	if !m.helpViewport.AtBottom() {
		t.Fatal("G did not reach the bottom of the help viewport")
	}
	if output := ansi.Strip(m.View()); !strings.Contains(output, "close") {
		t.Fatalf("help bottom does not show the closing hint:\n%s", output)
	}

	// Closing and reopening keeps the scroll offset (persistent offsets).
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(Model)
	if m.pane != paneList {
		t.Fatalf("esc did not close help pane: %v", m.pane)
	}
	updated, _ = m.Update(keyPress('?'))
	m = updated.(Model)
	if m.helpViewport.YOffset == 0 || !m.helpViewport.AtBottom() {
		t.Fatalf("help scroll offset was not persistent across close/reopen: YOffset=%d", m.helpViewport.YOffset)
	}
}

func TestErrorsPaneIsScrollableWrappedViewportAtMinimum(t *testing.T) {
	m := overlayTestModel(t)
	for i := 0; i < 30; i++ {
		m.diagnostics.recordOperationFailure(operationSync, diagnosticSync, fmt.Errorf("sync failed writing output number %d with a reasonably long message body", i))
	}

	updated, _ := m.Update(keyPress('e'))
	m = updated.(Model)
	if m.pane != paneErrors {
		t.Fatalf("e did not open errors pane: %v", m.pane)
	}

	output := ansi.Strip(m.View())
	assertFitsFrame(t, "errors", output, minimumTerminalWidth, minimumTerminalHeight)
	for _, token := range []string{"scroll", "esc"} {
		if !strings.Contains(output, token) {
			t.Fatalf("errors pane at 50x12 hid visible scroll/close key %q:\n%s", token, output)
		}
	}
	// Long diagnostic messages must wrap, not clip: the tail of the long
	// message is reachable after wrapping (either on screen or via scroll).
	updated, _ = m.Update(keyPress('G'))
	m = updated.(Model)
	if !m.errorsViewport.AtBottom() {
		t.Fatal("G did not reach the bottom of the errors viewport")
	}
	output = ansi.Strip(m.View())
	assertFitsFrame(t, "errors bottom", output, minimumTerminalWidth, minimumTerminalHeight)
	if !strings.Contains(output, "message body") {
		t.Fatalf("wrapped diagnostic message tail unreachable at bottom of errors pane:\n%s", output)
	}
}

func TestErrorsPaneCloseDoesNotAcknowledgeUnseenOperationDiagnostics(t *testing.T) {
	m := overlayTestModel(t)
	for i := 0; i < 30; i++ {
		m.diagnostics.recordOperationFailure(operationSync, diagnosticSync, fmt.Errorf("unseen sync failure %d", i))
	}

	updated, _ := m.Update(keyPress('e'))
	m = updated.(Model)
	if m.errorsViewport.AtBottom() {
		t.Fatal("precondition: errors content should not be fully visible at 50x12")
	}

	// Closing the truncated pane (esc) must NOT acknowledge the operation
	// diagnostics the user never saw.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(Model)
	if m.pane != paneList {
		t.Fatalf("esc did not close errors pane: %v", m.pane)
	}
	if got := len(m.diagnostics.operations); got != 30 {
		t.Fatalf("closing a truncated errors pane acknowledged %d unseen operation diagnostics", 30-got)
	}

	// Reopen, scroll to the bottom so every operation diagnostic has been
	// visible, then esc acknowledges.
	updated, _ = m.Update(keyPress('e'))
	m = updated.(Model)
	updated, _ = m.Update(keyPress('G'))
	m = updated.(Model)
	if !m.errorsViewport.AtBottom() {
		t.Fatal("did not reach bottom before acknowledgement")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(Model)
	if got := len(m.diagnostics.operations); got != 0 {
		t.Fatalf("bottom-reached close left %d operation diagnostics unacknowledged", got)
	}
}

func TestErrorsPaneCloseAcknowledgesWhenAllDiagnosticsVisible(t *testing.T) {
	m := overlayTestModel(t)
	m.diagnostics.recordOperationFailure(operationSync, diagnosticSync, fmt.Errorf("single visible sync failure"))

	updated, _ := m.Update(keyPress('e'))
	m = updated.(Model)
	if m.pane != paneErrors {
		t.Fatalf("e did not open errors pane: %v", m.pane)
	}
	if !m.errorsViewport.AtBottom() {
		t.Fatal("precondition: a single short diagnostic must be fully visible")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(Model)
	if got := len(m.diagnostics.operations); got != 0 {
		t.Fatalf("fully-visible operation diagnostics were not acknowledged: %d retained", got)
	}
}

func TestErrorsPaneETogglesCloseWithSameAcknowledgementRule(t *testing.T) {
	m := overlayTestModel(t)
	for i := 0; i < 30; i++ {
		m.diagnostics.recordOperationFailure(operationUpdate, diagnosticUpdate, fmt.Errorf("unseen update failure %d", i))
	}
	updated, _ := m.Update(keyPress('e'))
	m = updated.(Model)
	// 'e' closes the pane too; while truncated it must not acknowledge.
	updated, _ = m.Update(keyPress('e'))
	m = updated.(Model)
	if m.pane != paneList {
		t.Fatalf("e did not close errors pane: %v", m.pane)
	}
	if got := len(m.diagnostics.operations); got != 30 {
		t.Fatalf("e-close on truncated pane acknowledged unseen diagnostics: %d retained", got)
	}
}

func TestOverlayScrollOffsetSurvivesDiagnosticsRefresh(t *testing.T) {
	m := overlayTestModel(t)
	for i := 0; i < 30; i++ {
		m.diagnostics.recordOperationFailure(operationSync, diagnosticSync, fmt.Errorf("sync failure %d", i))
	}
	updated, _ := m.Update(keyPress('e'))
	m = updated.(Model)
	updated, _ = m.Update(keyPress('j'))
	m = updated.(Model)
	updated, _ = m.Update(keyPress('j'))
	m = updated.(Model)
	offset := m.errorsViewport.YOffset
	if offset == 0 {
		t.Fatal("precondition: scrolled errors viewport has zero offset")
	}

	// A snapshot replacement while the pane is open (new warning arrives)
	// must keep the user's scroll position instead of jumping to the top.
	m.diagnostics.replaceSnapshot([]tuiDiagnostic{snapshotDiagnostic(diagnosticWarning, diagnosticConfig, fmt.Errorf("new snapshot warning"))})
	updated, _ = m.Update(tea.WindowSizeMsg{Width: minimumTerminalWidth, Height: minimumTerminalHeight})
	m = updated.(Model)
	if m.errorsViewport.YOffset != offset {
		t.Fatalf("diagnostics refresh moved scroll offset %d -> %d", offset, m.errorsViewport.YOffset)
	}
}

func TestCompactFooterPrioritizesErrorsReloadQuitWhenFailuresOrDrift(t *testing.T) {
	// Compact layout: width < 60 (50x12 is the supported minimum).
	compact := func() Model {
		m := NewModel(t.TempDir(), t.TempDir())
		m.loading = false
		m.width, m.height = minimumTerminalWidth, minimumTerminalHeight
		m.driftAssessment = driftReady
		m.driftReport = materialize.DriftReport{}
		return m
	}

	m := compact()
	if footer := ansi.Strip(renderFooter(m)); !strings.Contains(footer, "?:help · s:sync · q:quit") {
		t.Fatalf("clean compact footer = %q, want standard help/sync/quit actions", footer)
	}

	m = compact()
	m.diagnostics.replaceSnapshot([]tuiDiagnostic{snapshotDiagnostic(diagnosticError, diagnosticConfig, fmt.Errorf("broken config"))})
	if footer := ansi.Strip(renderFooter(m)); !strings.Contains(footer, "e:errors · r:reload · q:quit") {
		t.Fatalf("compact footer with errors = %q, want prioritized e:errors · r:reload · q:quit", footer)
	}

	m = compact()
	m.driftReport = materialize.DriftReport{Missing: []materialize.DriftItem{{Name: "audit", Reason: materialize.DriftMissing}}}
	if footer := ansi.Strip(renderFooter(m)); !strings.Contains(footer, "e:errors · r:reload · q:quit") {
		t.Fatalf("compact footer with pending drift = %q, want prioritized e:errors · r:reload · q:quit", footer)
	}

	m = compact()
	m.driftAssessment = driftFailed
	if footer := ansi.Strip(renderFooter(m)); !strings.Contains(footer, "e:errors · r:reload · q:quit") {
		t.Fatalf("compact footer with failed drift = %q, want prioritized e:errors · r:reload · q:quit", footer)
	}

	// Active search keeps its own actions even when errors exist.
	m = compact()
	m.diagnostics.replaceSnapshot([]tuiDiagnostic{snapshotDiagnostic(diagnosticError, diagnosticConfig, fmt.Errorf("broken config"))})
	m.filterActive = true
	if footer := ansi.Strip(renderFooter(m)); !strings.Contains(footer, "enter apply · esc cancel · q quit") {
		t.Fatalf("compact footer while searching = %q, want search actions", footer)
	}

	// Warnings alone do not displace the sync action.
	m = compact()
	m.diagnostics.replaceSnapshot([]tuiDiagnostic{snapshotDiagnostic(diagnosticWarning, diagnosticCatalog, fmt.Errorf("soft warning"))})
	if footer := ansi.Strip(renderFooter(m)); !strings.Contains(footer, "?:help · s:sync · q:quit") {
		t.Fatalf("warning-only compact footer = %q, want standard actions", footer)
	}
}

func TestOverlayScrollOffsetClampedOnResizeWithinSupportedRange(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	for i := 0; i < 60; i++ {
		m.diagnostics.recordOperationFailure(operationSync, diagnosticSync, fmt.Errorf("sync failure %d", i))
	}
	updated, _ = m.Update(keyPress('e'))
	m = updated.(Model)
	updated, _ = m.Update(keyPress('G'))
	m = updated.(Model)
	wideOffset := m.errorsViewport.YOffset
	if wideOffset == 0 {
		t.Fatal("precondition: wide errors viewport did not scroll")
	}

	updated, _ = m.Update(tea.WindowSizeMsg{Width: minimumTerminalWidth, Height: minimumTerminalHeight})
	m = updated.(Model)
	if m.pane != paneErrors {
		t.Fatalf("resize changed active pane: %v", m.pane)
	}
	// Sticky-bottom anchor: a viewport scrolled to the bottom stays at the
	// bottom even though rewrapping at the narrower width changes the
	// absolute line offset (wideOffset).
	if !m.errorsViewport.AtBottom() {
		t.Fatal("bottom-anchored scroll position lost across resize")
	}
	if m.errorsViewport.YOffset == 0 {
		t.Fatalf("resize discarded the scroll position (was %d at 120x40)", wideOffset)
	}
	assertFitsFrame(t, "errors resized", ansi.Strip(m.View()), minimumTerminalWidth, minimumTerminalHeight)
}
