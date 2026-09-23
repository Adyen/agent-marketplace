package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestV4GitAndDirectorySourcesRunThroughNormalCLI(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	gitURL := env.CreateBareRepo("v4-git", RegistryFiles([]Skill{{Name: "git-review"}, {Name: "git-followup"}}, nil, nil))
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

	env.AssertExitCode(env.RunAM("install", "git-followup", "--scope", "repo"), 0)
	env.AssertFileExists("/workspace/.factory/skills/git-followup/SKILL.md")
	env.AssertExitCode(env.RunAM("remove", "git-followup", "--scope", "repo"), 0)
	env.AssertFileAbsent("/workspace/.factory/skills/git-followup")
	env.AssertExitCode(env.RunAM("sync", "--check"), 0)
}

func TestV4UserDirectoryNamedInstallRemovePreservesSelectedResource(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	registry := env.mustResolvePath("/root/resources")
	for _, name := range []string{"original", "followup"} {
		env.WriteFile("/root/resources/skills/"+name+"/SKILL.md", (Skill{Name: name}).SkillFile())
	}
	env.WriteGlobalConfig(fmt.Sprintf(`apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  personal: {directory: %s}
resources:
  personal: {include: [skill/original]}
`, registry))
	env.AssertExitCode(env.RunAM("sync"), 0)
	env.AssertExitCode(env.RunAM("install", "followup", "--scope", "user"), 0)
	env.AssertFileExists("/root/.factory/skills/followup/SKILL.md")
	env.AssertExitCode(env.RunAM("remove", "followup", "--scope", "user"), 0)
	env.AssertFileAbsent("/root/.factory/skills/followup")
	env.AssertFileExists("/root/.factory/skills/original/SKILL.md")
	env.AssertExitCode(env.RunAM("sync", "--check"), 0)

	invalid := env.ReadFile("/root/.agents/config.yaml") + "unknown_field: true\n"
	env.WriteFile("/root/.agents/config.yaml", invalid)
	rejected := env.RunAM("install", "followup", "--scope", "user")
	env.AssertExitCode(rejected, 1)
	if got := env.ReadFile("/root/.agents/config.yaml"); got != invalid {
		t.Fatalf("failed install changed invalid v4 user config: got %q, want %q; output: %s", got, invalid, rejected.Combined)
	}
	env.AssertFileAbsent("/root/.factory/skills/followup")
	env.AssertFileExists("/root/.factory/skills/original/SKILL.md")
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

func TestV4OrganizationDoctorWithoutLockAfterColdSync(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("Organization collection", true))
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	orgConfig := fmt.Sprintf(`apiVersion: agent-marketplace/v4
allowed_marketplaces:
  - '^%s$'
sources:
  hosted: {marketplace: %s}
resources:
  hosted: {include: [behavior-source/collection/behavior-bundle]}
`, regexp.QuoteMeta(fixture.server.URL), fixture.server.URL)
	env.WriteOrgConfig(orgConfig)
	before := env.ReadFile("/etc/agent-marketplace/config.yaml")

	env.AssertExitCode(env.RunAM("sync"), 0)
	env.AssertFileExists("/root/.factory/skills/skill-one/SKILL.md")
	lockPath := env.mustResolvePath("/etc/agent-marketplace/lock.yaml")
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("sync wrote organization lock: %v", err)
	}

	freshCache := filepath.Join(env.home, "empty-cache")
	result := env.RunAMWithEnv(map[string]string{"XDG_CACHE_HOME": freshCache}, "doctor")
	env.AssertExitCode(result, 0)
	env.AssertOutputNotContains(result, "v4.marketplace-lock-required")
	env.AssertOutputNotContains(result, "CONFIGURATION ERRORS")
	repair := env.RunAMWithEnv(map[string]string{"XDG_CACHE_HOME": freshCache + "-repair"}, "doctor", "--repair")
	env.AssertExitCode(repair, 0)
	env.AssertOutputContains(repair, "nothing to repair")
	env.AssertOutputNotContains(repair, "v4.marketplace-lock-required")
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("doctor wrote organization lock: %v", err)
	}
	if got := env.ReadFile("/etc/agent-marketplace/config.yaml"); got != before {
		t.Fatalf("doctor changed organization config:\n%s", got)
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
