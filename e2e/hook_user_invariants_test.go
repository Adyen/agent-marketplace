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
	"slices"
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
	case "opencode":
		// OpenCode declarations are named after the hook; these fixtures
		// install PreToolUse-command.
		base := ".opencode"
		if machine {
			base = ".config/opencode"
		}
		return []string{base + "/hooks/agent-marketplace/PreToolUse-command.json", base + "/plugins/agent-marketplace.js"}
	}
	panic("hookInvariantHookFiles: unknown target " + target)
}

var hookInvariantTargets = []string{"claude", "factory", "cursor", "gemini", "windsurf", "devin", "codex"}

// hookInvariantNativeTargets adds OpenCode, which shipped after the pinned
// released client and so is absent from the released-client matrices.
var hookInvariantNativeTargets = append(slices.Clone(hookInvariantTargets), "opencode")

// hookInvariantLoadedCommands parses the executable hook handlers a target's
// own loader reads, relative to the scope root: the native file only, never a
// plugin-layout copy. Windsurf reads the repository .windsurf/hooks.json only
// while .devin/hooks.json defines no hook, and OpenCode reads its hook
// declarations only through the installed adapter plugin.
func hookInvariantLoadedCommands(t *testing.T, target, root string, machine bool) []string {
	t.Helper()
	var commands []string
	read := func(rel string) (map[string]json.RawMessage, bool) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if errors.Is(err, os.ErrNotExist) {
			return nil, false
		}
		if err != nil {
			t.Fatalf("read loaded hook file %s: %v", rel, err)
		}
		var document map[string]json.RawMessage
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatalf("decode loaded hook file %s: %v\n%s", rel, err, data)
		}
		return document, true
	}
	events := func(rel, member string) map[string][]json.RawMessage {
		t.Helper()
		document, ok := read(rel)
		if !ok {
			return nil
		}
		if member != "" {
			raw := document[member]
			if raw == nil {
				return nil
			}
			document = nil
			if err := json.Unmarshal(raw, &document); err != nil {
				t.Fatalf("decode %s %s member: %v", rel, member, err)
			}
		}
		byEvent := make(map[string][]json.RawMessage, len(document))
		for event, raw := range document {
			var groups []json.RawMessage
			if err := json.Unmarshal(raw, &groups); err != nil {
				t.Fatalf("decode %s %s handlers: %v", rel, event, err)
			}
			byEvent[event] = groups
		}
		return byEvent
	}
	grouped := func(rel, member string) {
		t.Helper()
		for event, groups := range events(rel, member) {
			for _, raw := range groups {
				var group struct {
					Hooks []struct {
						Type    string `json:"type"`
						Command string `json:"command"`
					} `json:"hooks"`
				}
				if err := json.Unmarshal(raw, &group); err != nil {
					t.Fatalf("decode %s %s matcher group: %v", rel, event, err)
				}
				for _, handler := range group.Hooks {
					if handler.Type == "command" && handler.Command != "" {
						commands = append(commands, handler.Command)
					}
				}
			}
		}
	}
	flat := func(rel string) {
		t.Helper()
		for event, handlers := range events(rel, "hooks") {
			for _, raw := range handlers {
				var handler struct {
					Command string `json:"command"`
				}
				if err := json.Unmarshal(raw, &handler); err != nil {
					t.Fatalf("decode %s %s handler: %v", rel, event, err)
				}
				if handler.Command != "" {
					commands = append(commands, handler.Command)
				}
			}
		}
	}
	switch target {
	case "claude":
		grouped(".claude/settings.json", "hooks")
	case "factory":
		grouped(".factory/hooks.json", "")
	case "cursor":
		flat(".cursor/hooks.json")
	case "gemini":
		grouped(".gemini/settings.json", "hooks")
	case "windsurf":
		switch {
		case machine:
			flat(".codeium/windsurf/hooks.json")
		case len(events(".devin/hooks.json", "hooks")) > 0:
			flat(".devin/hooks.json")
		default:
			flat(".windsurf/hooks.json")
		}
	case "devin":
		if machine {
			grouped(".config/devin/config.json", "hooks")
		} else {
			grouped(".devin/hooks.v1.json", "")
		}
	case "codex":
		grouped(".codex/hooks.json", "hooks")
	case "opencode":
		base := ".opencode"
		if machine {
			base = ".config/opencode"
		}
		paths, err := filepath.Glob(filepath.Join(root, base, "hooks", "agent-marketplace", "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(paths) > 0 {
			if _, err := os.Stat(filepath.Join(root, base, "plugins", "agent-marketplace.js")); err != nil {
				t.Fatalf("OpenCode hook declarations exist without the adapter plugin that loads them: %v", err)
			}
		}
		for _, path := range paths {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatal(err)
			}
			document, _ := read(filepath.ToSlash(rel))
			var format, command string
			if json.Unmarshal(document["format"], &format) != nil || json.Unmarshal(document["command"], &command) != nil ||
				format != "agent-marketplace/opencode-hook-v1" || command == "" {
				t.Fatalf("OpenCode hook declaration %s is not loadable: %v", rel, document)
			}
			commands = append(commands, command)
		}
	default:
		t.Fatalf("hookInvariantLoadedCommands: unknown target %s", target)
	}
	return commands
}

// hookInvariantLoadedCount returns how many loaded executable handlers run a
// command containing token.
func hookInvariantLoadedCount(t *testing.T, target, root string, machine bool, token string) int {
	t.Helper()
	count := 0
	for _, command := range hookInvariantLoadedCommands(t, target, root, machine) {
		if strings.Contains(command, token) {
			count++
		}
	}
	return count
}

// hookInvariantCount returns how often token occurs across files under root.
// It is discovery-only evidence: a token in a plugin-layout file proves
// neither that the harness loads it nor that it is executable. Prefer
// hookInvariantLoadedCount wherever the outcome is a working hook.
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
	for _, target := range hookInvariantNativeTargets {
		t.Run(target, func(t *testing.T) {
			sync := func(matcher string) (outcome, ExecResult) {
				t.Helper()
				e := newEnv(t)
				e.InitGitRepo()
				token := "am-invariant-match-all-" + target
				registry := hookInvariantRegistry(e, "match-all", fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":%q,"hooks":[{"type":"command","command":"echo %s"}]}]}}`, matcher, token))
				e.WriteRepoConfig(hookInvariantV4Config(target, registry, "hook/PreToolUse-command"))
				r := e.RunAM("sync")
				return outcome{exit: r.ExitCode, copies: hookInvariantLoadedCount(t, target, e.workspace, false, token)}, r
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

	first := e.RunAM("sync")
	e.AssertOutputContains(first, "hook.windsurf.event-unmappable")
	if got := hookInvariantCount(t, e.workspace, hookInvariantHookFiles("windsurf", false), refused); got != 0 {
		t.Fatalf("refused sibling installed %d times; the fixture no longer exercises a refusal", got)
	}
	if got := hookInvariantLoadedCount(t, "windsurf", e.workspace, false, valid); got != 1 {
		t.Fatalf("valid hook installed %d times before deselection, want 1\n%s", got, first.Combined)
	}

	// Exit codes are not asserted: whether a refused sibling fails the
	// command is a separate policy. The invariant is the removal itself.
	e.RunAM("remove", "PreToolUse-command", "--kind", "hook", "--scope", "repo")
	if e.CheckFileContains("/workspace/.agents/config.yaml", "hook/PreToolUse-command") {
		t.Fatalf("remove left the hook selected:\n%s", e.ReadFile("/workspace/.agents/config.yaml"))
	}
	last := e.RunAM("sync")
	if got := hookInvariantLoadedCount(t, "windsurf", e.workspace, false, valid); got != 0 {
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
	if got := hookInvariantLoadedCount(t, "claude", e.workspace, false, "scripts/guard.sh"); got != 1 {
		t.Fatalf("first teammate installed %d loaded hook handlers, want 1", got)
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

	if got := hookInvariantLoadedCount(t, "claude", checkout, false, "scripts/guard.sh"); got != 1 {
		t.Errorf("teammate's sync left %d loaded hook handlers, want 1", got)
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
	if got := hookInvariantLoadedCount(t, "windsurf", e.workspace, false, managed); got != 1 {
		t.Fatalf("managed Windsurf hook loads %d times, want 1", got)
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
	if got := hookInvariantLoadedCount(t, "claude", e.workspace, false, token); got != 1 {
		t.Errorf("hook from a file:// working tree loads %d times, want 1", got)
	}
}

type windsurfHookFixture struct {
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory"`
}

func executeWindsurfHookFixture(e *Env, handler windsurfHookFixture) ([]byte, error) {
	cwd := handler.WorkingDirectory
	if cwd == "" {
		cwd = e.workspace
	} else if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(e.workspace, cwd)
	}
	cwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(e.home))
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, cwd)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("working directory %q is outside fixture sandbox", handler.WorkingDirectory)
	}
	ctx, cancel := context.WithTimeout(e.t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "bash", "-c", handler.Command)
	command.Dir = cwd
	for _, assignment := range subprocessEnvWithOverrides(e.subprocessEnv(), map[string]string{"CLAUDE_PROJECT_DIR": e.workspace}) {
		key, _, _ := strings.Cut(assignment, "=")
		switch key {
		case "BASH_ENV", "ENV", "POSIXLY_CORRECT", "SHELLOPTS", "BASHOPTS":
		default:
			command.Env = append(command.Env, assignment)
		}
	}
	return command.CombinedOutput()
}

func TestWindsurfHookFixtureUsesNativeExecutionContract(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	subdir := filepath.Join(e.workspace, "subdir")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	for name, target := range map[string]string{"contained-alias": subdir, "external-alias": outside} {
		if err := os.Symlink(target, filepath.Join(e.workspace, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name    string
		handler windsurfHookFixture
		cwd     string
		refused bool
	}{
		{name: "default-repository", handler: windsurfHookFixture{Command: "pwd -P"}, cwd: e.workspace},
		{name: "relative-directory", handler: windsurfHookFixture{Command: "pwd -P", WorkingDirectory: "subdir"}, cwd: subdir},
		{name: "contained-alias", handler: windsurfHookFixture{Command: "pwd -P", WorkingDirectory: "contained-alias"}, cwd: subdir},
		{name: "external-alias-refused", handler: windsurfHookFixture{Command: "pwd -P", WorkingDirectory: "external-alias"}, refused: true},
		{name: "absolute-home-directory", handler: windsurfHookFixture{Command: "cd . && pwd -P", WorkingDirectory: e.home}, cwd: e.home},
		{name: "repository-relative-command", handler: windsurfHookFixture{Command: "cd . && pwd -P"}, cwd: e.workspace},
		{name: "no-operand-cd-reaches-home", handler: windsurfHookFixture{Command: "cd && pwd -P"}, cwd: e.home},
		{name: "native-bash-not-posix-sh", handler: windsurfHookFixture{Command: "! shopt -qo posix && pwd -P"}, cwd: e.workspace},
		{name: "outside-sandbox-refused", handler: windsurfHookFixture{Command: "pwd -P", WorkingDirectory: outside}, refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := executeWindsurfHookFixture(e, tc.handler)
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "outside fixture sandbox") || len(output) != 0 {
					t.Fatalf("unsafe working directory was not refused before execution: %q / %v", output, err)
				}
				return
			}
			want, resolveErr := filepath.EvalSymlinks(tc.cwd)
			if resolveErr != nil {
				t.Fatal(resolveErr)
			}
			if err != nil || strings.TrimSpace(string(output)) != want {
				t.Fatalf("handler=%+v cwd=%q err=%v; want %q", tc.handler, output, err, want)
			}
		})
	}
}

func TestWindsurfHookFixtureIgnoresHostShellStartup(t *testing.T) {
	e := newEnv(t)
	startup := filepath.Join(e.home, "host-startup.sh")
	if err := os.WriteFile(startup, []byte("printf 'unexpected-host-startup\\n'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"BASH_ENV": startup, "ENV": startup, "POSIXLY_CORRECT": "1", "SHELLOPTS": "posix", "BASHOPTS": "extglob"} {
		t.Setenv(key, value)
	}
	output, err := executeWindsurfHookFixture(e, windsurfHookFixture{Command: "! shopt -qo posix && ! shopt -q extglob && pwd -P"})
	want, resolveErr := filepath.EvalSymlinks(e.workspace)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if err != nil || strings.TrimSpace(string(output)) != want {
		t.Fatalf("host shell settings affected fixture: output=%q err=%v; want %q", output, err, want)
	}
}

// spec: hook-resources/whole-word-project-directory-references-keep-the-repository-directory
func TestHookInvariantProjectDirectoryReferencesKeepRepositoryCWD(t *testing.T) {
	for _, scope := range []string{"repo", "user"} {
		for _, mode := range []string{"symlink", "file"} {
			for _, reference := range []struct{ name, word string }{
				{"bare", "$CLAUDE_PROJECT_DIR"},
				{"braced", "${CLAUDE_PROJECT_DIR}"},
				{"quoted", `"$CLAUDE_PROJECT_DIR"`},
				{"path-suffix", `"$CLAUDE_PROJECT_DIR"/.`},
			} {
				t.Run(scope+"/"+mode+"/"+reference.name, func(t *testing.T) {
					t.Parallel()
					e := newEnv(t)
					e.InitGitRepo()
					authored := "cd " + reference.word + " && pwd -P"
					hooks := fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":%q}]}]}}`, authored)
					registry := hookInvariantRegistry(e, "project-directory", hooks)
					config := hookInvariantV4Config("windsurf", registry, "hook/PreToolUse-command") + "materialization: {mode: " + mode + "}\n"
					base := "apiVersion: agent-marketplace/v4\nharnesses: [windsurf]\n"
					if scope == "user" {
						e.WriteGlobalConfig(config)
						e.WriteRepoConfig(base)
					} else {
						e.WriteGlobalConfig(base)
						e.WriteRepoConfig(config)
					}
					e.MustRunAM("sync")
					path := filepath.Join(e.workspace, ".devin", "hooks.json")
					if scope == "user" {
						path = filepath.Join(e.home, ".codeium", "windsurf", "hooks.json")
					}
					data, err := os.ReadFile(path)
					if errors.Is(err, os.ErrNotExist) && scope == "repo" {
						path = filepath.Join(e.workspace, ".windsurf", "hooks.json")
						data, err = os.ReadFile(path)
					}
					if err != nil {
						t.Fatal(err)
					}
					var native struct {
						Hooks map[string][]windsurfHookFixture `json:"hooks"`
					}
					if err := json.Unmarshal(data, &native); err != nil {
						t.Fatalf("decode active Windsurf hooks: %v", err)
					}
					entries := native.Hooks["pre_run_command"]
					if len(native.Hooks) != 1 || len(entries) != 1 || entries[0].Command == "" {
						t.Fatalf("expected exactly one executable pre_run_command handler: %s", data)
					}
					output, runErr := executeWindsurfHookFixture(e, entries[0])
					want, err := filepath.EvalSymlinks(e.workspace)
					if err != nil {
						t.Fatal(err)
					}
					if runErr != nil || strings.TrimSpace(string(output)) != want {
						t.Fatalf("authored %q became %q: cwd=%q err=%v; want repository %q", authored, entries[0].Command, output, runErr, want)
					}
					before, err := pathFingerprint(path)
					if err != nil {
						t.Fatal(err)
					}
					e.MustRunAM("sync", "--check")
					e.MustRunAM("sync")
					if after, err := pathFingerprint(path); err != nil || after != before {
						t.Fatalf("repeat sync or check changed the active hook: %v", err)
					}
				})
			}
		}
	}
}

// spec: hook-resources/independent-checkout-refuses-removal-of-a-committed-hook
func TestHookInvariantTeammateHookRemovalRefusesWithoutOwnership(t *testing.T) {
	for _, target := range hookInvariantTargets {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			const token = "am-invariant-team-guard"
			registry := e.CreateBareRepo("team", map[string]string{
				"hooks/hooks.json": fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo %s"}]}]}}`, token),
			})
			e.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [%s]\nsources:\n  reg: {git: '%s', ref: main}\nresources:\n  reg: {include: [hook/PreToolUse-command]}\n", target, registry))
			e.InitGitRepo()
			e.MustRunAM("sync")
			hookInvariantCommitAll(e, "first teammate syncs hooks")
			files := hookInvariantHookFiles(target, false)
			if got := hookInvariantLoadedCount(t, target, e.workspace, false, token); got != 1 {
				t.Fatalf("first teammate installed %d loaded copies, want 1", got)
			}

			home := filepath.Join(filepath.Dir(e.home), "teammate-home")
			if err := os.MkdirAll(home, 0o755); err != nil {
				t.Fatal(err)
			}
			checkout := filepath.Join(filepath.Dir(e.workspace), "teammate-checkout")
			e.MustRun(fmt.Sprintf("git clone --no-hardlinks %q %q", e.workspace, checkout))
			// Acquire B's own exact snapshot without materializing or adopting hooks.
			e.AssertExitCode(hookInvariantRunAs(e, home, checkout, "lock"), 0)
			paths := append([]string{".agents/config.yaml", ".agents/lock.yaml", ".agents/config.local.yaml", ".agents/lock.local.yaml"}, files...)
			before := make(map[string]string)
			for _, rel := range paths {
				fingerprint, err := pathFingerprint(filepath.Join(checkout, filepath.FromSlash(rel)))
				if errors.Is(err, os.ErrNotExist) {
					before[rel] = ""
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				before[rel] = fingerprint
			}
			result := hookInvariantRunAs(e, home, checkout, "remove", "PreToolUse-command", "--kind", "hook", "--scope", "repo")
			e.AssertExitCode(result, 1)
			for _, want := range []string{"hook.remove-unowned", target, "active", "ownership"} {
				if !strings.Contains(result.Combined, want) {
					t.Errorf("refusal does not identify %q: %s", want, result.Combined)
				}
			}
			if strings.Contains(result.Combined, "Removed PreToolUse-command") || strings.Contains(result.Combined, token) {
				t.Errorf("refusal reports success or quotes a hook payload: %s", result.Combined)
			}
			for rel, fingerprint := range before {
				after, err := pathFingerprint(filepath.Join(checkout, filepath.FromSlash(rel)))
				if fingerprint == "" && errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil || after != fingerprint {
					t.Errorf("refused remove changed %s: %v", rel, err)
				}
			}
			if _, err := os.Stat(filepath.Join(home, ".local", "state", "agent-marketplace", "ownership")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("refused remove created ownership state: %v", err)
			}
			e.AssertExitCode(hookInvariantRunAs(e, home, checkout, "sync", "--check"), 0)
			if got := hookInvariantLoadedCount(t, target, checkout, false, token); got != 1 {
				t.Errorf("refused remove left %d loaded copies, want the unchanged active hook", got)
			}
			e.MustRunAM("remove", "PreToolUse-command", "--kind", "hook", "--scope", "repo")
			if got := hookInvariantCount(t, e.workspace, files, token); got != 0 {
				t.Errorf("same-owner removal left %d copies in any hook file, want none", got)
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
