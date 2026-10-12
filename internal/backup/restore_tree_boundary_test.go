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
	type restoreRootCase struct {
		kind        byte
		replacement string
		supplied    bool
	}
	var cases []restoreRootCase
	for _, kind := range []byte{tar.TypeDir, tar.TypeSymlink, tar.TypeReg} {
		for _, replacement := range []string{"alias", "directory"} {
			for _, supplied := range []bool{false, true} {
				cases = append(cases, restoreRootCase{kind: kind, replacement: replacement, supplied: supplied})
			}
		}
	}
	for _, tc := range cases {
		kind := tc.kind
		name := map[byte]string{tar.TypeDir: "directory", tar.TypeSymlink: "symbolic-leaf", tar.TypeReg: "regular-file"}[kind] + "/" + tc.replacement + map[bool]string{false: "/opened-root", true: "/supplied-root"}[tc.supplied]
		t.Run(name, func(t *testing.T) {
			root, foreign := t.TempDir(), t.TempDir()
			retained := filepath.Join(t.TempDir(), "retained-root")
			var data bytes.Buffer
			gzipWriter := gzip.NewWriter(&data)
			tarWriter := tar.NewWriter(gzipWriter)
			payload := []byte("inert archived regular payload")
			header := &tar.Header{Name: "child", Typeflag: kind, Mode: 0o755, Linkname: "inert-target"}
			if kind == tar.TypeReg {
				header.Mode, header.Linkname, header.Size = 0o640, "", int64(len(payload))
			}
			if err := tarWriter.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			if kind == tar.TypeReg {
				if _, err := tarWriter.Write(payload); err != nil {
					t.Fatal(err)
				}
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
				if tc.replacement == "directory" {
					if err := os.Mkdir(root, 0o755); err != nil {
						t.Fatal(err)
					}
					return
				}
				if err := os.Symlink(foreign, root); err != nil {
					t.Fatal(err)
				}
			}}
			var captured *fsboundary.Dir
			if tc.supplied {
				var err error
				if captured, err = fsboundary.OpenDir(root); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = captured.Close() })
			}
			if err := extractArchivePayload(reader, root, captured); !errors.Is(err, fsboundary.ErrGenerationConflict) {
				t.Fatalf("retargeted tree root accepted: %v", err)
			}
			if entries, err := os.ReadDir(foreign); err != nil || len(entries) != 0 {
				t.Fatalf("restore mutated foreign tree: %v %v", entries, err)
			}
			if entries, err := os.ReadDir(retained); err != nil || len(entries) != 0 {
				t.Fatalf("refused restore published into the detached original root: %v %v", entries, err)
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
				t.Fatalf("refused restore mutated the replacement root: %v %v", entries, err)
			}
			if err := extractArchivePayload(bytes.NewReader(data.Bytes()), retained, nil); err != nil {
				t.Fatalf("valid tree restore refused: %v", err)
			}
			if info, err := os.Lstat(filepath.Join(retained, "child")); err != nil || kind == tar.TypeDir && !info.IsDir() || kind == tar.TypeSymlink && info.Mode()&os.ModeSymlink == 0 || kind == tar.TypeReg && !info.Mode().IsRegular() {
				t.Fatalf("valid tree output absent: %v", err)
			}
			if kind == tar.TypeSymlink {
				if text, err := os.Readlink(filepath.Join(retained, "child")); err != nil || text != "inert-target" {
					t.Fatalf("symbolic restore target changed: %q %v", text, err)
				}
			}
			if kind == tar.TypeReg {
				if data, err := os.ReadFile(filepath.Join(retained, "child")); err != nil || !bytes.Equal(data, payload) {
					t.Fatalf("regular restore payload changed: %q %v", data, err)
				}
			}
		})
	}
}
