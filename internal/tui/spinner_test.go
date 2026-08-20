package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// newSpinnerTestModel returns a minimal Model with the spinner initialised,
// suitable for unit-testing the footer and spinner Update logic.
func newSpinnerTestModel() Model {
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneList
	m.tabStates = newTabStates()
	return m
}

func TestFooterShowsSpinnerWhenLoading(t *testing.T) {
	m := newSpinnerTestModel()
	m.loading = true

	footer := renderFooter(m)
	spinnerView := m.spinner.View()
	if spinnerView == "" {
		t.Fatalf("spinner.View() should produce output; got empty string")
	}
	if !strings.Contains(footer, spinnerView) {
		t.Fatalf("footer should contain spinner view %q when loading; got %q", spinnerView, footer)
	}
}

func TestFooterShowsSpinnerWhenSyncing(t *testing.T) {
	m := newSpinnerTestModel()
	m.syncing = true

	footer := renderFooter(m)
	spinnerView := m.spinner.View()
	if !strings.Contains(footer, spinnerView) {
		t.Fatalf("footer should contain spinner view %q when syncing; got %q", spinnerView, footer)
	}
}

func TestFooterShowsSpinnerWhenRefreshing(t *testing.T) {
	m := newSpinnerTestModel()
	m.refreshing = true

	footer := renderFooter(m)
	spinnerView := m.spinner.View()
	if !strings.Contains(footer, spinnerView) {
		t.Fatalf("footer should contain spinner view %q when refreshing; got %q", spinnerView, footer)
	}
}

func TestFooterNoSpinnerWhenIdle(t *testing.T) {
	m := newSpinnerTestModel()
	// loading, syncing, refreshing all false; drift in sync.
	m.loading = false
	m.driftReport = materialize.DriftReport{}

	footer := renderFooter(m)
	spinnerView := m.spinner.View()
	if strings.Contains(footer, spinnerView) && spinnerView != "" {
		t.Fatalf("footer should NOT contain spinner view %q when idle; got %q", spinnerView, footer)
	}
}

func TestFooterKeepsToastWithinNarrowTerminal(t *testing.T) {
	m := newSpinnerTestModel()
	m.width = 12
	m.toast = "a long error toast"
	m.toastExp = time.Now().Add(time.Minute)

	footer := renderFooter(m)
	if got := lipgloss.Width(footer); got > m.width {
		t.Fatalf("footer width = %d, want <= %d: %q", got, m.width, footer)
	}
	if lipgloss.Width(footer) != m.width {
		t.Fatalf("narrow footer width = %d, want exactly %d: %q", lipgloss.Width(footer), m.width, footer)
	}
}

func TestFooterRightAlignsToastWhenContentFits(t *testing.T) {
	m := newSpinnerTestModel()
	m.width = 80
	m.toast = "saved"
	m.toastExp = time.Now().Add(time.Minute)

	footer := renderFooter(m)
	toast := styleToast.Render(m.toast)
	if !strings.HasSuffix(footer, toast) {
		t.Fatalf("footer should end with toast %q, got %q", toast, footer)
	}
	if got := lipgloss.Width(footer); got > m.width {
		t.Fatalf("footer width = %d, want <= %d", got, m.width)
	}
}

func TestUpdateHandlesSpinnerTickMsg(t *testing.T) {
	m := newSpinnerTestModel()
	m.loading = true

	tickMsg := spinner.TickMsg{
		Time: time.Now(),
		ID:   m.spinner.ID(),
	}

	updatedModel, cmd := m.Update(tickMsg)
	um := updatedModel.(Model)

	// The spinner should have advanced (Update returns a new tick cmd).
	if cmd == nil {
		t.Fatalf("expected spinner.Update to return a tick command, got nil")
	}

	// Spinner should still be active (loading is true).
	if !um.loading {
		t.Fatalf("loading flag should still be true after spinner tick")
	}
}

func TestUpdateStopsSpinnerTickWhenIdle(t *testing.T) {
	m := newSpinnerTestModel()
	// All in-flight flags false; drift in sync.
	m.loading = false
	m.syncing = false
	m.refreshing = false
	m.driftReport = materialize.DriftReport{}

	tickMsg := spinner.TickMsg{
		Time: time.Now(),
		ID:   m.spinner.ID(),
	}

	updatedModel, cmd := m.Update(tickMsg)
	um := updatedModel.(Model)

	// When no in-flight work, spinner tick should NOT produce another tick.
	if cmd != nil {
		t.Fatalf("expected no tick command when idle, got non-nil cmd")
	}
	_ = um
}

func TestInitBatchesSpinnerTick(t *testing.T) {
	m := newSpinnerTestModel()
	m.loading = true

	cmd := m.Init()
	if cmd == nil {
		t.Fatalf("Init should return a batched command including spinner tick")
	}
}

func TestSpinnerStartsWhenSyncBegins(t *testing.T) {
	m := newSpinnerTestModel()
	// Add at least one pending drift item so sync is allowed.
	m.driftReport = materialize.DriftReport{Missing: []materialize.DriftItem{{Reason: materialize.DriftMissing}}}
	m.resolved = nil // sync will fail quickly but that's fine for testing the cmd

	// Simulate pressing 's'
	updatedModel, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatalf("expected sync keypress to return a command")
	}
	um := updatedModel.(Model)
	if !um.syncing {
		t.Fatalf("expected syncing flag to be true after pressing 's'")
	}
	// The footer should now show the spinner.
	footer := renderFooter(um)
	spinnerView := um.spinner.View()
	if !strings.Contains(footer, spinnerView) {
		t.Fatalf("footer should contain spinner view %q when syncing; got %q", spinnerView, footer)
	}
}
