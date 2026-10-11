package ownership

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkOwnershipMetadataOpenDisk(b *testing.B) {
	base := b.TempDir()
	target := filepath.Join(base, "target")
	state := filepath.Join(base, "state")
	for _, dir := range []string{target, state} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Open(target, WithStateRoot(state)); err != nil {
			b.Fatal(err)
		}
	}
}
