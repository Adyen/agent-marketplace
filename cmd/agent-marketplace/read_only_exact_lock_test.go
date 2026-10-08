package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/devincli"
)

func setupReadOnlyExactRegistry(t *testing.T) (repo, origin, working, lockPath string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working = createBareRepoWithWorkingClone(t, "read-only-exact.git")
	writeSkill(t, working, "locked-skill", "Locked", nil)
	commitAndPushAll(t, working, "locked")
	repo = filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      include: [locked-skill]
`)
	withWorkingDir(t, repo)
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("create exact lock: %v\n%s", err, output)
	}
	return repo, origin, working, filepath.Join(repo, ".agents", "lock.yaml")
}

func TestConfiguredListUsesExactLockWithoutReconciliation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, _, working, lockPath := setupReadOnlyExactRegistry(t)
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	writeSkill(t, working, "mutable-new-skill", "Mutable", nil)
	commitAndPushAll(t, working, "move mutable ref")

	output, err := executeCommand("list")
	if err != nil {
		t.Fatalf("list exact snapshot: %v\n%s", err, output)
	}
	if !strings.Contains(output, "locked-skill") || strings.Contains(output, "mutable-new-skill") {
		t.Fatalf("list did not stay on locked commit:\n%s", output)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("list reconciled lock:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestDoctorRepairUsesExactGitLockWithoutRefreshingMutableCache(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, origin, working, lockPath := setupReadOnlyExactRegistry(t)
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	writeSkill(t, working, "mutable-new-skill", "Mutable", nil)
	commitAndPushAll(t, working, "move mutable ref")
	mutableSkill := filepath.Join(cache.RegistryCachePath(origin, "main"), "skills", "mutable-new-skill", "SKILL.md")
	if _, err := os.Lstat(mutableSkill); !os.IsNotExist(err) {
		t.Fatalf("mutable cache already contains advanced ref: %v", err)
	}

	output, err := executeCommand("doctor", "--repair")
	if err != nil || !strings.Contains(output, "nothing to repair") {
		t.Fatalf("doctor --repair did not use the locked snapshot: %v\n%s", err, output)
	}
	if _, err := os.Lstat(mutableSkill); !os.IsNotExist(err) {
		t.Fatalf("doctor --repair refreshed mutable Git cache despite an exact lock: %v", err)
	}
	if after, err := os.ReadFile(lockPath); err != nil || string(after) != string(before) {
		t.Fatalf("doctor --repair rewrote the exact lock: %v\n%s", err, after)
	}
}

func TestDoctorRepairReplacesConflictFromLockedGitRevision(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "repair-exact.git")
	writeFile(t, filepath.Join(working, ".mcp.json"), `{"mcpServers":{"managed":{"command":"locked-binary"}}}`)
	commitAndPushAll(t, working, "locked MCP")
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses: [devin]
registries:
  - url: `+origin+`
    ref: main
    mcp:
      include: [managed]
`)
	withWorkingDir(t, repo)
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("create exact lock: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	locked, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(repo, ".devin", "mcp_config.json")
	writeFile(t, mcpPath, `{"mcpServers":{"managed":{"command":"handwritten"}}}`)
	writeFile(t, filepath.Join(working, ".mcp.json"), `{"mcpServers":{"managed":{"command":"mutable-binary"}}}`)
	commitAndPushAll(t, working, "move mutable ref")

	var out bytes.Buffer
	if err := runDoctor(doctorOptions{
		out: &out, in: strings.NewReader("y\n"), isTTY: true, repair: true,
		probe: staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
	}); err != nil || !strings.Contains(out.String(), "replaced mcp-server \"managed\"") {
		t.Fatalf("doctor did not repair the locked MCP conflict: %v\n%s", err, out.String())
	}
	if after, err := os.ReadFile(mcpPath); err != nil || !bytes.Contains(after, []byte("locked-binary")) || bytes.Contains(after, []byte("mutable-binary")) || bytes.Contains(after, []byte("handwritten")) {
		t.Fatalf("repair installed content outside locked revision: %v\n%s", err, after)
	}
	if after, err := os.ReadFile(lockPath); err != nil || !bytes.Equal(after, locked) {
		t.Fatalf("repair changed the pinned Git lock: %v\n%s", err, after)
	}
}

func TestDoctorRepairRejectsMissingGitLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, _, _, lockPath := setupReadOnlyExactRegistry(t)
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	output, err := executeCommand("doctor", "--repair")
	if err == nil || !strings.Contains(err.Error(), "lock.missing") {
		t.Fatalf("doctor --repair accepted a missing repo Git lock: %v\n%s", err, output)
	}
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("doctor --repair recreated a missing repo lock: %v", err)
	}
}

func TestDoctorRepairRejectsMalformedGitLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, _, _, lockPath := setupReadOnlyExactRegistry(t)
	malformed := []byte("not: [valid\n")
	if err := os.WriteFile(lockPath, malformed, 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := executeCommand("doctor", "--repair")
	if err == nil || !strings.Contains(err.Error(), "lock.invalid") {
		t.Fatalf("doctor --repair accepted a malformed repo Git lock: %v\n%s", err, output)
	}
	if after, err := os.ReadFile(lockPath); err != nil || string(after) != string(malformed) {
		t.Fatalf("doctor --repair changed the malformed lock: %v\n%s", err, after)
	}
}

func TestConfiguredListMissingLockNamesAffectedLayer(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	stateHome := filepath.Join(home, "state")
	t.Setenv("XDG_STATE_HOME", stateHome)
	origin, working := createBareRepoWithWorkingClone(t, "missing-read-lock.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repo)
	output, err := executeCommand("list")
	if err == nil {
		t.Fatalf("list accepted missing lock:\n%s", output)
	}
	if !strings.Contains(err.Error(), "lock.missing") || !strings.Contains(err.Error(), "repo lock") {
		t.Fatalf("missing lock did not identify repo layer: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("list created lock: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(stateHome, "agent-marketplace", "dependencies")); !os.IsNotExist(statErr) {
		t.Fatalf("read-only list created dependency transaction state: %v", statErr)
	}
}

func TestStatusShowsLockMetadataWhenExactSnapshotIsMissing(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, _, _, lockPath := setupReadOnlyExactRegistry(t)
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lockBytes), "sha1:") {
		t.Fatalf("fixture lock has no exact commit:\n%s", lockBytes)
	}
	if err := os.RemoveAll(filepath.Join(cache.CacheDir(), "exact-git", "snapshots")); err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("status")
	if err == nil {
		t.Fatalf("status accepted incomplete exact view:\n%s", output)
	}
	for _, want := range []string{"EXACT REGISTRY LOCKS", "sha1:", "missing", "DRIFT", "Unknown (exact registry view incomplete)", "lock.snapshot-unavailable"} {
		if !strings.Contains(output, want) && !strings.Contains(err.Error(), want) {
			t.Fatalf("status missing %q:\n%s\nerror: %v", want, output, err)
		}
	}
}

func TestDeinitDryRunUsesOrganizationExactLockWithoutReconciliation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "org-read-only-exact.git")
	writeSkill(t, working, "locked-org-skill", "Locked org", nil)
	commitAndPushAll(t, working, "locked org")
	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, orgPath, `
registries:
  - url: `+origin+`
    ref: main
    required: true
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "registries: []\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("lock", "--scope", "organization"); err != nil {
		t.Fatalf("create organization lock: %v\n%s", err, output)
	}
	lockPath := filepath.Join(filepath.Dir(orgPath), "lock.yaml")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	writeSkill(t, working, "mutable-org-skill", "Mutable org", nil)
	commitAndPushAll(t, working, "move org ref")
	stateRoot := filepath.Join(home, "state", "agent-marketplace")
	stateBefore := stateTreeSnapshot(t, stateRoot)

	output, err := executeCommand("deinit", "--scope", "user", "--dry-run")
	if err != nil {
		t.Fatalf("deinit dry-run exact organization view: %v\n%s", err, output)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("deinit dry-run reconciled organization lock:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if stateAfter := stateTreeSnapshot(t, stateRoot); stateAfter != stateBefore {
		t.Fatalf("deinit dry-run changed durable state:\nbefore:\n%s\nafter:\n%s", stateBefore, stateAfter)
	}
}

func TestConfiguredListKeepsLocalRegistryLiveWithoutLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	registry := filepath.Join(t.TempDir(), "registry")
	writeSkill(t, registry, "first", "First", nil)
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `
registries:
  - url: `+registry+`
`)
	withWorkingDir(t, repo)
	writeSkill(t, registry, "second", "Second", nil)
	output, err := executeCommand("list")
	if err != nil {
		t.Fatalf("list local registry: %v\n%s", err, output)
	}
	if !strings.Contains(output, "first") || !strings.Contains(output, "second") {
		t.Fatalf("local registry was not read live:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("local registry unexpectedly required a lock: %v", err)
	}
}
