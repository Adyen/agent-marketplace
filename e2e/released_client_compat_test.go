package e2e_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// releasedClientArch maps the checksum of each pinned latest-release Linux
// asset to its architecture. Bump these, the CI download, and the version
// label after every stable release.
const releasedClientVersion = "v4.1.1"

var releasedClientArch = map[string]string{
	"188f48669e15bdd8286c25cc43f601afc2bae9408ee26ebca9d47bfd7bc573cc": "amd64",
	"d8523397babe0fa057f8a46749d1e3bdba959409d5eae5fc0feced1e3f1de85b": "arm64",
}

// Each actor has its own HOME and checkout. Only committed configuration and
// lock state are shared, never generated output or cache-backed symlinks.
// spec: regression-gates/released-client-remains-usable-after-candidate-mutations
func TestReleasedClientReadsCandidateCommittedState(t *testing.T) {
	image := os.Getenv("AM_E2E_RELEASED_IMAGE")
	if os.Getenv("AM_E2E_RELEASED_BINARY") == "" || image == "" {
		t.Skip("released executable and git-capable image are supplied by the dedicated released-client gate")
	}
	released, arch := verifiedExecutable(t, "AM_E2E_RELEASED_BINARY", releasedClientArch)
	world := newHistoricalWorld(t, image)
	candidate := world.buildCandidate(arch)
	workspace := world.path("workspace-released")
	candidateWorkspace := world.path("workspace-candidate")
	for _, dir := range []string{filepath.Join(workspace, ".agents"), world.path("home-released"), world.path("home-candidate")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	registry := world.gitRegistry(map[string]string{"review": "Review body", "second": "Second body"})
	gitRun(t, "", "init", "-q", "-b", "main", workspace)
	excludeOutputs := func(checkout string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(checkout, ".git", "info", "exclude"), []byte(".factory/\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	excludeOutputs(workspace)

	homes := map[string]string{released: world.path("home-released"), candidate: world.path("home-candidate")}
	workspaces := map[string]string{released: workspace, candidate: candidateWorkspace}
	run := func(executable string, args ...string) (string, error) {
		t.Helper()
		return world.run(homes[executable], workspaces[executable], executable, args...)
	}
	must := func(executable string, args ...string) string {
		t.Helper()
		return world.must(homes[executable], workspaces[executable], executable, args...)
	}
	// Teammates and CI fetch the committed exact snapshot with --locked; the
	// check-only mode is deliberately offline and runs afterwards.
	releasedReads := func(after string) {
		t.Helper()
		gitRun(t, workspace, "fetch", "-q", "origin")
		gitRun(t, workspace, "merge", "-q", "--ff-only", "origin/main")
		lockPath := filepath.Join(workspace, ".agents", "lock.yaml")
		before, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}} {
			if output, err := run(released, args...); err != nil {
				t.Fatalf("%s %v cannot use state committed after candidate %s: %v\n%s", releasedClientVersion, args, after, err, output)
			}
			if got, err := os.ReadFile(lockPath); err != nil || !bytes.Equal(got, before) {
				t.Fatalf("%s %v changed committed lock after %s: %v", releasedClientVersion, args, after, err)
			}
		}
	}
	lockPath := filepath.Join(candidateWorkspace, ".agents", "lock.yaml")
	readLock := func() []byte {
		t.Helper()
		lock, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		return lock
	}
	must(released, "init", "--harnesses", "factory")
	repoConfig := fmt.Sprintf("apiVersion: agent-marketplace/v2\nregistries:\n  - url: %s\n    ref: main\n    skills:\n      include: [review]\n", registry.URL)
	if err := os.WriteFile(filepath.Join(workspace, ".agents", "config.yaml"), []byte(repoConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	must(released, "sync")
	(&gitRegistry{t: t, source: workspace}).commit("initial released configuration and lock")
	gitRun(t, "", "clone", "-q", workspace, candidateWorkspace)
	excludeOutputs(candidateWorkspace)
	gitRun(t, workspace, "remote", "add", "origin", candidateWorkspace)
	must(candidate, "init", "--harnesses", "factory")
	committed := readLock()
	releasedReads("initial sync")

	commit := func(after string) {
		t.Helper()
		(&gitRegistry{t: t, source: candidateWorkspace}).commit("candidate " + after)
	}
	must(candidate, "sync")
	if got := readLock(); !bytes.Equal(got, committed) {
		t.Fatalf("plain candidate sync changed a healthy committed lock:\nbefore:\n%s\nafter:\n%s", committed, got)
	}
	releasedReads("sync")

	must(candidate, "install", "second", "--scope", "repo")
	if _, err := os.Stat(filepath.Join(candidateWorkspace, ".factory", "skills", "second", "SKILL.md")); err != nil {
		t.Fatalf("candidate did not materialize its committed selection: %v", err)
	}
	commit("install")
	releasedReads("install")

	registry.publish("review", "Review body, revised")
	updateOutput := must(candidate, "update", "--scope", "repo")
	reviewPath := filepath.Join(candidateWorkspace, ".factory", "skills", "review", "SKILL.md")
	if got, err := os.ReadFile(reviewPath); err != nil || string(got) != skillDocument("review", "Review body, revised") {
		t.Fatalf("candidate update did not materialize revised review: %v\n%s\nupdate:\n%s", err, got, updateOutput)
	}
	commit("update")
	releasedReads("update")

	must(candidate, "remove", "second", "--scope", "repo")
	if _, err := os.Lstat(filepath.Join(candidateWorkspace, ".factory", "skills", "second")); !os.IsNotExist(err) {
		t.Fatalf("candidate remove left second installed: %v", err)
	}
	commit("remove")
	releasedReads("remove")
	must(candidate, "sync", "--locked")
	must(candidate, "sync", "--check")
}
