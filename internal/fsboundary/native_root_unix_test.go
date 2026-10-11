//go:build unix

package fsboundary

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeRootRetainsCapturedDirectoryAuthority(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "captured")
	mustMkdir(t, path)
	captured, _ := captureRoot(t, path)
	moved := filepath.Join(parent, "retained")
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, path)
	root, err := captured.NativeRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := root.Symlink("inert target", "stage"); err != nil {
		t.Fatal(err)
	}
	if err := root.Rename("stage", "published"); err != nil {
		t.Fatal(err)
	}
	if text, err := os.Readlink(filepath.Join(moved, "published")); err != nil || text != "inert target" {
		t.Fatalf("native root lost captured authority: %q %v", text, err)
	}
	if entries, err := os.ReadDir(path); err != nil || len(entries) != 0 {
		t.Fatalf("native operation reached replacement directory: %v %v", entries, err)
	}
	outside := t.TempDir()
	victim := filepath.Join(outside, "sentinel")
	mustWrite(t, victim, []byte("preserve external bytes"), 0o640)
	before := statOf(t, victim)
	if err := root.Mkdir("owned-tree", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := root.Symlink(outside, "owned-tree/external"); err != nil {
		t.Fatal(err)
	}
	if err := root.RemoveAll("owned-tree"); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(victim); err != nil || string(raw) != "preserve external bytes" {
		t.Fatalf("tree removal followed external link: %q %v", raw, err)
	}
	if after := statOf(t, victim); !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("tree removal changed external metadata")
	}
}
