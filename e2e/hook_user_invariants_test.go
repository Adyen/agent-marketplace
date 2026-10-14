package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Hook user invariants.
//
// These tests pin what a user observes after a sync, independent of which
// file a harness target reads its hooks from. Each assertion counts a unique
// command token across every location the target's hooks have been written
// to (plugin-layout file and native file), so a test fails only when the
// user-visible outcome changes, not when output moves between locations.

// hookInvariantHookFiles lists, relative to the scope root (the repository
// for repository scope, HOME for machine scope), every file the target's
// hooks have been materialized into.
func hookInvariantHookFiles(target string, machine bool) []string {
	switch target {
	case "claude":
		return []string{".claude/hooks/hooks.json", ".claude/settings.json"}
	case "factory":
		return []string{".factory/hooks/hooks.json", ".factory/hooks.json"}
	case "cursor":
		return []string{".cursor/hooks/hooks.json", ".cursor/hooks.json"}
	case "gemini":
		return []string{".gemini/settings.json"}
	case "windsurf":
		if machine {
			return []string{".codeium/windsurf/hooks.json"}
		}
		return []string{".windsurf/hooks.json", ".devin/hooks.json"}
	case "devin":
		if machine {
			return []string{".config/devin/config.json"}
		}
		return []string{".devin/hooks.v1.json"}
	case "codex":
		return []string{".codex/hooks.json"}
	}
	panic("hookInvariantHookFiles: unknown target " + target)
}

var hookInvariantTargets = []string{"claude", "factory", "cursor", "gemini", "windsurf", "devin", "codex"}

// hookInvariantCount returns how often token occurs across files under root.
func hookInvariantCount(t *testing.T, root string, files []string, token string) int {
	t.Helper()
	count := 0
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read hook file %s: %v", rel, err)
		}
		count += strings.Count(string(data), token)
	}
	return count
}

// hookInvariantRegistry writes a directory registry whose hooks/hooks.json
// holds the given plugin-layout hook document and returns its absolute path.
func hookInvariantRegistry(e *Env, name, hooks string) string {
	e.t.Helper()
	e.WriteFile("/repos/"+name+"/hooks/hooks.json", hooks)
	return e.mustResolvePath("/repos/" + name)
}

func hookInvariantV4Config(harnesses, directory string, include ...string) string {
	return fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [%s]\nsources:\n  reg: {directory: '%s'}\nresources:\n  reg: {include: [%s]}\n",
		harnesses, directory, strings.Join(include, ", "))
}

// hookInvariantRunAs runs the CLI as another teammate: its own HOME, XDG
// roots and CODEX_HOME, from its own checkout.
func hookInvariantRunAs(e *Env, home, dir string, args ...string) ExecResult {
	e.t.Helper()
	env := subprocessEnvWithOverrides(e.subprocessEnv(), map[string]string{
		"HOME":            home,
		"XDG_STATE_HOME":  filepath.Join(home, ".local", "state"),
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_CACHE_HOME":  filepath.Join(home, ".cache"),
		"CODEX_HOME":      filepath.Join(home, ".codex"),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, e.binary, args...)
	c.Dir = dir
	c.Env = env
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	result := ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), Combined: stdout.String() + stderr.String()}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			e.t.Fatalf("exec agent-marketplace %v as teammate: %v", args, err)
		}
		result.ExitCode = exitErr.ExitCode()
	}
	return result
}

func hookInvariantCommitAll(e *Env, message string) {
	e.t.Helper()
	e.MustRun(fmt.Sprintf(`git add -A && git -c user.name='E2E Test' -c user.email='e2e@test.local' commit -q --allow-empty -m %q`, message))
}

// A registry whose resources sit at its root (skills/ without a plugin
// manifest) often also commits its maintainers' own .claude/settings.json.
// That project settings file must not change which resources the registry
// provides.
func TestHookInvariantBareRegistryWithClaudeSettingsKeepsResources(t *testing.T) {
	e := newEnv(t)
	e.InitGitRepo()
	url := e.CreateBareRepo("bare-with-claude-settings", map[string]string{
		"skills/review/SKILL.md": Skill{Name: "review"}.SkillFile(),
		"skills/lint/SKILL.md":   Skill{Name: "lint"}.SkillFile(),
		".claude/settings.json":  `{"permissions":{"allow":["Bash(make test)"]}}`,
	})
	e.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  reg: {git: '%s', ref: main}\nresources:\n  reg: {include: [skill/review, skill/lint]}\n", url))

	e.AssertExitCode(e.RunAM("sync"), 0)
	for _, name := range []string{"review", "lint"} {
		e.AssertFileContains("/workspace/.claude/skills/"+name+"/SKILL.md", "name: "+name)
	}
	e.AssertExitCode(e.RunAM("sync", "--check"), 0)
}

// spec: hook-resources/match-all-regex-syncs-like-star
// `.*` and `*` both select every tool. A hook must produce the same outcome
// on every target whichever of the two spellings its author used.
func TestHookInvariantMatchAllRegexMatcherSyncsLikeStar(t *testing.T) {
	type outcome struct{ exit, copies int }
	installedStar := 0
	for _, target := range hookInvariantTargets {
		t.Run(target, func(t *testing.T) {
			sync := func(matcher string) (outcome, ExecResult) {
				t.Helper()
				e := newEnv(t)
				e.InitGitRepo()
				token := "am-invariant-match-all-" + target
				registry := hookInvariantRegistry(e, "match-all", fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":%q,"hooks":[{"type":"command","command":"echo %s"}]}]}}`, matcher, token))
				e.WriteRepoConfig(hookInvariantV4Config(target, registry, "hook/PreToolUse-command"))
				r := e.RunAM("sync")
				return outcome{exit: r.ExitCode, copies: hookInvariantCount(t, e.workspace, hookInvariantHookFiles(target, false), token)}, r
			}
			star, starResult := sync("*")
			regex, _ := sync(".*")
			// Windsurf splits tool events by category and has no event for
			// every tool, so the "*" control is refused there; every other
			// target installs it exactly once.
			if target == "windsurf" {
				if star.copies != 0 || !strings.Contains(starResult.Combined, "hook.windsurf.event-unmappable") {
					t.Fatalf("Windsurf \"*\" control installed %d copies; want a hook.windsurf.event-unmappable refusal\n%s", star.copies, starResult.Combined)
				}
			} else if star != (outcome{exit: 0, copies: 1}) {
				t.Fatalf("\"*\" control gave exit %d with %d installed copies, want exit 0 with 1\n%s", star.exit, star.copies, starResult.Combined)
			}
			if star.copies > 0 {
				installedStar++
			}
			if regex != star {
				t.Errorf("matcher \".*\" gave exit %d with %d installed copies; \"*\" gave exit %d with %d", regex.exit, regex.copies, star.exit, star.copies)
			}
		})
	}
	if installedStar == 0 {
		t.Fatal("no target installed the \"*\" control hook; the comparison is vacuous")
	}
}

// spec: hook-resources/refused-sibling-does-not-block-deselection
// Deselecting a hook removes it even when another selected hook for the
// same target cannot be represented there.
func TestHookInvariantRefusedSiblingDoesNotBlockDeselection(t *testing.T) {
	e := newEnv(t)
	e.InitGitRepo()
	const valid, refused = "am-invariant-valid-hook", "am-invariant-refused-hook"
	// Windsurf has no subagent lifecycle event, so SubagentStop is refused.
	registry := hookInvariantRegistry(e, "refused-sibling", fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo %s"}]}],"SubagentStop":[{"hooks":[{"type":"command","command":"echo %s"}]}]}}`, valid, refused))
	e.WriteRepoConfig(hookInvariantV4Config("windsurf", registry, "hook/PreToolUse-command", "hook/SubagentStop-command"))
	files := hookInvariantHookFiles("windsurf", false)

	first := e.RunAM("sync")
	e.AssertOutputContains(first, "hook.windsurf.event-unmappable")
	if got := hookInvariantCount(t, e.workspace, files, refused); got != 0 {
		t.Fatalf("refused sibling installed %d times; the fixture no longer exercises a refusal", got)
	}
	if got := hookInvariantCount(t, e.workspace, files, valid); got != 1 {
		t.Fatalf("valid hook installed %d times before deselection, want 1\n%s", got, first.Combined)
	}

	// Exit codes are not asserted: whether a refused sibling fails the
	// command is a separate policy. The invariant is the removal itself.
	e.RunAM("remove", "PreToolUse-command", "--kind", "hook", "--scope", "repo")
	if e.CheckFileContains("/workspace/.agents/config.yaml", "hook/PreToolUse-command") {
		t.Fatalf("remove left the hook selected:\n%s", e.ReadFile("/workspace/.agents/config.yaml"))
	}
	last := e.RunAM("sync")
	if got := hookInvariantCount(t, e.workspace, files, valid); got != 0 {
		t.Errorf("deselected hook still installed %d times\n%s", got, last.Combined)
	}
}

var hookInvariantProjectDir = regexp.MustCompile(`^\$(CLAUDE_PROJECT_DIR\b|\{CLAUDE_PROJECT_DIR\})`)

// hookInvariantScriptPath resolves the file a Claude hook command runs for a
// checkout. The command must execute script, directly or through sh or
// bash; its path may use $CLAUDE_PROJECT_DIR or be relative to the checkout
// and must resolve to an existing file inside checkout. Unresolved variables
// and absolute paths outside checkout, such as another teammate's checkout
// or cache, are errors.
func hookInvariantScriptPath(command, checkout, script string) (string, error) {
	fields := strings.Fields(command)
	if len(fields) > 1 && (filepath.Base(fields[0]) == "sh" || filepath.Base(fields[0]) == "bash") {
		fields = fields[1:]
	}
	var word string
	if len(fields) > 0 {
		word = strings.NewReplacer(`"`, "", `'`, "").Replace(fields[0])
	}
	if !strings.HasSuffix(word, "/"+script) && word != script {
		return "", fmt.Errorf("command %q does not run %s", command, script)
	}
	path := hookInvariantProjectDir.ReplaceAllLiteralString(word, checkout)
	if strings.Contains(path, "$") {
		return "", fmt.Errorf("command %q leaves a variable unresolved", command)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(checkout, filepath.FromSlash(path))
	}
	if rel, err := filepath.Rel(checkout, filepath.Clean(path)); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("command %q runs %s outside the checkout", command, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("command %q runs a missing file: %w", command, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("command %q runs %s, which is not a regular file", command, path)
	}
	return path, nil
}

func TestHookInvariantScriptPathResolution(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	checkout, other := filepath.Join(root, "checkout"), filepath.Join(root, "other-checkout")
	for _, dir := range []string{checkout, other} {
		if err := os.MkdirAll(filepath.Join(dir, ".claude", "support", "scripts"), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"guard.sh", "guard.sh.disabled"} {
			if err := os.WriteFile(filepath.Join(dir, ".claude", "support", "scripts", name), []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		name, command string
		ok            bool
	}{
		{"quoted project directory", `"$CLAUDE_PROJECT_DIR"/.claude/support/scripts/guard.sh`, true},
		{"braced project directory", `${CLAUDE_PROJECT_DIR}/.claude/support/scripts/guard.sh --flag`, true},
		{"checkout-relative", `.claude/support/scripts/guard.sh`, true},
		{"absolute inside checkout", filepath.Join(checkout, ".claude", "support", "scripts", "guard.sh"), true},
		{"through bash", `bash "$CLAUDE_PROJECT_DIR"/.claude/support/scripts/guard.sh`, true},
		{"through an absolute sh", `/bin/sh .claude/support/scripts/guard.sh`, true},
		{"script only as an argument", `echo "$CLAUDE_PROJECT_DIR"/.claude/support/scripts/guard.sh`, false},
		{"different file sharing the prefix", `"$CLAUDE_PROJECT_DIR"/.claude/support/scripts/guard.sh.disabled`, false},
		{"missing file", `"$CLAUDE_PROJECT_DIR"/.claude/missing/scripts/guard.sh`, false},
		{"another teammate's checkout", filepath.Join(other, ".claude", "support", "scripts", "guard.sh"), false},
		{"plugin root variable", `${CLAUDE_PLUGIN_ROOT}/scripts/guard.sh`, false},
		{"other variable", `$HOME/.claude/support/scripts/guard.sh`, false},
		{"escaping relative path", `../other-checkout/.claude/support/scripts/guard.sh`, false},
		{"no script", `echo unrelated`, false},
	} {
		if _, err := hookInvariantScriptPath(tc.command, checkout, "scripts/guard.sh"); (err == nil) != tc.ok {
			t.Errorf("%s: hookInvariantScriptPath(%q) error = %v, want ok=%t", tc.name, tc.command, err, tc.ok)
		}
	}
}

// hookInvariantCommands returns every "command" string in a hook document.
func hookInvariantCommands(t *testing.T, data []byte) []string {
	t.Helper()
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode hook document: %v\n%s", err, data)
	}
	var commands []string
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				if command, ok := child.(string); ok && key == "command" {
					commands = append(commands, command)
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(document)
	return commands
}

// spec: hook-resources/repository-local-support-paths-survive-teammate-checkout
// Teammates check a repository out at different paths. A teammate's sync of
// a healthy committed tree, with an in-repository directory source whose
// hook ships a support script, must not duplicate the hook, leave it
// pointing at a missing script, or change tracked files.
func TestHookInvariantTeammateCheckoutPathDoesNotDuplicateHooks(t *testing.T) {
	e := newEnv(t)
	e.WriteFile("/workspace/vendor/reg/hooks/scripts/guard.sh", "#!/bin/sh\necho guard\n")
	if err := os.Chmod(e.mustResolvePath("/workspace/vendor/reg/hooks/scripts/guard.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	const authored = "${CLAUDE_PLUGIN_ROOT}/scripts/guard.sh"
	e.WriteFile("/workspace/vendor/reg/hooks/hooks.json", `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"`+authored+`"}]}]}}`)
	e.WriteRepoConfig(hookInvariantV4Config("claude", "./vendor/reg", "hook/PreToolUse-command"))
	e.InitGitRepo()
	e.MustRunAM("sync")
	hookInvariantCommitAll(e, "first teammate syncs hooks")
	files := hookInvariantHookFiles("claude", false)
	if got := hookInvariantCount(t, e.workspace, files, "scripts/guard.sh"); got != 1 {
		t.Fatalf("first teammate installed %d hook entries, want 1", got)
	}

	sandbox := filepath.Dir(e.home)
	home := filepath.Join(sandbox, "teammate-home")
	checkout := filepath.Join(sandbox, "teammate", "elsewhere", "repo")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if r := e.Exec("git", "clone", "-q", e.workspace, checkout); r.ExitCode != 0 {
		t.Fatalf("clone repository for teammate: %s", r.Combined)
	}
	r := hookInvariantRunAs(e, home, checkout, "sync")
	e.AssertExitCode(r, 0)

	if got := hookInvariantCount(t, checkout, files, "scripts/guard.sh"); got != 1 {
		t.Errorf("teammate's sync left %d hook entries, want 1", got)
	}
	// Claude Code loads hooks from .claude/settings.json; it never reads the
	// plugin-layout .claude/hooks/hooks.json, so a command there cannot run
	// and must merely stay the authored one. Every command in a file Claude
	// Code loads must run a script that exists in this checkout.
	const pluginLayout = ".claude/hooks/hooks.json"
	checked := 0
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(checkout, filepath.FromSlash(rel)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, command := range hookInvariantCommands(t, data) {
			checked++
			if rel == pluginLayout {
				if command != authored {
					t.Errorf("%s command = %q, want the authored %q", rel, command, authored)
				}
				continue
			}
			if _, err := hookInvariantScriptPath(command, checkout, "scripts/guard.sh"); err != nil {
				t.Errorf("%s: %v", rel, err)
			}
		}
	}
	if checked == 0 {
		t.Error("teammate's checkout holds no hook command to check")
	}
	if status := e.Exec("git", "-C", checkout, "status", "--porcelain"); strings.TrimSpace(status.Stdout) != "" || status.ExitCode != 0 {
		t.Errorf("teammate's sync of a healthy committed tree changed tracked files:\n%s", status.Combined)
	}
}

// spec: hook-resources/handwritten-windsurf-fallback-hooks-remain-active
// Devin Desktop stops reading the workspace's .windsurf/hooks.json once
// .devin/hooks.json defines any hook (documented in the native-hook
// integration design, PR #451, D10). Syncing a Windsurf hook must therefore
// not leave a user's handwritten .windsurf/hooks.json silently shadowed.
func TestHookInvariantHandwrittenWindsurfHooksAreNotShadowed(t *testing.T) {
	e := newEnv(t)
	const handwritten, managed = "am-invariant-handwritten-formatter", "am-invariant-managed-hook"
	e.WriteFile("/workspace/.windsurf/hooks.json", fmt.Sprintf(`{"hooks":{"post_write_code":[{"command":"echo %s"}]}}`, handwritten))
	registry := hookInvariantRegistry(e, "windsurf-shadow", fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo %s"}]}]}}`, managed))
	e.WriteRepoConfig(hookInvariantV4Config("windsurf", registry, "hook/PreToolUse-command"))
	e.InitGitRepo()

	e.AssertExitCode(e.RunAM("sync"), 0)
	if got := hookInvariantCount(t, e.workspace, hookInvariantHookFiles("windsurf", false), managed); got != 1 {
		t.Fatalf("managed Windsurf hook installed %d times, want 1", got)
	}
	e.AssertFileContains("/workspace/.windsurf/hooks.json", handwritten)

	data, err := os.ReadFile(filepath.Join(e.workspace, ".devin", "hooks.json"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	var workspace struct {
		Hooks map[string][]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(data, &workspace); err != nil {
		t.Fatalf("decode .devin/hooks.json: %v\n%s", err, data)
	}
	defines := false
	for _, entries := range workspace.Hooks {
		defines = defines || len(entries) > 0
	}
	if defines && !strings.Contains(string(data), handwritten) {
		t.Errorf(".devin/hooks.json defines hooks, so Devin Desktop no longer loads the handwritten .windsurf/hooks.json hook:\n%s", data)
	}
}

// A legacy configuration may name a local working-tree checkout of a
// registry through a file:// URL. Its hooks must install like any other
// Git registry's.
func TestHookInvariantLegacyFileURLWorkingTreeHooksSync(t *testing.T) {
	e := newEnv(t)
	e.InitGitRepo()
	const token = "am-invariant-working-tree-hook"
	checkout := filepath.Join(e.repos, "working-tree-registry")
	e.WriteFile("/repos/working-tree-registry/.claude-plugin/plugin.json", `{"name":"working-tree","version":"1.0.0"}`)
	e.WriteFile("/repos/working-tree-registry/hooks/hooks.json", fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo %s"}]}]}}`, token))
	e.MustRun(fmt.Sprintf(`cd %q && git init -q -b main && git add -A && git -c user.name='E2E Test' -c user.email='e2e@test.local' commit -q -m seed`, checkout))
	e.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources: {team: {git: 'file://%s', ref: main}}\nresources: {team: {include: [hook/PreToolUse-command]}}\n", checkout))

	e.AssertExitCode(e.RunAM("sync"), 0)
	if got := hookInvariantCount(t, e.workspace, hookInvariantHookFiles("claude", false), token); got != 1 {
		t.Errorf("hook from a file:// working tree installed %d times, want 1", got)
	}
}

// A teammate with their own HOME must be able to deselect a hook another
// teammate installed and committed.
func TestHookInvariantTeammateCanRemoveCommittedHook(t *testing.T) {
	t.Skip("known defect: https://github.com/Adyen/agent-marketplace/issues/630")
	for _, target := range []string{"claude", "devin", "codex"} {
		t.Run(target, func(t *testing.T) {
			e := newEnv(t)
			const token = "am-invariant-team-guard"
			registry := hookInvariantRegistry(e, "team", fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo %s"}]}]}}`, token))
			e.WriteRepoConfig(hookInvariantV4Config(target, registry, "hook/PreToolUse-command"))
			e.InitGitRepo()
			e.MustRunAM("sync")
			hookInvariantCommitAll(e, "first teammate syncs hooks")
			files := hookInvariantHookFiles(target, false)
			if got := hookInvariantCount(t, e.workspace, files, token); got != 1 {
				t.Fatalf("first teammate installed %d copies, want 1", got)
			}

			home := filepath.Join(filepath.Dir(e.home), "teammate-home")
			if err := os.MkdirAll(home, 0o755); err != nil {
				t.Fatal(err)
			}
			e.AssertExitCode(hookInvariantRunAs(e, home, e.workspace, "remove", "PreToolUse-command", "--kind", "hook", "--scope", "repo"), 0)
			e.AssertExitCode(hookInvariantRunAs(e, home, e.workspace, "sync"), 0)
			if got := hookInvariantCount(t, e.workspace, files, token); got != 0 {
				t.Errorf("teammate's remove left %d copies of the committed hook", got)
			}
		})
	}
}

// Windsurf's pre_mcp_tool_use event has no matcher, so only a match-all MCP
// matcher is representable; a narrower one must not be widened to every
// MCP tool.
func TestHookInvariantWindsurfNarrowMCPMatcherIsNotWidened(t *testing.T) {
	t.Skip("known defect: https://github.com/Adyen/agent-marketplace/issues/631")
	install := func(matcher string) int {
		t.Helper()
		e := newEnv(t)
		e.InitGitRepo()
		const token = "am-invariant-mcp-guard"
		registry := hookInvariantRegistry(e, "mcp", fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":%q,"hooks":[{"type":"command","command":"echo %s"}]}]}}`, matcher, token))
		e.WriteRepoConfig(hookInvariantV4Config("windsurf", registry, "hook/PreToolUse-command"))
		e.RunAM("sync")
		return hookInvariantCount(t, e.workspace, hookInvariantHookFiles("windsurf", false), token)
	}
	if got := install("mcp__.*"); got != 1 {
		t.Fatalf("match-all MCP control installed %d times, want 1", got)
	}
	for _, matcher := range []string{"mcp__github__.*", "mcp__github__create_issue"} {
		if got := install(matcher); got != 0 {
			t.Errorf("narrow matcher %q was widened to every MCP tool (%d installed copies)", matcher, got)
		}
	}
}
