package backup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func BenchmarkRestoreSymlinkNamespaceDisk(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "restored")
	parent, err := fsboundary.OpenDir(dir)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = parent.Close() })
	for _, captured := range []bool{false, true} {
		name := "legacy"
		if captured {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if captured {
					err = restoreCapturedSymlink(parent, "restored", "inert-target")
				} else {
					err = os.RemoveAll(path)
					if err == nil {
						err = os.Symlink("inert-target", path)
					}
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
