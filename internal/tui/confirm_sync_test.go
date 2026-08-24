package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/materialize"
	tea "github.com/charmbracelet/bubbletea"
)

// noopSyncCmd replaces runSyncCmd in tests so that anyCmdProducesQuitMsg
// does not execute real sync I/O when it calls commands to inspect their
// output.
var noopSyncCmd = func(ctx context.Context, repoRoot string) tea.Cmd {
	return func() tea.Msg {
		return syncDoneMsg{err: nil}
	}
}

// cmdProducesQuitMsg calls cmd (if non-nil) and reports whether the
// resulting message is a tea.QuitMsg.
func cmdProducesQuitMsg(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// batchCmds calls a tea.Batch cmd and returns the individual commands.
// If the cmd is not a batch, returns nil.
func batchCmds(cmd tea.Cmd) []tea.Cmd {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return nil
	}
	return batch
}

// anyCmdProducesQuitMsg checks whether any command in a batch (or the
// single command if not a batch) produces tea.QuitMsg.
func anyCmdProducesQuitMsg(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if cmdProducesQuitMsg(c) {
				return true
			}
		}
		return false
	}
	_, ok := msg.(tea.QuitMsg)
	return ok
}

func TestConfirmYesStartsSyncWithoutImmediateQuit(t *testing.T) {
	origRunSyncCmd := runSyncCmd
	runSyncCmd = noopSyncCmd
	defer func() { runSyncCmd = origRunSyncCmd }()

	m := newTestModel()
	m.pane = paneConfirm
	m.driftReport = materialize.DriftReport{
		Missing: []materialize.DriftItem{
			{Name: "a", Reason: materialize.DriftMissing},
		},
	}

	updated, cmd := m.Update(keyPress('y'))
	got := updated.(Model)

	if !got.syncing {
		t.Fatalf("ConfirmYes should set syncing=true")
	}
	if !got.quitAfterSync {
		t.Fatalf("ConfirmYes should set quitAfterSync=true")
	}
	if got.pane != paneList {
		t.Fatalf("ConfirmYes should switch to paneList, got %v", got.pane)
	}
	if anyCmdProducesQuitMsg(cmd) {
		t.Fatalf("ConfirmYes should NOT emit tea.Quit immediately — sync must complete first")
	}
	// The batch should contain at least one command (the sync cmd).
	cmds := batchCmds(cmd)
	if cmds == nil {
		t.Fatalf("ConfirmYes should return a batch of commands (sync + spinner)")
	}
}

func TestSyncDoneMsgWithQuitAfterSyncEmitsQuit(t *testing.T) {
	m := newTestModel()
	m.syncing = true
	m.quitAfterSync = true

	updated, cmd := m.Update(syncDoneMsg{err: nil})
	got := updated.(Model)

	if got.syncing {
		t.Fatalf("syncDoneMsg should clear syncing")
	}
	if got.quitAfterSync {
		t.Fatalf("syncDoneMsg should clear quitAfterSync after quitting")
	}
	if !cmdProducesQuitMsg(cmd) {
		t.Fatalf("syncDoneMsg with quitAfterSync=true should emit tea.Quit")
	}
}

func TestSyncDoneMsgWithoutQuitAfterSyncDoesNotQuit(t *testing.T) {
	m := newTestModel()
	m.syncing = true
	m.quitAfterSync = false

	updated, cmd := m.Update(syncDoneMsg{err: nil})
	got := updated.(Model)

	if got.syncing {
		t.Fatalf("syncDoneMsg should clear syncing")
	}
	if got.quitAfterSync {
		t.Fatalf("syncDoneMsg should not set quitAfterSync")
	}
	if cmdProducesQuitMsg(cmd) {
		t.Fatalf("syncDoneMsg without quitAfterSync should NOT emit tea.Quit")
	}
}

func TestSyncDoneMsgWithErrorAndQuitAfterSyncStillQuits(t *testing.T) {
	m := newTestModel()
	m.syncing = true
	m.quitAfterSync = true

	updated, cmd := m.Update(syncDoneMsg{err: errors.New("sync failed")})
	got := updated.(Model)

	// Even on sync error, we should still quit (the user asked to sync+quit).
	if got.quitAfterSync {
		t.Fatalf("syncDoneMsg should clear quitAfterSync")
	}
	if !cmdProducesQuitMsg(cmd) {
		t.Fatalf("syncDoneMsg with quitAfterSync=true should emit tea.Quit even on error")
	}
}

func TestConfirmNoQuitsImmediatelyAndClearsQuitAfterSync(t *testing.T) {
	m := newTestModel()
	m.pane = paneConfirm
	m.quitAfterSync = true // stale from a previous confirm cycle

	updated, cmd := m.Update(keyPress('n'))
	got := updated.(Model)

	if got.quitAfterSync {
		t.Fatalf("ConfirmNo should clear quitAfterSync")
	}
	if !cmdProducesQuitMsg(cmd) {
		t.Fatalf("ConfirmNo should emit tea.Quit")
	}
}

func TestConfirmCancelClearsQuitAfterSync(t *testing.T) {
	m := newTestModel()
	m.pane = paneConfirm
	m.quitAfterSync = true // stale from a previous confirm cycle

	updated, cmd := m.Update(keyNamed("esc"))
	got := updated.(Model)

	if got.quitAfterSync {
		t.Fatalf("ConfirmCancel should clear quitAfterSync")
	}
	if cmdProducesQuitMsg(cmd) {
		t.Fatalf("ConfirmCancel should NOT emit tea.Quit")
	}
	if got.pane != paneList {
		t.Fatalf("ConfirmCancel should switch to paneList, got %v", got.pane)
	}
}

func TestConfirmYesWhileSyncingDoesNotStartSecondSync(t *testing.T) {
	m := newTestModel()
	m.pane = paneConfirm
	m.syncing = true // already syncing

	updated, cmd := m.Update(keyPress('y'))
	got := updated.(Model)

	if got.pane != paneList {
		t.Fatalf("ConfirmYes while syncing should return to paneList, got %v", got.pane)
	}
	if cmd != nil {
		t.Fatalf("ConfirmYes while syncing should return nil cmd (no second sync)")
	}
}

func TestSyncDoneMsgWithErrorStoresSyncQuitErr(t *testing.T) {
	m := newTestModel()
	m.syncing = true
	m.quitAfterSync = true

	syncErr := errors.New("sync failed")
	updated, _ := m.Update(syncDoneMsg{err: syncErr})
	got := updated.(Model)

	if got.syncQuitErr == nil {
		t.Fatalf("syncDoneMsg with error should store syncQuitErr for Run() to propagate")
	}
	if !errors.Is(got.syncQuitErr, syncErr) {
		t.Fatalf("syncQuitErr should match the sync error, got %v", got.syncQuitErr)
	}
}

func TestUpdateDoneMsgClearsQuitAfterSync(t *testing.T) {
	m := newTestModel()
	m.syncing = true
	m.quitAfterSync = true // stale from a previous sync+quit confirm

	updated, cmd := m.Update(updateDoneMsg{err: nil})
	got := updated.(Model)

	if got.syncing {
		t.Fatalf("updateDoneMsg should clear syncing")
	}
	if got.quitAfterSync {
		t.Fatalf("updateDoneMsg should clear stale quitAfterSync to prevent later unrelated quit")
	}
	if cmdProducesQuitMsg(cmd) {
		t.Fatalf("updateDoneMsg should NOT emit tea.Quit")
	}
}

func TestToastExpiredMsgClearsToast(t *testing.T) {
	m := newTestModel()
	m.toast = "some error"
	m.toastExp = time.Now().Add(-1 * time.Second) // already expired

	updated, _ := m.Update(toastExpiredMsg{})
	got := updated.(Model)

	if got.toast != "" {
		t.Fatalf("toastExpiredMsg should clear expired toast, got %q", got.toast)
	}
}

func TestToastExpiredMsgDoesNotClearFreshToast(t *testing.T) {
	m := newTestModel()
	m.toast = "some error"
	m.toastExp = time.Now().Add(10 * time.Second) // not yet expired

	updated, _ := m.Update(toastExpiredMsg{})
	got := updated.(Model)

	if got.toast != "some error" {
		t.Fatalf("toastExpiredMsg should NOT clear fresh toast")
	}
}
