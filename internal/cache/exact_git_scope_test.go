package cache

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// installCountingGit prepends a git wrapper to PATH that appends one
// "PWD|args" line per invocation, then execs the real git. The returned
// counter reports how many recorded lines contain every given substring.
func installCountingGit(t *testing.T) func(contains ...string) int {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "git.log")
	script := "#!/bin/sh\nprintf '%s|%s\\n' \"$PWD\" \"$*\" >> \"$AM_TEST_GIT_LOG\"\nexec \"$AM_TEST_REAL_GIT\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AM_TEST_GIT_LOG", logPath)
	t.Setenv("AM_TEST_REAL_GIT", realGit)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func(contains ...string) int {
		t.Helper()
		raw, err := os.ReadFile(logPath)
		if errors.Is(err, os.ErrNotExist) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, line := range strings.Split(string(raw), "\n") {
			if line == "" {
				continue
			}
			matched := true
			for _, want := range contains {
				if !strings.Contains(line, want) {
					matched = false
					break
				}
			}
			if matched {
				n++
			}
		}
		return n
	}
}

func acquireScopedSnapshot(t *testing.T, ctx context.Context, canonical, repo string) (GitOID, string) {
	t.Helper()
	oid := resolveExactGitRefForTest(t, canonical, repo, "main")
	path, err := AcquireExactGitWithContext(ctx, canonical, repo, oid)
	if err != nil {
		t.Fatalf("AcquireExactGitWithContext: %v", err)
	}
	return oid, path
}

func TestExactGitVerificationScopeVerifiesOncePerScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	count := installCountingGit(t)
	repo := newExactGitRepository(t, "sha1")
	canonical := "https://example.invalid/scoped"
	ctx := WithExactGitVerificationScope(context.Background())
	oid, path := acquireScopedSnapshot(t, ctx, canonical, repo)
	lsTree := func() int { return count(path, "ls-tree") }

	before := lsTree()
	if _, err := OpenExactGitReadOnlyWithContext(ctx, canonical, oid); err != nil {
		t.Fatalf("first scoped open: %v", err)
	}
	if _, err := OpenExactGitReadOnlyWithContext(ctx, canonical, oid); err != nil {
		t.Fatalf("second scoped open: %v", err)
	}
	if got := lsTree() - before; got != 0 {
		t.Fatalf("two scoped opens ran %d extra verifications, want 0", got)
	}
}

func TestExactGitWithoutVerificationScopeVerifiesEveryOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	count := installCountingGit(t)
	repo := newExactGitRepository(t, "sha1")
	canonical := "https://example.invalid/unscoped"
	oid, path := acquireScopedSnapshot(t, context.Background(), canonical, repo)
	lsTree := func() int { return count(path, "ls-tree") }

	before := lsTree()
	for i := 0; i < 2; i++ {
		if _, err := OpenExactGitReadOnlyWithContext(context.Background(), canonical, oid); err != nil {
			t.Fatalf("unscoped open %d: %v", i, err)
		}
	}
	if got := lsTree() - before; got != 2 {
		t.Fatalf("two unscoped opens ran %d verifications, want 2", got)
	}
}

func TestExactGitVerificationScopeFailedOpenReverifies(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	count := installCountingGit(t)
	repo := newExactGitRepository(t, "sha1")
	canonical := "https://example.invalid/scope-failed"
	ctx := WithExactGitVerificationScope(context.Background())
	oid, path := acquireScopedSnapshot(t, context.Background(), canonical, repo)
	lsTree := func() int { return count(path, "ls-tree") }

	tracked := filepath.Join(path, "resource.txt")
	original := readExactFile(t, path, "resource.txt")
	if err := os.WriteFile(tracked, []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := lsTree()
	if _, err := OpenExactGitReadOnlyWithContext(ctx, canonical, oid); !errors.Is(err, ErrExactGitSnapshotUntrusted) {
		t.Fatalf("tampered scoped open error = %v, want ErrExactGitSnapshotUntrusted", err)
	}
	if got := lsTree() - before; got != 1 {
		t.Fatalf("failed open ran %d verifications, want 1", got)
	}
	if err := os.WriteFile(tracked, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	before = lsTree()
	if _, err := OpenExactGitReadOnlyWithContext(ctx, canonical, oid); err != nil {
		t.Fatalf("restored open did not re-verify cleanly: %v", err)
	}
	if got := lsTree() - before; got != 1 {
		t.Fatalf("open after a failure reused verification: %d runs, want 1", got)
	}
	before = lsTree()
	if _, err := OpenExactGitReadOnlyWithContext(ctx, canonical, oid); err != nil {
		t.Fatalf("open after re-verification: %v", err)
	}
	if got := lsTree() - before; got != 0 {
		t.Fatalf("re-verified snapshot ran %d extra verifications, want 0", got)
	}
}

func TestExactGitVerificationScopeRepublishReverifies(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	count := installCountingGit(t)
	repo := newExactGitRepository(t, "sha1")
	canonical := "https://example.invalid/scope-republish"
	ctx := WithExactGitVerificationScope(context.Background())
	oid, path := acquireScopedSnapshot(t, context.Background(), canonical, repo)
	lsTree := func() int { return count(path, "ls-tree") }
	stageLsTree := func() int { return count(".snapshot-stage-", "ls-tree") }

	if err := os.WriteFile(filepath.Join(path, "resource.txt"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, beforeStage := lsTree(), stageLsTree()
	republished, err := AcquireExactGitWithContext(ctx, canonical, repo, oid)
	if err != nil {
		t.Fatalf("republish after quarantine: %v", err)
	}
	if republished != path {
		t.Fatalf("republished snapshot path = %q, want %q", republished, path)
	}
	if got := lsTree() - before; got != 1 {
		t.Fatalf("tampered snapshot was not re-verified before quarantine: %d runs, want 1", got)
	}
	if got := stageLsTree() - beforeStage; got != 1 {
		t.Fatalf("republished snapshot was not verified during publish: %d runs, want 1", got)
	}
	before = lsTree()
	if _, err := OpenExactGitReadOnlyWithContext(ctx, canonical, oid); err != nil {
		t.Fatalf("open after republish: %v", err)
	}
	if got := lsTree() - before; got != 0 {
		t.Fatalf("snapshot verified during publish re-verified in the same scope: %d", got)
	}
}

// spec: exact-snapshot-verification/snapshot-tampered-between-commands-is-rejected-by-the-next-read-only-command
func TestExactGitVerificationScopeDoesNotCrossScopes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newExactGitRepository(t, "sha1")
	canonical := "https://example.invalid/scope-boundary"
	first := WithExactGitVerificationScope(context.Background())
	oid, path := acquireScopedSnapshot(t, first, canonical, repo)
	if _, err := OpenExactGitReadOnlyWithContext(first, canonical, oid); err != nil {
		t.Fatalf("open in first scope: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "resource.txt"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := WithExactGitVerificationScope(context.Background())
	if _, err := OpenExactGitReadOnlyWithContext(second, canonical, oid); !errors.Is(err, ErrExactGitSnapshotUntrusted) {
		t.Fatalf("tampered snapshot in a fresh scope error = %v, want ErrExactGitSnapshotUntrusted", err)
	}
}

func TestExactGitVerificationScopeSanitizesStoreOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	count := installCountingGit(t)
	repo := newExactGitRepository(t, "sha1")
	canonical := "https://example.invalid/scope-store"
	store := exactGitObjectStorePath(canonical)
	ctx := WithExactGitVerificationScope(context.Background())
	storeLists := func() int { return count("|"+store, "--name-only") }
	stageLists := func() int { return count(".objects-stage-", "--name-only") }

	if _, err := ResolveExactGitRefWithContext(ctx, canonical, repo, "main"); err != nil {
		t.Fatalf("scoped resolve: %v", err)
	}
	oid := resolveExactGitRefForTest(t, canonical, repo, "main")
	if _, err := AcquireExactGitWithContext(ctx, canonical, repo, oid); err != nil {
		t.Fatalf("scoped acquire: %v", err)
	}
	if got := storeLists(); got != 0 {
		t.Fatalf("sanitized store re-inspected %d times inside one scope, want 0", got)
	}
	// Each cloned store is sanitized once on its staging directory; a
	// subprocess sanitize lists the local keys before and after cleanup.
	stages := stageLists()
	if stages != 2 {
		t.Fatalf("fresh clone sanitized %d staging repositories, want 1", stages/2)
	}
	if err := os.RemoveAll(store); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveExactGitRefWithContext(ctx, canonical, repo, "main"); err != nil {
		t.Fatalf("resolve after store loss: %v", err)
	}
	if got := stageLists(); got != 4 {
		t.Fatalf("re-cloned store was not re-sanitized: %d staging sanitizations, want 2", got/2)
	}
	if got := storeLists(); got != 0 {
		t.Fatalf("re-cloned store re-inspected %d times inside one scope, want 0", got)
	}
}

func TestWithExactGitVerificationScopeReusesExistingScope(t *testing.T) {
	ctx := WithExactGitVerificationScope(context.Background())
	if got := WithExactGitVerificationScope(ctx); got != ctx {
		t.Fatal("WithExactGitVerificationScope replaced an existing scope")
	}
}

func TestWithFreshExactGitVerificationScopeShadowsOuterScope(t *testing.T) {
	outer := WithExactGitVerificationScope(context.Background())
	fresh := WithFreshExactGitVerificationScope(outer)
	if exactGitVerificationScopeFrom(fresh) == nil {
		t.Fatal("WithFreshExactGitVerificationScope attached no scope")
	}
	if exactGitVerificationScopeFrom(fresh) == exactGitVerificationScopeFrom(outer) {
		t.Fatal("WithFreshExactGitVerificationScope reused the outer scope")
	}
}
