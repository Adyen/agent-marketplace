package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: backup-recovery/restore-tree-retarget-refuses-before-foreign-mutation
func TestRestoreTreeRefusesRetargetedRoot(t *testing.T) {
	for _, kind := range []byte{tar.TypeDir, tar.TypeSymlink} {
		t.Run(map[byte]string{tar.TypeDir: "directory", tar.TypeSymlink: "symbolic-leaf"}[kind], func(t *testing.T) {
			root, foreign := t.TempDir(), t.TempDir()
			retained := filepath.Join(t.TempDir(), "retained-root")
			var data bytes.Buffer
			gzipWriter := gzip.NewWriter(&data)
			tarWriter := tar.NewWriter(gzipWriter)
			header := &tar.Header{Name: "child", Typeflag: kind, Mode: 0o755, Linkname: "inert-target"}
			if err := tarWriter.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			if err := tarWriter.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gzipWriter.Close(); err != nil {
				t.Fatal(err)
			}
			reader := &restorePublicationReader{Reader: bytes.NewReader(data.Bytes()), before: func() {
				if err := os.Rename(root, retained); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreign, root); err != nil {
					t.Fatal(err)
				}
			}}
			if err := extractArchivePayload(reader, root, nil); !errors.Is(err, fsboundary.ErrGenerationConflict) {
				t.Fatalf("retargeted tree root accepted: %v", err)
			}
			if entries, err := os.ReadDir(foreign); err != nil || len(entries) != 0 {
				t.Fatalf("restore mutated foreign tree: %v %v", entries, err)
			}
			if err := extractArchivePayload(bytes.NewReader(data.Bytes()), retained, nil); err != nil {
				t.Fatalf("valid tree restore refused: %v", err)
			}
			if info, err := os.Lstat(filepath.Join(retained, "child")); err != nil || kind == tar.TypeDir && !info.IsDir() || kind == tar.TypeSymlink && info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("valid tree output absent: %v", err)
			}
			if kind == tar.TypeSymlink {
				if text, err := os.Readlink(filepath.Join(retained, "child")); err != nil || text != "inert-target" {
					t.Fatalf("symbolic restore target changed: %q %v", text, err)
				}
			}
		})
	}
}
