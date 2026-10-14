package e2e_test

import (
	"fmt"
	"testing"
)

// Two named v4 sources at one Git origin retain both exact refs and the
// deterministic collision winner. A later upstream commit cannot change it.
func TestV4DuplicateRefCollisionAndLockStability(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("harnesses: [factory]\n")
	url := env.CreateBareRepo("duplicate-ref-v4", SkillsOnly(Skill{Name: "review", Description: "Review from main"}, Skill{Name: "second", Description: "Second from main"}))
	env.MustRun("git clone " + url + " release-fixture")
	env.MustRun("git -C release-fixture checkout -b release")
	env.WriteFile("/workspace/release-fixture/skills/review/SKILL.md", (Skill{Name: "review", Description: "Review from release"}).SkillFile())
	env.MustRun("git -C release-fixture add . && git -C release-fixture -c user.name='E2E Test' -c user.email='e2e@test.local' commit -m release && git -C release-fixture push origin release")
	env.WriteRepoConfig(fmt.Sprintf("sources:\n  first: {git: %q, ref: main}\n  second: {git: %q, ref: release}\nresources:\n  first: {include: [skill/review, skill/second]}\n  second: {include: [skill/review]}\n", url, url))
	env.MustRunAM("sync")
	// The authored first source wins; do not derive the expected winner from
	// whichever output the first sync happened to produce.
	env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "Review from main")
	env.AssertFileContains("/workspace/.factory/skills/second/SKILL.md", "Second from main")
	before := env.ReadFile("/workspace/.agents/lock.yaml")
	env.AssertFileContains("/workspace/.agents/lock.yaml", "ref: main")
	env.AssertFileContains("/workspace/.agents/lock.yaml", "ref: release")
	publishRegistryFiles(t, env, url, map[string]string{"skills/second/SKILL.md": (Skill{Name: "second", Description: "Upstream moved"}).SkillFile()})
	for _, args := range [][]string{{"sync", "--locked"}, {"sync"}, {"sync", "--check"}} {
		env.MustRunAM(args...)
		env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "Review from main")
		env.AssertFileContains("/workspace/.factory/skills/second/SKILL.md", "Second from main")
		if got := env.ReadFile("/workspace/.agents/lock.yaml"); got != before {
			t.Fatalf("%v changed multi-ref lock bytes", args)
		}
	}
}
