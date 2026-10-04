package materialize

import (
	"context"
	"testing"
)

func BenchmarkOrphanRemovalEvidenceDisk(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		repo, _ := benchRepairFixture(b, 1)
		preview, err := DevinRepairPreview(context.Background(), repo, nil)
		if err != nil {
			b.Fatal(err)
		}
		var file *RepairFile
		for _, candidate := range preview.Files {
			if len(candidate.Orphans) == 1 {
				file = candidate
				break
			}
		}
		if file == nil {
			b.Fatal("actual orphan removal fixture absent")
		}
		b.StartTimer()
		if result, err := file.RemoveOrphan(0, true); err != nil || result.Generation == 0 || result.BackupPath == "" {
			b.Fatalf("actual backed-up orphan removal: %+v %v", result, err)
		}
	}
}
