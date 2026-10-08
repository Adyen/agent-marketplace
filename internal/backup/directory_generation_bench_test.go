package backup

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func BenchmarkCreateDirectoryGenerationDisk(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	home, root := b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	directory := filepath.Join(root, "skills", "nested")
	if err := os.MkdirAll(directory, 0o750); err != nil {
		b.Fatal(err)
	}
	for i := range 50 {
		if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("entry-%02d.txt", i)), []byte("inert directory archive payload\n"), 0o640); err != nil {
			b.Fatal(err)
		}
	}
	managed := map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"./skills"}}}
	archives := filepath.Join(home, ".agent-marketplace", "backups", "factory")
	b.ReportAllocs()
	for b.Loop() {
		if err := Create(managed); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		entries, err := os.ReadDir(archives)
		if err != nil {
			b.Fatal(err)
		}
		for _, entry := range entries {
			if err := os.Remove(filepath.Join(archives, entry.Name())); err != nil {
				b.Fatal(err)
			}
		}
		b.StartTimer()
	}
}
