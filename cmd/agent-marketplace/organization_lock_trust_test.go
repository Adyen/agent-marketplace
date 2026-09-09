//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestSyncAllowsOrganizationRequirementsWithoutLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	testSyncAllowsOrganizationRequirementsWithoutLock(t, true, "sync")
}

func TestSyncLockedAllowsOrganizationRequirementsWithoutLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	testSyncAllowsOrganizationRequirementsWithoutLock(t, false, "sync", "--locked")
}

func testSyncAllowsOrganizationRequirementsWithoutLock(t *testing.T, withRepoRegistry bool, args ...string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "org-missing-lock.git")
	writeSkill(t, working, "required-org", "Required org", nil)
	commitAndPushAll(t, working, "required org")

	authority := canonicalTempDir(t)
	orgPath := filepath.Join(authority, "config.yaml")
	writeConfig(t, orgPath, "registries:\n  - url: "+origin+"\n    ref: main\n    required: true\n")
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)
	if withRepoRegistry {
		repo := filepath.Join(canonicalTempDir(t), "repo")
		writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "harnesses: [factory]\nregistries:\n  - url: "+origin+"\n    ref: main\n")
		withWorkingDir(t, repo)
	} else {
		writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "harnesses: [factory]\n")
		withWorkingDir(t, t.TempDir())
	}

	output, err := executeCommand(args...)
	if err != nil {
		t.Fatalf("%s rejected organization config without a lock: %v\n%s", strings.Join(args, " "), err, output)
	}
	if _, statErr := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("consumer created or replaced organization lock: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".factory", "skills", "required-org")); statErr != nil {
		t.Fatalf("organization requirement was not materialized: %v\n%s", statErr, output)
	}
}

func TestReadOnlyViewsDiscoverUnlockedOrganizationRegistry(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "org-unlocked-views.git")
	writeSkill(t, working, "required-org", "Required org", nil)
	commitAndPushAll(t, working, "required org")

	authority := canonicalTempDir(t)
	orgPath := filepath.Join(authority, "config.yaml")
	writeConfig(t, orgPath, "registries:\n  - url: "+origin+"\n    ref: main\n    required: true\n")
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "harnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())
	if _, err := cache.CloneOrPull(origin, "main"); err != nil {
		t.Fatalf("prime unlocked mutable cache: %v", err)
	}

	for _, command := range []string{"list", "status"} {
		output, err := executeCommand(command)
		if err != nil {
			t.Fatalf("%s rejected intentionally unlocked organization registry: %v\n%s", command, err, output)
		}
		if !strings.Contains(output, "required-org") {
			t.Fatalf("%s did not discover unlocked organization resource:\n%s", command, output)
		}
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("read-only view created organization lock: %v", err)
	}
}

func TestDeinitPreservesUnlockedOrganizationOutputAndCache(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "org-unlocked-deinit.git")
	writeSkill(t, working, "required-org", "Required org", nil)
	commitAndPushAll(t, working, "required org")

	authority := canonicalTempDir(t)
	orgPath := filepath.Join(authority, "config.yaml")
	writeConfig(t, orgPath, "registries:\n  - url: "+origin+"\n    ref: main\n    required: true\n")
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "harnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync unlocked organization output: %v\n%s", err, output)
	}
	outputPath := filepath.Join(home, ".factory", "skills", "required-org")
	before, err := os.Readlink(outputPath)
	if err != nil {
		t.Fatalf("read organization output link: %v", err)
	}
	if output, err := executeCommand("deinit", "--scope", "user"); err != nil {
		t.Fatalf("deinit with unlocked organization registry: %v\n%s", err, output)
	}
	after, err := os.Readlink(outputPath)
	if err != nil {
		t.Fatalf("organization output was not preserved: before=%q err=%v", before, err)
	}
	if _, err := os.Stat(after); err != nil {
		t.Fatalf("organization cache target was removed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("deinit created organization lock: %v", err)
	}
}

func TestDeinitBlocksOnPresentInvalidOrganizationLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	authority := canonicalTempDir(t)
	orgPath := filepath.Join(authority, "config.yaml")
	writeConfig(t, orgPath, "registries:\n  - url: https://example.invalid/org.git\n    ref: main\n    required: true\n")
	writeFile(t, filepath.Join(authority, "lock.yaml"), "not: [valid")
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "harnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("deinit", "--scope", "user")
	if err == nil {
		t.Fatalf("deinit accepted malformed organization lock:\n%s", output)
	}
	if !strings.Contains(err.Error(), "organization.trust.lock.malformed") {
		t.Fatalf("deinit error = %v, want organization.trust.lock.malformed", err)
	}
}

func TestExplicitOrganizationLockCreatesTrustedRegularFile(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "org-explicit-lock.git")
	writeSkill(t, working, "required-org", "Required org", nil)
	commitAndPushAll(t, working, "required org")

	authority := canonicalTempDir(t)
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
