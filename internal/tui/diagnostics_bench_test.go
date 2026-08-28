package tui

import (
	"errors"
	"testing"
)

func BenchmarkDiagnosticSnapshotReplace(b *testing.B) {
	diagnostics := make([]tuiDiagnostic, 32)
	for i := range diagnostics {
		diagnostics[i] = snapshotDiagnostic(diagnosticWarning, diagnosticCatalog, errors.New("catalog warning"))
	}
	var store diagnosticStore
	b.ReportAllocs()
	for b.Loop() {
		store.replaceSnapshot(diagnostics)
	}
}
