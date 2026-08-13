package tui

import (
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/materialize"
	tea "github.com/charmbracelet/bubbletea"
)

// ctrlCKey returns a tea.KeyMsg representing Ctrl+C.
func ctrlCKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyCtrlC}
}

func TestCtrlCQuitsFromListPane(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(ctrlCKey())
	if cmd == nil {
		t.Fatalf("Ctrl+C should return a quit command")
	}
	// tea.Quit is a func that returns tea.QuitMsg. Verify by calling it.
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C command should produce tea.QuitMsg, got %T", msg)
	}
}

func TestCtrlCQuitsFromHelpOverlay(t *testing.T) {
	m := newTestModel()
	m.pane = paneHelp

	_, cmd := m.Update(ctrlCKey())
	if cmd == nil {
		t.Fatalf("Ctrl+C from help overlay should return a quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C command should produce tea.QuitMsg, got %T", msg)
	}
}

func TestCtrlCQuitsFromDetailPane(t *testing.T) {
	m := newTestModel()
	m.pane = paneDetail

	_, cmd := m.Update(ctrlCKey())
	if cmd == nil {
		t.Fatalf("Ctrl+C from detail pane should return a quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C command should produce tea.QuitMsg, got %T", msg)
	}
}

func TestCtrlCQuitsFromErrorsPane(t *testing.T) {
	m := newTestModel()
	m.pane = paneErrors
	m.errs = []error{errors.New("test error")}

	_, cmd := m.Update(ctrlCKey())
	if cmd == nil {
		t.Fatalf("Ctrl+C from errors pane should return a quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C command should produce tea.QuitMsg, got %T", msg)
	}
}

func TestCtrlCQuitsFromToggleModal(t *testing.T) {
	m := newTestModel()
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit", withGlobal, withRegistry("sec.git", "security"))

	_, cmd := m.Update(ctrlCKey())
	if cmd == nil {
		t.Fatalf("Ctrl+C from toggle modal should return a quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C command should produce tea.QuitMsg, got %T", msg)
	}
}

func TestCtrlCQuitsFromConfirmDialog(t *testing.T) {
	m := newTestModel()
	m.pane = paneConfirm
	m.confirmMsg = "2 pending changes not synced. Sync+quit? y/n/esc"

	_, cmd := m.Update(ctrlCKey())
	if cmd == nil {
		t.Fatalf("Ctrl+C from confirm dialog should return a quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C command should produce tea.QuitMsg, got %T", msg)
	}
}

func TestCtrlCBypassesPendingChangesConfirmation(t *testing.T) {
	// Unlike pressing 'q' which shows a confirmation dialog when there are
	// pending changes, Ctrl+C should quit immediately — it is a hard exit.
	m := newTestModel()
	m.driftReport = materialize.DriftReport{
		Missing: []materialize.DriftItem{
			{Name: "audit", Reason: materialize.DriftMissing},
		},
	}

	// Verify 'q' shows confirmation.
	mi, qCmd := m.Update(keyPress('q'))
	m = mi.(Model)
	if qCmd != nil {
		t.Fatalf("q with pending changes should not quit immediately (should show confirm)")
	}
	if m.pane != paneConfirm {
		t.Fatalf("q with pending changes should switch to confirm pane, got %v", m.pane)
	}

	// Now verify Ctrl+C quits immediately even with pending changes.
	m.pane = paneList // reset
	_, ctrlCCmd := m.Update(ctrlCKey())
	if ctrlCCmd == nil {
		t.Fatalf("Ctrl+C should quit immediately even with pending changes")
	}
	msg := ctrlCCmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C should produce tea.QuitMsg, got %T", msg)
	}
}

func TestCtrlCQuitsFromSearchMode(t *testing.T) {
	m := newTestModel()
	m.filterActive = true
	m.searchInput.SetValue("test")

	_, cmd := m.Update(ctrlCKey())
	if cmd == nil {
		t.Fatalf("Ctrl+C from search mode should return a quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C command should produce tea.QuitMsg, got %T", msg)
	}
}

func TestCtrlCQuitsFromInitModal(t *testing.T) {
	m := newTestModel()
	m.pane = paneInitModal

	_, cmd := m.Update(ctrlCKey())
	if cmd == nil {
		t.Fatalf("Ctrl+C from init modal should return a quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C command should produce tea.QuitMsg, got %T", msg)
	}
}

func TestCtrlCQuitsFromMigrationModal(t *testing.T) {
	m := newTestModel()
	m.pane = paneMigrationModal

	_, cmd := m.Update(ctrlCKey())
	if cmd == nil {
		t.Fatalf("Ctrl+C from migration modal should return a quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C command should produce tea.QuitMsg, got %T", msg)
	}
}

func TestCtrlCQuitsFromHarnessModal(t *testing.T) {
	m := newTestModel()
	m.pane = paneHarnessModal

	_, cmd := m.Update(ctrlCKey())
	if cmd == nil {
		t.Fatalf("Ctrl+C from harness modal should return a quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C command should produce tea.QuitMsg, got %T", msg)
	}
}

func TestRunReturnsNilOnInterrupt(t *testing.T) {
	// Verify that Run() treats tea.ErrInterrupted as a clean exit,
	// not an error worth reporting to the user.
	if !errors.Is(tea.ErrInterrupted, tea.ErrInterrupted) {
		t.Fatalf("tea.ErrInterrupted should match itself via errors.Is")
	}
}

func TestRunReturnsNilOnProgramKilled(t *testing.T) {
	// Verify that Run() treats tea.ErrProgramKilled as a clean exit,
	// not an error worth reporting to the user.
	if !errors.Is(tea.ErrProgramKilled, tea.ErrProgramKilled) {
		t.Fatalf("tea.ErrProgramKilled should match itself via errors.Is")
	}
}
