package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func TestTUIDependencyLifecyclePropagatesExactLockLeaseIntoSync(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot := t.TempDir()
	withWorkingDir(t, repoRoot)

	originalPrepare, originalSync := prepareTUILocks, runTUIExactSync
	t.Cleanup(func() {
		prepareTUILocks = originalPrepare
		runTUIExactSync = originalSync
	})
	leaseAcquired := false
	leaseReleased := false
	prepareTUILocks = func(context.Context, string, string, lockMode) (exactLockLeaseFunc, error) {
		return func(context.Context) (func(), error) {
			leaseAcquired = true
			return func() { leaseReleased = true }, nil
		}, nil
	}
	runTUIExactSync = func(ctx context.Context, opts pipeline.ResolveOptions) pipeline.SyncResult {
		if !opts.ReadOnlyExactLocks {
			t.Error("TUI sync did not request exact read-only locks")
		}
		if opts.ExactLockLease == nil {
			t.Fatal("TUI sync dropped the prepared exact lock lease")
		}
		release, err := opts.ExactLockLease(ctx)
		if err != nil {
			t.Fatal(err)
		}
		release()
		return pipeline.SyncResult{}
	}

	if err := runTUIDependencyLifecycle(context.Background(), mustWorkspace(t, repoRoot), false); err != nil {
		t.Fatal(err)
	}
	if !leaseAcquired || !leaseReleased {
		t.Fatalf("propagated lease lifecycle = acquired %t, released %t", leaseAcquired, leaseReleased)
	}
}

func TestConfiguredInstallKeepsValidLayerExactWhenAnotherLayerLockIsMissing(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	lockedOrigin, lockedWorking := createBareRepoWithWorkingClone(t, "locked-layer.git")
	writeSkill(t, lockedWorking, "reviewed", "Reviewed before locking", nil)
	commitAndPushAll(t, lockedWorking, "seed locked layer")

	bootstrapOrigin, bootstrapWorking := createBareRepoWithWorkingClone(t, "bootstrap-layer.git")
	writeSkill(t, bootstrapWorking, "bootstrap", "Available from missing-lock layer", nil)
	commitAndPushAll(t, bootstrapWorking, "seed bootstrap layer")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
registries:
  - url: `+lockedOrigin+`
    ref: main
`)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses: [factory]
registries:
  - url: `+bootstrapOrigin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("lock", "--scope", "user"); err != nil {
		t.Fatalf("lock user layer: %v\n%s", err, output)
	}
	writeSkill(t, lockedWorking, "unreviewed", "Added after the user lock", nil)
	commitAndPushAll(t, lockedWorking, "advance locked layer")

	if output, err := executeCommand("install", "unreviewed"); err == nil || !strings.Contains(err.Error(), "not found in configured registries") {
		t.Fatalf("advanced resource from valid locked layer became installable: err=%v\n%s", err, output)
	}
	if output, err := executeCommand("install", "bootstrap"); err != nil {
		t.Fatalf("missing-lock layer did not retain mutable bootstrap: %v\n%s", err, output)
	}
	assertFactorySkillExists(t, repoRoot, "bootstrap")
}

func TestReadOnlyCommandsPreserveWarmExactCacheBytesAndMetadata(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "xdg-cache"))
	origin, working := createBareRepoWithWorkingClone(t, "warm-check.git")
	writeSkill(t, working, "keeper", "Warm exact cache content", nil)
	commitAndPushAll(t, working, "seed warm check")
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses: [factory]
registries:
  - url: `+origin+`
    ref: main
    skills: [keeper]
`)
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("seed lock: %v\n%s", err, output)
	}
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed materialization: %v\n%s", err, output)
	}
	exactRoot := filepath.Join(cache.CacheDir(), "exact-git")
	before := exactCacheTreeSnapshot(t, exactRoot)
	for _, args := range [][]string{{"sync", "--check"}, {"lock", "--check", "--scope", "repo"}, {"status"}} {
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, output)
		}
		if after := exactCacheTreeSnapshot(t, exactRoot); after != before {
			t.Fatalf("%s changed warm exact cache\nbefore:\n%s\nafter:\n%s", strings.Join(args, " "), before, after)
		}
	}
}

func exactCacheTreeSnapshot(t *testing.T, root string) string {
	t.Helper()
	var entries []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		content := ""
		switch {
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			content = fmt.Sprintf("%x", data)
		case info.Mode()&os.ModeSymlink != 0:
			content, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		entries = append(entries, fmt.Sprintf("%s|%s|%d|%d|%s", rel, info.Mode(), info.Size(), info.ModTime().UnixNano(), content))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n")
}

func TestCheckCommandsDoNotPopulateColdExactCacheOrState(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	stateRoot := filepath.Join(homeDir, "state")
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "xdg-cache"))
	t.Setenv("XDG_STATE_HOME", stateRoot)
	cacheRoot := cache.CacheDir()

	origin, working := createBareRepoWithWorkingClone(t, "cold-check.git")
	writeSkill(t, working, "keeper", "Locked but deliberately absent from cache", nil)
	commitAndPushAll(t, working, "seed cold check")
	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v2
harnesses: [factory]
registries:
  - url: `+origin+`
    ref: main
    skills: [keeper]
`)
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("seed lock: %v\n%s", err, output)
	}
	if err := os.RemoveAll(cacheRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(stateRoot); err != nil {
		t.Fatal(err)
	}

	assertRootsAbsent := func(command string) {
		t.Helper()
		for _, root := range []string{cacheRoot, stateRoot} {
			if _, statErr := os.Lstat(root); !os.IsNotExist(statErr) {
				t.Fatalf("%s created read-only root %s (%v)", command, root, statErr)
			}
		}
	}
	for _, args := range [][]string{{"sync", "--check"}, {"lock", "--check", "--scope", "repo"}} {
		output, err := executeCommand(args...)
		if err == nil || !strings.Contains(err.Error(), "lock.snapshot-unavailable") {
			t.Fatalf("%s on cold exact cache: err=%v\n%s", strings.Join(args, " "), err, output)
		}
		assertRootsAbsent(strings.Join(args, " "))
	}

	// Stale checks must diagnose from config+lock bytes, not resolve the mutable
	// ref (which deliberately does not exist) or create cache/state while trying.
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(strings.Replace(string(raw), "ref: main", "ref: does-not-exist", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		args []string
		code string
	}{{[]string{"sync", "--check"}, "lock.stale"}, {[]string{"lock", "--check", "--scope", "repo"}, "lock.change-required"}} {
		output, err := executeCommand(test.args...)
		if err == nil || !strings.Contains(err.Error(), test.code) {
			t.Fatalf("%s stale read-only check: err=%v\n%s", strings.Join(test.args, " "), err, output)
		}
		assertRootsAbsent(strings.Join(test.args, " "))
	}
}
