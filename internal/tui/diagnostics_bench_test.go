package tui

import (
	"fmt"
	"testing"
)

func BenchmarkDiagnosticSnapshotReplace(b *testing.B) {
	diagnostics := make([]tuiDiagnostic, 32)
	for i := range diagnostics {
		diagnostics[i] = snapshotDiagnostic(diagnosticWarning, diagnosticCatalog, fmt.Errorf("catalog warning %d", i))
	}
	var store diagnosticStore
	b.ReportAllocs()
	for b.Loop() {
		store.replaceSnapshot(diagnostics)
	}
}
