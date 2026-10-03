package materialize

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkPruneDirectoryReadDisk(b *testing.B) {
	dir := b.TempDir()
	for _, name := range []string{"one.md", "two.md", "three.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("inert bytes"), 0o640); err != nil {
			b.Fatal(err)
		}
	}
	for _, captured := range []bool{false, true} {
		name := "legacy"
		if captured {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var entries []os.DirEntry
				var err error
				if captured {
					entries, err = readPruneDirectory(dir, nil)
				} else {
					entries, err = os.ReadDir(dir)
				}
				if err != nil || len(entries) != 3 {
					b.Fatalf("directory control: %v %v", entries, err)
				}
			}
		})
	}
}
