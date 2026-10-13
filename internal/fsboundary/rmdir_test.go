package fsboundary

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRmdirPreservesFilesLinksAndNonemptyDirectories(t *testing.T) {
	path := t.TempDir()
	if err := os.WriteFile(filepath.Join(path, "file"), []byte("inert user bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, "nonempty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "nonempty", "file"), []byte("nested user bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("empty", filepath.Join(path, "link")); err != nil {
		t.Fatal(err)
	}
	root, err := OpenDir(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	for _, name := range []string{"file", "nonempty", "link"} {
		if err := root.Rmdir(name); err == nil {
			t.Fatalf("kernel empty-directory removal accepted %s", name)
		}
		if _, err := os.Lstat(filepath.Join(path, name)); err != nil {
			t.Fatalf("kernel removal changed %s: %v", name, err)
		}
	}
	if err := root.Rmdir("empty"); err != nil {
		t.Fatalf("valid empty-directory control failed: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(path, "file")); err != nil || string(data) != "inert user bytes" {
		t.Fatalf("regular-file bytes changed: %q %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(path, "nonempty", "file")); err != nil || string(data) != "nested user bytes" {
		t.Fatalf("nested bytes changed: %q %v", data, err)
	}
}
