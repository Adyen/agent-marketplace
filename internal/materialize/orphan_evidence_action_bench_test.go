package materialize

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkOrphanRemovalEvidenceDisk(b *testing.B) {
	discardBenchLogs(b)
	base := b.TempDir()
	// Register restoration once; each iteration then points the roots at its
	// own directory and removes it, so fixtures never accumulate on disk.
	b.Setenv("HOME", base)
	b.Setenv("XDG_STATE_HOME", base)
	b.Setenv("XDG_CACHE_HOME", base)
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		iteration, err := os.MkdirTemp(base, "iteration-")
		if err != nil {
			b.Fatal(err)
		}
		for env, dir := range map[string]string{"HOME": "home", "XDG_STATE_HOME": "state", "XDG_CACHE_HOME": "cache"} {
			if err := os.Setenv(env, filepath.Join(iteration, dir)); err != nil {
				b.Fatal(err)
			}
		}
		repo := filepath.Join(iteration, "repo")
		if err := os.Mkdir(repo, 0o755); err != nil {
			b.Fatal(err)
		}
		syncRepairFixture(b, repo, 1)
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
		if err := os.RemoveAll(iteration); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}
