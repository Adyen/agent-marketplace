package backup

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkRestoreCodexNamespaceColdDisk(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	home, parent, codex := b.TempDir(), b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	agents := filepath.Join(parent, "native")
	if err := os.Mkdir(agents, 0o750); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "agents.txt"), []byte("inert restore namespace agents"), 0o640); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codex, "config.txt"), []byte("inert restore namespace codex"), 0o600); err != nil {
		b.Fatal(err)
	}
	partitions := []CodexPartition{{ID: "machine-agents", Root: agents, RelPaths: []string{"agents.txt"}}, {ID: "machine-codex", Root: codex, RelPaths: []string{"config.txt"}}}
	operation, err := CreateCodexSet(partitions)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := os.RemoveAll(agents); err != nil {
			b.Fatal(err)
		}
		if restored, err := RestoreCodexSet(partitions); err != nil || restored != operation {
			b.Fatalf("actual cold namespace RestoreCodexSet: %q %v", restored, err)
		}
	}
}
