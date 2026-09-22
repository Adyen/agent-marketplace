package e2e_test

import (
	"fmt"
	"testing"
)

func TestV4GitAndDirectorySourcesRunThroughNormalCLI(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	gitURL := env.CreateBareRepo("v4-git", RegistryFiles([]Skill{{Name: "git-review"}}, nil, nil))
	// Keep the directory source below the repository root and deliberately give
	// it a .git suffix: v4 source kind, not the path spelling, controls locality.
	env.WriteFile("/workspace/registry.git/skills/local-review/SKILL.md", (Skill{Name: "local-review"}).SkillFile())
	env.WriteRepoConfig(fmt.Sprintf(`apiVersion: agent-marketplace/v4
harnesses: [factory]
materialization:
  mode: symlink
  targets: {factory: file}
sources:
  git-source: {git: %s, ref: main}
  directory-source: {directory: registry.git}
resources:
  git-source: {include: [skill/git-review]}
  directory-source: {include: [skill/local-review]}
`, gitURL))

	env.AssertExitCode(env.RunAM("lock", "--scope", "repo"), 0)

	listed := env.RunAM("list")
	env.AssertExitCode(listed, 0)
	env.AssertOutputContains(listed, "git-review")
	env.AssertOutputContains(listed, "local-review")

	synced := env.RunAM("sync")
	env.AssertExitCode(synced, 0)
	env.AssertFileExists("/workspace/.factory/skills/git-review/SKILL.md")
	env.AssertFileExists("/workspace/.factory/skills/local-review/SKILL.md")
	if !env.CheckRegularFile("/workspace/.factory/skills/git-review/SKILL.md") || !env.CheckRegularFile("/workspace/.factory/skills/local-review/SKILL.md") {
		t.Fatal("v4 file materialization target produced a symlink")
	}

	status := env.RunAM("status")
	env.AssertExitCode(status, 0)
	env.AssertOutputContains(status, "git-review")
	env.AssertOutputContains(status, "local-review")
}

func TestV4MarketplaceMissingLockCreatedByWritingCommands(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("Review carefully.", false))
	for _, command := range []string{"sync", "update", "lock"} {
		t.Run(command, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
			env.WriteRepoConfig(fmt.Sprintf(`apiVersion: agent-marketplace/v4
sources:
  behavior: {marketplace: %s}
resources:
  behavior: {include: [behavior-source/skill/skill-one]}
`, fixture.server.URL))
			before := env.ReadFile("/workspace/.agents/config.yaml")

			result := env.RunAM(command)
			env.AssertExitCode(result, 0)
			if got := env.ReadFile("/workspace/.agents/config.yaml"); got != before {
				t.Fatalf("%s changed authored v4 config: %q", command, got)
			}
			for _, want := range []string{"agent-marketplace-lock/v2", "behavior-marketplace", "behavior-source-id", "skill-one"} {
				if !env.CheckFileContains("/workspace/.agents/lock.yaml", want) {
					t.Fatalf("%s lock missing %q:\n%s", command, want, env.ReadFile("/workspace/.agents/lock.yaml"))
				}
			}
			if command == "sync" {
				env.AssertFileExists("/workspace/.factory/skills/skill-one/SKILL.md")
			}
		})
	}
}

func TestV4MarketplaceRuntimeRejectionIsFailClosed(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteRepoConfig(`apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: https://market.example}
resources:
  hosted: {include: [registry/skill/review]}
`)
	before := env.ReadFile("/workspace/.agents/config.yaml")

	result := env.RunAM("list")
	env.AssertExitCode(result, 1)
	env.AssertOutputContains(result, "v4.marketplace-lock-required")
	if got := env.ReadFile("/workspace/.agents/config.yaml"); got != before {
		t.Fatalf("Marketplace rejection changed authored v4 config: %q", got)
	}
	if env.CheckFileExists("/workspace/.factory/skills/review/SKILL.md") {
		t.Fatal("Marketplace rejection crossed the materialization boundary")
	}
}
