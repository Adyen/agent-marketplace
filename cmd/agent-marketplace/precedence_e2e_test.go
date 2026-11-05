package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E007_ConfigMergeUserRepoLayers verifies that user and repo selections
// retain scope-local authority even when both bind the same source:
//   - User includes skill-a and skill-b from a registry.
//   - Repo includes skill-b and skill-c from the same registry, but excludes skill-a.
//   - After sync: skill-a remains user-scoped but is absent from repo output;
//     skill-b and skill-c are installed in the repo.
func TestE2E007_ConfigMergeUserRepoLayers(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "merge-layers.git")
	writeSkill(t, working, "skill-a", "Skill A", []string{"test"})
	writeSkill(t, working, "skill-b", "Skill B", []string{"test"})
	writeSkill(t, working, "skill-c", "Skill C", []string{"test"})
	commitAndPushAll(t, working, "Add skills")

	// User config includes skill-a and skill-b.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/skill-a, skill/skill-b]
`)

	// Repo config includes skill-b and skill-c, excludes skill-a.
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted:
    include: [skill/skill-b, skill/skill-c]
    exclude: [skill/skill-a]
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

	// The repo exclusion must not suppress the user's independently bound source.
	assertFactorySkillAbsent(t, repoRoot, "skill-a")
	assertFactorySkillExists(t, homeDir, "skill-a")
	assertFactorySkillExists(t, homeDir, "skill-b")

	// Status must retain the user-scoped resource rather than dropping it.
	if !strings.Contains(status, "skill-a") {
		t.Fatalf("expected status to mention user-scoped skill-a, got:\n%s", status)
	}
}
