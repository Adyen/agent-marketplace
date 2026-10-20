package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hookInvariantDocument(groups ...string) string {
	return `{"hooks":{"PreToolUse":[` + strings.Join(groups, ",") + `]}}`
}

func hookInvariantGroup(matcher, command string) string {
	return fmt.Sprintf(`{"matcher":%q,"hooks":[{"type":"command","command":%q}]}`, matcher, command)
}

// Every target, in both scopes, must leave a selected hook as exactly one
// parsed executable handler in the file its own loader reads, and remove it on
// deselection. This is the positive control for hookInvariantLoadedCommands:
// a plugin-layout copy of the command is never counted.
// spec: hook-resources/every-harness-target-receives-hooks-in-its-native-file
func TestHookInvariantSelectedHookReachesEachNativeLoader(t *testing.T) {
	for _, target := range hookInvariantNativeTargets {
		for _, scope := range []string{"repo", "user"} {
			t.Run(target+"/"+scope, func(t *testing.T) {
				t.Parallel()
				e := newEnv(t)
				const command = "echo am-loaded-handler"
				registry := hookInvariantRegistry(e, "loaded", hookInvariantDocument(hookInvariantGroup("Bash", command)))
				selected := hookInvariantV4Config(target, registry, "hook/PreToolUse-command")
				base := "apiVersion: agent-marketplace/v4\nharnesses: [" + target + "]\n"
				root, machine := e.workspace, scope == "user"
				write := func(config string) {
					if machine {
						e.WriteGlobalConfig(config)
						e.WriteRepoConfig(base)
						return
					}
					e.WriteGlobalConfig(base)
					e.WriteRepoConfig(config)
				}
				if machine {
					root = e.home
				}
				write(selected)
				e.InitGitRepo()
				e.MustRunAM("sync")
				if got := hookInvariantLoadedCommands(t, target, root, machine); len(got) != 1 || got[0] != command {
					t.Fatalf("loaded handlers = %q, want exactly %q", got, command)
				}
				e.MustRunAM("sync", "--check")
				write(base)
				e.MustRunAM("sync")
				if got := hookInvariantLoadedCommands(t, target, root, machine); len(got) != 0 {
					t.Fatalf("deselection left loaded handlers %q", got)
				}
			})
		}
	}
}

// Two registries that ship byte-identical hooks under the same name must run
// the hook once, as one entry both providers contribute. When one provider
// inserts a handler ahead of the shared one, its positional selection now
// names the inserted handler: that handler is added for review, while the
// shared entry stays, untouched, for the other provider alone, so removing
// that provider removes it. A handwritten copy of the shared hook satisfies
// the selection without a duplicate, is never claimed, and survives
// deselection.
// spec: hook-resources/same-named-hooks-from-two-registries-both-install
func TestHookInvariantEqualHooksFromDistinctProviders(t *testing.T) {
	const (
		shared   = "echo am-equal-shared"
		inserted = "echo am-equal-inserted"
	)
	// OpenCode is excluded until same-named hooks from two providers stop
	// overwriting one declaration file (#806); see
	// TestHookInvariantOpenCodeSameNamedProvidersBothInstall.
	for _, target := range hookInvariantTargets {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			a := hookInvariantRegistry(e, "provider-a", hookInvariantDocument(hookInvariantGroup("Bash", shared)))
			b := hookInvariantRegistry(e, "provider-b", hookInvariantDocument(hookInvariantGroup("Bash", shared)))
			config := func(providers ...string) string {
				body := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [%s]\nsources:\n  a: {directory: '%s'}\n  b: {directory: '%s'}\n", target, a, b)
				if len(providers) > 0 {
					body += "resources:\n"
					for _, provider := range providers {
						body += "  " + provider + ": {include: [hook/PreToolUse-command]}\n"
					}
				}
				return body
			}
			runs := func(phase, command string, want int) {
				t.Helper()
				if got := hookInvariantLoadedCount(t, target, e.workspace, false, command); got != want {
					t.Fatalf("%s: %q runs %d times, want %d; loaded %q", phase, command, got, want, hookInvariantLoadedCommands(t, target, e.workspace, false))
				}
			}
			e.WriteRepoConfig(config("a", "b"))
			e.InitGitRepo()
			e.MustRunAM("sync")
			runs("both providers", shared, 1)
			loaded := hookInvariantLoadedFile(t, target)
			twin := e.ReadFile("/workspace/" + loaded)
			if strings.Contains(twin, "_managed_by") {
				t.Fatalf("%s carries an in-file ownership marker:\n%s", loaded, twin)
			}

			e.WriteFile("/repos/provider-a/hooks/hooks.json", hookInvariantDocument(hookInvariantGroup("Bash", inserted), hookInvariantGroup("Bash", shared)))
			r := e.MustRunAM("sync")
			runs("after insertion", shared, 1)
			runs("after insertion", inserted, 1)
			if !strings.Contains(r.Combined, `+ command: "`+inserted+`"`) || strings.Contains(r.Combined, `- command: "`+shared+`"`) {
				t.Fatalf("insertion review must add only the inserted handler:\n%s", r.Combined)
			}

			e.WriteRepoConfig(config("a"))
			e.MustRunAM("sync")
			runs("provider b removed", shared, 0)
			runs("provider b removed", inserted, 1)

			e.WriteRepoConfig(config())
			e.MustRunAM("sync")
			runs("no providers", inserted, 0)
			e.WriteFile("/workspace/"+loaded, twin)
			runs("handwritten twin", shared, 1)
			e.WriteRepoConfig(config("b"))
			r = e.MustRunAM("sync")
			runs("twin satisfies provider b", shared, 1)
			if !strings.Contains(r.Combined, "satisfied-by-unmanaged") {
				t.Fatalf("handwritten twin did not report satisfaction:\n%s", r.Combined)
			}
			e.WriteRepoConfig(config())
			e.MustRunAM("sync")
			runs("twin after deselection", shared, 1)
		})
	}
}

// hookInvariantLoadedFile names the repository file whose handlers
// hookInvariantLoadedCommands reads for a single-file target.
func hookInvariantLoadedFile(t *testing.T, target string) string {
	t.Helper()
	switch target {
	case "claude":
		return ".claude/settings.json"
	case "factory":
		return ".factory/hooks.json"
	case "cursor":
		return ".cursor/hooks.json"
	case "gemini":
		return ".gemini/settings.json"
	case "windsurf":
		return ".devin/hooks.json"
	case "devin":
		return ".devin/hooks.v1.json"
	case "codex":
		return ".codex/hooks.json"
	}
	t.Fatalf("hookInvariantLoadedFile: %s has no single loaded file", target)
	return ""
}

// OpenCode installs each hook as a declaration file named after the hook, so
// two registries' same-named hooks share one path and only one survives.
func TestHookInvariantOpenCodeSameNamedProvidersBothInstall(t *testing.T) {
	t.Skip("known defect: https://github.com/Adyen/agent-marketplace/issues/806")
	e := newEnv(t)
	a := hookInvariantRegistry(e, "provider-a", hookInvariantDocument(hookInvariantGroup("Bash", "echo am-opencode-provider-a")))
	b := hookInvariantRegistry(e, "provider-b", hookInvariantDocument(hookInvariantGroup("Bash", "echo am-opencode-provider-b")))
	e.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [opencode]\nsources:\n  a: {directory: '%s'}\n  b: {directory: '%s'}\nresources:\n  a: {include: [hook/PreToolUse-command]}\n  b: {include: [hook/PreToolUse-command]}\n", a, b))
	e.InitGitRepo()
	e.MustRunAM("sync")
	for _, command := range []string{"echo am-opencode-provider-a", "echo am-opencode-provider-b"} {
		if got := hookInvariantLoadedCount(t, "opencode", e.workspace, false, command); got != 1 {
			t.Errorf("%q runs %d times, want 1; loaded %q", command, got, hookInvariantLoadedCommands(t, "opencode", e.workspace, false))
		}
	}
	if _, err := os.Stat(filepath.Join(e.workspace, ".opencode", "plugins", "agent-marketplace.js")); err != nil {
		t.Fatal(err)
	}
}

// A teammate's removal of a committed OpenCode hook must either take effect
// in that teammate's checkout or refuse without reporting success; it must
// never report success while the declaration keeps the hook active.
func TestHookInvariantOpenCodeTeammateRemovalTakesEffect(t *testing.T) {
	t.Skip("known defect: https://github.com/Adyen/agent-marketplace/issues/810")
	e := newEnv(t)
	const token = "am-opencode-team-guard"
	registry := e.CreateBareRepo("team", map[string]string{"hooks/hooks.json": hookInvariantDocument(hookInvariantGroup("Bash", "echo "+token))})
	e.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [opencode]\nsources:\n  reg: {git: '%s', ref: main}\nresources:\n  reg: {include: [hook/PreToolUse-command]}\n", registry))
	e.InitGitRepo()
	e.MustRunAM("sync")
	hookInvariantCommitAll(e, "first teammate syncs hooks")
	home := filepath.Join(filepath.Dir(e.home), "teammate-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(filepath.Dir(e.workspace), "teammate-checkout")
	e.MustRun(fmt.Sprintf("git clone --no-hardlinks %q %q", e.workspace, checkout))
	e.AssertExitCode(hookInvariantRunAs(e, home, checkout, "lock"), 0)
	removed := hookInvariantRunAs(e, home, checkout, "remove", "PreToolUse-command", "--kind", "hook", "--scope", "repo")
	hookInvariantRunAs(e, home, checkout, "sync")
	active := hookInvariantLoadedCount(t, "opencode", checkout, false, token)
	if removed.ExitCode == 0 && active != 0 {
		t.Fatalf("teammate removal reported success while %d loaded copies stay active:\n%s", active, removed.Combined)
	}
	if removed.ExitCode != 0 && active != 1 {
		t.Fatalf("refused teammate removal changed the active hook: %d loaded copies", active)
	}
}
