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
	beforeLock := readLockDocument(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))

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

	afterLock := readLockDocument(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))
	if len(beforeLock.Registries) != 1 || len(afterLock.Registries) != 1 || beforeLock.Registries[0].Commit == afterLock.Registries[0].Commit {
		t.Fatalf("update did not advance exact lock: before=%+v after=%+v", beforeLock.Registries, afterLock.Registries)
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

	originalConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	originalCommit := commitDependencyTransaction
	transactionObserved := false
	commitDependencyTransaction = func(ctx context.Context, stateDir string, desired []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
		current, readErr := os.ReadFile(configPath)
		if readErr != nil || !bytes.Equal(current, originalConfig) {
			t.Fatalf("repo config changed before dependency transaction: %q, %v", current, readErr)
		}
		foundConfig, foundLock := false, false
		for _, file := range desired {
			foundConfig = foundConfig || filepath.Base(file.Path) == "config.yaml" && bytes.Contains(file.Bytes, []byte("ref: v1.1"))
			foundLock = foundLock || filepath.Base(file.Path) == "lock.yaml" && bytes.Contains(file.Bytes, []byte("ref: v1.1"))
		}
		transactionObserved = foundConfig && foundLock
		return originalCommit(ctx, stateDir, desired, opts...)
	}
	t.Cleanup(func() { commitDependencyTransaction = originalCommit })

	output, err := executeCommand("update", "--bump")
	if err != nil {
		t.Fatalf("expected update --bump to succeed: %v\n%s", err, output)
	}
	if !transactionObserved {
		t.Fatal("update --bump did not commit config and lock in one transaction")
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected updated repo config to load: %v", err)
	}

	if got, want := cfg.Registries[0].Ref, "v1.1"; got != want {
		t.Fatalf("expected bumped ref %q, got %q", want, got)
	}

	locked := readLockDocument(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))
	if len(locked.Registries) != 1 || locked.Registries[0].Ref != "v1.1" {
		t.Fatalf("bumped lock = %+v, want ref v1.1", locked.Registries)
	}

}

func TestTargetedUpdateDoesNotMaterializeUnrelatedMovedBranch(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	targetOrigin, targetWorking := createBareRepoWithWorkingClone(t, "target-skills.git")
	writeSkill(t, targetWorking, "target", "Target v1", nil)
	commitAndPushAll(t, targetWorking, "target v1")
	unrelatedOrigin, unrelatedWorking := createBareRepoWithWorkingClone(t, "unrelated-skills.git")
	writeSkill(t, unrelatedWorking, "unrelated", "Unrelated v1", nil)
	commitAndPushAll(t, unrelatedWorking, "unrelated v1")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+targetOrigin+`
    ref: main
    skills:
      include: [target]
  - url: `+unrelatedOrigin+`
    ref: main
    skills:
      include: [unrelated]
`)
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	before := readLockDocument(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))
	canonicalTarget, err := canonicalRegistryURL(targetOrigin)
	if err != nil {
		t.Fatal(err)
	}
	canonicalUnrelated, err := canonicalRegistryURL(unrelatedOrigin)
	if err != nil {
		t.Fatal(err)
	}
	var targetCommit, unrelatedCommit lockfile.GitOID
	for _, registry := range before.Registries {
		switch registry.URL {
		case canonicalTarget:
			targetCommit = registry.Commit
		case canonicalUnrelated:
			unrelatedCommit = registry.Commit
		}
	}
	if targetCommit == (lockfile.GitOID{}) || unrelatedCommit == (lockfile.GitOID{}) {
		t.Fatalf("initial lock missing registry: %+v", before.Registries)
	}

	writeSkill(t, targetWorking, "target", "Target v2", nil)
	commitAndPushAll(t, targetWorking, "target v2")
	writeSkill(t, unrelatedWorking, "target", "Transient collision", nil)
	commitAndPushAll(t, unrelatedWorking, "unrelated moved branch")

	output, err := executeCommand("update", "--registry", targetOrigin)
	if err != nil {
		t.Fatalf("targeted update loaded unrelated moved branch: %v\n%s", err, output)
	}
	after := readLockDocument(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))
	for _, registry := range after.Registries {
		switch registry.URL {
		case canonicalTarget:
			if registry.Commit == targetCommit {
				t.Fatalf("selected lock did not advance from %v", targetCommit)
			}
		case canonicalUnrelated:
			if registry.Commit != unrelatedCommit {
				t.Fatalf("unrelated lock moved from %v to %v", unrelatedCommit, registry.Commit)
			}
		}
	}
}

func TestScopedUpdateReconcilesMissingUnselectedWritableLayer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	userOrigin, userWorking := createBareRepoWithWorkingClone(t, "user-missing-lock.git")
	repoOrigin, repoWorking := createBareRepoWithWorkingClone(t, "repo-update.git")
	writeSkill(t, userWorking, "user-skill", "User v1", nil)
	writeSkill(t, repoWorking, "repo-skill", "Repo v1", nil)
	commitAndPushAll(t, userWorking, "user v1")
	commitAndPushAll(t, repoWorking, "repo v1")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nregistries:\n  - url: "+userOrigin+"\n    ref: main\n")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "\nregistries:\n  - url: "+repoOrigin+"\n    ref: main\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	userLockPath := filepath.Join(home, ".agents", "lock.yaml")
	repoLockPath := filepath.Join(repo, ".agents", "lock.yaml")
	repoBefore := readLockDocument(t, repoLockPath).Registries[0].Commit
	if err := os.Remove(userLockPath); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, repoWorking, "repo-skill", "Repo v2", nil)
	commitAndPushAll(t, repoWorking, "repo v2")

	output, err := executeCommand("update", "--scope", "repo")
	if err != nil {
		t.Fatalf("scoped update: %v\n%s", err, output)
	}
	userLock := readLockDocument(t, userLockPath)
	if len(userLock.Registries) != 1 {
		t.Fatalf("missing unselected user lock was not reconciled: %+v", userLock)
	}
	if got := readLockDocument(t, repoLockPath).Registries[0].Commit; got == repoBefore {
		t.Fatal("selected repo lock did not advance")
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
