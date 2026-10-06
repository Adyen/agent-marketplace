package backup

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkBackupPayloadReadDisk(b *testing.B) {
	path := filepath.Join(b.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(path, bytes.Repeat([]byte("inert compressed-byte standin\n"), 1024), 0o600); err != nil {
		b.Fatal(err)
	}
	for _, captured := range []bool{false, true} {
		name := "legacy"
		if captured {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if captured {
					if err := withBackupPayload(path, func(reader io.Reader) error { _, err := io.Copy(io.Discard, reader); return err }); err != nil {
						b.Fatal(err)
					}
				} else {
					file, err := os.Open(path)
					if err != nil {
						b.Fatal(err)
					}
					_, err = io.Copy(io.Discard, file)
					closeErr := file.Close()
					if err != nil || closeErr != nil {
						b.Fatalf("legacy payload read: %v %v", err, closeErr)
					}
				}
			}
		})
	}
}
