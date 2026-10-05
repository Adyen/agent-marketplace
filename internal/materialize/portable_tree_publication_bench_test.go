package materialize

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkPortableTreePublicationDisk includes publication disk I/O, not setup.
func BenchmarkPortableTreePublicationDisk(b *testing.B) {
	root := b.TempDir()
	src, dst := filepath.Join(root, "source"), filepath.Join(root, "output")
	if err := os.Mkdir(src, 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("inert skill content"), 0o640); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		if err := os.RemoveAll(dst); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := publishPortableTree(src, dst, nil, "", nil); err != nil {
			b.Fatal(err)
		}
	}
}
