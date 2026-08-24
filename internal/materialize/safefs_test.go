package materialize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
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

// Verified directory handles are cached, so a directory this type deletes must
// drop its cached handle: otherwise a path re-created as a symlink would still
// be written through the handle to the directory that used to be there, and the
// cache would have quietly re-introduced the escape it was meant to speed up.
func TestConfinedFSRefusesSymlinkReplacingARemovedCachedDirectory(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), ".factory")
	external := t.TempDir()
	f, err := openConfinedFS(rootPath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	nested := filepath.Join(rootPath, "skills", "nested")
	if err := f.AtomicWrite(filepath.Join(nested, "file.txt"), []byte("first"), 0o644); err != nil {
		t.Fatalf("seed cached handles: %v", err)
	}
	if err := f.RemoveAll(nested); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if err := os.Symlink(external, nested); err != nil {
		t.Fatal(err)
	}

	if err := f.AtomicWrite(filepath.Join(nested, "escaped.txt"), []byte("escape"), 0o644); err == nil {
		t.Fatal("AtomicWrite followed a symlink planted over a removed cached directory")
	}
	if _, err := os.Lstat(filepath.Join(external, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatalf("write escaped through the stale cached handle: %v", err)
	}
}

// A symlinked ancestor is canonicalized, not refused. Symlinked checkout
// directories, symlinked $HOME, and macOS's /var alias are ordinary; refusing
// them made sync unable to write anything on such a machine, while confinement
// is unaffected because the boundary is the resolved harness root.
func TestOpenConfinedFSCanonicalizesSymlinkedAncestor(t *testing.T) {
	parent := t.TempDir()
	external := t.TempDir()
	ancestor := filepath.Join(parent, "linked")
	if err := os.Symlink(external, ancestor); err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(ancestor, "output")

	f, err := openConfinedFS(rootPath, true)
	if err != nil {
		t.Fatalf("openConfinedFS rejected a symlinked ancestor: %v", err)
	}
	defer func() { _ = f.Close() }()

	if err := f.AtomicWrite(filepath.Join(rootPath, "file.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(external, "output", "file.txt"))
	if err != nil {
		t.Fatalf("read through canonical root: %v", err)
	}
	if string(got) != "inside" {
		t.Fatalf("content = %q, want inside", got)
	}
	escape := filepath.Join(external, "escaped.txt")
	if err := f.AtomicWrite(escape, []byte("escape"), 0o644); err == nil {
		t.Fatal("AtomicWrite escaped the canonicalized root")
	}
	if _, err := os.Lstat(escape); !os.IsNotExist(err) {
		t.Fatalf("path above the canonical root was written: %v", err)
	}
}

// The last component is the boundary itself, so it is refused even though its
// ancestors are followed.
func TestOpenConfinedFSRejectsSymlinkedRootUnderSymlinkedAncestor(t *testing.T) {
	parent := t.TempDir()
	external := t.TempDir()
	elsewhere := t.TempDir()
	ancestor := filepath.Join(parent, "linked")
	if err := os.Symlink(external, ancestor); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(external, "output")); err != nil {
		t.Fatal(err)
	}

	if _, err := openConfinedFS(filepath.Join(ancestor, "output"), true); err == nil {
		t.Fatal("openConfinedFS accepted a symlinked output root")
	}
}

func TestOpenConfinedFSRejectsSymlinkCycleAboveRoot(t *testing.T) {
	parent := t.TempDir()
	first := filepath.Join(parent, "a")
	second := filepath.Join(parent, "b")
	if err := os.Symlink(second, first); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, second); err != nil {
		t.Fatal(err)
	}

	if _, err := openConfinedFS(filepath.Join(first, "output"), true); err == nil {
		t.Fatal("openConfinedFS accepted a symlink cycle above the root")
	}
}

func TestConfinedFSRejectsCachePrefixLookalike(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cacheRoot := cache.CacheDir()
	lookalike := cacheRoot + "-other"
	if err := os.MkdirAll(lookalike, 0o755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(filepath.Join(lookalike, "entry"), linkPath); err != nil {
		t.Fatal(err)
	}
	outputFS, err := openConfinedFS(filepath.Dir(linkPath), true, cacheRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outputFS.Close() }()

	if outputFS.IsManagedSymlink(linkPath) {
		t.Fatal("cache-prefix lookalike was classified as managed")
	}
}

func TestConfinedFSReadsSymlinkFromOpenedTrustedRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cacheRoot := cache.CacheDir()
	cacheFile := filepath.Join(cacheRoot, "compiled", "mcp.json")
	want := []byte(`{"mcpServers":{"cached":{"command":"cached"}}}`)
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheFile, want, 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.Symlink(cacheFile, linkPath); err != nil {
		t.Fatal(err)
	}
	outputFS, err := openConfinedFS(filepath.Dir(linkPath), true, cacheRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outputFS.Close() }()

	got, err := outputFS.readMergeSource(linkPath)
	if err != nil {
		t.Fatalf("readMergeSource: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("readMergeSource = %q, want %q", got, want)
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

func TestCopyDirToRootPreservesDirectoryPermissions(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	nested := filepath.Join(source, "sensitive")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	outputRoot := filepath.Join(t.TempDir(), "output")
	f, err := openConfinedFS(outputRoot, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	destination := filepath.Join(outputRoot, "copied")
	if err := copyDirToRoot(f, source, destination); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{destination, filepath.Join(destination, "sensitive")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Errorf("%s mode = %o, want 0700", path, got)
		}
	}
}

func TestConfinedFSDirRootDefaultsDirectoryPermissions(t *testing.T) {
	outputRoot := filepath.Join(t.TempDir(), "output")
	f, err := openConfinedFS(outputRoot, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.dirRoot("defaulted", true, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.dirRoot("explicit", true, 0o755); err != nil {
		t.Fatal(err)
	}
	defaulted, err := os.Stat(filepath.Join(outputRoot, "defaulted"))
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := os.Stat(filepath.Join(outputRoot, "explicit"))
	if err != nil {
		t.Fatal(err)
	}
	if defaulted.Mode().Perm() != explicit.Mode().Perm() {
		t.Fatalf("defaulted mode = %o, explicit mode = %o", defaulted.Mode().Perm(), explicit.Mode().Perm())
	}
}
