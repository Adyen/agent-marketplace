package e2e_test

import "testing"

// The former conversion test retains the selection/preservation boundary as a
// real v4 workflow. Read-only inspection leaves root and included bytes alone.
func TestV4IncludedSelectionCheckAndInstallPreservesAuthoredFiles(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("harnesses: [factory]\n")
	for _, name := range []string{"review", "second"} {
		env.WriteFile("/workspace/.agents/vendor/registry/skills/"+name+"/SKILL.md", (Skill{Name: name}).SkillFile())
	}
	env.WriteRepoConfig("include: [fragment.yaml]\n")
	const fragment = "sources: {team: {directory: ./vendor/registry}}\nresources: {team: {include: [skill/review]}}\n"
	env.WriteFile("/workspace/.agents/fragment.yaml", fragment)
	env.MustRunAM("sync")
	before := snapshotConsumerEvidence(t, env)
	env.MustRunAM("sync", "--check")
	assertConsumerEvidence(t, env, before)
	env.MustRunAM("install", "second", "--scope", "repo")
	env.AssertFileContains("/workspace/.factory/skills/second/SKILL.md", "name: second")
	env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "name: review")
	if got := env.ReadFile("/workspace/.agents/fragment.yaml"); got != fragment {
		t.Fatal("supported edit changed included selection authority")
	}
	env.MustRunAM("sync", "--check")
}
