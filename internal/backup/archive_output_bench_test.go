package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkArchiveOutputDisk(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "backup.tar.gz")
	data := []byte("inert archive output bytes")
	b.ReportAllocs()
	for b.Loop() {
		if err := withArchiveOutput(path, func(file *os.File) error { _, err := file.Write(data); return err }); err != nil {
			b.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			b.Fatal(err)
		}
	}
}
