package materialize

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

type portableTreeBarrierWriter struct {
	io.Writer
	barrier func()
	after   []byte
}

func (w *portableTreeBarrierWriter) Write(data []byte) (int, error) {
	if w.after != nil {
		n, err := w.Writer.Write(data)
		if err == nil && n == len(data) && bytes.Equal(data, w.after) {
			w.fire()
		}
		return n, err
	}
	w.fire()
	return w.Writer.Write(data)
}

func (w *portableTreeBarrierWriter) fire() {
	if w.barrier != nil {
		barrier := w.barrier
		w.barrier = nil
		barrier()
	}
}

func TestPortableTreeDigestRetainsLegacyOrderingAndStructuralLinks(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "payload.md"), []byte("body"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "payload.md"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("payload.md", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	linkInfo, err := os.Lstat(filepath.Join(root, "alias"))
	if err != nil {
		t.Fatal(err)
	}
	linkMode := "0777"
	if linkInfo.Mode().Perm() == 0o755 {
		linkMode = "0755"
	}
	sum := sha256.Sum256([]byte("dir\x00\x000755\x00symlink\x00alias\x00" + linkMode + "\x0010\x00payload.mdfile\x00payload.md\x000640\x004\x00body"))
	want := "sha256:" + hex.EncodeToString(sum[:])
	if got, err := portablePathDigest(root); err != nil || got != want {
		t.Fatalf("portable tree legacy order/encoding changed: %q want %q err %v", got, want, err)
	}
}

// spec: materialization/portable-tree-digest-refuses-replaced-directory-authority
func TestPortableTreeDigestRefusesDirectoryReplacement(t *testing.T) {
	parent, outside := t.TempDir(), t.TempDir()
	tree := filepath.Join(parent, "tree")
	if err := os.Mkdir(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "payload.md"), []byte("original inert body"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := portablePathDigest(tree); err != nil {
		t.Fatalf("valid tree digest control failed: %v", err)
	}
	foreign := filepath.Join(outside, "payload.md")
	if err := os.WriteFile(foreign, []byte("foreign inert body"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(foreign)
	if err != nil {
		t.Fatal(err)
	}
	var hashed bytes.Buffer
	writer := &portableTreeBarrierWriter{Writer: &hashed, barrier: func() {
		if err := os.Rename(tree, filepath.Join(parent, "retained-tree")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, tree); err != nil {
			t.Fatal(err)
		}
	}}
	err = writePortableDigestPath(writer, tree, "")
	if !errors.Is(err, fsboundary.ErrGenerationConflict) && !errors.Is(err, fsboundary.ErrUnsafePath) {
		t.Fatalf("replaced directory supplied ownership bytes: %q %v", hashed.Bytes(), err)
	}
	if bytes.Contains(hashed.Bytes(), []byte("foreign inert body")) {
		t.Fatalf("foreign bytes entered ownership hash: %q", hashed.Bytes())
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "foreign inert body" {
		t.Fatalf("tree digest changed foreign bytes: %q %v", data, err)
	}
	if after, err := os.Stat(foreign); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("tree digest changed foreign metadata: %v", err)
	}
}

func TestPortableTreeDigestRefusesNestedPostDescendantChanges(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		name := "in-place-generation"
		if replacement {
			name = "replacement"
		}
		t.Run(name, func(t *testing.T) {
			parent, outside := t.TempDir(), t.TempDir()
			nested := filepath.Join(parent, "nested")
			if err := os.Mkdir(nested, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(nested, 0o755); err != nil {
				t.Fatal(err)
			}
			payload := filepath.Join(nested, "payload.md")
			if err := os.WriteFile(payload, []byte("body"), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(payload, 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("payload.md", filepath.Join(nested, "z-alias")); err != nil {
				t.Fatal(err)
			}
			linkInfo, err := os.Lstat(filepath.Join(nested, "z-alias"))
			if err != nil {
				t.Fatal(err)
			}
			linkMode := "0777"
			if linkInfo.Mode().Perm() == 0o755 {
				linkMode = "0755"
			}
			want := "dir\x00nested\x000755\x00file\x00nested/payload.md\x000640\x004\x00bodysymlink\x00nested/z-alias\x00" + linkMode + "\x0010\x00payload.md"
			root, err := fsboundary.OpenDir(parent)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = root.Close() })
			var control bytes.Buffer
			if err := writePortableDigestEntry(&control, root, "nested", "nested"); err != nil || control.String() != want {
				t.Fatalf("healthy nested digest differs from independent encoding: %q %v", control.Bytes(), err)
			}
			before, err := os.Stat(payload)
			if err != nil {
				t.Fatal(err)
			}
			foreign := filepath.Join(outside, "foreign.md")
			if err := os.WriteFile(foreign, []byte("foreign inert body"), 0o640); err != nil {
				t.Fatal(err)
			}
			foreignBefore, err := os.Stat(foreign)
			if err != nil {
				t.Fatal(err)
			}
			var hashed bytes.Buffer
			retained := nested
			writer := &portableTreeBarrierWriter{Writer: &hashed, after: []byte("10\x00payload.md"), barrier: func() {
				if replacement {
					retained = filepath.Join(parent, "retained")
					if err := os.Rename(nested, retained); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(outside, nested); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink(foreign, filepath.Join(nested, "foreign.md")); err != nil {
					t.Fatal(err)
				}
			}}
			err = writePortableDigestEntry(writer, root, "nested", "nested")
			if writer.barrier != nil || hashed.String() != want {
				t.Fatalf("post-descendant barrier did not consume exactly the captured subtree: %q", hashed.Bytes())
			}
			if !errors.Is(err, fsboundary.ErrGenerationConflict) {
				t.Fatalf("nested post-descendant change was accepted: %v", err)
			}
			if bytes.Contains(hashed.Bytes(), []byte("foreign inert body")) {
				t.Fatalf("foreign nested bytes reached ownership digest: %q", hashed.Bytes())
			}
			for _, check := range []struct {
				path string
				info os.FileInfo
				body string
			}{
				{filepath.Join(retained, "payload.md"), before, "body"},
				{foreign, foreignBefore, "foreign inert body"},
			} {
				if data, err := os.ReadFile(check.path); err != nil || string(data) != check.body {
					t.Fatalf("nested digest changed payload bytes: %q %v", data, err)
				}
				if after, err := os.Stat(check.path); err != nil || !os.SameFile(check.info, after) || check.info.Mode() != after.Mode() || !check.info.ModTime().Equal(after.ModTime()) {
					t.Fatalf("nested digest changed payload generation: %v", err)
				}
			}
		})
	}
}
