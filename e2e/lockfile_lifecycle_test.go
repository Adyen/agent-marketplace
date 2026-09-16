package e2e_test

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type lockfileGitFixture struct {
	env     *Env
	url     string
	working string
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
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

func TestLockfileFileURLGitLifecycleRetainsExactCommitUntilUpdate(t *testing.T) {
	fixture := newLockfileGitFixture(t, "lock-lifecycle", Skill{Name: "review", Description: "Review version one"})
	// Keep this spelling literal: the regression is specifically the absolute
	// file:// URL whose repository path ends in .git.
	literalURL := "file://" + filepath.ToSlash(filepath.Join(fixture.env.repos, "lock-lifecycle.git"))
	if fixture.url != literalURL {
		t.Fatalf("fixture URL = %q, want literal absolute file URL %q", fixture.url, literalURL)
	}
	fixture.writeRepoConfig(fmt.Sprintf("  - url: %s\n    ref: main\n    default: true", literalURL))

	result := fixture.env.RunAM("sync")
	if result.ExitCode != 0 {
		t.Fatalf("first sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	lockBefore := fixture.env.ReadFile("/workspace/.agents/lock.yaml")
	canonicalURL := strings.TrimSuffix(literalURL, ".git")
	for _, want := range []string{"apiVersion: agent-marketplace-lock/v1", "scope: repo", "url: " + canonicalURL, "commit: sha1:", "revision_schema: v3"} {
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

func TestOrganizationIncludeTrustBlocksSyncWithoutMutation(t *testing.T) {
	for _, unsafe := range []string{"writable", "symlink-dotdot"} {
		t.Run(unsafe, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			env.WriteGlobalConfig("harnesses: [factory]\nregistries: []")
			registry := filepath.Join(env.workspace, "live-registry")
			for path, content := range SkillsOnly(Skill{Name: "review", Description: "Trusted organization include"}) {
				env.WriteFile(filepath.Join(registry, path), content)
			}
			external := filepath.Join(env.workspace, "external")
			fragment := filepath.Join(external, "nested", "fragment.yaml")
			middle := filepath.Join(external, "middle.yaml")
			env.WriteFile(fragment, fmt.Sprintf("registries:\n  - path: %s\n    required: true\n", registry))
			env.WriteFile(middle, "include: nested/fragment.yaml\n")
			env.WriteOrgConfig("include: " + middle)
			if result := env.RunAM("sync"); result.ExitCode != 0 {
				t.Fatalf("trusted external include failed: %s", result.Combined)
			}
			env.AssertFileContains("/root/.factory/skills/review/SKILL.md", "Trusted organization include")
			env.AssertFileAbsent("/etc/agent-marketplace/lock.yaml")
			code := "organization.trust.include.writable"
			if unsafe == "writable" {
				if err := os.Chmod(fragment, 0o666); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink(filepath.Dir(fragment), filepath.Join(external, "alias")); err != nil {
					t.Fatal(err)
				}
				env.WriteFile(middle, "include: alias/../nested/fragment.yaml\n")
				code = "organization.trust.ancestor.symlink"
			}
			before := snapshotWorkspace(t, env.workspace)
			outputBefore := snapshotWorkspace(t, filepath.Join(env.home, ".factory"))
			orgBefore := env.ReadFile("/etc/agent-marketplace/config.yaml")
			result := env.RunAM("sync", "--locked")
			if result.ExitCode == 0 || !strings.Contains(result.Combined, code) {
				t.Fatalf("unsafe include accepted or wrong diagnostic: exit=%d %s", result.ExitCode, result.Combined)
			}
			assertWorkspaceSnapshot(t, env.workspace, before)
			assertWorkspaceSnapshot(t, filepath.Join(env.home, ".factory"), outputBefore)
			if env.ReadFile("/etc/agent-marketplace/config.yaml") != orgBefore {
				t.Fatal("organization config changed")
			}
			env.AssertFileAbsent("/etc/agent-marketplace/lock.yaml")
		})
	}
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

func TestLockfilePendingRecoveryInspectionPreservesSandbox(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("harnesses: [factory]\nregistries: []")
	env.WriteRepoConfig("harnesses: [factory]\nregistries: []")
	state := filepath.Join(env.home, ".local", "state", "agent-marketplace", "dependencies")
	if err := os.MkdirAll(state, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, ".dependencytxn-pending.json"), []byte("{"), 0o640); err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(env.home)
	before := snapshotWorkspace(t, root)
	for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--offline"}, {"sync", "--check"}, {"lock", "--check"}, {"list"}, {"status"}} {
		result := env.RunAM(args...)
		if result.ExitCode == 0 || !strings.Contains(result.Combined, "dependency.recovery-required") {
			t.Fatalf("%v: exit %d, output %s", args, result.ExitCode, result.Combined)
		}
		assertWorkspaceSnapshot(t, root, before)
	}
	if result := env.RunAM("sync"); result.ExitCode == 0 || !strings.Contains(result.Combined, "recovery blocked") {
		t.Fatalf("writing sync did not attempt recovery: %s", result.Combined)
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
