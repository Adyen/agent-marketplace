package tui

import "testing"

func BenchmarkPreviewSelectionInput(b *testing.B) {
	m := selectionDetailModel(b, "alpha beta gamma", "second selectable row", "third selectable row")
	snap := selSnap(m)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m, _ = selSend(b, m, snap, selClick(33, 0))
		m, _ = selSend(b, m, snap, selMotion(45, 1))
		m, _ = selSend(b, m, snap, selRelease(45, 1))
	}
}
