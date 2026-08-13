package e2e_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestSyncCreatesGlobalConfigSkeleton verifies that running sync without an
// existing ~/.agents/config.yaml creates the global config skeleton with the
// required global fields (apiVersion, harnesses) and no logging block, so the
// file stays clean while the logging runtime relies on code defaults.
func TestSyncCreatesGlobalConfigSkeleton(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	regURL := env.CreateBareRepo("skeleton-reg", SkillsOnly(
		Skill{Name: "skeleton-skill", Description: "Skill for skeleton test"},
	))

	// Org config provides a default registry (team-style).
	// No user config exists; sync should create a minimal skeleton without a
	// logging block.
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

	// The global config should have been created by EnsureGlobalConfig.
	env.AssertFileExists("/root/.agents/config.yaml")
	env.AssertFileContains("/root/.agents/config.yaml", "apiVersion: agent-marketplace/v2")
	env.AssertFileContains("/root/.agents/config.yaml", "harnesses:")
	// Logging is intentionally omitted from the skeleton; the runtime uses
	// code defaults unless the user opts in with an explicit logging block.
	if skeleton := env.ReadFile("/root/.agents/config.yaml"); strings.Contains(skeleton, "logging:") {
		t.Fatalf("skeleton must not contain a logging block, got:\n%s", skeleton)
	}

	// Verify the skill actually materialized to machine scope.
	env.AssertFileExists("/root/.factory/skills/skeleton-skill/SKILL.md")

	// Re-running sync should leave the existing skeleton untouched.
	before := env.ReadFile("/root/.agents/config.yaml")
	r2 := env.RunAM("sync")
	if r2.ExitCode != 0 {
		t.Fatalf("second sync should succeed, but exited %d\noutput:\n%s", r2.ExitCode, r2.Combined)
	}
	after := env.ReadFile("/root/.agents/config.yaml")
	if strings.TrimSpace(before) != strings.TrimSpace(after) {
		t.Fatalf("second sync should not modify global skeleton\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
