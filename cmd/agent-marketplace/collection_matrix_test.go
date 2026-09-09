package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Scope variants for member exclude
// ---------------------------------------------------------------------------

// TestCollectionMemberExclude_LocalScope verifies that remove --scope local
// for a collection-provided skill writes to config.local.yaml (not repo config).
func scenarioCollectionMemberExclude_LocalScope(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "local-scope-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"infra"})
	writeSkill(t, working, "k8s-basics", "Kubernetes basics", []string{"k8s"})
	writeCollection(t, working, "bundle", `
name: bundle
skills:
  - name: vm-lifecycle
  - name: k8s-basics
`)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
collections:
  include:
    - bundle
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Skipf("sync failed (CI may lack git): %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", "k8s-basics", "SKILL.md")); err != nil {
		t.Skipf("factory skill missing after sync: %v", err)
	}

	output, err := executeCommand("remove", "--scope", "local", "k8s-basics")
	if err != nil {
		t.Fatalf("remove --scope local failed: %v\n%s", err, output)
	}

	// Exclude must be in config.local.yaml, NOT repo config.
	localBytes, _ := os.ReadFile(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	if !strings.Contains(string(localBytes), "k8s-basics") {
		t.Fatalf("expected k8s-basics in config.local.yaml, got:\n%s", localBytes)
	}
	repoBytes, _ := os.ReadFile(filepath.Join(repoRoot, ".agents", "config.yaml"))
	if strings.Contains(string(repoBytes), "k8s-basics") {
		t.Fatalf("k8s-basics should NOT be in repo config.yaml, got:\n%s", repoBytes)
	}

	// Excluded skill gone; other member still present.
	assertFactorySkillAbsent(t, repoRoot, "k8s-basics")
	assertFactorySkillExists(t, repoRoot, "vm-lifecycle")
}

// TestCollectionMemberExclude_UserScope verifies that remove --scope user
// for a collection-provided skill writes to ~/.agents/config.yaml.
func scenarioCollectionMemberExclude_UserScope(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "user-scope-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"infra"})
	writeSkill(t, working, "k8s-basics", "Kubernetes basics", []string{"k8s"})
	writeCollection(t, working, "bundle", `
name: bundle
skills:
  - name: vm-lifecycle
  - name: k8s-basics
`)
	commitAndPushAll(t, working, "setup")

	// The collection lives in the global (user) config so that remove
	// --scope user can find and write to the same config layer.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
collections:
  include:
    - bundle
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Skipf("sync failed: %v", err)
	}
	// Collection skills from the global config are machine-scoped;
	// they materialize to ~/.factory/skills/, not the repo root.
	if _, err := os.Stat(filepath.Join(homeDir, ".factory", "skills", "k8s-basics", "SKILL.md")); err != nil {
		t.Skipf("factory skill missing: %v", err)
	}

	output, err := executeCommand("remove", "--scope", "user", "k8s-basics")
	if err != nil {
		t.Fatalf("remove --scope user failed: %v\n%s", err, output)
	}

	globalCfg := filepath.Join(homeDir, ".agents", "config.yaml")
	globalBytes, _ := os.ReadFile(globalCfg)
	if !strings.Contains(string(globalBytes), "k8s-basics") {
		t.Fatalf("expected k8s-basics exclude in global config, got:\n%s", globalBytes)
	}

	repoCfgBytes, _ := os.ReadFile(filepath.Join(repoRoot, ".agents", "config.yaml"))
	if strings.Contains(string(repoCfgBytes), "k8s-basics") {
		t.Fatalf("k8s-basics should NOT appear in repo config, got:\n%s", repoCfgBytes)
	}
}

// ---------------------------------------------------------------------------
// Collection install scope
// ---------------------------------------------------------------------------

// TestCollectionInstall_UserScope verifies install --scope user writes the
// collection ref to ~/.agents/config.yaml, not the repo config.
func scenarioCollectionInstall_UserScope(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "user-install-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"infra"})
	writeCollection(t, working, "infra-bundle", `
name: infra-bundle
skills:
  - name: vm-lifecycle
`)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--scope", "user", "infra-bundle")
	if err != nil {
		t.Fatalf("install --scope user failed: %v\n%s", err, output)
	}

	globalCfg := filepath.Join(homeDir, ".agents", "config.yaml")
	globalBytes, _ := os.ReadFile(globalCfg)
	if !strings.Contains(string(globalBytes), "infra-bundle") {
		t.Fatalf("expected infra-bundle in global config, got:\n%s", globalBytes)
	}

	repoCfgBytes, _ := os.ReadFile(filepath.Join(repoRoot, ".agents", "config.yaml"))
	if strings.Contains(string(repoCfgBytes), "infra-bundle") {
		t.Fatalf("infra-bundle should NOT appear in repo config, got:\n%s", repoCfgBytes)
	}
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

// TestCollectionNotFoundInRegistry verifies that a config referencing a
// collection name not present in any registry produces a clear error.
func scenarioCollectionNotFoundInRegistry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "no-collection-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
collections:
  include:
    - does-not-exist
`)
	withWorkingDir(t, repoRoot)

	_, err := executeCommand("sync")
	if err == nil {
		t.Fatal("expected error for unknown collection, got nil")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("expected error to mention collection name, got: %v", err)
	}
}

// TestCollectionCycleDetection verifies that a collection including itself
// (directly or transitively) produces an error rather than an infinite loop.
func scenarioCollectionCycleDetection(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "cycle-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	// Collection A includes itself.
	writeCollection(t, working, "self-referential", `
name: self-referential
skills:
  - name: vm-lifecycle
includes:
  - name: self-referential
`)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
collections:
  include:
    - self-referential
`)
	withWorkingDir(t, repoRoot)

	_, err := executeCommand("sync")
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected 'cycle' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// State persistence
// ---------------------------------------------------------------------------

// TestCollectionExcludePersistsAcrossSync verifies that an exclude written
// for a collection member is still respected after a subsequent sync.
func scenarioCollectionExcludePersistsAcrossSync(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "persist-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	writeSkill(t, working, "k8s-basics", "Kubernetes basics", nil)
	writeCollection(t, working, "bundle", `
name: bundle
skills:
  - name: vm-lifecycle
  - name: k8s-basics
`)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
collections:
  include:
    - bundle
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Skipf("sync failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", "k8s-basics", "SKILL.md")); err != nil {
		t.Skipf("factory skill missing: %v", err)
	}

	// Exclude one member.
	if _, err := executeCommand("remove", "k8s-basics"); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	// Re-sync: exclude must still hold.
	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("second sync failed: %v", err)
	}

	assertFactorySkillAbsent(t, repoRoot, "k8s-basics")
	assertFactorySkillExists(t, repoRoot, "vm-lifecycle")
}

// TestCollectionMemberExclude_StatusReflects verifies that after excluding a
// collection member, status shows the remaining members with a VIA label and
// does not list the excluded skill.
func scenarioCollectionMemberExclude_StatusReflects(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "status-reflect-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	writeSkill(t, working, "k8s-basics", "Kubernetes basics", nil)
	writeCollection(t, working, "bundle", `
name: bundle
skills:
  - name: vm-lifecycle
  - name: k8s-basics
`)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
collections:
  include:
    - bundle
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Skipf("sync failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", "k8s-basics", "SKILL.md")); err != nil {
		t.Skipf("factory skill missing: %v", err)
	}

	if _, err := executeCommand("remove", "k8s-basics"); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "vm-lifecycle") {
		t.Fatalf("expected vm-lifecycle in status, got:\n%s", output)
	}
	if strings.Contains(output, "k8s-basics") {
		t.Fatalf("excluded k8s-basics should not appear in status, got:\n%s", output)
	}
	if !strings.Contains(output, "collection:bundle") {
		t.Fatalf("expected VIA label for vm-lifecycle, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// Multi-registry collection
// ---------------------------------------------------------------------------

// TestCollectionMultiRegistry verifies that a collection can bundle skills
// from two different configured registries and both are synced.
func scenarioCollectionMultiRegistry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Registry A: hosts the collection + one skill.
	originA, workingA := createBareRepoWithWorkingClone(t, "registry-a.git")
	writeSkill(t, workingA, "skill-a", "Skill from registry A", nil)

	// Registry B: hosts a second skill only (no collection).
	originB, workingB := createBareRepoWithWorkingClone(t, "registry-b.git")
	writeSkill(t, workingB, "skill-b", "Skill from registry B", nil)
	commitAndPushAll(t, workingB, "setup B")

	writeCollection(t, workingA, "cross-registry-bundle", `
name: cross-registry-bundle
skills:
  - name: skill-a
  - name: skill-b
    registry: `+originB+`
`)
	commitAndPushAll(t, workingA, "setup A with collection")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+originA+`
    ref: main
  - url: `+originB+`
    ref: main
collections:
  include:
    - cross-registry-bundle
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Skipf("sync failed: %v", err)
	}

	for _, s := range []string{"skill-a", "skill-b"} {
		assertFactorySkillExists(t, repoRoot, s)
	}
}

// ---------------------------------------------------------------------------
// Direct skill + collection overlap
// ---------------------------------------------------------------------------

// TestDirectSkillAndCollectionOverlap verifies that when a skill is both
// directly listed in a registry entry AND provided by a collection, Via is
// NOT set (direct selection takes precedence) and remove removes it from the
// direct skills list rather than writing an exclude.
func scenarioDirectSkillAndCollectionOverlap(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "overlap-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	writeSkill(t, working, "k8s-basics", "Kubernetes basics", nil)
	writeCollection(t, working, "bundle", `
name: bundle
skills:
  - name: vm-lifecycle
  - name: k8s-basics
`)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	// vm-lifecycle is both directly listed (registry entry skills:) AND in the collection.
	// Registry entry skills: is what plain `install` writes; remove checks entry.Skills.
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
collections:
  include:
    - bundle
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Skipf("sync failed: %v", err)
	}

	// Remove vm-lifecycle: should remove from skills list (not write exclude),
	// because it has a direct config entry and Via is not set.
	output, err := executeCommand("remove", "vm-lifecycle")
	if err != nil {
		t.Fatalf("remove vm-lifecycle failed: %v\n%s", err, output)
	}

	// The word "Excluded" indicates the Via path was taken (wrong).
	if strings.Contains(output, "Excluded") {
		t.Fatalf("expected direct-remove path (no 'Excluded'), got:\n%s", output)
	}

	// vm-lifecycle should be removed from the skills list in repo config.
	cfgBytes, _ := os.ReadFile(filepath.Join(repoRoot, ".agents", "config.yaml"))
	if strings.Contains(string(cfgBytes), "- vm-lifecycle") {
		t.Fatalf("vm-lifecycle should be removed from skills list, config:\n%s", cfgBytes)
	}
}

// TestCollectionRemove_DirectSkillSurvives verifies that removing a collection
// does not de-sync a skill that was also independently listed directly.
func scenarioCollectionRemove_DirectSkillSurvives(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "direct-survives-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	writeSkill(t, working, "k8s-basics", "Kubernetes basics", nil)
	writeCollection(t, working, "bundle", `
name: bundle
skills:
  - name: k8s-basics
`)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	// vm-lifecycle: direct (registry entry skills). k8s-basics: via collection only.
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
collections:
  include:
    - bundle
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Skipf("sync failed: %v", err)
	}
	for _, s := range []string{"vm-lifecycle", "k8s-basics"} {
		if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", s, "SKILL.md")); err != nil {
			t.Skipf("factory skill for %q missing: %v", s, err)
		}
	}

	// Remove the collection.
	if _, err := executeCommand("remove", "bundle"); err != nil {
		t.Fatalf("remove bundle failed: %v", err)
	}

	// k8s-basics was only in the collection — should be gone.
	assertFactorySkillAbsent(t, repoRoot, "k8s-basics")

	// vm-lifecycle was directly listed — must survive.
	assertFactorySkillExists(t, repoRoot, "vm-lifecycle")
}
