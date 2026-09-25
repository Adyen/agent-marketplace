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
