package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A user config created by init is v4. Installing a named resource from an
// ad-hoc Git branch and nested path must still work, and removing it through
// the same ad-hoc coordinates must leave a healthy workspace.
func TestV4UserConfigInstallsAndRemovesAdHocGitResource(t *testing.T) {
	env := newEnv(t)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(env.home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(env.home, "state"))
	env.InitGitRepo()
	env.MustRunAM("init", "--harnesses", "factory")
	env.AssertFileContains("/root/.agents/config.yaml", "apiVersion: agent-marketplace/v4")

	const (
		branch    = "add-skill-daily-sync"
		skillPath = "teams/unified_platform/plugins/identity_platform/skills/daily-sync"
	)
	url := env.CreateBareRepo("awesome-ai", map[string]string{"README.md": "registry"})
	working := filepath.Join(t.TempDir(), "working")
	gitRun(t, "", "clone", strings.TrimPrefix(url, "file://"), working)
	gitRun(t, working, "checkout", "-b", branch)
	skillFile := filepath.Join(working, filepath.FromSlash(skillPath), "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillFile, []byte("---\nname: daily-sync\ndescription: Daily sync\n---\n\nDaily sync body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, working, "add", "-A")
	gitRun(t, working, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-m", "add daily-sync")
	gitRun(t, working, "push", "origin", branch)

	adHoc := []string{"--from=" + url, "--ref=" + branch, "--path=" + skillPath, "--scope=user"}
	install := env.RunAM(append([]string{"install", "daily-sync"}, adHoc...)...)
	if install.ExitCode != 0 {
		t.Fatalf("ad-hoc install into v4 user config failed (exit %d):\n%s", install.ExitCode, install.Combined)
	}
	if strings.Contains(install.Combined, ".agent-marketplace-config-") {
		t.Fatalf("install output leaked a staged config path:\n%s", install.Combined)
	}
	env.AssertFileContains("/root/.factory/skills/daily-sync/SKILL.md", "Daily sync body")
	userConfig := env.ReadFile("/root/.agents/config.yaml")
	for _, want := range []string{"apiVersion: agent-marketplace/v4", "git: " + url, "ref: " + branch, "path: " + skillPath, "skill/daily-sync"} {
		if !strings.Contains(userConfig, want) {
			t.Fatalf("v4 user config missing %q:\n%s", want, userConfig)
		}
	}
	env.AssertExitCode(env.RunAM("sync", "--check"), 0)

	repeat := env.RunAM(append([]string{"install", "daily-sync"}, adHoc...)...)
	if repeat.ExitCode != 0 || env.ReadFile("/root/.agents/config.yaml") != userConfig {
		t.Fatalf("repeat ad-hoc install was not idempotent (exit %d):\n%s", repeat.ExitCode, repeat.Combined)
	}

	remove := env.RunAM(append([]string{"remove", "daily-sync"}, adHoc...)...)
	if remove.ExitCode != 0 {
		t.Fatalf("ad-hoc remove from v4 user config failed (exit %d):\n%s", remove.ExitCode, remove.Combined)
	}
	env.AssertFileAbsent("/root/.factory/skills/daily-sync")
	env.AssertFileContains("/root/.agents/config.yaml", "apiVersion: agent-marketplace/v4")
	env.AssertExitCode(env.RunAM("sync", "--check"), 0)
}

// A relative in-repository directory installs and removes through --from,
// whether the v4 root is initially empty or already authors that source. Both
// workflows must keep the committed source portable and edits idempotent.
func TestV4AdHocRelativeDirectoryWithEmptyAndConfiguredRoots(t *testing.T) {
	for _, fixture := range []struct{ name, root string }{
		{"ad-hoc", "apiVersion: agent-marketplace/v4\n"},
		{"configured", "apiVersion: agent-marketplace/v4\nsources: {local: {directory: ./vendor/registry}}\nresources: {local: {include: [skill/review]}}\n"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			env.MustRunAM("init", "--harnesses", "factory")
			env.WriteFile("/workspace/vendor/registry/skills/review/SKILL.md", "---\nname: review\ndescription: Review\n---\n\nReview body\n")
			env.WriteRepoConfig(fixture.root)

			install := env.RunAM("install", "review", "--from", "./vendor/registry", "--scope", "repo")
			if install.ExitCode != 0 {
				t.Fatalf("relative ad-hoc install failed (exit %d):\n%s", install.ExitCode, install.Combined)
			}
			env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "Review body")
			config := env.ReadFile("/workspace/.agents/config.yaml")
			for _, want := range []string{"apiVersion: agent-marketplace/v4", "sources:", "directory: ./vendor/registry", "skill/review"} {
				if !strings.Contains(config, want) {
					t.Fatalf("relative install lost v4 source/selection %q:\n%s", want, config)
				}
			}
			env.AssertFileAbsent("/workspace/.agents/lock.yaml")
			if strings.Contains(config, env.workspace) || strings.Contains(config, "/private/") {
				t.Fatalf("committed repository config records a machine-specific path:\n%s", config)
			}
			env.AssertExitCode(env.RunAM("sync", "--check"), 0)
			repeat := env.RunAM("install", "review", "--from", "./vendor/registry", "--scope", "repo")
			if repeat.ExitCode != 0 || env.ReadFile("/workspace/.agents/config.yaml") != config {
				t.Fatalf("repeat relative ad-hoc install was not idempotent (exit %d):\n%s", repeat.ExitCode, repeat.Combined)
			}

			remove := env.RunAM("remove", "review", "--from", "./vendor/registry", "--scope", "repo")
			if remove.ExitCode != 0 {
				t.Fatalf("relative ad-hoc remove failed (exit %d):\n%s", remove.ExitCode, remove.Combined)
			}
			env.AssertFileAbsent("/workspace/.factory/skills/review")
			env.AssertExitCode(env.RunAM("sync", "--check"), 0)
		})
	}
}

const gitLabProject = "example-org/ai-projects/awesome-ai"

// gitLabSSH seeds a bare repository at gitLabProject (main plus an optional
// branch) and returns a GIT_SSH_COMMAND environment that serves it to
// git@gitlab.example.com URLs. Users paste SSH remotes and GitLab tree URLs,
// which resolve to SSH remotes, and this runs both without a network.
func gitLabSSH(t *testing.T, env *Env, main map[string]string, branch string, branchFiles map[string]string) map[string]string {
	t.Helper()
	url := env.CreateBareRepo(gitLabProject, main)
	if branch != "" {
		working := filepath.Join(t.TempDir(), "working")
		gitRun(t, "", "clone", strings.TrimPrefix(url, "file://"), working)
		gitRun(t, working, "checkout", "-b", branch)
		for path, content := range branchFiles {
			file := filepath.Join(working, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		gitRun(t, working, "add", "-A")
		gitRun(t, working, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-m", "add "+branch)
		gitRun(t, working, "push", "origin", branch)
	}
	// Named ssh so Git treats it as OpenSSH; the last argument is the remote
	// command, e.g. git-upload-pack 'example-org/ai-projects/awesome-ai.git'.
	ssh := filepath.Join(t.TempDir(), "ssh")
	script := "#!/bin/sh\nfor arg; do command=$arg; done\ncd '" + env.repos + "' && exec sh -c \"git ${command#git-}\"\n"
	if err := os.WriteFile(ssh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return map[string]string{"GIT_SSH_COMMAND": ssh}
}

// A user pastes the GitLab SSH remote with a branch and a nested skill path
// into the v4 user config that init creates (v4.0.8 failed with
// v4.writer-opt-in).
func TestV4UserConfigInstallsFromSSHRemoteBranchAndPath(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.MustRunAM("init", "--harnesses", "factory")
	const (
		branch    = "add-skill-daily-sync"
		skillPath = "teams/unified_platform/plugins/identity_platform/skills/daily-sync"
	)
	ssh := gitLabSSH(t, env, map[string]string{"README.md": "registry"}, branch, map[string]string{
		skillPath + "/SKILL.md": "---\nname: daily-sync\ndescription: Daily sync\n---\n\nDaily sync body\n",
	})
	remote := "git@gitlab.example.com:" + gitLabProject + ".git"
	adHoc := []string{"--from=" + remote, "--ref=" + branch, "--path=" + skillPath, "--scope=user"}

	install := env.RunAMWithEnv(ssh, append([]string{"install", "daily-sync"}, adHoc...)...)
	if install.ExitCode != 0 {
		t.Fatalf("install from SSH remote into v4 user config failed (exit %d):\n%s", install.ExitCode, install.Combined)
	}
	env.AssertFileContains("/root/.factory/skills/daily-sync/SKILL.md", "Daily sync body")
	userConfig := env.ReadFile("/root/.agents/config.yaml")
	for _, want := range []string{"apiVersion: agent-marketplace/v4", "git: " + remote, "ref: " + branch, "path: " + skillPath, "skill/daily-sync"} {
		if !strings.Contains(userConfig, want) {
			t.Fatalf("v4 user config missing %q:\n%s", want, userConfig)
		}
	}
	env.AssertExitCode(env.RunAMWithEnv(ssh, "sync", "--check"), 0)

	remove := env.RunAMWithEnv(ssh, append([]string{"remove", "daily-sync"}, adHoc...)...)
	if remove.ExitCode != 0 {
		t.Fatalf("remove through SSH remote from v4 user config failed (exit %d):\n%s", remove.ExitCode, remove.Combined)
	}
	env.AssertFileAbsent("/root/.factory/skills/daily-sync")
	env.AssertExitCode(env.RunAMWithEnv(ssh, "sync", "--check"), 0)
}

// A user pastes a GitLab tree URL for a skills directory without naming a
// resource; the v4 user config declares the source with an all: true group
// (v4.0.8 failed with v4.writer-opt-in).
func TestV4UserConfigInstallsEveryResourceFromGitLabTreeURL(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.MustRunAM("init", "--harnesses", "factory")
	const skills = "teams/ops/plugins/professional-services/performance-optimization/skills"
	ssh := gitLabSSH(t, env, map[string]string{
		skills + "/perf-profiling/SKILL.md": "---\nname: perf-profiling\ndescription: Profiling\n---\n\nProfiling body\n",
		skills + "/perf-tuning/SKILL.md":    "---\nname: perf-tuning\ndescription: Tuning\n---\n\nTuning body\n",
	}, "", nil)

	install := env.RunAMWithEnv(ssh, "install", "--from", "https://gitlab.example.com/"+gitLabProject+"/-/tree/main/"+skills, "--scope", "user")
	if install.ExitCode != 0 {
		t.Fatalf("install from GitLab tree URL into v4 user config failed (exit %d):\n%s", install.ExitCode, install.Combined)
	}
	env.AssertFileContains("/root/.factory/skills/perf-profiling/SKILL.md", "Profiling body")
	env.AssertFileContains("/root/.factory/skills/perf-tuning/SKILL.md", "Tuning body")
	userConfig := env.ReadFile("/root/.agents/config.yaml")
	for _, want := range []string{"apiVersion: agent-marketplace/v4", "git: git@gitlab.example.com:" + gitLabProject, "ref: main", "path: " + skills, "all: true"} {
		if !strings.Contains(userConfig, want) {
			t.Fatalf("v4 user config missing %q:\n%s", want, userConfig)
		}
	}
	env.AssertExitCode(env.RunAMWithEnv(ssh, "sync", "--check"), 0)
}

// A trailing slash on --path names the same registry subdirectory.
func TestAdHocGitPathTrailingSlashIntoV4UserConfig(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.MustRunAM("init", "--harnesses", "factory")
	url := env.CreateBareRepo("slash", SkillsOnly(Skill{Name: "review", Description: "Review changes"}))
	install := env.RunAM("install", "review", "--from", url, "--ref", "main", "--path", "skills/", "--scope", "user")
	if install.ExitCode != 0 {
		t.Fatalf("install with trailing-slash --path failed (exit %d):\n%s", install.ExitCode, install.Combined)
	}
	env.AssertFileContains("/root/.factory/skills/review/SKILL.md", "Review changes")
	remove := env.RunAM("remove", "review", "--from", url, "--ref", "main", "--path", "skills/", "--scope", "user")
	if remove.ExitCode != 0 {
		t.Fatalf("remove with trailing-slash --path failed (exit %d):\n%s", remove.ExitCode, remove.Combined)
	}
	env.AssertFileAbsent("/root/.factory/skills/review")
}

// A user installs a skill from a feature branch into each scope. When the
// branch advances, plain sync keeps the locked commit, update moves to the new
// commit, and removal by name alone clears it.
func TestV4AdHocBranchSourceKeepsPinUntilUpdate(t *testing.T) {
	configs := map[string]string{
		"user":  "/root/.agents/config.yaml",
		"repo":  "/workspace/.agents/config.yaml",
		"local": "/workspace/.agents/config.local.yaml",
	}
	for _, tc := range []struct{ scope, skill, lock string }{
		{scope: "user", skill: "/root/.factory/skills/daily-sync/SKILL.md", lock: "/root/.agents/lock.yaml"},
		{scope: "repo", skill: "/workspace/.factory/skills/daily-sync/SKILL.md", lock: "/workspace/.agents/lock.yaml"},
		{scope: "local", skill: "/workspace/.factory/skills/daily-sync/SKILL.md", lock: "/workspace/.agents/lock.local.yaml"},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			env.MustRunAM("init", "--harnesses", "factory")
			url := env.CreateBareRepo("branch-source", map[string]string{"README.md": "registry"})
			working := filepath.Join(t.TempDir(), "working")
			gitRun(t, "", "clone", strings.TrimPrefix(url, "file://"), working)
			gitRun(t, working, "checkout", "-b", "feature")
			push := func(body string) {
				t.Helper()
				file := filepath.Join(working, "skills", "daily-sync", "SKILL.md")
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte("---\nname: daily-sync\ndescription: Daily sync\n---\n\n"+body+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				gitRun(t, working, "add", "-A")
				gitRun(t, working, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-m", body)
				gitRun(t, working, "push", "origin", "feature")
			}
			push("First revision")

			install := env.RunAM("install", "daily-sync", "--from", url, "--ref", "feature", "--path", "skills/daily-sync", "--scope", tc.scope)
			if install.ExitCode != 0 {
				t.Fatalf("install from a feature branch into %s scope failed (exit %d):\n%s", tc.scope, install.ExitCode, install.Combined)
			}
			env.AssertFileContains(tc.skill, "First revision")
			for scope, path := range configs {
				data, err := os.ReadFile(env.mustResolvePath(path))
				if err != nil && !os.IsNotExist(err) {
					t.Fatalf("read %s config: %v", scope, err)
				}
				if recorded := strings.Contains(string(data), url) && strings.Contains(string(data), "feature"); recorded != (scope == tc.scope) {
					t.Fatalf("%s config records the feature-branch source = %v after a %s-scope install:\n%s", scope, recorded, tc.scope, data)
				}
			}
			pinned := env.ReadFile(tc.lock)

			push("Second revision")
			env.AssertExitCode(env.RunAM("sync"), 0)
			env.AssertFileContains(tc.skill, "First revision")
			if got := env.ReadFile(tc.lock); got != pinned {
				t.Fatalf("plain sync moved the %s-scope lock:\nbefore:\n%s\nafter:\n%s", tc.scope, pinned, got)
			}
			env.AssertExitCode(env.RunAM("sync", "--check"), 0)

			env.AssertExitCode(env.RunAM("update"), 0)
			env.AssertFileContains(tc.skill, "Second revision")
			if env.ReadFile(tc.lock) == pinned {
				t.Fatalf("update left the %s-scope lock on the first revision:\n%s", tc.scope, pinned)
			}
			env.AssertExitCode(env.RunAM("sync", "--check"), 0)
			if status := env.RunAM("status"); status.ExitCode != 0 || !strings.Contains(status.Combined, "daily-sync") {
				t.Fatalf("status after update did not report daily-sync (exit %d):\n%s", status.ExitCode, status.Combined)
			}

			remove := env.RunAM("remove", "daily-sync")
			if remove.ExitCode != 0 {
				t.Fatalf("remove by name from %s scope failed (exit %d):\n%s", tc.scope, remove.ExitCode, remove.Combined)
			}
			env.AssertFileAbsent(tc.skill)
			env.AssertExitCode(env.RunAM("sync", "--check"), 0)
		})
	}
}
