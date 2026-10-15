package tui

import (
	"fmt"
	"testing"
)

func BenchmarkRenderErrorsRecovery(b *testing.B) {
	for _, n := range []int{1, 50, 200} {
		b.Run(fmt.Sprintf("diagnostics=%d", n), func(b *testing.B) {
			m := NewModel(b.TempDir(), b.TempDir())
			diagnostics := make([]tuiDiagnostic, 0, n)
			for i := range n {
				diagnostics = append(diagnostics, snapshotDiagnostic(diagnosticError, diagnosticCatalog, fmt.Errorf("source %d unavailable", i)))
			}
			m.replaceSnapshotDiagnostics(diagnostics, nil, nil)
			m.syncDiagnosticViews()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_ = renderErrors(m)
			}
		})
	}
}
