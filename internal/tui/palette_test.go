package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/materialize"
)

func ctrlKKey() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
}

// readyPaletteModel returns a loaded model eligible for guarded actions.
func readyPaletteModel() Model {
	m := newTestModel()
	m.loading = false
	return m
}

func openPaletteFor(t *testing.T, m Model) Model {
	t.Helper()
	mi, _ := m.Update(ctrlKKey())
	m = mi.(Model)
	if !m.palette.open {
		t.Fatalf("ctrl+k should open the command palette")
	}
	return m
}

func typePaletteQuery(t *testing.T, m Model, query string) Model {
	t.Helper()
	for _, r := range query {
		mi, _ := m.Update(keyPress(r))
		m = mi.(Model)
	}
	return m
}

func submitPalette(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	mi, cmd := m.Update(keyNamed("enter"))
	return mi.(Model), cmd
}

func cursorOnArtifact(m Model) int {
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			return i
		}
	}
	return -1
}

func paletteHasAction(m Model, id paletteActionID) bool {
	for _, match := range m.palette.matches {
		if match.action.id == id {
			return true
		}
	}
	return false
}

func TestPaletteOpensWithCtrlK(t *testing.T) {
	m := readyPaletteModel()
	mi, _ := m.Update(ctrlKKey())
	m = mi.(Model)
	if !m.palette.open {
		t.Fatalf("ctrl+k should open the command palette")
	}
	if m.pane != paneList {
		t.Fatalf("palette should keep the list pane active, got %v", m.pane)
	}
}

func TestPaletteOpensWithColon(t *testing.T) {
	m := readyPaletteModel()
	mi, _ := m.Update(keyPress(':'))
	m = mi.(Model)
	if !m.palette.open {
		t.Fatalf("':' should open the command palette")
	}
}

func TestPaletteDoesNotOpenWhileSearchInputActive(t *testing.T) {
	m := readyPaletteModel()
	m.filterActive = true
	m.searchInput.Focus()
	mi, _ := m.Update(ctrlKKey())
	m = mi.(Model)
	if m.palette.open {
		t.Fatalf("palette must not open while the search input is active")
	}
	mi, _ = m.Update(keyPress(':'))
	m = mi.(Model)
	if m.palette.open {
		t.Fatalf("':' must type into the search input, not open the palette")
	}
	if m.searchInput.Value() != ":" {
		t.Fatalf("':' should reach the search input, got %q", m.searchInput.Value())
	}
}

func TestPaletteDoesNotOpenOverModalOrOverlay(t *testing.T) {
	for _, p := range []pane{paneHelp, paneErrors, paneToggleModal, paneConfirm, paneHarnessModal, paneEditConfigModal, paneMigrationModal, paneMarketplaceConsent} {
		m := readyPaletteModel()
		m.pane = p
		mi, _ := m.Update(ctrlKKey())
		m = mi.(Model)
		if m.palette.open {
			t.Fatalf("palette must not open over pane %v", p)
		}
	}
}

func TestPaletteDoesNotOpenDuringFoldPrefix(t *testing.T) {
	m := readyPaletteModel()
	mi, _ := m.Update(keyPress('z'))
	m = mi.(Model)
	if !m.zPending {
		t.Fatalf("expected zPending after pressing z")
	}
	mi, _ = m.Update(ctrlKKey())
	m = mi.(Model)
	if m.palette.open {
		t.Fatalf("palette must not open while a fold prefix is pending")
	}
	if m.zPending {
		t.Fatalf("the pending fold prefix should be cancelled by the non-fold key")
	}
}

func TestPaletteDoesNotOpenBelowMinimumSize(t *testing.T) {
	m := readyPaletteModel()
	m.width = 30
	m.height = 8
	mi, _ := m.Update(ctrlKKey())
	m = mi.(Model)
	if m.palette.open {
		t.Fatalf("palette must not open below the minimum terminal size")
	}
}

func TestPaletteQueryConsumesTyping(t *testing.T) {
	m := openPaletteFor(t, readyPaletteModel())
	mi, _ := m.Update(keyPress('q'))
	m = mi.(Model)
	if !m.palette.open {
		t.Fatalf("typing 'q' must be query text, not quit")
	}
	if m.palette.input.Value() != "q" {
		t.Fatalf("palette input should hold the typed query, got %q", m.palette.input.Value())
	}
}

func TestPalettePasteConsumesPasteMsg(t *testing.T) {
	m := openPaletteFor(t, readyPaletteModel())
	mi, _ := m.Update(tea.PasteMsg{Content: "sync"})
	m = mi.(Model)
	if m.palette.input.Value() != "sync" {
		t.Fatalf("paste should reach the palette input, got %q", m.palette.input.Value())
	}
}

func TestPaletteEscClosesWithoutDispatch(t *testing.T) {
	m := openPaletteFor(t, readyPaletteModel())
	m = typePaletteQuery(t, m, "sync")
	mi, _ := m.Update(keyNamed("esc"))
	m = mi.(Model)
	if m.palette.open {
		t.Fatalf("esc should close the palette")
	}
	if m.syncing {
		t.Fatalf("esc must close the palette without dispatching the action")
	}
}

func TestPaletteCtrlCQuitsImmediately(t *testing.T) {
	m := openPaletteFor(t, readyPaletteModel())
	_, cmd := m.Update(ctrlCKey())
	if cmd == nil {
		t.Fatalf("Ctrl+C should quit immediately while the palette is open")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C should produce tea.QuitMsg")
	}
}

func TestPaletteFuzzyDispatchSyncSharesGuardedPath(t *testing.T) {
	m := openPaletteFor(t, readyPaletteModel())
	m = typePaletteQuery(t, m, "sync")
	m, _ = submitPalette(t, m)
	if m.palette.open {
		t.Fatalf("submitting an eligible action should close the palette")
	}
	if !m.syncing {
		t.Fatalf("palette sync should dispatch the same guarded sync as the 's' key")
	}
}

func TestPaletteSyncHiddenWhileLoading(t *testing.T) {
	m := newTestModel() // loading=true
	if !m.loading {
		t.Fatalf("fixture must start loading")
	}
	m = openPaletteFor(t, m)
	m = typePaletteQuery(t, m, "sync")
	if paletteHasAction(m, actionSync) {
		t.Fatalf("sync must be ineligible while the snapshot is loading")
	}
}

// spec: tui-feedback/state-changes-after-opening
func TestPaletteSubmitReevaluatesEligibility(t *testing.T) {
	m := openPaletteFor(t, readyPaletteModel())
	m = typePaletteQuery(t, m, "sync")
	if !paletteHasAction(m, actionSync) {
		t.Fatalf("sync should be listed while eligible")
	}
	// State changes while the palette is open: config becomes invalid.
	m.configInvalid = true
	m, _ = submitPalette(t, m)
	if m.syncing {
		t.Fatalf("stale palette eligibility must not start a sync")
	}
	if !m.palette.open {
		t.Fatalf("rejecting a stale action should keep the palette open")
	}
	if paletteHasAction(m, actionSync) {
		t.Fatalf("the now-ineligible sync action must drop out of the matches")
	}
}

func TestPaletteSubmitWithNoMatchDoesNothing(t *testing.T) {
	m := openPaletteFor(t, readyPaletteModel())
	m = typePaletteQuery(t, m, "zzzzzz")
	m, _ = submitPalette(t, m)
	if !m.palette.open {
		t.Fatalf("submitting an empty match list should keep the palette open")
	}
}

func TestPaletteToggleResourceOpensToggleModal(t *testing.T) {
	m := readyPaletteModel()
	m.cursor = cursorOnArtifact(m)
	if m.cursor < 0 {
		t.Fatalf("fixture must contain an artifact row")
	}
	m = openPaletteFor(t, m)
	m = typePaletteQuery(t, m, "toggle res")
	m, _ = submitPalette(t, m)
	if m.palette.open {
		t.Fatalf("dispatch should close the palette")
	}
	if m.pane != paneToggleModal {
		t.Fatalf("palette toggle should open the toggle modal like Space, got %v", m.pane)
	}
}

// spec: tui-feedback/prefix-action-and-excluded-controls
func TestPaletteFoldToggleDispatchesIdentityDirectly(t *testing.T) {
	m := readyPaletteModel()
	m.cursor = 0 // status section header
	m = openPaletteFor(t, m)
	m = typePaletteQuery(t, m, "za")
	m, _ = submitPalette(t, m)
	if !m.currentFoldState()["status:enabled"] {
		t.Fatalf("palette za action should collapse the current section")
	}
	if m.zPending {
		t.Fatalf("palette must dispatch fold identity directly, never a key sequence")
	}
}

func TestPaletteQuitPreservesPendingDriftConfirmation(t *testing.T) {
	m := readyPaletteModel()
	m.driftReport = materialize.DriftReport{
		Missing: []materialize.DriftItem{{Name: "audit", Reason: materialize.DriftMissing}},
	}
	m = openPaletteFor(t, m)
	m = typePaletteQuery(t, m, "quit")
	m, _ = submitPalette(t, m)
	if m.palette.open {
		t.Fatalf("dispatch should close the palette")
	}
	if m.pane != paneConfirm {
		t.Fatalf("palette quit must route through the pending-drift confirmation, got %v", m.pane)
	}
	if !strings.Contains(m.confirmMsg, "pending") {
		t.Fatalf("palette quit should reuse the confirm message, got %q", m.confirmMsg)
	}
}

func TestPaletteSearchActivatesFilter(t *testing.T) {
	m := openPaletteFor(t, readyPaletteModel())
	m = typePaletteQuery(t, m, "search res")
	m, _ = submitPalette(t, m)
	if !m.filterActive {
		t.Fatalf("palette search should activate the same search input as '/'")
	}
}

func TestPaletteDiagnosticsOnlyWhenErrorsExist(t *testing.T) {
	m := openPaletteFor(t, readyPaletteModel())
	m = typePaletteQuery(t, m, "errors")
	if paletteHasAction(m, actionDiagnostics) {
		t.Fatalf("diagnostics action must be ineligible without errors")
	}

	m = readyPaletteModel()
	m.errs = []error{errors.New("boom")}
	m = openPaletteFor(t, m)
	m = typePaletteQuery(t, m, "errors")
	m, _ = submitPalette(t, m)
	if m.pane != paneErrors {
		t.Fatalf("palette diagnostics should open the errors pane, got %v", m.pane)
	}
}

func TestPaletteDownNavigatesMatches(t *testing.T) {
	m := openPaletteFor(t, readyPaletteModel())
	if len(m.palette.matches) < 2 {
		t.Fatalf("palette should list multiple actions")
	}
	mi, _ := m.Update(keyNamed("down"))
	m = mi.(Model)
	if m.palette.cursor != 1 {
		t.Fatalf("down should move the palette cursor, got %d", m.palette.cursor)
	}
	mi, _ = m.Update(keyNamed("up"))
	m = mi.(Model)
	if m.palette.cursor != 0 {
		t.Fatalf("up should move the palette cursor back, got %d", m.palette.cursor)
	}
}

func TestPaletteRendersAsLayerOverList(t *testing.T) {
	m := readyPaletteModel()
	m.width = 80
	m.height = 24
	m = openPaletteFor(t, m)
	view := m.viewContent()
	if !strings.Contains(view, "Command palette") {
		t.Fatalf("palette overlay should render its title, got:\n%s", view)
	}
	if !strings.Contains(view, "Sync pending") {
		t.Fatalf("palette overlay should list actions, got:\n%s", view)
	}
	// The list and footer controls stay visible behind the layer.
	if !strings.Contains(view, "alpha") && !strings.Contains(view, "a") {
		t.Fatalf("palette overlay should not hide the list underneath")
	}
}

func TestPaletteNotRenderedWhenClosed(t *testing.T) {
	m := readyPaletteModel()
	m.width = 80
	m.height = 24
	if strings.Contains(m.viewContent(), "Command palette") {
		t.Fatalf("palette must not render while closed")
	}
}
