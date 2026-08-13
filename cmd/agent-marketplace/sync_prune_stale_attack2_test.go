package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pruneFixture builds the simplest possible prune scenario: one healthy skill
// plus stale names that do not exist upstream, supplied by the caller through
// the configBody function. It returns the repo root and config path.
func pruneFixture(t *testing.T, bareName string, configBody func(origin string) string) (string, string) {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, bareName)
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "Add skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, configBody(origin))
	withWorkingDir(t, repoRoot)
	return repoRoot, configPath
}

// ATTACK B1: the stale selection lives in the TOP-LEVEL `skills:` list, not in
// a per-registry include. pruneStaleSelections calls
// DisableResourceWithShadowFallback, which only knows how to edit per-registry
// includes; when the name is not in the entry's include it falls through to
// "add an exclude". The CLI then prints `pruned stale skill "gone" from
// config` while the selection is still there.
func TestSyncPruneStaleRemovesTopLevelSelection(t *testing.T) {
	_, configPath := pruneFixture(t, "top-level-sel.git", func(origin string) string {
		return `
registries:
  - url: ` + origin + `
    ref: main
skills:
  include:
    - keeper
    - gone
`
	})

	output, _ := executeCommand("sync", "--prune-stale")
	t.Logf("output:\n%s", output)
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	t.Logf("config after prune:\n%s", configData)
	assertValidConfigYAML(t, configData)

	claimedPruned := strings.Contains(output, `pruned stale skill "gone" from config`)
	if !claimedPruned {
		t.Fatalf("expected the stale top-level selection to be pruned; output:\n%s", output)
	}
	stillSelected := false
	for _, line := range strings.Split(string(configData), "\n") {
		if strings.TrimSpace(line) == "- gone" {
			stillSelected = true
		}
	}
	if stillSelected {
		t.Errorf("ATTACK SUCCEEDED (config churn): the stale top-level selection is still present.\nconfig:\n%s\noutput:\n%s", configData, output)
	}
	if strings.Contains(string(configData), "exclude") {
		t.Errorf("ATTACK SUCCEEDED (config churn): the prune wrote an `exclude:` block the user never asked for.\nconfig:\n%s", configData)
	}
}

// ATTACK B2: the same name appears in BOTH the top-level list and a
// per-registry include. A single prune pass must clear both, otherwise the
// next run finds it stale again.
func TestAttackPruneSelectionInBothScopes(t *testing.T) {
	_, configPath := pruneFixture(t, "both-scopes.git", func(origin string) string {
		return `
registries:
  - url: ` + origin + `
    ref: main
    skills:
      - keeper
      - gone
skills:
  include:
    - gone
`
	})

	out1, _ := executeCommand("sync", "--prune-stale")
	after1, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	t.Logf("out1:\n%s\nconfig1:\n%s", out1, after1)
	assertValidConfigYAML(t, after1)

	out2, _ := executeCommand("sync", "--prune-stale")
	after2, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	t.Logf("out2:\n%s\nconfig2:\n%s", out2, after2)

	if string(after1) != string(after2) {
		t.Errorf("ATTACK SUCCEEDED: prune is not idempotent when a selection is in two scopes.\nafter 1:\n%s\nafter 2:\n%s", after1, after2)
	}
	if strings.Contains(out2, "pruned stale") {
		t.Errorf("ATTACK SUCCEEDED: the second run pruned again — the first pass did not finish the job.\nout2:\n%s", out2)
	}
}

// ATTACK B3: the repo config is a SYMLINK (a common dotfile setup: the repo's
// .agents/config.yaml points at a shared file). writeConfigDocument renames a
// temp file over configPath, which REPLACES the symlink with a regular file and
// leaves the real target unedited.
func TestAttackPruneThroughSymlinkedConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "symlink-config.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "Add skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	realConfig := filepath.Join(t.TempDir(), "shared", "config.yaml")
	writeConfig(t, realConfig, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - keeper
      - gone
`)
	linkPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(realConfig, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	withWorkingDir(t, repoRoot)

	output, _ := executeCommand("sync", "--prune-stale")
	t.Logf("output:\n%s", output)

	if !strings.Contains(output, "pruned stale") {
		t.Skip("nothing pruned; attack not applicable")
	}
	fi, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("ATTACK SUCCEEDED (config corruption): the prune replaced the user's config SYMLINK with a regular file, detaching it from %s", realConfig)
	}
	shared, err := os.ReadFile(realConfig)
	if err != nil {
		t.Fatalf("read shared config: %v", err)
	}
	if strings.Contains(string(shared), "- gone") {
		t.Errorf("ATTACK SUCCEEDED (config corruption): the shared config the symlink pointed at was never edited, so the prune's claim is false.\nshared:\n%s", shared)
	}
}

// ATTACK B4: a multi-document YAML config. parseConfigDocument unmarshals into
// a single yaml.Node, which captures only the FIRST document; writing it back
// silently DELETES every subsequent document.
func TestAttackPruneMultiDocumentConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "multidoc.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "Add skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeFile(t, configPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      - keeper
      - gone
---
# a second document the user keeps notes in
notes: keep me
`)
	withWorkingDir(t, repoRoot)

	output, _ := executeCommand("sync", "--prune-stale")
	t.Logf("output:\n%s", output)
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	t.Logf("config after prune:\n%s", configData)

	if strings.Contains(output, "pruned stale") && !strings.Contains(string(configData), "notes: keep me") {
		t.Errorf("ATTACK SUCCEEDED (config corruption): the prune silently DELETED the second YAML document.\nconfig:\n%s", configData)
	}
}

// ATTACK B5 — CONFIRMED DEFECT, NOT FIXED HERE.
//
// A resource that exists upstream but whose SKILL.md frontmatter is broken
// never reaches catalog Entries. It lands in CatalogLoad.Malformed, which
// produces NO catalog error, so CatalogTrusted() is true and the resource is
// reported "selected but not found" — and its selection is DELETED. The run
// even prints "Malformed 1" on the same summary line as the prune, so the
// trust model is ignoring a signal it already surfaces. The user's fix is to
// repair the file, not to lose the selection.
//
// This is pre-existing (the old len(errs)==0 gate deleted it too, since a
// malformed resource contributes no error), and the sibling bead that adds
// resolver.SelectedMalformedError owns the correct fix: classify the resource
// as malformed rather than absent, so the narrow type predicate in
// runSync (`*resolver.SelectedNotFoundError` only) stops matching it.
// Implementing a second, name-based guard here would collide with that.
//
// The sibling bead (agent-marketplace-4pip, resolver.SelectedMalformedError) has
// since landed on this branch, so the skip is gone and this must pass: a
// malformed-but-present selection is reported as a parse failure, which is NOT a
// SelectedNotFoundError and therefore is never prunable.
func TestAttackPruneBrokenFrontmatter(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "broken-frontmatter.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	writeFile(t, filepath.Join(working, "skills", "broken", "SKILL.md"),
		"---\nname: broken\ndescription: \"unterminated\ntags: [a, b\n---\n\n# broken\n")
	commitAndPushAll(t, working, "Add a skill with broken frontmatter")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - keeper
      - broken
`)
	withWorkingDir(t, repoRoot)

	output, _ := executeCommand("sync", "--prune-stale")
	t.Logf("output:\n%s", output)
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	t.Logf("config after prune:\n%s", configData)

	if strings.Contains(output, `pruned stale skill "broken"`) {
		t.Errorf("ATTACK SUCCEEDED (wrongly deleted config): the selection for a MALFORMED-but-present resource was deleted; the resource is still upstream.\nconfig:\n%s\noutput:\n%s", configData, output)
	}
}

// ATTACK B6: --strict --prune-stale where the re-sync surfaces a NEW stale
// selection. Pruning `gone-a` removes it; the second pass must still promote
// anything else that is stale, and the run must exit non-zero.
func TestAttackStrictReSyncSurfacesNewStale(t *testing.T) {
	_, configPath := pruneFixture(t, "strict-new-stale.git", func(origin string) string {
		return `
registries:
  - url: ` + origin + `
    ref: main
    skills:
      - keeper
      - gone-a
collections:
  include:
    - no-such-collection
`
	})

	output, err := executeCommand("sync", "--strict", "--prune-stale")
	t.Logf("output:\n%s\nerr: %v", output, err)
	if err == nil {
		t.Errorf("ATTACK SUCCEEDED (wrong exit code): --strict exited 0 with an unresolvable collection still in the config.\noutput:\n%s", output)
	}
	configData, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	if !strings.Contains(string(configData), "no-such-collection") {
		t.Errorf("ATTACK SUCCEEDED (wrongly deleted config): the collection reference was pruned.\nconfig:\n%s", configData)
	}
}

// ATTACK B8: the config is modified between the prune and the re-sync is not
// something we can race deterministically, but we CAN check the weaker
// invariant that matters: the prune must never remove a selection whose
// resource is present in a DIFFERENT registry entry of the same config.
func TestAttackPruneSelectionSatisfiedByAnotherRegistry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	originA, workingA := createBareRepoWithWorkingClone(t, "reg-a.git")
	writeSkill(t, workingA, "keeper", "Keeper", nil)
	commitAndPushAll(t, workingA, "Add keeper")

	originB, workingB := createBareRepoWithWorkingClone(t, "reg-b.git")
	writeSkill(t, workingB, "shared", "Shared skill, only in B", nil)
	commitAndPushAll(t, workingB, "Add shared")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	// `shared` is selected at the top level, so it applies to both entries.
	writeConfig(t, configPath, `
registries:
  - url: `+originA+`
    ref: main
    skills:
      - keeper
  - url: `+originB+`
    ref: main
    skills:
      - shared
skills:
  include:
    - shared
`)
	withWorkingDir(t, repoRoot)

	output, _ := executeCommand("sync", "--prune-stale")
	t.Logf("output:\n%s", output)
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	t.Logf("config after prune:\n%s", configData)
	assertValidConfigYAML(t, configData)

	if strings.Contains(output, `pruned stale skill "shared"`) {
		t.Errorf("ATTACK SUCCEEDED (wrongly deleted config): %q was pruned although registry B provides it.\nconfig:\n%s\noutput:\n%s", "shared", configData, output)
	}
	if !strings.Contains(string(configData), "shared") {
		t.Errorf("ATTACK SUCCEEDED (wrongly deleted config): every trace of %q was removed although registry B provides it.\nconfig:\n%s", "shared", configData)
	}
	if _, statErr := os.Lstat(filepath.Join(repoRoot, ".factory", "skills", "shared")); statErr != nil {
		t.Errorf("ATTACK SUCCEEDED: %q is no longer materialized after the prune: %v\noutput:\n%s", "shared", statErr, output)
	}
}
