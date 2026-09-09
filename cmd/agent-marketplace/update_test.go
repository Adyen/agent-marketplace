package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func scenarioUpdateCommandRefreshesPinnedRefsAndSyncsWithoutChangingConfig(t *testing.T) {
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

func scenarioUpdateCommandWithBumpUpdatesRepoConfigRefAndSyncsNewCache(t *testing.T) {
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

func scenarioTargetedUpdateDoesNotMaterializeUnrelatedMovedBranch(t *testing.T) {
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

func scenarioUpdateBumpHonorsScopeAndDirectTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	targetOrigin, targetWorking := createBareRepoWithWorkingClone(t, "bump-target.git")
	writeSkill(t, targetWorking, "target", "Target v1", nil)
	commitAndPushAll(t, targetWorking, "target v1")
	createAndPushTag(t, targetWorking, "v1.0")
	writeSkill(t, targetWorking, "target", "Target v1.1", nil)
	commitAndPushAll(t, targetWorking, "target v1.1")
	createAndPushTag(t, targetWorking, "v1.1")

	otherOrigin, otherWorking := createBareRepoWithWorkingClone(t, "bump-other.git")
	writeSkill(t, otherWorking, "other", "Other v1", nil)
	commitAndPushAll(t, otherWorking, "other v1")
	createAndPushTag(t, otherWorking, "v1.0")
	writeSkill(t, otherWorking, "other", "Other v1.1", nil)
	commitAndPushAll(t, otherWorking, "other v1.1")
	createAndPushTag(t, otherWorking, "v1.1")

	repo := filepath.Join(t.TempDir(), "repo")
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, repoConfig, "\nharnesses: [claude]\nregistries:\n  - url: "+targetOrigin+"\n    ref: v1.0\n    skills: [target]\n  - url: "+otherOrigin+"\n    ref: v1.0\n    skills: [other]\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	beforeOther, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeOther.Registries) != 2 {
		t.Fatalf("unexpected initial registries: %+v", beforeOther.Registries)
	}

	output, err := executeCommand("update", "--bump", "--scope", "repo", "--registry", targetOrigin, "--ref", "v1.0", "--path", ".", "--kind", "skill", "target")
	if err != nil {
		t.Fatalf("targeted bump: %v\n%s", err, output)
	}
	after, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := after.Registries[0].Ref; got != "v1.1" {
		t.Fatalf("target ref = %q, want v1.1", got)
	}
	if got := after.Registries[1].Ref; got != "v1.0" {
		t.Fatalf("unrelated ref mutated by targeted bump: %q", got)
	}
}

func scenarioUpdateBumpHonorsUserScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "bump-user.git")
	writeSkill(t, working, "user-target", "User v1", nil)
	commitAndPushAll(t, working, "user v1")
	createAndPushTag(t, working, "v1.0")
	writeSkill(t, working, "user-target", "User v1.1", nil)
	commitAndPushAll(t, working, "user v1.1")
	createAndPushTag(t, working, "v1.1")

	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nregistries:\n  - url: "+origin+"\n    ref: v1.0\n    skills: [user-target]\n")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "\nharnesses: [claude]\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}

	output, err := executeCommand("update", "--bump", "--scope", "user", "--registry", origin, "--kind", "skill", "user-target")
	if err != nil {
		t.Fatalf("user targeted bump: %v\n%s", err, output)
	}
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := globalCfg.Registries[0].Ref; got != "v1.1" {
		t.Fatalf("user target ref = %q, want v1.1", got)
	}
}

func scenarioUpdateBumpHonorsLocalScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	localOrigin, localWorking := createBareRepoWithWorkingClone(t, "bump-local.git")
	writeSkill(t, localWorking, "local-target", "Local v1", nil)
	commitAndPushAll(t, localWorking, "local v1")
	createAndPushTag(t, localWorking, "v1.0")
	writeSkill(t, localWorking, "local-target", "Local v1.1", nil)
	commitAndPushAll(t, localWorking, "local v1.1")
	createAndPushTag(t, localWorking, "v1.1")

	repoOrigin, repoWorking := createBareRepoWithWorkingClone(t, "bump-local-other.git")
	writeSkill(t, repoWorking, "repo-target", "Repo v1", nil)
	commitAndPushAll(t, repoWorking, "repo v1")
	createAndPushTag(t, repoWorking, "v1.0")
	writeSkill(t, repoWorking, "repo-target", "Repo v1.1", nil)
	commitAndPushAll(t, repoWorking, "repo v1.1")
	createAndPushTag(t, repoWorking, "v1.1")

	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "\nharnesses: [claude]\nregistries:\n  - url: "+repoOrigin+"\n    ref: v1.0\n    skills: [repo-target]\n")
	localConfig := filepath.Join(repo, ".agents", "config.local.yaml")
	writeConfig(t, localConfig, "\nregistries:\n  - url: "+localOrigin+"\n    ref: v1.0\n    skills: [local-target]\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}

	output, err := executeCommand("update", "--bump", "--scope", "local", "--registry", localOrigin, "--kind", "skill", "local-target")
	if err != nil {
		t.Fatalf("local targeted bump: %v\n%s", err, output)
	}
	localCfg, err := config.LoadLocalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := localCfg.Registries[0].Ref; got != "v1.1" {
		t.Fatalf("local target ref = %q, want v1.1", got)
	}
	repoCfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := repoCfg.Registries[0].Ref; got != "v1.0" {
		t.Fatalf("repo ref mutated by local-scoped bump: %q", got)
	}
}

func scenarioUpdateBumpSkipsOrganization(t *testing.T) {
	for _, tc := range []struct {
		name  string
		scope string
		layer string
	}{
		{name: "default", layer: "repo"},
		{name: "all", scope: "all", layer: "user"},
		{name: "local", scope: "local", layer: "local"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := canonicalTempDir(t)
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			stagingRoot := canonicalTempDir(t)
			t.Setenv("TMPDIR", stagingRoot)
			origin, working := createBareRepoWithWorkingClone(t, "org-bump.git")
			writeSkill(t, working, "target", "Target v1", nil)
			commitAndPushAll(t, working, "target v1")
			createAndPushTag(t, working, "v1.0")
			writeSkill(t, working, "target", "Target v1.1", nil)
			commitAndPushAll(t, working, "target v1.1")
			createAndPushTag(t, working, "v1.1")

			authority := canonicalTempDir(t)
			orgPath := filepath.Join(authority, "config.yaml")
			registryConfig := "registries:\n  - url: " + origin + "\n    ref: v1.0\n    skills: [target]\n"
			writeConfig(t, orgPath, registryConfig)
			orgBefore, err := os.ReadFile(orgPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(authority, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
			config.SetTestOrganizationConfigPath(t, orgPath)
			repo := filepath.Join(canonicalTempDir(t), "repo")
			configPath := filepath.Join(repo, ".agents", "config.yaml")
			lockPath := filepath.Join(repo, ".agents", "lock.yaml")
			writeConfig(t, configPath, "harnesses: [factory]\n")
			switch tc.layer {
			case "user":
				configPath = filepath.Join(home, ".agents", "config.yaml")
				lockPath = filepath.Join(home, ".agents", "lock.yaml")
			case "local":
				configPath = filepath.Join(repo, ".agents", "config.local.yaml")
				lockPath = filepath.Join(repo, ".agents", "lock.local.yaml")
			}
			writeConfig(t, configPath, registryConfig)
			withWorkingDir(t, repo)
			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("initial sync: %v\n%s", err, output)
			}
			beforeLock := readLockDocument(t, lockPath)
			if len(beforeLock.Registries) != 1 || beforeLock.Registries[0].Ref != "v1.0" {
				t.Fatalf("initial lock = %+v, want v1.0", beforeLock.Registries)
			}

			args := []string{"update", "--bump"}
			if tc.scope != "" {
				args = append(args, "--scope", tc.scope)
			}
			output, updateErr := executeCommand(args...)
			orgAfter, err := os.ReadFile(orgPath)
			if err != nil || !bytes.Equal(orgBefore, orgAfter) {
				t.Fatalf("organization config changed: %v\nbefore:\n%s\nafter:\n%s", err, orgBefore, orgAfter)
			}
			if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
				t.Fatalf("organization lock created: %v", err)
			}
			residue, err := filepath.Glob(filepath.Join(stagingRoot, ".agent-marketplace-config-*"))
			if err != nil || len(residue) != 0 {
				t.Fatalf("config staging residue = %v, %v", residue, err)
			}
			if updateErr != nil {
				t.Fatalf("update --bump: %v\n%s", updateErr, output)
			}
			afterConfig, err := os.ReadFile(configPath)
			if err != nil || !bytes.Contains(afterConfig, []byte("ref: v1.1")) {
				t.Fatalf("selected %s config did not bump: %s, %v", tc.layer, afterConfig, err)
			}
			afterLock := readLockDocument(t, lockPath)
			if len(afterLock.Registries) != 1 || afterLock.Registries[0].Ref != "v1.1" || afterLock.Registries[0].Commit == beforeLock.Registries[0].Commit {
				t.Fatalf("selected %s lock did not advance: %+v", tc.layer, afterLock.Registries)
			}
		})
	}
}

func scenarioScopedUpdateReconcilesMissingUnselectedWritableLayer(t *testing.T) {
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

func scenarioUpdateBumpWithSimpleTrailingInteger(t *testing.T) {
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

func scenarioUpdateBumpWithThreeSegmentSemver(t *testing.T) {
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

func scenarioUpdateBumpDoesNotCrossMajorVersion(t *testing.T) {
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

func scenarioUpdateBumpWithNonVersionRefDoesNothing(t *testing.T) {
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

func scenarioUpdateCommandBlockedRegistryReturnsError(t *testing.T) {
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
