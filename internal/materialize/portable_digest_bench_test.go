package materialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
