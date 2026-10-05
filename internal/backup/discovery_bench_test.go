package backup

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// BenchmarkArchiveSelectedDirectoryWalk includes captured disk metadata and
// payload reads and tar encoding, but not compression or archive publication.
// Fixtures are reused and tar output is discarded; there is no network I/O.
func BenchmarkArchiveSelectedDirectoryWalk(b *testing.B) {
	dir := b.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "skills", "nested"), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills", "nested", "reference.txt"), []byte("inert support"), 0o640); err != nil {
		b.Fatal(err)
	}
	root, err := fsboundary.OpenDir(dir)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = root.Close() })
	b.ReportAllocs()
	for b.Loop() {
		writer := tar.NewWriter(io.Discard)
		if err := archiveDir(writer, root, "skills/nested"); err != nil {
			b.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBackupSymbolicMetadataDisk(b *testing.B) {
	dir := b.TempDir()
	file := filepath.Join(dir, "entry")
	if err := os.Symlink("inert-relative-target", file); err != nil {
		b.Fatal(err)
	}
	root, err := fsboundary.OpenDir(dir)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = root.Close() })
	for _, captured := range []bool{false, true} {
		name := "legacy"
		if captured {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if captured {
					info, text, err := readBackupSourceMetadata(root, "entry")
					if err != nil || info == nil || text != "inert-relative-target" {
						b.Fatalf("captured metadata: %q %v", text, err)
					}
				} else {
					if _, err := os.Lstat(file); err != nil {
						b.Fatal(err)
					}
					if text, err := os.Readlink(file); err != nil || text != "inert-relative-target" {
						b.Fatalf("legacy metadata: %q %v", text, err)
					}
				}
			}
		})
	}
}
