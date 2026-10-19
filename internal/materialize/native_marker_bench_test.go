package materialize

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkNativeOwnershipMarkerReadDisk(b *testing.B) {
	leaf := filepath.Join(b.TempDir(), "command.md")
	if err := os.WriteFile(leaf, []byte("---\n_managed_by: agent-marketplace\n---\n\ninert native body"), 0o640); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if !isManagedCommandFile(leaf) {
			b.Fatal("healthy marker control refused")
		}
	}
}
