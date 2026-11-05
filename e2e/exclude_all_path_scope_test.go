package e2e_test

import (
	"strings"
	"testing"
)

// TestExcludeAllScopedByPathNotJustURL is the end-to-end regression for
// the case where a single git registry URL exposes many path-scoped bundles
// (teams/<team>/plugins/...). An exclude_all on ONE path in a
// higher-precedence layer (config.local.yaml) must not suppress resources
// selected from a DIFFERENT path of the same URL.
//
// Before the fix, applyCrossLayerExcludes matched on the registry URL only,
// so the exclude_all on teams/example-team/plugins/oncall-engineering silently
// nuked every repo-scoped resource sharing the awesome-ai URL — including the
// unrelated teams/example-product/plugins/pm-market-research bundle, which
// showed as "available" instead of "enabled" with no error.
func TestExcludeAllScopedByPathNotJustURL(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	pm := Skill{Name: "competitor-analysis", Description: "PM market research skill"}
	oncall := Skill{Name: "oncall-runbook", Description: "Dev oncall skill"}

	// One repo, two path-scoped bundles under distinct team sub-paths.
	regURL := env.CreateBareRepo("awesome-ai", map[string]string{
		"teams/example-product/plugins/pm-market-research/" + pm.SkillPath():  pm.SkillFile(),
		"teams/example-team/plugins/oncall-engineering/" + oncall.SkillPath(): oncall.SkillFile(),
	})

	// Repo config legitimately selects BOTH path-scoped bundles. Selecting the
	// oncall path here is what makes the negative assertion below meaningful:
	// without the local exclude_all, oncall-runbook WOULD materialize, so its
	// absence proves the exclude_all fired rather than being a no-op.
	env.WriteRepoConfig(strings.TrimSpace(`
apiVersion: agent-marketplace/v4
sources:
  pm: {git: ` + regURL + `, ref: main, path: teams/example-product/plugins/pm-market-research}
  oncall: {git: ` + regURL + `, ref: main, path: teams/example-team/plugins/oncall-engineering}
resources:
  pm: {all: true}
  oncall: {all: true}
`))
	// Execute the unexcluded selection before applying the overlay. Both paths
	// must be reachable, and the higher layer must remove an owned oncall copy.
	env.MustRunAM("sync")
	env.AssertFileContains("/workspace/.factory/skills/competitor-analysis/SKILL.md", "PM market research skill")
	env.AssertFileContains("/workspace/.factory/skills/oncall-runbook/SKILL.md", "Dev oncall skill")
	lockBefore := env.ReadFile("/workspace/.agents/lock.yaml")
	for _, path := range []string{"teams/example-product/plugins/pm-market-research", "teams/example-team/plugins/oncall-engineering"} {
		if !strings.Contains(lockBefore, "path: "+path) {
			t.Fatalf("acquisition lock omitted selected path %s:\n%s", path, lockBefore)
		}
	}

	// Local (higher-precedence) config excludes ALL of the oncall path only.
	env.WriteFile("/workspace/.agents/config.local.yaml", strings.TrimSpace(`
apiVersion: agent-marketplace/v4
sources:
  blocked: {git: `+regURL+`, ref: main, path: teams/example-team/plugins/oncall-engineering}
resources:
  blocked: {exclude: ['*']}
`))

	r := env.RunAM("sync")
	if r.ExitCode != 0 {
		t.Fatalf("sync should succeed, exited %d\noutput:\n%s", r.ExitCode, r.Combined)
	}

	// The pm-market-research resource must materialize: the exclude_all on the
	// unrelated oncall-engineering path must not suppress it.
	env.AssertRepoFactorySkillExists("competitor-analysis")

	// The oncall path IS selected at the repo layer, so its absence proves the
	// higher-precedence exclude_all suppressed it — not that it was never picked.
	env.AssertRepoFactorySkillAbsent("oncall-runbook")
	if env.ReadFile("/workspace/.agents/lock.yaml") != lockBefore {
		t.Fatal("path-local exclusion rewrote the committed exact source lock")
	}
}
