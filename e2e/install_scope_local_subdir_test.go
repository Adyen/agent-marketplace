package e2e_test

import (
	"strings"
	"testing"
)

// TestInstallScopeLocalFromSubdir verifies that install --scope local from a
// monorepo subdirectory:
//   - writes the local config to cwd (not the parent)
//   - does NOT create a skeleton config.yaml
//   - keeps relative --from paths relative in the config
//   - materializes at the subdir (not the parent repo root)
//   - inherits the parent's config.yaml as the repo layer
func TestInstallScopeLocalFromSubdir(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	// Create a subdirectory inside the workspace.
	env.WriteFile("/workspace/subdir/.gitkeep", "")

	// Create a local registry directory with a skill inside the subdirectory.
	env.WriteFile("/workspace/subdir/registry/skills/subdir-skill/SKILL.md",
		Skill{Name: "subdir-skill", Description: "Skill for subdir local test"}.SkillFile())

	// Write a global config so harnesses are configured.
	env.WriteGlobalConfig(``)

	// Run install --from with a relative path from the subdirectory.
	r := env.RunAMFrom("/workspace/subdir", "install", "--from", "./registry", "--scope", "local")
	if r.ExitCode != 0 {
		t.Fatalf("install --scope local from subdir should succeed, but exited %d\noutput:\n%s",
			r.ExitCode, r.Combined)
	}

	// NO skeleton config.yaml should be created for --scope local.
	env.AssertFileAbsent("/workspace/subdir/.agents/config.yaml")

	// Local config should be in the subdir, not the parent.
	env.AssertFileExists("/workspace/subdir/.agents/config.local.yaml")
	// Relative path should stay relative (not resolved to absolute).
	env.AssertFileContains("/workspace/subdir/.agents/config.local.yaml", "./registry")

	// Parent should NOT have a config.local.yaml.
	env.AssertFileAbsent("/workspace/.agents/config.local.yaml")

	// Skill should materialize at the subdir (where the user is working),
	// not the parent repo root.
	env.AssertFileExists("/workspace/subdir/.factory/skills/subdir-skill/SKILL.md")
}

// TestInstallScopeLocalFromSubdirRejectsParentRelativeFrom verifies that local
// registry paths cannot escape the directory containing config.local.yaml.
func TestInstallScopeLocalFromSubdirRejectsParentRelativeFrom(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteFile("/workspace/subdir/.gitkeep", "")
	env.WriteFile("/workspace/registry/skills/parent-skill/SKILL.md",
		Skill{Name: "parent-skill", Description: "Skill outside local config directory"}.SkillFile())
	env.WriteGlobalConfig(``)

	r := env.RunAMFrom("/workspace/subdir", "install", "--from", "../registry", "--scope", "local")
	env.AssertExitCode(r, 1)
	env.AssertOutputContains(r, "must not contain '..' path components")
	env.AssertFileAbsent("/workspace/subdir/.factory/skills/parent-skill")
}

// TestInstallScopeRepoFromSubdir is the repo-scope counterpart, verifying
// the existing fix still works: skeleton config.yaml created in cwd, subdir
// becomes a new project root, materialization at the subdir.
func TestInstallScopeRepoFromSubdir(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	// Create a subdirectory inside the workspace.
	env.WriteFile("/workspace/subdir/.gitkeep", "")

	// Create a local registry directory with a skill inside the subdirectory.
	env.WriteFile("/workspace/subdir/registry/skills/repo-skill/SKILL.md",
		Skill{Name: "repo-skill", Description: "Skill for subdir repo test"}.SkillFile())

	// Write a global config so harnesses are configured.
	env.WriteGlobalConfig(``)

	// Run install --from with a relative path from the subdirectory.
	r := env.RunAMFrom("/workspace/subdir", "install", "--from", "./registry", "--scope", "repo")
	if r.ExitCode != 0 {
		t.Fatalf("install --scope repo from subdir should succeed, but exited %d\noutput:\n%s",
			r.ExitCode, r.Combined)
	}

	// Skeleton .agents/config.yaml should be created in the subdir.
	env.AssertFileExists("/workspace/subdir/.agents/config.yaml")
	// The skeleton is born into the current schema and carries no harnesses
	// list: repo configs are forbidden from declaring one (the target harness
	// set is machine-wide).
	env.AssertFileContains("/workspace/subdir/.agents/config.yaml", "apiVersion: agent-marketplace/v3")
	if skeleton := env.ReadFile("/workspace/subdir/.agents/config.yaml"); strings.Contains(skeleton, "harnesses:") {
		t.Fatalf("subdir skeleton must not declare harnesses (the target harness set is machine-wide), got:\n%s", skeleton)
	}
	// Repo config keeps relative paths (committed, portable).
	env.AssertFileContains("/workspace/subdir/.agents/config.yaml", "./registry")

	// Skill should materialize in the subdir's .factory/skills/ (new project root).
	env.AssertFileExists("/workspace/subdir/.factory/skills/repo-skill/SKILL.md")
}

// TestInstallScopeLocalFromSubdirNoFrom verifies that --scope local from a
// subdirectory also works for non-adhoc installs (using a pre-configured
// registry in the global config), does NOT create a skeleton config.yaml,
// and materializes at the subdir.
func TestInstallScopeLocalFromSubdirNoFrom(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	// Create a bare repo registry with a skill.
	regURL := env.CreateBareRepo("subdir-local-reg", SkillsOnly(
		Skill{Name: "global-reg-skill", Description: "Skill from global registry"},
	))

	// Create a subdirectory inside the workspace.
	env.WriteFile("/workspace/subdir/.gitkeep", "")

	// Write a global config with the registry.
	env.WriteGlobalConfig(strings.TrimSpace(`
registries:
  - url: ` + regURL + `
    ref: main
`))

	// Run install --scope local from the subdirectory (no --from).
	r := env.RunAMFrom("/workspace/subdir", "install", "--scope", "local", "global-reg-skill")
	if r.ExitCode != 0 {
		t.Fatalf("install --scope local from subdir should succeed, but exited %d\noutput:\n%s",
			r.ExitCode, r.Combined)
	}

	// NO skeleton config.yaml should be created for --scope local.
	env.AssertFileAbsent("/workspace/subdir/.agents/config.yaml")

	// Local config should be in the subdir.
	env.AssertFileExists("/workspace/subdir/.agents/config.local.yaml")
	env.AssertFileContains("/workspace/subdir/.agents/config.local.yaml", "global-reg-skill")

	// Skill should materialize at the subdir.
	env.AssertFileExists("/workspace/subdir/.factory/skills/global-reg-skill/SKILL.md")
}

// TestSyncFromSubdirInheritsParentConfig verifies that sync from a
// subdirectory with a config.local.yaml inherits the parent's config.yaml
// registries. The parent's config.yaml is the repo layer (base); the
// subdir's config.local.yaml is the local layer (on top). Both materialize
// at the subdir.
func TestSyncFromSubdirInheritsParentConfig(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	// The machine-wide target harness set comes from the user-personal
	// config; sync never fabricates one.
	env.WriteGlobalConfig(``)

	// Create two registries: one for the parent, one for the subdir.
	parentRegURL := env.CreateBareRepo("parent-reg", SkillsOnly(
		Skill{Name: "parent-skill", Description: "Skill from parent config"},
	))
	subdirRegURL := env.CreateBareRepo("subdir-reg", SkillsOnly(
		Skill{Name: "subdir-skill", Description: "Skill from subdir local config"},
	))

	// Write parent's repo config with a registry and default:true.
	env.WriteRepoConfig(strings.TrimSpace(`
registries:
  - url: ` + parentRegURL + `
    ref: main
    default: true
`))

	// Create a subdirectory with a local config that adds another registry.
	env.WriteFile("/workspace/subdir/.gitkeep", "")
	env.WriteFile("/workspace/subdir/.agents/config.local.yaml", strings.TrimSpace(`
registries:
  - url: `+subdirRegURL+`
    ref: main
    default: true
`))

	// Run sync from the subdirectory.
	r := env.RunAMFrom("/workspace/subdir", "sync")
	if r.ExitCode != 0 {
		t.Fatalf("sync from subdir should succeed, but exited %d\noutput:\n%s",
			r.ExitCode, r.Combined)
	}

	// Both skills should materialize at the subdir (where the user is working).
	env.AssertFileExists("/workspace/subdir/.factory/skills/parent-skill/SKILL.md")
	env.AssertFileExists("/workspace/subdir/.factory/skills/subdir-skill/SKILL.md")

	// Neither should materialize at the parent.
	env.AssertFileAbsent("/workspace/.factory/skills/parent-skill")
	env.AssertFileAbsent("/workspace/.factory/skills/subdir-skill")
}

// TestSyncFromSubdirLocalExcludeParent verifies that a local config in a
// subdirectory can exclude a skill from the parent's config.yaml.
func TestSyncFromSubdirLocalExcludeParent(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	// The machine-wide target harness set comes from the user-personal
	// config; sync never fabricates one.
	env.WriteGlobalConfig(``)

	// Create a registry with two skills.
	regURL := env.CreateBareRepo("exclude-reg", SkillsOnly(
		Skill{Name: "keep-skill", Description: "Skill to keep"},
		Skill{Name: "exclude-skill", Description: "Skill to exclude"},
	))

	// Write parent's repo config with the registry (default:true).
	env.WriteRepoConfig(strings.TrimSpace(`
registries:
  - url: ` + regURL + `
    ref: main
    default: true
`))

	// Create a subdirectory with a local config that excludes one skill.
	env.WriteFile("/workspace/subdir/.gitkeep", "")
	env.WriteFile("/workspace/subdir/.agents/config.local.yaml", strings.TrimSpace(`
registries:
  - url: `+regURL+`
    ref: main
    skills:
      exclude:
        - exclude-skill
`))

	// Run sync from the subdirectory.
	r := env.RunAMFrom("/workspace/subdir", "sync")
	if r.ExitCode != 0 {
		t.Fatalf("sync from subdir should succeed, but exited %d\noutput:\n%s",
			r.ExitCode, r.Combined)
	}

	// keep-skill should materialize at the subdir.
	env.AssertFileExists("/workspace/subdir/.factory/skills/keep-skill/SKILL.md")
	// exclude-skill should NOT materialize.
	env.AssertFileAbsent("/workspace/subdir/.factory/skills/exclude-skill")
}
