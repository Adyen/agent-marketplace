package e2e_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A brownfield repository commits a Git lock written by the last pre-v4
// release. Replacing the binary must keep that exact revision while upgrading
// the writable lock to v2 on reconciliation.
func TestPreV4GitLockUpgrade(t *testing.T) {
	if os.Getenv("AM_E2E_PRE_V4_BINARY") == "" {
		t.Skip("historical executable is supplied by the dedicated upgrade gate")
	}
	historical, arch := verifiedExecutable(t, "AM_E2E_PRE_V4_BINARY", preV4ReleaseArch)
	world := newHistoricalWorld(t, upgradeImage(t))
	candidate := world.buildCandidate(arch, "e2e")
	home, workspace := world.path("home"), world.path("workspace")
	for _, dir := range []string{home, filepath.Join(workspace, ".agents")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	registry := world.gitRegistry(map[string]string{"review": "Historical review", "second": "Second resource"})
	gitRun(t, "", "init", "-q", "-b", "main", workspace)
	must := func(executable string, args ...string) {
		t.Helper()
		world.must(home, workspace, executable, args...)
	}
	assertInstalled := func(name, body string) {
		t.Helper()
		got, err := os.ReadFile(filepath.Join(workspace, ".factory", "skills", name, "SKILL.md"))
		if err != nil || string(got) != skillDocument(name, body) {
			t.Fatalf("installed %s = %q, %v; want exact content %q", name, got, err, body)
		}
	}
	lockPath := filepath.Join(workspace, ".agents", "lock.yaml")
	readLock := func() []byte {
		t.Helper()
		lock, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		return lock
	}

	must(historical, "init", "--harnesses", "factory")
	repoConfig := filepath.Join(workspace, ".agents", "config.yaml")
	legacy := fmt.Sprintf("apiVersion: agent-marketplace/v2\nregistries:\n  - url: %s\n    ref: main\n    skills:\n      include: [review]\n", registry.URL)
	if err := os.WriteFile(repoConfig, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	must(historical, "sync")
	assertInstalled("review", "Historical review")
	committed := readLock()
	must(historical, "sync", "--check")
	registry.publish("review", "Upstream moved on")

	must(candidate, "sync", "--locked")
	assertInstalled("review", "Historical review")
	if got := readLock(); !bytes.Equal(got, committed) {
		t.Fatalf("locked sync rewrote the pre-v4 committed lock:\nbefore:\n%s\nafter:\n%s", committed, got)
	}
	must(candidate, "sync")
	assertInstalled("review", "Historical review")
	upgraded := expectV2GitLockUpgrade(t, committed, readLock())
	for _, args := range [][]string{{"sync", "--locked"}, {"sync"}, {"sync", "--check"}} {
		must(candidate, args...)
		assertInstalled("review", "Historical review")
		if got := readLock(); !bytes.Equal(got, upgraded) {
			t.Fatalf("candidate %v rewrote the upgraded lock:\nbefore:\n%s\nafter:\n%s", args, upgraded, got)
		}
	}

	must(candidate, "update", "--scope", "repo")
	assertInstalled("review", "Upstream moved on")
	must(candidate, "migrate", "--scope", "repo")
	if migrated, err := os.ReadFile(repoConfig); err != nil || !strings.Contains(string(migrated), "apiVersion: agent-marketplace/v4") {
		t.Fatalf("repository migration = %q, %v", migrated, err)
	}
	updated := readLock()
	must(candidate, "sync", "--locked")
	assertInstalled("review", "Upstream moved on")
	if got := readLock(); !bytes.Equal(got, updated) {
		t.Fatalf("locked sync after migration rewrote the lock:\nbefore:\n%s\nafter:\n%s", updated, got)
	}
	must(candidate, "install", "second", "--scope", "repo")
	assertInstalled("second", "Second resource")
	must(candidate, "remove", "second", "--scope", "repo")
	if _, err := os.Lstat(filepath.Join(workspace, ".factory", "skills", "second")); !os.IsNotExist(err) {
		t.Fatalf("migrated Git-locked removal left the resource: %v", err)
	}
	must(candidate, "sync", "--check")
	assertInstalled("review", "Upstream moved on")
}

func expectV2GitLockUpgrade(t *testing.T, original, actual []byte) []byte {
	t.Helper()
	v1 := []byte("apiVersion: agent-marketplace-lock/v1\n")
	if bytes.Count(original, v1) != 1 {
		t.Fatalf("historical lock does not declare v1 exactly once:\n%s", original)
	}
	expected := bytes.Replace(original, v1, []byte("apiVersion: agent-marketplace-lock/v2\n"), 1)
	if !bytes.Equal(actual, expected) {
		t.Fatalf("lock upgrade changed more than the version:\nbefore:\n%s\nafter:\n%s", original, actual)
	}
	return expected
}
