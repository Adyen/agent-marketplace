package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
	for _, version := range []string{"agent-marketplace/v4"} {
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
			assertClientOutputs := func(executable string) {
				t.Helper()
				reviewPath := filepath.Join(workspaces[executable], ".factory", "skills", "review", "SKILL.md")
				if got, err := os.ReadFile(reviewPath); err != nil || string(got) != skillDocument("review", reviewBody) {
					t.Fatalf("%s has incorrect review output: %v\n%s", filepath.Base(executable), err, got)
				}
				second := filepath.Join(workspaces[executable], ".factory", "skills", "second")
				if secondSelected {
					if got, err := os.ReadFile(filepath.Join(second, "SKILL.md")); err != nil || string(got) != skillDocument("second", "Second body") {
						t.Fatalf("%s has incorrect second output: %v\n%s", filepath.Base(executable), err, got)
					}
				} else if absent, err := checkPathAbsent(second); err != nil || !absent {
					t.Fatalf("%s left second installed: absent=%t err=%v", filepath.Base(executable), absent, err)
				}
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
				protected := protectClientFiles(released)
				for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}, {"sync", "--check", "--offline"}} {
					if output, err := run(released, args...); err != nil {
						t.Fatalf("%s %v cannot use state committed after candidate %s: %v\n%s", releasedClientVersion, args, after, err, output)
					}
					if got, err := os.ReadFile(lockPath); err != nil || !bytes.Equal(got, before) {
						t.Fatalf("%s %v changed committed lock after %s: %v", releasedClientVersion, args, after, err)
					}
					assertClientFilesUnchanged(protected)
					assertClientOutputs(released)
				}
			}
			must(released, "init", "--harnesses", "factory")
			repoConfig := fmt.Sprintf("apiVersion: %s\nsources:\n  tools: {git: '%s', ref: main}\nresources:\n  tools: {include: [skill/review]}\n", version, registry.URL)
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
				nativeRoot := filepath.Join(workspaces[executable], ".factory", "skills")
				nativeBefore, err := pathFingerprint(nativeRoot)
				if err != nil {
					t.Fatal(err)
				}
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
					} else {
						assertClientOutputs(executable)
					}
					assertClientFilesUnchanged(protected)
					if got, err := pathFingerprint(nativeRoot); err != nil || got != nativeBefore {
						t.Fatalf("read-only %v changed native output: %v", args, err)
					}
				}
			}
			renameFixturePath := func(from, to string) {
				t.Helper()
				for _, path := range []string{from, to} {
					rel, err := filepath.Rel(world.root, path)
					if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
						t.Fatalf("fixture rename escapes sandbox: %s", path)
					}
				}
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				defer cancel()
				args := []string{"run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges"}
				if runtime.GOOS == "linux" {
					args = append(args, "--user", strconv.Itoa(os.Getuid())+":"+strconv.Itoa(os.Getgid()))
				}
				args = append(args, "--mount", "type=bind,src="+world.root+",dst="+world.root, "--entrypoint", "sh", world.image,
					"-c", `test ! -e "$2" && test ! -L "$2" && mv -- "$1" "$2" && test ! -e "$1" && test ! -L "$1"`, "fixture-rename", from, to)
				if output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
					t.Fatalf("rename fixture %s to %s in consumer filesystem: %v\n%s", from, to, err, output)
				}
			}
			bare, hidden := world.path("registry.git"), world.path("registry-hidden.git")
			renameFixturePath(bare, hidden)
			for _, executable := range []string{candidate, released} {
				checkCacheOnly(executable, false)
				protected := protectClientFiles(executable)
				nativeRoot := filepath.Join(workspaces[executable], ".factory", "skills")
				review := filepath.Join(nativeRoot, "review")
				renameFixturePath(review, world.path("removed-review-"+filepath.Base(executable)))
				missingBefore, err := pathFingerprint(nativeRoot)
				if err != nil {
					t.Fatal(err)
				}
				output, err := run(executable, "sync", "--check", "--offline")
				if err == nil || !strings.Contains(output, "out of sync") || !strings.Contains(output, "review") {
					t.Fatalf("%s check accepted missing native output: %v\n%s", filepath.Base(executable), err, output)
				}
				if absent, err := checkPathAbsent(review); err != nil || !absent {
					t.Fatalf("read-only check recreated review: absent=%t err=%v", absent, err)
				}
				if got, err := pathFingerprint(nativeRoot); err != nil || got != missingBefore {
					t.Fatalf("read-only check changed the missing-output tree: %v", err)
				}
				assertClientFilesUnchanged(protected)
				must(executable, "sync", "--offline")
				assertClientFilesUnchanged(protected)
				assertClientOutputs(executable)
				checkCacheOnly(executable, false)
			}
			renameFixturePath(hidden, bare)
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

// spec: regression-gates/released-client-remains-usable-after-candidate-mutations
func TestPersonalLocalClaudePluginSyncLeavesCommittedWorkspaceUnchanged(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	registry := env.CreateBareRepo("shared", map[string]string{
		"skills/review/SKILL.md": "---\nname: review\ndescription: Committed skill\n---\nCommitted body.\n",
		"hooks/hooks.json":       `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo committed-claude-hook"}]}]}}`,
	})
	env.WriteRepoConfig(fmt.Sprintf(`apiVersion: agent-marketplace/v4
harnesses: [claude]
sources:
  shared: {git: %q, ref: main}
resources:
  shared: {include: [skill/review, hook/PreToolUse-command]}
`, registry))
	initial := env.RunAM("sync")
	if initial.ExitCode != 0 {
		t.Fatalf("initial committed Claude sync failed (exit %d):\n%s", initial.ExitCode, initial.Combined)
	}
	env.MustRun(`git add -A && git -c user.name=E2E -c user.email=e2e@example.com commit -q -m "committed Claude baseline"`)
	if tracked := env.Run("git ls-files --error-unmatch .claude/skills/review"); tracked.ExitCode != 0 {
		t.Fatalf("committed Claude skill was not tracked before personal-local sync:\n%s", tracked.Combined)
	}

	env.WriteFile("/workspace/vendor/native-kit/.claude-plugin/plugin.json", `{"name":"native-kit"}`)
	env.WriteFile("/workspace/vendor/native-kit/skills/review/SKILL.md", "---\nname: review\ndescription: Plugin member\n---\nPlugin member body.\n")
	env.WriteFile("/workspace/.agents/config.local.yaml", `apiVersion: agent-marketplace/v4
sources:
  personal: {directory: ./vendor/native-kit, format: claude-plugin}
resources:
  personal:
    - {kind: plugin, name: native-kit, delivery: native}
`)
	sync := env.RunAM("sync")
	if sync.ExitCode != 0 {
		t.Fatalf("personal-local Claude sync failed (exit %d):\n%s", sync.ExitCode, sync.Combined)
	}
	settings, err := os.ReadFile(env.mustResolvePath("/workspace/.claude/settings.local.json"))
	if err != nil || !bytes.Contains(settings, []byte("native-kit@")) {
		t.Fatalf("personal-local sync did not register the native Claude plugin: %v\n%s", err, settings)
	}
	diff := env.Run("git diff --name-only HEAD")
	if diff.ExitCode != 0 {
		t.Fatalf("read committed workspace diff failed (exit %d):\n%s", diff.ExitCode, diff.Combined)
	}
	if changed := strings.TrimSpace(diff.Stdout); changed != "" {
		details := env.Run("git diff HEAD --")
		t.Fatalf("personal-local Claude sync changed tracked workspace files:\n%s\n%s", changed, details.Combined)
	}
}

// spec: regression-gates/released-client-remains-usable-after-candidate-mutations
func TestReleasedClientNativeClaudePersonalLocalRegistration(t *testing.T) {
	image := os.Getenv("AM_E2E_RELEASED_IMAGE")
	if os.Getenv("AM_E2E_RELEASED_BINARY") == "" || image == "" {
		t.Skip("released executable and git-capable image are supplied by the dedicated released-client gate")
	}
	released, arch := verifiedExecutable(t, "AM_E2E_RELEASED_BINARY", releasedClientArch)
	world := newHistoricalWorld(t, image)
	candidate := world.buildCandidate(arch)
	candidateWorkspace := world.path("workspace-candidate")
	releasedWorkspace := world.path("workspace-released")
	candidateHome := world.path("home-candidate")
	releasedHome := world.path("home-released")
	for _, dir := range []string{filepath.Join(candidateWorkspace, ".agents"), candidateHome, releasedHome} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, "", "init", "-q", "-b", "main", candidateWorkspace)

	registry := world.gitRegistry(map[string]string{"review": "Review body"})
	hookSource := filepath.Join(registry.source, "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hookSource), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookSource, []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo committed-claude-hook"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	registry.commit("add Claude hook")
	gitRun(t, registry.source, "push", "-q", "origin", "main")

	homes := map[string]string{candidate: candidateHome, released: releasedHome}
	must := func(executable, workspace string, args ...string) string {
		t.Helper()
		return world.must(homes[executable], workspace, executable, args...)
	}
	must(candidate, candidateWorkspace, "init", "--harnesses", "claude")
	repoConfig := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  shared: {git: %q, ref: main}\nresources:\n  shared: {include: [skill/review, hook/PreToolUse-command]}\n", registry.URL)
	if err := os.WriteFile(filepath.Join(candidateWorkspace, ".agents", "config.yaml"), []byte(repoConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	must(candidate, candidateWorkspace, "sync")
	hookSettings := filepath.Join(candidateWorkspace, ".claude", "settings.json")
	hookBytes, err := os.ReadFile(hookSettings)
	if err != nil || !bytes.Contains(hookBytes, []byte("committed-claude-hook")) {
		t.Fatalf("candidate did not materialize the committed Claude hook: %v\n%s", err, hookBytes)
	}
	gitRun(t, candidateWorkspace, "add", "-A")
	gitRun(t, candidateWorkspace, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-q", "-m", "candidate Claude baseline")
	origin := world.path("repository.git")
	gitRun(t, "", "clone", "-q", "--bare", candidateWorkspace, origin)
	gitRun(t, candidateWorkspace, "remote", "add", "origin", origin)
	gitRun(t, candidateWorkspace, "push", "-q", "-u", "origin", "main")
	gitRun(t, "", "clone", "-q", origin, releasedWorkspace)
	must(released, releasedWorkspace, "init", "--harnesses", "claude")

	committedPaths := []string{
		filepath.Join(".agents", "config.yaml"),
		filepath.Join(".agents", "lock.yaml"),
		filepath.Join(".claude", "settings.json"),
	}
	fingerprints := func(workspace string) map[string]string {
		t.Helper()
		result := make(map[string]string, len(committedPaths))
		for _, rel := range committedPaths {
			path := filepath.Join(workspace, rel)
			fingerprint, err := pathFingerprint(path)
			if err != nil {
				t.Fatalf("fingerprint committed path %s: %v", rel, err)
			}
			result[rel] = fingerprint
		}
		return result
	}
	assertFingerprints := func(workspace string, before map[string]string, stage string) {
		t.Helper()
		for rel, fingerprint := range before {
			got, err := pathFingerprint(filepath.Join(workspace, rel))
			if err != nil || got != fingerprint {
				t.Fatalf("%s changed committed path %s: %v", stage, rel, err)
			}
		}
	}
	readLock := func(workspace string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(workspace, ".agents", "lock.yaml"))
		if err != nil {
			t.Fatalf("read %s lock: %v", workspace, err)
		}
		return data
	}
	candidateCommitted := fingerprints(candidateWorkspace)
	releasedCommitted := fingerprints(releasedWorkspace)
	lockBytes := readLock(candidateWorkspace)
	if len(lockBytes) == 0 {
		t.Fatal("candidate baseline did not create a Git lock")
	}
	releasedReads := func(stage string, checkCandidateNative bool, settingsLocal, catalogRoot, settingsFingerprint, catalogFingerprint string) {
		t.Helper()
		gitRun(t, releasedWorkspace, "fetch", "-q", "origin")
		gitRun(t, releasedWorkspace, "merge", "-q", "--ff-only", "origin/main")
		releasedLock := readLock(releasedWorkspace)
		for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}} {
			must(released, releasedWorkspace, args...)
			if got := readLock(releasedWorkspace); !bytes.Equal(got, releasedLock) {
				t.Fatalf("%v changed the released checkout's lock after %s", args, stage)
			}
			assertFingerprints(releasedWorkspace, releasedCommitted, fmt.Sprintf("%v after %s", args, stage))
			assertFingerprints(candidateWorkspace, candidateCommitted, fmt.Sprintf("released %v after %s", args, stage))
			if checkCandidateNative {
				for path, before := range map[string]string{
					settingsLocal: settingsFingerprint,
					catalogRoot:   catalogFingerprint,
				} {
					got, err := pathFingerprint(path)
					if err != nil || got != before {
						t.Fatalf("released %v changed candidate native state after %s at %s: %v", args, stage, path, err)
					}
				}
			}
		}
	}
	releasedReads("committed Claude baseline", false, "", "", "", "")

	pluginRoot := filepath.Join(candidateWorkspace, "vendor", "native-kit")
	pluginFiles := map[string]string{
		filepath.Join(".claude-plugin", "plugin.json"): `{"name":"native-kit"}`,
		filepath.Join("skills", "review", "SKILL.md"):  "---\nname: review\ndescription: Review plugin changes\n---\nReview plugin changes.\n",
	}
	for rel, data := range pluginFiles {
		path := filepath.Join(pluginRoot, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	localConfig := filepath.Join(candidateWorkspace, ".agents", "config.local.yaml")
	localDocument := "apiVersion: agent-marketplace/v4\nsources:\n  personal: {directory: ./vendor/native-kit, format: claude-plugin}\nresources:\n  personal:\n    - {kind: plugin, name: native-kit, delivery: native}\n"
	if err := os.WriteFile(localConfig, []byte(localDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	tracked := exec.Command("git", "ls-files", "--error-unmatch", filepath.Join(".agents", "config.local.yaml"))
	tracked.Dir = candidateWorkspace
	if output, err := tracked.CombinedOutput(); err == nil {
		t.Fatalf("personal-local configuration is tracked:\n%s", output)
	}
	must(candidate, candidateWorkspace, "sync")
	settingsLocal := filepath.Join(candidateWorkspace, ".claude", "settings.local.json")
	registration, err := os.ReadFile(settingsLocal)
	if err != nil || !bytes.Contains(registration, []byte("native-kit@")) || !bytes.Contains(registration, []byte("extraKnownMarketplaces")) {
		t.Fatalf("candidate did not write the native Claude registration from personal-local config: %v\n%s", err, registration)
	}
	manifests, err := filepath.Glob(filepath.Join(candidateHome, ".local", "state", "agent-marketplace", "native-plugins", "claude", "*", ".claude-plugin", "marketplace.json"))
	if err != nil || len(manifests) != 1 {
		t.Fatalf("candidate Claude catalogs = %v / %v, want one", manifests, err)
	}
	catalogBytes, err := os.ReadFile(manifests[0])
	if err != nil || !bytes.Contains(catalogBytes, []byte("native-kit")) {
		t.Fatalf("candidate catalog omitted the native plugin: %v\n%s", err, catalogBytes)
	}
	catalogRoot := filepath.Dir(filepath.Dir(manifests[0]))
	settingsFingerprint, err := pathFingerprint(settingsLocal)
	if err != nil {
		t.Fatal(err)
	}
	catalogBefore, err := pathFingerprint(catalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	assertFingerprints(candidateWorkspace, candidateCommitted, "candidate personal-local sync")
	if got := readLock(candidateWorkspace); !bytes.Equal(got, lockBytes) {
		t.Fatalf("personal-local sync changed committed Git lock:\nbefore:\n%s\nafter:\n%s", lockBytes, got)
	}
	diff := exec.Command("git", "diff", "--quiet", "HEAD")
	diff.Dir = candidateWorkspace
	if output, err := diff.CombinedOutput(); err != nil {
		t.Fatalf("personal-local sync changed tracked candidate bytes: %v\n%s", err, output)
	}

	releasedReads("native Claude personal-local sync", true, settingsLocal, catalogRoot, settingsFingerprint, catalogBefore)
	if got, err := pathFingerprint(catalogRoot); err != nil || got != catalogBefore {
		t.Fatalf("released-client reads changed candidate native catalog: %v", err)
	}
}

// The released client predates format sources and plugin locators, so it
// must refuse a plugin configuration before touching configuration, lock,
// or harness state.
// spec: plugin-packages/released-v4-client-refuses-a-plugin-configuration
func TestReleasedClientRefusesPluginConfiguration(t *testing.T) {
	image := os.Getenv("AM_E2E_RELEASED_IMAGE")
	if os.Getenv("AM_E2E_RELEASED_BINARY") == "" || image == "" {
		t.Skip("released executable and git-capable image are supplied by the dedicated released-client gate")
	}
	released, _ := verifiedExecutable(t, "AM_E2E_RELEASED_BINARY", releasedClientArch)
	world := newHistoricalWorld(t, image)
	workspace := world.path("workspace")
	home := world.path("home-released")
	for _, dir := range []string{filepath.Join(workspace, ".agents"), home} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	registry := world.gitRegistry(map[string]string{"seed": "Seed body"})
	catalog := filepath.Join(registry.source, ".claude-plugin")
	plugin := filepath.Join(registry.source, "plugins", "review-kit", ".claude-plugin")
	for _, dir := range []string{catalog, plugin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(catalog, "marketplace.json"), []byte(`{"name":"team","owner":{"name":"t"},"plugins":[{"name":"review-kit","source":"./plugins/review-kit"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugin, "plugin.json"), []byte(`{"name":"review-kit","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry.commit("add catalog")
	gitRun(t, registry.source, "push", "-q", "origin", "main")
	gitRun(t, "", "init", "-q", "-b", "main", workspace)

	world.must(home, workspace, released, "init", "--harnesses", "factory")
	configPath := filepath.Join(workspace, ".agents", "config.yaml")
	config := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  plugins: {git: '%s', ref: main, format: claude-marketplace}\nresources:\n  plugins: [plugin/review-kit]\n", registry.URL)
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, workspace, "add", ".agents/config.yaml")
	gitRun(t, workspace, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-q", "-m", "plugin configuration")
	harnessFile := filepath.Join(workspace, ".factory", "skills", "seed", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(harnessFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(harnessFile, []byte(skillDocument("seed", "Seed body")), 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(workspace, ".agents", "lock.yaml")
	if absent, err := checkPathAbsent(lockPath); err != nil || !absent {
		t.Fatalf("precondition: lock present before refusal: absent=%t err=%v", absent, err)
	}
	configBefore, err := pathFingerprint(configPath)
	if err != nil {
		t.Fatal(err)
	}
	harnessBefore, err := pathFingerprint(filepath.Join(workspace, ".factory"))
	if err != nil {
		t.Fatal(err)
	}

	output, err := world.run(home, workspace, released, "sync")
	if err == nil {
		t.Fatalf("%s accepted a plugin configuration:\n%s", releasedClientVersion, output)
	}
	if !strings.Contains(output, "v4.unknown-field") && !strings.Contains(output, "v4.locator") {
		t.Fatalf("%s refusal did not carry v4.unknown-field or v4.locator:\n%s", releasedClientVersion, output)
	}
	if got, err := pathFingerprint(configPath); err != nil || got != configBefore {
		t.Fatalf("%s changed configuration bytes: %v", releasedClientVersion, err)
	}
	if absent, err := checkPathAbsent(lockPath); err != nil || !absent {
		t.Fatalf("%s created a lock while refusing: absent=%t err=%v", releasedClientVersion, absent, err)
	}
	if got, err := pathFingerprint(filepath.Join(workspace, ".factory")); err != nil || got != harnessBefore {
		t.Fatalf("%s changed harness files: %v", releasedClientVersion, err)
	}
}
