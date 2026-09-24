package main

// scope_test.go — Phase 3 integration tests for --scope on install/remove
// and polymorphic remove behavior.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
)

// ---------------------------------------------------------------------------
// install --scope user
// ---------------------------------------------------------------------------

func TestInstallScopeUserWritesToGlobalConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skills.git")
	writeSkill(t, working, "secret-scanning", "Secret scanning", []string{"security"})
	commitAndPushAll(t, working, "Add skills")

	// Global config must already know about the registry so that the inherited
	// skills list resolves correctly after install --scope user writes to it.
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

	output, err := executeCommand("install", "--scope", "user", "secret-scanning")
	if err != nil {
		t.Fatalf("expected install --scope user to succeed: %v\n%s", err, output)
	}

	// Skill must appear in global config's top-level skills list.
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatalf("load global config: %v", err)
	}
	if globalCfg == nil || !containsStr(configSkillsInclude(globalCfg), "secret-scanning") {
		t.Fatalf("expected secret-scanning to be in global config skills, got %v", globalCfg)
	}

	// Repo config must not have secret-scanning in its own skills list.
	repoCfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("load repo config: %v", err)
	}
	if containsStr(configSkillsInclude(repoCfg), "secret-scanning") {
		t.Fatalf("expected secret-scanning NOT to be in repo config skills")
	}

	// Symlink must be in the global harness skills dir because
	// secret-scanning comes from the UserPersonal (global) config layer.
	assertSymlinkTarget(t,
		filepath.Join(homeDir, ".agents", "skills", "secret-scanning"),
		filepath.Join(cache.RegistryCachePath(origin, "main"), "secret-scanning"),
	)
}

// ---------------------------------------------------------------------------
// install --scope local
// ---------------------------------------------------------------------------

func TestInstallScopeLocalWritesToLocalConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skills.git")
	writeSkill(t, working, "k8s-deploy", "K8s deploy", []string{"k8s"})
	commitAndPushAll(t, working, "Add skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--scope", "local", "k8s-deploy")
	if err != nil {
		t.Fatalf("expected install --scope local to succeed: %v\n%s", err, output)
	}

	localCfg, err := config.LoadLocalConfig()
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	if localCfg == nil || !containsStr(localSkillsInclude(localCfg), "k8s-deploy") {
		t.Fatalf("expected k8s-deploy to be in local config skills, got %v", localCfg)
	}

	repoCfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("load repo config: %v", err)
	}
	if containsStr(configSkillsInclude(repoCfg), "k8s-deploy") {
		t.Fatalf("expected k8s-deploy NOT to be in repo config skills")
	}

	// Skill must be synced into the REPO skills dir.
	assertSymlinkTarget(t,
		filepath.Join(repoRoot, ".agents", "skills", "k8s-deploy"),
		filepath.Join(cache.RegistryCachePath(origin, "main"), "k8s-deploy"),
	)
}

func TestInstallScopeLocalUsesGlobalRegistriesWhenRepoConfigHasNone(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skills.git")
	writeSkill(t, working, "shared-global-skill", "Shared global skill", []string{"shared"})
	commitAndPushAll(t, working, "Add skills")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	// Repo config exists but has no registries configured.
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--scope", "local", "shared-global-skill")
	if err != nil {
		t.Fatalf("expected install --scope local to use global registries: %v\n%s", err, output)
	}

	localCfg, err := config.LoadLocalConfig()
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	if localCfg == nil || !containsStr(localSkillsInclude(localCfg), "shared-global-skill") {
		t.Fatalf("expected shared-global-skill in local config skills, got %v", localCfg)
	}

	assertSymlinkTarget(t,
		filepath.Join(repoRoot, ".agents", "skills", "shared-global-skill"),
		filepath.Join(cache.RegistryCachePath(origin, "main"), "shared-global-skill"),
	)
}

func TestInstallScopeRepoUsesGlobalRegistriesWhenRepoConfigHasNone(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skills.git")
	writeSkill(t, working, "shared-global-repo-skill", "Shared global repo skill", []string{"shared"})
	commitAndPushAll(t, working, "Add skills")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--scope", "repo", "shared-global-repo-skill")
	if err != nil {
		t.Fatalf("expected install --scope repo to use global registries: %v\n%s", err, output)
	}

	repoCfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("load repo config: %v", err)
	}
	if repoCfg == nil || !containsStr(configSkillsInclude(repoCfg), "shared-global-repo-skill") {
		t.Fatalf("expected shared-global-repo-skill in repo config skills, got %v", repoCfg)
	}
	if len(repoCfg.Registries) != 1 || repoCfg.Registries[0].URL != origin {
		t.Fatalf("expected repo config to include providing registry %q, got %#v", origin, repoCfg.Registries)
	}

	assertSymlinkTarget(t,
		filepath.Join(repoRoot, ".agents", "skills", "shared-global-repo-skill"),
		filepath.Join(cache.RegistryCachePath(origin, "main"), "shared-global-repo-skill"),
	)
}

func TestInstallScopeRepoOrLocalRemovesLocalExclude(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	testCases := []struct {
		name        string
		scopeArg    string
		expectRepo  bool
		expectLocal bool
	}{
		{name: "repo", scopeArg: "repo", expectRepo: true},
		{name: "local", scopeArg: "local", expectLocal: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)

			origin, working := createBareRepoWithWorkingClone(t, "skills.git")
			writeSkill(t, working, "resurrect-skill", "Resurrect skill", []string{"scope"})
			commitAndPushAll(t, working, "Add resurrect-skill")

			repoRoot := filepath.Join(t.TempDir(), "repo")
			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.local.yaml"), `
skills:
  exclude:
    - resurrect-skill
`)
			withWorkingDir(t, repoRoot)

			args := []string{"install", "--scope", tc.scopeArg, "resurrect-skill"}
			if output, err := executeCommand(args...); err != nil {
				t.Fatalf("install failed: %v\n%s", err, output)
			}

			localCfg, err := config.LoadLocalConfig()
			if err != nil {
				t.Fatalf("load local config: %v", err)
			}
			if localCfg != nil && containsStr(ksExclude(localCfg.Skills), "resurrect-skill") {
				t.Fatalf("expected resurrect-skill to be removed from local exclude, got %v", localCfg.Skills.Exclude)
			}

			repoCfg, err := config.LoadRepoConfig()
			if err != nil {
				t.Fatalf("load repo config: %v", err)
			}
			if containsStr(configSkillsInclude(repoCfg), "resurrect-skill") != tc.expectRepo {
				t.Fatalf("repo skills mismatch: got=%v want=%v skills=%v", containsStr(configSkillsInclude(repoCfg), "resurrect-skill"), tc.expectRepo, configSkillsInclude(repoCfg))
			}

			localHasSkill := localCfg != nil && containsStr(localSkillsInclude(localCfg), "resurrect-skill")
			if localHasSkill != tc.expectLocal {
				t.Fatalf("local skills mismatch: got=%v want=%v skills=%v", localHasSkill, tc.expectLocal, localSkillsInclude(localCfg))
			}

			assertSymlinkTarget(t,
				filepath.Join(repoRoot, ".agents", "skills", "resurrect-skill"),
				filepath.Join(cache.RegistryCachePath(origin, "main"), "resurrect-skill"),
			)
		})
	}
}

func TestInstallV4PreservesSourceBoundLocalExclusion(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("TMPDIR", t.TempDir())

	origin, working := createBareRepoWithWorkingClone(t, "v4-local-exclude.git")
	writeSkill(t, working, "resurrect-skill", "Resurrect skill", []string{"scope"})
	commitAndPushAll(t, working, "Add resurrect-skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  main: {git: file://"+origin+", ref: main}\n")
	localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	writeConfig(t, localPath, "apiVersion: agent-marketplace/v4\nsources:\n  main: {git: file://"+origin+", ref: main}\nresources:\n  main: {exclude: [skill/resurrect-skill]}\n")
	localBefore, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--scope", "repo", "resurrect-skill")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}
	localAfter, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(localAfter) != string(localBefore) {
		t.Fatalf("install rewrote v4 local source-bound exclusion:\nbefore:\n%s\nafter:\n%s", localBefore, localAfter)
	}
	cfg, err := config.LoadV4ConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var locators []string
	for _, group := range cfg.Groups {
		for _, req := range group.Include {
			locators = append(locators, req.Locator)
		}
	}
	if len(locators) != 1 || locators[0] != "skill/resurrect-skill" {
		t.Fatalf("repo locators = %v", locators)
	}
}

func TestInstallScopeRepoRemovesRepoExclude(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skills.git")
	writeSkill(t, working, "repo-excluded-skill", "Repo excluded skill", []string{"scope"})
	commitAndPushAll(t, working, "Add repo-excluded-skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      exclude:
        - repo-excluded-skill
`)
	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("install", "--scope", "repo", "repo-excluded-skill"); err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}

	repoCfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("load repo config: %v", err)
	}
	if !containsStr(configSkillsInclude(repoCfg), "repo-excluded-skill") {
		t.Fatalf("expected repo-excluded-skill in repo skills after install, got %v", configSkillsInclude(repoCfg))
	}
	if containsStr(configSkillsExclude(repoCfg), "repo-excluded-skill") {
		t.Fatalf("expected repo-excluded-skill removed from repo exclude, got %v", configSkillsExclude(repoCfg))
	}

	assertSymlinkTarget(t,
		filepath.Join(repoRoot, ".agents", "skills", "repo-excluded-skill"),
		filepath.Join(cache.RegistryCachePath(origin, "main"), "repo-excluded-skill"),
	)
}

func TestInstallScopeRepoOrLocalOverridesUserPlacement(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	testCases := []struct {
		name          string
		scopeArg      string
		expectRepoCfg bool
		expectLocal   bool
	}{
		{name: "repo", scopeArg: "repo", expectRepoCfg: true},
		{name: "local", scopeArg: "local", expectLocal: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)

			origin, working := createBareRepoWithWorkingClone(t, "shared.git")
			writeSkill(t, working, "shared-skill", "Shared skill", []string{"shared"})
			commitAndPushAll(t, working, "Add shared-skill")

			writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - shared-skill
`)

			repoRoot := filepath.Join(t.TempDir(), "repo")
			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
			withWorkingDir(t, repoRoot)

			if _, err := executeCommand("sync"); err != nil {
				t.Fatalf("initial sync failed: %v", err)
			}

			if output, err := executeCommand("install", "--scope", tc.scopeArg, "shared-skill"); err != nil {
				t.Fatalf("install failed: %v\n%s", err, output)
			}

			repoCfg, err := config.LoadRepoConfig()
			if err != nil {
				t.Fatalf("load repo config: %v", err)
			}
			localCfg, err := config.LoadLocalConfig()
			if err != nil {
				t.Fatalf("load local config: %v", err)
			}

			if containsStr(configSkillsInclude(repoCfg), "shared-skill") != tc.expectRepoCfg {
				t.Fatalf("repo config skill mismatch: got=%v want=%v", containsStr(configSkillsInclude(repoCfg), "shared-skill"), tc.expectRepoCfg)
			}
			localHas := localCfg != nil && containsStr(localSkillsInclude(localCfg), "shared-skill")
			if localHas != tc.expectLocal {
				t.Fatalf("local config skill mismatch: got=%v want=%v", localHas, tc.expectLocal)
			}

			cacheTarget := filepath.Join(cache.RegistryCachePath(origin, "main"), "shared-skill")
			assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", "shared-skill"), cacheTarget)
			if _, err := os.Lstat(filepath.Join(homeDir, ".agents", "skills", "shared-skill")); !os.IsNotExist(err) {
				t.Fatalf("expected user-scoped symlink to be removed, got err=%v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// remove: required skill → error
// ---------------------------------------------------------------------------

func TestRemoveRequiredSkillFails(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "security.git")
	writeSkill(t, working, "no-curl-bash", "No curl bash", []string{"security"})
	commitAndPushAll(t, working, "Add skills")

	// Global config marks registry as required.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    required: true
    skills:
      - no-curl-bash
`)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	// Sync first so the skill is installed.
	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	output, err := executeCommand("remove", "no-curl-bash")
	if err == nil {
		t.Fatalf("expected remove of required skill to fail, got output: %q", output)
	}
	if !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected 'required' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// remove: machine-scope non-required skill at default scope → succeeds via user scope
// ---------------------------------------------------------------------------

func TestRemoveMachineScopeNonRequiredAtDefaultScopeSucceeds(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "security.git")
	writeSkill(t, working, "shell-hardening", "Shell hardening", []string{"security"})
	commitAndPushAll(t, working, "Add skills")

	// Global config: non-required explicit skill in registry entry.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - shell-hardening
`)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	// Default scope should now infer user scope for machine-scope resources and succeed.
	output, err := executeCommand("remove", "shell-hardening")
	if err != nil {
		t.Fatalf("expected remove of machine-scope non-required skill to succeed, got error: %v\n%s", err, output)
	}

	// The skill should be excluded from the user config.
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatalf("load global config: %v", err)
	}
	if globalCfg != nil && containsStr(configSkillsInclude(globalCfg), "shell-hardening") {
		t.Fatal("expected shell-hardening to be removed from global config include")
	}

	// Output must note that the exclusion was written to the user config.
	if !strings.Contains(output, "applies across all repos") {
		t.Fatalf("expected user-scope note in output, got: %q", output)
	}
	if !strings.Contains(filepath.ToSlash(output), ".agents/config.yaml") {
		t.Fatalf("expected config path in output, got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// remove: default:true repo skill → adds to personal exclude
// ---------------------------------------------------------------------------

func TestRemoveDefaultSkillAddsToPersonalExclude(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "infra.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	writeSkill(t, working, "k8s-deploy", "K8s deploy", []string{"k8s"})
	commitAndPushAll(t, working, "Add skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	// Registry with default:true — all skills auto-selected.
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

	output, err := executeCommand("remove", "vm-lifecycle")
	if err != nil {
		t.Fatalf("expected remove of default skill to succeed: %v\n%s", err, output)
	}

	localCfg, err := config.LoadLocalConfig()
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	excluded := localCfg != nil && containsStr(ksExclude(localCfg.Skills), "vm-lifecycle")
	if localCfg != nil {
		for _, reg := range localCfg.Registries {
			if containsStr(ksExclude(reg.Skills), "vm-lifecycle") {
				excluded = true
			}
		}
	}
	if !excluded {
		t.Fatalf("expected vm-lifecycle in personal exclude, got: %v", localCfg)
	}

	// The repo config must remain unchanged (no removal from skills list,
	// since the skill was auto-selected via default:true, not listed explicitly).
	repoCfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("load repo config: %v", err)
	}
	if containsStr(configSkillsInclude(repoCfg), "vm-lifecycle") {
		t.Fatalf("expected vm-lifecycle NOT to be in repo config skills (it was auto-selected)")
	}
}

// ---------------------------------------------------------------------------
// remove --scope user: remove machine-scope skill from global config
// ---------------------------------------------------------------------------

func TestRemoveScopeUserRemovesFromGlobalConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "security.git")
	writeSkill(t, working, "shell-hardening", "Shell hardening", []string{"security"})
	commitAndPushAll(t, working, "Add skills")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - shell-hardening
`)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	output, err := executeCommand("remove", "--scope", "user", "shell-hardening")
	if err != nil {
		t.Fatalf("expected remove --scope user to succeed: %v\n%s", err, output)
	}

	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatalf("load global config: %v", err)
	}
	if containsStr(configSkillsInclude(globalCfg), "shell-hardening") {
		t.Fatalf("expected shell-hardening to be removed from global config, got: %v", configSkillsInclude(globalCfg))
	}
}

func TestInstallScopeMatrixWritesExpectedLayer(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	testCases := []struct {
		name          string
		scopeArg      string
		expectRepo    bool
		expectUser    bool
		expectLocal   bool
		expectSymlink string
	}{
		{name: "default-repo-scope", scopeArg: "", expectRepo: true, expectSymlink: "repo"},
		{name: "explicit-repo-scope", scopeArg: "repo", expectRepo: true, expectSymlink: "repo"},
		{name: "user-scope", scopeArg: "user", expectUser: true, expectSymlink: "user"},
		{name: "local-scope", scopeArg: "local", expectLocal: true, expectSymlink: "repo"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)

			origin, working := createBareRepoWithWorkingClone(t, "skills.git")
			writeSkill(t, working, "scope-matrix-skill", "Scope matrix", []string{"scope"})
			commitAndPushAll(t, working, "Add scope-matrix skill")

			repoRoot := filepath.Join(t.TempDir(), "repo")
			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
			withWorkingDir(t, repoRoot)

			args := []string{"install"}
			if tc.scopeArg != "" {
				args = append(args, "--scope", tc.scopeArg)
			}
			args = append(args, "scope-matrix-skill")

			output, err := executeCommand(args...)
			if err != nil {
				t.Fatalf("install failed: %v\n%s", err, output)
			}

			repoCfg, err := config.LoadRepoConfig()
			if err != nil {
				t.Fatalf("load repo config: %v", err)
			}
			globalCfg, err := config.LoadGlobalConfig()
			if err != nil {
				t.Fatalf("load global config: %v", err)
			}
			localCfg, err := config.LoadLocalConfig()
			if err != nil {
				t.Fatalf("load local config: %v", err)
			}

			var repoSkills []string
			if repoCfg != nil {
				repoSkills = configSkillsInclude(repoCfg)
			}
			var globalSkills []string
			if globalCfg != nil {
				globalSkills = configSkillsInclude(globalCfg)
			}
			localSkills := localSkillsInclude(localCfg)

			if containsStr(repoSkills, "scope-matrix-skill") != tc.expectRepo {
				t.Fatalf("repo config skill presence mismatch: got=%v want=%v skills=%v", containsStr(repoSkills, "scope-matrix-skill"), tc.expectRepo, repoSkills)
			}
			if containsStr(globalSkills, "scope-matrix-skill") != tc.expectUser {
				t.Fatalf("global config skill presence mismatch: got=%v want=%v skills=%v", containsStr(globalSkills, "scope-matrix-skill"), tc.expectUser, globalSkills)
			}
			if containsStr(localSkills, "scope-matrix-skill") != tc.expectLocal {
				t.Fatalf("local config skill presence mismatch: got=%v want=%v skills=%v", containsStr(localSkills, "scope-matrix-skill"), tc.expectLocal, localSkills)
			}

			cacheTarget := filepath.Join(cache.RegistryCachePath(origin, "main"), "scope-matrix-skill")
			if tc.expectSymlink == "user" {
				assertSymlinkTarget(t, filepath.Join(homeDir, ".agents", "skills", "scope-matrix-skill"), cacheTarget)
				if _, err := os.Lstat(filepath.Join(repoRoot, ".agents", "skills", "scope-matrix-skill")); !os.IsNotExist(err) {
					t.Fatalf("expected repo symlink to be absent for user scope, got err=%v", err)
				}
			} else {
				assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", "scope-matrix-skill"), cacheTarget)
			}
		})
	}
}

func TestRemoveScopeMatrixForRepoSelectedSkill(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	testCases := []struct {
		name                string
		scopeArg            string
		globalHasSkill      bool // whether the global config also selects the skill
		expectRepoHasSkill  bool
		expectUserHasSkill  bool
		expectLocalExcluded bool
		expectSymlink       bool
	}{
		{name: "default-infers-repo", scopeArg: "", expectRepoHasSkill: false, expectSymlink: false},
		{name: "explicit-repo", scopeArg: "repo", expectRepoHasSkill: false, expectSymlink: false},
		{name: "explicit-local", scopeArg: "local", expectRepoHasSkill: true, expectLocalExcluded: true, expectSymlink: false},
		{name: "explicit-user", scopeArg: "user", globalHasSkill: true, expectRepoHasSkill: true, expectUserHasSkill: false, expectSymlink: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)

			origin, working := createBareRepoWithWorkingClone(t, "repo-skill.git")
			writeSkill(t, working, "repo-scope-skill", "Repo scope skill", []string{"scope"})
			commitAndPushAll(t, working, "Add repo-scope-skill")

			// Build global config: include the skill in the registry entry
			// only when the subtest expects --scope user to be able to
			// remove it from the global layer.  Otherwise the global layer
			// has no skill selection so that repo-scope removal leaves the
			// user layer empty (expectUserHasSkill: false).
			globalSkillYAML := ""
			if tc.globalHasSkill {
				globalSkillYAML = "\n    skills:\n      - repo-scope-skill"
			}
			writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main`+globalSkillYAML+`
`)

			repoRoot := filepath.Join(t.TempDir(), "repo")
			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - repo-scope-skill
`)
			withWorkingDir(t, repoRoot)

			if _, err := executeCommand("sync"); err != nil {
				t.Fatalf("initial sync failed: %v", err)
			}

			args := []string{"remove"}
			if tc.scopeArg != "" {
				args = append(args, "--scope", tc.scopeArg)
			}
			args = append(args, "repo-scope-skill")
			if output, err := executeCommand(args...); err != nil {
				t.Fatalf("remove failed: %v\n%s", err, output)
			}

			repoCfg, err := config.LoadRepoConfig()
			if err != nil {
				t.Fatalf("load repo config: %v", err)
			}
			globalCfg, err := config.LoadGlobalConfig()
			if err != nil {
				t.Fatalf("load global config: %v", err)
			}
			localCfg, err := config.LoadLocalConfig()
			if err != nil {
				t.Fatalf("load local config: %v", err)
			}

			var repoSkills []string
			if repoCfg != nil {
				repoSkills = configSkillsInclude(repoCfg)
			}
			var globalSkills []string
			if globalCfg != nil {
				globalSkills = configSkillsInclude(globalCfg)
			}
			var localExclude []string
			if localCfg != nil {
				localExclude = append(localExclude, localCfg.Skills.Exclude...)
				for _, reg := range localCfg.Registries {
					localExclude = append(localExclude, reg.Skills.Exclude...)
				}
			}

			if containsStr(repoSkills, "repo-scope-skill") != tc.expectRepoHasSkill {
				t.Fatalf("repo skill presence mismatch: got=%v want=%v skills=%v", containsStr(repoSkills, "repo-scope-skill"), tc.expectRepoHasSkill, repoSkills)
			}
			if containsStr(globalSkills, "repo-scope-skill") != tc.expectUserHasSkill {
				t.Fatalf("global skill presence mismatch: got=%v want=%v skills=%v", containsStr(globalSkills, "repo-scope-skill"), tc.expectUserHasSkill, globalSkills)
			}
			if containsStr(localExclude, "repo-scope-skill") != tc.expectLocalExcluded {
				t.Fatalf("local exclude mismatch: got=%v want=%v exclude=%v", containsStr(localExclude, "repo-scope-skill"), tc.expectLocalExcluded, localExclude)
			}

			symlinkPath := filepath.Join(repoRoot, ".agents", "skills", "repo-scope-skill")
			if tc.expectSymlink {
				assertSymlinkTarget(t, symlinkPath, filepath.Join(cache.RegistryCachePath(origin, "main"), "repo-scope-skill"))
			} else if _, err := os.Lstat(symlinkPath); !os.IsNotExist(err) {
				t.Fatalf("expected symlink to be absent, got err=%v", err)
			}
		})
	}
}

func TestRemoveScopeMatrixForUserSelectedSkill(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	testCases := []struct {
		name               string
		scopeArg           string
		expectErrContains  string
		expectUserHasSkill bool
	}{
		{name: "default-infers-user", scopeArg: "", expectUserHasSkill: false},
		{name: "explicit-local", scopeArg: "local", expectErrContains: "Use --scope user", expectUserHasSkill: true},
		{name: "explicit-user", scopeArg: "user", expectUserHasSkill: false},
		{name: "explicit-repo", scopeArg: "repo", expectErrContains: "Use --scope user", expectUserHasSkill: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)

			origin, working := createBareRepoWithWorkingClone(t, "user-skill.git")
			writeSkill(t, working, "user-scope-skill", "User scope skill", []string{"scope"})
			commitAndPushAll(t, working, "Add user-scope-skill")

			writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - user-scope-skill
`)

			repoRoot := filepath.Join(t.TempDir(), "repo")
			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
			withWorkingDir(t, repoRoot)

			if _, err := executeCommand("sync"); err != nil {
				t.Fatalf("initial sync failed: %v", err)
			}

			args := []string{"remove"}
			if tc.scopeArg != "" {
				args = append(args, "--scope", tc.scopeArg)
			}
			args = append(args, "user-scope-skill")
			output, err := executeCommand(args...)
			if tc.expectErrContains != "" {
				if err == nil {
					t.Fatalf("expected remove to fail, output: %s", output)
				}
				if !strings.Contains(err.Error(), tc.expectErrContains) {
					t.Fatalf("expected error containing %q, got: %v", tc.expectErrContains, err)
				}
			} else if err != nil {
				t.Fatalf("remove failed: %v\n%s", err, output)
			}

			globalCfg, err := config.LoadGlobalConfig()
			if err != nil {
				t.Fatalf("load global config: %v", err)
			}
			localCfg, err := config.LoadLocalConfig()
			if err != nil {
				t.Fatalf("load local config: %v", err)
			}

			var globalSkills []string
			if globalCfg != nil {
				globalSkills = configSkillsInclude(globalCfg)
			}
			var localExclude []string
			if localCfg != nil {
				localExclude = append(localExclude, localCfg.Skills.Exclude...)
				for _, reg := range localCfg.Registries {
					localExclude = append(localExclude, reg.Skills.Exclude...)
				}
			}

			if containsStr(globalSkills, "user-scope-skill") != tc.expectUserHasSkill {
				t.Fatalf("global skill presence mismatch: got=%v want=%v skills=%v", containsStr(globalSkills, "user-scope-skill"), tc.expectUserHasSkill, globalSkills)
			}
			if containsStr(localExclude, "user-scope-skill") {
				t.Fatalf("local exclude should never be written for machine-scope skill, got %v", localExclude)
			}
			symlinkPath := filepath.Join(homeDir, ".agents", "skills", "user-scope-skill")
			if tc.expectUserHasSkill {
				assertSymlinkTarget(t, symlinkPath, filepath.Join(cache.RegistryCachePath(origin, "main"), "user-scope-skill"))
			} else if _, err := os.Lstat(symlinkPath); !os.IsNotExist(err) {
				t.Fatalf("expected user symlink to be absent, got err=%v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// install --from <local-path> --scope local: registry entry in config.local.yaml
// ---------------------------------------------------------------------------

func TestInstallFromFlagScopeLocalWritesRegistryEntryToLocalConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "local-from-skill.git")
	writeSkill(t, working, "local-from-skill", "Local from skill", []string{"local"})
	commitAndPushAll(t, working, "Add skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "local-from-skill", "--from", origin, "--ref", "main", "--scope", "local")
	if err != nil {
		t.Fatalf("expected install --from --scope local to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "Installed local-from-skill") {
		t.Fatalf("expected install output for local-from-skill, got %q", output)
	}

	// The registry entry must appear in config.local.yaml, not config.yaml.
	localCfg, err := config.LoadLocalConfig()
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	if localCfg == nil {
		t.Fatal("expected local config to exist after install --from --scope local")
	}
	if len(localCfg.Registries) == 0 {
		t.Fatal("expected at least one registry entry in local config")
	}
	foundURL := false
	for _, entry := range localCfg.Registries {
		if strings.TrimSpace(entry.URL) == "file://"+origin {
			foundURL = true
			if !containsStr(entry.Skills.Include, "local-from-skill") {
				t.Fatalf("expected local-from-skill in registry entry skills.include, got %v", entry.Skills.Include)
			}
		}
	}
	if !foundURL {
		t.Fatalf("expected registry with URL %q in local config, got entries: %v", "file://"+origin, localCfg.Registries)
	}

	// Repo config must NOT have the registry entry.
	repoCfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("load repo config: %v", err)
	}
	for _, entry := range repoCfg.Registries {
		if strings.TrimSpace(entry.URL) == origin {
			t.Fatalf("registry should NOT be in repo config, found URL %q", origin)
		}
	}

	// Skill must be materialized after sync.
	assertSymlinkTarget(t,
		filepath.Join(repoRoot, ".agents", "skills", "local-from-skill"),
		filepath.Join(cache.RegistryCachePath("file://"+origin, "main"), "local-from-skill"),
	)
}

// ---------------------------------------------------------------------------
// remove --scope local for RepoPersonal-sourced resource
// ---------------------------------------------------------------------------

func TestRemoveScopeLocalRemovesRepoPersonalResource(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "local-remove-skill.git")
	writeSkill(t, working, "local-remove-skill", "Local remove skill", []string{"local"})
	commitAndPushAll(t, working, "Add skills")

	// Set up: global config with the registry, repo config with the registry.
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

	// Pre-populate config.local.yaml with a registry entry that selects
	// the skill — this makes the resource's source RepoPersonal.
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.local.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - local-remove-skill
`)
	withWorkingDir(t, repoRoot)

	// Sync first so the skill is installed.
	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	output, err := executeCommand("remove", "--scope", "local", "local-remove-skill")
	if err != nil {
		t.Fatalf("expected remove --scope local to succeed: %v\n%s", err, output)
	}

	// Verify the skill is removed from the registry entry in config.local.yaml.
	localCfg, err := config.LoadLocalConfig()
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	if localCfg == nil {
		t.Fatal("expected local config to exist after remove --scope local")
	}
	for _, entry := range localCfg.Registries {
		if strings.TrimSpace(entry.URL) == origin {
			if containsStr(entry.Skills.Include, "local-remove-skill") {
				t.Fatalf("expected local-remove-skill to be removed from registry entry skills.include, still present")
			}
			// The skill should be in the exclude list (to prevent re-selection
			// by default/required/tags on the same entry).
			if !containsStr(entry.Skills.Exclude, "local-remove-skill") {
				t.Fatalf("expected local-remove-skill in registry entry skills.exclude, got %v", entry.Skills.Exclude)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// install <name> --scope local: writes to registry entry when source is RepoPersonal
// ---------------------------------------------------------------------------

func TestInstallScopeLocalWritesToRegistryEntryForRepoPersonalSource(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "local-entry-skill.git")
	writeSkill(t, working, "local-entry-skill", "Local entry skill", []string{"local"})
	commitAndPushAll(t, working, "Add skills")

	// Global config: has harnesses but no registries. The local config's
	// registry is the only one, so the providing entry has RepoPersonal source
	// and install --scope local writes to the registry entry (not the
	// top-level selector).
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries: []
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)

	// Pre-populate config.local.yaml with a registries: entry for the registry.
	// This makes the skill discoverable from the RepoPersonal layer.
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.local.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--scope", "local", "local-entry-skill")
	if err != nil {
		t.Fatalf("expected install --scope local to succeed: %v\n%s", err, output)
	}

	localCfg, err := config.LoadLocalConfig()
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	if localCfg == nil {
		t.Fatal("expected local config to exist after install --scope local")
	}

	// The skill must be in the registry entry's skills.include, NOT in the
	// top-level skills: include.
	for _, entry := range localCfg.Registries {
		if strings.TrimSpace(entry.URL) == origin {
			if !containsStr(entry.Skills.Include, "local-entry-skill") {
				t.Fatalf("expected local-entry-skill in registry entry skills.include, got %v", entry.Skills.Include)
			}
		}
	}

	// Top-level skills: include must NOT contain the skill (it should be
	// in the registry entry, not the top-level selector).
	if containsStr(ksInclude(localCfg.Skills), "local-entry-skill") {
		t.Fatalf("expected local-entry-skill NOT to be in top-level skills.include, got %v", localCfg.Skills.Include)
	}

	// Skill must be materialized after sync.
	assertSymlinkTarget(t,
		filepath.Join(repoRoot, ".agents", "skills", "local-entry-skill"),
		filepath.Join(cache.RegistryCachePath(origin, "main"), "local-entry-skill"),
	)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// ksInclude extracts the Include list from a KindSelector for use with containsStr.
func ksInclude(sel config.KindSelector) []string {
	return sel.Include
}

// ksExclude extracts the Exclude list from a KindSelector for use with containsStr.
func ksExclude(sel config.KindSelector) []string {
	return sel.Exclude
}

// configSkillsInclude collects all skill names from the Include lists of every
// registry entry in cfg. Used in place of the removed cfg.Skills.Include field.
func configSkillsInclude(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	var out []string
	for _, entry := range cfg.Registries {
		out = append(out, entry.Skills.Include...)
	}
	return out
}

// localSkillsInclude collects skill includes from a LocalConfig across both the
// top-level selector and every registry entry. Install/TUI now write
// registry-scoped includes, so tests assert "enabled in local config"
// regardless of which representation is used.
func localSkillsInclude(localCfg *config.LocalConfig) []string {
	if localCfg == nil {
		return nil
	}
	out := append([]string(nil), localCfg.Skills.Include...)
	for _, entry := range localCfg.Registries {
		out = append(out, entry.Skills.Include...)
	}
	return out
}

// configSkillsExclude collects all skill names from the Exclude lists of every
// registry entry in cfg. Used in place of the removed cfg.Skills.Exclude field.
func configSkillsExclude(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	var out []string
	for _, entry := range cfg.Registries {
		out = append(out, entry.Skills.Exclude...)
	}
	return out
}
