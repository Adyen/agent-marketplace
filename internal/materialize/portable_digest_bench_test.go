package materialize

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkPortableTreeOutputDigestDisk(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 16; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("payload-%02d.md", i)), []byte(strings.Repeat("inert native output\n", 128)), 0o640); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := portablePathDigest(root); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPortableRegularOutputDigestDisk(b *testing.B) {
	path := filepath.Join(b.TempDir(), "output.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("inert native output\n", 128)), 0o640); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := portablePathDigest(path); err != nil {
			b.Fatal(err)
		}
	}
}
