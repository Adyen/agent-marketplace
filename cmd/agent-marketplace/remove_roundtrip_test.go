package main

// remove_roundtrip_test.go — regression guards for the full install→remove
// lifecycle across all scopes. Each test does the full round-trip so a
// regression in either verb is caught by the same test.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "repo-scope.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
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
		t.Fatalf("factory skill missing after sync (CI limitation): %v", err)
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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "user-scope.git")
	writeSkill(t, working, "secret-scan", "Secret scanning", nil)
	commitAndPushAll(t, working, "setup")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
    ref: main
`)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "local-scope.git")
	writeSkill(t, working, "k8s-deploy", "K8s deploy", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
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
		t.Fatalf("factory skill missing after sync: %v", err)
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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "not-enabled.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("seed repo lock: %v\n%s", err, out)
	}

	_, err := executeCommand("remove", "does-not-exist")
	if err == nil {
		t.Fatal("expected error removing a skill that is not enabled, got nil")
	}
	if !strings.Contains(err.Error(), "not currently enabled") {
		t.Fatalf("expected 'not currently enabled' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// tag-selected skill remove → repo exclude (not personal exclude)
// ---------------------------------------------------------------------------

// TestRemoveTagSelectedSkill_RepoScope verifies that when a skill is selected
// via a tag match (not explicit skills: list), remove writes to the repo
// config's exclude: list rather than the personal (local) exclude.
func TestRemoveTagSelectedSkill_RepoScope(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "tag-remove.git")
	writeSkill(t, working, "k8s-basics", "K8s basics", []string{"k8s"})
	writeSkill(t, working, "k8s-deploy", "K8s deploy", []string{"k8s"})
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	// Repo config selects all k8s-tagged skills via tags:.
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
    ref: main
resources:
  registry:
    tags: [k8s]
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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	registryRoot := filepath.Join(t.TempDir(), "registry")
	writeSkill(t, registryRoot, "committing-code", "Committing code", []string{"dev"})
	writeSkill(t, registryRoot, "other-skill", "Other skill", []string{"dev"})

	orgCfgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeConfig(t, orgCfgPath, `
apiVersion: agent-marketplace/v4
sources:
  registry:
    directory: `+registryRoot+`
resources:
  registry:
    all: true
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	// User config exists but does NOT contain the org registry.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses:
  - factory
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "from-registry.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	writeSkill(t, working, "k8s-onboarding", "Kubernetes onboarding", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources: {}
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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "from-registry.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources: {}
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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources: {}
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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "from-registry.git")
	writeSkill(t, working, "deploy-helper", "Deploy helper", nil)
	writeSubagent(t, working, "worker-generic", "Worker", []string{"Read"})
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources: {}
`)
	withWorkingDir(t, repoRoot)

	// Install the subagent explicitly via --from --kind subagent.
	if out, err := executeCommand("install", "worker-generic", "--from", origin, "--ref", "main", "--kind", "subagent"); err != nil {
		t.Fatalf("install --from --kind subagent failed: %v\n%s", err, out)
	}

	// Remove the subagent WITHOUT --kind. The resolved kind (subagent)
	// must be used, not the flag default (skill).
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
// Local-scope remove accepts the canonical root generated by install.
// ---------------------------------------------------------------------------

// TestRemoveFromFlag_LocalScope_NoAPIVersion retains the historical local-root
// round trip: install creates a supported v4 root and remove must edit that
// same local layer and actually de-materialize the selected resource.
func TestRemoveFromFlag_LocalScope_NoAPIVersion(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "local-noapiver.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources: {}
`)
	withWorkingDir(t, repoRoot)

	// Install at local scope — generates config.local.yaml with exact v4.
	if out, err := executeCommand("install", "vm-lifecycle", "--from", origin, "--ref", "main", "--scope", "local"); err != nil {
		t.Fatalf("install --scope local failed: %v\n%s", err, out)
	}

	localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	localBytes, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read config.local.yaml: %v", err)
	}
	if !strings.Contains(string(localBytes), "apiVersion: agent-marketplace/v4") {
		t.Fatalf("precondition: generated local config should be canonical v4, got:\n%s", localBytes)
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

// TestRemove_LocalScope_WithManualAPIVersion verifies a handwritten v4 local
// root is accepted and its apiVersion survives the selector edit.
func TestRemove_LocalScope_WithManualAPIVersion(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "local-apiver.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources: {}
`)
	// Hand-written local config WITH apiVersion present.
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.local.yaml"), `apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
    ref: main
resources:
  registry:
    include: [skill/vm-lifecycle]
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed local lock and materialization: %v\n%s", err, out)
	}
	assertFileExists(t, filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle", "SKILL.md"))

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
	if !strings.Contains(string(localBytes), "apiVersion: agent-marketplace/v4") {
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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "xlayer-all.git")
	writeSkill(t, working, "db-migrate", "DB migrate", nil)
	writeSkill(t, working, "db-backup", "DB backup", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	// Project config explicitly selects all resources from the source.
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
    ref: main
resources:
  registry:
    all: true
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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "xlayer-named.git")
	writeSkill(t, working, "db-migrate", "DB migrate", nil)
	writeSkill(t, working, "db-backup", "DB backup", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
    ref: main
resources:
  registry:
    all: true
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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "exclude-then-install.git")
	writeSkill(t, working, "db-migrate", "DB migrate", nil)
	writeSkill(t, working, "db-backup", "DB backup", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
    ref: main
resources:
  registry:
    all: true
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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "blocked-registry.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
harnesses:
  - factory
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
    ref: main
resources:
  registry:
    all: true
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v\n%s", err, out)
	}
	assertFactorySkillExists(t, repoRoot, "vm-lifecycle")

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

	assertFactorySkillAbsent(t, repoRoot, "vm-lifecycle")
}

type blockedRemovalAcquisitionFixture struct {
	home, repo   string
	originA      string
	originB      string
	originC      string
	configPath   string
	lockPath     string
	aPath, bPath string
	cPath        string
}

func setupBlockedRemovalAcquisitionFixture(t *testing.T) blockedRemovalAcquisitionFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	originA, workingA := createV4GitRegistry(t, "blocked-a.git")
	writeSkill(t, workingA, "skill-a", "BODY-A", nil)
	commitAndPushAll(t, workingA, "seed A")
	originB, workingB := createV4GitRegistry(t, "blocked-b.git")
	writeSkill(t, workingB, "skill-b", "BODY-B", nil)
	commitAndPushAll(t, workingB, "seed B")
	originC, workingC := createV4GitRegistry(t, "blocked-c.git")
	writeSkill(t, workingC, "skill-c", "BODY-C", nil)
	commitAndPushAll(t, workingC, "seed C")
	repo := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nharnesses: [factory]\n")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nsources:\n  registry-a:\n    git: "+originA+"\n    ref: main\n  registry-b:\n    git: "+originB+"\n    ref: main\nresources:\n  registry-a:\n    all: true\n  registry-b:\n    all: true\n")
	withWorkingDir(t, repo)
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, out)
	}
	aPath := filepath.Join(repo, ".factory", "skills", "skill-a", "SKILL.md")
	bPath := filepath.Join(repo, ".factory", "skills", "skill-b", "SKILL.md")
	for path, body := range map[string]string{aPath: "BODY-A", bPath: "BODY-B"} {
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), body) {
			t.Fatalf("installed skill %s = %q err=%v", path, data, err)
		}
	}
	return blockedRemovalAcquisitionFixture{home: home, repo: repo, originA: originA, originB: originB, originC: originC, configPath: configPath, lockPath: filepath.Join(repo, ".agents", "lock.yaml"), aPath: aPath, bPath: bPath, cPath: filepath.Join(repo, ".factory", "skills", "skill-c", "SKILL.md")}
}

func addBlockedRemovalThirdRegistry(t *testing.T, fixture blockedRemovalAcquisitionFixture) {
	t.Helper()
	writeConfig(t, fixture.configPath, "apiVersion: agent-marketplace/v4\nsources:\n  registry-a:\n    git: "+fixture.originA+"\n    ref: main\n  registry-b:\n    git: "+fixture.originB+"\n    ref: main\n  registry-c:\n    git: "+fixture.originC+"\n    ref: main\nresources:\n  registry-a:\n    all: true\n  registry-b:\n    all: true\n  registry-c:\n    all: true\n")
}

func TestRemoveBlockedRegistryRefusesNewAcquisition(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := setupBlockedRemovalAcquisitionFixture(t)
	addBlockedRemovalThirdRegistry(t, fixture)
	writeConfig(t, filepath.Join(fixture.home, ".agents", "config.yaml"), "\nharnesses: [factory]\nallowed_registries: []\n")
	beforeConfig, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeLock, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeA, err := os.ReadFile(fixture.aPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeB, err := os.ReadFile(fixture.bPath)
	if err != nil {
		t.Fatal(err)
	}
	var acquisitions atomic.Int32
	originalHook := lifecycle.LockSnapshotAcquiredHook
	lifecycle.LockSnapshotAcquiredHook = func() { acquisitions.Add(1) }
	t.Cleanup(func() { lifecycle.LockSnapshotAcquiredHook = originalHook })
	out, err := executeCommand("remove", "--from", fixture.originA)
	var lifecycle *lifecycle.LockLifecycleError
	if !errors.As(err, &lifecycle) || lifecycle.Code != "lock.offline-resolution" {
		t.Fatalf("remove error = %v, want lock.offline-resolution\n%s", err, out)
	}
	if acquisitions.Load() != 0 {
		t.Fatalf("snapshot acquisitions = %d", acquisitions.Load())
	}
	afterConfig, configErr := os.ReadFile(fixture.configPath)
	afterLock, lockErr := os.ReadFile(fixture.lockPath)
	if configErr != nil || lockErr != nil || !bytes.Equal(beforeConfig, afterConfig) || !bytes.Equal(beforeLock, afterLock) {
		t.Fatalf("failed removal changed config/lock: configErr=%v lockErr=%v", configErr, lockErr)
	}
	afterA, aErr := os.ReadFile(fixture.aPath)
	afterB, bErr := os.ReadFile(fixture.bPath)
	if aErr != nil || bErr != nil || !bytes.Equal(beforeA, afterA) || !bytes.Equal(beforeB, afterB) {
		t.Fatalf("existing output bytes changed: aErr=%v bErr=%v", aErr, bErr)
	}
	if _, err := os.Stat(fixture.cPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new registry output exists: %v", err)
	}
}

func TestRemoveBlockedRegistryMissingExactSnapshot(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := setupBlockedRemovalAcquisitionFixture(t)
	writeConfig(t, filepath.Join(fixture.home, ".agents", "config.yaml"), "\nharnesses: [factory]\nallowed_registries: []\n")
	beforeConfig, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeLock, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeA, err := os.ReadFile(fixture.aPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeB, err := os.ReadFile(fixture.bPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeBInfo, err := os.Lstat(fixture.bPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeBLink := ""
	if beforeBInfo.Mode()&os.ModeSymlink != 0 {
		beforeBLink, err = os.Readlink(fixture.bPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	doc, err := lockfile.Read(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalB, err := lifecycle.CanonicalRegistryURL(fixture.originB)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := ""
	for _, registry := range doc.Registries {
		if registry.URL != canonicalB {
			continue
		}
		oid, err := cache.ParseGitOID(registry.Commit.String())
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err = cache.OpenExactGit(canonicalB, oid)
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if snapshot == "" {
		t.Fatal("B exact snapshot not found in seeded lock")
	}
	if err := os.RemoveAll(snapshot); err != nil {
		t.Fatal(err)
	}
	var acquisitions atomic.Int32
	originalHook := lifecycle.LockSnapshotAcquiredHook
	lifecycle.LockSnapshotAcquiredHook = func() { acquisitions.Add(1) }
	t.Cleanup(func() { lifecycle.LockSnapshotAcquiredHook = originalHook })
	out, err := executeCommand("remove", "--from", fixture.originA)
	var lifecycle *lifecycle.LockLifecycleError
	if !errors.As(err, &lifecycle) || lifecycle.Code != "lock.offline-snapshot-missing" {
		t.Fatalf("remove error = %v, want lock.offline-snapshot-missing\n%s", err, out)
	}
	if acquisitions.Load() != 0 {
		t.Fatalf("snapshot acquisitions = %d", acquisitions.Load())
	}
	afterConfig, configErr := os.ReadFile(fixture.configPath)
	afterLock, lockErr := os.ReadFile(fixture.lockPath)
	if configErr != nil || lockErr != nil || !bytes.Equal(beforeConfig, afterConfig) || !bytes.Equal(beforeLock, afterLock) {
		t.Fatalf("failed removal changed config/lock: configErr=%v lockErr=%v", configErr, lockErr)
	}
	afterA, aErr := os.ReadFile(fixture.aPath)
	if aErr != nil || !bytes.Equal(beforeA, afterA) {
		t.Fatalf("A output changed: %q err=%v", afterA, aErr)
	}
	afterB, bErr := os.ReadFile(fixture.bPath)
	if bErr == nil {
		if !bytes.Equal(beforeB, afterB) {
			t.Fatalf("B output changed: %q", afterB)
		}
	} else {
		if !errors.Is(bErr, os.ErrNotExist) || beforeBInfo.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("B output unreadable: %v", bErr)
		}
		afterBInfo, err := os.Lstat(fixture.bPath)
		if err != nil || afterBInfo.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("B link changed: %v/%v", afterBInfo, err)
		}
		afterBLink, err := os.Readlink(fixture.bPath)
		if err != nil || afterBLink != beforeBLink {
			t.Fatalf("B link target changed: %q/%v", afterBLink, err)
		}
	}
}

func TestRemoveFromBlockedWarmLockedRegistry(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := setupBlockedRemovalAcquisitionFixture(t)
	writeConfig(t, filepath.Join(fixture.home, ".agents", "config.yaml"), "\nharnesses: [factory]\nallowed_registries: []\n")
	beforeB, err := os.ReadFile(fixture.bPath)
	if err != nil {
		t.Fatal(err)
	}
	var acquisitions atomic.Int32
	originalHook := lifecycle.LockSnapshotAcquiredHook
	lifecycle.LockSnapshotAcquiredHook = func() { acquisitions.Add(1) }
	t.Cleanup(func() { lifecycle.LockSnapshotAcquiredHook = originalHook })
	if out, err := executeCommand("remove", "--from", fixture.originA); err != nil {
		t.Fatalf("remove blocked warm registry: %v\n%s", err, out)
	}
	if acquisitions.Load() != 0 {
		t.Fatalf("snapshot acquisitions = %d", acquisitions.Load())
	}
	if _, err := os.Stat(fixture.aPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed registry output remains: %v", err)
	}
	afterB, err := os.ReadFile(fixture.bPath)
	if err != nil || !bytes.Equal(beforeB, afterB) {
		t.Fatalf("unrelated output changed: %q err=%v", afterB, err)
	}
}

func TestRemoveFromAllowedRegistryMayAcquireNewDependency(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := setupBlockedRemovalAcquisitionFixture(t)
	addBlockedRemovalThirdRegistry(t, fixture)
	var acquisitions atomic.Int32
	originalHook := lifecycle.LockSnapshotAcquiredHook
	lifecycle.LockSnapshotAcquiredHook = func() { acquisitions.Add(1) }
	t.Cleanup(func() { lifecycle.LockSnapshotAcquiredHook = originalHook })
	if out, err := executeCommand("remove", "--from", fixture.originA); err != nil {
		t.Fatalf("remove allowed registry: %v\n%s", err, out)
	}
	if acquisitions.Load() == 0 {
		t.Fatal("allowed removal did not acquire newly configured registry")
	}
	if _, err := os.Stat(fixture.aPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed registry output remains: %v", err)
	}
	data, err := os.ReadFile(fixture.cPath)
	if err != nil || !strings.Contains(string(data), "BODY-C") {
		t.Fatalf("new allowed output = %q err=%v", data, err)
	}
}

// TestRemoveFromFlag_BlockedRegistry_RemovesNamedResource verifies that a
// single resource can be removed from a registry that is now blocked by
// allowed_registries. The registry entry remains, but the named resource is
// excluded and de-synced while its siblings stay materialized.
func TestRemoveFromFlag_BlockedRegistry_RemovesNamedResource(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "blocked-named.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", nil)
	writeSkill(t, working, "k8s-onboarding", "Kubernetes onboarding", nil)
	commitAndPushAll(t, working, "setup")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
harnesses:
  - factory
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
    ref: main
resources:
  registry:
    all: true
`)
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v\n%s", err, out)
	}
	for _, name := range []string{"vm-lifecycle", "k8s-onboarding"} {
		assertFactorySkillExists(t, repoRoot, name)
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

	assertFactorySkillAbsent(t, repoRoot, "vm-lifecycle")
	assertFactorySkillExists(t, repoRoot, "k8s-onboarding")
}
