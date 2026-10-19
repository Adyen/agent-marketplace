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

func BenchmarkListBackupsGenerationDisk(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	home, root := b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(root, "native.txt"), []byte("inert listing benchmark archive"), 0o640); err != nil {
		b.Fatal(err)
	}
	if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"native.txt"}}}); err != nil {
		b.Fatal(err)
	}
	partitions := []CodexPartition{{ID: "machine-agents", Root: root, RelPaths: []string{"native.txt"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
	if _, err := CreateCodexSet(partitions); err != nil {
		b.Fatal(err)
	}
	directory := filepath.Join(home, ".agent-marketplace", "backups", "factory")
	for i := range 50 {
		if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("2000-01-01-000000-%03d.tar.gz", i)), []byte("inert filename-only listing metadata"), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		listed, err := ListBackups()
		if err != nil || len(listed[hcc.TargetFactory]) != 51 || len(listed[hcc.TargetCodex]) != 1 {
			b.Fatalf("actual ordinary/Codex listing: %v %v", listed, err)
		}
	}
}
