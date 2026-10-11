package backup

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func BenchmarkRestoreRegularPublicationDisk(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "restored.json")
	parent, err := fsboundary.OpenDir(dir)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = parent.Close() })
	data := bytes.Repeat([]byte("inert restored bytes\n"), 128)
	for _, captured := range []bool{false, true} {
		name := "legacy"
		if captured {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if captured {
					if err := publishRestoredFile(parent, "restored.json", bytes.NewReader(data), 0o640); err != nil {
						b.Fatal(err)
					}
				} else {
					file, err := os.Create(path)
					if err != nil {
						b.Fatal(err)
					}
					_, err = io.Copy(file, bytes.NewReader(data))
					closeErr := file.Close()
					if err != nil || closeErr != nil {
						b.Fatalf("legacy restore: %v %v", err, closeErr)
					}
					if err := os.Chmod(path, 0o640); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
