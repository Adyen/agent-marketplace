package fsboundary

import (
	"os"
	"testing"
)

// tempRoot captures a fresh temporary directory as an authorized root.
func tempRoot(t *testing.T) (*Dir, string) {
	t.Helper()
	return captureRoot(t, t.TempDir())
}

func captureRoot(t *testing.T, path string) (*Dir, string) {
	t.Helper()
	root, err := OpenDir(path)
	if err != nil {
		t.Fatalf("OpenDir(%q): %v", path, err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root, root.Path()
}

func mustWrite(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func statOf(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink %s -> %s: %v", link, target, err)
	}
}
