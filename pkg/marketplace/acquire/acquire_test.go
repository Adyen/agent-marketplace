package acquire

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCloneValidRepo(t *testing.T) {
	repo := createGitRepo(t, map[string]string{
		"README.md": "hello",
	})

	runGitTestCommand(t, repo, "git", "update-ref", "refs/pull/1/head", "HEAD")
	for _, ref := range []string{"refs/heads/main", "origin/main", "refs/pull/1/head"} {
		t.Run(ref, func(t *testing.T) {
			res, err := Clone(context.Background(), repo, ref, Credentials{}, Limits{}, time.Minute)
			if err != nil {
				t.Fatalf("Clone: %v", err)
			}
			defer func() { _ = res.Release() }()

			data, err := os.ReadFile(filepath.Join(res.Dir, "README.md"))
			if err != nil {
				t.Fatalf("read cloned README: %v", err)
			}
			if string(data) != "hello" {
				t.Fatalf("README = %q, want hello", data)
			}
			if len(res.Commit) != 40 {
				t.Fatalf("commit = %q, want 40-char hex", res.Commit)
			}
		})
	}
}

func TestCloneAtCommitRequiresAndChecksOutExactCommit(t *testing.T) {
	repo := createGitRepo(t, map[string]string{"README.md": "hello"})
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repo
	commitBytes, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.TrimSpace(string(commitBytes))
	res, err := CloneAtCommit(context.Background(), repo, "main", commit, Credentials{}, Limits{}, time.Minute)
	if err != nil {
		t.Fatalf("CloneAtCommit: %v", err)
	}
	defer func() { _ = res.Release() }()
	if res.Commit != commit {
		t.Fatalf("commit = %q, want %q", res.Commit, commit)
	}
	headBytes, err := exec.Command("git", "-C", res.Dir, "rev-parse", "--verify", "HEAD").Output()
	if err != nil {
		t.Fatalf("verify cloned HEAD: %v", err)
	}
	if got := strings.TrimSpace(string(headBytes)); got != commit {
		t.Fatalf("cloned HEAD = %q, want %q", got, commit)
	}
	if _, err := CloneAtCommit(context.Background(), repo, "main", strings.Repeat("a", 40), Credentials{}, Limits{}, time.Minute); err == nil {
		t.Fatal("expected unavailable exact commit to fail")
	}
}

func TestCloneAtCommitDoesNotFollowMovingRefTip(t *testing.T) {
	repo := createGitRepo(t, map[string]string{"selected/value.txt": "first", "unrelated/ignored.txt": "ignored"})
	firstBytes, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	first := strings.TrimSpace(string(firstBytes))
	if err := os.WriteFile(filepath.Join(repo, "selected", "value.txt"), []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "selected/value.txt")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-q", "-m", "second")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	secondBytes, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	second := strings.TrimSpace(string(secondBytes))
	if first == second {
		t.Fatal("test repository did not create two commits")
	}

	res, err := CloneAtCommit(context.Background(), repo, "main", first, Credentials{}, Limits{}, time.Minute, "selected")
	if err != nil {
		t.Fatalf("CloneAtCommit pinned commit: %v", err)
	}
	defer func() { _ = res.Release() }()
	if res.Commit != first {
		t.Fatalf("commit = %q, want pinned %q", res.Commit, first)
	}
	got, err := os.ReadFile(filepath.Join(res.Dir, "selected", "value.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Fatalf("pinned tree = %q, want first; ref tip was %s", got, second)
	}
	if _, err := os.Stat(filepath.Join(res.Dir, "unrelated")); !os.IsNotExist(err) {
		t.Fatalf("unselected root exists or returned unexpected error: %v", err)
	}
}

func TestCloneAcceptsCommitRef(t *testing.T) {
	repo := createGitRepo(t, map[string]string{"selected/value.txt": "value"})
	commitBytes, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.TrimSpace(string(commitBytes))

	res, err := Clone(context.Background(), repo, commit, Credentials{}, Limits{}, time.Minute, "selected")
	if err != nil {
		t.Fatalf("Clone commit ref: %v", err)
	}
	defer func() { _ = res.Release() }()
	if res.Commit != commit {
		t.Fatalf("commit = %q, want %q", res.Commit, commit)
	}
}

func TestCloneInvalidURL(t *testing.T) {
	_, err := Clone(context.Background(), "--option", "main", Credentials{}, Limits{}, time.Minute)
	if err == nil {
		t.Fatalf("expected error for option-like URL")
	}
}

func TestCloneInvalidRef(t *testing.T) {
	repo := createGitRepo(t, map[string]string{
		"README.md": "hello",
	})

	_, err := Clone(context.Background(), repo, "--bad", Credentials{}, Limits{}, time.Minute)
	if err == nil {
		t.Fatalf("expected error for option-like ref")
	}
}

func TestCloneOnlyAcquiresSelectedRoots(t *testing.T) {
	repo := createGitRepo(t, map[string]string{
		"skills[team]/review/SKILL.md": "---\nname: review\ndescription: Review code\n---\nReview.\n",
		"large.bin":                    strings.Repeat("x", 1024),
	})

	runGitTestCommand(t, repo, "git", "config", "uploadpack.allowFilter", "true")
	limits := Limits{RepositoryBytes: 128, FileCount: 2, FileBytes: 128}
	res, err := Clone(context.Background(), "file://"+repo, "main", Credentials{}, limits, time.Minute, "skills[team]")
	if err != nil {
		t.Fatalf("Clone selected root: %v", err)
	}
	defer func() { _ = res.Release() }()

	if _, err := os.Stat(filepath.Join(res.Dir, ".git", "shallow")); err != nil {
		t.Fatalf("clone is not shallow: %v", err)
	}
	promisor, err := exec.Command("git", "-C", res.Dir, "config", "--get", "remote.origin.promisor").Output()
	if err != nil || strings.TrimSpace(string(promisor)) != "true" {
		t.Fatalf("partial clone promisor = %q, err = %v", promisor, err)
	}
	if _, err := os.Stat(filepath.Join(res.Dir, "skills[team]", "review", "SKILL.md")); err != nil {
		t.Fatalf("selected skill missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res.Dir, "large.bin")); !os.IsNotExist(err) {
		t.Fatalf("unselected file exists or returned unexpected error: %v", err)
	}
}

func TestCloneRejectsInvalidRoot(t *testing.T) {
	repo := createGitRepo(t, map[string]string{"README.md": "hello"})
	for _, root := range []string{"../skills", "/skills", "-skills", "skills\n/*"} {
		t.Run(root, func(t *testing.T) {
			if _, err := Clone(context.Background(), repo, "main", Credentials{}, Limits{}, time.Minute, root); err == nil {
				t.Fatalf("Clone accepted invalid root %q", root)
			}
		})
	}
}

func TestEnforceTreeLimitsDoesNotFetchMissingBlobs(t *testing.T) {
	repo := createGitRepo(t, map[string]string{
		"selected/a.txt": "a",
		"selected/b.txt": "b",
	})
	runGitTestCommand(t, repo, "git", "config", "uploadpack.allowFilter", "true")
	cloneDir := filepath.Join(t.TempDir(), "clone")
	workspace, err := newWorkspace(Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspace.cleanup() }()
	ctx := context.Background()
	if _, err := runGit(ctx, workspace.env(), "", append(hardenedGitArgs(), "init", "--", cloneDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, workspace.env(), cloneDir, append(hardenedGitArgs(), "remote", "add", "origin", "file://"+repo)); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, workspace.env(), cloneDir, fetchArgs("main", true)); err != nil {
		t.Fatal(err)
	}
	commitBytes, err := runGit(ctx, workspace.env(), cloneDir, append(hardenedGitArgs(), "rev-parse", "FETCH_HEAD^{commit}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, workspace.env(), cloneDir, append(hardenedGitArgs(), "remote", "set-url", "origin", "file:///missing")); err != nil {
		t.Fatal(err)
	}
	if err := enforceTreeLimits(ctx, workspace.env(), cloneDir, strings.TrimSpace(string(commitBytes)), []string{"selected"}, Limits{FileCount: 2}); err != nil {
		t.Fatalf("enforceTreeLimits fetched missing blobs: %v", err)
	}
}

func TestCloneEnforcesFileBytes(t *testing.T) {
	repo := createGitRepo(t, map[string]string{
		"small.txt": strings.Repeat("x", 100),
		"large.txt": strings.Repeat("x", 200),
	})
	runGitTestCommand(t, repo, "git", "config", "uploadpack.allowFilter", "true")

	limits := Limits{RepositoryBytes: 1024 * 1024, FileCount: 1024, FileBytes: 150}
	_, err := Clone(context.Background(), "file://"+repo, "main", Credentials{}, limits, time.Minute)
	if err == nil {
		t.Fatalf("expected error for file exceeding file_bytes")
	}
	if !strings.Contains(err.Error(), "file_bytes") {
		t.Fatalf("expected file_bytes limit error, got %v", err)
	}
}

func TestCloneEnforcesFileCount(t *testing.T) {
	files := make(map[string]string)
	for i := 0; i < 10; i++ {
		files[fmt.Sprintf("file%d.txt", i)] = "x"
	}
	repo := createGitRepo(t, files)

	limits := Limits{RepositoryBytes: 1024 * 1024, FileCount: 5, FileBytes: 1024}
	_, err := Clone(context.Background(), repo, "main", Credentials{}, limits, time.Minute)
	if err == nil {
		t.Fatalf("expected error for file count exceeding limit")
	}
}

func TestCloneEnforcesRepositoryBytes(t *testing.T) {
	repo := createGitRepo(t, map[string]string{
		"big.txt": strings.Repeat("x", 500),
	})

	limits := Limits{RepositoryBytes: 100, FileCount: 1024, FileBytes: 1024}
	_, err := Clone(context.Background(), repo, "main", Credentials{}, limits, time.Minute)
	if err == nil {
		t.Fatalf("expected error for repository bytes exceeding limit")
	}
}

func TestCloneDereferencesLocalSymlinks(t *testing.T) {
	repo := createGitRepo(t, map[string]string{
		"docs/real.txt":       "safe",
		"docs/real/nested.md": "nested",
	})
	addSymlink(t, repo, "real.txt", "docs/file-link.txt")
	addSymlink(t, repo, "file-link.txt", "docs/chained-link.txt")
	addSymlink(t, repo, "real", "docs/dir-link")
	commitAll(t, repo, "add local symlinks")

	res, err := Clone(context.Background(), repo, "main", Credentials{}, Limits{}, time.Minute)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	defer func() { _ = res.Release() }()

	for path, want := range map[string]string{
		"docs/file-link.txt":      "safe",
		"docs/chained-link.txt":   "safe",
		"docs/dir-link/nested.md": "nested",
	} {
		full := filepath.Join(res.Dir, path)
		info, err := os.Lstat(full)
		if err != nil {
			t.Fatalf("lstat %q: %v", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("%q remained a symlink", path)
		}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(full)
			if err != nil {
				t.Fatalf("read %q: %v", path, err)
			}
			if string(data) != want {
				t.Fatalf("%q = %q, want %q", path, data, want)
			}
		}
	}
	dirInfo, err := os.Lstat(filepath.Join(res.Dir, "docs/dir-link"))
	if err != nil {
		t.Fatal(err)
	}
	if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("directory link mode = %v", dirInfo.Mode())
	}
}

func TestCloneRejectsUnsafeSymlinks(t *testing.T) {
	tests := []struct {
		name    string
		links   map[string]string
		wantErr string
	}{
		{name: "absolute", links: map[string]string{"link": "/etc/passwd"}, wantErr: "absolute"},
		{name: "escape", links: map[string]string{"link": "../outside"}, wantErr: "escapes repository"},
		{name: "indirect escape", links: map[string]string{"link": "next", "next": "../outside"}, wantErr: "escapes repository"},
		{name: "broken", links: map[string]string{"link": "missing"}, wantErr: "broken"},
		{name: "cycle", links: map[string]string{"a": "b", "b": "a"}, wantErr: "cycle"},
		{name: "ancestor cycle", links: map[string]string{"docs/link": "."}, wantErr: "cycle"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := createGitRepo(t, map[string]string{"README.md": "safe"})
			for path, target := range tc.links {
				addSymlink(t, repo, target, path)
			}
			commitAll(t, repo, "add unsafe symlink")

			_, err := Clone(context.Background(), repo, "main", Credentials{}, Limits{}, time.Minute)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Clone error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestCloneEnforcesLimitsAfterSymlinkExpansion(t *testing.T) {
	repo := createGitRepo(t, map[string]string{"target.txt": strings.Repeat("x", 100)})
	addSymlink(t, repo, "target.txt", "copy.txt")
	commitAll(t, repo, "add expanding symlink")

	limits := Limits{RepositoryBytes: 150, FileCount: 10, FileBytes: 100}
	_, err := Clone(context.Background(), repo, "main", Credentials{}, limits, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "workspace size") {
		t.Fatalf("Clone error = %v, want expanded workspace size error", err)
	}
}

func TestSymlinkExpansionHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := dereferenceRepositorySymlinks(ctx, t.TempDir(), Limits{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dereferenceRepositorySymlinks error = %v", err)
	}
}

func TestRepositoryPathRejectsGitMetadataCaseInsensitively(t *testing.T) {
	root := t.TempDir()
	_, err := repositoryPath(root, filepath.Join(root, ".GIT", "config"))
	if err == nil || !strings.Contains(err.Error(), "repository metadata") {
		t.Fatalf("repositoryPath error = %v", err)
	}
}

func TestCloneContextCancel(t *testing.T) {
	repo := createGitRepo(t, map[string]string{
		"README.md": "hello",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Clone(ctx, repo, "main", Credentials{}, Limits{}, time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Clone error = %v, want context cancellation", err)
	}
}

func TestGitEnvUsesSourceCAFile(t *testing.T) {
	t.Setenv("GIT_SSL_CAINFO", "/ambient/ca.pem")
	env, err := gitEnv(Credentials{CAFile: "/source/ca.pem"}, "")
	if err != nil {
		t.Fatalf("gitEnv: %v", err)
	}
	got := envMap(env)
	if got["GIT_SSL_CAINFO"] != "/source/ca.pem" {
		t.Fatalf("GIT_SSL_CAINFO = %q", got["GIT_SSL_CAINFO"])
	}
	for name, want := range map[string]string{
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_CONFIG_GLOBAL":   "/dev/null",
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_LFS_SKIP_SMUDGE": "1",
	} {
		if got[name] != want {
			t.Fatalf("%s = %q, want %q", name, got[name], want)
		}
	}
}

func TestGitEnvDoesNotInheritAmbientCA(t *testing.T) {
	t.Setenv("GIT_SSL_CAINFO", "/ambient/ca.pem")
	env, err := gitEnv(Credentials{}, "")
	if err != nil {
		t.Fatalf("gitEnv: %v", err)
	}
	if _, ok := envMap(env)["GIT_SSL_CAINFO"]; ok {
		t.Fatal("ambient GIT_SSL_CAINFO survived acquisition filtering")
	}
}

func TestCloneUsesSourceCAFile(t *testing.T) {
	repo := createGitRepo(t, map[string]string{"README.md": "hello"})
	serverRoot := t.TempDir()
	bareRepo := filepath.Join(serverRoot, "repo.git")
	runGitTestCommand(t, serverRoot, "git", "clone", "-q", "--bare", repo, bareRepo)
	runGitTestCommand(t, serverRoot, "git", "--git-dir", bareRepo, "update-server-info")
	commitBytes, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("resolve test commit: %v", err)
	}
	commit := strings.TrimSpace(string(commitBytes))

	server := httptest.NewTLSServer(http.FileServer(http.Dir(serverRoot)))
	defer server.Close()
	origin := server.URL + "/repo.git"
	if res, err := CloneAtCommit(context.Background(), origin, "main", commit, Credentials{}, Limits{}, time.Minute); err == nil {
		_ = res.Release()
		t.Fatal("CloneAtCommit succeeded without the source CA")
	}

	caFile := filepath.Join(t.TempDir(), "source-ca.pem")
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caFile, cert, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := CloneAtCommit(context.Background(), origin, "main", commit, Credentials{CAFile: caFile}, Limits{}, time.Minute)
	if err != nil {
		t.Fatalf("CloneAtCommit with source CA: %v", err)
	}
	defer func() { _ = res.Release() }()
}

func envMap(values []string) map[string]string {
	out := make(map[string]string, len(values))
	for _, value := range values {
		name, value, _ := strings.Cut(value, "=")
		out[name] = value
	}
	return out
}

func createGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	for path, content := range files {
		full := filepath.Join(repo, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %q: %v", path, err)
		}
	}

	for _, c := range [][]string{
		{"git", "init", "-q", "--initial-branch=main"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test"},
	} {
		runGitTestCommand(t, repo, c...)
	}
	commitAll(t, repo, "initial")
	return repo
}

func addSymlink(t *testing.T, repo, target, path string) {
	t.Helper()
	full := filepath.Join(repo, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir symlink parent: %v", err)
	}
	if err := os.Symlink(target, full); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func commitAll(t *testing.T, repo, message string) {
	t.Helper()
	runGitTestCommand(t, repo, "git", "add", ".")
	runGitTestCommand(t, repo, "git", "commit", "-q", "-m", message)
}

func runGitTestCommand(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}
