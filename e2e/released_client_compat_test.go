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
const releasedClientVersion = "v4.0.8"

var releasedClientArch = map[string]string{
	"02a34b1786ffbe2cf2d19f5163a9383ab5712c61db44636abc803771654665bb": "amd64",
	"51829f84d90da1035b983f24cf7f02a9c93c9d3aae32e2846ae4fcd10768370b": "arm64",
}

// A teammate on the candidate upgrades a committed Git-only lock to v2.
// The latest release can read v1, but must refuse v2 without altering the
// repository. Each actor has its own HOME; only the repository is shared.
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
	// Teammates and CI fetch the committed exact snapshot with --locked; the
	// check-only mode is deliberately offline and runs afterwards.
	releasedReads := func(after string) {
		t.Helper()
		for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}} {
			if output, err := run(released, args...); err != nil {
				t.Fatalf("%s %v cannot use state committed after candidate %s: %v\n%s", releasedClientVersion, args, after, err, output)
			}
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
	releasedRejects := func(after string) {
		t.Helper()
		lock := readLock()
		for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}} {
			output, err := run(released, args...)
			if err == nil || !strings.Contains(output, "lock.invalid-document") || !strings.Contains(output, `apiVersion "agent-marketplace-lock/v2"`) {
				t.Fatalf("%s %v after candidate %s did not reject Git-only v2: %v\n%s", releasedClientVersion, args, after, err, output)
			}
			if got := readLock(); !bytes.Equal(got, lock) {
				t.Fatalf("%s %v changed the unsupported lock after %s:\nbefore:\n%s\nafter:\n%s", releasedClientVersion, args, after, lock, got)
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
	releasedReads("initial v1 sync")

	must(candidate, "sync")
	expectV2GitLockUpgrade(t, committed, readLock())
	releasedRejects("sync")

	must(candidate, "install", "second", "--scope", "repo")
	if _, err := os.Stat(filepath.Join(workspace, ".factory", "skills", "second", "SKILL.md")); err != nil {
		t.Fatalf("candidate did not materialize its committed selection: %v", err)
	}
	releasedRejects("install")

	registry.publish("review", "Review body, revised")
	must(candidate, "update", "--scope", "repo")
	releasedRejects("update")

	must(candidate, "remove", "second", "--scope", "repo")
	releasedRejects("remove")
	must(candidate, "sync", "--locked")
	must(candidate, "sync", "--check")
}
