package e2e_test

import (
	"fmt"
	"strings"
	"testing"
)

// Consumer roots are v4; the acquisition-lock contract still keeps the exact
// revision and byte-identical healthy lock until an explicit update.
func TestV4GitLockRemainsExactUntilUpdate(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("harnesses: [factory]\n")
	url := env.CreateBareRepo("locked-v4", SkillsOnly(Skill{Name: "review", Description: "Original review"}, Skill{Name: "second"}))
	env.WriteRepoConfig(fmt.Sprintf("sources: {team: {git: %q, ref: main}}\nresources: {team: {include: [skill/review]}}\n", url))
	env.MustRunAM("sync")
	before := env.ReadFile("/workspace/.agents/lock.yaml")
	// The acquisition-lock v1 codec is independent of retired consumer
	// schemas. Strict reads keep its bytes; reconciliation changes only its
	// version, not its admitted commit or source-layout hash evidence.
	if strings.Count(before, "apiVersion: agent-marketplace-lock/v2\n") != 1 {
		t.Fatalf("initial lock is not v2:\n%s", before)
	}
	v1 := strings.Replace(before, "apiVersion: agent-marketplace-lock/v2\n", "apiVersion: agent-marketplace-lock/v1\n", 1)
	env.WriteFile("/workspace/.agents/lock.yaml", v1)
	env.MustRunAM("sync", "--locked")
	if got := env.ReadFile("/workspace/.agents/lock.yaml"); got != v1 {
		t.Fatal("strict read changed the v1 acquisition-lock bytes")
	}
	env.MustRunAM("sync")
	if got := env.ReadFile("/workspace/.agents/lock.yaml"); got != before {
		t.Fatalf("v1 reconciliation changed more than the lock version:\nbefore:\n%s\nafter:\n%s", v1, got)
	}
	publishRegistryFiles(t, env, url, map[string]string{"skills/review/SKILL.md": (Skill{Name: "review", Description: "Revised upstream"}).SkillFile()})
	for _, args := range [][]string{{"sync", "--locked"}, {"sync"}, {"sync", "--check"}} {
		env.MustRunAM(args...)
		env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "Original review")
		if got := env.ReadFile("/workspace/.agents/lock.yaml"); got != before {
			t.Fatalf("%v changed healthy lock bytes", args)
		}
	}
	env.MustRunAM("update", "--scope", "repo")
	env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "Revised upstream")
	if got := env.ReadFile("/workspace/.agents/lock.yaml"); got == before {
		t.Fatal("explicit update did not refresh the exact revision")
	}
	env.MustRunAM("install", "second", "--scope", "repo")
	env.AssertFileContains("/workspace/.factory/skills/second/SKILL.md", "name: second")
	env.MustRunAM("remove", "second", "--scope", "repo")
	env.MustRunAM("sync")
	env.AssertFileAbsent("/workspace/.factory/skills/second")
	env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "Revised upstream")
	env.MustRunAM("sync", "--check")
}

func publishRegistryFiles(t *testing.T, env *Env, url string, files map[string]string) {
	t.Helper()
	clone := t.TempDir()
	env.AssertExitCode(env.Exec("git", "clone", url, clone), 0)
	for path, content := range files {
		env.WriteFile(clone+"/"+path, content)
	}
	for _, args := range [][]string{{"add", "."}, {"-c", "user.name=E2E Test", "-c", "user.email=e2e@test.local", "commit", "-m", "publish revised fixture"}, {"push", "origin", "main"}} {
		cmd := append([]string{"git", "-C", clone}, args...)
		env.AssertExitCode(env.Exec(cmd...), 0)
	}
}
