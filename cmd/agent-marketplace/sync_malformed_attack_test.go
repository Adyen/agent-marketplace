package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyncPruneStaleKeepsMalformedSelectionWhenAnotherRegistryLacksIt is the
// highest-severity prune attack: the same name is selected from two registries,
// malformed in one and genuinely absent from the other. The absent one is
// stale and prunable; the malformed one must survive, and pruning the first
// must not take the second with it.
func TestSyncPruneStaleKeepsMalformedSelectionWhenAnotherRegistryLacksIt(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	originA, workingA := createBareRepoWithWorkingClone(t, "prune-two-a.git")
	writeSkill(t, workingA, "valid-a", "Valid A", nil)
	writeFile(t, filepath.Join(workingA, "skills", "shared-skill", "SKILL.md"), brokenSkillFrontmatter("shared-skill"))
	commitAndPushAll(t, workingA, "A: valid + malformed shared-skill")

	originB, workingB := createBareRepoWithWorkingClone(t, "prune-two-b.git")
	writeSkill(t, workingB, "valid-b", "Valid B", nil)
	commitAndPushAll(t, workingB, "B: no shared-skill at all")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+originA+`
    ref: main
    skills:
      - valid-a
      - shared-skill
  - url: `+originB+`
    ref: main
    skills:
      - valid-b
      - shared-skill
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync", "--prune-stale")
	if err != nil {
		t.Fatalf("expected sync --prune-stale to succeed: %v\n%s", err, output)
	}

	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	cfg := string(data)
	sectionA, sectionB := splitConfigAtRegistry(cfg, originB)
	if sectionB == "" {
		t.Fatalf("splitConfigAtRegistry did not find registry URL %q in config:\n%s", originB, cfg)
	}
	// The registry whose copy is merely unparseable keeps its selection.
	if !strings.Contains(sectionA, "shared-skill") {
		t.Fatalf("the malformed selection was pruned from the registry that has it:\n%s", cfg)
	}
	// The registry that genuinely lacks it is still pruned: correlation must
	// not make a stale selection unprunable everywhere.
	if strings.Contains(sectionB, "shared-skill") {
		t.Fatalf("the genuinely absent selection was not pruned from the registry that lacks it:\n%s", cfg)
	}
	if !strings.Contains(output, `pruned stale skill "shared-skill"`) {
		t.Fatalf("expected the absent copy to be pruned, got:\n%s", output)
	}
}

// splitConfigAtRegistry splits a rendered config into the text before and after
// the given registry URL so per-entry selections can be asserted separately.
func splitConfigAtRegistry(cfg, url string) (before, after string) {
	idx := strings.Index(cfg, url)
	if idx < 0 {
		return cfg, ""
	}
	return cfg[:idx], cfg[idx:]
}

// TestSyncStrictPruneStaleDoesNotDeleteMalformedSelection combines the two
// flags: strict promotes the malformed warning to an error, which must not open
// a path to deleting the selection.
func TestSyncStrictPruneStaleDoesNotDeleteMalformedSelection(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "strict-prune-malformed.git")
	writeSkill(t, working, "valid-skill", "Valid skill", nil)
	writeFile(t, filepath.Join(working, "skills", "broken-skill", "SKILL.md"), brokenSkillFrontmatter("broken-skill"))
	commitAndPushAll(t, working, "Add valid and malformed skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - valid-skill
      - broken-skill
`)

	withWorkingDir(t, repoRoot)

	output, _ := executeCommand("sync", "--strict", "--prune-stale")

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "broken-skill") {
		t.Fatalf("--strict --prune-stale deleted a selection whose skill merely failed to parse:\n%s\noutput:\n%s", data, output)
	}
}

// TestSyncRepeatedRunsNeverPruneMalformedSelection attacks the "second sync"
// path: pruning re-runs sync, and a malformed selection must not become
// prunable on any subsequent pass.
func TestSyncRepeatedRunsNeverPruneMalformedSelection(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "repeat-prune-malformed.git")
	writeSkill(t, working, "valid-skill", "Valid skill", nil)
	writeFile(t, filepath.Join(working, "skills", "broken-skill", "SKILL.md"), brokenSkillFrontmatter("broken-skill"))
	commitAndPushAll(t, working, "Add valid and malformed skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - valid-skill
      - broken-skill
      - deleted-skill
`)

	withWorkingDir(t, repoRoot)

	for i := 0; i < 3; i++ {
		output, err := executeCommand("sync", "--prune-stale")
		if err != nil {
			t.Fatalf("sync run %d failed: %v\n%s", i, err, output)
		}
		data, readErr := os.ReadFile(configPath)
		if readErr != nil {
			t.Fatalf("read config: %v", readErr)
		}
		if !strings.Contains(string(data), "broken-skill") {
			t.Fatalf("run %d pruned the malformed selection:\n%s", i, data)
		}
	}
}

// TestSyncMalformedSkillDoesNotBlockPruningAStaleCommandOfTheSameName is the
// cross-kind consequence at CLI level: a malformed SKILL named "shared" must
// not reclassify a genuinely stale COMMAND selection of the same name as
// "present but broken" — which would both misname the file to fix and stop
// --prune-stale from cleaning up a truly dead selection.
func TestSyncMalformedSkillDoesNotBlockPruningAStaleCommandOfTheSameName(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "cross-kind-malformed.git")
	writeSkill(t, working, "valid-skill", "Valid skill", nil)
	writeFile(t, filepath.Join(working, "skills", "shared", "SKILL.md"), brokenSkillFrontmatter("shared"))
	commitAndPushAll(t, working, "Add a malformed skill named shared")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - valid-skill
    commands:
      - shared
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync", "--prune-stale")
	if err != nil {
		t.Fatalf("expected sync to succeed: %v\n%s", err, output)
	}
	if strings.Contains(output, `command "shared" was selected and found`) {
		t.Fatalf("a malformed skill was blamed for an absent command:\n%s", output)
	}
	if !strings.Contains(output, `pruned stale command "shared"`) {
		t.Fatalf("expected the genuinely stale command selection to be pruned, got:\n%s", output)
	}
	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	if strings.Contains(string(data), "- shared") {
		t.Fatalf("expected the stale command selection to be gone from config:\n%s", data)
	}
}

// TestSyncDiagnosesMalformedRootLevelSkill covers the bundle whose ROOT is the
// resource. The ingester reports that failure with DirName "." and the bundle
// root as the path, so neither names the resource — only the recovered
// frontmatter name does. Without it the user is told a skill that is sitting
// right there was removed upstream.
func TestSyncDiagnosesMalformedRootLevelSkill(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "root-skill-malformed.git")
	writeFile(t, filepath.Join(working, "SKILL.md"), brokenSkillFrontmatter("root-skill"))
	commitAndPushAll(t, working, "Add a malformed root-level skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - root-skill
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync to succeed with a soft warning: %v\n%s", err, output)
	}
	if !strings.Contains(output, "failed to parse") {
		t.Fatalf("expected the parse-failure diagnosis for a root-level skill, got:\n%s", output)
	}
	assertNoRemovalGuessOutput(t, output)
}

// TestSyncMalformedSkillDoesNotFailAnUnrelatedUsersSync pins the blast radius
// end to end: a broken resource in a shared registry that this user never
// selected must not change the exit code or emit a selection warning.
func TestSyncMalformedSkillDoesNotFailAnUnrelatedUsersSync(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "unrelated-malformed.git")
	writeSkill(t, working, "valid-skill", "Valid skill", nil)
	writeFile(t, filepath.Join(working, "skills", "broken-skill", "SKILL.md"), brokenSkillFrontmatter("broken-skill"))
	commitAndPushAll(t, working, "Add valid and malformed skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - valid-skill
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync", "--strict")
	if err != nil {
		t.Fatalf("an unselected malformed resource must not fail even a strict sync: %v\n%s", err, output)
	}
	if strings.Contains(output, "was selected and found") {
		t.Fatalf("unexpected selection diagnosis for an unselected resource:\n%s", output)
	}
}

// TestSyncRequiredRegistryMalformedSelectionFailsTheRun keeps policy
// enforcement intact: a required registry whose selected resource cannot be
// parsed is a hard failure, not a warning.
func TestSyncRequiredRegistryMalformedSelectionFailsTheRun(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "required-malformed.git")
	writeSkill(t, working, "valid-skill", "Valid skill", nil)
	writeFile(t, filepath.Join(working, "skills", "broken-skill", "SKILL.md"), brokenSkillFrontmatter("broken-skill"))
	commitAndPushAll(t, working, "Add valid and malformed skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    required: true
    skills:
      - valid-skill
      - broken-skill
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err == nil {
		t.Fatalf("expected a required registry's malformed selection to fail the run, got:\n%s", output)
	}
	if !strings.Contains(err.Error()+output, "failed to parse") {
		t.Fatalf("expected the parse failure in the error, got %v\n%s", err, output)
	}
}
