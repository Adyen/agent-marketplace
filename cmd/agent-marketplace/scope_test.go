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

// TestInstallScopeUserWithOrgFallbackCreatesV3ConfigWithoutHarnesses pins the
// first-run creation contract for install --scope user: when
// ~/.agents/config.yaml is missing and the machine-wide target set comes from
// the organization layer, the install creates the user-personal config as
// minimal agent-marketplace/v3 WITHOUT a harnesses list — a generated list
// would become authoritative and silently override the organization fallback
// on every later run.
func TestInstallScopeUserWithOrgFallbackCreatesV3ConfigWithoutHarnesses(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "org-install-skills.git")
	writeSkill(t, working, "org-adopted-skill", "Org adopted skill", []string{"org"})
	commitAndPushAll(t, working, "Add org skills")

	// The organization layer owns the registry and the machine-wide target
	// harness set. No user-personal config exists.
	orgPath := filepath.Join(t.TempDir(), "org-config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      include:
        - org-adopted-skill
`)
	config.SetTestV3OrganizationConfigPath(t, orgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--scope", "user", "org-adopted-skill")
	if err != nil {
		t.Fatalf("expected install --scope user to succeed: %v\n%s", err, output)
	}

	// The install created the missing user config to hold the selection. It
	// must be strict v3 and must NOT pin a harnesses list.
	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("install --scope user did not create %q: %v", globalPath, err)
	}
	content := string(data)
	if !strings.Contains(content, "apiVersion: "+config.APIVersionV3) {
		t.Fatalf("created user config must be %s, got:\n%s", config.APIVersionV3, content)
	}
	if strings.Contains(content, "harnesses:") {
		t.Fatalf("created user config must not pin harnesses (the organization fallback must survive), got:\n%s", content)
	}

	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatalf("load created user config: %v", err)
	}
	if globalCfg == nil || len(globalCfg.Registries) != 1 {
		t.Fatalf("expected the registry entry in the created user config, got %#v", globalCfg)
	}
	if !containsStr(globalCfg.Registries[0].Skills.Include, "org-adopted-skill") {
		t.Fatalf("expected org-adopted-skill in the user config include, got %#v", globalCfg.Registries[0])
	}

	// The organization fallback still drives materialization: the skill landed
	// at machine scope through the org-declared factory harness.
	assertFactorySkillExists(t, homeDir, "org-adopted-skill")
}

// ---------------------------------------------------------------------------
// install --scope local
// ---------------------------------------------------------------------------

func TestInstallScopeLocalWritesToLocalConfig(t *testing.T) {
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

func TestInstallScopeRepoRemovesRepoExclude(t *testing.T) {
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
// install --scope repo: skeleton creation is strict v3 and carries no
// harnesses (the machine-wide target set stays with the user/org layers)
// ---------------------------------------------------------------------------

// TestInstallScopeRepoCreatesV3SkeletonWithoutHarnesses pins the first-run
// repo-config creation contract for install: when no .agents/config.yaml
// exists, install --scope repo creates a skeleton that is born into the
// current schema (agent-marketplace/v3) and does NOT declare a top-level
// harnesses list — top-level harnesses in a repo config are forbidden
// because the target harness set is machine-wide.
func TestInstallScopeRepoCreatesV3SkeletonWithoutHarnesses(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skeleton-skills.git")
	writeSkill(t, working, "skeleton-skill", "Skeleton skill", []string{"skeleton"})
	commitAndPushAll(t, working, "Add skeleton skill")

	// The user-personal config provides the registry and the machine-wide
	// target set; the repo has NO config yet.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--scope", "repo", "skeleton-skill")
	if err != nil {
		t.Fatalf("expected install --scope repo to succeed: %v\n%s", err, output)
	}

	repoConfigPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	data, err := os.ReadFile(repoConfigPath)
	if err != nil {
		t.Fatalf("install --scope repo did not create %q: %v", repoConfigPath, err)
	}
	content := string(data)
	if !strings.Contains(content, "apiVersion: "+config.APIVersionV3) {
		t.Fatalf("created repo skeleton must be %s, got:\n%s", config.APIVersionV3, content)
	}
	if strings.Contains(content, "harnesses:") {
		t.Fatalf("created repo skeleton must not declare harnesses (the target harness set is machine-wide), got:\n%s", content)
	}

	repoCfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("load created repo config: %v", err)
	}
	if repoCfg == nil || repoCfg.APIVersion != config.APIVersionV3 {
		t.Fatalf("created repo config apiVersion = %+v, want %s", repoCfg, config.APIVersionV3)
	}
	if len(repoCfg.Registries) != 1 || !containsStr(repoCfg.Registries[0].Skills.Include, "skeleton-skill") {
		t.Fatalf("expected shadow registry entry with skeleton-skill include, got %#v", repoCfg)
	}

	assertFactorySkillExists(t, repoRoot, "skeleton-skill")
}

// TestInstallScopeRepoFromSubdirCreatesV3SkeletonWithoutHarnesses pins the
// monorepo-subdirectory skeleton: when install --scope repo runs from a
// subdirectory with no .agents/config.yaml, the CLI seeds a new project root
// in the cwd instead of writing to an ancestor config. The seeded skeleton
// must be strict v3 with no harnesses list.
func TestInstallScopeRepoFromSubdirCreatesV3SkeletonWithoutHarnesses(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "subdir-skills.git")
	writeSkill(t, working, "subdir-skill", "Subdir skill", []string{"subdir"})
	commitAndPushAll(t, working, "Add subdir skill")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	monorepo := filepath.Join(t.TempDir(), "monorepo")
	if err := os.MkdirAll(monorepo, 0o755); err != nil {
		t.Fatalf("mkdir monorepo: %v", err)
	}
	runGit(t, "", "init", "--initial-branch=main", monorepo)
	subdir := filepath.Join(monorepo, "payments")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	withWorkingDir(t, subdir)

	output, err := executeCommand("install", "--scope", "repo", "subdir-skill")
	if err != nil {
		t.Fatalf("expected install --scope repo from subdir to succeed: %v\n%s", err, output)
	}

	// The skeleton lands in the cwd, which becomes the new project root; the
	// parent monorepo stays config-free.
	subdirConfigPath := filepath.Join(subdir, ".agents", "config.yaml")
	data, err := os.ReadFile(subdirConfigPath)
	if err != nil {
		t.Fatalf("install did not create the subdir skeleton %q: %v", subdirConfigPath, err)
	}
	content := string(data)
	if !strings.Contains(content, "apiVersion: "+config.APIVersionV3) {
		t.Fatalf("subdir skeleton must be %s, got:\n%s", config.APIVersionV3, content)
	}
	if strings.Contains(content, "harnesses:") {
		t.Fatalf("subdir skeleton must not declare harnesses, got:\n%s", content)
	}
	if _, err := os.Stat(filepath.Join(monorepo, ".agents", "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("parent monorepo config must not be created, stat err=%v", err)
	}

	repoCfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("load created subdir repo config: %v", err)
	}
	if repoCfg == nil || repoCfg.APIVersion != config.APIVersionV3 {
		t.Fatalf("subdir repo config apiVersion = %+v, want %s", repoCfg, config.APIVersionV3)
	}
	if len(repoCfg.Registries) != 1 || !containsStr(repoCfg.Registries[0].Skills.Include, "subdir-skill") {
		t.Fatalf("expected shadow registry entry with subdir-skill include, got %#v", repoCfg)
	}

	// The subdir is the new project root, so repo-scope output materializes there.
	assertFactorySkillExists(t, subdir, "subdir-skill")
}

// ---------------------------------------------------------------------------
// remove: required skill → error
// ---------------------------------------------------------------------------

func TestRemoveRequiredSkillFails(t *testing.T) {
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
// remove --scope user with a missing user config: the created file is strict
// v3 and carries no harnesses, so the organization fallback keeps applying
// ---------------------------------------------------------------------------

// TestRemoveScopeUserWithOrgFallbackCreatesV3ConfigWithoutHarnesses pins the
// first-run creation contract for the remove/exclude user-scope path: when
// ~/.agents/config.yaml is missing and the machine-wide target set comes from
// the organization layer, removing an org-provided resource creates the
// user-personal config as minimal agent-marketplace/v3 WITHOUT a harnesses
// list. Injecting the code-default harnesses here would make the user layer
// authoritative and silently override the organization fallback.
func TestRemoveScopeUserWithOrgFallbackCreatesV3ConfigWithoutHarnesses(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "org-skills.git")
	writeSkill(t, working, "keep-skill", "Keep skill", []string{"keep"})
	writeSkill(t, working, "drop-skill", "Drop skill", []string{"drop"})
	commitAndPushAll(t, working, "Add org skills")

	// The organization layer owns the registry, the selection, and the
	// machine-wide target harness set. No user-personal config exists.
	orgPath := filepath.Join(t.TempDir(), "org-config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      include:
        - keep-skill
        - drop-skill
`)
	config.SetTestV3OrganizationConfigPath(t, orgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}
	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
		t.Fatalf("sync must not create the user config under organization fallback, stat err=%v", err)
	}
	assertFactorySkillExists(t, homeDir, "keep-skill")
	assertFactorySkillExists(t, homeDir, "drop-skill")

	output, err := executeCommand("remove", "--scope", "user", "drop-skill")
	if err != nil {
		t.Fatalf("expected remove --scope user to succeed: %v\n%s", err, output)
	}

	// The remove created the missing user config to hold the exclude. It must
	// be strict v3 and must NOT pin a harnesses list...
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("remove --scope user did not create %q: %v", globalPath, err)
	}
	content := string(data)
	if !strings.Contains(content, "apiVersion: "+config.APIVersionV3) {
		t.Fatalf("created user config must be %s, got:\n%s", config.APIVersionV3, content)
	}
	if strings.Contains(content, "harnesses:") {
		t.Fatalf("created user config must not pin harnesses (the organization fallback must survive), got:\n%s", content)
	}

	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatalf("load created user config: %v", err)
	}
	if globalCfg == nil || len(globalCfg.Registries) != 1 {
		t.Fatalf("expected the shadow registry entry in the created user config, got %#v", globalCfg)
	}
	if !containsStr(globalCfg.Registries[0].Skills.Exclude, "drop-skill") {
		t.Fatalf("expected drop-skill in the shadow entry exclude, got %#v", globalCfg.Registries[0])
	}

	// ...and the organization fallback still drives materialization: the
	// exclude took effect through the org-declared factory harness.
	assertFactorySkillExists(t, homeDir, "keep-skill")
	assertFactorySkillAbsent(t, homeDir, "drop-skill")
}

// ---------------------------------------------------------------------------
// remove: default:true repo skill → adds to personal exclude
// ---------------------------------------------------------------------------

func TestRemoveDefaultSkillAddsToPersonalExclude(t *testing.T) {
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
	if localCfg == nil || !containsStr(ksExclude(localCfg.Skills), "vm-lifecycle") {
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
				localExclude = localCfg.Skills.Exclude
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
				localExclude = localCfg.Skills.Exclude
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
		if strings.TrimSpace(entry.URL) == origin {
			foundURL = true
			if !containsStr(entry.Skills.Include, "local-from-skill") {
				t.Fatalf("expected local-from-skill in registry entry skills.include, got %v", entry.Skills.Include)
			}
		}
	}
	if !foundURL {
		t.Fatalf("expected registry with URL %q in local config, got entries: %v", origin, localCfg.Registries)
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
		filepath.Join(cache.RegistryCachePath(origin, "main"), "local-from-skill"),
	)
}

// ---------------------------------------------------------------------------
// remove --scope local for RepoPersonal-sourced resource
// ---------------------------------------------------------------------------

func TestRemoveScopeLocalRemovesRepoPersonalResource(t *testing.T) {
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
