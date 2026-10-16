package e2e_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestInstallFromHomeDir verifies that running install --all from the home
// directory does NOT truncate the global config file.
//
// The bug: FindRepoRoot() returns ~ when cwd is the home directory and no
// git repo is found. ProjectConfigPath(~) = ~/.agents/config.yaml which is
// the same path as the global config. The skeleton creation then overwrites
// the global config with just apiVersion, destroying harnesses and registries.
//
// The fix: Install() now checks IsHomeDir(repoRoot) and refuses to proceed
// with repo scope, suggesting --scope user instead.
func TestInstallFromHomeDir(t *testing.T) {
	env := newEnv(t)
	// Do NOT call env.InitGitRepo() — we want no git repo so that
	// FindRepoRoot falls back to cwd = home.

	// Create a registry with one skill.
	regURL := env.CreateBareRepo("home-test-reg", SkillsOnly(
		Skill{Name: "home-skill", Description: "Skill for home-dir test"},
	))

	// Write a valid global config with harnesses and registries.
	env.WriteGlobalConfig(fmt.Sprintf(`
harnesses:
  - factory
sources: {team: {git: %q, ref: main}}
resources: {team: {all: true}}
`, regURL))

	// Read the global config before install — it must contain harnesses.
	env.AssertFileContains("/root/.agents/config.yaml", "harnesses")
	env.AssertFileContains("/root/.agents/config.yaml", "factory")

	// Run install --all from the HOME directory (not workspace).
	// This should fail with the home-directory guard error.
	r := env.RunAMFrom("/root", "install", "--all")
	if r.ExitCode == 0 {
		t.Fatalf("install --all from home dir should have failed, but exited 0\noutput:\n%s", r.Combined)
	}
	if !strings.Contains(r.Combined, "home directory") {
		t.Errorf("error should mention home directory\ngot:\n%s", r.Combined)
	}
	if !strings.Contains(r.Combined, "--scope user") {
		t.Errorf("error should suggest --scope user\ngot:\n%s", r.Combined)
	}

	// Verify the global config was NOT truncated — harnesses must still be present.
	env.AssertFileContains("/root/.agents/config.yaml", "harnesses")
	env.AssertFileContains("/root/.agents/config.yaml", "factory")
	env.AssertFileContains("/root/.agents/config.yaml", "home-test-reg")

	// Verify the suggested workaround: install with --scope user should
	// succeed from home dir and NOT truncate the global config.
	r2 := env.RunAMFrom("/root", "install", "--scope", "user", "home-skill")
	if r2.ExitCode != 0 {
		t.Fatalf("install --scope user from home dir should succeed, but exited %d\noutput:\n%s", r2.ExitCode, r2.Combined)
	}
	env.AssertFileContains("/root/.agents/config.yaml", "harnesses")
	env.AssertFileExists("/root/.factory/skills/home-skill/SKILL.md")
}
