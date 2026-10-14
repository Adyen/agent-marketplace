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

	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// stubSyncWorkspaceDiscovery replaces CLI workspace discovery for one test so
// the ordering and reuse of the single discovered Workspace are observable.
func stubSyncWorkspaceDiscovery(t *testing.T, fn func() (pipeline.Workspace, error)) {
	t.Helper()
	prev := discoverSyncWorkspace
	discoverSyncWorkspace = fn
	t.Cleanup(func() { discoverSyncWorkspace = prev })
}

func canonicalTestPath(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("canonicalize %q: %v", path, err)
	}
	return filepath.Clean(canonical)
}

// TestSyncFailsBeforeLockPreparationWhenWorkspaceDiscoveryFails pins the
// ordering invariant: sync and sync-check must construct the validated
// Workspace before any lock preparation runs. A discovery failure must
// surface without a lockfile being written.
// spec: workspace-resolution/escaping-personal-local-root
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
apiVersion: agent-marketplace/v4
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/locked-skill]
`)
	withWorkingDir(t, repoRoot)

	sentinel := errors.New("workspace discovery failed")
	stubSyncWorkspaceDiscovery(t, func() (pipeline.Workspace, error) {
		return pipeline.Workspace{}, sentinel
	})
	previousEnsure := ensureGlobalConfigForSync
	ensureCalls := 0
	ensureGlobalConfigForSync = func() error {
		ensureCalls++
		return nil
	}
	t.Cleanup(func() { ensureGlobalConfigForSync = previousEnsure })

	if err := runSyncWithLocks(context.Background(), io.Discard, false, false, false, false, false); !errors.Is(err, sentinel) {
		t.Fatalf("runSyncWithLocks error = %v; want discovery sentinel", err)
	}
	if _, statErr := os.Stat(filepath.Join(repoRoot, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("lock preparation ran before workspace validation; stat err = %v", statErr)
	}
	if ensureCalls != 0 {
		t.Fatalf("global config initialization ran before workspace validation: %d calls", ensureCalls)
	}
	if err := runSyncCheck(context.Background(), io.Discard, false); !errors.Is(err, sentinel) {
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
apiVersion: agent-marketplace/v4
sources:
  local: {directory: `+registry+`}
resources:
  local: [skill/discovered-skill]
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
	if err := runSyncCheck(context.Background(), io.Discard, false); err != nil {
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
apiVersion: agent-marketplace/v4
sources:
  local: {directory: `+repoRegistry+`}
resources:
  local: [skill/repo-skill, skill/repo-skill2]
`)
	nested = filepath.Join(repoRoot, "sub")
	writeFile(t, filepath.Join(nested, ".agents", "config.local.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  local: {directory: `+localRegistry+`}
resources:
  local: [skill/nested-skill]
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

// TestStatusFromNestedLocalDryRunsTheWorkspaceMaterializeRoot pins status's
// drift probe to the same nested materialization root used by workspace sync.
func TestStatusFromNestedLocalDryRunsTheWorkspaceMaterializeRoot(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())

	repoRoot, nested := writeNestedLocalFixture(t)
	withWorkingDir(t, nested)

	var gotRoot string
	previous := runStatusMaterialize
	runStatusMaterialize = func(ctx context.Context, root string, artifacts []resolver.ResolvedArtifact, opts ...materialize.RunOption) (*materialize.Report, error) {
		gotRoot = root
		return &materialize.Report{}, nil
	}
	t.Cleanup(func() { runStatusMaterialize = previous })

	if err := runStatus(context.Background(), io.Discard, time.Now()); err != nil {
		t.Fatalf("status from nested local dir: %v", err)
	}
	if got, want := canonicalTestPath(t, gotRoot), canonicalTestPath(t, nested); got != want {
		t.Fatalf("status dry-run root = %q; want workspace materialization root %q (repository root %q)", got, want, repoRoot)
	}
}

// spec: workspace-resolution/nested-personal-local-browsing-and-inspection
func TestBrowsingCommandsReadCommittedAndNestedLocalLayers(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())

	repoRoot, nested := writeNestedLocalFixture(t)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  committed: {marketplace: http://127.0.0.1:1/committed-mp}
`)
	writeFile(t, filepath.Join(nested, ".agents", "config.local.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  nested: {marketplace: http://127.0.0.1:1/nested-mp}
`)
	withWorkingDir(t, nested)

	err := runSearchContext(context.Background(), io.Discard, io.Discard, []string{"skill"}, "", "", "table", 0, 0)
	if err == nil {
		t.Fatal("search against unreachable Marketplace endpoints must fail")
	}
	for _, marker := range []string{"committed-mp", "nested-mp"} {
		if !strings.Contains(err.Error(), marker) {
			t.Fatalf("search error %v does not contain the %s endpoint; a configuration layer root was dropped", err, marker)
		}
	}

	committedPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	localPath := filepath.Join(nested, ".agents", "config.local.yaml")
	writeConfig(t, committedPath, "registries: [")
	writeFile(t, localPath, "registries: [")

	var doctorOut strings.Builder
	if err := runDoctor(doctorOptions{ctx: context.Background(), out: &doctorOut}); err != nil {
		t.Fatalf("doctor with malformed layers: %v", err)
	}
	for _, path := range []string{committedPath, localPath} {
		if !strings.Contains(doctorOut.String(), path) {
			t.Fatalf("doctor output does not report layer error at %s:\n%s", path, doctorOut.String())
		}
	}

	listErr := runListFrom(context.Background(), io.Discard, io.Discard, filepath.Join(t.TempDir(), "registry"), "", "", "")
	if listErr == nil {
		t.Fatal("list --from must surface configuration layer errors instead of ignoring them")
	}
	for _, path := range []string{committedPath, localPath} {
		if !strings.Contains(listErr.Error(), path) {
			t.Fatalf("list --from error %v does not report layer error at %s", listErr, path)
		}
	}

	gcErr := runGC(context.Background(), io.Discard, true)
	if gcErr == nil {
		t.Fatal("gc must surface configuration layer errors instead of ignoring them")
	}
	for _, path := range []string{committedPath, localPath} {
		if !strings.Contains(gcErr.Error(), path) {
			t.Fatalf("gc error %v does not report layer error at %s", gcErr, path)
		}
	}
}

// spec: workspace-resolution/escaping-personal-local-root-during-browsing-and-collection
func TestBrowsingCommandsRejectEscapingLocalRoot(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())

	repoRoot := canonicalTestPath(t, t.TempDir())
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
`)
	outside := canonicalTestPath(t, t.TempDir())
	writeFile(t, filepath.Join(outside, ".agents", "config.local.yaml"), `
apiVersion: agent-marketplace/v4
`)
	link := filepath.Join(repoRoot, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink escaping local root: %v", err)
	}
	withWorkingDir(t, link)
	t.Setenv("PWD", link)

	assertInvalidWorkspace := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s must reject a personal-local root outside the repository", name)
		}
		if !errors.Is(err, pipeline.ErrInvalidWorkspace) {
			t.Fatalf("%s error = %v; want an invalid-workspace failure", name, err)
		}
	}

	assertInvalidWorkspace("list --from", runListFrom(context.Background(), io.Discard, io.Discard, "https://example.com/registry.git", "", "", ""))
	assertInvalidWorkspace("search", runSearchContext(context.Background(), io.Discard, io.Discard, []string{"skill"}, "", "", "table", 0, 0))
	assertInvalidWorkspace("doctor", runDoctor(doctorOptions{ctx: context.Background(), out: io.Discard}))
	assertInvalidWorkspace("gc", runGC(context.Background(), io.Discard, true))

	if _, err := os.Stat(filepath.Join(repoRoot, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("command mutated repository state before workspace validation; stat err = %v", err)
	}
}

// TestListFromNestedLocalDryRunsTheWorkspaceMaterializeRoot pins list's drift
// probe to the same nested materialization root used by workspace sync.
func TestListFromNestedLocalDryRunsTheWorkspaceMaterializeRoot(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())

	repoRoot, nested := writeNestedLocalFixture(t)
	withWorkingDir(t, nested)

	var gotRoot string
	previous := runListMaterialize
	runListMaterialize = func(ctx context.Context, root string, artifacts []resolver.ResolvedArtifact, opts ...materialize.RunOption) (*materialize.Report, error) {
		gotRoot = root
		return &materialize.Report{}, nil
	}
	t.Cleanup(func() { runListMaterialize = previous })

	if err := runList(context.Background(), io.Discard, io.Discard, ""); err != nil {
		t.Fatalf("list from nested local dir: %v", err)
	}
	if got, want := canonicalTestPath(t, gotRoot), canonicalTestPath(t, nested); got != want {
		t.Fatalf("list dry-run root = %q; want workspace materialization root %q (repository root %q)", got, want, repoRoot)
	}
}
