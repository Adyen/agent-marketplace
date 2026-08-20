package tui

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

func TestStructuredLifecycleLogsExcludeRawDiagnosticContent(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m)
	active := m.beginRequest(requestDetail)
	m.acceptsRequest(requestDetail, active-1)
	m.acceptsRequest(requestDetail, active)
	raw := "raw-untrusted-secret\x1b[31m"
	m.diagnostics.recordOperationFailure(operationSync, diagnosticSync, errors.New(raw))
	m.diagnostics.recordOperationSuccess(operationSync)

	logs := output.String()
	for _, event := range []string{
		"tui request started",
		"tui stale request completion rejected",
		"tui request completion accepted",
		"tui operation failed",
		"tui operation recovered",
	} {
		if !strings.Contains(logs, event) {
			t.Fatalf("structured logs missing %q:\n%s", event, logs)
		}
	}
	if strings.Contains(logs, raw) || strings.Contains(logs, "raw-untrusted-secret") {
		t.Fatalf("structured lifecycle logs exposed raw diagnostic content:\n%s", logs)
	}
}

func TestAcceptedRefreshPreservesWarningOnlyDiagnostics(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m)
	generation := m.beginRequest(requestSnapshot)
	warning := errors.New("actionable refresh warning")
	updated, _ := m.Update(refreshLoadedMsg{snapshotLoadedMsg: snapshotLoadedMsg{
		generation:  generation,
		mergedCfg:   &config.Config{},
		diagnostics: []tuiDiagnostic{snapshotDiagnostic(diagnosticWarning, diagnosticCatalog, warning)},
	}})
	m = updated.(Model)
	warnings := m.diagnostics.bySeverity(diagnosticWarning)
	if len(warnings) != 1 || !errors.Is(warnings[0].err, warning) {
		t.Fatalf("accepted warning-only refresh lost diagnostics: %v", warnings)
	}
}

func TestAcceptedSnapshotDoesNotEraseSyncFailure(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m)
	syncErr := errors.New("sync write failed")
	updated, _ := m.Update(syncDoneMsg{err: syncErr})
	m = updated.(Model)
	generation := m.requests.active(requestSnapshot)
	updated, _ = m.Update(snapshotLoadedMsg{generation: generation, mergedCfg: &config.Config{}})
	m = updated.(Model)
	errs := m.diagnostics.bySeverity(diagnosticError)
	if len(errs) != 1 || !errors.Is(errs[0].err, syncErr) {
		t.Fatalf("accepted snapshot erased retained sync failure: %v", errs)
	}
}

func TestOperationDiagnosticsRecoverByOperationOrAcknowledgement(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m)
	syncErr := errors.New("sync failed")
	updateErr := errors.New("update failed")
	updated, _ := m.Update(syncDoneMsg{err: syncErr})
	m = updated.(Model)
	// The update result carries the active snapshot generation so it is
	// accepted rather than rejected as stale.
	updated, _ = m.Update(updateDoneMsg{
		snapshot: snapshotLoadedMsg{generation: m.requests.active(requestSnapshot), mergedCfg: &config.Config{}},
		err:      updateErr,
	})
	m = updated.(Model)
	if got := m.diagnostics.count(diagnosticError); got != 2 {
		t.Fatalf("operation failures = %d, want 2", got)
	}

	updated, _ = m.Update(syncDoneMsg{})
	m = updated.(Model)
	errs := m.diagnostics.bySeverity(diagnosticError)
	if len(errs) != 1 || !errors.Is(errs[0].err, updateErr) {
		t.Fatalf("sync success cleared unrelated update failure: %v", errs)
	}

	m.diagnostics.replaceSnapshot([]tuiDiagnostic{snapshotDiagnostic(diagnosticWarning, diagnosticCatalog, errors.New("snapshot warning"))})
	m.pane = paneErrors
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(Model)
	if got := m.diagnostics.count(diagnosticError); got != 0 {
		t.Fatalf("acknowledgement retained %d operation errors", got)
	}
	if got := m.diagnostics.count(diagnosticWarning); got != 1 {
		t.Fatalf("acknowledgement cleared current snapshot diagnostics: %d", got)
	}
}

func TestIndependentOperationAttemptsAreNotCollapsed(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m)
	err := errors.New("same sync failure")
	updated, _ := m.Update(syncDoneMsg{err: err})
	m = updated.(Model)
	updated, _ = m.Update(syncDoneMsg{err: err})
	m = updated.(Model)
	if got := m.diagnostics.count(diagnosticError); got != 2 {
		t.Fatalf("independent sync attempts collapsed to %d diagnostics", got)
	}
}

func TestUpdateOutcomeCopyNamesRefreshAndMaterialization(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m)
	updated, _ := m.Update(updateDoneMsg{snapshot: snapshotLoadedMsg{
		generation:      m.requests.active(requestSnapshot),
		driftAssessment: driftReady,
		mergedCfg:       &config.Config{},
	}})
	m = updated.(Model)
	if m.toast != "Update refresh and materialization complete" {
		t.Fatalf("update success copy = %q", m.toast)
	}

	failure := errors.New("update materialization failed: write refused")
	updated, _ = m.Update(updateDoneMsg{
		snapshot: snapshotLoadedMsg{generation: m.requests.active(requestSnapshot), mergedCfg: &config.Config{}},
		stage:    updateStageMaterialization,
		err:      failure,
	})
	m = updated.(Model)
	if m.toast != failure.Error() || m.toast == "Update refresh and materialization complete" {
		t.Fatalf("update failure copy = %q", m.toast)
	}
}

type wrappedDiagnosticKeyError struct {
	key string
}

func (e wrappedDiagnosticKeyError) Error() string    { return "keyed diagnostic" }
func (e wrappedDiagnosticKeyError) DedupKey() string { return e.key }

func TestStableDiagnosticKeyUsesWrappedKeyer(t *testing.T) {
	const want = "stable-wrapped-key"
	err := fmt.Errorf("context: %w", wrappedDiagnosticKeyError{key: want})
	if got := stableDiagnosticKey(err); got != want {
		t.Fatalf("stableDiagnosticKey() = %q, want wrapped key %q", got, want)
	}
}

func TestReplaceSnapshotCopiesCallerSlice(t *testing.T) {
	originalErr := errors.New("original warning")
	caller := []tuiDiagnostic{{
		severity:   diagnosticWarning,
		provenance: diagnosticOperation,
		category:   diagnosticCatalog,
		operation:  operationSync,
		attempt:    9,
		err:        originalErr,
	}}
	var store diagnosticStore
	store.replaceSnapshot(caller)

	if caller[0].provenance != diagnosticOperation || caller[0].operation != operationSync || caller[0].attempt != 9 || caller[0].stableKey != "" {
		t.Fatalf("replaceSnapshot mutated caller element: %#v", caller[0])
	}
	caller[0] = snapshotDiagnostic(diagnosticError, diagnosticConfig, errors.New("replacement error"))
	if len(store.snapshot) != 1 || !errors.Is(store.snapshot[0].err, originalErr) {
		t.Fatalf("replaceSnapshot retained caller backing array: %#v", store.snapshot)
	}
}

func TestOperationSuccessLogsOnlyWhenItClearsDiagnostics(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	var store diagnosticStore
	store.recordOperationSuccess(operationSync)
	if strings.Contains(output.String(), "tui operation recovered") {
		t.Fatalf("success without retained diagnostics logged recovery:\n%s", output.String())
	}

	store.recordOperationFailure(operationSync, diagnosticSync, errors.New("sync failed"))
	output.Reset()
	store.recordOperationSuccess(operationSync)
	if !strings.Contains(output.String(), "tui operation recovered") {
		t.Fatalf("success that cleared diagnostics omitted recovery log:\n%s", output.String())
	}
}

func TestAcceptedSnapshotDeduplicatesStableDiagnosticIdentity(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m)
	warning := errors.New("same warning")
	updated, _ := m.Update(snapshotLoadedMsg{
		generation: m.requests.active(requestSnapshot),
		mergedCfg:  &config.Config{},
		diagnostics: []tuiDiagnostic{
			snapshotDiagnostic(diagnosticWarning, diagnosticCatalog, warning),
			snapshotDiagnostic(diagnosticWarning, diagnosticCatalog, warning),
		},
	})
	m = updated.(Model)
	warnings := m.diagnostics.bySeverity(diagnosticWarning)
	if len(warnings) != 1 {
		t.Fatalf("stable warning identity rendered %d times: %v", len(warnings), warnings)
	}
}
