package cache

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func exactGitRootFixture(t *testing.T) (string, string, GitOID) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	repo := newExactGitRepository(t, "sha1")
	work := filepath.Join(filepath.Dir(repo), "work")
	skills := filepath.Join(work, "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"skill.md": "skill payload\n",
		"run.sh":   "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(skills, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(skills, "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("skill.md", filepath.Join(skills, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(work, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "commands", "command.md"), []byte("outside command\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(work, "skills[ab]"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "skills[ab]", "literal.txt"), []byte("literal root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(work, "skillsa"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "skillsa", "wildcard.txt"), []byte("not in literal root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, work, "add", ".")
	gitTest(t, work, "commit", "-m", "root fixture")
	gitTest(t, work, "push", "origin", "HEAD:main")
	oid := resolveExactGitRefForTest(t, "https://example.invalid/root-scoped", repo, "main")
	return "https://example.invalid/root-scoped", repo, oid
}

func exactGitSnapshotFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(candidate string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, candidate)
		if err != nil {
			return err
		}
		if rel == ".git" {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func exactGitOIDForTest(t testing.TB, value string) GitOID {
	t.Helper()
	oid, err := ParseGitOID(value)
	if err != nil {
		t.Fatal(err)
	}
	return oid
}

func TestExactGitRootSnapshotContainsAndVerifiesOnlyConfiguredFiles(t *testing.T) {
	source, repo, oid := exactGitRootFixture(t)
	snapshot, err := AcquireExactGitWithRootContext(context.Background(), source, repo, oid, "skills")
	if err != nil {
		t.Fatalf("acquire root-scoped snapshot: %v", err)
	}
	var consumed string
	if err := WithAcquiredExactGitRoot(context.Background(), source, repo, oid, "skills", func(path string) error {
		consumed = path
		return nil
	}); err != nil {
		t.Fatalf("consume acquired root-scoped snapshot: %v", err)
	}
	if consumed != snapshot {
		t.Fatalf("acquired consumer path = %q, want %q", consumed, snapshot)
	}
	want := []string{"skills/alias", "skills/run.sh", "skills/skill.md"}
	if got := exactGitSnapshotFiles(t, snapshot); !reflect.DeepEqual(got, want) {
		t.Fatalf("root snapshot files = %v, want %v", got, want)
	}
	if info, err := os.Lstat(filepath.Join(snapshot, "skills", "run.sh")); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("executable mode was not preserved: %v, %v", info, err)
	}
	if target, err := os.Readlink(filepath.Join(snapshot, "skills", "alias")); err != nil || target != "skill.md" {
		t.Fatalf("symlink was not preserved: target=%q error=%v", target, err)
	}
	if err := os.MkdirAll(filepath.Join(snapshot, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshot, "commands", "added-after-publication"), []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, release, err := LeaseExactGitReadOnlyWithRootContext(context.Background(), source, oid, "skills"); err != nil {
		t.Fatalf("verify after adding content outside the root: %v", err)
	} else {
		release()
	}
	if err := os.WriteFile(filepath.Join(snapshot, "skills", "skill.md"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, release, err := LeaseExactGitReadOnlyWithRootContext(context.Background(), source, oid, "skills"); !errors.Is(err, ErrExactGitSnapshotUntrusted) {
		if release != nil {
			release()
		}
		t.Fatalf("verify after changing content inside the root error = %v, want ErrExactGitSnapshotUntrusted", err)
	}
}

func TestExactGitRootPublicationChecksOutOnlyConfiguredRoot(t *testing.T) {
	source, repo, oid := exactGitRootFixture(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "git.log")
	wrapper := filepath.Join(bin, "git")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$COMMAND_LOG\"\nexec \"$REAL_GIT_BINARY\" \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMAND_LOG", logPath)
	t.Setenv("REAL_GIT_BINARY", realGit)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	snapshot, err := AcquireExactGitWithRootContext(context.Background(), source, repo, oid, "skills")
	if err != nil {
		t.Fatalf("acquire root-scoped snapshot: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(snapshot, "commands", "command.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside tracked file exists after publication: %v", err)
	}
	state, ok := readExactGitLocalState(snapshot, false)
	if !ok || state.head != oid.Hex() {
		t.Fatalf("published repository state = %#v, %v; want canonical metadata and detached HEAD %s", state, ok, oid.Hex())
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var sawScopedCheckout bool
	for _, invocation := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(invocation)
		for i, field := range fields {
			if field != "checkout" {
				continue
			}
			if !reflect.DeepEqual(fields[i:], []string{"checkout", oid.Hex(), "--", "skills"}) {
				t.Fatalf("checkout command = %q, want only the configured root", invocation)
			}
			sawScopedCheckout = true
		}
	}
	if !sawScopedCheckout {
		t.Fatalf("git command log contains no checkout limited to skills: %s", raw)
	}
}

func TestExactGitRootLiteralPathAndSymlinkComponents(t *testing.T) {
	source, repo, oid := exactGitRootFixture(t)
	snapshot, err := AcquireExactGitWithRootContext(context.Background(), source, repo, oid, "skills[ab]")
	if err != nil {
		t.Fatalf("acquire literal root: %v", err)
	}
	if got, want := exactGitSnapshotFiles(t, snapshot), []string{"skills[ab]/literal.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("literal root files = %v, want %v", got, want)
	}

	snapshot, err = AcquireExactGitWithRootContext(context.Background(), source, repo, oid, "skills")
	if err != nil {
		t.Fatalf("acquire skills root: %v", err)
	}
	realRoot := filepath.Join(snapshot, "skills-real")
	if err := os.Rename(filepath.Join(snapshot, "skills"), realRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realRoot, filepath.Join(snapshot, "skills")); err != nil {
		t.Fatal(err)
	}
	if _, release, err := LeaseExactGitReadOnlyWithRootContext(context.Background(), source, oid, "skills"); !errors.Is(err, ErrExactGitSnapshotUntrusted) {
		if release != nil {
			release()
		}
		t.Fatalf("verify through a symlink root component error = %v, want ErrExactGitSnapshotUntrusted", err)
	}
}

func TestExactGitRootFallbackAndNormalization(t *testing.T) {
	source, repo, oid := exactGitRootFixture(t)
	for input, want := range map[string]string{"": ".", ".": ".", "./skills//": "skills"} {
		got, err := NormalizeExactGitRoot(input)
		if err != nil || got != want {
			t.Fatalf("NormalizeExactGitRoot(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, root := range []string{"/skills", "../skills", "skills/../../outside", `skills\child`} {
		if _, err := NormalizeExactGitRoot(root); err == nil {
			t.Fatalf("NormalizeExactGitRoot(%q) succeeded, want rejection", root)
		}
	}
	legacy := snapshotKey(source, oid)
	if got := snapshotKeyForRoot(source, oid, "."); got != legacy {
		t.Fatalf("root . key = %q, want legacy key %q", got, legacy)
	}
	if snapshotKeyForRoot(source, oid, "skills") == legacy ||
		snapshotKeyForRoot(source, oid, "skills") == snapshotKeyForRoot(source, oid, "commands") {
		t.Fatal("distinct configured roots shared an exact snapshot key")
	}

	whole, err := AcquireExactGitWithContext(context.Background(), source, repo, oid)
	if err != nil {
		t.Fatalf("acquire whole-repository snapshot: %v", err)
	}
	fallback, err := AcquireExactGitWithRootContext(context.Background(), source, repo, oid, "commands/command.md")
	if err != nil {
		t.Fatalf("acquire non-tree root fallback: %v", err)
	}
	if fallback != whole {
		t.Fatalf("non-tree root returned %q, want whole-repository snapshot %q", fallback, whole)
	}
	if _, err := AcquireExactGitWithRootContext(context.Background(), source, repo, oid, "../escape"); err == nil {
		t.Fatal("acquire accepted an escaping root")
	}
}

func TestExactGitRootReadOnlyUsesWholeRepositoryFallback(t *testing.T) {
	source, repo, oid := exactGitRootFixture(t)
	whole, err := AcquireExactGitWithContext(context.Background(), source, repo, oid)
	if err != nil {
		t.Fatalf("acquire whole-repository snapshot: %v", err)
	}
	rootSnapshot := exactGitSnapshotPathForRoot(source, oid, "skills")
	if _, err := os.Lstat(rootSnapshot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("root snapshot already exists: %v", err)
	}
	if err := os.WriteFile(filepath.Join(whole, "commands", "command.md"), []byte("outside changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, release, err := LeaseExactGitReadOnlyWithRootContext(context.Background(), source, oid, "skills")
	if err != nil {
		t.Fatalf("read-only root fallback: %v", err)
	}
	if path != whole {
		release()
		t.Fatalf("read-only fallback path = %q, want whole snapshot %q", path, whole)
	}
	release()
	if _, err := os.Lstat(rootSnapshot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only fallback published a root snapshot: %v", err)
	}
	if err := WithExactGitReadOnlyRoot(context.Background(), source, oid, "skills", func(path string) error {
		if path != whole {
			t.Fatalf("read-only callback path = %q, want %q", path, whole)
		}
		return nil
	}); err != nil {
		t.Fatalf("read-only callback fallback: %v", err)
	}
	if err := os.WriteFile(filepath.Join(whole, "skills", "skill.md"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, release, err := LeaseExactGitReadOnlyWithRootContext(context.Background(), source, oid, "skills"); !errors.Is(err, ErrExactGitSnapshotUntrusted) {
		if release != nil {
			release()
		}
		t.Fatalf("read-only fallback did not verify its consumed root: %v", err)
	}

	missingOID := exactGitOIDForTest(t, "sha1:"+strings.Repeat("b", 40))
	if _, release, err := LeaseExactGitReadOnlyWithRootContext(context.Background(), source, missingOID, "skills"); !errors.Is(err, ErrExactGitSnapshotUnavailable) {
		if release != nil {
			release()
		}
		t.Fatalf("missing read-only snapshots error = %v, want ErrExactGitSnapshotUnavailable", err)
	}
}

func TestExactGitRootGarbageCollectionReachability(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	source := "https://example.invalid/root-gc"
	oid := exactGitOIDForTest(t, "sha1:"+strings.Repeat("a", 40))
	rootKey := snapshotKeyForRoot(source, oid, "skills")
	orphanKey := snapshotKeyForRoot(source, oid, "commands")
	snapshotDir := filepath.Join(exactGitRoot(), "snapshots")
	for _, key := range []string{snapshotKey(source, oid), rootKey, orphanKey} {
		if err := os.MkdirAll(filepath.Join(snapshotDir, key), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	doc := lockfile.Document{
		APIVersion: lockfile.APIVersionV1,
		Scope:      lockfile.ScopeRepo,
		Registries: []lockfile.GitRegistry{{
			URL: source, Ref: "main", Commit: oid,
			Roots: []lockfile.GitRoot{{Path: "skills", SourceHarness: "factory", RevisionSchema: "v1", RevisionDigest: lockfile.Digest("sha256:" + strings.Repeat("c", 64))}},
		}},
	}
	_, reachable, err := garbageCollectExactGitUnderLease(context.Background(), true, []lockfile.Document{doc}, nil)
	if err != nil {
		t.Fatalf("dry-run exact Git GC: %v", err)
	}
	for _, key := range []string{snapshotKey(source, oid), rootKey} {
		if _, ok := reachable[key]; !ok {
			t.Fatalf("reachable snapshots omit key %q", key)
		}
	}
	if _, ok := reachable[orphanKey]; ok {
		t.Fatalf("GC retained unconfigured root key %q", orphanKey)
	}
}
