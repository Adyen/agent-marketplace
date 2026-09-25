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

	initialConfig := env.ReadFile("/root/.agents/config.yaml")
	bundle := env.RunAM("install", "--from="+url, "--ref="+branch, "--scope=user")
	if bundle.ExitCode == 0 || !strings.Contains(bundle.Combined, "/.agents/config.yaml") || strings.Contains(bundle.Combined, ".agent-marketplace-config-") {
		t.Fatalf("unsupported v4 bundle install did not refuse against the real config path (exit %d):\n%s", bundle.ExitCode, bundle.Combined)
	}
	if got := env.ReadFile("/root/.agents/config.yaml"); got != initialConfig {
		t.Fatalf("refused v4 bundle install changed user config:\n%s", got)
	}

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

// A relative in-repository directory registry installs and removes through
// `--from` in a legacy repository config; a v4 repository config must behave
// the same and keep the committed source portable.
func TestAdHocRelativeDirectoryParityAcrossSchemas(t *testing.T) {
	for _, apiVersion := range []string{"agent-marketplace/v2", "agent-marketplace/v4"} {
		t.Run(apiVersion, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			env.MustRunAM("init", "--harnesses", "factory")
			env.WriteFile("/workspace/vendor/registry/skills/review/SKILL.md", "---\nname: review\ndescription: Review\n---\n\nReview body\n")
			env.WriteRepoConfig("apiVersion: " + apiVersion + "\n")

			install := env.RunAM("install", "review", "--from", "./vendor/registry", "--scope", "repo")
			if install.ExitCode != 0 {
				t.Fatalf("relative ad-hoc install failed (exit %d):\n%s", install.ExitCode, install.Combined)
			}
			env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "Review body")
			config := env.ReadFile("/workspace/.agents/config.yaml")
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
	for _, tc := range []struct{ scope, skill string }{
		{scope: "user", skill: "/root/.factory/skills/daily-sync/SKILL.md"},
		{scope: "repo", skill: "/workspace/.factory/skills/daily-sync/SKILL.md"},
		{scope: "local", skill: "/workspace/.factory/skills/daily-sync/SKILL.md"},
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

			push("Second revision")
			env.AssertExitCode(env.RunAM("sync"), 0)
			env.AssertFileContains(tc.skill, "First revision")
			env.AssertExitCode(env.RunAM("sync", "--check"), 0)

			env.AssertExitCode(env.RunAM("update"), 0)
			env.AssertFileContains(tc.skill, "Second revision")
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
