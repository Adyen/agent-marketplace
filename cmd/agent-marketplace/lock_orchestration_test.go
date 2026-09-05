package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestLockPublicationHoldsExactLeaseFromSnapshotThroughCommit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "publication-gc.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "\nregistries:\n  - url: "+origin+"\n    ref: main\n")
	withWorkingDir(t, repo)

	gcDone := make(chan error, 1)
	originalHook := lockSnapshotAcquiredHook
	lockSnapshotAcquiredHook = func() {
		go func() {
			gcDone <- runGC(context.Background(), io.Discard, false)
		}()
		select {
		case err := <-gcDone:
			t.Fatalf("GC crossed snapshot-to-publication gap: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Cleanup(func() { lockSnapshotAcquiredHook = originalHook })

	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("lock publication: %v\n%s", err, output)
	}
	select {
	case err := <-gcDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("GC did not resume after lock publication released its lease")
	}
	entries, err := os.ReadDir(filepath.Join(cache.CacheDir(), "exact-git", "snapshots"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("published exact snapshot count = %d, want 1", len(entries))
	}
}

func TestDependencyRecoveryWaitsForExactGCNamespace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	stateDir, err := dependencyStateDir()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stateDir, ".dependencytxn-broken.json"), "{")

	gcEntered := make(chan struct{})
	releaseGC := make(chan struct{})
	gcDone := make(chan error, 1)
	go func() {
		_, err := cache.GarbageCollectWithReachabilityAndContext(context.Background(), true, func(context.Context) (cache.GCReachability, error) {
			close(gcEntered)
			<-releaseGC
			return cache.GCReachability{Configs: []*config.Config{{}}}, nil
		})
		gcDone <- err
	}()
	<-gcEntered

	recoveryDone := make(chan error, 1)
	go func() { recoveryDone <- recoverDependencyTransactions(context.Background()) }()
	select {
	case err := <-recoveryDone:
		t.Fatalf("recovery entered publication while GC held the exclusive namespace lease: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseGC)
	if err := <-gcDone; err != nil {
		t.Fatal(err)
	}
	if err := <-recoveryDone; !errors.Is(err, dependencytxn.ErrRecoveryBlocked) {
		t.Fatalf("recovery error = %v, want recovery blocked after GC released", err)
	}
}

func TestDeinitOrganizationResolutionReturnsHeldExactLease(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "deinit-contention.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "seed")
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repo)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nharnesses: [factory]\n")
	orgConfig := filepath.Join(t.TempDir(), "config.yaml")
	config.SetTestOrganizationConfigPath(t, orgConfig)
	writeConfig(t, orgConfig, "\nregistries:\n  - url: "+origin+"\n    ref: main\n    skills: [review]\n")
	if output, err := executeCommand("lock", "--scope", "organization"); err != nil {
		t.Fatalf("seed organization exact lock: %v\n%s", err, output)
	}

	artifacts, release, err := resolveOrgOwnedArtifacts(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 || artifacts[0].Name != "review" || artifacts[0].Source != config.Organization {
		release()
		t.Fatalf("organization artifacts = %#v, want organization-owned review", artifacts)
	}
	gcDone := make(chan error, 1)
	go func() {
		_, err := cache.GarbageCollectWithLocks(false, nil, &config.Config{})
		gcDone <- err
	}()
	select {
	case err := <-gcDone:
		release()
		t.Fatalf("GC completed while deinit held organization artifacts: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	select {
	case err := <-gcDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("GC did not resume after deinit released its exact lease")
	}
}

func TestSyncCheckExactConsumerLeasePreventsSnapshotDeletion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	origin, working := createBareRepoWithWorkingClone(t, "check-contention.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "seed")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `
harnesses: [factory]
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repo)
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("seed exact lock: %v\n%s", err, output)
	}

	_, _, checkLease, err := prepareRuntimeLocks(context.Background(), repo, "", lockMode{locked: true, check: true})
	if err != nil {
		t.Fatalf("prepare sync check: %v", err)
	}
	release, err := checkLease(context.Background())
	if err != nil {
		t.Fatalf("acquire sync check lease: %v", err)
	}
	leaseHeld := true
	defer func() {
		if leaseHeld {
			release()
		}
	}()

	snapshotRoot := filepath.Join(cache.CacheDir(), "exact-git", "snapshots")
	entries, err := os.ReadDir(snapshotRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("exact snapshot count = %d, want one deletion candidate", len(entries))
	}
	snapshot := filepath.Join(snapshotRoot, entries[0].Name())
	started := make(chan struct{})
	gcDone := make(chan error, 1)
	go func() {
		close(started)
		_, err := cache.GarbageCollectWithReachabilityAndContext(context.Background(), false, func(context.Context) (cache.GCReachability, error) {
			return cache.GCReachability{Configs: []*config.Config{{}}}, nil
		})
		gcDone <- err
	}()
	<-started
	select {
	case err := <-gcDone:
		t.Fatalf("GC completed while exact consumer held its lease: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := os.Stat(snapshot); err != nil {
		t.Fatalf("GC deleted snapshot while exact consumer held its lease: %v", err)
	}

	release()
	leaseHeld = false
	select {
	case err := <-gcDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("GC did not resume after exact consumer released its lease")
	}
	if _, err := os.Stat(snapshot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unreachable snapshot survived GC: %v", err)
	}
}

func TestSyncLocalRegistryDoesNotCreateLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	registry := filepath.Join(t.TempDir(), "registry")
	writeSkill(t, registry, "review", "Review", nil)
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `
registries:
  - url: `+registry+`
    skills:
      include: [review]
`)
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync local registry: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("local registry created a lock: %v", err)
	}
}

func TestLockOmitsUnsupportedRuleAndPrintsWarning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "unsupported-rule.git")
	writeFile(t, filepath.Join(working, ".factory", "skills", "review", "SKILL.md"), "---\nname: review\ndescription: Review\n---\nReview body.\n")
	writeFile(t, filepath.Join(working, ".factory", "rules", "end-of-response-ask-user-question.md"), "Ask the user a question.\n")
	commitAndPushAll(t, working, "skill and rule")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "\nregistries:\n  - url: "+origin+"\n    ref: main\n    source_harness: factory\n")
	withWorkingDir(t, repo)

	output, err := executeCommand("lock", "--scope", "repo")
	if err != nil {
		t.Fatalf("lock: %v\n%s", err, output)
	}
	if !strings.Contains(output, "warning:") || !strings.Contains(output, "rule/end-of-response-ask-user-question") {
		t.Fatalf("lock output lacks unsupported rule warning:\n%s", output)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".agents", "lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := lockfile.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Registries) != 1 || len(doc.Registries[0].Roots) != 1 {
		t.Fatalf("lock registry roots = %+v, want one", doc.Registries)
	}
	resources := doc.Registries[0].Roots[0].Resources
	if len(resources) != 1 || resources[0].Kind != "skill" || resources[0].Name != "review" {
		t.Fatalf("locked resources = %+v, want only skill/review", resources)
	}
}

func TestRuntimeRecoversDependencyTransactionBeforeLoadingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "recover-before-load.git")
	writeSkill(t, working, "review", "Recovered review", nil)
	commitAndPushAll(t, working, "review")
	repo := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	newConfig := []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: " + origin + "\n    ref: main\n")
	writeFile(t, configPath, string(newConfig))
	withWorkingDir(t, repo)
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("seed exact lock: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	newLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	writeConfig(t, configPath, "\nregistries: []\n")
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	canonicalConfigDir, err := filepath.EvalSymlinks(filepath.Dir(configPath))
	if err != nil {
		t.Fatal(err)
	}
	canonicalConfig := filepath.Join(canonicalConfigDir, filepath.Base(configPath))
	canonicalLock := filepath.Join(canonicalConfigDir, filepath.Base(lockPath))
	configGeneration, err := dependencytxn.ReadGeneration(canonicalConfig)
	if err != nil {
		t.Fatal(err)
	}
	stateDir, err := dependencyStateDir()
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("interrupt after journal")
	_, err = dependencytxn.Commit(context.Background(), stateDir, []dependencytxn.DesiredFile{
		{Path: canonicalConfig, Expected: configGeneration, Bytes: newConfig, Mode: 0o644},
		{Path: canonicalLock, Expected: dependencytxn.Generation{}, Bytes: newLock, Mode: 0o644},
	}, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
		if point.Stage == dependencytxn.FaultAfterJournal {
			return injected
		}
		return nil
	}))
	if !errors.Is(err, injected) {
		t.Fatalf("prepare interrupted transaction: %v", err)
	}

	if output, err := executeCommand("lock", "--check", "--scope", "repo"); err != nil {
		t.Fatalf("lock check after recovery: %v\n%s", err, output)
	}
	output, err := executeCommand("list")
	if err != nil {
		t.Fatalf("list recovered exact lock: %v\n%s", err, output)
	}
	if !strings.Contains(output, "review") {
		t.Fatalf("runtime did not observe recovered config and lock:\n%s", output)
	}
}

func TestConfiguredReadCommandsRecoverBeforeLoadingConfig(t *testing.T) {
	for _, command := range []string{"list", "status", "gc", "tui"} {
		t.Run(command, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nregistries: []\n")
			stateDir, err := dependencyStateDir()
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(stateDir, ".dependencytxn-broken.json"), "{")

			output, err := executeCommand(command)
			if !errors.Is(err, dependencytxn.ErrRecoveryBlocked) {
				t.Fatalf("%s error = %v\n%s; want recovery blocked", command, err, output)
			}
		})
	}
}

func TestConfiguredListDoesNotCreateDependencyState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Chdir(t.TempDir())
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nregistries: []\n")
	if output, err := executeCommand("list"); err != nil {
		t.Fatalf("list: %v\n%s", err, output)
	}
	stateDir, err := dependencyStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only recovery created state directory: %v", err)
	}
}

func TestLockCommitRejectsConcurrentConfigEdit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "config-cas.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")
	repo := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "\nregistries:\n  - url: "+origin+"\n    ref: main\n")
	withWorkingDir(t, repo)

	originalCommit := commitDependencyTransaction
	commitDependencyTransaction = func(ctx context.Context, stateDir string, desired []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
		writeConfig(t, configPath, "\nregistries: []\n")
		return dependencytxn.Commit(ctx, stateDir, desired, opts...)
	}
	t.Cleanup(func() { commitDependencyTransaction = originalCommit })

	output, err := executeCommand("lock", "--scope", "repo")
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("lock error = %v\n%s; want generation conflict", err, output)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("stale lock was published after concurrent config edit: %v", statErr)
	}
}

func TestSyncMalformedExistingLockIsNotOverwritten(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	origin, working := createBareRepoWithWorkingClone(t, "malformed-lock.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")
	repo := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      include: [review]
`)
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	bad := []byte("apiVersion: future/v9\nscope: repo\n")
	if err := os.WriteFile(lockPath, bad, 0o644); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err == nil {
		t.Fatalf("sync accepted unsupported lock:\n%s", output)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(bad) {
		t.Fatalf("invalid lock was overwritten:\n%s", after)
	}
}
