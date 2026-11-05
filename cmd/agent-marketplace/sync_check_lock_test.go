package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/ownership"
)

// This file is the command-level half of bead agent-marketplace-wrp8: the Devin
// aggregate reconciler now holds a per-canonical-root advisory lock across its whole
// read-modify-commit, and the READ-ONLY commands must be untouched by that.
//
// The property is easy to break and expensive to lose. `status` runs on a human's
// prompt and the TUI refreshes in the background, so either blocking behind a
// long-running `sync` or creating state on a read would be a visible regression —
// and creating `locks/` on a read would also turn every first-ever run's silent
// first-use path into an ownership.state-missing warning, because
// ownership.Store.EverRecorded reads the state directories as durable evidence.
//
// The contention is manufactured, not raced: this test takes the lock itself, with
// its own Store, and therefore its own open file description. A flock belongs to
// the description rather than to the process, so the command under test genuinely
// contends with it even though both run here.

// devinLockRepo seeds a repo whose config selects the Devin harness and one MCP
// server, syncs it once, and returns the repo root and the state root.
func devinLockRepo(t *testing.T) (repoRoot, stateRoot string) {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "devin-lock.git")
	writeSkill(t, working, "keeper", "Survives a held lock", nil)
	writeFile(t, filepath.Join(working, ".mcp.json"), `{
  "mcpServers": {
    "good": {"command": "npx"}
  }
}`)
	commitAndPushAll(t, working, "Add devin fixtures")

	repoRoot = filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses:
  - devin
sources:
  origin: {git: file://`+origin+`, ref: main}
resources:
  origin: [skill/keeper, mcp/good]
`)
	withWorkingDir(t, repoRoot)
	return repoRoot, filepath.Join(homeDir, "state", "agent-marketplace")
}

// TestReadOnlyCommandsNeitherBlockNorCreateStateWhileTheLockIsHeld covers
// `sync --check` and `status` while another writer provably holds the root's
// ownership lock, with the DEFAULT 30 s timeout — so a command that acquired would
// take half a minute and then fail rather than flake.
func TestReadOnlyCommandsNeitherBlockNorCreateStateWhileTheLockIsHeld(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, stateRoot := devinLockRepo(t)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync: %v\noutput:\n%s", err, output)
	}

	// A separate Store, therefore a separate open file description, therefore
	// genuine mutual exclusion against the commands below.
	holder, err := ownership.Open(filepath.Join(repoRoot, ".devin"))
	if err != nil {
		t.Fatalf("open holder store: %v", err)
	}
	release, err := holder.Lock()
	if err != nil {
		t.Fatalf("hold the root lock: %v", err)
	}
	defer release()
	if !holder.HoldsLock() {
		t.Fatal("the holder does not hold the lock, so this test would prove nothing")
	}

	before := stateTreeSnapshot(t, stateRoot)
	for _, args := range [][]string{{"sync", "--check"}, {"status"}} {
		name := strings.Join(args, " ")
		started := time.Now()
		output, err := executeCommand(args...)
		elapsed := time.Since(started)
		// `sync --check` exits 1 on drift and 0 when clean; either is fine. What
		// must not happen is a locking failure or a wait.
		if err != nil && strings.Contains(err.Error(), "lock") {
			t.Fatalf("%s reported a locking failure: %v\noutput:\n%s", name, err, output)
		}
		if elapsed > 20*time.Second {
			t.Fatalf("%s took %s; it is waiting on the per-root ownership lock", name, elapsed)
		}
		for _, code := range []string{"ownership.lock-timeout", "ownership.lock-contended"} {
			if strings.Contains(output, code) {
				t.Fatalf("%s reported %s; read-only commands must not take the lock:\n%s", name, code, output)
			}
		}
		if after := stateTreeSnapshot(t, stateRoot); after != before {
			t.Fatalf("%s changed the durable state root:\nbefore:\n%s\nafter:\n%s", name, before, after)
		}
	}
}

// TestSyncCheckFirstEverLocalDirectoryOnlyCreatesNoCacheOrState proves a
// check with no exact Git consumers does not bootstrap coordination metadata.
func TestSyncCheckFirstEverLocalDirectoryOnlyCreatesNoCacheOrState(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	cacheRoot := filepath.Join(home, "cache")
	stateRoot := filepath.Join(home, "state")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", cacheRoot)
	t.Setenv("XDG_STATE_HOME", stateRoot)

	registry := filepath.Join(t.TempDir(), "registry")
	writeSkill(t, registry, "local-only", "Local only", nil)
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  local: {directory: `+registry+`}
resources:
  local: [skill/local-only]
`)
	withWorkingDir(t, repo)

	output, err := executeCommand("sync", "--check")
	if err == nil || !strings.Contains(err.Error(), "out of sync") {
		t.Fatalf("sync --check error = %v, want materialization drift\n%s", err, output)
	}
	for _, root := range []string{cacheRoot, stateRoot} {
		if _, statErr := os.Lstat(root); !os.IsNotExist(statErr) {
			t.Fatalf("sync --check created read-only root %s (%v)", root, statErr)
		}
	}
}

// TestReadOnlyCommandsOnAFirstEverRunCreateNoStateRootAtAll proves read-only
// commands do not create durable ownership state before the first sync.
func TestReadOnlyCommandsOnAFirstEverRunCreateNoStateRootAtAll(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, stateRoot := devinLockRepo(t)

	for _, args := range [][]string{{"sync", "--check"}, {"status"}} {
		name := strings.Join(args, " ")
		output, err := executeCommand(args...)
		if err != nil && strings.Contains(err.Error(), "ownership.lock") {
			t.Fatalf("%s reported an ownership locking failure: %v\noutput:\n%s", name, err, output)
		}
		if args[0] == "sync" && (err == nil || !strings.Contains(err.Error(), "lock.missing")) {
			t.Fatalf("%s must report missing lock freshness on a first run: %v\noutput:\n%s", name, err, output)
		}
		if _, statErr := os.Lstat(stateRoot); !os.IsNotExist(statErr) {
			t.Fatalf("%s created the durable state root %s (%v)", name, stateRoot, statErr)
		}
	}
}

// stateTreeSnapshot renders every path under the state root with its size, so a
// test can assert that a read-only command changed nothing at all.
func stateTreeSnapshot(t *testing.T, root string) string {
	t.Helper()
	var sb strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if info.IsDir() {
			fmt.Fprintf(&sb, "%s dir\n", rel)
			return nil
		}
		fmt.Fprintf(&sb, "%s %d\n", rel, info.Size())
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return sb.String()
}
