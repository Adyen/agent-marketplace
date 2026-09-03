//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestSyncAllowsOrganizationRequirementsWithoutLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "org-missing-lock.git")
	writeSkill(t, working, "required-org", "Required org", nil)
	commitAndPushAll(t, working, "required org")

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeConfig(t, orgPath, "registries:\n  - url: "+origin+"\n    ref: main\n    required: true\n")
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "harnesses: [factory]\nregistries:\n  - url: "+origin+"\n    ref: main\n")
	withWorkingDir(t, repo)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync rejected organization config without a lock: %v\n%s", err, output)
	}
	if _, statErr := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("consumer created or replaced organization lock: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".factory", "skills", "required-org")); statErr != nil {
		t.Fatalf("organization requirement was not materialized: %v\n%s", statErr, output)
	}
}

func TestExplicitOrganizationLockCreatesTrustedRegularFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "org-explicit-lock.git")
	writeSkill(t, working, "required-org", "Required org", nil)
	commitAndPushAll(t, working, "required org")

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeConfig(t, orgPath, "registries:\n  - url: "+origin+"\n    ref: main\n    required: true\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("lock", "--scope", "organization"); err != nil {
		t.Fatalf("explicit organization lock failed: %v\n%s", err, output)
	}
	lockPath := filepath.Join(authority, "lock.yaml")
	info, err := os.Lstat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		t.Fatalf("resulting lock is not trusted regular file: mode=%v", info.Mode())
	}
	if err := config.ValidateOrganizationTrustUnit(orgPath, true, false); err != nil {
		t.Fatalf("resulting trust unit failed verification: %v", err)
	}
}
