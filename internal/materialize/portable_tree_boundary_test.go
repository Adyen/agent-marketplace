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
}

func (w *portableTreeBarrierWriter) Write(data []byte) (int, error) {
	if w.barrier != nil {
		barrier := w.barrier
		w.barrier = nil
		barrier()
	}
	return w.Writer.Write(data)
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
