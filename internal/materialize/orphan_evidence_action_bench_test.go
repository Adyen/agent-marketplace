package materialize

import (
	"context"
	"os"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/ownership"
)

func BenchmarkOrphanRemovalEvidenceDisk(b *testing.B) {
	b.StopTimer()
	repo, selection := benchRepairFixture(b, 1)
	store, err := ownership.Open(devinProjectRoot(repo))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.StartTimer()
	for b.Loop() {
		b.StopTimer()
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
		b.StopTimer()
		if _, err := Run(context.Background(), repo, selection); err != nil {
			b.Fatal(err)
		}
		if err := os.Remove(store.StateFile()); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}
