package e2e_test

import (
	"strings"
	"testing"
)

// TestSyncFromSubdirExcludeShadowedByAncestor verifies that when the repo root
// has already materialized a skill and a subdir local config tries to exclude
// it, sync from the subdir:
//   - does NOT materialize the excluded skill in the subdir
//   - leaves the ancestor (repo root) materialization intact
//   - emits a loud, remediable warning explaining the exclude is ineffective
//     because agent harnesses discover the ancestor copy via upward walk
func TestSyncFromSubdirExcludeShadowedByAncestor(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	regURL := env.CreateBareRepo("ancestor-shadow-reg", SkillsOnly(
		Skill{Name: "keep-skill", Description: "Skill to keep"},
		Skill{Name: "target-skill", Description: "Skill the subdir tries to exclude"},
	))

	// Repo root selects both skills and syncs, materializing them at the root.
	env.WriteRepoConfig(strings.TrimSpace(`
apiVersion: agent-marketplace/v4
sources:
  tools: {git: ` + regURL + `, ref: main}
resources:
  tools: {all: true}
`))
	if r := env.RunAM("sync"); r.ExitCode != 0 {
		t.Fatalf("root sync should succeed, exited %d\noutput:\n%s", r.ExitCode, r.Combined)
	}
	env.AssertFileExists("/workspace/.factory/skills/keep-skill/SKILL.md")
	env.AssertFileExists("/workspace/.factory/skills/target-skill/SKILL.md")

	// Subdir local config excludes target-skill.
	env.WriteFile("/workspace/subdir/.gitkeep", "")
	env.WriteFile("/workspace/subdir/.agents/config.local.yaml", strings.TrimSpace(`
apiVersion: agent-marketplace/v4
sources:
  overlay: {git: `+regURL+`, ref: main}
resources:
  overlay: {exclude: [skill/target-skill]}
`))

	r := env.RunAMFrom("/workspace/subdir", "sync")
	if r.ExitCode != 0 {
		t.Fatalf("subdir sync should succeed, exited %d\noutput:\n%s", r.ExitCode, r.Combined)
	}

	// The subdir does not materialize the excluded skill.
	env.AssertFileAbsent("/workspace/subdir/.factory/skills/target-skill")

	// The ancestor copy is untouched: the exclude is genuinely ineffective.
	env.AssertFileExists("/workspace/.factory/skills/target-skill/SKILL.md")

	// A remediable warning must be printed naming the skill, the ancestor
	// config to edit, and the whole-checkout caveat.
	if !strings.Contains(r.Combined, "has no effect") || !strings.Contains(r.Combined, "target-skill") {
		t.Fatalf("expected ineffective-exclude warning for target-skill, got:\n%s", r.Combined)
	}
	if !strings.Contains(r.Combined, "config.local.yaml") || !strings.Contains(r.Combined, "entire") {
		t.Fatalf("expected remediation pointing at ancestor config.local.yaml with whole-checkout note, got:\n%s", r.Combined)
	}
}
