package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func TestInstallAndRemoveReconcileOwningGitLock(t *testing.T) {
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

func TestInstallMaterializationFailureKeepsCommittedConfigAndLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	origin, working := createBareRepoWithWorkingClone(t, "committed-sync-failure.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")

	repo := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "\nharnesses: [devin]\nmaterialization:\n  mode: file\n")
	withWorkingDir(t, repo)
	_, err := executeCommand("install", "review", "--from", origin, "--ref", "main")
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
