package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// countingProbe installs a deterministic stand-in for the git probe and
// returns a counter of how many times it actually forked (conceptually — the
// stand-in never forks). The real cache is cleared before and after, so the
// test is independent of anything a sibling test memoized and leaves nothing
// behind for the next one (including a second `-count=2` pass).
func countingProbe(t *testing.T, roots map[string]string) *atomic.Int64 {
	t.Helper()
	var calls atomic.Int64
	previous := gitRootProbe
	ResetRepoRootCache()
	gitRootProbe = func(dir string) (string, bool) {
		calls.Add(1)
		return roots[dir], true
	}
	t.Cleanup(func() {
		gitRootProbe = previous
		ResetRepoRootCache()
	})
	return &calls
}

// TestGitRootForResolvesAtMostOncePerDirectory is the acceptance criterion:
// "the repo root is resolved at most once per process invocation". Every case
// asks for the same directory repeatedly and asserts a single resolution plus
// a stable answer.
func TestGitRootForResolvesAtMostOncePerDirectory(t *testing.T) {
	cases := []struct {
		name  string
		dir   string
		roots map[string]string
		calls int
		want  string
	}{
		{
			name:  "inside a git repo",
			dir:   "/repo/sub/dir",
			roots: map[string]string{"/repo/sub/dir": "/repo"},
			calls: 8,
			want:  "/repo",
		},
		{
			name:  "at the repo root itself",
			dir:   "/repo",
			roots: map[string]string{"/repo": "/repo"},
			calls: 3,
			want:  "/repo",
		},
		{
			name: "outside any git repo caches the negative answer",
			dir:  "/not/a/repo",
			// No entry: the probe returns "" for this directory.
			roots: map[string]string{},
			calls: 8,
			want:  "",
		},
		{
			name:  "a single lookup still resolves exactly once",
			dir:   "/repo",
			roots: map[string]string{"/repo": "/repo"},
			calls: 1,
			want:  "/repo",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := countingProbe(t, tc.roots)

			for i := 0; i < tc.calls; i++ {
				if got := gitRootFor(tc.dir); got != tc.want {
					t.Fatalf("gitRootFor(%q) call %d = %q; want %q", tc.dir, i+1, got, tc.want)
				}
			}

			if got := calls.Load(); got != 1 {
				t.Fatalf("git probe ran %d times for %d lookups; want exactly 1", got, tc.calls)
			}
		})
	}
}

// TestGitRootForKeysOnDirectory proves the memo is keyed on its input, so a
// test (or a command) that changes working directory cannot read another
// directory's answer.
func TestGitRootForKeysOnDirectory(t *testing.T) {
	roots := map[string]string{
		"/repo-a/sub": "/repo-a",
		"/repo-b":     "/repo-b",
	}
	calls := countingProbe(t, roots)

	lookups := []struct {
		dir  string
		want string
	}{
		{"/repo-a/sub", "/repo-a"},
		{"/repo-b", "/repo-b"},
		{"/nowhere", ""},
		{"/repo-a/sub", "/repo-a"},
		{"/repo-b", "/repo-b"},
		{"/nowhere", ""},
	}
	for i, l := range lookups {
		if got := gitRootFor(l.dir); got != l.want {
			t.Fatalf("lookup %d: gitRootFor(%q) = %q; want %q", i, l.dir, got, l.want)
		}
	}

	if got := calls.Load(); got != 3 {
		t.Fatalf("git probe ran %d times for 3 distinct directories; want 3", got)
	}
}

// TestResetRepoRootCacheForcesReresolution verifies the cache is explicitly
// resettable, which is what lets a test change a directory's git boundary in
// place without leaking a stale answer.
func TestResetRepoRootCacheForcesReresolution(t *testing.T) {
	roots := map[string]string{"/repo": "/repo"}
	calls := countingProbe(t, roots)

	if got := gitRootFor("/repo"); got != "/repo" {
		t.Fatalf("gitRootFor = %q; want /repo", got)
	}
	if got := gitRootFor("/repo"); got != "/repo" {
		t.Fatalf("cached gitRootFor = %q; want /repo", got)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("probe ran %d times before reset; want 1", got)
	}

	ResetRepoRootCache()

	// The boundary "moved" while the process was running; after a reset the
	// new answer is observed.
	roots["/repo"] = "/outer"
	if got := gitRootFor("/repo"); got != "/outer" {
		t.Fatalf("gitRootFor after reset = %q; want /outer", got)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("probe ran %d times after reset; want 2", got)
	}
}

// TestGitRootForConcurrent exercises the memo from many goroutines, which is
// how the TUI reaches it (snapshot loads and background refreshes run as
// tea.Cmds). Run under `go test -race` it also proves the map is guarded.
func TestGitRootForConcurrent(t *testing.T) {
	calls := countingProbe(t, map[string]string{"/repo": "/repo"})

	const goroutines = 64
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]string, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = gitRootFor("/repo")
		}(i)
	}
	close(start)
	wg.Wait()

	for i, got := range results {
		if got != "/repo" {
			t.Fatalf("goroutine %d got %q; want /repo", i, got)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("git probe ran %d times across %d concurrent lookups; want exactly 1", got, goroutines)
	}
}

// TestFindRepoRootReresolvesConfigBoundaryWithinAProcess is the regression
// test for the reason only the git boundary is memoized. `install --scope
// repo` in a monorepo subdirectory writes a skeleton .agents/config.yaml into
// cwd and then expects FindRepoRoot to return cwd on the next call inside the
// SAME process. A cache of the whole result would return the parent.
func TestFindRepoRootReresolvesConfigBoundaryWithinAProcess(t *testing.T) {
	repoRoot := t.TempDir()
	writeAgentsConfig(t, repoRoot)
	subDir := filepath.Join(repoRoot, "services", "service-a")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pretend the whole temp tree is one git repo, without shelling out.
	roots := map[string]string{}
	realRepoRoot := canonicalPath(t, repoRoot)
	realSubDir := canonicalPath(t, subDir)
	roots[repoRoot] = realRepoRoot
	roots[subDir] = realRepoRoot
	roots[realRepoRoot] = realRepoRoot
	roots[realSubDir] = realRepoRoot
	calls := countingProbe(t, roots)

	withWorkingDir(t, subDir)

	if got, want := canonicalPath(t, FindRepoRoot()), realRepoRoot; got != want {
		t.Fatalf("FindRepoRoot() before seeding subdir config = %q; want %q", got, want)
	}

	// The install flow seeds a project config in cwd.
	writeAgentsConfig(t, subDir)

	if got, want := canonicalPath(t, FindRepoRoot()), realSubDir; got != want {
		t.Fatalf("FindRepoRoot() after seeding subdir config = %q; want %q (the .agents boundary must not be cached)", got, want)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("git probe ran %d times across two FindRepoRoot calls; want 1", got)
	}
}

// TestFindRepoRootOutsideGitRepoIsUnchanged pins the out-of-repo behaviour the
// bead requires to stay identical: with no git boundary at all, the walk still
// finds a .agents/config.yaml, and with neither it falls back to cwd. Repeated
// calls return the same answer (the negative git result is cached, not retried
// into a different answer).
func TestFindRepoRootOutsideGitRepoIsUnchanged(t *testing.T) {
	cases := []struct {
		name        string
		agentsAt    string // "" = no .agents/config.yaml anywhere
		wantSubdir  bool   // want the subdir (cwd) rather than the tree root
		description string
	}{
		{
			name:       "config at the tree root",
			agentsAt:   "root",
			wantSubdir: false,
		},
		{
			name:       "config in the working directory",
			agentsAt:   "sub",
			wantSubdir: true,
		},
		{
			name:       "no config anywhere falls back to cwd",
			agentsAt:   "",
			wantSubdir: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The probe reports "not a git repo" for every directory.
			calls := countingProbe(t, map[string]string{})

			treeRoot := t.TempDir()
			subDir := filepath.Join(treeRoot, "a", "b")
			if err := os.MkdirAll(subDir, 0o755); err != nil {
				t.Fatal(err)
			}
			switch tc.agentsAt {
			case "root":
				writeAgentsConfig(t, treeRoot)
			case "sub":
				writeAgentsConfig(t, subDir)
			}

			withWorkingDir(t, subDir)

			want := canonicalPath(t, treeRoot)
			if tc.wantSubdir {
				want = canonicalPath(t, subDir)
			}

			first := canonicalPath(t, FindRepoRoot())
			if first != want {
				t.Fatalf("FindRepoRoot() = %q; want %q", first, want)
			}
			second := canonicalPath(t, FindRepoRoot())
			if second != first {
				t.Fatalf("second FindRepoRoot() = %q; want the same answer %q", second, first)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("git probe ran %d times for two calls outside a repo; want 1", got)
			}
		})
	}
}

// TestProbeGitRootRealBinary checks the un-memoized probe against the real git
// binary, so the memo is never the only thing under test. It creates a
// temporary Git worktree to stay independent of the checkout.
func TestProbeGitRootRealBinary(t *testing.T) {
	repoRoot := t.TempDir()
	if err := exec.Command("git", "init", repoRoot).Run(); err != nil {
		t.Skipf("git not available or cannot init repo: %v", err)
	}
	subDir := filepath.Join(repoRoot, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	root, ok := probeGitRoot(subDir)
	if !ok {
		t.Fatalf("probeGitRoot(%q) failed", subDir)
	}
	if root == "" {
		t.Fatalf("probeGitRoot(%q) returned empty root for a git worktree", subDir)
	}
	if !isWithinRoot(subDir, root) {
		t.Fatalf("probeGitRoot(%q) = %q, which does not contain the working directory", subDir, root)
	}
	if !fileExists(filepath.Join(root, ".git")) {
		t.Fatalf("probeGitRoot returned %q, which has no .git entry", root)
	}

	// A directory that is definitively outside any worktree: the temp path has
	// no .git anywhere above it in the sandbox sense we care about here, so
	// only assert the shape of the answer, never a specific path.
	outside, ok := probeGitRoot(filepath.Join(t.TempDir(), "does-not-exist"))
	if !ok {
		t.Fatalf("probeGitRoot on a nonexistent directory failed")
	}
	if outside != "" {
		t.Fatalf("probeGitRoot on a nonexistent directory = %q; want \"\"", outside)
	}
}

func writeAgentsConfig(t *testing.T, dir string) {
	t.Helper()
	agentsDir := filepath.Join(dir, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "apiVersion: agent-marketplace/v2\nharnesses:\n  - factory\n"
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
