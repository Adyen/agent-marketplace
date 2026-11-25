package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

// healthyAuthoritativeSnapshot is the accepted final-stage reload that ends
// a sync operation successfully.
func healthyAuthoritativeSnapshot(generation requestGeneration) snapshotLoadedMsg {
	return snapshotLoadedMsg{
		generation:      generation,
		mergedCfg:       &config.Config{},
		driftAssessment: driftReady,
	}
}

func beginSyncOperation(t *testing.T) Model {
	t.Helper()
	m := progressTestModel(t)
	m.syncing = true
	m.beginOperationProgress("sync", m.requests.Active(requestSnapshot))
	return m
}

// TestSyncDoneKeepsOperationActiveThroughAuthoritativeReload proves pipeline
// completion alone cannot claim success: syncing, phase observation, and the
// reload phase stay active and no "Sync complete" toast appears until the
// accepted healthy snapshot ends the operation.
// spec: session-feedback/phase-observed-before-completion
func TestSyncDoneKeepsOperationActiveThroughAuthoritativeReload(t *testing.T) {
	m := beginSyncOperation(t)

	next, cmd := m.Update(syncDoneMsg{err: nil})
	m = next.(Model)
	if !m.syncing {
		t.Fatal("pipeline completion finished the sync before its authoritative reload")
	}
	if m.progress == nil {
		t.Fatal("pipeline completion cleared phase observation before reload")
	}
	if m.operationPhase != string(pipeline.PhaseReload) {
		t.Fatalf("active sync phase = %q, want %q during reload", m.operationPhase, pipeline.PhaseReload)
	}
	reload := m.requests.Active(requestSnapshot)
	if reload == 0 {
		t.Fatal("pipeline completion did not schedule an authoritative reload")
	}
	if strings.Contains(m.toast, "Sync complete") {
		t.Fatalf("pipeline completion claimed success before reload: %q", m.toast)
	}
	if cmd == nil {
		t.Fatal("pipeline completion did not emit the reload command")
	}

	next, _ = m.Update(healthyAuthoritativeSnapshot(reload))
	m = next.(Model)
	if m.syncing || m.progress != nil {
		t.Fatalf("accepted healthy reload did not finish the sync: syncing %t progress %v", m.syncing, m.progress)
	}
	if !strings.Contains(m.toast, "Sync complete") {
		t.Fatalf("accepted healthy reload did not report success: %q", m.toast)
	}
	if len(m.errs) != 0 {
		t.Fatalf("accepted healthy reload recorded diagnostics: %v", m.errs)
	}
}

// TestSyncReloadFailureEndsSyncAsFailure covers the failed reload branch: a
// successful pipeline whose authoritative reload cannot be verified ends the
// operation as an operationSync failure with diagnostics, never "Sync
// complete".
func TestSyncReloadFailureEndsSyncAsFailure(t *testing.T) {
	m := beginSyncOperation(t)

	next, _ := m.Update(syncDoneMsg{err: nil})
	m = next.(Model)
	reload := m.requests.Active(requestSnapshot)
	if reload == 0 {
		t.Fatal("pipeline completion did not schedule an authoritative reload")
	}

	m.errs = []error{errors.New("reload failed")}
	next, _ = m.Update(snapshotLoadedMsg{generation: reload})
	m = next.(Model)
	if m.syncing || m.progress != nil {
		t.Fatalf("failed reload left the sync running: syncing %t progress %v", m.syncing, m.progress)
	}
	if len(m.errs) == 0 {
		t.Fatal("failed reload dropped the diagnostic record")
	}
	failed := false
	for _, d := range m.diagnostics.Operations {
		if d.Operation == operationSync && d.Severity == diagnosticError {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("failed reload did not record an operationSync failure: %+v", m.diagnostics.Operations)
	}
	if strings.Contains(m.toast, "Sync complete") {
		t.Fatalf("failed reload claimed success: %q", m.toast)
	}
}

// TestSyncReloadSupersededKeepsOperationActiveThroughLatestSnapshot proves a
// sync whose own reload generation is superseded stays active: the stale
// delivery is rejected and only the accepted latest snapshot ends the
// operation.
func TestSyncReloadSupersededKeepsOperationActiveThroughLatestSnapshot(t *testing.T) {
	m := beginSyncOperation(t)

	next, _ := m.Update(syncDoneMsg{err: nil})
	m = next.(Model)
	staleReload := m.requests.Active(requestSnapshot)
	if staleReload == 0 {
		t.Fatal("pipeline completion did not schedule an authoritative reload")
	}

	// A newer snapshot request supersedes the sync reload before it lands.
	latest := m.beginRequest(requestSnapshot)
	next, _ = m.Update(healthyAuthoritativeSnapshot(staleReload))
	m = next.(Model)
	if !m.syncing || m.progress == nil {
		t.Fatal("stale sync reload finished the operation")
	}
	if strings.Contains(m.toast, "Sync complete") {
		t.Fatalf("stale sync reload claimed success: %q", m.toast)
	}

	next, _ = m.Update(healthyAuthoritativeSnapshot(latest))
	m = next.(Model)
	if m.syncing || m.progress != nil {
		t.Fatalf("accepted latest snapshot did not finish the sync: syncing %t progress %v", m.syncing, m.progress)
	}
	if !strings.Contains(m.toast, "Sync complete") {
		t.Fatalf("accepted latest snapshot did not report success: %q", m.toast)
	}
}

// TestSyncPipelineErrorPreservedThroughReload proves a failed pipeline is
// never turned into success by a healthy authoritative reload: the sync's own
// error wins the verdict.
func TestSyncPipelineErrorPreservedThroughReload(t *testing.T) {
	m := beginSyncOperation(t)
	syncErr := errors.New("materialization failed")

	next, _ := m.Update(syncDoneMsg{err: syncErr})
	m = next.(Model)
	if !m.syncing || m.progress == nil {
		t.Fatal("pipeline failure finished the sync before its authoritative reload")
	}
	reload := m.requests.Active(requestSnapshot)
	if reload == 0 {
		t.Fatal("pipeline failure did not schedule an authoritative reload")
	}
	if strings.Contains(m.toast, "Sync complete") {
		t.Fatalf("pipeline failure claimed success before reload: %q", m.toast)
	}

	next, _ = m.Update(healthyAuthoritativeSnapshot(reload))
	m = next.(Model)
	if m.syncing || m.progress != nil {
		t.Fatalf("accepted reload did not finish the failed sync: syncing %t progress %v", m.syncing, m.progress)
	}
	if len(m.errs) == 0 || !errors.Is(m.errs[len(m.errs)-1], syncErr) {
		t.Fatalf("healthy reload erased the pipeline failure: %v", m.errs)
	}
	if strings.Contains(m.toast, "Sync complete") {
		t.Fatalf("failed sync claimed success after reload: %q", m.toast)
	}
	if !strings.Contains(m.toast, "sync error") {
		t.Fatalf("failed sync did not surface its error: %q", m.toast)
	}
}

// TestSyncQuitStillWaitsForCompletionWithoutFakeSuccess proves sync+quit
// still quits at pipeline completion without recording an on-screen success
// claim the absent reload could never verify.
func TestSyncQuitStillWaitsForCompletionWithoutFakeSuccess(t *testing.T) {
	m := beginSyncOperation(t)
	m.quitAfterSync = true

	next, cmd := m.Update(syncDoneMsg{err: nil})
	m = next.(Model)
	if m.syncing {
		t.Fatal("sync+quit left the operation flagged after pipeline completion")
	}
	if m.quitAfterSync {
		t.Fatal("sync+quit did not consume quitAfterSync")
	}
	if !cmdProducesQuitMsg(cmd) {
		t.Fatal("sync+quit did not emit tea.Quit at pipeline completion")
	}
	if strings.Contains(m.toast, "Sync complete") {
		t.Fatalf("sync+quit claimed an unverifiable success: %q", m.toast)
	}
}

// TestSyncQuitConfirmedDuringReloadQuitsAtAuthority proves a sync+quit
// confirmed while the authoritative reload is already in flight still quits
// when that reload is accepted, carrying the verdict instead of a toast.
func TestSyncQuitConfirmedDuringReloadQuitsAtAuthority(t *testing.T) {
	m := beginSyncOperation(t)

	next, _ := m.Update(syncDoneMsg{err: nil})
	m = next.(Model)
	reload := m.requests.Active(requestSnapshot)
	if reload == 0 {
		t.Fatal("pipeline completion did not schedule an authoritative reload")
	}

	// The confirm answer lands while the reload is in flight.
	m.quitAfterSync = true
	next, cmd := m.Update(healthyAuthoritativeSnapshot(reload))
	m = next.(Model)
	if m.syncing || m.quitAfterSync {
		t.Fatalf("accepted reload did not finish sync+quit: syncing %t quitAfterSync %t", m.syncing, m.quitAfterSync)
	}
	quit := false
	for _, c := range batchCmds(cmd) {
		if cmdProducesQuitMsg(c) {
			quit = true
		}
	}
	if !quit && !cmdProducesQuitMsg(cmd) {
		t.Fatal("accepted reload did not emit tea.Quit for sync+quit")
	}
	if strings.Contains(m.toast, "Sync complete") {
		t.Fatalf("sync+quit claimed an unverifiable success: %q", m.toast)
	}
}
