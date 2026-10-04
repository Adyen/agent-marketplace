package backup

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkRestoreCodexSetGenerationDisk(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	home := b.TempDir()
	b.Setenv("HOME", home)
	root := filepath.Join(home, ".agents")
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		b.Fatal(err)
	}
	native := filepath.Join(root, "skills", "entry")
	archived := bytes.Repeat([]byte("inert archived payload\n"), 256)
	if err := os.WriteFile(native, archived, 0o640); err != nil {
		b.Fatal(err)
	}
	partitions := []CodexPartition{{ID: "machine-agents", Root: root, RelPaths: []string{"skills"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
	operation, err := CreateCodexSet(partitions)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		if err := os.WriteFile(native, []byte("inert current payload"), 0o640); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if restored, err := RestoreCodexSet(partitions); err != nil || restored != operation {
			b.Fatalf("real two-partition restore: %q %v", restored, err)
		}
		b.StopTimer()
		if raw, err := os.ReadFile(native); err != nil || !bytes.Equal(raw, archived) {
			b.Fatalf("actual archived payload absent: %v", err)
		}
		b.StartTimer()
	}
}
