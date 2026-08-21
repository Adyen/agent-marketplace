//go:build unix

package cache

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// TestRegistryCacheLockIsExclusive proves the serialization is real rather than
// silently degraded: two independent handles on the same cache directory — which
// is what two processes have — cannot hold the lock at once.
func TestRegistryCacheLockIsExclusive(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	url := "https://example.com/org/registry.git"
	holder, err := openCacheStore(CacheDir(), HashURL(url))
	if err != nil {
		t.Fatalf("open holder store: %v", err)
	}
	defer holder.close()
	contender, err := openCacheStore(CacheDir(), HashURL(url))
	if err != nil {
		t.Fatalf("open contender store: %v", err)
	}
	defer contender.close()

	if err := holder.lock(); err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if err := flockDir(contender.dir(), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, errLockBusy) {
		t.Fatalf("expected the contender to be refused while the lock is held, got %v", err)
	}

	holder.unlock()
	if err := flockDir(contender.dir(), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("expected the lock to be free after release, got %v", err)
	}
	if err := unlockDir(contender.dir()); err != nil {
		t.Fatalf("release contender lock: %v", err)
	}
}

// Environment variables through which the parent test drives the subprocess
// helper below. They also gate the helper, so it is inert during a normal run.
const (
	subprocessHomeEnv   = "AGENT_MARKETPLACE_TEST_CACHE_HOME"
	subprocessOriginEnv = "AGENT_MARKETPLACE_TEST_CACHE_ORIGIN"
)

// TestCloneOrPullConvergesAcrossProcesses is the cross-process half of the
// convergence guarantee. Goroutines share a lock table; separate processes do not,
// so only this test shows that the advisory lock and the convergent publication
// hold where they actually have to.
func TestCloneOrPullConvergesAcrossProcesses(t *testing.T) {
	sharedHome := t.TempDir()
	t.Setenv("HOME", sharedHome)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "across processes\n", "initial commit")

	const processes = 4
	outputs := make([]string, processes)
	errs := make([]error, processes)
	var wait sync.WaitGroup
	for i := range processes {
		wait.Add(1)
		go func() {
			defer wait.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestCloneOrPullSubprocessHelper$", "-test.v")
			cmd.Env = append(os.Environ(),
				subprocessHomeEnv+"="+sharedHome,
				subprocessOriginEnv+"="+origin,
			)
			output, err := cmd.CombinedOutput()
			outputs[i], errs[i] = string(output), err
		}()
	}
	wait.Wait()

	for i := range processes {
		if errs[i] != nil {
			t.Fatalf("subprocess %d failed: %v\n%s", i, errs[i], outputs[i])
		}
	}

	cachePath := filepath.Join(sharedHome, ".cache", "agent-marketplace", HashURL(origin), cacheRefComponent("main"))
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "across processes\n")
	assertNoScratchEntries(t, filepath.Dir(cachePath))
}

// TestCloneOrPullSubprocessHelper is not a test of its own: it is the body of the
// child processes TestCloneOrPullConvergesAcrossProcesses spawns, and it skips
// unless that parent asked for it.
func TestCloneOrPullSubprocessHelper(t *testing.T) {
	home := os.Getenv(subprocessHomeEnv)
	origin := os.Getenv(subprocessOriginEnv)
	if home == "" || origin == "" {
		t.Skip("driven by TestCloneOrPullConvergesAcrossProcesses")
	}
	t.Setenv("HOME", home)

	cachePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("clone or pull: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cachePath, "skill.txt"))
	if err != nil {
		t.Fatalf("read published entry: %v", err)
	}
	if strings.TrimSpace(string(data)) != "across processes" {
		t.Fatalf("published entry contains %q", data)
	}
}
