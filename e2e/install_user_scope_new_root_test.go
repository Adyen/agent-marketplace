package e2e_test

import (
	"strings"
	"testing"
)

// An install into user scope creates a missing user config as a v4 root, the
// way repository and personal-local installs create theirs, when ~/.agents
// does not exist yet. On macOS the sandbox home is under /var/folders, a
// system alias into /private, which is where the two spellings of the new
// root used to diverge.
func TestInstallUserScopeCreatesAMissingUserConfig(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured bool
	}{
		{name: "ad hoc registry"},
		{name: "configured registry", configured: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			registry := env.CreateBareRepo("user-scope-root", RegistryFiles([]Skill{{Name: "review", Description: "Review"}}, nil, nil))
			args := []string{"install", "review", "--from", registry, "--ref", "main", "--scope", "user"}
			repo := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
			if tc.configured {
				repo += "sources:\n  tools: {git: '" + registry + "', ref: main}\n"
				args = []string{"install", "review", "--scope", "user"}
			}
			env.WriteRepoConfig(repo)
			result := env.RunAM(args...)
			if result.ExitCode != 0 {
				t.Fatalf("install --scope user without a user config failed (exit %d):\n%s", result.ExitCode, result.Combined)
			}
			user := env.ReadFile("/root/.agents/config.yaml")
			if !strings.Contains(user, "apiVersion: agent-marketplace/v4") || !strings.Contains(user, "skill/review") {
				t.Fatalf("user config is not a v4 root selecting the skill:\n%s", user)
			}
			env.AssertFileExists("/root/.factory/skills/review/SKILL.md")
			if repoAfter := env.ReadFile("/workspace/.agents/config.yaml"); strings.Contains(repoAfter, "include") {
				t.Fatalf("user-scope install edited the repository config:\n%s", repoAfter)
			}
			env.AssertExitCode(env.RunAM("sync", "--check"), 0)
		})
	}
}

// With no harness set in any layer, the new user root gets the default
// harnesses, the same list init and sync write, and the install succeeds.
func TestInstallUserScopeWithoutHarnessesUsesTheDefaultTargets(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	registry := env.CreateBareRepo("user-scope-no-harness", RegistryFiles([]Skill{{Name: "review", Description: "Review"}}, nil, nil))
	result := env.RunAM("install", "review", "--from", registry, "--ref", "main", "--scope", "user")
	if result.ExitCode != 0 {
		t.Fatalf("install --scope user without any harness set failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if user := env.ReadFile("/root/.agents/config.yaml"); !strings.Contains(user, "apiVersion: agent-marketplace/v4") || !strings.Contains(user, "harnesses: [factory, windsurf]") {
		t.Fatalf("user config is not a v4 root with the default harnesses:\n%s", user)
	}
	env.AssertFileExists("/root/.factory/skills/review/SKILL.md")
	env.AssertExitCode(env.RunAM("sync", "--check"), 0)
}

// A Marketplace install into user scope creates the user config and its lock
// under the same conditions.
func TestInstallUserScopeMarketplaceCreatesAMissingUserConfig(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("User scope body", false))
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteRepoConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")

	result := env.RunAM("install", fixture.server.URL+"/r/behavior-source/skill/skill-one", "--scope", "user", "--yes")
	if result.ExitCode != 0 {
		t.Fatalf("Marketplace install --scope user without a user config failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if user := env.ReadFile("/root/.agents/config.yaml"); !strings.Contains(user, "apiVersion: agent-marketplace/v4") || !strings.Contains(user, "marketplace:") {
		t.Fatalf("user config is not a v4 root with the Marketplace source:\n%s", user)
	}
	env.AssertFileContains("/root/.agents/lock.yaml", "skill-one")
	env.AssertFileContains("/root/.factory/skills/skill-one/SKILL.md", "User scope body")
	env.AssertExitCode(env.RunAM("sync", "--check"), 0)
}
