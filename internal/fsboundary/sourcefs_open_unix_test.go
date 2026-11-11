//go:build unix

package fsboundary

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceFSReadFileOpensEachPathComponentOnce(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("source payload")
	if err := os.WriteFile(filepath.Join(root, "a", "b", "c.md"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	source, err := OpenSourceFS(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })

	originalOpenat := openat
	calls := 0
	openat = func(dirfd int, path string, flags int, mode uint32) (int, error) {
		calls++
		return originalOpenat(dirfd, path, flags, mode)
	}
	t.Cleanup(func() { openat = originalOpenat })

	got, err := source.ReadFile("a/b/c.md")
	if err != nil || string(got) != string(want) {
		t.Fatalf("ReadFile returned %q, %v; want %q", got, err, want)
	}
	if calls != 3 {
		t.Fatalf("openat calls = %d, want 3", calls)
	}
}
