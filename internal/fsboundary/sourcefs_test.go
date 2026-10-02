package fsboundary

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceFSKeepsCapturedRootAndDirectoryInfo(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("captured inert data"), 0o640); err != nil {
		t.Fatal(err)
	}
	source, err := OpenSourceFS(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	entries, err := fs.ReadDir(source, ".")
	if err != nil || len(entries) != 1 {
		t.Fatalf("captured directory control failed: %v %v", entries, err)
	}
	if info, err := entries[0].Info(); err != nil || info.Name() != "data.txt" {
		t.Fatalf("directory info outlived captured owner: %v %v", info, err)
	}
	retained := root + "-retained"
	if err := os.Rename(root, retained); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Rename(retained, root); err != nil {
			t.Errorf("restore fixture root: %v", err)
		}
	})
	data, err := fs.ReadFile(source, "data.txt")
	if err != nil || string(data) != "captured inert data" {
		t.Fatalf("renamed source root redirected read: %q %v", data, err)
	}
	if _, err := fs.ReadFile(source, "../data.txt"); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("invalid FS path did not refuse: %v", err)
	}
}
