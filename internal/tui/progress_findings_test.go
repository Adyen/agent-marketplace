package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

// TestStaleProgressPollDoesNotSpawnPump proves an idle wake from a finished
// operation cannot reschedule the observation pump of a newer operation:
// the poll is bound to its source and dropped once the source is superseded.
func TestStaleProgressPollDoesNotSpawnPump(t *testing.T) {
	m := progressTestModel(t)
	m.beginOperationProgress("sync", 1)
	sourceA := m.progress
	if sourceA == nil {
		t.Fatal("sync progress source was not created")
	}

	// Operation A ends and B begins before A's idle pump wakes; the wake
	// arrives with A's source identity after it has been superseded.
	m.beginOperationProgress("update", 2)
	sourceB := m.progress
	if sourceB == nil {
		t.Fatal("update progress source was not created")
	}

	next, cmd := m.Update(operationProgressPollMsg{source: sourceA})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("stale pump event spawned a second pump for the active operation")
	}
	if m.progress != sourceB || m.progressLabel != "update" {
		t.Fatalf("stale pump event changed active source=%p label=%q, want source=%p label=update", m.progress, m.progressLabel, sourceB)
	}
}

// TestStaleUpdateDoneKeepsOperationActiveThroughReload proves a superseded
// update completion cannot finish the operation early: progress and
// serialization stay active through the authoritative reload and only
// resolve when the fresh snapshot is accepted or fails.
// spec: tui-interactions/cancel-or-stale-completion
func TestStaleUpdateDoneKeepsOperationActiveThroughReload(t *testing.T) {
	m := progressTestModel(t)
	previousWarning := errors.New("previous update warning")
	warning := errors.New("stale update warning")
	m.recordOperationWarnings(operationUpdate, diagnosticUpdate, previousWarning)
	gen1 := m.beginRequest(requestSnapshot)
	m.syncing = true
	m.beginOperationProgress("update", gen1)

	// A newer snapshot request supersedes the update's reload before its
	// completion message arrives.
	m.beginRequest(requestSnapshot)

	next, cmd := m.Update(updateDoneMsg{generation: gen1, stage: updateStageMaterialization, warnings: []error{warning}})
	m = next.(Model)
	if !m.syncing {
		t.Fatal("superseded update completion finished the operation before its reload")
	}
	if m.progress == nil {
		t.Fatal("superseded update completion cleared phase observation before reload")
	}
	if m.operationPhase != string(pipeline.PhaseReload) {
		t.Fatalf("active operation phase = %q, want %q during superseding reload", m.operationPhase, pipeline.PhaseReload)
	}
	if len(m.warnings) != 1 || m.warnings[0] != previousWarning {
		t.Fatalf("stale completion changed operation warnings before assessment: %v", m.warnings)
	}
	if cmd == nil {
		t.Fatal("superseded update did not schedule an authoritative reload")
	}

	next, _ = m.Update(snapshotLoadedMsg{
		generation:      m.requests.active(requestSnapshot),
		mergedCfg:       &config.Config{},
		driftAssessment: driftReady,
	})
	m = next.(Model)
	if m.syncing || m.progress != nil {
		t.Fatalf("accepted reload did not finish the operation: syncing %t progress %v", m.syncing, m.progress)
	}
	if len(m.warnings) != 1 || m.warnings[0] != warning {
		t.Fatalf("accepted reload did not replace prior warnings with the update warning: %v", m.warnings)
	}
	if m.toast != "Update complete with 1 warning(s)" {
		t.Fatalf("accepted reload toast = %q; want warning completion", m.toast)
	}
}

// TestStaleUpdateReloadFailureEndsOperation covers the failed reload branch:
// a superseded update whose authoritative reload cannot be verified ends the
// operation as a failure rather than leaving it running or claiming success.
func TestStaleUpdateReloadFailureEndsOperation(t *testing.T) {
	m := progressTestModel(t)
	pendingWarning := errors.New("update warning before failed reload")
	gen1 := m.beginRequest(requestSnapshot)
	m.syncing = true
	m.beginOperationProgress("update", gen1)
	m.beginRequest(requestSnapshot)

	next, _ := m.Update(updateDoneMsg{generation: gen1, stage: updateStageMaterialization, warnings: []error{pendingWarning}})
	m = next.(Model)

	m.errs = []error{errors.New("reload failed")}
	next, _ = m.Update(snapshotLoadedMsg{
		generation: m.requests.active(requestSnapshot),
		// mergedCfg nil + errs → failed authoritative reload.
	})
	m = next.(Model)
	if m.syncing || m.progress != nil {
		t.Fatalf("failed reload left the operation running: syncing %t progress %v", m.syncing, m.progress)
	}
	if len(m.errs) == 0 {
		t.Fatal("failed reload dropped the diagnostic record")
	}
	if len(m.warnings) != 1 || !errors.Is(m.warnings[0], pendingWarning) {
		t.Fatalf("failed reload discarded the completed update warning: %v", m.warnings)
	}
	if strings.Contains(m.toast, "Update complete") {
		t.Fatalf("failed reload claimed success: %q", m.toast)
	}
}

// TestToastStackRendersTopRightLayer proves up to three toasts are visible
// simultaneously as a top-right Lip Gloss layer at both supported sizes,
// without covering footer controls.
func TestToastStackRendersTopRightLayer(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {50, 12}} {
		m := progressTestModel(t)
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		for _, text := range []string{"alpha-note", "beta-note", "gamma-note"} {
			next, _ = m.Update(toastMsg{text: text, duration: time.Minute})
			m = next.(Model)
		}
		view := m.viewContent()
		for _, text := range []string{"alpha-note", "beta-note", "gamma-note"} {
			if !strings.Contains(view, text) {
				t.Fatalf("%dx%d view hides stacked toast %q:\n%s", size[0], size[1], text, view)
			}
		}
		if !strings.Contains(view, "quit") {
			t.Fatalf("%dx%d toast layer obscured footer controls:\n%s", size[0], size[1], view)
		}
	}
}

// TestToastStackExpiresOnTick proves stacked toasts expire independently on
// tea.Tick: the expired entry leaves the visible stack while newer entries
// remain and re-arm their own expiry.
func TestToastStackExpiresOnTick(t *testing.T) {
	m := progressTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	for _, text := range []string{"first", "second", "third"} {
		next, _ = m.Update(toastMsg{text: text, duration: time.Minute})
		m = next.(Model)
	}
	if len(m.toastQueue) != toastStackMax {
		t.Fatalf("visible stack = %d toasts, want %d", len(m.toastQueue), toastStackMax)
	}
	m.toastQueue[0].exp = time.Now().Add(-time.Second)
	next, cmd := m.Update(toastExpiredMsg{})
	m = next.(Model)
	view := m.viewContent()
	if strings.Contains(view, "first") {
		t.Fatalf("expired toast stayed visible:\n%s", view)
	}
	if !strings.Contains(view, "second") || !strings.Contains(view, "third") {
		t.Fatalf("live toasts were dropped with the expired one:\n%s", view)
	}
	if cmd == nil {
		t.Fatal("remaining toasts were not re-armed for expiry")
	}
}
