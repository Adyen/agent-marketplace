package backup

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
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

func withBackupPayload(path string, read func(io.Reader) error) (err error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	root, err := fsboundary.OpenDir(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	file, err := fsboundary.CaptureDestination(root, filepath.Base(absolute), fsboundary.LinkPolicyRepository)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if file.Link() != nil {
		return fmt.Errorf("%w: backup archive %s is linked", fsboundary.ErrUnsafeFile, absolute)
	}
	return file.ReadTargetWith(read)
}
