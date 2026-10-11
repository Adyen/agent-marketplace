package backup

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkCreateCodexSetDisk(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	home := b.TempDir()
	b.Setenv("HOME", home)
	partitions := []CodexPartition{{ID: "machine-agents", Root: filepath.Join(home, ".agents")}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
	dir := filepath.Join(home, ".agent-marketplace", "backups", "codex")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := CreateCodexSet(partitions); err != nil {
			b.Fatal(err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			b.Fatal(err)
		}
		for _, entry := range entries {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				b.Fatal(err)
			}
		}
	}
}
