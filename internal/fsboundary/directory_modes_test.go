package fsboundary

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestCapturedDirectoryMetadataModeOperations(t *testing.T) {
	root, path := tempRoot(t)
	stagePath := filepath.Join(path, "stage")
	if err := os.Mkdir(stagePath, 0o700); err != nil {
		t.Fatal(err)
	}
	stage, err := root.OpenPath("stage")
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.Chmod(0o500); err != nil {
		t.Fatal(err)
	}
	info, err := stage.Stat()
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o500 {
		t.Fatalf("opened read-only directory metadata: %v %v", info, err)
	}
	if err := os.Rename(stagePath, filepath.Join(path, "retained")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stagePath, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign, err := os.Stat(stagePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.Chmod(0o700); err != nil {
		t.Fatal(err)
	}
	if retained, err := stage.Stat(); err != nil || !os.SameFile(info, retained) || retained.Mode().Perm() != 0o700 {
		t.Fatalf("captured chmod lost object binding: %v %v", retained, err)
	}
	if current, err := os.Stat(stagePath); err != nil || !os.SameFile(foreign, current) || current.Mode() != foreign.Mode() || !current.ModTime().Equal(foreign.ModTime()) {
		t.Fatalf("descriptor chmod changed replacement: %v", err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := stage.Stat(); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("closed directory Stat lost cause: %v", err)
	}
	if err := stage.Chmod(0o755); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("closed directory Chmod lost cause: %v", err)
	}
}

func TestCapturedDirectoryModeAdmissionRefusesAliasAndHardLink(t *testing.T) {
	root, path := tempRoot(t)
	external := t.TempDir()
	file := filepath.Join(external, "user.md")
	mustWrite(t, file, []byte("foreign bytes"), 0o444)
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, external, filepath.Join(path, "alias"))
	if err := os.Link(file, filepath.Join(path, "hardlink")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alias", "hardlink"} {
		if captured, err := root.OpenPath(name); !errors.Is(err, ErrUnsafePath) || captured != nil {
			t.Fatalf("unsafe directory metadata admission for %s: %v", name, err)
		}
	}
	if after, err := os.Stat(file); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("directory admission changed external metadata: %v", err)
	}
	if mustRead(t, file) != "foreign bytes" {
		t.Fatal("directory admission changed external bytes")
	}
}
