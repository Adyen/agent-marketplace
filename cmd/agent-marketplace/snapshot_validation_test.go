package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
)

// spec: tui-feedback/sync-recovery-preserves-the-acquisition-lock
func TestUpdatedLockRecoversFromStaleCacheWithoutAdvancing(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, schema := range []string{"v2", "v4"} {
		for _, recovery := range []string{"sync-locked", "sync", "tui-sync"} {
			t.Run(schema+"/"+recovery, func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
				t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
				origin, working := createBareRepoWithWorkingClone(t, "snapshot-validation.git")
				writeSkill(t, working, "pinned", "Revision A", nil)
				commitAndPushAll(t, working, "revision A")
				repo := t.TempDir()
				configPath := filepath.Join(repo, ".agents", "config.yaml")
				contents := "apiVersion: agent-marketplace/v2\nharnesses: [devin]\nregistries:\n  - url: file://" + origin + "\n    ref: main\n    skills: [pinned]\n"
				if schema == "v4" {
					contents = "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  shared:\n    git: file://" + origin + "\n    ref: main\nresources:\n  shared: [skill/pinned]\n"
				}
				writeConfig(t, configPath, contents)
				withWorkingDir(t, repo)
				mustRun := func(args ...string) {
					t.Helper()
					if output, err := executeCommand(args...); err != nil {
						t.Fatalf("%v: %v\n%s", args, err, output)
					}
				}
				mustRun("sync")
				lockPath := filepath.Join(repo, ".agents", "lock.yaml")
				first := readExactManifestLock(t, lockPath).Registries[0]
				stores, err := filepath.Glob(filepath.Join(cache.CacheDir(), "exact-git", "objects", "*.git"))
				if err != nil || len(stores) != 1 {
					t.Fatalf("expected one consumer object store: %v, %v", stores, err)
				}
				writeSkill(t, working, "pinned", "Revision B", nil)
				commitAndPushAll(t, working, "revision B")
				runGit(t, stores[0], "cat-file", "-e", first.Commit.Hex()+"^{commit}")
				t.Setenv("HOME", t.TempDir())
				mustRun("lock", "--scope", "repo", "--upgrade")
				second := readExactManifestLock(t, lockPath).Registries[0]
				lockedBytes, err := os.ReadFile(lockPath)
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv("HOME", home)
				if first.Commit == second.Commit {
					t.Fatal("author did not advance the lock")
				}
				if output, err := exec.Command("git", "-C", stores[0], "cat-file", "-e", second.Commit.Hex()+"^{commit}").CombinedOutput(); err == nil {
					t.Fatalf("consumer already has revision B: %s", output)
				}
				if _, err := cache.OpenExactGit(second.URL, second.Commit); !errors.Is(err, cache.ErrExactGitSnapshotUnavailable) {
					t.Fatalf("new snapshot is not missing: %v", err)
				}
				writeSkill(t, working, "pinned", "Revision C", nil)
				writeSkill(t, working, "unlocked-only", "Only at current main", nil)
				commitAndPushAll(t, working, "revision C")
				assertLockUnchanged := func() {
					t.Helper()
					if after, err := os.ReadFile(lockPath); err != nil || !bytes.Equal(after, lockedBytes) {
						t.Fatalf("lock changed: %v\n%s", err, after)
					}
				}
				for _, args := range [][]string{{"list"}, {"status"}, {"sync", "--check"}, {"lock", "--check"}, {"sync", "--offline"}} {
					code := "lock.snapshot-unavailable"
					if len(args) == 2 && args[1] == "--offline" {
						code = "lock.offline-snapshot-missing"
					}
					output, err := executeCommand(args...)
					if err == nil || !strings.Contains(output+err.Error(), code) {
						t.Fatalf("%v did not retain %s: %v\n%s", args, code, err, output)
					}
					t.Logf("%v retained %s", args, code)
					assertLockUnchanged()
				}
				workspace, err := pipeline.NewWorkspace(repo, "")
				if err != nil {
					t.Fatal(err)
				}
				for _, mode := range []catalog.LoadMode{catalog.LoadModeCached, catalog.LoadModeRefresh} {
					result := pipeline.Resolve(context.Background(), pipeline.ResolveOptions{Workspace: workspace, CatalogMode: mode, ReadOnlyExactLocks: true, AllowMissingReadOnlyLocks: true})
					var lockErr *pipeline.LockViewError
					if !errors.As(errors.Join(result.Errors...), &lockErr) || lockErr.Code != "lock.snapshot-unavailable" || !errors.Is(lockErr, cache.ErrExactGitSnapshotUnavailable) {
						t.Fatalf("TUI read mode %v did not report the missing snapshot: %v", mode, result.Errors)
					}
					if result.CatalogLoad.RefreshAfterLoad {
						t.Fatal("missing exact snapshot unexpectedly schedules automatic refresh")
					}
					assertLockUnchanged()
				}
				switch recovery {
				case "sync-locked":
					mustRun("sync", "--locked")
				case "sync":
					mustRun("sync")
				case "tui-sync":
					if warnings, err := session.RuntimeDependencyLifecycle(context.Background(), workspace, false); err != nil {
						t.Fatalf("TUI sync: %v; warnings: %v", err, warnings)
					}
				}
				assertLockUnchanged()
				installed, err := os.ReadFile(filepath.Join(repo, ".devin", "skills", "pinned", "SKILL.md"))
				if err != nil || !bytes.Contains(installed, []byte("Revision B")) || bytes.Contains(installed, []byte("Revision C")) {
					t.Fatalf("recovery did not install pinned B: %v\n%s", err, installed)
				}
				if _, err := cache.OpenExactGit(second.URL, second.Commit); err != nil {
					t.Fatalf("recovered exact snapshot unavailable: %v", err)
				}
				mustRun("sync", "--check")
				output, err := executeCommand("list")
				if err != nil || !strings.Contains(output, "pinned") || strings.Contains(output, "unlocked-only") {
					t.Fatalf("recovered list escaped the pin: %v\n%s", err, output)
				}
				t.Logf("%s fetched missing B, installed B rather than current main C, preserved lock bytes, and cleared read-only errors", recovery)
			})
		}
	}
}
