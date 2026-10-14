package e2e_test

import "testing"

// Relative directory sources and personal-local edits remain supported without
// a consumer migrator. Repository and local selections keep their owning roots.
func TestV4RelativeDirectoryLocalAndRepoScopes(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("harnesses: [factory]\n")
	for _, name := range []string{"review", "second"} {
		env.WriteFile("/workspace/vendor/registry/skills/"+name+"/SKILL.md", (Skill{Name: name, Description: name + " body"}).SkillFile())
	}
	env.WriteRepoConfig("sources: {team: {directory: ./vendor/registry}}\nresources: {team: {include: [skill/review]}}\n")
	env.MustRunAM("sync")
	repo := env.ReadFile("/workspace/.agents/config.yaml")
	env.MustRunAM("install", "second", "--scope", "local")
	env.AssertFileContains("/workspace/.factory/skills/second/SKILL.md", "second body")
	if got := env.ReadFile("/workspace/.agents/config.yaml"); got != repo {
		t.Fatal("local install edited repository root")
	}
	env.AssertFileContains("/workspace/.agents/config.local.yaml", "apiVersion: agent-marketplace/v4")
	env.MustRunAM("remove", "second", "--scope", "local")
	env.MustRunAM("sync")
	env.AssertFileAbsent("/workspace/.factory/skills/second")
	env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "review body")
	env.MustRunAM("install", "second", "--scope", "local")
	local := env.ReadFile("/workspace/.agents/config.local.yaml")
	env.MustRunAM("remove", "review", "--scope", "repo")
	env.MustRunAM("sync")
	env.AssertFileAbsent("/workspace/.factory/skills/review")
	env.AssertFileContains("/workspace/.factory/skills/second/SKILL.md", "second body")
	if got := env.ReadFile("/workspace/.agents/config.local.yaml"); got != local {
		t.Fatal("repository removal edited personal-local root")
	}
	env.MustRunAM("install", "review", "--scope", "repo")
	env.MustRunAM("sync", "--check")
	env.AssertFileContains("/workspace/.agents/config.yaml", "directory: ./vendor/registry")
	env.AssertFileAbsent("/workspace/.agents/lock.yaml")
	env.AssertFileAbsent("/workspace/.agents/lock.local.yaml")
	settled := snapshotConsumerEvidence(t, env)
	env.MustRunAM("sync")
	assertConsumerEvidence(t, env, settled)
}
