package fsboundary

import (
	"errors"
	"os"
	"runtime"
	"syscall"
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

// mustSymlink creates a symbolic link. Windows allows it only with Developer
// Mode or administrator rights; a host without that privilege cannot exercise
// symlink behavior, so the test is skipped there. Link refusal is also proved
// with junctions, which need no privilege (see junction_windows_test.go).
func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if errors.Is(err, syscall.Errno(1314)) { // ERROR_PRIVILEGE_NOT_HELD
			t.Skipf("symbolic links need Developer Mode or administrator rights: %v", err)
		}
		t.Fatalf("symlink %s -> %s: %v", link, target, err)
	}
}

// skipWhereLinksAreNotHonored skips a test that exercises behavior built on
// honored symbolic links. Windows refuses every link instead (see
// openspec platform-support "Filesystem boundaries on Windows"); its refusals
// are proved by the Windows-specific tests.
func skipWhereLinksAreNotHonored(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links are not honored on Windows; link refusal is covered by the Windows boundary tests")
	}
}
