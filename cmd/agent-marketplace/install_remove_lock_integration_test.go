package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
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
