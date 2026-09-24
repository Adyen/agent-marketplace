package tui

import "testing"

func BenchmarkRequestGeneration(b *testing.B) {
	var requests requestState
	b.ReportAllocs()
	for b.Loop() {
		generation := requests.begin(requestSnapshot)
		if generation != requests.active(requestSnapshot) {
			b.Fatal("request generation was not active")
		}
		requests.cancel(requestSnapshot)
	}
}

func BenchmarkApplySnapshotStatusWithoutPendingMutation(b *testing.B) {
	m := NewModel(b.TempDir(), b.TempDir())
	msg := snapshotLoadedMsg{generation: 1, driftAssessment: driftReady}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.applySnapshotStatus(msg)
	}
}
