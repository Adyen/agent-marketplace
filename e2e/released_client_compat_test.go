package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// releasedClientArch maps the checksum of each pinned latest-release Linux
// asset to its architecture. Bump these, the CI download, and the version
// label after every stable release.
const releasedClientVersion = "v4.1.4"

var releasedClientArch = map[string]string{
	"7b6f49e7565b9c9ab08ecdf1900cb3228926ae59a9d14d91a27db90a6936adb2": "amd64",
	"2749c5ede81222d389e2a1043e9397f6fb0c0d753842770363c61a6583486379": "arm64",
}

// spec: hook-resources/latest-release-still-verifies-after-migration
// A teammate on the candidate syncs a committed Git-only v2 lock and
// migrates a hook the release installed in its legacy layout into the
// native file. The latest release reads v2 locks, so its sync --locked and
// sync --check must keep succeeding without rewriting the healthy lock,
// recreating the legacy file, or duplicating the hook. Each actor has its
// own HOME; only the repository is shared.
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
	legacySource := filepath.Join(registry.source, "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(legacySource), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacySource, []byte(`{"hooks":{"PreToolUse":[{"type":"command","matcher":"Bash","command":"echo released-legacy-hook"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	registry.commit("add hooks")
	gitRun(t, registry.source, "push", "-q", "origin", "main")
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
	legacyHook := filepath.Join(workspace, ".factory", "hooks", "hooks.json")
	nativeHook := filepath.Join(workspace, ".factory", "hooks.json")
	// Teammates and CI fetch the committed exact snapshot with --locked; the
	// check-only mode is deliberately offline and runs afterwards. Neither
	// may rewrite a healthy lock.
	var migratedNative []byte
	var migratedMode os.FileMode
	var migratedMtime time.Time
	assertMigratedHook := func(stage string) {
		t.Helper()
		data, err := os.ReadFile(nativeHook)
		if err != nil {
			t.Fatalf("%s: candidate did not converge the hook into the native file: %v", stage, err)
		}
		var document map[string]json.RawMessage
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatalf("%s: native hook file is not a Factory event map: %v\n%s", stage, err, data)
		}
		if _, wrapped := document["hooks"]; wrapped {
			t.Fatalf("%s: native hook file wraps the event map in a hooks member:\n%s", stage, data)
		}
		var groups []struct {
			Matcher string           `json:"matcher"`
			Hooks   []map[string]any `json:"hooks"`
		}
		if err := json.Unmarshal(document["PreToolUse"], &groups); err != nil {
			t.Fatalf("%s: native PreToolUse member is not a group array: %v\n%s", stage, err, data)
		}
		if len(groups) != 1 || groups[0].Matcher != "Execute" {
			t.Fatalf("%s: native PreToolUse groups = %+v, want exactly one Execute group:\n%s", stage, groups, data)
		}
		if len(groups[0].Hooks) != 1 || groups[0].Hooks[0]["type"] != "command" || groups[0].Hooks[0]["command"] != "echo released-legacy-hook" {
			t.Fatalf("%s: native Execute handlers = %+v, want exactly one command handler echo released-legacy-hook:\n%s", stage, groups[0].Hooks, data)
		}
		if bytes.Contains(data, []byte("_managed_by")) {
			t.Fatalf("%s: native hook file carries an in-file ownership marker:\n%s", stage, data)
		}
		info, err := os.Stat(nativeHook)
		if err != nil {
			t.Fatalf("%s: stat native hook file: %v", stage, err)
		}
		if migratedNative == nil {
			migratedNative = data
			migratedMode = info.Mode()
			migratedMtime = info.ModTime()
		} else if !bytes.Equal(data, migratedNative) || info.Mode() != migratedMode || !info.ModTime().Equal(migratedMtime) {
			t.Fatalf("%s: native hook file bytes, mode, or mtime changed after migration:\nbefore:\n%s\nafter:\n%s", stage, migratedNative, data)
		}
		if _, err := os.Lstat(legacyHook); !os.IsNotExist(err) {
			t.Fatalf("%s: legacy hook file still present or recreated after candidate migration: %v", stage, err)
		}
	}
	releasedReads := func(after string) {
		t.Helper()
		lock := readLock()
		for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}} {
			output, err := run(released, args...)
			if err != nil {
				t.Fatalf("%s %v cannot use state committed after candidate %s: %v\n%s", releasedClientVersion, args, after, err, output)
			}
			if got := readLock(); !bytes.Equal(got, lock) {
				t.Fatalf("%s %v rewrote a healthy lock after %s:\nbefore:\n%s\nafter:\n%s", releasedClientVersion, args, after, lock, got)
			}
			if migratedNative != nil {
				assertMigratedHook(fmt.Sprintf("%s %v", args, after))
			}
		}
	}

	must(released, "init", "--harnesses", "factory")
	must(candidate, "init", "--harnesses", "factory")
	repoConfig := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {shared: {git: %q, ref: main}}\nresources: {shared: {include: [skill/review, hook/PreToolUse-command]}}\n", registry.URL)
	if err := os.WriteFile(filepath.Join(workspace, ".agents", "config.yaml"), []byte(repoConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	must(released, "sync")
	legacy, err := os.ReadFile(legacyHook)
	if err != nil || !bytes.Contains(legacy, []byte("released-legacy-hook")) {
		t.Fatalf("%s did not seed its legacy hook output: %v\n%s", releasedClientVersion, err, legacy)
	}
	committed := readLock()
	releasedReads("initial sync")

	must(candidate, "sync")
	if got := readLock(); !bytes.Equal(got, committed) {
		t.Fatalf("a healthy candidate sync rewrote the committed v2 lock:\nbefore:\n%s\nafter:\n%s", committed, got)
	}
	assertMigratedHook("candidate sync")
	releasedReads("candidate hook migration")

	must(candidate, "sync")
	if got := readLock(); !bytes.Equal(got, committed) {
		t.Fatalf("a repeat healthy candidate sync rewrote the lock:\nbefore:\n%s\nafter:\n%s", committed, got)
	}
	assertMigratedHook("repeat candidate sync")

	must(candidate, "install", "second", "--scope", "repo")
	if _, err := os.Stat(filepath.Join(workspace, ".factory", "skills", "second", "SKILL.md")); err != nil {
		t.Fatalf("candidate did not materialize its committed selection: %v", err)
	}
	assertMigratedHook("install")
	releasedReads("install")

	registry.publish("review", "Review body, revised")
	must(candidate, "update", "--scope", "repo")
	assertMigratedHook("update")
	releasedReads("update")

	must(candidate, "remove", "second", "--scope", "repo")
	assertMigratedHook("remove")
	releasedReads("remove")
	must(candidate, "sync", "--locked")
	must(candidate, "sync", "--check")
}

// Each actor has its own HOME and checkout. Only committed configuration and
// lock state are shared, never generated output or cache-backed symlinks.
// spec: regression-gates/released-client-remains-usable-after-candidate-mutations
func TestReleasedClientReadsCandidateIsolatedCommittedState(t *testing.T) {
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
	lockPath := filepath.Join(candidateWorkspace, ".agents", "lock.yaml")
	readLock := func() []byte {
		t.Helper()
		lock, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		return lock
	}
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
	must(released, "init", "--harnesses", "factory")
	repoConfig := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {shared: {git: %q, ref: main}}\nresources: {shared: {include: [skill/review]}}\n", registry.URL)
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
