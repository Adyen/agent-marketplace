package e2e_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type lockfileGitFixture struct {
	env     *Env
	url     string
	working string
}

func newLockfileGitFixture(t *testing.T, name string, skills ...Skill) *lockfileGitFixture {
	t.Helper()
	env := newEnv(t)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(env.home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(env.home, "state"))
	env.InitGitRepo()
	env.WriteGlobalConfig("harnesses: [factory]\nregistries: []")
	url := env.CreateBareRepo(name, SkillsOnly(skills...))
	working := filepath.Join(t.TempDir(), "working")
	gitRun(t, "", "clone", "-b", "main", strings.TrimPrefix(url, "file://"), working)
	gitRun(t, working, "config", "user.email", "e2e@example.com")
	gitRun(t, working, "config", "user.name", "E2E")
	return &lockfileGitFixture{env: env, url: url, working: working}
}

func (f *lockfileGitFixture) writeRepoConfig(entries string) {
	f.env.WriteRepoConfig("harnesses: [factory]\nregistries:\n" + entries)
}

func (f *lockfileGitFixture) replaceSkills(t *testing.T, message string, skills ...Skill) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(f.working, "skills")); err != nil {
		t.Fatal(err)
	}
	for path, content := range SkillsOnly(skills...) {
		fullPath := filepath.Join(f.working, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, f.working, "add", "-A")
	gitRun(t, f.working, "commit", "-m", message)
	gitRun(t, f.working, "push", "origin", "main")
}

func TestLockfileSyncRetainsExactCommitUntilUpdate(t *testing.T) {
	fixture := newLockfileGitFixture(t, "lock-lifecycle", Skill{Name: "review", Description: "Review version one"})
	fixture.writeRepoConfig(fmt.Sprintf("  - url: %s\n    ref: main\n    default: true", fixture.url))

	result := fixture.env.RunAM("sync")
	if result.ExitCode != 0 {
		t.Fatalf("first sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	lockBefore := fixture.env.ReadFile("/workspace/.agents/lock.yaml")
	for _, want := range []string{"apiVersion: agent-marketplace-lock/v1", "scope: repo", "commit: sha1:"} {
		if !strings.Contains(lockBefore, want) {
			t.Fatalf("first sync lock missing %q:\n%s", want, lockBefore)
		}
	}
	fixture.env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "Review version one")

	fixture.replaceSkills(t, "move main", Skill{Name: "review", Description: "Review version two"})
	result = fixture.env.RunAM("sync")
	if result.ExitCode != 0 {
		t.Fatalf("retaining sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if lockAfter := fixture.env.ReadFile("/workspace/.agents/lock.yaml"); lockAfter != lockBefore {
		t.Fatalf("ordinary sync advanced or rewrote the lock:\nbefore:\n%s\nafter:\n%s", lockBefore, lockAfter)
	}
	fixture.env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "Review version one")

	result = fixture.env.RunAM("update")
	if result.ExitCode != 0 {
		t.Fatalf("update failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if lockAfter := fixture.env.ReadFile("/workspace/.agents/lock.yaml"); lockAfter == lockBefore {
		t.Fatalf("update retained the old lock:\n%s", lockAfter)
	}
	fixture.env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "Review version two")
}

func TestLockfileLockedFailuresLeaveWorkspaceUnchanged(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		fixture := newLockfileGitFixture(t, "lock-missing", Skill{Name: "review", Description: "Missing lock"})
		fixture.writeRepoConfig(fmt.Sprintf("  - url: %s\n    ref: main\n    default: true", fixture.url))
		before := snapshotWorkspace(t, fixture.env.workspace)

		result := fixture.env.RunAM("sync", "--locked")
		if result.ExitCode == 0 || !strings.Contains(result.Combined, "lock.missing") {
			t.Fatalf("sync --locked with no lock = exit %d:\n%s", result.ExitCode, result.Combined)
		}
		assertWorkspaceSnapshot(t, fixture.env.workspace, before)
		fixture.env.AssertFileAbsent("/workspace/.agents/lock.yaml")
		fixture.env.AssertFileAbsent("/workspace/.factory/skills/review")
	})

	t.Run("stale", func(t *testing.T) {
		fixture := newLockfileGitFixture(t, "lock-stale-one", Skill{Name: "one", Description: "First registry"})
		fixture.writeRepoConfig(fmt.Sprintf("  - url: %s\n    ref: main\n    default: true", fixture.url))
		if result := fixture.env.RunAM("sync"); result.ExitCode != 0 {
			t.Fatalf("initial sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
		}
		secondURL := fixture.env.CreateBareRepo("lock-stale-two", SkillsOnly(Skill{Name: "two", Description: "Second registry"}))
		fixture.writeRepoConfig(fmt.Sprintf("  - url: %s\n    ref: main\n    default: true\n  - url: %s\n    ref: main\n    default: true", fixture.url, secondURL))
		before := snapshotWorkspace(t, fixture.env.workspace)

		result := fixture.env.RunAM("sync", "--locked")
		if result.ExitCode == 0 || !strings.Contains(result.Combined, "lock.stale") {
			t.Fatalf("sync --locked with stale lock = exit %d:\n%s", result.ExitCode, result.Combined)
		}
		assertWorkspaceSnapshot(t, fixture.env.workspace, before)
		fixture.env.AssertFileContains("/workspace/.factory/skills/one/SKILL.md", "First registry")
		fixture.env.AssertFileAbsent("/workspace/.factory/skills/two")
	})
}

func TestLockfileOfflineUsesOnlyExactSnapshot(t *testing.T) {
	fixture := newLockfileGitFixture(t, "lock-offline", Skill{Name: "offline", Description: "Exact offline snapshot"})
	fixture.writeRepoConfig(fmt.Sprintf("  - url: %s\n    ref: main\n    default: true", fixture.url))
	if result := fixture.env.RunAM("sync"); result.ExitCode != 0 {
		t.Fatalf("initial sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	fixture.replaceSkills(t, "move remote beyond lock", Skill{Name: "offline", Description: "Unpinned remote content"})
	if err := os.RemoveAll(filepath.Join(fixture.env.workspace, ".factory")); err != nil {
		t.Fatal(err)
	}
	if result := fixture.env.RunAM("sync", "--offline"); result.ExitCode != 0 {
		t.Fatalf("offline exact sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	fixture.env.AssertFileContains("/workspace/.factory/skills/offline/SKILL.md", "Exact offline snapshot")

	if err := os.RemoveAll(filepath.Join(fixture.env.home, ".cache", "agent-marketplace", "exact-git", "snapshots")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(fixture.env.workspace, ".factory")); err != nil {
		t.Fatal(err)
	}
	result := fixture.env.RunAM("sync", "--offline")
	if result.ExitCode == 0 || !strings.Contains(result.Combined, "lock.offline-snapshot-missing") {
		t.Fatalf("offline sync without exact snapshot = exit %d:\n%s", result.ExitCode, result.Combined)
	}
	fixture.env.AssertFileAbsent("/workspace/.factory/skills/offline")
}

func TestLockfileLocalRegistryRemainsUnlocked(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("harnesses: [factory]\nregistries: []")
	for path, content := range SkillsOnly(Skill{Name: "local-only", Description: "Live local registry"}) {
		env.WriteFile("/workspace/registry/"+path, content)
	}
	env.WriteRepoConfig("harnesses: [factory]\nregistries:\n  - url: ./registry\n    default: true")

	if result := env.RunAM("sync"); result.ExitCode != 0 {
		t.Fatalf("local registry sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	env.AssertFileContains("/workspace/.factory/skills/local-only/SKILL.md", "Live local registry")
	env.AssertFileAbsent("/workspace/.agents/lock.yaml")
}

func TestLockfileMonorepoLocalLayerOwnsAdjacentLock(t *testing.T) {
	fixture := newLockfileGitFixture(t, "monorepo-local-lock", Skill{Name: "package-skill", Description: "Package local lock"})
	fixture.env.WriteFile("/workspace/packages/app/.gitkeep", "")
	fixture.env.WriteFile("/workspace/packages/app/.agents/config.local.yaml", fmt.Sprintf("registries:\n  - url: %s\n    ref: main\n    default: true", fixture.url))

	result := fixture.env.RunAMFrom("/workspace/packages/app", "sync")
	if result.ExitCode != 0 {
		t.Fatalf("subdirectory sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	fixture.env.AssertFileContains("/workspace/packages/app/.agents/lock.local.yaml", "scope: local")
	fixture.env.AssertFileAbsent("/workspace/.agents/lock.local.yaml")
	fixture.env.AssertFileContains("/workspace/packages/app/.factory/skills/package-skill/SKILL.md", "Package local lock")
}

func TestLockfileConfiguredListIsExactButListFromIsLive(t *testing.T) {
	fixture := newLockfileGitFixture(t, "lock-list", Skill{Name: "locked-only", Description: "Locked catalog"})
	fixture.writeRepoConfig(fmt.Sprintf("  - url: %s\n    ref: main\n    default: true", fixture.url))
	if result := fixture.env.RunAM("sync"); result.ExitCode != 0 {
		t.Fatalf("initial sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	lockBefore := fixture.env.ReadFile("/workspace/.agents/lock.yaml")
	fixture.replaceSkills(t, "publish live catalog", Skill{Name: "locked-only", Description: "Locked catalog"}, Skill{Name: "live-only", Description: "Live catalog"})

	configured := fixture.env.RunAM("list")
	if configured.ExitCode != 0 || !strings.Contains(configured.Combined, "locked-only") || strings.Contains(configured.Combined, "live-only") {
		t.Fatalf("configured list did not use exact lock (exit %d):\n%s", configured.ExitCode, configured.Combined)
	}
	if lockAfter := fixture.env.ReadFile("/workspace/.agents/lock.yaml"); lockAfter != lockBefore {
		t.Fatalf("configured list rewrote lock:\nbefore:\n%s\nafter:\n%s", lockBefore, lockAfter)
	}

	live := fixture.env.RunAM("list", "--from", fixture.url, "--ref", "main")
	if live.ExitCode != 0 || !strings.Contains(live.Combined, "live-only") {
		t.Fatalf("list --from did not browse live ref (exit %d):\n%s", live.ExitCode, live.Combined)
	}
}

func snapshotWorkspace(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := info.Mode().String()
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			value += " -> " + target
		} else if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += "\n" + string(data)
		}
		snapshot[rel] = value
		return nil
	}); err != nil {
		t.Fatalf("snapshot workspace: %v", err)
	}
	return snapshot
}

func assertWorkspaceSnapshot(t *testing.T, root string, want map[string]string) {
	t.Helper()
	got := snapshotWorkspace(t, root)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("failed command mutated workspace\nbefore: %#v\nafter:  %#v", want, got)
	}
}
