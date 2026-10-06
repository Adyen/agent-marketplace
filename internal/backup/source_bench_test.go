package backup

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func BenchmarkBackupSourcePayloadDisk(b *testing.B) {
	dir := b.TempDir()
	file := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(file, bytes.Repeat([]byte("inert backup source\n"), 1024), 0o640); err != nil {
		b.Fatal(err)
	}
	info, err := os.Lstat(file)
	if err != nil {
		b.Fatal(err)
	}
	root, err := fsboundary.OpenDir(dir)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = root.Close() })
	for _, captured := range []bool{false, true} {
		name := "legacy"
		if captured {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if captured {
					if err := writeCapturedBackupSource(io.Discard, root, "SKILL.md", info); err != nil {
						b.Fatal(err)
					}
				} else {
					opened, err := os.Open(file)
					if err != nil {
						b.Fatal(err)
					}
					_, err = io.Copy(io.Discard, opened)
					closeErr := opened.Close()
					if err != nil || closeErr != nil {
						b.Fatalf("legacy backup source: %v %v", err, closeErr)
					}
				}
			}
		})
	}
}
