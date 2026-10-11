package backup

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func BenchmarkCreateOrdinaryNamespaceColdDisk(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	home, root := b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(root, "native.txt"), []byte("inert cold namespace benchmark archive"), 0o640); err != nil {
		b.Fatal(err)
	}
	managed := map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"native.txt"}}}
	b.ReportAllocs()
	for b.Loop() {
		if err := Create(managed); err != nil {
			b.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(home, ".agent-marketplace")); err != nil {
			b.Fatal(err)
		}
	}
}
