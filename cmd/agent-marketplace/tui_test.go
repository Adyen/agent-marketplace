package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestTUIDependencyLifecycleReconcilesBeforeExactSync(t *testing.T) {
	previousPrepare, previousSync := prepareTUILocks, runTUIExactSync
	t.Cleanup(func() {
		prepareTUILocks, runTUIExactSync = previousPrepare, previousSync
	})

	for _, test := range []struct {
		name    string
		upgrade bool
	}{
		{name: "sync"},
		{name: "update", upgrade: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared := false
			prepareTUILocks = func(_ context.Context, repoRoot, _ string, mode lockMode) error {
				if repoRoot != "/repo" {
					t.Fatalf("prepare repo root = %q, want /repo", repoRoot)
				}
				if mode.upgrade != test.upgrade {
					t.Fatalf("prepare upgrade = %v, want %v", mode.upgrade, test.upgrade)
				}
				prepared = true
				return nil
			}
			runTUIExactSync = func(_ context.Context, options pipeline.ResolveOptions) pipeline.SyncResult {
				if !prepared {
					t.Fatal("exact sync ran before lock preparation")
				}
				if !options.ReadOnlyExactLocks {
					t.Fatal("TUI lifecycle sync was not read-only exact-lock backed")
				}
				return pipeline.SyncResult{}
			}

			if err := runTUIDependencyLifecycle(context.Background(), "/repo", test.upgrade); err != nil {
				t.Fatalf("lifecycle failed: %v", err)
			}
		})
	}
}

func TestTUIConfigMutationCommitsProjectConfigWithExactLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "tui-toggle.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nharnesses: [factory]\nregistries:\n  - url: "+origin+"\n    ref: main\n    path: skills\n")
	projectPath := config.ProjectConfigPath(repo)
	writeConfig(t, projectPath, "\n")
	t.Chdir(repo)
	if _, _, _, _, err := prepareRuntimeLocks(context.Background(), repo, repo, lockMode{}); err != nil {
		t.Fatalf("prepare initial locks: %v", err)
	}
	if err := runTUIConfigMutation(context.Background(), repo, func(mapper pipeline.ConfigPathMapper) error {
		staged, err := mapper(projectPath)
		if err != nil {
			return err
		}
		if err := config.EnsureRegistryWithPathsInConfig(staged, origin, "main", "skills"); err != nil {
			return err
		}
		return config.AddResourceToRegistryInConfig(staged, origin, "skills", ir.KindSkill, "review")
	}); err != nil {
		t.Fatalf("TUI config mutation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); err != nil {
		t.Fatalf("project lock not committed: %v", err)
	}
	resolved := pipeline.Resolve(context.Background(), pipeline.ResolveOptions{RepoRoot: repo, ReadOnlyExactLocks: true})
	if err := errors.Join(resolved.ConfigErrors...); err != nil {
		t.Fatalf("exact config load failed: %v", err)
	}
	if err := errors.Join(resolved.CatalogErrors...); err != nil {
		t.Fatalf("exact catalog load failed: %v", err)
	}
}

func TestTUIDependencyLifecycleAllowsOrganizationWithoutLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "tui-org-unlocked.git")
	writeSkill(t, working, "required-org", "Required org", nil)
	commitAndPushAll(t, working, "required org")

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeConfig(t, orgPath, "registries:\n  - url: "+origin+"\n    ref: main\n    required: true\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "harnesses: [factory]\nregistries: []\n")
	t.Chdir(repo)

	if err := runTUIDependencyLifecycle(context.Background(), repo, false); err != nil {
		t.Fatalf("TUI lifecycle rejected organization config without a lock: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("TUI lifecycle created organization lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "required-org")); err != nil {
		t.Fatalf("TUI lifecycle did not materialize organization requirement: %v", err)
	}
}

func TestTUIDependencyLifecycleLockFailureBlocksSync(t *testing.T) {
	previousPrepare, previousSync := prepareTUILocks, runTUIExactSync
	t.Cleanup(func() {
		prepareTUILocks, runTUIExactSync = previousPrepare, previousSync
	})
	failure := errors.New("lock failed")
	prepareTUILocks = func(context.Context, string, string, lockMode) error { return failure }
	runTUIExactSync = func(context.Context, pipeline.ResolveOptions) pipeline.SyncResult {
		t.Fatal("exact sync ran after lock failure")
		return pipeline.SyncResult{}
	}

	if err := runTUIDependencyLifecycle(context.Background(), "/repo", false); !errors.Is(err, failure) {
		t.Fatalf("lifecycle error = %v, want lock failure", err)
	}
}
