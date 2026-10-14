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
const releasedClientVersion = "v4.1.3"

var releasedClientArch = map[string]string{
	"f29846a258081d2789d07263a5b5919a770c631f6c8dec69ed5c7e0f0fc59aeb": "amd64",
	"e4bca90471bd1b8f0cef82c804c9755fd5d27a92065584e837c47cbf227ea02a": "arm64",
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
	for _, version := range []string{"agent-marketplace/v2", "agent-marketplace/v4"} {
		t.Run(version, func(t *testing.T) {
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
			protectClientFiles := func(executable string) map[string]string {
				t.Helper()
				protected := map[string]string{}
				for _, name := range []string{"config.yaml", "lock.yaml"} {
					path := filepath.Join(workspaces[executable], ".agents", name)
					fingerprint, err := pathFingerprint(path)
					if err != nil {
						t.Fatal(err)
					}
					protected[path] = fingerprint
				}
				return protected
			}
			assertClientFilesUnchanged := func(protected map[string]string) {
				t.Helper()
				for path, fingerprint := range protected {
					if got, err := pathFingerprint(path); err != nil || got != fingerprint {
						t.Fatalf("changed client config/lock bytes, mode or mtime at %s: %v", path, err)
					}
				}
			}
			reviewBody := "Review body"
			secondSelected := false
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
				protected := protectClientFiles(released)
				for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}, {"sync", "--check", "--offline"}} {
					if output, err := run(released, args...); err != nil {
						t.Fatalf("%s %v cannot use state committed after candidate %s: %v\n%s", releasedClientVersion, args, after, err, output)
					}
					if got, err := os.ReadFile(lockPath); err != nil || !bytes.Equal(got, before) {
						t.Fatalf("%s %v changed committed lock after %s: %v", releasedClientVersion, args, after, err)
					}
					assertClientFilesUnchanged(protected)
					reviewPath := filepath.Join(workspace, ".factory", "skills", "review", "SKILL.md")
					if got, err := os.ReadFile(reviewPath); err != nil || string(got) != skillDocument("review", reviewBody) {
						t.Fatalf("%s %v did not materialize review after %s: %v\n%s", releasedClientVersion, args, after, err, got)
					}
					second := filepath.Join(workspace, ".factory", "skills", "second")
					if secondSelected {
						if got, err := os.ReadFile(filepath.Join(second, "SKILL.md")); err != nil || string(got) != skillDocument("second", "Second body") {
							t.Fatalf("%s %v did not materialize second after %s: %v\n%s", releasedClientVersion, args, after, err, got)
						}
					} else if absent, err := checkPathAbsent(second); err != nil || !absent {
						t.Fatalf("%s %v left second after %s: absent=%t err=%v", releasedClientVersion, args, after, absent, err)
					}
				}
			}
			must(released, "init", "--harnesses", "factory")
			repoConfig := fmt.Sprintf("apiVersion: %s\nregistries:\n  - url: %s\n    ref: main\n    skills:\n      include: [review]\n", version, registry.URL)
			if version == "agent-marketplace/v4" {
				repoConfig = fmt.Sprintf("apiVersion: %s\nsources:\n  tools: {git: '%s', ref: main}\nresources:\n  tools: {include: [skill/review]}\n", version, registry.URL)
			}
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
			candidateProtected := protectClientFiles(candidate)
			must(candidate, "sync")
			if got := readLock(); !bytes.Equal(got, committed) {
				t.Fatalf("plain candidate sync changed a healthy committed lock:\nbefore:\n%s\nafter:\n%s", committed, got)
			}
			assertClientFilesUnchanged(candidateProtected)
			releasedReads("sync")

			must(candidate, "install", "second", "--scope", "repo")
			if _, err := os.Stat(filepath.Join(candidateWorkspace, ".factory", "skills", "second", "SKILL.md")); err != nil {
				t.Fatalf("candidate did not materialize its committed selection: %v", err)
			}
			commit("install")
			secondSelected = true
			releasedReads("install")

			registry.publish("review", "Review body, revised")
			updateOutput := must(candidate, "update", "--scope", "repo")
			reviewPath := filepath.Join(candidateWorkspace, ".factory", "skills", "review", "SKILL.md")
			if got, err := os.ReadFile(reviewPath); err != nil || string(got) != skillDocument("review", "Review body, revised") {
				t.Fatalf("candidate update did not materialize revised review: %v\n%s\nupdate:\n%s", err, got, updateOutput)
			}
			commit("update")
			reviewBody = "Review body, revised"
			releasedReads("update")

			must(candidate, "remove", "second", "--scope", "repo")
			if _, err := os.Lstat(filepath.Join(candidateWorkspace, ".factory", "skills", "second")); !os.IsNotExist(err) {
				t.Fatalf("candidate remove left second installed: %v", err)
			}
			commit("remove")
			secondSelected = false
			releasedReads("remove")
			must(candidate, "sync", "--locked")
			must(candidate, "sync", "--check")
			must(candidate, "sync", "--check", "--offline")
			checkCacheOnly := func(executable string, missing bool) {
				t.Helper()
				protected := protectClientFiles(executable)
				for _, args := range [][]string{{"sync", "--check"}, {"sync", "--check", "--offline"}} {
					output, err := run(executable, args...)
					if missing {
						code := "lock.snapshot-unavailable"
						if len(args) == 3 {
							code = "lock.offline-snapshot-missing"
						}
						if err == nil || !strings.Contains(output, code) {
							t.Fatalf("%s %v did not refuse the missing cache despite an accessible origin: %v\n%s", filepath.Base(executable), args, err, output)
						}
						cachePath := filepath.Join(homes[executable], ".cache", "agent-marketplace")
						if absent, err := checkPathAbsent(cachePath); err != nil || !absent {
							t.Fatalf("read-only %v recreated the missing cache: absent=%t err=%v", args, absent, err)
						}
					} else if err != nil {
						t.Fatalf("%s %v could not use its warm cache: %v\n%s", filepath.Base(executable), args, err, output)
					}
					assertClientFilesUnchanged(protected)
				}
			}
			bare, hidden := world.path("registry.git"), world.path("registry-hidden.git")
			if err := os.Rename(bare, hidden); err != nil {
				t.Fatal(err)
			}
			for _, executable := range []string{candidate, released} {
				checkCacheOnly(executable, false)
			}
			if err := os.Rename(hidden, bare); err != nil {
				t.Fatal(err)
			}
			for _, executable := range []string{candidate, released} {
				warmHome := homes[executable]
				coldHome := warmHome + "-cold"
				config, err := os.ReadFile(filepath.Join(warmHome, ".agents", "config.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(coldHome, ".agents"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(coldHome, ".agents", "config.yaml"), config, 0o600); err != nil {
					t.Fatal(err)
				}
				homes[executable] = coldHome
				checkCacheOnly(executable, true)
				homes[executable] = warmHome
				checkCacheOnly(executable, false)
			}
		})
	}
}
