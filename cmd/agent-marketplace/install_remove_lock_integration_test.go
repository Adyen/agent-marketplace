package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func TestRemoveFromV4PreservesMarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name, scope   string
		git, included bool
	}{
		{name: "user directory", scope: "user"},
		{name: "repo included relative directory", scope: "repo", included: true},
		{name: "repo git", scope: "repo", git: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("TMPDIR", t.TempDir())
			server := newV4MarketplaceLockServer(t)
			defer server.Close()
			repo := t.TempDir()
			root := repo
			if tc.scope == "user" {
				root = home
			}
			configPath := filepath.Join(root, ".agents", "config.yaml")
			lockPath := filepath.Join(root, ".agents", "lock.yaml")
			source := filepath.Join(root, "resources")
			working := source
			sourceYAML := "directory: " + source
			if tc.git {
				origin, clone := createBareRepoWithWorkingClone(t, "v4-remove.git")
				source, working = "file://"+origin, clone
				sourceYAML = "git: " + source + ", ref: main"
			} else if tc.included {
				source = filepath.Join(root, ".agents", "resources")
				working = source
				sourceYAML = "directory: resources"
			}
			writeSubagent(t, working, "scrutiny-feature-reviewer", "Review features", []string{"Read"})
			writeSubagent(t, working, "keep", "Keep sibling", []string{"Read"})
			if tc.git {
				commitAndPushAll(t, working, "seed subagents")
			}
			selection := "sources:\n  local: {" + sourceYAML + "}\nresources:\n  local: {include: [subagent/scrutiny-feature-reviewer, subagent/keep]}\n"
			body := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
			fragment := filepath.Join(root, ".agents", "selections.yaml")
			if tc.included {
				writeFile(t, fragment, selection)
				body += "include: [selections.yaml]\nsources:\n  hosted: {marketplace: " + server.URL + "}\nresources:\n"
			} else {
				body += "sources:\n  local: {" + sourceYAML + "}\n  hosted: {marketplace: " + server.URL + "}\nresources:\n  local: {include: [subagent/scrutiny-feature-reviewer, subagent/keep]}\n"
			}
			writeConfig(t, configPath, body+"  hosted: {include: [tools/skill/review]}\n")
			withWorkingDir(t, repo)
			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("seed sync: %v\n%s", err, output)
			}
			target := filepath.Join(root, ".factory", "droids", "scrutiny-feature-reviewer.md")
			assertFileExists(t, target)
			preserved := make(map[string][]byte)
			paths := []string{lockPath, filepath.Join(root, ".factory", "droids", "keep.md"), filepath.Join(root, ".factory", "skills", "review", "SKILL.md")}
			if tc.included {
				paths = append(paths, fragment)
			}
			for _, path := range paths {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				preserved[path] = data
			}
			if err := os.Chmod(lockPath, 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"remove", "--kind", "subagent", "--scope", tc.scope, "--from", source, "scrutiny-feature-reviewer"}
			if output, err := executeCommand(args...); err != nil {
				t.Fatalf("remove: %v\n%s", err, output)
			}
			if _, err := os.Lstat(target); !os.IsNotExist(err) {
				t.Fatalf("removed subagent remains: %v", err)
			}
			for path, before := range preserved {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Errorf("remove changed %s: %v", path, err)
				}
			}
			if info, err := os.Stat(lockPath); err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("lock permissions changed: %v", err)
			}
			if residue, err := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), ".agent-marketplace-config-*")); err != nil || len(residue) != 0 {
				t.Fatalf("staging residue = %v err=%v", residue, err)
			}
		})
	}
}

func TestInstallAndRemoveReconcileOwningGitLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	origin, working := createBareRepoWithWorkingClone(t, "install-remove-lock.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")

	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "\nharnesses: [claude]\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("install", "review", "--from", origin, "--ref", "main"); err != nil {
		t.Fatalf("install: %v\n%s", err, output)
	}

	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	doc := readExactManifestLock(t, lockPath)
	if len(doc.Registries) != 1 || len(doc.Registries[0].Roots) != 1 || len(doc.Registries[0].Roots[0].Resources) != 1 {
		t.Fatalf("install lock inventory = %+v", doc.Registries)
	}
	if got := doc.Registries[0].Roots[0].Resources[0].Name; got != "review" {
		t.Fatalf("locked resource = %q, want review", got)
	}

	if output, err := executeCommand("remove", "--from", origin, "--ref", "main"); err != nil {
		t.Fatalf("remove registry: %v\n%s", err, output)
	}
	doc = readExactManifestLock(t, lockPath)
	if len(doc.Registries) != 0 {
		t.Fatalf("orphan Git lock retained after remove: %+v", doc.Registries)
	}
}

func TestConfiguredInstallUsesLockedInventoryWhenMutableRefMoves(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "configured-install-lock.git")
	writeSkill(t, working, "locked", "Locked A", nil)
	commitAndPushAll(t, working, "inventory A")

	repo := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "\nharnesses: [claude]\nregistries:\n  - url: "+origin+"\n    ref: main\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync: %v\n%s", err, output)
	}
	beforeConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeLock, err := os.ReadFile(filepath.Join(repo, ".agents", "lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	writeSkill(t, working, "new", "New B", nil)
	commitAndPushAll(t, working, "inventory B")
	output, err := executeCommand("install", "new")
	if err == nil {
		t.Fatalf("configured install reported success for resource absent from locked inventory:\n%s", output)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("configured install error = %v, want a safe not-found result", err)
	}
	afterConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterConfig) != string(beforeConfig) {
		t.Fatalf("failed install changed config:\nbefore:\n%s\nafter:\n%s", beforeConfig, afterConfig)
	}
	afterLock, err := os.ReadFile(filepath.Join(repo, ".agents", "lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(afterLock) != string(beforeLock) {
		t.Fatalf("failed install changed lock:\nbefore:\n%s\nafter:\n%s", beforeLock, afterLock)
	}
}

func TestAdHocInstallUsesLockedInventoryWhenMutableRefMoves(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("TMPDIR", t.TempDir())
	origin, working := createBareRepoWithWorkingClone(t, "adhoc-install-lock.git")
	writeSkill(t, working, "locked", "Locked A", nil)
	commitAndPushAll(t, working, "inventory A")
	repo := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "\nharnesses: [claude]\nregistries:\n  - url: "+origin+"\n    ref: main\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	beforeConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	writeSkill(t, working, "new", "New B", nil)
	commitAndPushAll(t, working, "inventory B")
	output, err := executeCommand("install", "new", "--from", origin, "--ref", "main")
	if err == nil {
		t.Fatalf("ad-hoc install reported success for mutable-only resource:\n%s", output)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("ad-hoc install error = %v, want safe not-found result", err)
	}
	afterConfig, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(beforeConfig, afterConfig) {
		t.Fatalf("failed install changed config: %v", err)
	}
	afterLock, err := os.ReadFile(lockPath)
	if err != nil || !bytes.Equal(beforeLock, afterLock) {
		t.Fatalf("failed install changed lock: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".claude", "skills", "new")); !os.IsNotExist(err) {
		t.Fatalf("new output exists: %v", err)
	}
	if residue, err := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), ".agent-marketplace-config-*")); err != nil || len(residue) != 0 {
		t.Fatalf("staging residue = %v err=%v", residue, err)
	}
}

func TestRemoveUsesLockedInventoryWhenMutableRefMoves(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, from := range []bool{false, true} {
		name := "ordinary"
		if from {
			name = "from"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			stagingRoot := t.TempDir()
			t.Setenv("TMPDIR", stagingRoot)
			origin, working := createBareRepoWithWorkingClone(t, "remove-moved-ref.git")
			writeCommand(t, working, "review", "Review A", "echo review")
			writeCommand(t, working, "keep", "Keep A", "echo keep")
			commitAndPushAll(t, working, "inventory A")

			repo := filepath.Join(t.TempDir(), "repo")
			configPath := filepath.Join(repo, ".agents", "config.yaml")
			writeConfig(t, configPath, "\nharnesses: [factory]\nregistries:\n  - url: "+origin+"\n    ref: main\n    commands: [review, keep]\n")
			userPath := filepath.Join(home, ".agents", "config.yaml")
			writeConfig(t, userPath, "\nharnesses: [factory]\n")
			withWorkingDir(t, repo)
			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("seed sync: %v\n%s", err, output)
			}
			reviewPath := filepath.Join(repo, ".factory", "commands", "review.md")
			assertFileExists(t, reviewPath)
			preserved := make(map[string][]byte)
			for _, path := range []string{userPath, filepath.Join(repo, ".agents", "lock.yaml"), filepath.Join(repo, ".factory", "commands", "keep.md")} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				preserved[path] = data
			}

			if err := os.Remove(filepath.Join(working, "commands", "review.md")); err != nil {
				t.Fatal(err)
			}
			writeCommand(t, working, "keep", "Keep B", "echo changed")
			commitAndPushAll(t, working, "inventory B")
			args := []string{"remove", "review"}
			if from {
				args = append(args, "--from", origin, "--ref", "main")
			}
			if output, err := executeCommand(args...); err != nil {
				t.Fatalf("remove locked command: %v\n%s", err, output)
			}
			cfg, err := config.LoadConfigFromPath(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if cfg == nil || len(cfg.Registries) != 1 || len(cfg.Registries[0].Commands.Include) != 1 || cfg.Registries[0].Commands.Include[0] != "keep" {
				t.Fatalf("remove did not retain only the keep command: %+v", cfg)
			}
			if len(cfg.Registries[0].Skills.Exclude) != 0 {
				t.Fatalf("remove wrote a skill exclusion for a command: %+v", cfg.Registries[0].Skills)
			}
			if _, err := os.Lstat(reviewPath); !os.IsNotExist(err) {
				t.Fatalf("removed command still materialized: %v", err)
			}
			for path, before := range preserved {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Errorf("remove changed %s: %v\nbefore:\n%s\nafter:\n%s", path, err, before, after)
				}
			}
			for _, path := range []string{config.LocalConfigPath(repo), filepath.Join(repo, ".agents", "lock.local.yaml")} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Errorf("remove unexpectedly created %s: %v", path, err)
				}
			}
			if residue, err := filepath.Glob(filepath.Join(stagingRoot, ".agent-marketplace-config-*")); err != nil || len(residue) != 0 {
				t.Errorf("config staging residue = %v, %v", residue, err)
			}
		})
	}
}

func TestInstallMaterializationFailureKeepsCommittedConfigAndLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	origin, working := createBareRepoWithWorkingClone(t, "committed-sync-failure.git")
	writeUndocumentedTransportMCP(t, working, "review")
	commitAndPushAll(t, working, "review")

	repo := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "\nharnesses: [devin]\n")
	withWorkingDir(t, repo)
	_, err := executeCommand("install", "review", "--kind", "mcp", "--from", origin, "--ref", "main")
	var committed *pipeline.DependencyCommittedSyncError
	if !errors.As(err, &committed) {
		t.Fatalf("install error = %v, want DependencyCommittedSyncError", err)
	}
	configBytes, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Contains(configBytes, []byte("review")) {
		t.Fatalf("committed requirement was rolled back:\n%s", configBytes)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); statErr != nil {
		t.Fatalf("committed lock missing after materialization failure: %v", statErr)
	}
}

func TestInstallFromV4UserRefPathUsesCanonicalEditor(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	originPath, working := createBareRepoWithWorkingCloneOnBranch(t, "user-v4-skills.git", "add-skill-daily-sync")
	writeSkill(t, filepath.Join(working, "teams", "unified_platform", "plugins", "identity_platform", "skills", "daily-sync"), "daily-sync", "Daily sync", []string{"sync"})
	commitAndPushAllToBranch(t, working, "Add daily-sync skill", "add-skill-daily-sync")
	origin := "file://" + originPath

	configPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "registries: []\n")
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "daily-sync", "--from", origin, "--ref", "add-skill-daily-sync", "--path", "teams/unified_platform/plugins/identity_platform/skills/daily-sync", "--scope", "user")
	if err != nil {
		t.Fatalf("install --from failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"apiVersion: agent-marketplace/v4", "git:", "ref: add-skill-daily-sync", "path: teams/unified_platform/plugins/identity_platform/skills/daily-sync", "skill/daily-sync"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("user config missing %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), "registries:") {
		t.Fatalf("v4 config gained legacy registries:\n%s", data)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o", info.Mode().Perm())
	}

	lockData, err := lockfile.ReadBytes(filepath.Join(home, ".agents", "lock.yaml"))
	if err != nil {
		t.Fatalf("user lock: %v", err)
	}
	lock, err := lockfile.Decode(lockData)
	if err != nil {
		t.Fatalf("user lock decode: %v", err)
	}
	if lock.APIVersion != lockfile.APIVersionV2 || lock.Scope != lockfile.ScopeUser {
		t.Fatalf("lock = %v %v", lock.APIVersion, lock.Scope)
	}
	if len(lock.Registries) != 1 || lock.Registries[0].Ref != "add-skill-daily-sync" || !strings.Contains(lock.Registries[0].URL, "user-v4-skills") {
		t.Fatalf("lock registries = %#v", lock.Registries)
	}
	if len(lock.Registries[0].Roots) != 1 || lock.Registries[0].Roots[0].Path != "teams/unified_platform/plugins/identity_platform/skills/daily-sync" {
		t.Fatalf("lock roots = %#v", lock.Registries[0].Roots)
	}

	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "daily-sync", "SKILL.md")); err != nil {
		t.Fatalf("machine factory skill missing: %v", err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".agent-marketplace-config-") {
			t.Fatalf("staging residue %s", entry.Name())
		}
	}
}

func TestConfiguredInstallV4UsesCanonicalEditor(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name         string
		args         func(origin string) []string
		wantLocators []string
		wantTags     []string
		materialized []string
	}{
		{
			name:         "named skill",
			args:         func(origin string) []string { return []string{"install", "review"} },
			wantLocators: []string{"skill/review"},
			materialized: []string{filepath.Join(".factory", "skills", "review", "SKILL.md")},
		},
		{
			name:         "named collection",
			args:         func(origin string) []string { return []string{"install", "platform-bundle"} },
			wantLocators: []string{"collection/platform-bundle"},
			materialized: []string{filepath.Join(".factory", "skills", "review", "SKILL.md")},
		},
		{
			name:         "tag",
			args:         func(origin string) []string { return []string{"install", "--tag", "platform"} },
			wantTags:     []string{"platform"},
			materialized: []string{filepath.Join(".factory", "skills", "review", "SKILL.md")},
		},
		{
			name:         "tag with kind",
			args:         func(origin string) []string { return []string{"install", "--tag", "platform", "--kind", "command"} },
			wantLocators: []string{"command/audit"},
			materialized: []string{filepath.Join(".factory", "commands", "audit.md")},
		},
		{
			name: "all kind registry",
			args: func(origin string) []string {
				return []string{"install", "--all", "--kind", "subagent", "--registry", origin}
			},
			wantLocators: []string{"subagent/gatekeeper"},
			materialized: []string{filepath.Join(".factory", "droids", "gatekeeper.md")},
		},
		{
			name:         "all",
			args:         func(origin string) []string { return []string{"install", "--all"} },
			wantLocators: []string{"collection/platform-bundle", "command/audit", "skill/review", "subagent/gatekeeper"},
			materialized: []string{
				filepath.Join(".factory", "skills", "review", "SKILL.md"),
				filepath.Join(".factory", "commands", "audit.md"),
				filepath.Join(".factory", "droids", "gatekeeper.md"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)

			originPath, working := createBareRepoWithWorkingClone(t, "configured-v4.git")
			writeSkill(t, working, "review", "Review", []string{"platform"})
			writeSubagent(t, working, "gatekeeper", "Gatekeeper", []string{"Read"})
			writeFile(t, filepath.Join(working, "commands", "audit.md"), "---\nname: audit\ndescription: Audit\ncommand: echo audit\nmetadata:\n  tags:\n    - platform\n---\n\n# audit\n")
			writeCollection(t, working, "platform-bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: platform-bundle
version: 1.0.0
skills:
  - name: review
`)
			commitAndPushAll(t, working, "seed inventory")
			origin := "file://" + originPath

			repoRoot := filepath.Join(t.TempDir(), "repo")
			configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
			writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {git: "+origin+", ref: main}\n")
			withWorkingDir(t, repoRoot)

			output, err := executeCommand(tc.args(origin)...)
			if err != nil {
				t.Fatalf("install failed: %v\n%s", err, output)
			}
			raw, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "registries:") {
				t.Fatalf("v4 config gained legacy registries:\n%s", raw)
			}
			cfg, err := config.LoadV4ConfigFromPath(configPath)
			if err != nil {
				t.Fatalf("load v4 config: %v", err)
			}
			if len(cfg.Sources) != 1 || cfg.Sources["hosted"].Kind != config.V4SourceGit {
				t.Fatalf("sources = %#v", cfg.Sources)
			}
			if len(cfg.Groups) != 1 {
				t.Fatalf("expected one resource group: %#v", cfg.Groups)
			}
			var group config.V4ResourceGroup
			for _, g := range cfg.Groups {
				group = g
			}
			var locators []string
			for _, req := range group.Include {
				locators = append(locators, req.Locator)
			}
			slices.Sort(locators)
			want := slices.Clone(tc.wantLocators)
			slices.Sort(want)
			if !slices.Equal(locators, want) {
				t.Fatalf("group locators = %v, want %v\n%s", locators, want, raw)
			}
			gotTags := slices.Clone(group.Tags)
			slices.Sort(gotTags)
			wantTags := slices.Clone(tc.wantTags)
			slices.Sort(wantTags)
			if !slices.Equal(gotTags, wantTags) {
				t.Fatalf("group tags = %v, want %v\n%s", gotTags, wantTags, raw)
			}
			if group.All != nil && *group.All {
				t.Fatalf("configured install wrote all:true:\n%s", raw)
			}
			lock := readExactManifestLock(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))
			if lock.APIVersion != lockfile.APIVersionV2 || lock.Scope != lockfile.ScopeRepo {
				t.Fatalf("lock = %v %v", lock.APIVersion, lock.Scope)
			}
			for _, rel := range tc.materialized {
				assertFileExists(t, filepath.Join(repoRoot, rel))
			}
			if residue, err := filepath.Glob(filepath.Join(tmp, ".agent-marketplace-config-*")); err != nil || len(residue) != 0 {
				t.Fatalf("staging residue = %v err=%v", residue, err)
			}
		})
	}
}

func TestConfiguredInstallV4AllWritesLocatorsUnderEachProvider(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("TMPDIR", t.TempDir())

	skillsOrigin, skillsWorking := createBareRepoWithWorkingClone(t, "provider-skills.git")
	writeSkill(t, skillsWorking, "alpha", "Alpha", nil)
	commitAndPushAll(t, skillsWorking, "alpha")
	toolsOrigin, toolsWorking := createBareRepoWithWorkingClone(t, "provider-tools.git")
	writeCommand(t, toolsWorking, "beta", "Beta", "echo beta")
	commitAndPushAll(t, toolsWorking, "beta")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  alpha-src: {git: file://"+skillsOrigin+", ref: main}\n  beta-src: {git: file://"+toolsOrigin+", ref: main}\n")
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--all")
	if err != nil {
		t.Fatalf("install --all failed: %v\n%s", err, output)
	}
	cfg, err := config.LoadV4ConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load v4 config: %v", err)
	}
	locators := func(source string) []string {
		group, ok := cfg.Groups[source]
		if !ok {
			t.Fatalf("missing resource group for %s: %#v", source, cfg.Groups)
		}
		var out []string
		for _, req := range group.Include {
			out = append(out, req.Locator)
		}
		slices.Sort(out)
		return out
	}
	if got := locators("alpha-src"); !slices.Equal(got, []string{"skill/alpha"}) {
		t.Fatalf("alpha-src locators = %v", got)
	}
	if got := locators("beta-src"); !slices.Equal(got, []string{"command/beta"}) {
		t.Fatalf("beta-src locators = %v", got)
	}
	assertFileExists(t, filepath.Join(repoRoot, ".factory", "skills", "alpha", "SKILL.md"))
	assertFileExists(t, filepath.Join(repoRoot, ".factory", "commands", "beta.md"))
}

func TestInstallFromV4AdHocCollectionAndBundle(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "named collection", args: []string{"platform-bundle", "--from", "%s", "--ref", "main"}, want: "collection/platform-bundle"},
		{name: "bundle", args: []string{"--from", "%s", "--ref", "main"}, want: "all: true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("TMPDIR", t.TempDir())

			originPath, working := createBareRepoWithWorkingClone(t, "v4-adhoc.git")
			writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"infra"})
			writeCollection(t, working, "platform-bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: platform-bundle
version: 1.0.0
skills:
  - name: vm-lifecycle
`)
			commitAndPushAll(t, working, "Add skill and collection")
			origin := "file://" + originPath

			repoRoot := filepath.Join(t.TempDir(), "repo")
			configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
			if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"), 0o640); err != nil {
				t.Fatal(err)
			}
			withWorkingDir(t, repoRoot)

			args := []string{"install"}
			for _, arg := range tc.args {
				if arg == "%s" {
					arg = origin
				}
				args = append(args, arg)
			}
			output, err := executeCommand(args...)
			if err != nil {
				t.Fatalf("install failed: %v\n%s", err, output)
			}
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "apiVersion: agent-marketplace/v4") || strings.Contains(string(data), "registries:") {
				t.Fatalf("schema drifted:\n%s", data)
			}
			if !strings.Contains(string(data), tc.want) {
				t.Fatalf("config missing %q:\n%s", tc.want, data)
			}
			assertFactorySkillExists(t, repoRoot, "vm-lifecycle")
		})
	}
}
