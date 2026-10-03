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
				var count int
				var err error
				if captured {
					var entries []pruneDirectoryEntry
					entries, err = readPruneDirectory(dir, nil, nil)
					count = len(entries)
				} else {
					var entries []os.DirEntry
					entries, err = os.ReadDir(dir)
					count = len(entries)
				}
				if err != nil || count != 3 {
					b.Fatalf("directory control: count=%d %v", count, err)
				}
			}
		})
	}
}
