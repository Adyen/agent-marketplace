package backup

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func BenchmarkOrdinaryRestoreSelectionDisk(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	home, root := b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(root, "native.txt"), []byte("inert actual restore selection benchmark"), 0o640); err != nil {
		b.Fatal(err)
	}
	if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"native.txt"}}}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if restored, err := Restore(hcc.TargetFactory, root); err != nil || restored == "" {
			b.Fatalf("actual restore selection: %q %v", restored, err)
		}
	}
}
