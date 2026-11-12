package tui

import (
	"fmt"
	"github.com/Adyen/agent-marketplace/internal/session"
	"testing"
)

func BenchmarkDiagnosticSnapshotReplace(b *testing.B) {
	diagnostics := make([]tuiDiagnostic, 32)
	for i := range diagnostics {
		diagnostics[i] = snapshotDiagnostic(diagnosticWarning, session.DiagnosticCatalog, fmt.Errorf("catalog warning %d", i))
	}
	var store diagnosticStore
	b.ReportAllocs()
	for b.Loop() {
		store.ReplaceSnapshot(diagnostics)
	}
}
