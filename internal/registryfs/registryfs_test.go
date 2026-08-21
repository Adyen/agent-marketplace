package registryfs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidateTreeRejectsRemoteSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	err := ValidateTree(root, true)
	if err == nil || !strings.Contains(err.Error(), "remote registries do not allow symlinks") {
		t.Fatalf("ValidateTree error = %v, want actionable remote symlink rejection", err)
	}
}

func TestValidateTreeAllowsContainedRelativeSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills", "one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../target", filepath.Join(root, "skills", "one", "link")); err != nil {
		t.Fatal(err)
	}

	if err := ValidateTree(root, false); err != nil {
		t.Fatalf("ValidateTree rejected contained local symlink: %v", err)
	}

	fsys, err := New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()
	data, err := fsys.ReadFile("skills/one/link")
	if err != nil || string(data) != "inside" {
		t.Fatalf("ReadFile through contained symlink = %q, %v", data, err)
	}
}

func TestValidateTreeRejectsEscapingLocalSymlinks(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "sentinel"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(external, "sentinel"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	err := ValidateTree(root, false)
	if err == nil {
		t.Fatal("ValidateTree accepted a symlink pointing outside the registry root")
	}
	if !strings.Contains(err.Error(), "registry root") {
		t.Fatalf("ValidateTree error = %v, want containment rejection", err)
	}
}

// TestValidateTreeRejectsAbsoluteSymlinkInsideRoot pins the deliberate policy
// choice: an absolute target is refused even when it names a path inside the
// root, because following it would require re-resolving a path by name.
func TestValidateTreeRejectsAbsoluteSymlinkInsideRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	err := ValidateTree(root, false)
	if err == nil || !strings.Contains(err.Error(), "absolute symlink") {
		t.Fatalf("ValidateTree error = %v, want absolute symlink rejection", err)
	}
}

func TestValidateTreeSkipsGitMetadata(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A clone's own metadata is never ingested, so its contents must not decide
	// whether a registry is readable.
	if err := os.Symlink(filepath.Join(external, "anything"), filepath.Join(root, ".git", "objects", "link")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTree(root, true); err != nil {
		t.Fatalf("ValidateTree walked .git: %v", err)
	}
}

func TestReadFileRefusesEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../external/sentinel", filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	// Also cover a relative traversal that leaves the root without a symlink.
	fsys, err := New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()

	if data, err := fsys.ReadFile("escape"); err == nil {
		t.Fatalf("ReadFile followed an escaping symlink and returned %q", data)
	}
	if _, err := fsys.ReadFile("../sentinel"); err == nil {
		t.Fatal("ReadFile accepted a parent traversal")
	}
	if fsys.ValidationError() == nil {
		t.Fatal("ValidationError did not record the containment rejection")
	}
}

// TestOpenDoesNotBlockOnFIFO is the regression for the confirmed hang: a plain
// open(2) of a FIFO with no writer never returns, so a registry could stall any
// command that reads its files.
func TestOpenDoesNotBlockOnFIFO(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "trap")
	mkfifo(t, fifo)

	fsys, err := New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()

	done := make(chan error, 1)
	go func() {
		_, err := fsys.ReadFile("trap")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ReadFile accepted a FIFO")
		}
		if !strings.Contains(err.Error(), "unsupported file type") {
			t.Fatalf("ReadFile error = %v, want unsupported file type", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ReadFile blocked on a FIFO")
	}

	// Enumeration must refuse the entry too, so a caller that lists a directory
	// never even attempts the read.
	if _, err := fsys.ReadDir("."); err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("ReadDir error = %v, want unsupported file type", err)
	}
	if _, err := fsys.Stat("trap"); err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("Stat error = %v, want unsupported file type", err)
	}
	if err := ValidateTree(root, false); err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("ValidateTree error = %v, want unsupported file type", err)
	}
}

// TestOpenDoesNotBlockOnFIFOReachedThroughSymlink covers the same trap hidden
// behind a contained symlink, which passes a naive "is it a symlink" check.
func TestOpenDoesNotBlockOnFIFOReachedThroughSymlink(t *testing.T) {
	root := t.TempDir()
	mkfifo(t, filepath.Join(root, "trap"))
	if err := os.Symlink("trap", filepath.Join(root, "innocent.md")); err != nil {
		t.Fatal(err)
	}

	fsys, err := New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()

	done := make(chan error, 1)
	go func() {
		_, err := fsys.ReadFile("innocent.md")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
			t.Fatalf("ReadFile error = %v, want unsupported file type", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ReadFile blocked on a FIFO reached through a symlink")
	}
}

func TestNewRejectsFileRootAndMissingRoot(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(file, false); err == nil {
		t.Fatal("New accepted a regular file as a registry root")
	}
	if _, err := New(filepath.Join(root, "missing"), false); err == nil {
		t.Fatal("New accepted a missing registry root")
	}
}

func TestNewRejectsSymlinkedRootForRemoteRegistries(t *testing.T) {
	parent := t.TempDir()
	real := filepath.Join(parent, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := New(link, true); err == nil || !strings.Contains(err.Error(), "remote registries do not allow symlinks") {
		t.Fatalf("New error = %v, want remote symlink rejection", err)
	}
	fsys, err := New(link, false)
	if err != nil {
		t.Fatalf("New rejected a symlinked local registry root: %v", err)
	}
	_ = fsys.Close()
}

func TestStatMissingPathIsNotExist(t *testing.T) {
	fsys, err := New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()
	if _, err := fsys.Stat("missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Stat error = %v, want fs.ErrNotExist", err)
	}
	if fsys.ValidationError() != nil {
		t.Fatalf("a missing path recorded a validation error: %v", fsys.ValidationError())
	}
	if err := ValidatePath(fsys.Root(), "missing", true); err != nil {
		t.Fatalf("ValidatePath on a missing path = %v, want nil", err)
	}
}

func TestReadFileModeReportsOpenedDescriptorMode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "script.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fsys, err := New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()
	data, mode, err := fsys.ReadFileMode("script.sh")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "#!/bin/sh\n" || mode.Perm() != 0o755 {
		t.Fatalf("ReadFileMode = %q, %v", data, mode)
	}
	if _, _, err := fsys.ReadFileMode("."); err == nil {
		t.Fatal("ReadFileMode accepted a directory")
	}
}

func TestWalkDirAppliesEntryPolicy(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(external, "sentinel"), filepath.Join(root, "skills", "leak")); err != nil {
		t.Fatal(err)
	}
	fsys, err := New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()
	err = fs.WalkDir(fsys, ".", func(_ string, _ fs.DirEntry, err error) error { return err })
	if err == nil {
		t.Fatal("fs.WalkDir accepted an escaping symlink")
	}
}

func TestSubtreeConfinesToRegistryRootAndFallsBack(t *testing.T) {
	registry := t.TempDir()
	skillDir := filepath.Join(registry, "skills", "one")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(registry, "shared.txt"), []byte("shared"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../shared.txt", filepath.Join(skillDir, "shared.txt")); err != nil {
		t.Fatal(err)
	}

	fsys, base, err := Subtree(registry, skillDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if base != "skills/one" {
		t.Fatalf("Subtree base = %q, want skills/one", base)
	}
	if data, err := fsys.ReadFile(base + "/shared.txt"); err != nil || string(data) != "shared" {
		t.Fatalf("registry-root confinement blocked an intra-registry symlink: %q %v", data, err)
	}
	_ = fsys.Close()

	// With no registry provenance the subtree narrows to the directory itself,
	// so the same intra-registry symlink is refused rather than followed.
	fsys, base, err = Subtree("", skillDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()
	if base != "." {
		t.Fatalf("Subtree base = %q, want .", base)
	}
	if _, err := fsys.ReadFile("shared.txt"); err == nil {
		t.Fatal("directory-scoped confinement followed a symlink out of the directory")
	}
}

func TestReadDirLenientDropsRefusedEntriesOnly(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "sentinel"), []byte("SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(external, "sentinel"), filepath.Join(root, "leak")); err != nil {
		t.Fatal(err)
	}
	mkfifo(t, filepath.Join(root, "pipe"))

	fsys, err := New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()

	entries, refused, err := fsys.ReadDirLenient(".")
	if err != nil {
		t.Fatalf("ReadDirLenient: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "keep.sh" {
		t.Fatalf("kept entries = %v, want only keep.sh", entries)
	}
	if len(refused) != 2 {
		t.Fatalf("refused = %v, want one per hostile entry", refused)
	}
	// A refused entry is dropped from the listing, never made readable.
	if data, err := fsys.ReadFile("leak"); err == nil {
		t.Fatalf("a refused entry was readable: %q", data)
	}
}

// TestReadAfterValidationRefusesSwappedEntry is the TOCTOU regression: a tree
// that passed validation is re-checked by the read itself, so replacing a
// checked regular file with an escaping symlink afterwards changes nothing.
func TestReadAfterValidationRefusesSwappedEntry(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(root, "skills", "one", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(victim), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, []byte("honest"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ValidateTree(root, false); err != nil {
		t.Fatalf("ValidateTree on an honest tree: %v", err)
	}
	fsys, err := New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()
	if data, err := fsys.ReadFile("skills/one/SKILL.md"); err != nil || string(data) != "honest" {
		t.Fatalf("baseline read = %q, %v", data, err)
	}

	// The registry rewrites itself after validation passed.
	if err := os.Remove(victim); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, victim); err != nil {
		t.Fatal(err)
	}
	if data, err := fsys.ReadFile("skills/one/SKILL.md"); err == nil {
		t.Fatalf("a swapped entry was read: %q", data)
	}

	// Swapping a directory component is refused the same way.
	if err := os.RemoveAll(filepath.Join(root, "skills")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "skills")); err != nil {
		t.Fatal(err)
	}
	if data, err := fsys.ReadFile("skills/sentinel"); err == nil {
		t.Fatalf("a swapped directory component was read: %q", data)
	}
}

// TestConcurrentRewriteNeverDisclosesSentinel hammers the same swap while reads
// are in flight; run under -race it also covers the FS's own shared state.
func TestConcurrentRewriteNeverDisclosesSentinel(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(root, "SKILL.md")
	if err := os.WriteFile(victim, []byte("honest"), 0o644); err != nil {
		t.Fatal(err)
	}
	fsys, err := New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.Remove(victim)
			if i%2 == 0 {
				_ = os.Symlink(sentinel, victim)
			} else {
				_ = os.WriteFile(victim, []byte("honest"), 0o644)
			}
		}
	}()

	for i := 0; i < 2000; i++ {
		data, err := fsys.ReadFile("SKILL.md")
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "SENTINEL") {
			close(stop)
			wg.Wait()
			t.Fatal("a concurrent rewrite disclosed content from outside the registry root")
		}
	}
	close(stop)
	wg.Wait()
}

func TestReadResourceFileConfinesDirectoryAndFile(t *testing.T) {
	registry := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "sentinel"), []byte("SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(registry, "skills", "good")
	if err := os.MkdirAll(good, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "SKILL.md"), []byte("---\nname: good\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(registry, "skills", "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(external, "sentinel"), filepath.Join(bad, "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	data, file, err := ReadResourceFile(registry, good)
	if err != nil || !strings.Contains(string(data), "name: good") {
		t.Fatalf("ReadResourceFile(dir) = %q, %q, %v", data, file, err)
	}
	if file != filepath.Join(good, "SKILL.md") {
		t.Fatalf("ReadResourceFile source file = %q", file)
	}
	if data, _, err := ReadResourceFile(registry, bad); err == nil {
		t.Fatalf("ReadResourceFile followed an escaping SKILL.md symlink: %q", data)
	}
	if data, _, err := ReadResourceFile(registry, filepath.Join(bad, "SKILL.md")); err == nil {
		t.Fatalf("ReadResourceFile followed an escaping file symlink: %q", data)
	}
}
