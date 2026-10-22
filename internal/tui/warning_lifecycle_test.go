package tui

import (
	"errors"
	"testing"
)

// An operation that repeats a snapshot warning does not double-count it, and
// once an accepted snapshot stops reporting that warning its operation copy
// is retired too, while operation warnings the snapshot never reported stay
// until a later operation supersedes them.
// spec: tui-error-recovery/operation-copies-of-snapshot-warnings-follow-the-snapshot
func TestOperationCopiesOfSnapshotWarningsFollowTheSnapshot(t *testing.T) {
	missing := errors.New(`skill "gamma-skill" was selected but not found in file:///reg.git (skills)@main`)
	independent := errors.New("harness output could not be pruned")
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false

	m.replaceSnapshotDiagnostics([]tuiDiagnostic{snapshotDiagnostic(diagnosticWarning, diagnosticResolution, missing)}, nil, nil)
	m.recordOperationWarnings(operationSync, diagnosticSync, missing, independent)
	if len(m.warnings) != 2 {
		t.Fatalf("warnings after the sync = %v, want the snapshot warning once plus the independent one", m.warnings)
	}

	m.replaceSnapshotDiagnostics([]tuiDiagnostic{snapshotDiagnostic(diagnosticWarning, diagnosticResolution, missing)}, nil, nil)
	if len(m.warnings) != 2 {
		t.Fatalf("warnings after a snapshot that still reports the miss = %v, want 2", m.warnings)
	}

	m.replaceSnapshotDiagnostics(nil, nil, nil)
	if len(m.warnings) != 1 || m.warnings[0] != independent {
		t.Fatalf("warnings after the selection is removed = %v, want only the independent operation warning", m.warnings)
	}

	// A warning the snapshot never reported is not retired by a snapshot.
	m.recordOperationWarnings(operationSync, diagnosticSync, missing)
	m.replaceSnapshotDiagnostics(nil, nil, nil)
	if len(m.warnings) != 2 {
		t.Fatalf("warnings after an unrelated snapshot = %v, want both operation warnings kept", m.warnings)
	}
}
