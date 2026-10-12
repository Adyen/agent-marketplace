package pipeline

import "testing"

func BenchmarkBuildInventorySnapshotLegacy(b *testing.B) {
	result := inventoryResolveResult()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, omission, ok := BuildInventorySnapshot(result)
		if !ok || omission != "" {
			b.Fatalf("legacy inventory omitted: ok=%v reason=%q", ok, omission)
		}
	}
}
