package cache

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"golang.org/x/sys/unix"
)

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
	got, err := AcquireExactGit(canonical, repo, oid)
	if err != nil {
		t.Fatalf("AcquireExactGit(tampered): %v", err)
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

func TestExactGitRejectsEscapingSymlink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newExactGitRepository(t, "sha1")
	work := filepath.Join(filepath.Dir(repo), "work")
	if err := os.Symlink("../outside", filepath.Join(work, "escape")); err != nil {
		t.Fatal(err)
	}
	gitTest(t, work, "add", "escape")
	gitTest(t, work, "commit", "-m", "symlink")
	gitTest(t, work, "push", "origin", "HEAD:main")

	oid := resolveExactGitRefForTest(t, "https://example.invalid/symlink", repo, "main")
	if _, err := AcquireExactGit("https://example.invalid/symlink", repo, oid); !errors.Is(err, ErrExactGitSnapshotUntrusted) {
		t.Fatalf("AcquireExactGit(symlink) error = %v, want ErrExactGitSnapshotUntrusted", err)
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
			if oid.Algorithm != algorithm {
				t.Fatalf("resolved algorithm = %q, want %q", oid.Algorithm, algorithm)
			}
			path, err := AcquireExactGit("https://example.invalid/"+algorithm, repo, oid)
			if err != nil {
				t.Fatalf("AcquireExactGit: %v", err)
			}
			assertExactHead(t, path, oid.Hex)
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

func assertExactHead(t testing.TB, root, want string) {
	t.Helper()
	if got := strings.TrimSpace(gitTest(t, root, "rev-parse", "HEAD")); got != want {
		t.Fatalf("HEAD = %q, want %q", got, want)
	}
}
