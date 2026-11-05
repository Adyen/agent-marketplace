package e2e_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestSyncPrunesOrgOwnedWhenOrgConfigDisappears verifies that previously
// organization-owned managed artifacts are removed once the external v4
// organization configuration is gone.
func TestSyncPrunesOrgOwnedWhenOrgConfigDisappears(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	regURL := env.CreateBareRepo("org-disappear-reg", SkillsOnly(
		Skill{Name: "user-skill", Description: "User-owned skill"},
		Skill{Name: "org-skill", Description: "Organization-owned skill"},
	))

	env.WriteGlobalConfig(fmt.Sprintf(`sources: {team: {git: %q, ref: main}}
resources: {team: {include: [skill/user-skill]}}
`, regURL))

	env.WriteOrgConfig(fmt.Sprintf(`sources: {team: {git: %q, ref: main}}
resources: {team: {include: [skill/org-skill]}}
`, regURL))

	r := env.RunAM("sync")
	if r.ExitCode != 0 {
		t.Fatalf("expected first sync to succeed, got exit %d:\n%s", r.ExitCode, r.Combined)
	}
	if !strings.Contains(r.Combined, "Installed 2") {
		t.Fatalf("expected first sync to install 2 skills, got:\n%s", r.Combined)
	}
	env.AssertFileExists("/root/.factory/skills/user-skill/SKILL.md")
	env.AssertFileExists("/root/.factory/skills/org-skill/SKILL.md")

	// Simulate the managed organization configuration disappearing.
	orgPath := env.mustResolvePath("/etc/agent-marketplace/config.yaml")
	if err := os.Remove(orgPath); err != nil {
		t.Fatalf("failed to remove org config: %v", err)
	}

	r = env.RunAM("sync")
	if r.ExitCode != 0 {
		t.Fatalf("expected second sync to succeed after org config disappearance, got exit %d:\n%s", r.ExitCode, r.Combined)
	}
	if !strings.Contains(r.Combined, "Removed 1") {
		t.Fatalf("expected second sync to remove the org-owned skill, got:\n%s", r.Combined)
	}

	env.AssertFileExists("/root/.factory/skills/user-skill/SKILL.md")
	env.AssertFileAbsent("/root/.factory/skills/org-skill/SKILL.md")
}

// TestSyncPrunesOrgOwnedWhenOrgPolicyChanges verifies that reducing the
// organization config's skill selection causes sync to prune the previously
// selected managed artifacts.
func TestSyncPrunesOrgOwnedWhenOrgPolicyChanges(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	regURL := env.CreateBareRepo("org-policy-reg", SkillsOnly(
		Skill{Name: "org-keep", Description: "Organization skill to keep"},
		Skill{Name: "org-drop", Description: "Organization skill to drop"},
	))

	// Target harness only; selection comes from the org config.
	env.WriteGlobalConfig("sources: {}")

	env.WriteOrgConfig(fmt.Sprintf(`sources: {team: {git: %q, ref: main}}
resources: {team: {include: [skill/org-keep, skill/org-drop]}}
`, regURL))

	r := env.RunAM("sync")
	if r.ExitCode != 0 {
		t.Fatalf("expected first sync to succeed, got exit %d:\n%s", r.ExitCode, r.Combined)
	}
	env.AssertFileExists("/root/.factory/skills/org-keep/SKILL.md")
	env.AssertFileExists("/root/.factory/skills/org-drop/SKILL.md")

	// Change organization policy to stop selecting org-drop.
	env.WriteOrgConfig(fmt.Sprintf(`sources: {team: {git: %q, ref: main}}
resources: {team: {include: [skill/org-keep]}}
`, regURL))

	r = env.RunAM("sync")
	if r.ExitCode != 0 {
		t.Fatalf("expected second sync to succeed after policy change, got exit %d:\n%s", r.ExitCode, r.Combined)
	}
	if !strings.Contains(r.Combined, "Removed 1") {
		t.Fatalf("expected second sync to remove one managed artifact, got:\n%s", r.Combined)
	}
	env.AssertFileExists("/root/.factory/skills/org-keep/SKILL.md")
	env.AssertFileAbsent("/root/.factory/skills/org-drop/SKILL.md")
}

// TestDeinitUserPreservesOrgOwnedOutput verifies that a user-scoped deinit
// removes user-owned machine-scope artifacts while leaving organization-owned
// artifacts in place. The external organization config is never modified.
func TestDeinitUserPreservesOrgOwnedOutput(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	regURL := env.CreateBareRepo("deinit-org-reg", SkillsOnly(
		Skill{Name: "user-skill", Description: "User-owned skill"},
		Skill{Name: "org-skill", Description: "Organization-owned skill"},
	))

	env.WriteGlobalConfig(fmt.Sprintf(`sources: {team: {git: %q, ref: main}}
resources: {team: {include: [skill/user-skill]}}
`, regURL))

	orgPath := env.mustResolvePath("/etc/agent-marketplace/config.yaml")
	env.WriteOrgConfig(fmt.Sprintf(`sources: {team: {git: %q, ref: main}}
resources: {team: {include: [skill/org-skill]}}
`, regURL))

	r := env.RunAM("sync")
	if r.ExitCode != 0 {
		t.Fatalf("expected sync to succeed, got exit %d:\n%s", r.ExitCode, r.Combined)
	}
	env.AssertFileExists("/root/.factory/skills/user-skill/SKILL.md")
	env.AssertFileExists("/root/.factory/skills/org-skill/SKILL.md")

	r = env.RunAM("deinit")
	if r.ExitCode != 0 {
		t.Fatalf("expected deinit to succeed, got exit %d:\n%s", r.ExitCode, r.Combined)
	}

	env.AssertFileAbsent("/root/.factory/skills/user-skill/SKILL.md")
	env.AssertFileExists("/root/.factory/skills/org-skill/SKILL.md")

	// The external org config must remain untouched.
	if _, err := os.Stat(orgPath); err != nil {
		t.Fatalf("organization config should not be removed by user deinit: %v", err)
	}
}

// TestSyncFailsWithInvalidManagedOrgConfig verifies that a present but invalid
// organization configuration blocks synchronization before any catalog fetch.
func TestSyncFailsWithInvalidManagedOrgConfig(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	regURL := env.CreateBareRepo("invalid-org-reg", SkillsOnly(
		Skill{Name: "user-skill", Description: "User-owned skill"},
	))

	env.WriteGlobalConfig(fmt.Sprintf(`sources: {team: {git: %q, ref: main}}
resources: {team: {include: [skill/user-skill]}}
`, regURL))

	// Write an invalid present organization config directly; WriteOrgConfig
	// would auto-inject a valid v4 header.
	env.WriteFile("/etc/agent-marketplace/config.yaml", `apiVersion: agent-marketplace/v1
harnesses:
  - factory
registries: []
`)

	r := env.RunAM("sync")
	if r.ExitCode == 0 {
		t.Fatalf("expected sync to fail with invalid org config, got exit 0:\n%s", r.Combined)
	}
	if !strings.Contains(r.Combined, "v4.api-version") {
		t.Fatalf("expected sync error to mention unsupported apiVersion, got:\n%s", r.Combined)
	}
}
