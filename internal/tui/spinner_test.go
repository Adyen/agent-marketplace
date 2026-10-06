package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Adyen/agent-marketplace/internal/materialize"
)

// newSpinnerTestModel returns a minimal Model with the spinner initialised,
// suitable for unit-testing the footer and spinner Update logic.
func newSpinnerTestModel() Model {
	m := NewModel("/repo", "/home")
	m.loading = false
	m.pane = paneList
	return m
}

func TestStartupProgressSurvivesFooterCrowding(t *testing.T) {
	for _, width := range []int{50, 60, 80, 100, 120} {
		for _, assessment := range []bool{false, true} {
			for _, withError := range []bool{false, true} {
				m := newSpinnerTestModel()
				m.width, m.height = width, 24
				m.loading = !assessment
				m.assessmentPending = assessment
				m.provisionalRows = []row{{}}
				m.warnings = []error{errors.New("warning")}
				m.discoveryErrs = []error{errors.New("discovery failed")}
				m.toast, m.toastExp = "saved", time.Now().Add(time.Minute)
				if withError {
					m.errs = []error{errors.New("catalog failed")}
				}
				view := m.View().Content
				if !strings.Contains(view, "verifying resources...") {
					t.Errorf("width=%d assessment=%t error=%t hides progress:\n%s", width, assessment, withError, view)
				}
				footer := renderFooter(m)
				if !strings.Contains(footer, m.spinner.View()) || !strings.Contains(footer, "quit") {
					t.Errorf("footer lost spinner or quit hint: %q", footer)
				}
				if withError && !strings.Contains(footer, "! 1 errors") {
					t.Errorf("footer lost error count: %q", footer)
				}
				assertTerminalBounds(t, view, width, m.height)
			}
		}
	}
}

func TestFooterShowsSpinnerWhenLoading(t *testing.T) {
	m := newSpinnerTestModel()
	m.loading = true

	footer := renderFooter(m)
	spinnerView := m.spinner.View()
	if spinnerView == "" {
		t.Fatalf("spinner.View().Content should produce output; got empty string")
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

func TestSpinnerContinuesDuringStartupAssessment(t *testing.T) {
	m := newSpinnerTestModel()
	generation := m.requests.Active(requestSnapshot)
	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageInventory, generation: generation})
	m = updated.(Model)
	if m.loading {
		t.Fatal("inventory must remain usable during assessment")
	}
	updated, cmd := m.Update(spinner.TickMsg{Time: time.Now(), ID: m.spinner.ID()})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("spinner stopped before startup assessment finished")
	}
	m.errs = []error{errors.New("catalog failed")}
	if footer := renderFooter(m); !strings.Contains(footer, "verifying resources...") || !strings.Contains(footer, "! 1 errors") {
		t.Fatalf("error footer hid startup progress or error: %q", footer)
	}
	updated, _ = m.Update(snapshotLoadedMsg{generation: generation - 1, driftAssessment: driftReady})
	m = updated.(Model)
	if footer := renderFooter(m); !strings.Contains(footer, "verifying resources...") {
		t.Fatalf("stale completion stopped startup progress: %q", footer)
	}
	updated, _ = m.Update(snapshotLoadedMsg{generation: generation, driftAssessment: driftFailed})
	m = updated.(Model)
	if footer := renderFooter(m); strings.Contains(footer, "verifying resources...") {
		t.Fatalf("failed assessment still shows startup progress: %q", footer)
	}
	_, cmd = m.Update(spinner.TickMsg{Time: time.Now(), ID: m.spinner.ID()})
	if cmd != nil {
		t.Fatal("spinner kept ticking after failed assessment")
	}
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
	updatedModel, cmd := m.handleKey(tea.KeyPressMsg{Code: 's', Text: string('s')})
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
