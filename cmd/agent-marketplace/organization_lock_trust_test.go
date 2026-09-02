//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestSyncBlocksOrganizationRequirementsWithoutTrustedLock(t *testing.T) {
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
	config.SetTestOrganizationConfigPath(t, orgPath)
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "harnesses: [factory]\nregistries: []\n")
	withWorkingDir(t, repo)

	output, err := executeCommand("sync")
	if err == nil || !strings.Contains(err.Error(), "organization.trust.lock.missing") {
		t.Fatalf("sync did not fail closed for missing organization lock: err=%v\n%s", err, output)
	}
	if _, statErr := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("consumer created or replaced organization lock: %v", statErr)
	}
	if _, statErr := os.Lstat(filepath.Join(repo, ".factory", "skills", "required-org")); !os.IsNotExist(statErr) {
		t.Fatalf("consumer materialized blocked organization policy: %v", statErr)
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
