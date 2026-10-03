package materialize

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkNativeCommandRemovalDisk(b *testing.B) {
	for _, guarded := range []bool{false, true} {
		name := "legacy"
		if guarded {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			leaf := filepath.Join(b.TempDir(), "command.md")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				b.StopTimer()
				if err := os.WriteFile(leaf, []byte("---\n_managed_by: agent-marketplace\n---\n\ninert body"), 0o644); err != nil {
					b.Fatal(err)
				}
				info, err := os.Lstat(leaf)
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if guarded {
					err = removeNativeCommandFile(leaf, info, nil)
				} else {
					err = os.Remove(leaf)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
