package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// brokenSkillFrontmatter is a SKILL.md whose YAML frontmatter cannot be parsed
// (the description list is unterminated) but whose `name:` line is still
// readable, so both the directory-name and frontmatter-name correlation paths
// can be exercised from one fixture shape.
func brokenSkillFrontmatter(name string) string {
	return `---
name: ` + name + `
description: [unterminated
metadata:
  tags:
    - broken
---

# Broken
`
}

// assertNoRemovalGuessOutput fails when CLI output blames an upstream removal,
// a stale ref pin, or a cache issue for a resource that is present upstream but
// failed ingest validation.
func assertNoRemovalGuessOutput(t *testing.T, output string) {
	t.Helper()
	for _, phrase := range []string{
		"upstream removal",
		"stale ref pin",
		"cache issue",
		"to drop the selection",
	} {
		if strings.Contains(output, phrase) {
			t.Fatalf("sync must not blame an upstream removal (%q) for a malformed skill, got:\n%s", phrase, output)
		}
	}
}

// TestSyncCommandDiagnosesSelectedButMalformedSkill is the end-to-end
// regression test for bead agent-marketplace-4pip: a selected skill that fails
// ingest validation must be diagnosed with its file and parse error, not
// reported as though it had been deleted upstream.
func TestSyncCommandDiagnosesSelectedButMalformedSkill(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "malformed-selected.git")
	writeSkill(t, working, "valid-skill", "Valid skill", []string{"tested"})
	writeFile(t, filepath.Join(working, "skills", "broken-skill", "SKILL.md"), brokenSkillFrontmatter("broken-skill"))
	commitAndPushAll(t, working, "Add valid and malformed skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - valid-skill
      - broken-skill
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync to succeed with a soft warning: %v\n%s", err, output)
	}

	for _, needle := range []string{
		`skill "broken-skill"`,
		"failed to parse",
		filepath.Join("skills", "broken-skill", "SKILL.md"),
		"did not find expected ',' or ']'",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected sync output to contain %q, got:\n%s", needle, output)
		}
	}
	if strings.Contains(output, "was selected but not found") {
		t.Fatalf("expected the malformed diagnosis instead of a not-found warning, got:\n%s", output)
	}
	assertNoRemovalGuessOutput(t, output)

	// The healthy selection still materializes.
	assertFactorySkillExists(t, repoRoot, "valid-skill")
}

// TestSyncCommandDiagnosesMalformedSkillSelectedByFrontmatterName covers the
// case that most resembles a rename: the selection uses the frontmatter name
// while the dropped resource is only known to the ingester by its directory.
func TestSyncCommandDiagnosesMalformedSkillSelectedByFrontmatterName(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "malformed-frontmatter-name.git")
	writeSkill(t, working, "valid-skill", "Valid skill", nil)
	writeFile(t, filepath.Join(working, "skills", "bubbletea-tui", "SKILL.md"), brokenSkillFrontmatter("bubble-tea"))
	commitAndPushAll(t, working, "Add skill whose frontmatter name differs from its directory")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - valid-skill
      - bubble-tea
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync to succeed with a soft warning: %v\n%s", err, output)
	}

	for _, needle := range []string{
		`skill "bubble-tea"`,
		"failed to parse",
		filepath.Join("skills", "bubbletea-tui", "SKILL.md"),
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected sync output to contain %q, got:\n%s", needle, output)
		}
	}
	assertNoRemovalGuessOutput(t, output)
}

// TestSyncCommandGenuinelyAbsentSkillKeepsNotFoundRemediation pins the
// behavior that must NOT change: a skill that really is gone upstream keeps the
// existing warning and its remove-the-selection remediation.
func TestSyncCommandGenuinelyAbsentSkillKeepsNotFoundRemediation(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "absent-and-malformed.git")
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
      - beads
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync to succeed with a soft warning: %v\n%s", err, output)
	}

	if !strings.Contains(output, `skill "beads" was selected but not found`) {
		t.Fatalf("expected the unchanged not-found warning for a genuinely absent skill, got:\n%s", output)
	}
	if !strings.Contains(output, "upstream removal") || !strings.Contains(output, "to drop the selection") {
		t.Fatalf("expected the unchanged not-found remediation, got:\n%s", output)
	}
}

// TestSyncPruneStaleDoesNotPruneMalformedSelection is the sharpest form of the
// misdiagnosis this bead fixes: --prune-stale acts on stale selections, and a
// selection whose resource is present but unparseable is not stale. Pruning it
// would delete a working selection over a registry-side syntax error, while the
// genuinely-absent selection alongside it is still pruned.
func TestSyncPruneStaleDoesNotPruneMalformedSelection(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "prune-vs-malformed.git")
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

	output, err := executeCommand("sync", "--prune-stale")
	if err != nil {
		t.Fatalf("expected sync --prune-stale to succeed: %v\n%s", err, output)
	}

	if strings.Contains(output, `pruned stale skill "broken-skill"`) {
		t.Fatalf("a malformed selection must never be pruned, got:\n%s", output)
	}
	if !strings.Contains(output, `pruned stale skill "deleted-skill"`) {
		t.Fatalf("expected the genuinely absent selection to still be pruned, got:\n%s", output)
	}

	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(configData), "broken-skill") {
		t.Fatalf("expected broken-skill to remain selected in config, got:\n%s", configData)
	}
}

// TestSyncCommandDefaultOutputPointsAtMalformedDetail closes the
// discoverability gap: default output reports only a count, so it must name the
// command that explains it. It must NOT start dumping the detail itself —
// TestSyncCommandIncludesMalformedSkillsInSummary pins that suppression.
func TestSyncCommandDefaultOutputPointsAtMalformedDetail(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "malformed-hint.git")
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

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Malformed 1") {
		t.Fatalf("expected the malformed count in the summary, got:\n%s", output)
	}
	if !strings.Contains(output, "sync --verbose") {
		t.Fatalf("expected default output to name the command that explains the malformed count, got:\n%s", output)
	}
	if strings.Contains(output, "malformed (1):") {
		t.Fatalf("default output must not dump malformed detail, got:\n%s", output)
	}
}
