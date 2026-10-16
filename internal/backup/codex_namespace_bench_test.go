package backup

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkCreateCodexNamespaceColdDisk(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	home, agents, codex := b.TempDir(), b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(agents, "agents.txt"), []byte("inert cold agents partition"), 0o640); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codex, "config.txt"), []byte("inert cold codex partition"), 0o600); err != nil {
		b.Fatal(err)
	}
	partitions := []CodexPartition{{ID: "machine-agents", Root: agents, RelPaths: []string{"agents.txt"}}, {ID: "machine-codex", Root: codex, RelPaths: []string{"config.txt"}}}
	b.ReportAllocs()
	for b.Loop() {
		if operation, err := CreateCodexSet(partitions); err != nil || operation == "" {
			b.Fatalf("genuine cold Codex namespace: %q %v", operation, err)
		}
		if err := os.RemoveAll(filepath.Join(home, ".agent-marketplace")); err != nil {
			b.Fatal(err)
		}
	}
}
