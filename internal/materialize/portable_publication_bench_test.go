package materialize

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkPortableRegularPublicationDisk(b *testing.B) {
	for _, guarded := range []bool{false, true} {
		name := "legacy"
		if guarded {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			root := b.TempDir()
			src, dst := filepath.Join(root, "source.md"), filepath.Join(root, "output.md")
			if err := os.WriteFile(src, []byte("inert compiled native output"), 0o640); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				b.StopTimer()
				if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
					b.Fatal(err)
				}
				b.StartTimer()
				var err error
				if guarded {
					err = publishPortableFile(src, dst, nil, nil)
				} else {
					err = copyFile(src, dst)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
