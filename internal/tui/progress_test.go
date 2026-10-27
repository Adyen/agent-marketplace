package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
)

// phaseObservingLoaders lets a real pipeline.Resolve emit configure+resolve
// phase observations against an empty, fully-injected configuration.
func phaseObservingLoaders() pipeline.Loaders {
	return pipeline.Loaders{
		Org:    func() (*config.Config, error) { return &config.Config{}, nil },
		Global: func() (*config.Config, error) { return &config.Config{Harnesses: []string{"factory"}}, nil },
		Repo:   func() (*config.Config, error) { return &config.Config{}, nil },
		Local:  func() (*config.LocalConfig, error) { return &config.LocalConfig{}, nil },
	}
}

func progressTestModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	m.driftAssessment = driftFailed
	return m
}

// drainProgressCmds runs tea.Cmds to completion on goroutines and returns a
// channel that yields each produced message. The progress pump cmd blocks
// until a phase is emitted; the operation cmd blocks on the test gate.
func runCmdsAsync(cmds []tea.Cmd) chan tea.Msg {
	out := make(chan tea.Msg, len(cmds)+4)
	for _, c := range cmds {
		if c == nil {
			continue
		}
		go func(c tea.Cmd) {
			out <- c()
		}(c)
	}
	return out
}

// TestSyncPhaseVisibleBeforeCompletion proves a phase observation emitted by
// the real pipeline reaches the model while the sync command is still
// blocked — progress is visible BEFORE operation completion on the direct
// TUI sync path.
// spec: session-feedback/phase-observed-before-completion
func TestSyncPhaseVisibleBeforeCompletion(t *testing.T) {
	gate := make(chan struct{})
	workspace := defaultTUIWorkspace(t.TempDir())
	previousSync := session.RunPipelineSync
	session.RunPipelineSync = func(ctx context.Context, opts pipeline.ResolveOptions) pipeline.SyncResult {
		// Emit real pipeline phases through the observed context, then block
		// to simulate a long-running operation still in flight.
		pipeline.Resolve(ctx, pipeline.ResolveOptions{Workspace: workspace, Loaders: phaseObservingLoaders()})
		<-gate
		return pipeline.SyncResult{}
	}
	t.Cleanup(func() { session.RunPipelineSync = previousSync })

	m := progressTestModel(t)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = updated.(Model)
	if m.progress == nil {
		t.Fatal("sync did not open operation progress observation")
	}
	if !m.syncing {
		t.Fatal("sync did not start")
	}
	msgs := runCmdsAsync(batchCmds(cmd))

	var sawPhase bool
	deadline := time.After(5 * time.Second)
	for !sawPhase {
		select {
		case msg := <-msgs:
			if _, done := msg.(syncDoneMsg); done {
				t.Fatal("operation completed before a phase was observed")
			}
			progress, ok := msg.(operationProgressMsg)
			if !ok {
				continue
			}
			next, pump := m.Update(progress)
			m = next.(Model)
			if pump == nil {
				t.Fatal("accepted progress did not reschedule the observation pump")
			}
			go func() { msgs <- pump() }()
			if m.operationPhase == "" {
				t.Fatal("progress message did not set an active phase")
			}
			if !m.syncing || len(m.errs) > 0 {
				t.Fatalf("progress altered operation state: syncing %t errs %v", m.syncing, m.errs)
			}
			if footer := renderFooter(m); !strings.Contains(footer, m.operationPhase) {
				t.Fatalf("status bar hides active phase %q: %q", m.operationPhase, footer)
			}
			sawPhase = true
		case <-deadline:
			t.Fatal("no phase observation arrived while the operation was blocked")
		}
	}
	// The operation is still running; now allow completion and confirm the
	// completion message schedules the authoritative reload that finishes it.
	close(gate)
	deadline = time.After(5 * time.Second)
	for {
		select {
		case msg := <-msgs:
			if done, ok := msg.(syncDoneMsg); ok {
				next, _ := m.Update(done)
				m = next.(Model)
				if m.progress == nil || m.operationPhase != string(pipeline.PhaseReload) {
					t.Fatal("completion ended observation before the authoritative reload")
				}
				reload := m.requests.Active(requestSnapshot)
				if reload == 0 {
					t.Fatal("completion did not schedule an authoritative reload")
				}
				next, _ = m.Update(snapshotLoadedMsg{
					generation:      reload,
					mergedCfg:       &config.Config{},
					driftAssessment: driftReady,
				})
				m = next.(Model)
				if m.progress != nil || m.operationPhase != "" {
					t.Fatal("accepted reload did not clear progress observation")
				}
				return
			}
			if progress, ok := msg.(operationProgressMsg); ok {
				next, pump := m.Update(progress)
				m = next.(Model)
				if pump != nil {
					go func() { msgs <- pump() }()
				}
			}
		case <-deadline:
			t.Fatal("operation completion never arrived")
		}
	}
}

// TestInjectedLifecyclePhaseVisibleBeforeCompletion proves the same
// observation flows through the WithDependencyLifecycle adapter: the injected
// callback receives an observed context and its real pipeline calls report
// phases while the callback is still blocked.
// spec: session-feedback/phase-observed-before-completion
func TestInjectedLifecyclePhaseVisibleBeforeCompletion(t *testing.T) {
	gate := make(chan struct{})
	m := progressTestModel(t)
	m.dependencyLifecycle = DependencyLifecycle{
		Sync: func(ctx context.Context, workspace pipeline.Workspace) (pipeline.SyncResult, error) {
			pipeline.Resolve(ctx, pipeline.ResolveOptions{Workspace: workspace, Loaders: phaseObservingLoaders()})
			<-gate
			return pipeline.SyncResult{}, nil
		},
	}
	m.beginOperationProgress("sync", 0)
	m.syncing = true
	op := m.dependencySyncCmd()
	pump := m.operationProgressCmd()
	if op == nil || pump == nil {
		t.Fatal("observed lifecycle did not produce an operation and a progress pump")
	}
	msgs := runCmdsAsync([]tea.Cmd{op, pump})

	deadline := time.After(5 * time.Second)
	defer close(gate)
	for {
		select {
		case msg := <-msgs:
			if _, done := msg.(syncDoneMsg); done {
				t.Fatal("injected operation completed before a phase was observed")
			}
			progress, ok := msg.(operationProgressMsg)
			if !ok {
				continue
			}
			next, nextPump := m.Update(progress)
			m = next.(Model)
			if m.operationPhase == "" {
				t.Fatal("injected lifecycle phase did not reach the model")
			}
			if !m.syncing {
				t.Fatal("progress completed the injected operation")
			}
			if nextPump == nil {
				t.Fatal("progress pump was not rescheduled")
			}
			return
		case <-deadline:
			t.Fatal("no phase observation arrived from the injected lifecycle")
		}
	}
}

// TestUnobservedOperationShowsGenericRunning covers lifecycle paths that
// emit no phase observations: the status bar falls back to a generic running
// label rather than inventing phases.
// spec: session-feedback/no-observer
func TestUnobservedOperationShowsGenericRunning(t *testing.T) {
	m := progressTestModel(t)
	m.beginOperationProgress("sync", 0)
	m.syncing = true
	footer := renderFooter(m)
	if !strings.Contains(footer, "running") && !strings.Contains(footer, "syncing") {
		t.Fatalf("unobserved operation did not show generic running: %q", footer)
	}
	if strings.Contains(footer, string(pipeline.PhaseMaterialize)) {
		t.Fatalf("unobserved operation invented a phase: %q", footer)
	}
}

// TestProgressRejectsStaleDuplicateAndFinishedEvents keys progress to the
// active operation identity, applicable request generation, and a monotonic
// sequence: out-of-order, duplicate, foreign and post-finish observations
// must not alter phase, diagnostics, success, or serialization.
// spec: session-feedback/late-progress
func TestProgressRejectsStaleDuplicateAndFinishedEvents(t *testing.T) {
	m := progressTestModel(t)
	m.beginOperationProgress("sync", 7)

	foreign := operationProgressMsg{operation: m.progress.Operation() + 99, generation: 7, sequence: 1, phase: "x"}
	next, _ := m.Update(foreign)
	m = next.(Model)
	if m.operationPhase != "" {
		t.Fatal("foreign operation observation changed the active phase")
	}

	wrongGeneration := operationProgressMsg{operation: m.progress.Operation(), generation: 8, sequence: 1, phase: "x"}
	next, _ = m.Update(wrongGeneration)
	m = next.(Model)
	if m.operationPhase != "" {
		t.Fatal("wrong-generation observation changed the active phase")
	}

	first := operationProgressMsg{operation: m.progress.Operation(), generation: 7, sequence: 1, phase: "one"}
	next, _ = m.Update(first)
	m = next.(Model)
	if m.operationPhase != "one" {
		t.Fatalf("in-order observation was not accepted: %q", m.operationPhase)
	}

	for _, replay := range []operationProgressMsg{
		{operation: m.progress.Operation(), generation: 7, sequence: 1, phase: "stale"},
		{operation: m.progress.Operation(), generation: 7, sequence: 0, phase: "older"},
	} {
		next, _ = m.Update(replay)
		m = next.(Model)
		if m.operationPhase != "one" {
			t.Fatalf("out-of-order observation %v changed the active phase to %q", replay.sequence, m.operationPhase)
		}
	}
	if len(m.errs) > 0 || m.operations.Active != nil {
		t.Fatal("observations mutated diagnostics or operation serialization")
	}

	// A finished operation rejects every later observation.
	m.clearOperationProgress()
	late := operationProgressMsg{operation: 1, generation: 7, sequence: 2, phase: "late"}
	next, _ = m.Update(late)
	m = next.(Model)
	if m.operationPhase != "" {
		t.Fatal("post-finish observation resurrected a phase")
	}
}

// TestMarketplaceInstallConsentAndReloadPhases covers the Marketplace
// install-specific phases that are only reported when actually entered:
// consent when the consent pane opens, and authoritative reload while the
// post-install snapshot reload is pending.
func TestMarketplaceInstallConsentAndReloadPhases(t *testing.T) {
	m := progressTestModel(t)
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("install operation was not accepted")
	}
	m.beginOperationProgressFor(operation, "install", 0)

	// Consent path: a consent error opens the pane and must surface the
	// consent phase only because it was entered.
	next, _ := m.Update(discoveryInstallMsg{
		operation: operation,
		err:       &pipeline.MarketplaceConsentError{Err: errors.New("risk acceptance required"), Preview: "preview"},
	})
	m = next.(Model)
	if m.pane != paneMarketplaceConsent {
		t.Fatal("consent error did not open the consent pane")
	}
	if !strings.Contains(m.operationPhase, "consent") {
		t.Fatalf("entered consent did not surface a consent phase: %q", m.operationPhase)
	}

	// Decline and restart for the reload path.
	m.pane = paneList
	m.clearMarketplaceConsent()
	operation, ok = m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("second install operation was not accepted")
	}
	m.beginOperationProgressFor(operation, "install", 0)
	next, _ = m.Update(discoveryInstallMsg{operation: operation, result: &pipeline.InstallResult{}})
	m = next.(Model)
	if m.marketplaceInstallReload == 0 {
		t.Fatal("successful install did not schedule an authoritative reload")
	}
	if m.operationPhase != string(pipeline.PhaseReload) {
		t.Fatalf("authoritative reload phase = %q, want %q", m.operationPhase, pipeline.PhaseReload)
	}
	// The reload, not progress, completes the operation.
	if m.operations.Active == nil {
		t.Fatal("progress prematurely finished the install operation")
	}
}

// TestToastQueueBoundedAndExpires covers the bounded toast stack: at most
// three toasts are retained, expiry drops only the elapsed entries, and a
// replaced timer cannot clear a newer toast.
// spec: session-feedback/notification-overflow
func TestToastQueueBoundedAndExpires(t *testing.T) {
	m := progressTestModel(t)
	for i := 0; i < 5; i++ {
		next, _ := m.Update(toastMsg{text: fmt.Sprintf("toast-%d", i), duration: time.Minute})
		m = next.(Model)
	}
	if total := len(m.toastQueue); total != 3 {
		t.Fatalf("toast stack retained %d entries, want max 3", total)
	}
	if m.toast != "toast-4" {
		t.Fatalf("footer mirror = %q, want newest toast", m.toast)
	}

	// Expire the oldest toast; the survivors stay visible and re-arm.
	m.toastQueue[0].exp = time.Now().Add(-time.Second)
	next, cmd := m.Update(toastExpiredMsg{})
	m = next.(Model)
	if m.toast == "" || len(m.toastQueue) == 0 {
		t.Fatal("expiry dropped the live stack with the elapsed toast")
	}
	if cmd == nil {
		t.Fatal("surviving toasts were not given a fresh expiry timer")
	}

	// Expire everything; the stack must empty without a stale timer clearing
	// a later toast.
	m.toastExp = time.Now().Add(-time.Second)
	for i := range m.toastQueue {
		m.toastQueue[i].exp = time.Now().Add(-time.Second)
	}
	next, _ = m.Update(toastExpiredMsg{})
	m = next.(Model)
	if m.toast != "" || len(m.toastQueue) != 0 {
		t.Fatalf("expired toasts survived: current %q queued %d", m.toast, len(m.toastQueue))
	}
}

// TestStatusBarSegmentsAndKeyHints verifies the persistent segmented status
// keeps drift, Marketplace health, active operation and diagnostic counts
// readable while key hints stay separated.
func TestStatusBarSegmentsAndKeyHints(t *testing.T) {
	m := progressTestModel(t)
	m.width = 120
	m.driftAssessment = driftReady
	m.driftReport = materialize.DriftReport{Missing: []materialize.DriftItem{
		{Name: "one", Reason: materialize.DriftMissing},
		{Name: "two", Reason: materialize.DriftMissing},
	}}
	m.discoveryErrs = []error{errors.New("endpoint unreachable")}
	m.warnings = []error{errors.New("w")}
	m.beginOperationProgress("sync", 0)
	m.operationPhase = "materializing"
	m.syncing = true

	footer := renderFooter(m)
	for _, segment := range []string{"out of sync", "Marketplace", "materializing", "warnings"} {
		if !strings.Contains(footer, segment) {
			t.Fatalf("status bar lost the %q segment: %q", segment, footer)
		}
	}
	if !strings.Contains(footer, "quit") && !strings.Contains(footer, "help") {
		t.Fatalf("status bar lost separated key hints: %q", footer)
	}
}
