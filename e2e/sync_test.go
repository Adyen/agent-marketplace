package e2e_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestSyncCreatesGlobalConfigSkeleton verifies that running sync without an
// existing ~/.agents/config.yaml creates the global config skeleton with the
// required global fields (apiVersion, harnesses) and no logging block, so the
// file stays clean while the logging runtime relies on code defaults.
func TestSyncCreatesGlobalConfigSkeleton(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	regURL := env.CreateBareRepo("skeleton-reg", SkillsOnly(
		Skill{Name: "skeleton-skill", Description: "Skill for skeleton test"},
	))

	// Org config provides a default registry (team-style).
	// No user config exists; sync should create a minimal skeleton without a
	// logging block.
	env.WriteOrgConfig(fmt.Sprintf(`
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources: {team: {git: %q, ref: main}}
resources: {team: {all: true}}
`, regURL))

	r := env.RunAM("sync")
	if r.ExitCode != 0 {
		t.Fatalf("sync should succeed, but exited %d\noutput:\n%s", r.ExitCode, r.Combined)
	}
	t.Logf("first sync output:\n%s", r.Combined)

	// The global config should have been created by EnsureGlobalConfig.
	env.AssertFileExists("/root/.agents/config.yaml")
	env.AssertFileContains("/root/.agents/config.yaml", "apiVersion: agent-marketplace/v4")
	env.AssertFileContains("/root/.agents/config.yaml", "harnesses:")
	// Logging is intentionally omitted from the skeleton; the runtime uses
	// code defaults unless the user opts in with an explicit logging block.
	if skeleton := env.ReadFile("/root/.agents/config.yaml"); strings.Contains(skeleton, "logging:") {
		t.Fatalf("skeleton must not contain a logging block, got:\n%s", skeleton)
	}

	// Verify the skill actually materialized to machine scope.
	env.AssertFileExists("/root/.factory/skills/skeleton-skill/SKILL.md")

	// Re-running sync should leave the existing skeleton untouched.
	before := env.ReadFile("/root/.agents/config.yaml")
	r2 := env.RunAM("sync")
	if r2.ExitCode != 0 {
		t.Fatalf("second sync should succeed, but exited %d\noutput:\n%s", r2.ExitCode, r2.Combined)
	}
	after := env.ReadFile("/root/.agents/config.yaml")
	if strings.TrimSpace(before) != strings.TrimSpace(after) {
		t.Fatalf("second sync should not modify global skeleton\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestSyncPreservesExternalGitignore(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		env := newEnv(t)
		env.InitGitRepo()
		env.WriteGlobalConfig("harnesses: [factory]\n")
		env.WriteLocalConfig("apiVersion: agent-marketplace/v4\n")
		env.WriteRepoConfig("harnesses: [factory]\n")
		env.WriteFile("/root/outside-ignore", "sentinel\n")
		outside := env.mustResolvePath("/root/outside-ignore")
		gitignore := env.mustResolvePath("/workspace/.gitignore")
		if err := os.Symlink(outside, gitignore); err != nil {
			t.Fatal(err)
		}

		env.MustRunAM("sync")

		if got := env.ReadFile("/root/outside-ignore"); got != "sentinel\n" {
			t.Fatalf("external sentinel = %q", got)
		}
		info, err := os.Lstat(gitignore)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("workspace gitignore symlink was replaced")
		}
	})

	t.Run("regular file", func(t *testing.T) {
		env := newEnv(t)
		env.InitGitRepo()
		env.WriteGlobalConfig("harnesses: [factory]\n")
		env.WriteLocalConfig("apiVersion: agent-marketplace/v4\n")
		env.WriteRepoConfig("harnesses: [factory]\n")
		env.WriteFile("/workspace/.gitignore", "user-rule\n")

		env.MustRunAM("sync")

		want := "user-rule\n.agents/config.local.yaml\n.agents/lock.local.yaml\n"
		if got := env.ReadFile("/workspace/.gitignore"); got != want {
			t.Fatalf("gitignore = %q, want %q", got, want)
		}
	})
}

// sync creates a missing user config as v4, the way init does, so a v4
// repository stays a pure v4 setup. An existing v4 user config is left alone
// without the skeleton warning sync used to log on every run.
func TestSyncCreatesAMissingUserConfigAsV4(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	registry := env.CreateBareRepo("sync-v4-user", RegistryFiles([]Skill{{Name: "review", Description: "Review"}}, nil, nil))
	env.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  tools: {git: '%s', ref: main}\nresources:\n  tools: {include: [skill/review]}\n", registry))

	env.AssertExitCode(env.RunAM("sync"), 0)
	user := env.ReadFile("/root/.agents/config.yaml")
	if !strings.Contains(user, "apiVersion: agent-marketplace/v4") || !strings.Contains(user, "factory") || strings.Contains(user, "registries") {
		t.Fatalf("sync did not create a v4 user config with the default harnesses:\n%s", user)
	}
	env.AssertFileExists("/workspace/.factory/skills/review/SKILL.md")

	again := env.RunAM("sync", "--debug")
	if again.ExitCode != 0 || strings.Contains(again.Combined, "failed to create global config skeleton") {
		t.Fatalf("second sync failed or warned about the existing v4 user config (exit %d):\n%s", again.ExitCode, again.Combined)
	}
	if got := env.ReadFile("/root/.agents/config.yaml"); got != user {
		t.Fatalf("second sync rewrote the user config:\n%s", got)
	}
	env.AssertExitCode(env.RunAM("sync", "--check"), 0)
}
