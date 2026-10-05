package materialize

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkPortableEmptyDirectoryPruneDisk includes capture and removal disk I/O.
func BenchmarkPortableEmptyDirectoryPruneDisk(b *testing.B) {
	root := b.TempDir()
	directory := filepath.Join(root, "empty")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		if err := os.Mkdir(directory, 0o755); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := removeEmptyPortableDirectory(root, directory, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPortableEmptyPruneLexicalNormalization isolates lexical work only;
// descriptor capture and removal I/O are measured separately above.
func BenchmarkPortableEmptyPruneLexicalNormalization(b *testing.B) {
	root := b.TempDir()
	directory := filepath.Join(root, "empty")
	for _, normalized := range []bool{false, true} {
		name := "original"
		if normalized {
			name = "normalized"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				absoluteRoot, err := filepath.Abs(root) // Both implementations capture an absolute root.
				if err != nil {
					b.Fatal(err)
				}
				r, d := root, directory
				if normalized {
					r = absoluteRoot
					d, err = filepath.Abs(directory)
					if err != nil {
						b.Fatal(err)
					}
				}
				if rel, err := filepath.Rel(r, d); err != nil || rel != "empty" {
					b.Fatalf("lexical control: %q %v", rel, err)
				}
			}
		})
	}
}

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
