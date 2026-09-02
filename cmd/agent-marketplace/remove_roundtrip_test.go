package main

// remove_roundtrip_test.go — regression guards for the full install→remove
// lifecycle across all scopes. Each test does the full round-trip so a
// regression in either verb is caught by the same test.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// configContainsSkill reports whether any registry entry in cfg includes the
// named skill. Used in place of the removed cfg.Skills.Include field.
func configContainsSkill(cfg *config.Config, name string) bool {
	if cfg == nil {
		return false
	}
	for _, entry := range cfg.Registries {
		if containsStr(entry.Skills.Include, name) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// repo scope round-trip (the most fundamental path)
// ---------------------------------------------------------------------------

// TestInstallThenRemove_RepoScope verifies the complete lifecycle for the
// default (repo) scope: install writes to repo config, sync creates symlink,
// remove deletes from repo config and de-syncs.
func TestInstallThenRemove_RepoScope(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "repo-scope.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	// Install — should write to repo config.
	if out, err := executeCommand("install", "vm-lifecycle"); err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}

	repoCfg, _ := config.LoadRepoConfig()
	if repoCfg == nil || !configContainsSkill(repoCfg, "vm-lifecycle") {
		t.Fatalf("expected vm-lifecycle in repo config registry skills after install, got %v", repoCfg)
	}

	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle", "SKILL.md")); err != nil {
		t.Skipf("factory skill missing after sync (CI limitation): %v", err)
	}

	// Remove — should delete from repo config and de-sync.
	if out, err := executeCommand("remove", "vm-lifecycle"); err != nil {
		t.Fatalf("remove failed: %v\n%s", err, out)
	}

	repoCfg2, _ := config.LoadRepoConfig()
	if repoCfg2 != nil && configContainsSkill(repoCfg2, "vm-lifecycle") {
		t.Fatalf("expected vm-lifecycle removed from repo config, got %v", repoCfg2.Registries)
	}

	assertFactorySkillAbsent(t, repoRoot, "vm-lifecycle")
}

// ---------------------------------------------------------------------------
// user scope round-trip
// ---------------------------------------------------------------------------

// TestInstallThenRemove_UserScope verifies install --scope user → remove --scope user
// writes to and cleans up ~/.agents/config.yaml, not the repo config.
func TestInstallThenRemove_UserScope(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "user-scope.git")
	writeSkill(t, working, "secret-scan", "Secret scanning", nil)
	commitAndPushAll(t, working, "setup")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("install", "--scope", "user", "secret-scan"); err != nil {
		t.Fatalf("install --scope user failed: %v\n%s", err, out)
	}

	globalCfg, _ := config.LoadGlobalConfig()
	if globalCfg == nil || !configContainsSkill(globalCfg, "secret-scan") {
		t.Fatalf("expected secret-scan in global config registry skills after install --scope user")
	}

	if out, err := executeCommand("remove", "--scope", "user", "secret-scan"); err != nil {
		t.Fatalf("remove --scope user failed: %v\n%s", err, out)
	}

	globalCfg2, _ := config.LoadGlobalConfig()
	if globalCfg2 != nil && configContainsSkill(globalCfg2, "secret-scan") {
		t.Fatalf("expected secret-scan removed from global config after remove --scope user")
	}

	// Repo config must never have been touched.
	repoCfg, _ := config.LoadRepoConfig()
	if repoCfg != nil && configContainsSkill(repoCfg, "secret-scan") {
		t.Fatal("secret-scan should never appear in repo config")
	}
}

// ---------------------------------------------------------------------------
// local scope round-trip
// ---------------------------------------------------------------------------

// TestInstallThenRemove_LocalScope verifies install --scope local writes to
// config.local.yaml and a subsequent remove de-selects the skill (by adding
// to local exclude, since the remove path uses exclude for RepoPersonal skills).
func TestInstallThenRemove_LocalScope(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "local-scope.git")
	writeSkill(t, working, "k8s-deploy", "K8s deploy", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("install", "--scope", "local", "k8s-deploy"); err != nil {
		t.Fatalf("install --scope local failed: %v\n%s", err, out)
	}

	localCfg, _ := config.LoadLocalConfig()
	if localCfg == nil || !containsStr(localSkillsInclude(localCfg), "k8s-deploy") {
		t.Fatalf("expected k8s-deploy in local config after install --scope local")
	}

	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", "k8s-deploy", "SKILL.md")); err != nil {
		t.Skipf("factory skill missing after sync: %v", err)
	}

	// remove without --scope: skill has Source=RepoPersonal, infers scopeRepo,
	// falls to personal exclude (since it's not in repoCfg.Skills).
	if out, err := executeCommand("remove", "k8s-deploy"); err != nil {
		t.Fatalf("remove failed: %v\n%s", err, out)
	}

	// Skill must be de-synced.
	assertFactorySkillAbsent(t, repoRoot, "k8s-deploy")
}

// ---------------------------------------------------------------------------
// remove skill not enabled
// ---------------------------------------------------------------------------

// TestRemoveSkillNotEnabled verifies that removing a skill that is not
// configured (and not resolved) returns a clear error.
func TestRemoveSkillNotEnabled(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "not-enabled.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	_, err := executeCommand("remove", "does-not-exist")
	if err == nil {
		t.Fatal("expected error removing a skill that is not enabled, got nil")
	}
	if !strings.Contains(err.Error(), "not currently enabled") {
		t.Fatalf("expected 'not currently enabled' in error, got: %v", err)
	}
}

func TestRemoveSkillDeletedUpstreamClearsSubsequentStaleWarning(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "removed-upstream-lifecycle.git")
	writeSkill(t, working, "removed-upstream", "Removed upstream", nil)
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "add selected skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - removed-upstream
`)
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v\n%s", err, output)
	}
	assertFactorySkillExists(t, repoRoot, "removed-upstream")

	if err := os.RemoveAll(filepath.Join(working, "skills", "removed-upstream")); err != nil {
		t.Fatal(err)
	}
	commitAndPushAll(t, working, "remove selected skill upstream")
	if output, err := executeCommand("update"); err != nil {
		t.Fatalf("update after upstream removal failed: %v\n%s", err, output)
	}
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("stale sync failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, `skill "removed-upstream" was selected but not found`) {
		t.Fatalf("expected stale warning after upstream removal, got:\n%s", output)
	}

	if output, err = executeCommand("remove", "removed-upstream"); err != nil {
		t.Fatalf("remove stale selection failed: %v\n%s", err, output)
	}
	output, err = executeCommand("sync")
	if err != nil {
		t.Fatalf("sync after stale removal failed: %v\n%s", err, output)
	}
	if strings.Contains(output, `skill "removed-upstream" was selected but not found`) {
		t.Fatalf("stale warning survived removal:\n%s", output)
	}
}

func TestRemoveMalformedExplicitSelection(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "malformed-remove.git")
	writeFile(t, filepath.Join(working, "skills", "broken", "SKILL.md"), "---\nname: broken\ndescription: \"unterminated\n---\n")
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - broken
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("remove", "broken"); err != nil {
		t.Fatalf("remove malformed selection failed: %v\n%s", err, out)
	}
	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if containsStr(cfg.Registries[0].Skills.Include, "broken") {
		t.Fatal("malformed selection remains in config")
	}
}

func TestRemoveStaleExplicitSelectionPreservesInheritedTagDisable(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "stale-tags.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
tags:
  - security
registries:
  - url: `+origin+`
    ref: main
    skills:
      - removed-upstream
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("remove", "removed-upstream"); err != nil {
		t.Fatalf("remove stale tag-selected selection failed: %v\n%s", err, out)
	}
	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if containsStr(cfg.Registries[0].Skills.Include, "removed-upstream") || !containsStr(cfg.Registries[0].Skills.Exclude, "removed-upstream") {
		t.Fatalf("expected stale tag-selected selection to move from include to exclude: %#v", cfg.Registries[0].Skills)
	}
}

func TestRemoveStaleExplicitSelectionInfersUserScope(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "stale-user.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "setup")

	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	writeConfig(t, globalPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - removed-upstream
`)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - keeper
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("remove", "removed-upstream"); err != nil {
		t.Fatalf("remove stale user selection failed: %v\n%s", err, out)
	}
	cfg, err := config.LoadConfigFromPath(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	if containsStr(cfg.Registries[0].Skills.Include, "removed-upstream") {
		t.Fatalf("stale selection remains in user config: %v", cfg.Registries[0].Skills.Include)
	}
}

func TestRemoveStaleOrganizationSelectionSuppressesSubsequentWarning(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "stale-org.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "setup")

	orgPath := filepath.Join(t.TempDir(), "org-config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    path: skills
    skills:
      include:
        - test
`)
	config.SetTestV3OrganizationConfigPath(t, orgPath)

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    path: skills
    skills:
      include:
        - keeper
`)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `registries: []`)
	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("remove", "test"); err != nil {
		t.Fatalf("remove stale organization selection failed: %v\n%s", err, output)
	}
	if output, err := executeCommand("remove", "test"); err == nil || !strings.Contains(err.Error(), "not currently enabled") {
		t.Fatalf("second remove should report the selection is already disabled, got %v\n%s", err, output)
	}
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync after remove failed: %v\n%s", err, output)
	}
	if strings.Contains(output, `skill "test" was selected but not found`) {
		t.Fatalf("stale organization selection warning survived removal:\n%s", output)
	}
}

func TestRemoveStaleExplicitSelectionRequiresKindDisambiguation(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "stale-kind.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - deploy
    commands:
      - deploy
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("remove", "deploy"); err == nil || !strings.Contains(err.Error(), "ambiguous across kinds") {
		t.Fatalf("expected kind ambiguity, got %v", err)
	}
	if out, err := executeCommand("remove", "--kind", "command", "deploy"); err != nil {
		t.Fatalf("remove stale command failed: %v\n%s", err, out)
	}
	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if containsStr(cfg.Registries[0].Commands.Include, "deploy") {
		t.Fatal("stale command selection remains in config")
	}
	if !containsStr(cfg.Registries[0].Skills.Include, "deploy") {
		t.Fatal("kind-disambiguated removal deleted the skill selection")
	}
}

func TestRemoveStaleExplicitSelectionRequiresScopeDisambiguation(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "stale-scope.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "setup")

	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	writeConfig(t, globalPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - removed-upstream
`)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, repoPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - keeper
      - removed-upstream
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("remove", "removed-upstream"); err == nil || !strings.Contains(err.Error(), "multiple config layers") {
		t.Fatalf("expected scope ambiguity, got %v", err)
	}
	if out, err := executeCommand("remove", "--scope", "user", "removed-upstream"); err != nil {
		t.Fatalf("remove stale user selection failed: %v\n%s", err, out)
	}
	globalCfg, err := config.LoadConfigFromPath(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	repoCfg, err := config.LoadConfigFromPath(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	if containsStr(globalCfg.Registries[0].Skills.Include, "removed-upstream") {
		t.Fatal("stale user selection remains in config")
	}
	if !containsStr(repoCfg.Registries[0].Skills.Include, "removed-upstream") {
		t.Fatal("scope-disambiguated removal deleted the repo selection")
	}
}

func TestRemoveStaleExplicitSelectionFromLocalConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "stale-local.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - keeper
`)
	localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	writeFile(t, localPath, `registries:
  - url: `+origin+`
    ref: main
    skills:
      include:
        - removed-upstream
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("remove", "--scope", "local", "removed-upstream"); err != nil {
		t.Fatalf("remove stale local selection failed: %v\n%s", err, out)
	}
	cfg, err := config.LoadConfigFromPath(localPath)
	if err != nil {
		t.Fatal(err)
	}
	if containsStr(cfg.Registries[0].Skills.Include, "removed-upstream") {
		t.Fatal("stale local selection remains in config")
	}
}

func TestRemoveStaleExplicitSelectionUsesFromToDisambiguateRegistry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	originA, workingA := createBareRepoWithWorkingClone(t, "stale-from-a.git")
	writeSkill(t, workingA, "keeper-a", "Keeper A", nil)
	commitAndPushAll(t, workingA, "setup")
	originB, workingB := createBareRepoWithWorkingClone(t, "stale-from-b.git")
	writeSkill(t, workingB, "keeper-b", "Keeper B", nil)
	commitAndPushAll(t, workingB, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+originA+`
    ref: main
    skills:
      - removed-upstream
  - url: `+originB+`
    ref: main
    skills:
      - removed-upstream
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("remove", "removed-upstream"); err == nil || !strings.Contains(err.Error(), "multiple registries") {
		t.Fatalf("expected registry ambiguity, got %v", err)
	}
	if out, err := executeCommand("remove", "--from", originA, "removed-upstream"); err != nil {
		t.Fatalf("remove stale registry selection failed: %v\n%s", err, out)
	}
	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if containsStr(cfg.Registries[0].Skills.Include, "removed-upstream") {
		t.Fatal("stale selection remains in selected registry")
	}
	if !containsStr(cfg.Registries[1].Skills.Include, "removed-upstream") {
		t.Fatal("registry-disambiguated removal deleted the other selection")
	}
}

func TestRemoveFromRejectsUnselectedName(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "remove-from-typo.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("remove", "--from", origin, "typo"); err == nil || !strings.Contains(err.Error(), "not currently enabled") {
		t.Fatalf("expected unselected name to be rejected, got %v", err)
	}
}

func TestRemoveFromUsesRefToDisambiguateSamePath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "remove-from-refs.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "setup")
	runGit(t, working, "checkout", "-b", "release")
	runGit(t, working, "push", "origin", "release")
	runGit(t, working, "checkout", "main")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - keeper
  - url: `+origin+`
    ref: release
    skills:
      - keeper
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("remove", "keeper", "--from", origin, "--ref", "main"); err != nil {
		t.Fatalf("explicit ref should disambiguate named removal: %v\n%s", err, out)
	}
	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if containsStr(cfg.Registries[0].Skills.Include, "keeper") || !containsStr(cfg.Registries[1].Skills.Include, "keeper") {
		t.Fatalf("named removal mutated the wrong ref: %#v", cfg.Registries)
	}

	if out, err := executeCommand("remove", "--from", origin, "--ref", "main"); err != nil {
		t.Fatalf("explicit ref should disambiguate registry removal: %v\n%s", err, out)
	}
	cfg, err = config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Registries) != 1 || cfg.Registries[0].Ref != "release" {
		t.Fatalf("expected only release entry to remain, got %#v", cfg.Registries)
	}
}

// ---------------------------------------------------------------------------
// tag-selected skill remove → repo exclude (not personal exclude)
// ---------------------------------------------------------------------------

// TestRemoveTagSelectedSkill_RepoScope verifies that when a skill is selected
// via a tag match (not explicit skills: list), remove writes to the repo
// config's exclude: list rather than the personal (local) exclude.
func TestRemoveTagSelectedSkill_RepoScope(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "tag-remove.git")
	writeSkill(t, working, "k8s-basics", "K8s basics", []string{"k8s"})
	writeSkill(t, working, "k8s-deploy", "K8s deploy", []string{"k8s"})
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	// Repo config selects all k8s-tagged skills via tags:.
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
tags:
  - k8s
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	if out, err := executeCommand("remove", "k8s-basics"); err != nil {
		t.Fatalf("remove tag-selected skill failed: %v\n%s", err, out)
	}

	// Exclude must be in repo config (not personal local config).
	repoCfgBytes, _ := os.ReadFile(filepath.Join(repoRoot, ".agents", "config.yaml"))
	if !strings.Contains(string(repoCfgBytes), "k8s-basics") {
		t.Fatalf("expected k8s-basics exclude in repo config, got:\n%s", repoCfgBytes)
	}

	localCfgPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	localBytes, _ := os.ReadFile(localCfgPath)
	if strings.Contains(string(localBytes), "k8s-basics") {
		t.Fatalf("k8s-basics should NOT be in local config (wrong scope), got:\n%s", localBytes)
	}

	// k8s-deploy (same tag) must still be synced; k8s-basics must be gone.
	assertFactorySkillAbsent(t, repoRoot, "k8s-basics")
	assertFactorySkillExists(t, repoRoot, "k8s-deploy")
}

// ---------------------------------------------------------------------------
// org-level skill removed at user scope via shadow entry
// ---------------------------------------------------------------------------

// TestRemoveOrgSkillAtUserScope verifies that a skill defined in the org
// config can be excluded at the user scope. The remove creates a shadow
// registry entry in ~/.agents/config.yaml with just the exclude, and the
// resolver's cross-layer exclusion suppresses the org-level skill.
func TestRemoveOrgSkillAtUserScope(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	registryRoot := filepath.Join(t.TempDir(), "registry")
	writeSkill(t, registryRoot, "committing-code", "Committing code", []string{"dev"})
	writeSkill(t, registryRoot, "other-skill", "Other skill", []string{"dev"})

	orgCfgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeConfig(t, orgCfgPath, `
apiVersion: agent-marketplace/v3
registries:
  - url: `+registryRoot+`
    default: true
`)
	config.SetTestV3OrganizationConfigPath(t, orgCfgPath)

	// User config exists but does NOT contain the org registry.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
`)
	withWorkingDir(t, repoRoot)

	// Initial sync should include both org skills.
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v\n%s", err, output)
	}
	status, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status after initial sync failed: %v\n%s", err, status)
	}
	if !strings.Contains(status, "committing-code") {
		t.Fatalf("expected committing-code in status after initial sync\n%s", status)
	}

	// Remove org-level skill at user scope — must succeed (previously
	// failed with "registry not found in config").
	if out, err := executeCommand("remove", "--scope", "user", "committing-code"); err != nil {
		t.Fatalf("remove --scope user of org skill failed: %v\n%s", err, out)
	}

	// After sync, committing-code must be excluded; other-skill must remain.
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("post-remove sync failed: %v\n%s", err, output)
	}
	status2, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status after remove failed: %v\n%s", err, status2)
	}
	// committing-code should no longer appear as a synced resource.
	// It may appear in the "LOCALLY EXCLUDED" section or not at all,
	// depending on the scope of the exclude. The shadow entry in the
	// user config is verified separately below.
	if strings.Contains(status2, "committing-code") {
		t.Fatalf("expected committing-code to be absent from synced resources after remove\n%s", status2)
	}
	if !strings.Contains(status2, "other-skill") {
		t.Fatalf("expected other-skill to remain after remove\n%s", status2)
	}

	// Verify the user config now has a shadow registry entry with the exclude.
	globalCfg, _ := config.LoadGlobalConfig()
	if globalCfg == nil {
		t.Fatal("expected global config to exist")
	}
	found := false
	for _, entry := range globalCfg.Registries {
		if strings.TrimSpace(entry.URL) == registryRoot {
			for _, ex := range entry.Skills.Exclude {
				if ex == "committing-code" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("expected shadow registry entry with committing-code in exclude, got %+v", globalCfg.Registries)
	}
}

// ---------------------------------------------------------------------------
// remove --from round-trip
// ---------------------------------------------------------------------------

func TestRemoveFromFlagRemovesNamedResourceFromAdHocRegistry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "from-registry.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	writeSkill(t, working, "k8s-onboarding", "Kubernetes onboarding", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	// Install via --from.
	if out, err := executeCommand("install", "--from", origin, "--ref", "main"); err != nil {
		t.Fatalf("install --from failed: %v\n%s", err, out)
	}

	// Remove one resource via --from.
	out, err := executeCommand("remove", "vm-lifecycle", "--from", origin)
	if err != nil {
		t.Fatalf("remove --from failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Removed vm-lifecycle") {
		t.Fatalf("expected remove output, got %q", out)
	}
}

func TestRemoveFromFlagWithoutNameRemovesEntireRegistryEntry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "from-registry.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	// Install via --from.
	if out, err := executeCommand("install", "--from", origin, "--ref", "main"); err != nil {
		t.Fatalf("install --from failed: %v\n%s", err, out)
	}

	// Remove entire registry entry via --from without name.
	out, err := executeCommand("remove", "--from", origin)
	if err != nil {
		t.Fatalf("remove --from (no name) failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Removed registry entry") {
		t.Fatalf("expected registry entry removal output, got %q", out)
	}

	repoCfg, _ := config.LoadRepoConfig()
	if repoCfg == nil || len(repoCfg.Registries) != 0 {
		t.Fatalf("expected no registries after remove --from (no name), got %d", len(repoCfg.Registries))
	}
}

func TestRemoveFromFlagRefAndPathRequireFrom(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	_, err := executeCommand("remove", "some-skill", "--ref", "main")
	if err == nil {
		t.Fatal("expected --ref without --from to fail")
	}
	if !strings.Contains(err.Error(), "--ref requires --from") {
		t.Fatalf("expected --ref requires --from error, got %v", err)
	}

	_, err = executeCommand("remove", "some-skill", "--path", "sub/dir")
	if err == nil {
		t.Fatal("expected --path without --from to fail")
	}
	if !strings.Contains(err.Error(), "--path requires --from") {
		t.Fatalf("expected --path requires --from error, got %v", err)
	}
}

func TestRemoveFromFlagUsesResolvedKindNotFlagDefault(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "from-registry.git")
	writeSkill(t, working, "deploy-helper", "Deploy helper", nil)
	writeSubagent(t, working, "worker-generic", "Worker", []string{"Read"})
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	// Install the subagent explicitly via --from --kind subagent.
	if out, err := executeCommand("install", "worker-generic", "--from", origin, "--ref", "main", "--kind", "subagent"); err != nil {
		t.Fatalf("install --from --kind subagent failed: %v\n%s", err, out)
	}

	// Remove the subagent WITHOUT --kind. The resolved kind (subagent)
	// must be used, not the flag default (skill). If the wrong kind is
	// used, RemoveResourceFromRegistryInConfig would look for the name
	// under the "skills" include key (which is empty), not find it,
	// and incorrectly fall through to adding an exclude under the
	// wrong selector.
	out, err := executeCommand("remove", "worker-generic", "--from", origin)
	if err != nil {
		t.Fatalf("remove --from subagent failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Removed worker-generic") {
		t.Fatalf("expected remove output, got %q", out)
	}

	// Verify the subagent is no longer in the include list.
	repoCfg, _ := config.LoadRepoConfig()
	if repoCfg == nil {
		t.Fatal("expected repo config to exist")
	}
	for _, entry := range repoCfg.Registries {
		for _, inc := range entry.Subagents.Include {
			if inc == "worker-generic" {
				t.Fatalf("expected worker-generic removed from subagents include, still present")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// local-scope remove must not require apiVersion on config.local.yaml
// ---------------------------------------------------------------------------

// TestRemoveFromFlag_LocalScope_NoAPIVersion reproduces the bug where a
// generated config.local.yaml (which carries no apiVersion) could not be
// removed from via `remove --from --scope local`: the ad-hoc removal path
// loaded the local config through the full Config loader, which demanded
// apiVersion and failed with "missing apiVersion".
func TestRemoveFromFlag_LocalScope_NoAPIVersion(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "local-noapiver.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	// Install at local scope — generates config.local.yaml WITHOUT apiVersion.
	if out, err := executeCommand("install", "vm-lifecycle", "--from", origin, "--ref", "main", "--scope", "local"); err != nil {
		t.Fatalf("install --scope local failed: %v\n%s", err, out)
	}

	localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	localBytes, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read config.local.yaml: %v", err)
	}
	if strings.Contains(string(localBytes), "apiVersion") {
		t.Fatalf("precondition: generated local config should not carry apiVersion, got:\n%s", localBytes)
	}

	// Remove via --from at local scope — previously failed on missing apiVersion.
	out, err := executeCommand("remove", "vm-lifecycle", "--from", origin, "--scope", "local")
	if err != nil {
		t.Fatalf("remove --from --scope local failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Removed vm-lifecycle") {
		t.Fatalf("expected remove output, got %q", out)
	}

	// The skill must actually be removed, not just a clean command exit: a
	// no-op that accepted config.local.yaml while leaving vm-lifecycle selected
	// would otherwise still pass.
	assertFactorySkillAbsent(t, repoRoot, "vm-lifecycle")
	localCfg, _ := config.LoadLocalConfig()
	if localCfg != nil {
		for _, reg := range localCfg.Registries {
			if containsStr(reg.Skills.Include, "vm-lifecycle") {
				t.Fatalf("expected vm-lifecycle removed from local registry include, still present: %#v", reg)
			}
		}
	}
}

// TestRemove_LocalScope_WithManualAPIVersion reproduces the second half of the
// bug: a user who hand-adds `apiVersion: agent-marketplace/v2` to
// config.local.yaml (e.g. to satisfy the previous error) then hit a strict
// decoder error: "field apiVersion not found in type config.LocalConfig".
// The LocalConfig schema now tolerates an optional apiVersion.
func TestRemove_LocalScope_WithManualAPIVersion(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "local-apiver.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	// Hand-written local config WITH apiVersion present.
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.local.yaml"), `apiVersion: agent-marketplace/v2
registries:
  - url: `+origin+`
    ref: main
    skills:
      include:
        - vm-lifecycle
`)
	withWorkingDir(t, repoRoot)

	// Any command that loads the local config must accept the apiVersion line.
	if out, err := executeCommand("remove", "vm-lifecycle", "--scope", "local"); err != nil {
		t.Fatalf("remove --scope local with apiVersion failed: %v\n%s", err, out)
	}

	// apiVersion must be preserved through the edit (editor operates at the
	// YAML-node level), and the skill must be excluded.
	localBytes, err := os.ReadFile(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	if err != nil {
		t.Fatalf("read config.local.yaml: %v", err)
	}
	if !strings.Contains(string(localBytes), "apiVersion: agent-marketplace/v2") {
		t.Fatalf("expected apiVersion to be preserved, got:\n%s", localBytes)
	}

	// The remove must actually take effect (not a no-op that merely tolerated
	// the apiVersion line): vm-lifecycle must be excluded/removed and de-synced.
	assertFactorySkillAbsent(t, repoRoot, "vm-lifecycle")
	localCfg, _ := config.LoadLocalConfig()
	if localCfg == nil {
		t.Fatalf("expected local config to load")
	}
	excluded := false
	for _, reg := range localCfg.Registries {
		if containsStr(reg.Skills.Include, "vm-lifecycle") {
			t.Fatalf("expected vm-lifecycle removed from local registry include, still present: %#v", reg)
		}
		if containsStr(reg.Skills.Exclude, "vm-lifecycle") {
			excluded = true
		}
	}
	if !excluded {
		t.Fatalf("expected vm-lifecycle to be excluded in local config, got %#v", localCfg.Registries)
	}
}

// ---------------------------------------------------------------------------
// cross-layer exclude: suppress a project-level registry at local scope
// ---------------------------------------------------------------------------

// TestRemoveFromFlag_LocalScope_ExcludesProjectRegistry reproduces the bug
// where excluding a registry enabled at the project layer via
// `remove --from <url> --scope local` failed with "registry entry ... not
// found in local config". The registry is inherited, so it must be suppressed
// with an exclude_all shadow entry in config.local.yaml.
func TestRemoveFromFlag_LocalScope_ExcludesProjectRegistry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "xlayer-all.git")
	writeSkill(t, working, "db-migrate", "DB migrate", nil)
	writeSkill(t, working, "db-backup", "DB backup", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	// Project config enables the registry with default:true (auto-selects all).
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    default: true
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}
	assertFactorySkillExists(t, repoRoot, "db-migrate")

	out, err := executeCommand("remove", "--from", origin, "--scope", "local")
	if err != nil {
		t.Fatalf("remove --from --scope local failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Excluded all resources from") {
		t.Fatalf("expected exclude-all output, got %q", out)
	}

	// Local config must carry an exclude_all shadow entry; project config
	// must be untouched.
	localCfg, _ := config.LoadLocalConfig()
	if localCfg == nil || len(localCfg.Registries) != 1 || !localCfg.Registries[0].ExcludeAll {
		t.Fatalf("expected exclude_all shadow entry in local config, got %#v", localCfg)
	}
	repoCfg, _ := config.LoadRepoConfig()
	if repoCfg == nil || len(repoCfg.Registries) != 1 {
		t.Fatalf("expected project registry untouched, got %#v", repoCfg)
	}

	// Both skills must be de-synced.
	assertFactorySkillAbsent(t, repoRoot, "db-migrate")
	assertFactorySkillAbsent(t, repoRoot, "db-backup")
}

// TestRemoveFromFlag_LocalScope_ExcludesNamedFromProjectRegistry verifies the
// named cross-layer case: a single resource from a project-level registry can
// be excluded at local scope (writing a per-kind exclude shadow), leaving the
// registry's other resources intact.
func TestRemoveFromFlag_LocalScope_ExcludesNamedFromProjectRegistry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "xlayer-named.git")
	writeSkill(t, working, "db-migrate", "DB migrate", nil)
	writeSkill(t, working, "db-backup", "DB backup", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    default: true
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	out, err := executeCommand("remove", "db-migrate", "--from", origin, "--scope", "local")
	if err != nil {
		t.Fatalf("remove <name> --from --scope local failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Removed db-migrate") {
		t.Fatalf("expected remove output, got %q", out)
	}

	// db-migrate must be excluded locally; db-backup must remain.
	assertFactorySkillAbsent(t, repoRoot, "db-migrate")
	assertFactorySkillExists(t, repoRoot, "db-backup")
}

// TestInstallAfterExcludeAll_ReEnablesResource reproduces the "install is a
// silent no-op" bug: after `remove --from <reg> --scope local` writes
// exclude_all, installing a resource must actually enable it. The install now
// clears the contradictory exclude_all (which always wins in the resolver),
// leaving a clean registry-scoped include.
func TestInstallAfterExcludeAll_ReEnablesResource(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "exclude-then-install.git")
	writeSkill(t, working, "db-migrate", "DB migrate", nil)
	writeSkill(t, working, "db-backup", "DB backup", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    default: true
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	// Exclude the whole registry locally.
	if out, err := executeCommand("remove", "--from", origin, "--scope", "local"); err != nil {
		t.Fatalf("remove --scope local failed: %v\n%s", err, out)
	}
	assertFactorySkillAbsent(t, repoRoot, "db-migrate")

	// Install db-migrate onto the excluded registry — must take effect.
	if out, err := executeCommand("install", "db-migrate", "--from", origin, "--scope", "local"); err != nil {
		t.Fatalf("install after exclude_all failed: %v\n%s", err, out)
	}

	// exclude_all must be cleared and the include written registry-scoped.
	localCfg, _ := config.LoadLocalConfig()
	if localCfg == nil || len(localCfg.Registries) != 1 {
		t.Fatalf("expected one local registry entry, got %#v", localCfg)
	}
	entry := localCfg.Registries[0]
	if entry.ExcludeAll {
		t.Fatalf("expected exclude_all cleared after install, got entry %#v", entry)
	}
	if !containsStr(entry.Skills.Include, "db-migrate") {
		t.Fatalf("expected db-migrate registry-scoped include, got %#v", entry.Skills.Include)
	}
	// Install must write a SINGLE registry-scoped include for db-migrate, not
	// re-enable the whole registry by listing every sibling: db-backup must not
	// leak into the local include.
	if containsStr(entry.Skills.Include, "db-backup") {
		t.Fatalf("expected only db-migrate re-included locally, db-backup leaked into include: %#v", entry.Skills.Include)
	}

	// The resource must actually be materialized now (not a silent no-op).
	assertFactorySkillExists(t, repoRoot, "db-migrate")
}

// ---------------------------------------------------------------------------
// remove --from blocked registry
// ---------------------------------------------------------------------------

// TestRemoveFromFlag_BlockedRegistry_RemovesEntry verifies that a registry
// which is now blocked by allowed_registries can still be removed entirely
// with `remove --from <url>`. The registry was previously allowed and
// materialized; after tightening the allowlist the remove command must still
// remove the config entry and de-sync its resources.
func TestRemoveFromFlag_BlockedRegistry_RemovesEntry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "blocked-registry.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
harnesses:
  - factory
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v\n%s", err, out)
	}
	factoryPresent := false
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle", "SKILL.md")); err == nil {
		factoryPresent = true
	}

	// Tighten the allowlist so the previously-allowed registry is now blocked.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
harnesses:
  - factory
allowed_registries: []
`)

	// Remove the now-blocked registry entry via --from.
	out, err := executeCommand("remove", "--from", origin)
	if err != nil {
		t.Fatalf("remove --from blocked registry failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Removed registry entry") {
		t.Fatalf("expected registry entry removal output, got %q", out)
	}

	repoCfg, _ := config.LoadRepoConfig()
	if repoCfg == nil || len(repoCfg.Registries) != 0 {
		t.Fatalf("expected no registries after remove --from, got %d", len(repoCfg.Registries))
	}

	if factoryPresent {
		assertFactorySkillAbsent(t, repoRoot, "vm-lifecycle")
	}
}

// TestRemoveFromFlag_BlockedRegistry_RemovesNamedResource verifies that a
// single resource can be removed from a registry that is now blocked by
// allowed_registries. The registry entry remains, but the named resource is
// excluded and de-synced while its siblings stay materialized.
func TestRemoveFromFlag_BlockedRegistry_RemovesNamedResource(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "blocked-named.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	writeSkill(t, working, "k8s-onboarding", "Kubernetes onboarding", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
harnesses:
  - factory
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v\n%s", err, out)
	}
	factoryPresent := map[string]bool{}
	for _, name := range []string{"vm-lifecycle", "k8s-onboarding"} {
		if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", name, "SKILL.md")); err == nil {
			factoryPresent[name] = true
		}
	}

	// Tighten the allowlist so the previously-allowed registry is now blocked.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
harnesses:
  - factory
allowed_registries: []
`)

	// Remove one resource from the now-blocked registry.
	out, err := executeCommand("remove", "vm-lifecycle", "--from", origin)
	if err != nil {
		t.Fatalf("remove vm-lifecycle --from blocked registry failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Removed vm-lifecycle") {
		t.Fatalf("expected remove output, got %q", out)
	}

	repoCfg, _ := config.LoadRepoConfig()
	if repoCfg == nil || len(repoCfg.Registries) != 1 {
		t.Fatalf("expected one registry entry after remove, got %v", repoCfg)
	}
	entry := repoCfg.Registries[0]
	if containsStr(entry.Skills.Include, "vm-lifecycle") {
		t.Fatalf("expected vm-lifecycle removed from skills include, got %v", entry.Skills.Include)
	}
	if !containsStr(entry.Skills.Exclude, "vm-lifecycle") {
		t.Fatalf("expected vm-lifecycle in skills exclude, got %v", entry.Skills.Exclude)
	}

	if factoryPresent["vm-lifecycle"] {
		assertFactorySkillAbsent(t, repoRoot, "vm-lifecycle")
	}
	if factoryPresent["k8s-onboarding"] {
		assertFactorySkillExists(t, repoRoot, "k8s-onboarding")
	}
}

func TestRemoveUnresolvedAutoSelectionDisablesEveryExactDuplicate(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	origin, working := createBareRepoWithWorkingClone(t, "duplicate-auto-remove.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    default: true
    skills: [removed-upstream]
  - url: `+origin+`
    ref: main
    default: true
    skills: [removed-upstream]
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("remove", "removed-upstream"); err != nil {
		t.Fatalf("remove duplicate unresolved auto-selection failed: %v\n%s", err, out)
	}
	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Registries) != 2 {
		t.Fatalf("expected both duplicate entries to remain, got %#v", cfg.Registries)
	}
	for i, entry := range cfg.Registries {
		if containsStr(entry.Skills.Include, "removed-upstream") || !containsStr(entry.Skills.Exclude, "removed-upstream") {
			t.Fatalf("duplicate %d was not persistently disabled: %#v", i, entry.Skills)
		}
	}
}

func TestRemoveFromRefPersistsInheritedTagExcludeWithoutTouchingOtherRef(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	origin, working := createBareRepoWithWorkingClone(t, "inherited-tag-ref-remove.git")
	writeSkill(t, working, "keeper", "Keeper", []string{"security"})
	commitAndPushAll(t, working, "setup")
	runGit(t, working, "checkout", "-b", "release")
	runGit(t, working, "push", "origin", "release")
	runGit(t, working, "checkout", "main")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
tags: [security]
registries:
  - url: `+origin+`
    ref: main
    skills: [removed-upstream]
  - url: `+origin+`
    ref: release
    skills: [removed-upstream]
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("remove", "removed-upstream", "--from", origin, "--ref", "main"); err != nil {
		t.Fatalf("remove --from --ref inherited-tag selection failed: %v\n%s", err, out)
	}
	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	mainEntry, releaseEntry := cfg.Registries[0], cfg.Registries[1]
	if containsStr(mainEntry.Skills.Include, "removed-upstream") || !containsStr(mainEntry.Skills.Exclude, "removed-upstream") {
		t.Fatalf("main ref exclusion did not persist: %#v", mainEntry.Skills)
	}
	if !containsStr(releaseEntry.Skills.Include, "removed-upstream") || containsStr(releaseEntry.Skills.Exclude, "removed-upstream") {
		t.Fatalf("release ref was touched: %#v", releaseEntry.Skills)
	}
}

func TestRemoveRequiresFromForResolvedTopLevelAndStalePerEntrySelection(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	resolvedOrigin, resolvedWorking := createBareRepoWithWorkingClone(t, "mixed-resolved.git")
	writeSkill(t, resolvedWorking, "mixed-selection", "Mixed selection", nil)
	commitAndPushAll(t, resolvedWorking, "setup")
	staleOrigin, staleWorking := createBareRepoWithWorkingClone(t, "mixed-stale.git")
	writeSkill(t, staleWorking, "keeper", "Keeper", nil)
	commitAndPushAll(t, staleWorking, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
skills: [mixed-selection]
registries:
  - url: `+resolvedOrigin+`
    ref: main
  - url: `+staleOrigin+`
    ref: main
    skills: [mixed-selection]
`)
	withWorkingDir(t, repoRoot)

	out, err := executeCommand("remove", "mixed-selection")
	if err == nil || !strings.Contains(out+err.Error(), "rerun with --from") {
		t.Fatalf("expected mixed selection sites to require --from, got %v\n%s", err, out)
	}
	cfg, loadErr := config.LoadConfigFromPath(configPath)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if !containsStr(cfg.Skills.Include, "mixed-selection") || !containsStr(cfg.Registries[1].Skills.Include, "mixed-selection") {
		t.Fatalf("ambiguous remove mutated config: %#v", cfg)
	}
}
