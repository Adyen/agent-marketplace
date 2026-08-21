package materialize

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenConfinedFSRejectsSymlinkRoot(t *testing.T) {
	parent := t.TempDir()
	target := t.TempDir()
	rootPath := filepath.Join(parent, "output")
	if err := os.Symlink(target, rootPath); err != nil {
		t.Fatal(err)
	}

	if _, err := openConfinedFS(rootPath, true); err == nil {
		t.Fatal("openConfinedFS accepted a symlinked output root")
	}
}

func TestConfinedFSCreatesAndMutatesOnlyBelowRoot(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "output")
	f, err := openConfinedFS(rootPath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := f.AtomicWrite(filepath.Join(rootPath, "nested", "file.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := f.ReadFile(filepath.Join(rootPath, "nested", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "inside" {
		t.Fatalf("ReadFile = %q, want inside", got)
	}

	external := filepath.Join(t.TempDir(), "outside.txt")
	if err := f.AtomicWrite(external, []byte("outside"), 0o644); err == nil {
		t.Fatal("AtomicWrite accepted a path outside the output root")
	}
	if _, err := os.Lstat(external); !os.IsNotExist(err) {
		t.Fatalf("outside path was modified: %v", err)
	}
}

func TestConfinedFSMissingRootSupportsDryRunWithoutCreating(t *testing.T) {
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "output")
	f, err := openConfinedFS(rootPath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Lstat(rootPath); !os.IsNotExist(err) {
		t.Fatalf("Lstat missing root error = %v, want not-exist", err)
	}
	if _, err := os.Lstat(rootPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run root was created: %v", err)
	}
}

func TestConfinedFSRejectsSymlinkedParent(t *testing.T) {
	rootPath := t.TempDir()
	external := filepath.Join(t.TempDir(), "escape")
	if err := os.Symlink(external, filepath.Join(rootPath, "nested")); err != nil {
		t.Fatal(err)
	}
	f, err := openConfinedFS(rootPath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := f.AtomicWrite(filepath.Join(rootPath, "nested", "file.txt"), []byte("escape"), 0o644); err == nil {
		t.Fatal("AtomicWrite followed a symlinked parent")
	}
	if _, err := os.Lstat(filepath.Join(external, "file.txt")); !os.IsNotExist(err) {
		t.Fatalf("symlink target was modified: %v", err)
	}
}

func TestConfinedFSEnsureDirRejectsSymlinkedDirectory(t *testing.T) {
	rootPath := t.TempDir()
	realDir := filepath.Join(rootPath, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkedDir := filepath.Join(rootPath, "skills")
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatal(err)
	}
	f, err := openConfinedFS(rootPath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := f.EnsureDir(linkedDir, false); err == nil {
		t.Fatal("EnsureDir accepted a symlinked output directory")
	}
	info, err := os.Lstat(linkedDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("EnsureDir replaced the symlink instead of rejecting it")
	}
}

func TestConfinedFSRejectsSymlinkedParentWithinRoot(t *testing.T) {
	rootPath := t.TempDir()
	realDir := filepath.Join(rootPath, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkedDir := filepath.Join(rootPath, "nested")
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatal(err)
	}
	f, err := openConfinedFS(rootPath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := f.AtomicWrite(filepath.Join(linkedDir, "file.txt"), []byte("must not write"), 0o644); err == nil {
		t.Fatal("AtomicWrite followed an internal symlinked parent")
	}
	if _, err := os.Lstat(filepath.Join(realDir, "file.txt")); !os.IsNotExist(err) {
		t.Fatalf("internal symlink target was modified: %v", err)
	}
}

func TestConfinedFSRootHandleSurvivesRootPathReplacement(t *testing.T) {
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "output")
	originalRoot := filepath.Join(parent, "original")
	external := filepath.Join(parent, "external")
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("must survive"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := openConfinedFS(rootPath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := os.Rename(rootPath, originalRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, rootPath); err != nil {
		t.Fatal(err)
	}

	if err := f.AtomicWrite(filepath.Join(rootPath, "written.txt"), []byte("inside opened root"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(originalRoot, "written.txt")); err != nil {
		t.Fatalf("opened root was not mutated: %v", err)
	}
	assertUnchangedFile(t, sentinel, []byte("must survive"), 0o600)
}
