package e2e_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// releasedHookGate supplies the verified latest release and a Linux candidate
// built from this tree. Subtests must use world.run and report through their
// own *testing.T: world.must fails the parent test from the wrong goroutine.
func releasedHookGate(t *testing.T) (*historicalWorld, string, string) {
	t.Helper()
	image := os.Getenv("AM_E2E_RELEASED_IMAGE")
	if os.Getenv("AM_E2E_RELEASED_BINARY") == "" || image == "" {
		t.Skip("released executable and git-capable image are supplied by the dedicated released-client gate")
	}
	released, arch := verifiedExecutable(t, "AM_E2E_RELEASED_BINARY", releasedClientArch)
	world := newHistoricalWorld(t, image)
	return world, released, world.buildCandidate(arch)
}

func releasedHookWriteTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A committed lock written by the latest release keeps verifying for the
// candidate, and a lock the candidate refreshes keeps verifying for the
// release, for registries that also carry harness-native hook files or a
// project .claude/settings.json beside their resources, and for registries
// of native structured agents whose model, reasoning and tool fields the
// candidate's model and tool catalogs now translate. The release writes the
// lock's source revisions in its frozen format and the candidate's update
// writes the current one, so both directions are exercised.
func TestReleasedClientLockVerifiesNativeHookSourceLayouts(t *testing.T) {
	world, released, candidate := releasedHookGate(t)
	skill := skillDocument("review", "Review body")
	for _, shape := range []struct {
		name, harness, skillPath, include string
		files                             map[string]string
		// fields are the structured values every client must keep in
		// the same-harness output it materializes.
		fields []string
	}{
		{"factory-native-hooks", "factory", ".factory/skills/review/SKILL.md", "", map[string]string{
			".factory/skills/review/SKILL.md": skill,
			".factory/hooks.json":             `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"echo maintainer"}]}]}`,
		}, nil},
		{"cursor-native-hooks", "cursor", ".cursor/skills/review/SKILL.md", "", map[string]string{
			".cursor/skills/review/SKILL.md": skill,
			".cursor/hooks.json":             `{"version":1,"hooks":{"preToolUse":[{"command":"echo maintainer","matcher":"Shell"}]}}`,
		}, nil},
		{"devin-desktop-workspace-hooks", "windsurf", ".windsurf/skills/review/SKILL.md", "", map[string]string{
			".windsurf/skills/review/SKILL.md": skill,
			".devin/hooks.json":                `{"hooks":{"pre_run_command":[{"command":"echo maintainer"}]}}`,
		}, nil},
		{"claude-settings-hooks", "claude", ".claude/skills/review/SKILL.md", "", map[string]string{
			".claude/skills/review/SKILL.md": skill,
			".claude/settings.json":          `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo maintainer"}]}]}}`,
		}, nil},
		{"root-skills-with-claude-settings", "claude", ".claude/skills/review/SKILL.md", "", map[string]string{
			"skills/review/SKILL.md": skill,
			".claude/settings.json":  `{"permissions":{"allow":["Bash(make test)"]}}`,
		}, nil},
		{"claude-structured-agent", "claude", ".claude/agents/reader.md", "subagent/reader", map[string]string{
			".claude/agents/reader.md": "---\nname: reader\ndescription: Reads code\nmodel: sonnet\ntools:\n  - Read\n  - Grep\n---\nRead carefully.\n",
		}, []string{"\nmodel: sonnet\n", "\n  - Read\n", "\n  - Grep\n", "\nRead carefully.\n"}},
		{"factory-structured-droid", "factory", ".factory/droids/reader.md", "subagent/reader", map[string]string{
			".factory/droids/reader.md": "---\nname: reader\ndescription: Reads code\nmodel: claude-sonnet-5\nreasoningEffort: high\ntools: [\"Read\", \"Grep\"]\n---\nRead carefully.\n",
		}, []string{"\nmodel: claude-sonnet-5\n", "\nreasoningEffort: high\n", "\n  - Read\n", "\n  - Grep\n", "\nRead carefully.\n"}},
		{"codex-structured-agent", "codex", ".codex/agents/reader.toml", "subagent/reader", map[string]string{
			".codex/agents/reader.toml": "name = 'reader'\ndescription = 'Reads code'\ndeveloper_instructions = 'Read carefully.'\nmodel = 'gpt-5.6'\nmodel_reasoning_effort = 'high'\n",
		}, []string{"\nmodel = 'gpt-5.6'\n", "\nmodel_reasoning_effort = 'high'\n", "\ndeveloper_instructions = 'Read carefully.'\n"}},
	} {
		t.Run(shape.name, func(t *testing.T) {
			source, bare := world.path(shape.name, "source"), world.path(shape.name, "registry.git")
			workspace := world.path(shape.name, "workspace")
			homes := map[string]string{released: world.path(shape.name, "home-released"), candidate: world.path(shape.name, "home-candidate")}
			releasedHookWriteTree(t, source, shape.files)
			gitRun(t, "", "init", "-q", "-b", "main", source)
			(&gitRegistry{t: t, source: source}).commit("seed")
			gitRun(t, "", "clone", "-q", "--bare", source, bare)
			include := shape.include
			if include == "" {
				include = "skill/review"
			}
			releasedHookWriteTree(t, workspace, map[string]string{".agents/config.yaml": fmt.Sprintf(
				"apiVersion: agent-marketplace/v4\nharnesses: [%s]\nsources:\n  reg: {git: 'file://%s', ref: main}\nresources:\n  reg: {include: [%s]}\n", shape.harness, bare, include)})
			gitRun(t, "", "init", "-q", "-b", "main", workspace)
			for _, home := range homes {
				if err := os.MkdirAll(home, 0o700); err != nil {
					t.Fatal(err)
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
			step := func(executable string, args ...string) {
				t.Helper()
				if output, err := world.run(homes[executable], workspace, executable, args...); err != nil {
					t.Fatalf("%s %v: %v\n%s", filepath.Base(executable), args, err, output)
				}
			}

			// keeps requires the materialized output to retain every
			// structured field after the named client's sync.
			keeps := func(client string) {
				t.Helper()
				if len(shape.fields) == 0 {
					return
				}
				data, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(shape.skillPath)))
				if err != nil {
					t.Fatalf("%s did not materialize %s: %v", client, shape.skillPath, err)
				}
				for _, field := range shape.fields {
					if !strings.Contains(string(data), field) {
						t.Fatalf("%s output %s dropped %q:\n%s", client, shape.skillPath, field, data)
					}
				}
			}

			step(released, "sync")
			keeps(releasedClientVersion)
			written := readLock()
			// The candidate teammate fetches the committed snapshot with
			// --locked; the check-only mode is offline and runs afterwards.
			for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}, {"sync"}} {
				step(candidate, args...)
				if got := readLock(); !bytes.Equal(got, written) {
					t.Fatalf("candidate %v rewrote the %s lock:\nbefore:\n%s\nafter:\n%s", args, releasedClientVersion, written, got)
				}
			}
			if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(shape.skillPath))); err != nil {
				t.Fatalf("candidate did not materialize the locked %s: %v", include, err)
			}
			keeps("candidate")

			step(candidate, "update", "--scope", "repo")
			refreshed := readLock()
			for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}} {
				step(released, args...)
				if got := readLock(); !bytes.Equal(got, refreshed) {
					t.Fatalf("%s %v rewrote the candidate-refreshed lock", releasedClientVersion, args)
				}
			}
		})
	}
}

// spec: hook-resources/released-hook-output-upgrades-without-duplication
// Upgrading from the latest release keeps one copy of every hook the release
// installed, on every target and scope, and a later deselection removes it.
// The same HOME runs both executables, as when a user replaces the binary.
func TestReleasedClientHookOutputUpgrades(t *testing.T) {
	world, released, candidate := releasedHookGate(t)
	for _, scope := range []string{"repo", "user"} {
		for _, target := range hookInvariantTargets {
			name := scope + "-" + target
			t.Run(name, func(t *testing.T) {
				registry, home, workspace := world.path(name, "registry"), world.path(name, "home"), world.path(name, "workspace")
				pre, prompt := "am-upgrade-pre-"+name, "am-upgrade-prompt-"+name
				releasedHookWriteTree(t, registry, map[string]string{"hooks/hooks.json": fmt.Sprintf(
					`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo %s"}]}],"UserPromptSubmit":[{"hooks":[{"type":"command","command":"echo %s"}]}]}}`, pre, prompt)})
				config := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [%s]\nsources:\n  reg: {directory: '%s'}\nresources:\n  reg: {include: [hook/PreToolUse-command, hook/UserPromptSubmit-command]}\n", target, registry)
				root, configPath := workspace, filepath.Join(workspace, ".agents", "config.yaml")
				if scope == "user" {
					root, configPath = home, filepath.Join(home, ".agents", "config.yaml")
				}
				for _, dir := range []string{home, filepath.Join(workspace, ".agents"), filepath.Dir(configPath)} {
					if err := os.MkdirAll(dir, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
					t.Fatal(err)
				}
				gitRun(t, "", "init", "-q", "-b", "main", workspace)
				step := func(executable string, args ...string) {
					t.Helper()
					if output, err := world.run(home, workspace, executable, args...); err != nil {
						t.Fatalf("%s %v: %v\n%s", filepath.Base(executable), args, err, output)
					}
				}
				files := hookInvariantHookFiles(target, scope == "user")
				// copies is discovery-only: it counts the command token in
				// every plugin-layout and native file, which is all the
				// release's own output can be held to.
				copies := func(stage string, wantPre, wantPrompt int) {
					t.Helper()
					if got := hookInvariantCount(t, root, files, pre); got != wantPre {
						t.Errorf("%s: PreToolUse hook installed %d times, want %d", stage, got, wantPre)
					}
					if got := hookInvariantCount(t, root, files, prompt); got != wantPrompt {
						t.Errorf("%s: UserPromptSubmit hook installed %d times, want %d", stage, got, wantPrompt)
					}
				}
				// loaded counts parsed executable handlers in the file the
				// target's own loader reads.
				loaded := func(stage string, wantPre, wantPrompt int) {
					t.Helper()
					if got := hookInvariantLoadedCount(t, target, root, scope == "user", pre); got != wantPre {
						t.Errorf("%s: PreToolUse hook loads %d times, want %d", stage, got, wantPre)
					}
					if got := hookInvariantLoadedCount(t, target, root, scope == "user", prompt); got != wantPrompt {
						t.Errorf("%s: UserPromptSubmit hook loads %d times, want %d", stage, got, wantPrompt)
					}
				}

				step(released, "sync")
				copies(releasedClientVersion+" sync", 1, 1)
				// The release writes Claude, Factory and Cursor hooks only in
				// the plugin layout those harnesses never load, which is what
				// the candidate's upgrade repairs; its other targets' output
				// already loads.
				if releasedLoads := !slices.Contains([]string{"claude", "factory", "cursor"}, target); releasedLoads {
					loaded(releasedClientVersion+" sync", 1, 1)
				} else {
					loaded(releasedClientVersion+" sync", 0, 0)
				}
				if t.Failed() {
					t.FailNow()
				}
				step(candidate, "sync")
				copies("candidate upgrade sync", 1, 1)
				loaded("candidate upgrade sync", 1, 1)
				step(candidate, "sync", "--check")
				step(candidate, "remove", "UserPromptSubmit-command", "--kind", "hook", "--scope", scope)
				step(candidate, "sync")
				copies("candidate deselection", 1, 0)
				loaded("candidate deselection", 1, 0)
			})
		}
	}
}
