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
