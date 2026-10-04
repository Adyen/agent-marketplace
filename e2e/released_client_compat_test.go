package e2e_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// releasedClientArch maps the checksum of each pinned latest-release Linux
// asset to its architecture. Bump these, the CI download, and the version
// label after every stable release.
const releasedClientVersion = "v4.0.11"

var releasedClientArch = map[string]string{
	"38c248f3652e37f0af1c66e28d2fecbc1f3ca26dbb3f94a5278ce375041bfcc0": "amd64",
	"8dd530887e2002c22b2e9d8b3ef4cf0afb8889a05bd6c434be70bf6630fcf754": "arm64",
}

// A teammate on the candidate changes a committed Git-only v2 lock.
// The latest release still reads each committed generation without changing it.
// Each actor has its own HOME; only the repository is shared.
func TestReleasedClientReadsCandidateCommittedState(t *testing.T) {
	image := os.Getenv("AM_E2E_RELEASED_IMAGE")
	if os.Getenv("AM_E2E_RELEASED_BINARY") == "" || image == "" {
		t.Skip("released executable and git-capable image are supplied by the dedicated released-client gate")
	}
	released, arch := verifiedExecutable(t, "AM_E2E_RELEASED_BINARY", releasedClientArch)
	world := newHistoricalWorld(t, image)
	candidate := world.buildCandidate(arch)
	workspace := world.path("workspace")
	for _, dir := range []string{filepath.Join(workspace, ".agents"), world.path("home-released"), world.path("home-candidate")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	registry := world.gitRegistry(map[string]string{"review": "Review body", "second": "Second body"})
	gitRun(t, "", "init", "-q", "-b", "main", workspace)

	homes := map[string]string{released: world.path("home-released"), candidate: world.path("home-candidate")}
	run := func(executable string, args ...string) (string, error) {
		t.Helper()
		return world.run(homes[executable], workspace, executable, args...)
	}
	must := func(executable string, args ...string) {
		t.Helper()
		world.must(homes[executable], workspace, executable, args...)
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
	// Teammates and CI fetch the committed exact snapshot with --locked; the
	// check-only mode is deliberately offline and runs afterwards.
	releasedReads := func(after string) {
		t.Helper()
		before := readLock()
		for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}} {
			if output, err := run(released, args...); err != nil {
				t.Fatalf("%s %v cannot use state committed after candidate %s: %v\n%s", releasedClientVersion, args, after, err, output)
			}
			if current := readLock(); !bytes.Equal(current, before) {
				t.Fatalf("%s %v changed the committed lock after %s", releasedClientVersion, args, after)
			}
		}
	}
	must(released, "init", "--harnesses", "factory")
	must(candidate, "init", "--harnesses", "factory")
	repoConfig := fmt.Sprintf("apiVersion: agent-marketplace/v2\nregistries:\n  - url: %s\n    ref: main\n    skills:\n      include: [review]\n", registry.URL)
	if err := os.WriteFile(filepath.Join(workspace, ".agents", "config.yaml"), []byte(repoConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	must(released, "sync")
	committed := readLock()
	if !strings.Contains(string(committed), "apiVersion: agent-marketplace-lock/v2") {
		t.Fatalf("%s did not write the supported v2 lock", releasedClientVersion)
	}
	releasedReads("initial released sync")

	must(candidate, "sync")
	if current := readLock(); !bytes.Equal(current, committed) {
		t.Fatalf("candidate sync changed a healthy committed lock:\nbefore:\n%s\nafter:\n%s", committed, current)
	}
	releasedReads("sync")

	must(candidate, "install", "second", "--scope", "repo")
	if _, err := os.Stat(filepath.Join(workspace, ".factory", "skills", "second", "SKILL.md")); err != nil {
		t.Fatalf("candidate did not materialize its committed selection: %v", err)
	}
	releasedReads("install")

	registry.publish("review", "Review body, revised")
	must(candidate, "update", "--scope", "repo")
	releasedReads("update")

	must(candidate, "remove", "second", "--scope", "repo")
	releasedReads("remove")
	must(candidate, "sync", "--locked")
	must(candidate, "sync", "--check")
}
