package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

func setupMultiNameInstall(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	return home
}

func multiNameRepo(t *testing.T, configBody string) string {
	t.Helper()
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), configBody)
	withWorkingDir(t, repoRoot)
	return repoRoot
}

func stubDependencyCommit(t *testing.T, inspect func(desired []dependencytxn.DesiredFile)) *int {
	t.Helper()
	calls := new(int)
	originalCommit := commitDependencyTransaction
	commitDependencyTransaction = func(ctx context.Context, stateDir string, desired []dependencytxn.DesiredFile, options ...dependencytxn.Option) (dependencytxn.Result, error) {
		*calls++
		if inspect != nil {
			inspect(desired)
		}
		return originalCommit(ctx, stateDir, desired, options...)
	}
	t.Cleanup(func() { commitDependencyTransaction = originalCommit })
	return calls
}

// spec: resource-installation/install-several-names-from-configured-sources
// spec: resource-installation/repeated-names-are-deduplicated
func TestInstallMultipleNamesFromConfiguredSources(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupMultiNameInstall(t)
	origin, working := createBareRepoWithWorkingClone(t, "multi-configured.git")
	writeSkill(t, working, "alpha", "Alpha", nil)
	writeSkill(t, working, "beta", "Beta", nil)
	writeSkill(t, working, "gamma", "Gamma", nil)
	commitAndPushAll(t, working, "add skills")

	repoRoot := multiNameRepo(t, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - gamma
`)

	output, err := executeCommand("install", "beta", "alpha", "beta")
	if err != nil {
		t.Fatalf("install beta alpha beta: %v\n%s", err, output)
	}
	if output != "Installed beta, alpha in this repo\n" {
		t.Fatalf("unexpected output %q", output)
	}
	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("load repo config: %v", err)
	}
	if got := cfg.Registries[0].Skills.Include; !slices.Equal(got, []string{"gamma", "beta", "alpha"}) {
		t.Fatalf("registry include = %#v, want unrelated gamma retained and deduped first-request order", got)
	}
	for _, name := range []string{"alpha", "beta", "gamma"} {
		assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", name), filepath.Join(cache.RegistryCachePath(origin, "main"), name))
	}
}

func batchSourceURL(schema, origin string) string {
	if schema == "v4" {
		return "file://" + origin
	}
	return origin
}

func batchSourceYAML(schema, source string) (configBody, localBody string) {
	if schema == "v4" {
		return "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {git: " + source + ", ref: main}\nresources:\n  hosted: [skill/keep]\n",
			"apiVersion: agent-marketplace/v4\nsources:\n  hosted: {git: " + source + ", ref: main}\nresources:\n  hosted: {exclude: [skill/good]}\n"
	}
	return "harnesses: [factory]\nregistries:\n  - url: " + source + "\n    ref: main\n    skills: [keep]\n",
		"skills: {exclude: [good]}\n"
}

// spec: resource-installation/install-several-names-from-one-explicit-source
func TestInstallMultipleNamesFromExplicitSource(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		schema string
		scope  string
	}{
		{schema: "v2", scope: "repo"},
		{schema: "v4", scope: "repo"},
		{schema: "v4", scope: "local"},
	} {
		t.Run(tc.schema+"/"+tc.scope, func(t *testing.T) {
			setupMultiNameInstall(t)
			origin, working := createBareRepoWithWorkingClone(t, "multi-adhoc.git")
			writeSkill(t, filepath.Join(working, "bundles"), "first", "First", nil)
			writeSkill(t, filepath.Join(working, "bundles"), "second", "Second", nil)
			writeSkill(t, working, "root-only", "Root only", nil)
			commitAndPushAll(t, working, "add skills")

			keepOrigin, keepWorking := createBareRepoWithWorkingClone(t, "multi-adhoc-prior.git")
			writeSkill(t, keepWorking, "keep", "Keep", nil)
			commitAndPushAll(t, keepWorking, "add keep")

			configBody, _ := batchSourceYAML(tc.schema, batchSourceURL(tc.schema, keepOrigin))
			repoRoot := multiNameRepo(t, configBody)
			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("seed sync: %v\n%s", err, output)
			}
			keepBefore := string(readTestFile(t, filepath.Join(repoRoot, ".factory", "skills", "keep", "SKILL.md")))

			output, err := executeCommand("install", "first", "second", "--from", batchSourceURL(tc.schema, origin), "--ref", "main", "--path", "bundles", "--scope", tc.scope)
			if err != nil {
				t.Fatalf("install first second --from: %v\n%s", err, output)
			}
			if !strings.Contains(output, "Installed first, second") {
				t.Fatalf("unexpected output %q", output)
			}

			targetConfig := filepath.Join(repoRoot, ".agents", "config.yaml")
			lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
			if tc.scope == "local" {
				targetConfig = filepath.Join(repoRoot, ".agents", "config.local.yaml")
				lockPath = filepath.Join(repoRoot, ".agents", "lock.local.yaml")
			}
			configBytes := string(readTestFile(t, targetConfig))
			for _, needle := range []string{"bundles", "first", "second"} {
				if !strings.Contains(configBytes, needle) {
					t.Fatalf("%s missing %q:\n%s", targetConfig, needle, configBytes)
				}
			}
			if strings.Contains(configBytes, "root-only") {
				t.Fatalf("target config selected unrequested resource:\n%s", configBytes)
			}
			locked := readLockDocument(t, lockPath)
			found := false
			for _, reg := range locked.Registries {
				if reg.Ref != "main" {
					continue
				}
				for _, root := range reg.Roots {
					if root.Path == "bundles" {
						found = true
					}
				}
			}
			if !found {
				t.Fatalf("acquisition lock missing requested ref/path (main/bundles):\n%s", string(readTestFile(t, lockPath)))
			}

			for _, name := range []string{"first", "second"} {
				skillDoc := string(readTestFile(t, filepath.Join(repoRoot, ".factory", "skills", name, "SKILL.md")))
				if !strings.Contains(skillDoc, "name: "+name) {
					t.Fatalf("materialized %s content unexpected:\n%s", name, skillDoc)
				}
			}
			assertFactorySkillAbsent(t, repoRoot, "root-only")
			if got := string(readTestFile(t, filepath.Join(repoRoot, ".factory", "skills", "keep", "SKILL.md"))); got != keepBefore {
				t.Fatal("explicit-source batch changed unrelated keep materialization")
			}
			if !strings.Contains(string(readTestFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"))), "keep") {
				t.Fatal("explicit-source batch dropped unrelated keep selection")
			}
		})
	}
}

// spec: resource-installation/install-several-names-from-configured-sources
func TestInstallMultipleMixedKindsNeedKindFlag(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupMultiNameInstall(t)
	origin, working := createBareRepoWithWorkingClone(t, "multi-kinds.git")
	writeSkill(t, working, "solo", "Solo", nil)
	writeSkill(t, working, "dup", "Duplicate skill", nil)
	writeCommand(t, working, "dup", "Duplicate command", "echo dup")
	commitAndPushAll(t, working, "add mixed kinds")

	multiNameRepo(t, `
registries:
  - url: `+origin+`
    ref: main
`)

	output, err := executeCommand("install", "solo", "dup", "--kind", "skill")
	if err != nil {
		t.Fatalf("install solo dup --kind skill: %v\n%s", err, output)
	}
	if output != "Installed solo, dup in this repo\n" {
		t.Fatalf("unexpected output %q", output)
	}
	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("load repo config: %v", err)
	}
	if got := cfg.Registries[0].Skills.Include; !slices.Equal(got, []string{"solo", "dup"}) {
		t.Fatalf("registry include = %#v", got)
	}
	if len(cfg.Registries[0].Commands.Include) != 0 {
		t.Fatalf("commands include = %#v, want empty", cfg.Registries[0].Commands.Include)
	}
}

// spec: resource-installation/collections-and-explicit-resources-share-a-batch
func TestInstallMultipleCollectionAndResource(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupMultiNameInstall(t)
	origin, working := createBareRepoWithWorkingClone(t, "multi-collection.git")
	writeSkillRequiring(t, working, "needy", "Needy", []string{"mcp:ghost"})
	writeSkill(t, working, "quiet", "Quiet", nil)
	writeSkill(t, working, "extra", "Extra", nil)
	writeSkillRequiring(t, working, "stray", "Stray", []string{"mcp:ghost"})
	writeCollection(t, working, "bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: bundle
version: 1.0.0
description: Bundle
skills:
  - name: needy
  - name: quiet
`)
	commitAndPushAll(t, working, "add collection")

	repoRoot := multiNameRepo(t, `
registries:
  - url: `+origin+`
    ref: main
`)

	output, err := executeCommand("install", "bundle", "extra")
	if err != nil {
		t.Fatalf("install bundle extra: %v\n%s", err, output)
	}
	if strings.Contains(output, "Installed collection") {
		t.Fatalf("batch reported singleton collection output: %q", output)
	}
	if !strings.Contains(output, "extra") {
		t.Fatalf("install output missing explicit name: %q", output)
	}
	if !strings.Contains(output, `"needy" requires mcp "ghost"`) {
		t.Fatalf("expected prerequisite warning for member needy, got:\n%s", output)
	}
	if strings.Contains(output, "stray") {
		t.Fatalf("warning leaked unrequested stray resource: %q", output)
	}
	for _, name := range []string{"needy", "quiet", "extra"} {
		assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", name), filepath.Join(cache.RegistryCachePath(origin, "main"), name))
	}
	if _, err := os.Lstat(filepath.Join(repoRoot, ".factory", "skills", "stray")); !os.IsNotExist(err) {
		t.Fatalf("unrequested stray materialized: %v", err)
	}
}

// spec: resource-installation/explicit-batch-names-clear-matching-exclusions
func TestInstallMultipleClearsLocalExcludes(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupMultiNameInstall(t)
	origin, working := createBareRepoWithWorkingClone(t, "multi-exclude.git")
	writeSkill(t, working, "exone", "Excluded one", nil)
	writeSkill(t, working, "extwo", "Excluded two", nil)
	writeSkill(t, working, "keep", "Kept excluded", nil)
	commitAndPushAll(t, working, "add skills")

	repoRoot := multiNameRepo(t, `
registries:
  - url: `+origin+`
    ref: main
    default: true
`)
	localConfig := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	writeFile(t, localConfig, "skills: {exclude: [exone, extwo, keep]}\n")

	output, err := executeCommand("install", "exone", "extwo")
	if err != nil {
		t.Fatalf("install exone extwo: %v\n%s", err, output)
	}
	localBytes, err := os.ReadFile(localConfig)
	if err != nil {
		t.Fatalf("read local config: %v", err)
	}
	if strings.Contains(string(localBytes), "exone") || strings.Contains(string(localBytes), "extwo") {
		t.Fatalf("requested excludes not cleared:\n%s", localBytes)
	}
	if !strings.Contains(string(localBytes), "keep") {
		t.Fatalf("unrelated exclude removed:\n%s", localBytes)
	}
	for _, name := range []string{"exone", "extwo"} {
		assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", name), filepath.Join(cache.RegistryCachePath(origin, "main"), name))
	}
	if _, err := os.Lstat(filepath.Join(repoRoot, ".factory", "skills", "keep")); !os.IsNotExist(err) {
		t.Fatalf("still-excluded keep materialized: %v", err)
	}
}

type batchFixture struct {
	repoRoot string
	source   string
	keep     string
	lock     string
	config   string
	local    string
}

func seedBatchFixture(t *testing.T, schema, source string) batchFixture {
	t.Helper()
	configBody, localBody := batchSourceYAML(schema, source)
	repoRoot := multiNameRepo(t, configBody)
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.local.yaml"), localBody)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync: %v\n%s", err, output)
	}
	return batchFixture{
		repoRoot: repoRoot,
		source:   source,
		keep:     string(readTestFile(t, filepath.Join(repoRoot, ".factory", "skills", "keep", "SKILL.md"))),
		lock:     string(readTestFile(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))),
		config:   string(readTestFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"))),
		local:    string(readTestFile(t, filepath.Join(repoRoot, ".agents", "config.local.yaml"))),
	}
}

func (f batchFixture) requirePreserved(t *testing.T) {
	t.Helper()
	for path, before := range map[string]string{
		filepath.Join(f.repoRoot, ".factory", "skills", "keep", "SKILL.md"): f.keep,
		filepath.Join(f.repoRoot, ".agents", "lock.yaml"):                   f.lock,
		filepath.Join(f.repoRoot, ".agents", "config.yaml"):                 f.config,
		filepath.Join(f.repoRoot, ".agents", "config.local.yaml"):           f.local,
	} {
		if got := string(readTestFile(t, path)); got != before {
			t.Fatalf("failed batch changed %s", path)
		}
	}
	assertFactorySkillAbsent(t, f.repoRoot, "good")
}

func (f batchFixture) requireInstalled(t *testing.T) {
	t.Helper()
	assertFactorySkillExists(t, f.repoRoot, "good")
	if got := string(readTestFile(t, filepath.Join(f.repoRoot, ".agents", "config.local.yaml"))); strings.Contains(got, "good") {
		t.Fatalf("control install left exclusion for good:\n%s", got)
	}
}

// spec: resource-installation/invalid-later-name-preserves-installation-state
func TestInstallMultipleInvalidLaterNamePublishesNothing(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, working := createBareRepoWithWorkingClone(t, "multi-invalid.git")
	writeSkill(t, working, "good", "Good", nil)
	writeSkill(t, working, "keep", "Keep", nil)
	writeSkill(t, working, "dup", "Duplicate skill", nil)
	writeCommand(t, working, "dup", "Duplicate command", "echo dup")
	writeFile(t, filepath.Join(working, "skills", "broken", "SKILL.md"), "---\nname: broken\ndescription: [unterminated\n---\n")
	commitAndPushAll(t, working, "add skills")

	for _, schema := range []string{"v2", "v4"} {
		for _, mode := range []string{"configured", "from"} {
			for _, tc := range []struct {
				name    string
				bad     string
				wantErr string
			}{
				{name: "missing", bad: "absent-skill", wantErr: "not found"},
				{name: "malformed", bad: "broken", wantErr: "cannot be installed"},
				{name: "ambiguous", bad: "dup", wantErr: "ambiguous"},
				{name: "empty", bad: "", wantErr: "must not be empty"},
				{name: "whitespace", bad: "   ", wantErr: "must not be empty"},
			} {
				t.Run(schema+"/"+mode+"/"+tc.name, func(t *testing.T) {
					setupMultiNameInstall(t)
					fixture := seedBatchFixture(t, schema, batchSourceURL(schema, origin))
					batchArgs := []string{"good", tc.bad}
					if mode == "from" {
						batchArgs = append(batchArgs, "--from", fixture.source, "--ref", "main")
					}
					commits := stubDependencyCommit(t, nil)

					output, err := executeCommand(append([]string{"install"}, batchArgs...)...)
					if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
						t.Fatalf("install good %q error = %v, output %q; want %q failure", tc.bad, err, output, tc.wantErr)
					}
					if *commits != 0 {
						t.Fatalf("failed batch published %d dependency transaction(s)", *commits)
					}
					fixture.requirePreserved(t)
					requireNoInstallStaging(t)

					control := []string{"good"}
					if mode == "from" {
						control = append(control, "--from", fixture.source, "--ref", "main")
					}
					if output, err := executeCommand(append([]string{"install"}, control...)...); err != nil {
						t.Fatalf("valid control install failed: %v\n%s", err, output)
					}
					fixture.requireInstalled(t)
				})
			}
		}
	}
}

// spec: resource-installation/invalid-later-name-preserves-installation-state
func TestInstallMultipleScopeIneligibleLaterNamePublishesNothing(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := setupMultiNameInstall(t)
	origin, working := createBareRepoWithWorkingClone(t, "multi-scope.git")
	writeSkill(t, working, "good", "Good", nil)
	commitAndPushAll(t, working, "add skills")

	repoRoot := multiNameRepo(t, `
registries:
  - url: `+origin+`
    ref: main
  - url: ./plugins/local
`)
	writeFile(t, filepath.Join(repoRoot, "plugins", "local", "skills", "local-skill", "SKILL.md"), "---\nname: local-skill\ndescription: Local\n---\n")

	userConfig := filepath.Join(home, ".agents", "config.yaml")
	userBefore, _ := os.ReadFile(userConfig)
	commits := stubDependencyCommit(t, nil)
	before := ownedCommandSnapshot(t, repoRoot)

	output, err := executeCommand("install", "--scope", "user", "good", "local-skill")
	if err == nil || !strings.Contains(err.Error(), "global config") {
		t.Fatalf("install --scope user good local-skill error = %v, output %q; want scope refusal", err, output)
	}
	if *commits != 0 {
		t.Fatalf("failed batch published %d dependency transaction(s)", *commits)
	}
	if after := ownedCommandSnapshot(t, repoRoot); !reflect.DeepEqual(before, after) {
		t.Fatal("scope-ineligible batch changed repository state")
	}
	if after, _ := os.ReadFile(userConfig); !reflect.DeepEqual(userBefore, after) {
		t.Fatal("scope-ineligible batch changed user config")
	}
	requireNoInstallStaging(t)
}

// spec: resource-installation/invalid-later-name-preserves-installation-state
func TestInstallMultipleInvalidNameLeavesMissingRootAbsent(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := setupMultiNameInstall(t)
	origin, working := createBareRepoWithWorkingClone(t, "multi-missing-root.git")
	writeSkill(t, working, "good", "Good", nil)
	writeSkill(t, working, "keep", "Keep", nil)
	commitAndPushAll(t, working, "add skills")

	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nharnesses: [factory]\nregistries:\n  - url: "+origin+"\n    ref: main\n")

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repo)
	if output, err := executeCommand("install", "--scope", "user", "keep"); err != nil {
		t.Fatalf("seed user-scope keep install: %v\n%s", err, output)
	}
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	userLock := filepath.Join(home, ".agents", "lock.yaml")
	userBefore := string(readTestFile(t, userConfig))
	userLockBefore := string(readTestFile(t, userLock))
	commits := stubDependencyCommit(t, nil)

	output, err := executeCommand("install", "good", "absent-skill")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("install good absent-skill error = %v, output %q; want not found", err, output)
	}
	if *commits != 0 {
		t.Fatalf("failed batch published %d dependency transaction(s)", *commits)
	}
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	if _, statErr := os.Stat(repoConfig); !os.IsNotExist(statErr) {
		t.Fatalf("failed batch created repo config: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("failed batch created repo lock: %v", statErr)
	}
	assertFactorySkillAbsent(t, repo, "good")
	if got := string(readTestFile(t, userConfig)); got != userBefore {
		t.Fatal("failed batch changed user config")
	}
	if got := string(readTestFile(t, userLock)); got != userLockBefore {
		t.Fatal("failed batch changed existing user lock")
	}
	requireNoInstallStaging(t)

	if output, err := executeCommand("install", "good"); err != nil {
		t.Fatalf("valid control install failed: %v\n%s", err, output)
	}
	assertFactorySkillExists(t, repo, "good")
}

// spec: resource-installation/install-several-names-from-configured-sources
func TestInstallMultipleStagesOnePublication(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := setupMultiNameInstall(t)
	origin, working := createBareRepoWithWorkingClone(t, "multi-staged.git")
	writeSkill(t, working, "alpha", "Alpha", nil)
	writeSkill(t, working, "beta", "Beta", nil)
	commitAndPushAll(t, working, "add skills")

	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nharnesses: [factory]\nregistries:\n  - url: "+origin+"\n    ref: main\n")

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repo)
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	canonicalRepoConfig, err := canonicalizeFuturePath(repoConfig)
	if err != nil {
		t.Fatal(err)
	}
	commits := stubDependencyCommit(t, func(desired []dependencytxn.DesiredFile) {
		for _, file := range desired {
			if file.Path != canonicalRepoConfig {
				continue
			}
			for _, needle := range []string{"skill/alpha", "skill/beta"} {
				if !strings.Contains(string(file.Bytes), needle) {
					t.Fatalf("staged repo config missing %s:\n%s", needle, file.Bytes)
				}
			}
			return
		}
		t.Fatalf("staged repo config missing from desired transaction: %#v", desired)
	})

	output, err := executeCommand("install", "alpha", "beta")
	if err != nil {
		t.Fatalf("install alpha beta: %v\n%s", err, output)
	}
	if *commits != 1 {
		t.Fatalf("batch performed %d dependency transactions, want 1", *commits)
	}
	configBytes, err := os.ReadFile(repoConfig)
	if err != nil {
		t.Fatalf("read repo config: %v", err)
	}
	for _, needle := range []string{"alpha", "beta"} {
		if !strings.Contains(string(configBytes), needle) {
			t.Fatalf("repo config missing %q:\n%s", needle, configBytes)
		}
	}
	requireNoInstallStaging(t)
}

// spec: resource-installation/incompatible-batch-arguments-are-rejected-before-writes
func TestInstallMultipleRejectsSpecialArgumentBatches(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	reject := []struct {
		name  string
		flags map[string]string
		args  []string
	}{
		{name: "locator last", args: []string{"plain", "pub:reg/skill/thing"}},
		{name: "locator first", args: []string{"pub:reg/skill/thing", "plain"}},
		{name: "browser locator", args: []string{"https://market.example/r/reg/skill/thing", "plain"}},
		{name: "endpoint url", args: []string{"plain", "https://market.example"}},
		{name: "from locator", flags: map[string]string{"from": "https://registry.example/repo.git"}, args: []string{"plain", "pub:reg/skill/thing"}},
		{name: "marketplace flag", flags: map[string]string{"marketplace": "public"}, args: []string{"pub:reg/skill/thing", "pub:reg/skill/other"}},
		{name: "as flag", flags: map[string]string{"as": "alias-name"}, args: []string{"a", "b"}},
		{name: "yes flag", flags: map[string]string{"yes": "true"}, args: []string{"a", "b"}},
		{name: "accept-risk flag", flags: map[string]string{"accept-risk": "true"}, args: []string{"a", "b"}},
		{name: "all with names", flags: map[string]string{"all": "true"}, args: []string{"a", "b"}},
		{name: "tag with names", flags: map[string]string{"tag": "security"}, args: []string{"a", "b"}},
	}
	for _, tc := range reject {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			cmd := newInstallCmd()
			for flag, value := range tc.flags {
				if err := cmd.Flags().Set(flag, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := cmd.Args(cmd, tc.args); err == nil {
				t.Fatalf("validation accepted %v", tc.args)
			}
		})
	}
	accept := []struct {
		name  string
		flags map[string]string
		args  []string
	}{
		{name: "two names", args: []string{"a", "b"}},
		{name: "from batch", flags: map[string]string{"from": "https://registry.example/repo.git"}, args: []string{"a", "b", "c"}},
		{name: "from no names", flags: map[string]string{"from": "https://registry.example/repo.git"}},
		{name: "single name", args: []string{"a"}},
	}
	for _, tc := range accept {
		t.Run("accept/"+tc.name, func(t *testing.T) {
			cmd := newInstallCmd()
			for flag, value := range tc.flags {
				if err := cmd.Flags().Set(flag, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := cmd.Args(cmd, tc.args); err != nil {
				t.Fatalf("validation rejected %v: %v", tc.args, err)
			}
		})
	}
}
