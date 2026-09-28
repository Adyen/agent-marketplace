package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/testutil"
	"golang.org/x/sys/unix"
)

func TestPublishRenamedDirectorySyncsExactParentAndCleansStageOnSyncFailure(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	destination := filepath.Join(parent, "destination")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "HEAD"), []byte("ref"), 0o600); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected parent fsync failure")
	var synced string
	err := publishRenamedDirectory(stage, destination, func(dir string) error {
		synced = dir
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("publishRenamedDirectory error = %v, want injected fsync error", err)
	}
	if synced != parent {
		t.Fatalf("synced directory = %q, want exact destination parent %q", synced, parent)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("destination after successful rename = %v", err)
	}
	if _, err := os.Lstat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging residue after fsync failure = %v", err)
	}
}

func TestExactGitCacheIdentityUsesSharedCanonicalURLFixtures(t *testing.T) {
	for _, tc := range testutil.CanonicalGitURLFixtures {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := lockfile.CanonicalGitURL(tc.Raw)
			if tc.Canonical == "" {
				if err == nil {
					t.Fatalf("CanonicalGitURL(%q) = %q, want rejection", tc.Raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("CanonicalGitURL(%q): %v", tc.Raw, err)
			}
			if got != tc.Canonical {
				t.Fatalf("canonical URL = %q, want %q", got, tc.Canonical)
			}
			if sourceKey(got) != sourceKey(tc.Canonical) {
				t.Fatalf("equivalent URL spellings produced different cache identities")
			}
		})
	}
	for _, tc := range testutil.NonEquivalentGitURLPairs {
		t.Run("non-equivalent/"+tc.Name, func(t *testing.T) {
			left, err := lockfile.CanonicalGitURL(tc.Left)
			if err != nil {
				t.Fatalf("CanonicalGitURL(left): %v", err)
			}
			right, err := lockfile.CanonicalGitURL(tc.Right)
			if err != nil {
				t.Fatalf("CanonicalGitURL(right): %v", err)
			}
			if sourceKey(left) == sourceKey(right) {
				t.Fatalf("non-equivalent URL identities collided: %q", left)
			}
		})
	}
}

func TestParseGitOID(t *testing.T) {
	tests := []struct {
		name  string
		value string
		ok    bool
	}{
		{"sha1", "sha1:" + strings.Repeat("a", 40), true},
		{"sha256", "sha256:" + strings.Repeat("b", 64), true},
		{"abbreviated", "sha1:" + strings.Repeat("a", 12), false},
		{"uppercase", "sha1:" + strings.Repeat("A", 40), false},
		{"unsupported", "md5:" + strings.Repeat("a", 32), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oid, err := ParseGitOID(tt.value)
			if (err == nil) != tt.ok {
				t.Fatalf("ParseGitOID(%q) error = %v, want success %v", tt.value, err, tt.ok)
			}
			if tt.ok && oid.String() != tt.value {
				t.Fatalf("GitOID.String() = %q, want %q", oid.String(), tt.value)
			}
		})
	}
}

func TestResolveExactGitRefReturnsConciseSSHTransportFailure(t *testing.T) {
	isolatedGitAuth(t)
	canonical := "git+scp://git@example.invalid/repo"
	store := exactGitObjectStorePath(canonical)
	if err := prepareExactGitDirectory(filepath.Dir(store)); err != nil {
		t.Fatal(err)
	}
	gitTest(t, "", "init", "--bare", store)
	if err := os.Chmod(store, 0o700); err != nil {
		t.Fatal(err)
	}

	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTH_REAL_GIT", realGit)
	wrapper := authFile(t, "git", "#!/bin/sh\ncase \" $* \" in *\" fetch \"*) echo synthetic-auth-secret >&2; exit 128;; *) exec \"$AUTH_REAL_GIT\" \"$@\";; esac\n", 0o700)
	t.Setenv("PATH", filepath.Dir(wrapper)+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err = ResolveExactGitRefWithContext(context.Background(), canonical, "git@example.invalid:repo", "main")
	const want = "git.auth-transport-failed: Git SSH connection failed. Verify your SSH agent and repository access"
	if err == nil || err.Error() != want {
		t.Fatalf("ResolveExactGitRefWithContext() error = %q, want %q", err, want)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("ResolveExactGitRefWithContext() error does not preserve the Git process failure: %v", err)
	}
	if strings.Contains(err.Error(), "synthetic-auth-secret") {
		t.Fatal("transport diagnostic leaked a secret")
	}
}

func TestExactGitSubprocessHonorsCancellation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	binDir := t.TempDir()
	gitPath := filepath.Join(binDir, "git")
	if err := os.WriteFile(gitPath, []byte("#!/bin/sh\nexec sleep 60\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := exactGitBytesWithContext(ctx, "", "fetch")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("exact git error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancelled exact git took %s", elapsed)
	}
	if _, err := os.Lstat(CacheDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled exact git left cache residue: %v", err)
	}
}

func TestExactGitLifecycleCoexistenceAndOffline(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := newExactGitRepository(t, "sha1")
	canonical := "https://example.invalid/team/resources"

	first := resolveExactGitRefForTest(t, canonical, repo, "main")
	firstPath, err := AcquireExactGit(canonical, repo, first)
	if err != nil {
		t.Fatalf("AcquireExactGit(first): %v", err)
	}
	if got := readExactFile(t, firstPath, "resource.txt"); got != "first\n" {
		t.Fatalf("first snapshot content = %q", got)
	}

	second := commitExactGitFile(t, repo, "second\n")
	secondOID, err := ParseGitOID("sha1:" + second)
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := AcquireExactGit(canonical, repo, secondOID)
	if err != nil {
		t.Fatalf("AcquireExactGit(second): %v", err)
	}
	if firstPath == secondPath {
		t.Fatalf("distinct commits share snapshot path %q", firstPath)
	}
	if got := readExactFile(t, firstPath, "resource.txt"); got != "first\n" {
		t.Fatalf("old snapshot changed to %q", got)
	}
	if got := readExactFile(t, secondPath, "resource.txt"); got != "second\n" {
		t.Fatalf("second snapshot content = %q", got)
	}

	opened, err := OpenExactGit(canonical, first)
	if err != nil {
		t.Fatalf("OpenExactGit(first): %v", err)
	}
	if opened != firstPath {
		t.Fatalf("OpenExactGit path = %q, want %q", opened, firstPath)
	}
	if _, err := GarbageCollect(false, &config.Config{}); err != nil {
		t.Fatalf("GarbageCollect: %v", err)
	}
	if _, err := OpenExactGit(canonical, first); err != nil {
		t.Fatalf("OpenExactGit after registry garbage collection: %v", err)
	}

	missing, _ := ParseGitOID("sha1:" + strings.Repeat("f", 40))
	if _, err := OpenExactGit(canonical, missing); !errors.Is(err, ErrExactGitSnapshotUnavailable) {
		t.Fatalf("OpenExactGit(missing) error = %v, want ErrExactGitSnapshotUnavailable", err)
	}
}

func TestOpenExactGitDoesNotCreateCacheState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	oid, err := ParseGitOID("sha1:" + strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenExactGit("https://example.invalid/read-only", oid); !errors.Is(err, ErrExactGitSnapshotUnavailable) {
		t.Fatalf("OpenExactGit error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only open created cache state: %v", err)
	}
}

func TestExactGitCancelledContextHasNoCacheSideEffects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	canonical := "https://example.invalid/cancelled"
	oid, err := ParseGitOID("sha1:" + strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	consumed := false
	calls := []struct {
		name string
		call func() error
	}{
		{name: "resolve", call: func() error {
			_, err := ResolveExactGitRefWithContext(ctx, canonical, "/does/not/exist", "main")
			return err
		}},
		{name: "acquire", call: func() error {
			_, err := AcquireExactGitWithContext(ctx, canonical, "/does/not/exist", oid)
			return err
		}},
		{name: "acquire and consume", call: func() error {
			return WithAcquiredExactGit(ctx, canonical, "/does/not/exist", oid, func(string) error {
				consumed = true
				return nil
			})
		}},
		{name: "open", call: func() error {
			_, err := OpenExactGitReadOnlyWithContext(ctx, canonical, oid)
			return err
		}},
		{name: "read", call: func() error {
			return WithExactGitReadOnly(ctx, canonical, oid, func(string) error {
				consumed = true
				return nil
			})
		}},
		{name: "namespace lease", call: func() error {
			release, err := AcquireExactGitNamespaceLeaseWithContext(ctx)
			if release != nil {
				release()
			}
			return err
		}},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled call error = %v, want context.Canceled", err)
			}
		})
	}
	if consumed {
		t.Fatal("cancelled read invoked its consumer")
	}
	if _, err := os.Lstat(filepath.Join(home, ".cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled exact git calls created cache state: %v", err)
	}
}

func TestExactGitCancelledReadOnlyDoesNotCreateLockFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := newExactGitRepository(t, "sha1")
	canonical := "https://example.invalid/cancelled-read"
	oid := resolveExactGitRefForTest(t, canonical, repo, "main")
	if _, err := AcquireExactGit(canonical, repo, oid); err != nil {
		t.Fatal(err)
	}
	lockDir := filepath.Join(exactGitRoot(), "locks")
	if err := os.RemoveAll(lockDir); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenExactGitReadOnlyWithContext(ctx, canonical, oid); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled open error = %v, want context.Canceled", err)
	}
	readInvoked := false
	if err := WithExactGitReadOnly(ctx, canonical, oid, func(string) error {
		readInvoked = true
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read error = %v, want context.Canceled", err)
	}
	if readInvoked {
		t.Fatal("cancelled read invoked its consumer")
	}
	if _, err := os.Lstat(lockDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled read-only calls created lock state: %v", err)
	}
}

func TestExactGitRejectsMismatchAndTampering(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newExactGitRepository(t, "sha1")
	canonical := "https://example.invalid/team/tamper"
	oid := resolveExactGitRefForTest(t, canonical, repo, "main")
	path, err := AcquireExactGit(canonical, repo, oid)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(path, "resource.txt"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenExactGit(canonical, oid); !errors.Is(err, ErrExactGitSnapshotUntrusted) {
		t.Fatalf("OpenExactGit(tampered) error = %v, want ErrExactGitSnapshotUntrusted", err)
	}

	wrong, _ := ParseGitOID("sha1:" + strings.Repeat("e", 40))
	if _, err := AcquireExactGit(canonical, repo, wrong); err == nil {
		t.Fatal("AcquireExactGit accepted unavailable exact OID")
	}
	if _, err := os.Lstat(exactGitSnapshotPath(canonical, wrong)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed acquisition published snapshot: %v", err)
	}
	stages, err := filepath.Glob(filepath.Join(exactGitRoot(), "snapshots", ".snapshot-stage-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 0 {
		t.Fatalf("failed acquisition left staging directories: %v", stages)
	}
}

func TestAcquireExactGitReplacesTamperedSnapshot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newExactGitRepository(t, "sha1")
	canonical := "https://example.invalid/reacquire"
	oid := resolveExactGitRefForTest(t, canonical, repo, "main")
	path, err := AcquireExactGit(canonical, repo, oid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "resource.txt"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got string
	err = WithAcquiredExactGit(context.Background(), canonical, repo, oid, func(snapshot string) error {
		got = snapshot
		if readExactFile(t, snapshot, "resource.txt") != "first\n" {
			return errors.New("consumer received tampered bytes")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithAcquiredExactGit(tampered): %v", err)
	}
	if got != path || readExactFile(t, got, "resource.txt") != "first\n" {
		t.Fatalf("reacquired snapshot = %q with unexpected content", got)
	}
	if _, err := os.Lstat(path + ".quarantine"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("quarantine residue: %v", err)
	}
}

func TestExactGitRejectsMaliciousLocalConfigWithoutExecutingHelper(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newExactGitRepository(t, "sha1")
	canonical := "https://example.invalid/local-config"
	oid := resolveExactGitRefForTest(t, canonical, repo, "main")
	path, err := AcquireExactGit(canonical, repo, oid)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "helper-ran")
	gitTest(t, path, "config", "credential.helper", "!touch "+marker)
	if _, err := OpenExactGit(canonical, oid); !errors.Is(err, ErrExactGitSnapshotUntrusted) {
		t.Fatalf("OpenExactGit error = %v", err)
	}
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repository-local helper executed: %v", err)
	}
}

func TestSanitizeExactGitConfigAllowsPlatformLocalConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newExactGitRepository(t, "sha1")
	canonical := "https://example.invalid/platform-config"
	resolveExactGitRefForTest(t, canonical, repo, "main")
	configPath := filepath.Join(exactGitObjectStorePath(canonical), "config")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config = append(config, []byte("\n[core]\n\tignorecase = true\n")...)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sanitizeExactGitConfigWithContext(context.Background(), exactGitObjectStorePath(canonical)); err != nil {
		t.Fatalf("sanitizeExactGitConfigWithContext: %v", err)
	}
}

func TestExactGitRejectsObjectFormatMismatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newExactGitRepository(t, "sha1")
	canonical := "https://example.invalid/format-mismatch"
	resolveExactGitRefForTest(t, canonical, repo, "main")
	wrong, _ := ParseGitOID("sha256:" + strings.Repeat("a", 64))
	if _, err := AcquireExactGit(canonical, repo, wrong); !errors.Is(err, ErrGitOIDMismatch) {
		t.Fatalf("AcquireExactGit(format mismatch) error = %v, want ErrGitOIDMismatch", err)
	}
}

// Symlink safety is enforced per configured root by sourcerevision; an unsafe
// link elsewhere in the repository must not make the whole snapshot unusable.
func TestExactGitAcquiresSnapshotWithEscapingOrDanglingSymlink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newExactGitRepository(t, "sha1")
	work := filepath.Join(filepath.Dir(repo), "work")
	if err := os.Symlink("../outside", filepath.Join(work, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent/.factory/settings.json", filepath.Join(work, "dangling")); err != nil {
		t.Fatal(err)
	}
	gitTest(t, work, "add", "escape", "dangling")
	gitTest(t, work, "commit", "-m", "symlink")
	gitTest(t, work, "push", "origin", "HEAD:main")

	canonical := "https://example.invalid/symlink"
	oid := resolveExactGitRefForTest(t, canonical, repo, "main")
	if _, err := AcquireExactGit(canonical, repo, oid); err != nil {
		t.Fatalf("AcquireExactGit(unsafe symlink outside any root): %v", err)
	}
	if _, err := AcquireExactGit(canonical, repo, oid); err != nil {
		t.Fatalf("AcquireExactGit(reuse snapshot): %v", err)
	}
}

func TestExactGitAllowsContainedSymlink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newExactGitRepository(t, "sha1")
	work := filepath.Join(filepath.Dir(repo), "work")
	if err := os.Symlink("resource.txt", filepath.Join(work, "contained")); err != nil {
		t.Fatal(err)
	}
	gitTest(t, work, "add", "contained")
	gitTest(t, work, "commit", "-m", "contained symlink")
	gitTest(t, work, "push", "origin", "HEAD:main")
	canonical := "https://example.invalid/contained-symlink"
	oid := resolveExactGitRefForTest(t, canonical, repo, "main")
	if _, err := AcquireExactGit(canonical, repo, oid); err != nil {
		t.Fatalf("AcquireExactGit(contained symlink): %v", err)
	}
}

func TestExactGitDoesNotPersistFetchCredentialsAndUsesPrivateModes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newExactGitRepository(t, "sha1")
	gitTest(t, repo, "update-server-info")
	server := httptest.NewServer(http.FileServer(http.Dir(filepath.Dir(repo))))
	defer server.Close()
	fetchURL := "http://cache-user:super-secret-token@" + strings.TrimPrefix(server.URL, "http://") + "/" + filepath.Base(repo)
	canonical := "https://example.invalid/private-cache"
	oid := resolveExactGitRefForTest(t, canonical, fetchURL, "main")
	if _, err := AcquireExactGit(canonical, fetchURL, oid); err != nil {
		t.Fatalf("AcquireExactGit: %v", err)
	}

	for _, path := range []string{
		exactGitRoot(),
		filepath.Join(exactGitRoot(), "objects"),
		exactGitObjectStorePath(canonical),
		filepath.Join(exactGitRoot(), "snapshots"),
		exactGitSnapshotPath(canonical, oid),
		filepath.Join(exactGitRoot(), "locks"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode = %o, want 700", path, info.Mode().Perm())
		}
	}
	for _, repoPath := range []string{exactGitObjectStorePath(canonical), exactGitSnapshotPath(canonical, oid)} {
		configPath := filepath.Join(repoPath, "config")
		if repoPath == exactGitSnapshotPath(canonical, oid) {
			configPath = filepath.Join(repoPath, ".git", "config")
		}
		raw, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "super-secret-token") || strings.Contains(strings.ToLower(string(raw)), "remote \"origin\"") {
			t.Fatalf("credential-bearing origin persisted in %s: %s", configPath, raw)
		}
		info, err := os.Stat(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", configPath, info.Mode().Perm())
		}
	}
	if err := filepath.WalkDir(exactGitRoot(), func(path string, entry os.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "super-secret-token") {
			t.Errorf("fetch credential persisted in %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestExactGitLockHonorsContextDeadline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	release, err := acquireExactGitFileLock(context.Background(), "held", unix.LOCK_EX)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err = acquireExactGitFileLock(ctx, "held", unix.LOCK_EX)
	if !errors.Is(err, ErrExactGitLockTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock error = %v, want timeout and deadline", err)
	}
}

func TestExactGitNamespaceReadOnlyLeaseFailsClosedWithoutCreatingState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	release, err := AcquireExactGitNamespaceReadOnlyLeaseWithContext(context.Background())
	if release != nil {
		release()
	}
	if !errors.Is(err, ErrExactGitCoordinationUnavailable) {
		t.Fatalf("read-only namespace lease error = %v, want coordination unavailable", err)
	}
	if _, statErr := os.Lstat(CacheDir()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("read-only namespace lease created cache state: %v", statErr)
	}
}

func TestExactGitNamespaceReadOnlyLeasePreservesCoordinationMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	release, err := AcquireExactGitNamespaceLease()
	if err != nil {
		t.Fatal(err)
	}
	release()
	lockDir := filepath.Join(exactGitRoot(), "locks")
	lockPath := filepath.Join(lockDir, "namespace.lock")
	oldTime := time.Unix(1_700_000_000, 0)
	for _, path := range []string{lockDir, lockPath} {
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatal(err)
		}
	}
	beforeDir, err := os.Stat(lockDir)
	if err != nil {
		t.Fatal(err)
	}
	beforeLock, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	release, err = AcquireExactGitNamespaceReadOnlyLeaseWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	afterDir, err := os.Stat(lockDir)
	if err != nil {
		t.Fatal(err)
	}
	afterLock, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name          string
		before, after os.FileInfo
	}{{"lock directory", beforeDir, afterDir}, {"namespace lock", beforeLock, afterLock}} {
		if item.after.Mode() != item.before.Mode() || !item.after.ModTime().Equal(item.before.ModTime()) || item.after.Size() != item.before.Size() {
			t.Fatalf("%s metadata changed: before mode=%v mtime=%v size=%d; after mode=%v mtime=%v size=%d", item.name, item.before.Mode(), item.before.ModTime(), item.before.Size(), item.after.Mode(), item.after.ModTime(), item.after.Size())
		}
	}
}

func TestExactGitNamespaceLeaseHonorsContextDeadline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	release, err := acquireExactGitFileLock(context.Background(), "namespace", unix.LOCK_EX)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err = AcquireExactGitNamespaceLeaseWithContext(ctx)
	if !errors.Is(err, ErrExactGitLockTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("namespace lock error = %v, want timeout and deadline", err)
	}
}

func TestExactGitReadOnlyConsumptionBlocksGarbageCollection(t *testing.T) {
	for _, acquire := range []bool{false, true} {
		t.Run(fmt.Sprintf("acquire=%t", acquire), func(t *testing.T) {
			testExactGitConsumptionBlocksGarbageCollection(t, acquire)
		})
	}
}

func testExactGitConsumptionBlocksGarbageCollection(t *testing.T, acquire bool) {
	t.Setenv("HOME", t.TempDir())
	repo := newExactGitRepository(t, "sha1")
	canonical := "https://example.invalid/read-lease"
	oid := resolveExactGitRefForTest(t, canonical, repo, "main")
	if _, err := AcquireExactGit(canonical, repo, oid); err != nil {
		t.Fatal(err)
	}

	withSnapshot := func(consume func(string) error) error {
		return WithExactGitReadOnly(context.Background(), canonical, oid, consume)
	}
	if acquire {
		withSnapshot = func(consume func(string) error) error {
			return WithAcquiredExactGit(context.Background(), canonical, repo, oid, consume)
		}
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	consumeDone := make(chan error, 1)
	go func() {
		consumeDone <- withSnapshot(func(path string) error {
			close(entered)
			<-release
			raw, err := os.ReadFile(filepath.Join(path, "resource.txt"))
			if err != nil {
				return err
			}
			if got := string(raw); got != "first\n" {
				return fmt.Errorf("consumed snapshot = %q", got)
			}
			return nil
		})
	}()
	<-entered

	gcDone := make(chan error, 1)
	go func() {
		_, err := garbageCollectExactGit(false, nil)
		gcDone <- err
	}()
	select {
	case err := <-gcDone:
		t.Fatalf("garbage collection completed during exact snapshot consumption: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-consumeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-gcDone; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(exactGitSnapshotPath(canonical, oid)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("garbage collection did not remove unreferenced snapshot: %v", err)
	}
}

func TestExactGitNamespaceLeaseBlocksGarbageCollection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	release, err := AcquireExactGitNamespaceLease()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := garbageCollectExactGit(false, nil)
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("garbage collection completed while lease held: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("garbage collection remained blocked after lease release")
	}
}

func TestExactGitSupportsRepositoryObjectFormats(t *testing.T) {
	for _, algorithm := range []string{"sha1", "sha256"} {
		t.Run(algorithm, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			repo := newExactGitRepository(t, algorithm)
			oid := resolveExactGitRefForTest(t, "https://example.invalid/"+algorithm, repo, "main")
			if oid.Algorithm() != algorithm {
				t.Fatalf("resolved algorithm = %q, want %q", oid.Algorithm(), algorithm)
			}
			path, err := AcquireExactGit("https://example.invalid/"+algorithm, repo, oid)
			if err != nil {
				t.Fatalf("AcquireExactGit: %v", err)
			}
			assertExactHead(t, path, oid.Hex())
		})
	}
}

func resolveExactGitRefForTest(t testing.TB, canonical, repo, ref string) GitOID {
	t.Helper()
	oid, err := ResolveExactGitRef(canonical, repo, ref)
	if err != nil {
		t.Fatalf("ResolveExactGitRef: %v", err)
	}
	return oid
}

func newExactGitRepository(t testing.TB, algorithm string) string {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")
	gitTest(t, "", "init", "--bare", "--object-format="+algorithm, bare)
	gitTest(t, "", "init", "--object-format="+algorithm, work)
	gitTest(t, work, "config", "user.email", "test@example.invalid")
	gitTest(t, work, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(work, "resource.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, work, "add", "resource.txt")
	gitTest(t, work, "commit", "-m", "first")
	gitTest(t, work, "remote", "add", "origin", bare)
	gitTest(t, work, "push", "origin", "HEAD:main")
	return bare
}

func commitExactGitFile(t testing.TB, bare, content string) string {
	t.Helper()
	work := filepath.Join(filepath.Dir(bare), "work")
	if err := os.WriteFile(filepath.Join(work, "resource.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, work, "add", "resource.txt")
	gitTest(t, work, "commit", "-m", strings.TrimSpace(content))
	gitTest(t, work, "push", "origin", "HEAD:main")
	return strings.TrimSpace(gitTest(t, work, "rev-parse", "HEAD"))
}

func gitTest(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func readExactFile(t testing.TB, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exactGitBlobFixture(tb testing.TB, algorithm string, size int) string {
	tb.Helper()
	tb.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	tb.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	root := tb.TempDir()
	gitTest(tb, "", "init", "--object-format="+algorithm, root)
	if err := os.WriteFile(filepath.Join(root, "blob"), []byte(strings.Repeat("x", size)), 0o644); err != nil {
		tb.Fatal(err)
	}
	gitTest(tb, root, "add", "blob")
	gitTest(tb, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "blob")
	return root
}

func TestExactGitTrackedTreeLargeBlobMemory(t *testing.T) {
	const size = 8 << 20
	for _, algorithm := range []string{"sha1", "sha256"} {
		t.Run(algorithm, func(t *testing.T) {
			root := exactGitBlobFixture(t, algorithm, size)
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			err := verifyExactGitTrackedTreeWithContext(context.Background(), root, algorithm)
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatal(err)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= size/2 {
				t.Fatalf("verification allocated %d bytes for an %d-byte blob; want less than %d", allocated, size, size/2)
			}
		})
	}
}

func TestGitBlobOIDIndependentVectors(t *testing.T) {
	for _, tc := range []struct {
		name, data, sha1, sha256 string
	}{
		{"empty", "", "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391", "473a0f4c3be8a93681a267e3b1e9a7dcda1185436fe141f7749120a303721813"},
		{"binary", "\x00\xff\n\r\x80", "f8954e10bf6f369a02d0da43befdd00802f45fab", "dbbbd0d5e6e83d4e8e2c607d6720c27052219f69b217a0c255b9c94bac733bfa"},
		{"large", strings.Repeat("x", 8<<20), "1f6907f8a73acbeff2b1264800de17aa2cfc7b16", "11b606d886b40798e39234f97e41dc924bf7449da5c3c78e65cb170aa16a3b52"},
	} {
		for algorithm, want := range map[string]string{"sha1": tc.sha1, "sha256": tc.sha256} {
			t.Run(tc.name+"/"+algorithm, func(t *testing.T) {
				for _, reader := range []io.Reader{strings.NewReader(tc.data), struct{ io.Reader }{strings.NewReader(tc.data)}} {
					got, err := gitBlobOID(algorithm, reader, int64(len(tc.data)))
					if err != nil || got != want {
						t.Fatalf("blob OID = %s, %v; want %s", got, err, want)
					}
				}
			})
		}
	}
}

func TestGitBlobOIDRejectsSizeMismatchAndReadError(t *testing.T) {
	for _, algorithm := range []string{"sha1", "sha256"} {
		t.Run(algorithm, func(t *testing.T) {
			for _, size := range []int64{0, 2, 4} {
				got, err := gitBlobOID(algorithm, strings.NewReader("abc"), size)
				if !errors.Is(err, errGitBlobSizeMismatch) || got != "" {
					t.Fatalf("size %d: OID = %q, error = %v; want size mismatch and no OID", size, got, err)
				}
			}
			readErr := errors.New("fixture read failure")
			reader := io.MultiReader(strings.NewReader("abc"), iotest.ErrReader(readErr))
			got, err := gitBlobOID(algorithm, reader, 3)
			if !errors.Is(err, readErr) || got != "" {
				t.Fatalf("OID = %q, error = %v; want read failure and no OID", got, err)
			}
		})
	}
}

func TestExactGitTrackedTreePreservesExecutableAndSymlinkModes(t *testing.T) {
	for _, algorithm := range []string{"sha1", "sha256"} {
		t.Run(algorithm, func(t *testing.T) {
			root := exactGitBlobFixture(t, algorithm, 4096)
			blob := filepath.Join(root, "blob")
			if err := os.Chmod(blob, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("blob", filepath.Join(root, "link")); err != nil {
				t.Fatal(err)
			}
			gitTest(t, root, "add", "blob", "link")
			gitTest(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "modes")
			if err := verifyExactGitTrackedTreeWithContext(context.Background(), root, algorithm); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(blob, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := verifyExactGitTrackedTreeWithContext(context.Background(), root, algorithm); err == nil {
				t.Fatal("accepted removed executable bits")
			}
			if err := os.Remove(blob); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("link", blob); err != nil {
				t.Fatal(err)
			}
			if err := verifyExactGitTrackedTreeWithContext(context.Background(), root, algorithm); err == nil {
				t.Fatal("accepted a symlink in place of a regular file")
			}
		})
	}
}

func assertExactHead(t testing.TB, root, want string) {
	t.Helper()
	if got := strings.TrimSpace(gitTest(t, root, "rev-parse", "HEAD")); got != want {
		t.Fatalf("HEAD = %q, want %q", got, want)
	}
}
