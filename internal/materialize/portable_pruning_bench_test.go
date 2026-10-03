package materialize

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkPortableRegularPruneDisk(b *testing.B) {
	sum := sha256.Sum256([]byte("file\x00\x000640\x004\x00body"))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	for _, guarded := range []bool{false, true} {
		name := "legacy"
		if guarded {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			root := b.TempDir()
			leaf := filepath.Join(root, "output.md")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				b.StopTimer()
				if err := os.WriteFile(leaf, []byte("body"), 0o640); err != nil {
					b.Fatal(err)
				}
				if err := os.Chmod(leaf, 0o640); err != nil {
					b.Fatal(err)
				}
				info, err := os.Lstat(leaf)
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if guarded {
					err = removePortableOutput(leaf, info, digest, nil)
				} else {
					err = os.RemoveAll(leaf)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
