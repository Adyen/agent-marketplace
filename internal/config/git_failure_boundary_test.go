package config

import (
	"path/filepath"
	"testing"
)

func TestFailedGitDiscoveryDoesNotAdoptParent(t *testing.T) {
	for _, kind := range []string{"broken-gitdir", "empty-git-dir", "corrupt-head"} {
		t.Run(kind, func(t *testing.T) {
			ResetRepoRootCache()
			t.Cleanup(ResetRepoRootCache)
			parent := t.TempDir()
			runGit(t, parent, "init")
			writeAgentsConfig(t, parent)
			child := filepath.Join(parent, "child")
			cwd := filepath.Join(child, "sub")
			mustMkdirAll(t, cwd)
			switch kind {
			case "broken-gitdir":
				mustWriteFile(t, filepath.Join(child, ".git"), []byte("gitdir: missing-metadata\n"))
			case "empty-git-dir":
				mustMkdirAll(t, filepath.Join(child, ".git"))
			case "corrupt-head":
				runGit(t, child, "init")
				mustWriteFile(t, filepath.Join(child, ".git", "HEAD"), []byte("invalid-head\n"))
			}
			if root, valid := probeGitRoot(cwd); valid || root != "" {
				t.Errorf("malformed Git boundary accepted: %q %t", root, valid)
			}
			withWorkingDir(t, cwd)
			if got, want := canonicalPath(t, FindRepoRoot()), canonicalPath(t, cwd); got != want {
				t.Fatalf("adopted parent across failed Git discovery: %q want %q", got, want)
			}
		})
	}
}

func TestGitProbeFailureIsBoundedAndRetried(t *testing.T) {
	parent := t.TempDir()
	cwd := filepath.Join(parent, "child")
	mustMkdirAll(t, cwd)
	writeAgentsConfig(t, parent)
	previous := gitRootProbe
	ResetRepoRootCache()
	t.Cleanup(func() { gitRootProbe = previous; ResetRepoRootCache() })
	calls := 0
	gitRootProbe = func(string) (string, bool) {
		calls++
		if calls < 3 {
			return "", false
		}
		return "", true
	}
	withWorkingDir(t, cwd)
	for range 2 {
		if got := canonicalPath(t, FindRepoRoot()); got != canonicalPath(t, cwd) {
			t.Fatalf("failed probe widened boundary: %s", got)
		}
	}
	if got := canonicalPath(t, FindRepoRoot()); got != canonicalPath(t, parent) {
		t.Fatalf("genuine no-repo discovery did not recover: %s", got)
	}
	FindRepoRoot()
	if calls != 3 {
		t.Fatalf("failure cached or success not cached: calls=%d", calls)
	}
}

func TestProbeGitRootRealNoRepository(t *testing.T) {
	root, valid := probeGitRoot(t.TempDir())
	if !valid || root != "" {
		t.Fatalf("genuine no-repo probe failed: %q %t", root, valid)
	}
}
