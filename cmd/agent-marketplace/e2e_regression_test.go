package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestE2E002003ManualSkillsSurviveSync(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "manual-survive.git")
	writeSkill(t, working, "managed-skill", "Managed", []string{"test"})
	commitAndPushAll(t, working, "Add managed skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses:
  - factory
  - windsurf
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/managed-skill]
`)
	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected first sync to succeed: %v\n%s", err, output)
	}

	manualFactorySkill := filepath.Join(repoRoot, ".factory", "skills", "my-manual-skill", "SKILL.md")
	manualWindsurfSkill := filepath.Join(repoRoot, ".windsurf", "skills", "my-manual-skill", "SKILL.md")
	writeFile(t, manualFactorySkill, "# Manual factory skill\n")
	writeFile(t, manualWindsurfSkill, "# Manual windsurf skill\n")

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected second sync to succeed: %v\n%s", err, output)
	}

	if _, err := os.Stat(manualFactorySkill); err != nil {
		t.Fatalf("expected manual factory skill to survive sync: %v", err)
	}
	if _, err := os.Stat(manualWindsurfSkill); err != nil {
		t.Fatalf("expected manual windsurf skill to survive sync: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repoRoot, ".factory", "skills", "managed-skill")); err != nil {
		t.Fatalf("expected managed factory skill to remain materialized: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repoRoot, ".windsurf", "skills", "managed-skill")); err != nil {
		t.Fatalf("expected managed windsurf skill to remain materialized: %v", err)
	}
}

func TestE2E019HarnessRemovalPrunesArtifacts(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "harness-prune.git")
	writeSkill(t, working, "shared-skill", "Shared", []string{"test"})
	commitAndPushAll(t, working, "Add shared skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v4
harnesses:
  - factory
  - windsurf
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/shared-skill]
`)
	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected first sync to succeed: %v\n%s", err, output)
	}

	factorySkill := filepath.Join(repoRoot, ".factory", "skills", "shared-skill")
	windsurfSkill := filepath.Join(repoRoot, ".windsurf", "skills", "shared-skill")
	if _, err := os.Lstat(factorySkill); err != nil {
		t.Fatalf("expected factory skill to exist after first sync: %v", err)
	}
	if _, err := os.Lstat(windsurfSkill); err != nil {
		t.Fatalf("expected windsurf skill to exist after first sync: %v", err)
	}

	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/shared-skill]
`)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected second sync to succeed: %v\n%s", err, output)
	}

	if _, err := os.Lstat(factorySkill); err != nil {
		t.Fatalf("expected factory skill to remain after harness removal: %v", err)
	}
	if _, err := os.Lstat(windsurfSkill); err == nil {
		t.Fatal("expected windsurf skill to be pruned after removing windsurf harness, but it still exists")
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected error checking windsurf skill: %v", err)
	}
}

func TestE2E006014033DuplicateURLInstallRemoveWithPathDisambiguation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "duplicate-url.git")
	writeSkill(t, filepath.Join(working, "skills"), "brainstorming", "Brainstorming", []string{"test"})
	writeSkill(t, filepath.Join(working, "openspec"), "spec-review", "Spec review", []string{"test"})
	commitAndPushAll(t, working, "Add skills in two subpaths")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  skills: {git: file://`+origin+`, ref: main, path: skills}
  openspec: {git: file://`+origin+`, ref: main, path: openspec}
`)
	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("install", "brainstorming"); err != nil {
		t.Fatalf("expected install to succeed with duplicate URL + path disambiguation: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after install: %v", err)
	}
	if !slices.Contains(skillsForRegistryPath(t, cfg, "skills"), "brainstorming") {
		t.Fatalf("expected brainstorming to be added to registry path=skills, got %#v", cfg.Registries)
	}
	if slices.Contains(skillsForRegistryPath(t, cfg, "openspec"), "brainstorming") {
		t.Fatalf("expected brainstorming not to be added to registry path=openspec, got %#v", cfg.Registries)
	}

	if output, err := executeCommand("remove", "brainstorming"); err != nil {
		t.Fatalf("expected remove to succeed with duplicate URL + path disambiguation: %v\n%s", err, output)
	}

	cfg, err = config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after remove: %v", err)
	}
	if slices.Contains(skillsForRegistryPath(t, cfg, "skills"), "brainstorming") {
		t.Fatalf("expected brainstorming to be removed from registry path=skills, got %#v", cfg.Registries)
	}

	if output, err := executeCommand("install", "--all"); err != nil {
		t.Fatalf("expected install --all to succeed with duplicate URL + path disambiguation: %v\n%s", err, output)
	}

	cfg, err = config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after install --all: %v", err)
	}
	if !slices.Contains(skillsForRegistryPath(t, cfg, "skills"), "brainstorming") {
		t.Fatalf("expected brainstorming in registry path=skills after install --all, got %#v", cfg.Registries)
	}
	if !slices.Contains(skillsForRegistryPath(t, cfg, "openspec"), "spec-review") {
		t.Fatalf("expected spec-review in registry path=openspec after install --all, got %#v", cfg.Registries)
	}

	if _, err := os.Lstat(filepath.Join(repoRoot, ".factory", "skills", "brainstorming")); err != nil {
		t.Fatalf("expected brainstorming to be materialized after install --all: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repoRoot, ".factory", "skills", "spec-review")); err != nil {
		t.Fatalf("expected spec-review to be materialized after install --all: %v", err)
	}
}

func TestE2E009WindsurfMachineScopePrunesOnUserScopeRemove(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "machine-prune.git")
	writeSkill(t, working, "user-skill", "User skill", []string{"test"})
	commitAndPushAll(t, working, "Add user skill")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses:
  - factory
  - windsurf
sources:
  hosted: {git: file://`+origin+`, ref: main}
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses:
  - factory
  - windsurf
sources:
  hosted: {git: file://`+origin+`, ref: main}
`)
	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("install", "--scope", "user", "user-skill"); err != nil {
		t.Fatalf("expected install --scope user to succeed: %v\n%s", err, output)
	}

	factoryMachineSkill := filepath.Join(homeDir, ".factory", "skills", "user-skill")
	windsurfMachineSkill := filepath.Join(homeDir, ".codeium", "windsurf", "skills", "user-skill")
	if _, err := os.Lstat(factoryMachineSkill); err != nil {
		t.Fatalf("expected factory machine-scope skill to exist after install: %v", err)
	}
	if _, err := os.Lstat(windsurfMachineSkill); err != nil {
		t.Fatalf("expected windsurf machine-scope skill to exist after install: %v", err)
	}

	if output, err := executeCommand("remove", "--scope", "user", "user-skill"); err != nil {
		t.Fatalf("expected remove --scope user to succeed: %v\n%s", err, output)
	}

	if _, err := os.Lstat(factoryMachineSkill); err == nil {
		t.Fatal("expected factory machine-scope skill to be pruned, but it still exists")
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected error checking factory machine-scope skill: %v", err)
	}
	if _, err := os.Lstat(windsurfMachineSkill); err == nil {
		t.Fatal("expected windsurf machine-scope skill to be pruned, but it still exists")
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected error checking windsurf machine-scope skill: %v", err)
	}
}

func skillsForRegistryPath(t *testing.T, cfg *config.Config, path string) []string {
	t.Helper()
	for _, entry := range cfg.Registries {
		if entry.Path == path {
			return entry.Skills.Include
		}
	}
	t.Fatalf("expected registry with path %q", path)
	return nil
}
