package materialize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
)

func BenchmarkNativeManagedLinkPruneDisk(b *testing.B) {
	b.Setenv("XDG_CACHE_HOME", b.TempDir())
	for _, guarded := range []bool{false, true} {
		name := "legacy"
		if guarded {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			root := b.TempDir()
			leaf := filepath.Join(root, "link")
			target := filepath.Join(cache.CacheDir(), "inert-target")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				b.StopTimer()
				if err := os.Symlink(target, leaf); err != nil {
					b.Fatal(err)
				}
				info, err := os.Lstat(leaf)
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if guarded {
					err = removePortableOutput(leaf, info, "", nil)
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
