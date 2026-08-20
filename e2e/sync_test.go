package e2e_test

import (
	"fmt"
	"testing"
)

// TestSyncWithOrgFallbackDoesNotCreateUserConfig verifies the fallback
// contract: when the machine-wide target harness set comes solely from the
// organization layer (/etc/agent-marketplace/config.yaml), sync succeeds and
// materializes through the org-declared harnesses WITHOUT creating
// ~/.agents/config.yaml. The user-personal layer is authoritative only once
// the user writes it — a sync-created skeleton carrying default harnesses
// would silently win over the organization fallback on every later run.
// The declarative case config/cli_backend_target_product_contract pins the
// same absence; this standalone test adds the exact sync output and the
// repeat-run idempotence of the no-create behavior.
func TestSyncWithOrgFallbackDoesNotCreateUserConfig(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	regURL := env.CreateBareRepo("skeleton-reg", SkillsOnly(
		Skill{Name: "skeleton-skill", Description: "Skill for skeleton test"},
	))

	// Org config provides the target harness set and a default registry.
	// No user config exists; sync must not create one.
	env.WriteOrgConfig(fmt.Sprintf(`
apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: %s
    ref: main
    default: true
`, regURL))

	r := env.RunAM("sync")
	if r.ExitCode != 0 {
		t.Fatalf("sync should succeed, but exited %d\noutput:\n%s", r.ExitCode, r.Combined)
	}
	t.Logf("first sync output:\n%s", r.Combined)
	env.AssertOutputContains(r, "Installed 1, Removed 0, Malformed 0, Errors 0")

	// The org fallback must not be frozen into a user-personal skeleton:
	// no ~/.agents/config.yaml may appear.
	env.AssertFileAbsent("/root/.agents/config.yaml")

	// The skill materializes to machine scope through the org-declared
	// factory harness.
	env.AssertFileExists("/root/.factory/skills/skeleton-skill/SKILL.md")

	// Re-running sync keeps the no-create behavior (no lazy creation on a
	// warm cache either).
	r2 := env.RunAM("sync")
	if r2.ExitCode != 0 {
		t.Fatalf("second sync should succeed, but exited %d\noutput:\n%s", r2.ExitCode, r2.Combined)
	}
	env.AssertFileAbsent("/root/.agents/config.yaml")
	env.AssertFileExists("/root/.factory/skills/skeleton-skill/SKILL.md")
}
