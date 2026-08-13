package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestUpdateCommandRefreshesPinnedRefsAndSyncsWithoutChangingConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v1", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills v1")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      include:
        - vm-lifecycle
`)

	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected initial sync to succeed: %v\n%s", err, output)
	}

	beforeConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable before update: %v", err)
	}

	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2", []string{"openstack"})
	commitAndPushAll(t, working, "Update infra skills")

	output, err := executeCommand("update")
	if err != nil {
		t.Fatalf("expected update command to succeed: %v\n%s", err, output)
	}

	afterConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable after update: %v", err)
	}

	if string(afterConfig) != string(beforeConfig) {
		t.Fatalf("expected update without --bump to keep config unchanged\nbefore:\n%s\nafter:\n%s", beforeConfig, afterConfig)
	}

	assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", "vm-lifecycle"), filepath.Join(cache.RegistryCachePath(origin, "main"), "skills", "vm-lifecycle"))

	updatedSkill, err := os.ReadFile(filepath.Join(cache.RegistryCachePath(origin, "main"), "skills", "vm-lifecycle", "SKILL.md"))
	if err != nil {
		t.Fatalf("expected refreshed cache skill to be readable: %v", err)
	}

	if !strings.Contains(string(updatedSkill), "VM lifecycle v2") {
		t.Fatalf("expected refreshed cache to include updated skill content, got:\n%s", updatedSkill)
	}
}

func TestUpdateCommandWithBumpUpdatesRepoConfigRefAndSyncsNewCache(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v1.0", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills v1.0")
	createAndPushTag(t, working, "v1.0")

	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v1.1", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills v1.1")
	createAndPushTag(t, working, "v1.1")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: v1.0
    skills:
      include:
        - vm-lifecycle
`)

	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected initial sync to succeed: %v\n%s", err, output)
	}

	assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", "vm-lifecycle"), filepath.Join(cache.RegistryCachePath(origin, "v1.0"), "skills", "vm-lifecycle"))

	output, err := executeCommand("update", "--bump")
	if err != nil {
		t.Fatalf("expected update --bump to succeed: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected updated repo config to load: %v", err)
	}

	if got, want := cfg.Registries[0].Ref, "v1.1"; got != want {
		t.Fatalf("expected bumped ref %q, got %q", want, got)
	}

	assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", "vm-lifecycle"), filepath.Join(cache.RegistryCachePath(origin, "v1.1"), "skills", "vm-lifecycle"))

	bumpedSkill, err := os.ReadFile(filepath.Join(cache.RegistryCachePath(origin, "v1.1"), "skills", "vm-lifecycle", "SKILL.md"))
	if err != nil {
		t.Fatalf("expected bumped cache skill to be readable: %v", err)
	}

	if !strings.Contains(string(bumpedSkill), "VM lifecycle v1.1") {
		t.Fatalf("expected bumped cache to include v1.1 content, got:\n%s", bumpedSkill)
	}
}

func TestUpdateBumpWithSimpleTrailingInteger(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v1", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills v1")
	createAndPushTag(t, working, "v1")

	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills v2")
	createAndPushTag(t, working, "v2")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: v1
    skills:
      include:
        - vm-lifecycle
`)

	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected initial sync to succeed: %v\n%s", err, output)
	}

	output, err := executeCommand("update", "--bump")
	if err != nil {
		t.Fatalf("expected update --bump to succeed: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected updated repo config to load: %v", err)
	}

	if got, want := cfg.Registries[0].Ref, "v2"; got != want {
		t.Fatalf("expected bumped ref %q, got %q", want, got)
	}
}

func TestUpdateBumpWithThreeSegmentSemver(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v1.0.0", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills v1.0.0")
	createAndPushTag(t, working, "v1.0.0")

	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v1.0.1", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills v1.0.1")
	createAndPushTag(t, working, "v1.0.1")

	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v1.1.0", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills v1.1.0")
	createAndPushTag(t, working, "v1.1.0")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: v1.0.0
    skills:
      include:
        - vm-lifecycle
`)

	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected initial sync to succeed: %v\n%s", err, output)
	}

	output, err := executeCommand("update", "--bump")
	if err != nil {
		t.Fatalf("expected update --bump to succeed: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected updated repo config to load: %v", err)
	}

	if got, want := cfg.Registries[0].Ref, "v1.0.1"; got != want {
		t.Fatalf("expected bumped ref %q, got %q (bump should stay within v1.0.* family)", want, got)
	}
}

func TestUpdateBumpDoesNotCrossMajorVersion(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v1.0", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills v1.0")
	createAndPushTag(t, working, "v1.0")

	writeSkill(t, working, "vm-lifecycle", "VM lifecycle v2.0", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills v2.0")
	createAndPushTag(t, working, "v2.0")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: v1.0
    skills:
      include:
        - vm-lifecycle
`)

	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected initial sync to succeed: %v\n%s", err, output)
	}

	beforeConfig, _ := os.ReadFile(configPath)

	output, err := executeCommand("update", "--bump")
	if err != nil {
		t.Fatalf("expected update --bump to succeed: %v\n%s", err, output)
	}

	afterConfig, _ := os.ReadFile(configPath)

	if string(afterConfig) != string(beforeConfig) {
		t.Fatalf("expected --bump not to cross major versions (v1.0 should stay, not jump to v2.0)\nbefore:\n%s\nafter:\n%s", beforeConfig, afterConfig)
	}
}

func TestUpdateBumpWithNonVersionRefDoesNothing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      include:
        - vm-lifecycle
`)

	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected initial sync to succeed: %v\n%s", err, output)
	}

	beforeConfig, _ := os.ReadFile(configPath)

	output, err := executeCommand("update", "--bump")
	if err != nil {
		t.Fatalf("expected update --bump to succeed: %v\n%s", err, output)
	}

	afterConfig, _ := os.ReadFile(configPath)

	if string(afterConfig) != string(beforeConfig) {
		t.Fatalf("expected --bump on non-version ref to keep config unchanged\nbefore:\n%s\nafter:\n%s", beforeConfig, afterConfig)
	}
}

func TestUpdateCommandBlockedRegistryReturnsError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
allowed_registries: []
registries:
  - url: https://example.com/blocked.git
    ref: main
`)

	withWorkingDir(t, repoRoot)

	_, err := executeCommand("update")
	if err == nil {
		t.Fatal("expected update command to fail for blocked registry")
	}
	if !strings.Contains(err.Error(), "blocked by allowed_registries") {
		t.Fatalf("expected error to contain 'blocked by allowed_registries', got: %v", err)
	}
}

func createAndPushTag(t *testing.T, repoDir, tag string) {
	t.Helper()

	runGit(t, repoDir, "tag", tag)
	runGit(t, repoDir, "push", "origin", tag)
}
