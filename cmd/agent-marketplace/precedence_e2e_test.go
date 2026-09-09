package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E007_ConfigMergeUserRepoLayers verifies that user and repo configs
// merge correctly, with repo taking precedence for conflicts. Specifically:
//   - User includes skill-a and skill-b from a registry.
//   - Repo includes skill-b and skill-c from the same registry, but excludes skill-a.
//   - After sync: skill-a should be excluded (repo exclude wins), skill-b and
//     skill-c should be installed.
func scenarioE2E007_ConfigMergeUserRepoLayers(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "merge-layers.git")
	writeSkill(t, working, "skill-a", "Skill A", []string{"test"})
	writeSkill(t, working, "skill-b", "Skill B", []string{"test"})
	writeSkill(t, working, "skill-c", "Skill C", []string{"test"})
	commitAndPushAll(t, working, "Add skills")

	// User config includes skill-a and skill-b.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      - skill-a
      - skill-b
`)

	// Repo config includes skill-b and skill-c, excludes skill-a.
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      include:
        - skill-b
        - skill-c
      exclude:
        - skill-a
`)
	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync failed: %v\n%s", err, output)
	}

	status, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, status)
	}

	// skill-b and skill-c should be installed.
	assertFactorySkillExists(t, repoRoot, "skill-b")
	assertFactorySkillExists(t, repoRoot, "skill-c")

	// skill-a should be excluded by repo's cross-layer exclude.
	assertFactorySkillAbsent(t, repoRoot, "skill-a")

	// Status should show skill-a as excluded.
	if !strings.Contains(status, "skill-a") {
		t.Fatalf("expected status to mention skill-a (excluded), got:\n%s", status)
	}
}
