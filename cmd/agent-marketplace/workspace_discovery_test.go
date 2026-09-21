package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

// stubSyncWorkspaceDiscovery replaces CLI workspace discovery for one test so
// the ordering and reuse of the single discovered Workspace are observable.
func stubSyncWorkspaceDiscovery(t *testing.T, fn func() (pipeline.Workspace, error)) {
	t.Helper()
	prev := discoverSyncWorkspace
	discoverSyncWorkspace = fn
	t.Cleanup(func() { discoverSyncWorkspace = prev })
}

// TestSyncFailsBeforeLockPreparationWhenWorkspaceDiscoveryFails pins the
// ordering invariant: sync and sync-check must construct the validated
// Workspace before any lock preparation runs. A discovery failure must
// surface without a lockfile being written.
func TestSyncFailsBeforeLockPreparationWhenWorkspaceDiscoveryFails(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "locked-skills.git")
	writeSkill(t, working, "locked-skill", "Locked skill", nil)
	commitAndPushAll(t, working, "Add locked skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - locked-skill
`)
	withWorkingDir(t, repoRoot)

	sentinel := errors.New("workspace discovery failed")
	stubSyncWorkspaceDiscovery(t, func() (pipeline.Workspace, error) {
		return pipeline.Workspace{}, sentinel
	})

	if err := runSyncWithLocks(context.Background(), io.Discard, false, false, false, false, false); !errors.Is(err, sentinel) {
		t.Fatalf("runSyncWithLocks error = %v; want discovery sentinel", err)
	}
	if _, statErr := os.Stat(filepath.Join(repoRoot, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("lock preparation ran before workspace validation; stat err = %v", statErr)
	}
	if err := runSyncCheck(context.Background(), io.Discard); !errors.Is(err, sentinel) {
		t.Fatalf("runSyncCheck error = %v; want discovery sentinel", err)
	}
	if _, statErr := os.Stat(filepath.Join(repoRoot, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("sync-check lock preparation ran before workspace validation; stat err = %v", statErr)
	}
}

// TestSyncReusesTheDiscoveredWorkspaceAcrossLocksResolveAndMaterialization
// stubs discovery to return a Workspace rooted at a different repository than
// the working directory. Every consumer (lock preparation, effective config,
// Resolve/Sync, and materialization) must use that exact Workspace, so all
// effects land in the discovered repository and none in the working
// directory.
func TestSyncReusesTheDiscoveredWorkspaceAcrossLocksResolveAndMaterialization(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	registry := t.TempDir()
	writeSkill(t, registry, "discovered-skill", "Discovered skill", nil)

	discoveredRepo := filepath.Join(t.TempDir(), "discovered-repo")
	writeConfig(t, filepath.Join(discoveredRepo, ".agents", "config.yaml"), `
registries:
  - url: `+registry+`
    skills:
      - discovered-skill
`)

	cwdRepo := filepath.Join(t.TempDir(), "cwd-repo")
	if err := os.MkdirAll(cwdRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, cwdRepo)

	stubSyncWorkspaceDiscovery(t, func() (pipeline.Workspace, error) {
		return mustWorkspace(t, discoveredRepo), nil
	})

	if err := runSyncWithLocks(context.Background(), io.Discard, false, false, false, false, false); err != nil {
		t.Fatalf("runSyncWithLocks: %v", err)
	}
	assertFileExists(t, filepath.Join(discoveredRepo, ".factory", "skills", "discovered-skill", "SKILL.md"))
	if _, err := os.Stat(filepath.Join(cwdRepo, ".factory")); !os.IsNotExist(err) {
		t.Fatalf("materialization used the working-directory root instead of the discovered workspace; stat err = %v", err)
	}

	// sync --check must resolve and dry-run materialize against the same
	// discovered workspace: a fully materialized repository reports in sync.
	if err := runSyncCheck(context.Background(), io.Discard); err != nil {
		t.Fatalf("runSyncCheck against the discovered workspace error = %v; want in sync", err)
	}
}

// writeNestedLocalFixture builds a repository whose committed config selects
// repo-skill from one local registry while a nested subdirectory's
// config.local.yaml selects nested-skill from another.
func writeNestedLocalFixture(t *testing.T) (repoRoot, nested string) {
	t.Helper()

	repoRegistry := t.TempDir()
	writeSkill(t, repoRegistry, "repo-skill", "Repo skill", nil)
	writeSkill(t, repoRegistry, "repo-skill2", "Second repo skill", nil)
	localRegistry := t.TempDir()
	writeSkill(t, localRegistry, "nested-skill", "Nested skill", nil)

	repoRoot = filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+repoRegistry+`
    skills:
      - repo-skill
      - repo-skill2
`)
	nested = filepath.Join(repoRoot, "sub")
	writeFile(t, filepath.Join(nested, ".agents", "config.local.yaml"), `
registries:
  - url: `+localRegistry+`
    skills:
      - nested-skill
`)
	return repoRoot, nested
}

// TestSyncFromNestedLocalMaterializesBeneathTheNestedRoot pins the CLI seam:
// one discovered Workspace carries the nested local-config root through lock
// preparation, Resolve/Sync, and materialization.
func TestSyncFromNestedLocalMaterializesBeneathTheNestedRoot(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot, nested := writeNestedLocalFixture(t)
	withWorkingDir(t, nested)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync from nested local dir: %v\n%s", err, output)
	}
	assertFileExists(t, filepath.Join(nested, ".factory", "skills", "repo-skill", "SKILL.md"))
	assertFileExists(t, filepath.Join(nested, ".factory", "skills", "nested-skill", "SKILL.md"))
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory")); !os.IsNotExist(err) {
		t.Fatalf("materialization used the repository root instead of the nested local root; stat err = %v", err)
	}

	output, err = executeCommand("sync", "--check")
	if err != nil {
		t.Fatalf("sync --check from nested local dir error = %v; want in sync\n%s", err, output)
	}
}

// TestRemoveFromNestedLocalPreservesTheLocalLayer is the production-seam
// regression for remove: the re-sync after a repo-scope removal must keep
// resolving the nested local layer and materializing beneath the nested
// root instead of pinning the workspace to the repository root.
func TestRemoveFromNestedLocalPreservesTheLocalLayer(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot, nested := writeNestedLocalFixture(t)
	withWorkingDir(t, nested)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync from nested local dir: %v\n%s", err, output)
	}

	output, err = executeCommand("remove", "repo-skill", "--scope", "repo")
	if err != nil {
		t.Fatalf("remove repo-skill from nested local dir: %v\n%s", err, output)
	}

	assertFileExists(t, filepath.Join(nested, ".factory", "skills", "nested-skill", "SKILL.md"))
	assertFileExists(t, filepath.Join(nested, ".factory", "skills", "repo-skill2", "SKILL.md"))
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory")); !os.IsNotExist(err) {
		t.Fatalf("post-remove sync materialized at the repository root; the nested local root was dropped; stat err = %v", err)
	}
}

// TestInstallUserScopeFromNestedLocalPreservesTheLocalLayer is the
// production-seam regression for install: the re-sync after a user-scope
// install from a nested working directory must keep the nested local layer.
func TestInstallUserScopeFromNestedLocalPreservesTheLocalLayer(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot, nested := writeNestedLocalFixture(t)
	withWorkingDir(t, nested)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync from nested local dir: %v\n%s", err, output)
	}

	extraRegistry := t.TempDir()
	writeSkill(t, extraRegistry, "extra-skill", "Extra skill", nil)
	output, err = executeCommand("install", "extra-skill", "--from", extraRegistry, "--scope", "user")
	if err != nil {
		t.Fatalf("install extra-skill from nested local dir: %v\n%s", err, output)
	}

	assertFileExists(t, filepath.Join(nested, ".factory", "skills", "nested-skill", "SKILL.md"))
	assertFileExists(t, filepath.Join(homeDir, ".factory", "skills", "extra-skill", "SKILL.md"))
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory")); !os.IsNotExist(err) {
		t.Fatalf("post-install sync materialized at the repository root; the nested local root was dropped; stat err = %v", err)
	}
}
