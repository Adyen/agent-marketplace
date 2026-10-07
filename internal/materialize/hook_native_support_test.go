package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const nativeSupportScriptBody = "#!/bin/sh\nexec \"$CLAUDE_PROJECT_DIR\"/bin/tool\n"

// nativeSupportSource builds a real on-disk source tree in the given source
// harness layout, catalogs it, and returns the provider-shaped artifact the
// additive tests consume. files maps slash-relative paths to contents; the
// hook definition must be one of them.
func nativeSupportSource(t *testing.T, sourceHarness, defRel string, files map[string]string, selectedName ...string) resolver.ResolvedArtifact {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		additiveSafetyWrite(t, filepath.Join(root, filepath.FromSlash(rel)), body)
	}
	cat, err := catalog.Load(root, sourceHarness)
	if err != nil {
		t.Fatalf("catalog %s: %v", sourceHarness, err)
	}
	if len(selectedName) > 1 {
		t.Fatalf("nativeSupportSource selected names = %v, want at most one", selectedName)
	}
	for i := range cat.Entries {
		if cat.Entries[i].Kind == ir.KindHook && strings.HasSuffix(filepath.ToSlash(cat.Entries[i].Path), defRel) {
			entry := cat.Entries[i]
			if len(selectedName) == 1 {
				entry.Name = selectedName[0]
			}
			return resolver.ResolvedArtifact{Entry: entry, RegistryURL: root, CatalogKey: root}
		}
	}
	t.Fatalf("catalog %s produced no hook for %s: %#v", sourceHarness, defRel, cat.Entries)
	return resolver.ResolvedArtifact{}
}

// nativeSupportClaudeFiles renders a Claude-canonical plugin-layout hooks file
// plus the nested support script the command references. The script body is
// deliberately left referencing $CLAUDE_PROJECT_DIR verbatim so tests prove
// support bytes install unchanged.
func nativeSupportClaudeFiles(command string) map[string]string {
	return map[string]string{
		"hooks/hooks.json":           fmt.Sprintf(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":%q}]}]}`, command),
		"hooks/scripts/check.sh":     nativeSupportScriptBody,
		"hooks/scripts/payload.json": `{"check":"value"}`,
	}
}

// nativeSupportResolve runs the real resolver over provider-shaped sources.
func nativeSupportResolve(t *testing.T, target hcc.Target, machine bool, providers []resolver.ResolvedArtifact) []resolver.ResolvedArtifact {
	t.Helper()
	cfg := &config.Config{}
	catalogs := map[string][]catalog.Entry{}
	for _, provider := range providers {
		source := config.RepoCommitted
		if machine {
			source = config.UserPersonal
		}
		cfg.Registries = append(cfg.Registries, config.RegistryEntry{URL: provider.RegistryURL, CatalogKey: provider.CatalogKey, Source: source, Default: true})
		catalogs[provider.CatalogKey] = []catalog.Entry{provider.Entry}
	}
	resolved, errs := resolver.Resolve(cfg, catalogs, nil, resolver.WithTargetHarnesses([]hcc.Target{target}))
	if len(errs) != 0 {
		t.Fatalf("resolve: %v", errs)
	}
	return resolved
}

// nativeSupportLocation describes one target's current hook file and support roots.
type nativeSupportLocation struct {
	target             hcc.Target
	repoFile, userFile string
	container, event   string
	userContainer      string
	repoBase           string
	repoSupportRoot    string
	userSupportRoot    string
	matcher            string
	handlerType        string
	grouped            bool
	hasMatcher         bool
}

func nativeSupportLocations() []nativeSupportLocation {
	return []nativeSupportLocation{
		{target: hcc.TargetClaude, repoFile: ".claude/settings.json", userFile: ".claude/settings.json", container: "hooks", event: "PreToolUse", userContainer: "hooks", repoBase: `"$CLAUDE_PROJECT_DIR"/`, repoSupportRoot: ".claude/hook-support", userSupportRoot: ".claude/hook-support", matcher: "Bash", handlerType: "command", grouped: true, hasMatcher: true},
		{target: hcc.TargetFactory, repoFile: ".factory/hooks.json", userFile: ".factory/hooks.json", event: "PreToolUse", repoBase: `"$FACTORY_PROJECT_DIR"/`, repoSupportRoot: ".factory/hook-support", userSupportRoot: ".factory/hook-support", matcher: "Execute", handlerType: "command", grouped: true, hasMatcher: true},
		{target: hcc.TargetCursor, repoFile: ".cursor/hooks.json", userFile: ".cursor/hooks.json", container: "hooks", event: "preToolUse", userContainer: "hooks", repoBase: `"$CURSOR_PROJECT_DIR"/`, repoSupportRoot: ".cursor/hook-support", userSupportRoot: ".cursor/hook-support", matcher: "^Shell$", handlerType: "command", hasMatcher: true},
		{target: hcc.TargetGemini, repoFile: ".gemini/settings.json", userFile: ".gemini/settings.json", container: "hooks", event: "BeforeTool", userContainer: "hooks", repoBase: `"$GEMINI_PROJECT_DIR"/`, repoSupportRoot: ".gemini/hook-support", userSupportRoot: ".gemini/hook-support", matcher: "^run_shell_command$", handlerType: "command", grouped: true, hasMatcher: true},
		{target: hcc.TargetDevin, repoFile: ".devin/hooks.v1.json", userFile: ".config/devin/config.json", event: "PreToolUse", userContainer: "hooks", repoBase: `"$DEVIN_PROJECT_DIR"/`, repoSupportRoot: ".devin/hook-support", userSupportRoot: ".config/devin/hook-support", matcher: "^exec$", handlerType: "command", grouped: true, hasMatcher: true},
		{target: hcc.Target("codex"), repoFile: ".codex/hooks.json", userFile: ".codex/hooks.json", container: "hooks", event: "PreToolUse", userContainer: "hooks", repoBase: `"$(git rev-parse --show-toplevel)"/`, repoSupportRoot: ".codex/hook-support", userSupportRoot: ".codex/hook-support", matcher: "^Bash$", handlerType: "command", grouped: true, hasMatcher: true},
		{target: hcc.TargetWindsurf, repoFile: ".devin/hooks.json", userFile: ".codeium/windsurf/hooks.json", container: "hooks", event: "pre_run_command", userContainer: "hooks", repoSupportRoot: "hook-support", userSupportRoot: ".codeium/windsurf/hook-support"},
	}
}

func nativeSupportCommands(t *testing.T, loc nativeSupportLocation, file string, wantHandlers int) []string {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode hook file %s: %v", file, err)
	}
	if loc.container != "" {
		var ok bool
		document, ok = document[loc.container].(map[string]any)
		if !ok {
			t.Fatalf("missing %s object in %s", loc.container, file)
		}
	}
	if len(document) != 1 {
		t.Fatalf("hook events in %s = %v, want only %q", file, document, loc.event)
	}
	rawEvents, ok := document[loc.event].([]any)
	if !ok {
		t.Fatalf("missing event %q in %s", loc.event, file)
	}
	var handlers []any
	if loc.grouped {
		for _, rawGroup := range rawEvents {
			group, ok := rawGroup.(map[string]any)
			if !ok {
				t.Fatalf("event %s group is not an object", loc.event)
			}
			rawMatcher, present := group["matcher"]
			matcher, matcherIsString := rawMatcher.(string)
			if present != loc.hasMatcher || present && !matcherIsString || matcher != loc.matcher {
				t.Fatalf("event %s matcher = %v (present %v), want %q (present %v)", loc.event, rawMatcher, present, loc.matcher, loc.hasMatcher)
			}
			groupHandlers, ok := group["hooks"].([]any)
			if !ok || len(groupHandlers) == 0 {
				t.Fatalf("event %s group has no handlers", loc.event)
			}
			handlers = append(handlers, groupHandlers...)
		}
	} else {
		handlers = rawEvents
	}
	if len(handlers) != wantHandlers {
		t.Fatalf("event %s handler count = %d, want %d", loc.event, len(handlers), wantHandlers)
	}
	commands := make([]string, 0, len(handlers))
	for _, raw := range handlers {
		handler, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("event %s handler is not an object", loc.event)
		}
		handlerType, hasHandlerType := handler["type"]
		if loc.handlerType == "" {
			if hasHandlerType {
				t.Fatalf("event %s handler unexpectedly carries type %v", loc.event, handlerType)
			}
		} else if handlerType != loc.handlerType {
			t.Fatalf("event %s handler type = %v, want %s", loc.event, handlerType, loc.handlerType)
		}
		if !loc.grouped {
			rawMatcher, present := handler["matcher"]
			matcher, matcherIsString := rawMatcher.(string)
			if present != loc.hasMatcher || present && !matcherIsString || matcher != loc.matcher {
				t.Fatalf("event %s matcher = %v (present %v), want %q (present %v)", loc.event, rawMatcher, present, loc.matcher, loc.hasMatcher)
			}
		}
		command, ok := handler["command"].(string)
		if !ok {
			t.Fatalf("event %s handler has no command", loc.event)
		}
		commands = append(commands, command)
	}
	return commands
}

func nativeSupportLocationFor(t *testing.T, target hcc.Target) nativeSupportLocation {
	t.Helper()
	for _, loc := range nativeSupportLocations() {
		if loc.target == target {
			return loc
		}
	}
	t.Fatalf("no native support fixture for target %s", target)
	return nativeSupportLocation{}
}

type nativeSupportState struct {
	mode       os.FileMode
	isDir      bool
	size       int64
	modified   int64
	data       []byte
	linkTarget string
}

func nativeSupportSnapshot(t *testing.T, roots ...string) map[string]nativeSupportState {
	t.Helper()
	states := make(map[string]nativeSupportState)
	for _, root := range roots {
		if _, err := os.Lstat(root); os.IsNotExist(err) {
			states["absent:"+root] = nativeSupportState{}
			continue
		} else if err != nil {
			t.Fatalf("stat snapshot root %s: %v", root, err)
		}
		if err := filepath.Walk(root, func(name string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			state := nativeSupportState{mode: info.Mode(), isDir: info.IsDir(), size: info.Size(), modified: info.ModTime().UnixNano()}
			if info.Mode().IsRegular() {
				state.data, err = os.ReadFile(name)
				if err != nil {
					return err
				}
			} else if info.Mode()&os.ModeSymlink != 0 {
				state.linkTarget, err = os.Readlink(name)
				if err != nil {
					return err
				}
			}
			states[name] = state
			return nil
		}); err != nil {
			t.Fatalf("snapshot %s: %v", root, err)
		}
	}
	return states
}

func nativeSupportStableSnapshot(t *testing.T, roots ...string) map[string]nativeSupportState {
	t.Helper()
	states := nativeSupportSnapshot(t, roots...)
	for name, state := range states {
		if state.isDir {
			state.size = 0
			state.modified = 0
			states[name] = state
		}
	}
	return states
}

func nativeSupportPersistentSnapshot(t *testing.T, repo, home string) map[string]nativeSupportState {
	t.Helper()
	states := nativeSupportSnapshot(t, repo, home)
	locks := filepath.Join(home, "state", "agent-marketplace", "locks")
	for name, state := range states {
		rel, err := filepath.Rel(locks, name)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			state.size = 0
			state.modified = 0
			state.data = nil
			states[name] = state
		}
	}
	return states
}

func assertNativeSupportSnapshot(t *testing.T, before, after map[string]nativeSupportState) {
	t.Helper()
	var missing, added []string
	for name := range before {
		if _, ok := after[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			added = append(added, name)
		}
	}
	if len(missing) > 0 || len(added) > 0 {
		t.Fatalf("filesystem entries changed: missing=%v added=%v", missing, added)
	}
	for name, want := range before {
		got := after[name]
		if want.mode != got.mode || want.isDir != got.isDir || want.size != got.size || want.modified != got.modified || want.linkTarget != got.linkTarget || !bytes.Equal(want.data, got.data) {
			t.Fatalf("filesystem entry %s changed: before=%+v after=%+v", name, want, got)
		}
	}
}

func nativeSupportResolvedTarget(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return path
	}
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target)
}

func requireNativeSupportCompileError(t *testing.T, report *Report, protected string) {
	t.Helper()
	var count int
	for _, d := range report.Diags {
		if d.Code != "materialize.compile.error" {
			continue
		}
		count++
		if d.Severity != diag.SeverityError {
			t.Fatalf("support diagnostic severity = %v, want error", d.Severity)
		}
		if protected != "" && (strings.Contains(d.Message, protected) || strings.Contains(d.Hint, protected)) {
			t.Fatal("support diagnostic contains protected source data")
		}
	}
	if count != 1 {
		t.Fatalf("materialize.compile.error count = %d, want 1", count)
	}
}

// codexHomeCanonical returns CODEX_HOME with symlinks resolved, matching the
// canonical root the Codex partition resolver uses for emitted paths.
func codexHomeCanonical(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(os.Getenv("CODEX_HOME"))
	if err != nil {
		t.Fatalf("resolve CODEX_HOME: %v", err)
	}
	return home
}

// nativeSupportInstalledLoc returns the installed support-file location the
// command must reference, plus the command's file reference without any
// leading repository base token.
func (loc nativeSupportLocation) installedRef(t *testing.T, command, repo, home string, machine bool) (string, string) {
	t.Helper()
	if machine {
		base := filepath.Join(home, filepath.FromSlash(loc.userSupportRoot))
		if loc.target == hcc.Target("codex") {
			base = filepath.Join(codexHomeCanonical(t), "hook-support")
		}
		// The machine base is emitted as a POSIX single-quoted literal when
		// it carries shell-unsafe characters; the rest of the command stays
		// unquoted. Strip the quoting to recover the literal path.
		ref := strings.ReplaceAll(command, `'\''`, "\x00")
		ref = strings.ReplaceAll(ref, "'", "")
		ref = strings.ReplaceAll(ref, "\x00", "'")
		if !filepath.IsAbs(ref) {
			t.Fatalf("machine command %q is not an absolute literal path", command)
		}
		if !strings.HasPrefix(ref, base+string(os.PathSeparator)) {
			t.Fatalf("machine command %q is not under %s", command, base)
		}
		return ref, ref
	}
	ref := command
	if loc.repoBase != "" {
		if !strings.HasPrefix(ref, loc.repoBase) {
			t.Fatalf("repo command %q lacks base %q", command, loc.repoBase)
		}
		ref = strings.TrimPrefix(ref, loc.repoBase)
	}
	if strings.HasPrefix(ref, "/") || strings.Contains(filepath.ToSlash(ref), "../") {
		t.Fatalf("repo command %q is not a clean repository-relative path", command)
	}
	abs := filepath.Join(repo, filepath.FromSlash(ref))
	supportRoot := filepath.Join(repo, filepath.FromSlash(loc.repoSupportRoot))
	if !strings.HasPrefix(abs, supportRoot+string(os.PathSeparator)) {
		t.Fatalf("repo command %q is not under %s", command, supportRoot)
	}
	return abs, ref
}

// spec: hook-resources/repository-scope-commands-use-the-project-directory-variable
// spec: hook-resources/codex-and-windsurf-repository-commands-use-their-documented-base
// spec: hook-resources/machine-scope-commands-use-absolute-support-paths
// spec: hook-resources/no-plugin-only-variable-or-cache-path-is-emitted
// spec: hook-resources/support-files-reach-devin-codex-and-gemini
func TestNativeHookSupportAllTargetsScopesAndModes(t *testing.T) {
	for _, loc := range nativeSupportLocations() {
		for _, machine := range []bool{false, true} {
			scope := "repo"
			if machine {
				scope = "machine"
			}
			for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
				t.Run(fmt.Sprintf("%s/%s/%s", loc.target, scope, mode), func(t *testing.T) {
					repo, home := additiveSafetyEnv(t)
					provider := nativeSupportSource(t, "claude", "hooks/hooks.json", nativeSupportClaudeFiles("${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"))
					if err := os.Chmod(filepath.Join(provider.RegistryURL, "hooks", "scripts", "check.sh"), 0o755); err != nil {
						t.Fatal(err)
					}
					resolved := nativeSupportResolve(t, loc.target, machine, []resolver.ResolvedArtifact{provider})
					opts := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
					report, err := Run(context.Background(), repo, resolved, opts...)
					if err != nil {
						t.Fatalf("run: %v", err)
					}
					if hasErrorDiagnostics(report.Diags) {
						t.Fatalf("unexpected error diagnostics: %v", report.Diags)
					}

					base := repo
					file := loc.repoFile
					if machine {
						base = home
						file = loc.userFile
					}
					hookFile := filepath.Join(base, filepath.FromSlash(file))
					shape := loc
					if machine {
						shape.container = loc.userContainer
					}
					commands := nativeSupportCommands(t, shape, hookFile, 1)
					command := commands[0]
					if strings.Contains(command, "PLUGIN_ROOT") || strings.Contains(filepath.ToSlash(command), "agent-marketplace") {
						t.Fatalf("command leaks a plugin-only variable or cache path: %q", command)
					}

					installed, ref := loc.installedRef(t, command, repo, home, machine)
					expectedCommand := loc.repoBase + filepath.ToSlash(ref)
					if machine {
						expectedCommand = filepath.ToSlash(installed)
					}
					if command != expectedCommand {
						t.Fatalf("command = %q, want full installed support path %q", command, expectedCommand)
					}
					supportRoot := filepath.Join(repo, filepath.FromSlash(loc.repoSupportRoot))
					if machine {
						supportRoot = filepath.Join(home, filepath.FromSlash(loc.userSupportRoot))
						if loc.target == hcc.Target("codex") {
							supportRoot = filepath.Join(codexHomeCanonical(t), "hook-support")
						}
					}
					supportRel, relErr := filepath.Rel(supportRoot, installed)
					if relErr != nil {
						t.Fatal(relErr)
					}
					elements := strings.Split(filepath.ToSlash(supportRel), "/")
					installedName := provider.InstalledName
					if installedName == "" {
						installedName = provider.Name
					}
					if len(elements) != 4 || elements[0] == "" || elements[1] != installedName ||
						elements[2] != "scripts" || elements[3] != "check.sh" {
						t.Fatalf("installed command %q does not use the support source and installed name", command)
					}
					data, err := os.ReadFile(installed)
					if err != nil {
						t.Fatalf("support file missing at %s: %v", installed, err)
					}
					if string(data) != nativeSupportScriptBody {
						t.Fatalf("support bytes rewritten: %q", data)
					}
					linkInfo, err := os.Lstat(installed)
					if err != nil {
						t.Fatal(err)
					}
					if mode == config.MaterializationModeSymlink && linkInfo.Mode()&os.ModeSymlink == 0 ||
						mode == config.MaterializationModeFile && !linkInfo.Mode().IsRegular() {
						t.Fatalf("support path mode = %v, does not match %s", linkInfo.Mode(), mode)
					}
					info, err := os.Stat(installed)
					if err != nil {
						t.Fatalf("stat support target: %v", err)
					}
					if info.Mode().Perm() != 0o755 {
						t.Fatalf("support target mode = %v, want 755", info.Mode())
					}
					payload := filepath.Join(filepath.Dir(installed), "payload.json")
					payloadData, err := os.ReadFile(payload)
					if err != nil || string(payloadData) != `{"check":"value"}` {
						t.Fatalf("nested payload.json not installed beside the script at %s: %v", payload, err)
					}
				})
			}
		}
	}
}

// spec: hook-resources/deselected-hook-is-removed-from-the-merged-file
// spec: hook-resources/repeat-sync-is-byte-identical
// spec: hook-resources/same-named-scripts-from-two-registries-do-not-conflict
// spec: hook-resources/deselected-hooks-remove-their-support-directory
func TestNativeHookSupportNamespacesDeselectAndIdempotent(t *testing.T) {
	for _, machine := range []bool{false, true} {
		scope := "repo"
		if machine {
			scope = "machine"
		}
		for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
			t.Run(fmt.Sprintf("%s/%s", scope, mode), func(t *testing.T) {
				repo, home := additiveSafetyEnv(t)
				filesA := map[string]string{
					"hooks/hooks.json": `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/check.sh"}]}]}`,
					"hooks/check.sh":   "echo first-script\n",
				}
				filesB := map[string]string{
					"hooks/hooks.json": `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/check.sh"}]}]}`,
					"hooks/check.sh":   "echo second-script\n",
				}
				providerA := nativeSupportSource(t, "claude", "hooks/hooks.json", filesA)
				providerB := nativeSupportSource(t, "claude", "hooks/hooks.json", filesB)
				if err := os.Chmod(filepath.Join(providerA.RegistryURL, "hooks", "check.sh"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(providerB.RegistryURL, "hooks", "check.sh"), 0o644); err != nil {
					t.Fatal(err)
				}
				resolved := nativeSupportResolve(t, hcc.TargetFactory, machine, []resolver.ResolvedArtifact{providerA, providerB})
				if len(resolved) != 2 {
					t.Fatalf("expected two resolved hooks, got %d", len(resolved))
				}
				otherRoot := filepath.Join(home, ".factory")
				if machine {
					otherRoot = filepath.Join(repo, ".factory")
				}
				otherScopeBefore := nativeSupportSnapshot(t, otherRoot)
				opts := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
				report, err := Run(context.Background(), repo, resolved, opts...)
				if err != nil {
					t.Fatalf("run: %v", err)
				}
				if hasErrorDiagnostics(report.Diags) {
					t.Fatalf("same-named support files from two registries conflicted: %v", report.Diags)
				}
				assertNativeSupportSnapshot(t, otherScopeBefore, nativeSupportSnapshot(t, otherRoot))

				loc := nativeSupportLocationFor(t, hcc.TargetFactory)
				supportBase := repo
				hookFile := loc.repoFile
				supportRel := loc.repoSupportRoot
				if machine {
					supportBase = home
					hookFile = loc.userFile
					supportRel = loc.userSupportRoot
				}
				supportRoot := filepath.Join(supportBase, filepath.FromSlash(supportRel))
				matches, err := filepath.Glob(filepath.Join(supportRoot, "*", "*", "check.sh"))
				if err != nil || len(matches) != 2 {
					t.Fatalf("expected two per-hook scripts under %s, got %v (%v)", supportRoot, matches, err)
				}
				var installedA, installedB string
				for _, installed := range matches {
					data, err := os.ReadFile(installed)
					if err != nil {
						t.Fatal(err)
					}
					linkInfo, err := os.Lstat(installed)
					if err != nil {
						t.Fatalf("stat support path %s: %v", installed, err)
					}
					if mode == config.MaterializationModeSymlink && linkInfo.Mode()&os.ModeSymlink == 0 ||
						mode == config.MaterializationModeFile && !linkInfo.Mode().IsRegular() {
						t.Fatalf("support path mode %s = %v, does not match %s", installed, linkInfo.Mode(), mode)
					}
					info, err := os.Stat(installed)
					if err != nil {
						t.Fatalf("stat support target %s: %v", installed, err)
					}
					switch string(data) {
					case "echo first-script\n":
						if info.Mode().Perm() != 0o755 {
							t.Fatalf("first support target mode = %o, want 755", info.Mode().Perm())
						}
						installedA = installed
					case "echo second-script\n":
						if info.Mode().Perm() != 0o644 {
							t.Fatalf("second support target mode = %o, want 644", info.Mode().Perm())
						}
						installedB = installed
					default:
						t.Fatalf("unexpected support bytes at %s: %q", installed, data)
					}
				}
				if installedA == "" || installedB == "" || installedA == installedB {
					t.Fatalf("per-source install dirs not distinct: %q %q", installedA, installedB)
				}

				commands := nativeSupportCommands(t, loc, filepath.Join(supportBase, filepath.FromSlash(hookFile)), 2)
				referenced := make(map[string]bool, len(commands))
				for _, command := range commands {
					var path string
					if machine {
						path, _ = loc.installedRef(t, command, repo, home, true)
						if command != filepath.ToSlash(path) {
							t.Fatalf("machine command = %q, want full support path %q", command, filepath.ToSlash(path))
						}
					} else {
						if !strings.HasPrefix(command, loc.repoBase) {
							t.Fatalf("repository command %q lacks base %q", command, loc.repoBase)
						}
						rel := strings.TrimPrefix(command, loc.repoBase)
						path = filepath.Join(repo, filepath.FromSlash(rel))
						if command != loc.repoBase+filepath.ToSlash(filepath.Clean(rel)) {
							t.Fatalf("repository command is not a clean full path: %q", command)
						}
					}
					if path != installedA && path != installedB {
						t.Fatalf("command %q does not reference one of the two installed scripts", command)
					}
					referenced[path] = true
				}
				if !referenced[installedA] || !referenced[installedB] {
					t.Fatalf("commands %v do not reference both installed scripts %s and %s", commands, installedA, installedB)
				}

				sentinel := filepath.Join(supportRoot, "handwritten.txt")
				additiveSafetyWrite(t, sentinel, "keep me")
				repeatRoots := []string{
					filepath.Join(supportBase, filepath.FromSlash(hookFile)),
					supportRoot,
					sentinel,
					nativeSupportResolvedTarget(t, installedA),
					nativeSupportResolvedTarget(t, installedB),
				}
				beforeRepeat := nativeSupportStableSnapshot(t, repeatRoots...)
				if _, err := Run(context.Background(), repo, resolved, opts...); err != nil {
					t.Fatal(err)
				}
				assertNativeSupportSnapshot(t, beforeRepeat, nativeSupportStableSnapshot(t, repeatRoots...))
				assertNativeSupportSnapshot(t, otherScopeBefore, nativeSupportSnapshot(t, otherRoot))

				installedATarget := nativeSupportResolvedTarget(t, installedA)
				beforeDeselect := nativeSupportSnapshot(t, installedA, installedATarget, sentinel)
				resolvedOnlyA := nativeSupportResolve(t, hcc.TargetFactory, machine, []resolver.ResolvedArtifact{providerA})
				if _, err := Run(context.Background(), repo, resolvedOnlyA, opts...); err != nil {
					t.Fatal(err)
				}
				remaining := nativeSupportCommands(t, loc, filepath.Join(supportBase, filepath.FromSlash(hookFile)), 1)
				wantCommand := loc.repoBase
				if machine {
					wantCommand = filepath.ToSlash(installedA)
				} else {
					rel, err := filepath.Rel(repo, installedA)
					if err != nil {
						t.Fatal(err)
					}
					wantCommand += filepath.ToSlash(rel)
				}
				if remaining[0] != wantCommand {
					t.Fatalf("remaining hook command = %q, want %q", remaining[0], wantCommand)
				}
				assertNativeSupportSnapshot(t, beforeDeselect, nativeSupportSnapshot(t, installedA, installedATarget, sentinel))
				if _, err := os.Lstat(installedB); !os.IsNotExist(err) {
					t.Fatalf("deselected hook support file still present at %s: %v", installedB, err)
				}
				if _, err := os.Stat(filepath.Dir(installedB)); !os.IsNotExist(err) {
					t.Fatalf("deselected hook support directory still present: %v", err)
				}
			})
		}
	}
}

func TestNativeHookSupportSameIdentityConflictBlocksBoth(t *testing.T) {
	for _, tc := range []struct {
		name       string
		first      string
		second     string
		firstMode  os.FileMode
		secondMode os.FileMode
	}{
		{name: "different bytes", first: "echo one\n", second: "echo two\n", firstMode: 0o755, secondMode: 0o755},
		{name: "different permissions", first: "echo same\n", second: "echo same\n", firstMode: 0o755, secondMode: 0o644},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, home := additiveSafetyEnv(t)
			root := t.TempDir()
			makeProvider := func(dir, body string, mode os.FileMode) resolver.ResolvedArtifact {
				sourceRoot := filepath.Join(root, dir)
				hooksDir := filepath.Join(sourceRoot, "hooks")
				definition := filepath.Join(hooksDir, "hooks.json")
				additiveSafetyWrite(t, definition, `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/check.sh"}]}]}`)
				check := filepath.Join(hooksDir, "check.sh")
				additiveSafetyWrite(t, check, body)
				if err := os.Chmod(check, mode); err != nil {
					t.Fatal(err)
				}
				loaded, err := catalog.Load(sourceRoot, "claude")
				if err != nil || len(loaded.Entries) != 1 || loaded.Entries[0].Kind != ir.KindHook {
					t.Fatalf("Claude catalog ingest = %+v / %v", loaded.Entries, err)
				}
				entry := loaded.Entries[0]
				entry.Name = "guard"
				return resolver.ResolvedArtifact{
					Entry:       entry,
					RegistryURL: "https://github.com/Adyen/agent-marketplace.git",
					Ref:         "main",
					V4Kind:      config.V4SourceGit,
					CatalogKey:  sourceRoot,
					Source:      config.RepoCommitted,
					Harnesses:   []hcc.Target{hcc.TargetFactory},
				}
			}
			seed := makeProvider("first", tc.first, tc.firstMode)
			if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{seed}); err != nil {
				t.Fatalf("seed prior installation: %v", err)
			}
			factoryRoot := filepath.Join(repo, ".factory")
			hookFile := filepath.Join(factoryRoot, "hooks.json")
			supportRoot := filepath.Join(factoryRoot, "hook-support")
			matches, err := filepath.Glob(filepath.Join(supportRoot, "*", "guard", "check.sh"))
			if err != nil || len(matches) != 1 {
				t.Fatalf("seed support file: %v (%v)", matches, err)
			}
			seedInfo, err := os.Stat(matches[0])
			if err != nil {
				t.Fatalf("stat seeded support target: %v", err)
			}
			if seedInfo.Mode().Perm() != tc.firstMode.Perm() {
				t.Fatalf("seed target mode = %o, want %o", seedInfo.Mode().Perm(), tc.firstMode.Perm())
			}
			cacheCopy := nativeSupportResolvedTarget(t, matches[0])
			before := nativeSupportSnapshot(t, hookFile, supportRoot, cacheCopy)
			factoryMachineBefore := nativeSupportSnapshot(t, filepath.Join(home, ".factory"))
			cursor := nativeSupportLocationFor(t, hcc.TargetCursor)
			cursorRepoRoot := filepath.Join(repo, strings.Split(filepath.ToSlash(cursor.repoFile), "/")[0])
			cursorRepoBefore := nativeSupportSnapshot(t, cursorRepoRoot)

			first := makeProvider("first", tc.first, tc.firstMode)
			second := makeProvider("second", tc.second, tc.secondMode)
			independent := nativeSupportSource(t, "claude", "hooks/hooks.json", nativeSupportClaudeFiles("${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"))
			otherScope := nativeSupportResolve(t, hcc.TargetCursor, true, []resolver.ResolvedArtifact{independent})
			report, err := Run(context.Background(), repo, append([]resolver.ResolvedArtifact{first, second}, otherScope...))
			if err != nil {
				t.Fatalf("conflict run: %v", err)
			}
			var conflicts int
			for _, d := range report.Diags {
				if d.Severity == diag.SeverityError && d.Code != diag.CodeMaterializeHookSupportConflict {
					t.Fatalf("unexpected error diagnostic: %+v", d)
				}
				if d.Code == diag.CodeMaterializeHookSupportConflict && d.Severity == diag.SeverityError {
					conflicts++
				}
			}
			if conflicts != 2 {
				t.Fatalf("support conflict diagnostics = %d, want one error per producer: %v", conflicts, report.Diags)
			}
			assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, hookFile, supportRoot, cacheCopy))
			assertNativeSupportSnapshot(t, factoryMachineBefore, nativeSupportSnapshot(t, filepath.Join(home, ".factory")))
			assertNativeSupportSnapshot(t, cursorRepoBefore, nativeSupportSnapshot(t, cursorRepoRoot))

			cursorFile := filepath.Join(home, filepath.FromSlash(cursor.userFile))
			commands := nativeSupportCommands(t, cursor, cursorFile, 1)
			if !strings.HasPrefix(commands[0], filepath.Join(home, filepath.FromSlash(cursor.userSupportRoot))) {
				t.Fatalf("independent machine hook does not reference its support directory: %q", commands[0])
			}
			if _, err := os.Lstat(commands[0]); err != nil {
				t.Fatalf("independent machine support file missing at %s: %v", commands[0], err)
			}
		})
	}
}

func TestNativeHookSupportExcludesSourceAggregates(t *testing.T) {
	cases := []struct {
		name          string
		sourceHarness string
		target        hcc.Target
		defRel        string
		files         map[string]string
		supportRoot   string
		excluded      []string
		wantFiles     []string
	}{
		{
			name:          "devin native",
			sourceHarness: "devin",
			target:        hcc.TargetDevin,
			defRel:        ".devin/hooks.v1.json",
			files: map[string]string{
				".devin/hooks.v1.json":        `{"PreToolUse":[{"matcher":"exec","hooks":[{"type":"command","command":"./run.sh"}]}]}`,
				".devin/config.json":          `{"hooks":{}}`,
				".devin/mcp_config.json":      `{"mcpServers":{}}`,
				".devin/run.sh":               "echo run\n",
				".devin/payload.json":         `{"a":1}`,
				".devin/nested/tool.sh":       "echo nested\n",
				".devin/nested/hooks.v1.json": "nested payload\n",
				".devin/nested/config.json":   "nested payload\n",
			},
			supportRoot: ".devin/hook-support",
			excluded:    []string{"hooks.v1.json", "config.json", "mcp_config.json"},
			wantFiles:   []string{"run.sh", "payload.json", "nested/tool.sh", "nested/hooks.v1.json", "nested/config.json"},
		},
		{
			name:          "codex native",
			sourceHarness: "codex",
			target:        hcc.Target("codex"),
			defRel:        ".codex/hooks.json",
			files: map[string]string{
				".codex/hooks.json":         `{"hooks":{"PreToolUse":[{"matcher":"^Bash$","hooks":[{"type":"command","command":"./run.sh"}]}]}}`,
				".codex/config.toml":        `model = "x"`,
				".codex/run.sh":             "echo run\n",
				".codex/payload.json":       `{"a":1}`,
				".codex/nested/tool.sh":     "echo nested\n",
				".codex/nested/hooks.json":  "nested payload\n",
				".codex/nested/config.toml": "nested payload\n",
			},
			supportRoot: ".codex/hook-support",
			excluded:    []string{"hooks.json", "config.toml"},
			wantFiles:   []string{"run.sh", "payload.json", "nested/tool.sh", "nested/hooks.json", "nested/config.toml"},
		},
		{
			name:          "factory native",
			sourceHarness: "factory",
			target:        hcc.TargetFactory,
			defRel:        ".factory/hooks.json",
			files: map[string]string{
				".factory/hooks.json":        `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"./run.sh"}]}]}`,
				".factory/mcp.json":          `{"mcpServers":{}}`,
				".factory/run.sh":            "echo run\n",
				".factory/payload.json":      `{"a":1}`,
				".factory/nested/tool.sh":    "echo nested\n",
				".factory/nested/hooks.json": "nested payload\n",
				".factory/nested/mcp.json":   "nested payload\n",
			},
			supportRoot: ".factory/hook-support",
			excluded:    []string{"hooks.json", "mcp.json"},
			wantFiles:   []string{"run.sh", "payload.json", "nested/tool.sh", "nested/hooks.json", "nested/mcp.json"},
		},
		{
			name:          "factory legacy inventory",
			sourceHarness: "factory",
			target:        hcc.TargetFactory,
			defRel:        ".factory/hooks/hooks.json",
			files: map[string]string{
				".factory/hooks/hooks.json":        `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"./run.sh"}]}]}`,
				".factory/hooks/mcp.json":          "payload in legacy definition directory\n",
				".factory/hooks/run.sh":            "echo run\n",
				".factory/hooks/payload.json":      `{"a":1}`,
				".factory/hooks/nested/tool.sh":    "echo nested\n",
				".factory/hooks/nested/hooks.json": "nested payload\n",
				".factory/mcp.json":                `{"mcpServers":{}}`,
			},
			supportRoot: ".factory/hook-support",
			wantFiles:   []string{"mcp.json", "run.sh", "payload.json", "nested/tool.sh", "nested/hooks.json"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, _ := additiveSafetyEnv(t)
			provider := nativeSupportSource(t, tc.sourceHarness, tc.defRel, tc.files)
			resolved := nativeSupportResolve(t, tc.target, false, []resolver.ResolvedArtifact{provider})
			report, err := Run(context.Background(), repo, resolved)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if hasErrorDiagnostics(report.Diags) {
				t.Fatalf("unexpected errors: %v", report.Diags)
			}
			root := filepath.Join(repo, filepath.FromSlash(tc.supportRoot))
			hookDirs, err := filepath.Glob(filepath.Join(root, "*", "*"))
			if err != nil || len(hookDirs) != 1 {
				t.Fatalf("hook support namespace under %s: %v (%v)", root, hookDirs, err)
			}
			if filepath.Base(hookDirs[0]) != provider.Name {
				t.Fatalf("hook support directory = %s, want installed name %s", filepath.Base(hookDirs[0]), provider.Name)
			}
			installed := make(map[string]bool)
			if err := filepath.Walk(hookDirs[0], func(file string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
					rel, err := filepath.Rel(hookDirs[0], file)
					if err != nil {
						return err
					}
					installed[filepath.ToSlash(rel)] = true
				}
				return nil
			}); err != nil {
				t.Fatalf("walk support dir %s: %v", hookDirs[0], err)
			}
			for _, excluded := range tc.excluded {
				if installed[excluded] {
					t.Fatalf("native aggregate sibling %s leaked into support directory", excluded)
				}
			}
			if len(installed) != len(tc.wantFiles) {
				t.Fatalf("support files = %v, want %v", installed, tc.wantFiles)
			}
			for _, want := range tc.wantFiles {
				if !installed[want] {
					t.Fatalf("support file %s missing; got %v", want, installed)
				}
			}
		})
	}
}

func TestNativeHookSupportUnverifiedMirrorDirectoryRefuses(t *testing.T) {
	mirrorDir := t.TempDir()
	mirrorDefinition := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"}]}]}}`
	additiveSafetyWrite(t, filepath.Join(mirrorDir, "hooks", "hooks.json"), mirrorDefinition)
	additiveSafetyWrite(t, filepath.Join(mirrorDir, "scripts", "check.sh"), "echo mirror\n")
	mirrorCatalog, err := catalog.Load(mirrorDir, "claude")
	if err != nil || len(mirrorCatalog.Entries) != 1 || mirrorCatalog.Entries[0].Kind != ir.KindHook {
		t.Fatalf("Mirror source ingest = %+v / %v", mirrorCatalog.Entries, err)
	}
	mirrorEntry := mirrorCatalog.Entries[0]
	mirrorEntry.Path = mirrorDir
	mirror := resolver.ResolvedArtifact{
		Entry:                   mirrorEntry,
		RegistryURL:             "https://example.invalid/registry.git",
		RegistryPath:            "hooks",
		Ref:                     "main",
		Source:                  config.RepoCommitted,
		IsMarketplace:           true,
		MarketplaceAcquisition:  config.MarketplaceAcquisitionMirror,
		MarketplaceRegistryID:   "registry",
		MarketplaceResourceName: mirrorEntry.Name,
		InstalledName:           "guard",
		Harnesses:               []hcc.Target{hcc.TargetFactory},
	}
	t.Run("unverified mirror directory", func(t *testing.T) {
		repo, _ := additiveSafetyEnv(t)
		plans, _ := prepareHookSupport([]skillTarget{{skill: mirror, target: hcc.TargetFactory}})
		if len(plans) != 1 || plans[0].sourceDigest != "" {
			t.Fatalf("unverified mirror directory plans = %+v, want no source proof", plans)
		}
		report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{mirror})
		if err != nil {
			t.Fatalf("unverified mirror support run: %v", err)
		}
		refused := false
		for _, diagnostic := range report.Diags {
			if diagnostic.Code == "materialize.compile.error" && diagnostic.Severity == diag.SeverityError && diagnostic.Target == string(hcc.TargetFactory) {
				refused = true
			}
		}
		if !refused {
			t.Fatalf("unverified mirror hook was not refused: %+v", report.Diags)
		}
		for _, path := range []string{
			filepath.Join(repo, ".factory", "hooks.json"),
			filepath.Join(repo, ".factory", "hooks", "hooks.json"),
			filepath.Join(repo, ".factory", "hook-support"),
		} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("unverified mirror input produced %s: %v", path, err)
			}
		}
	})

}

func TestNativeHookSupportPinnedGitSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name         string
		registryPath string
		pinned       bool
	}{
		{name: "source_relative_definition", pinned: true},
		{name: "registry_path_prefix", registryPath: "teams/a", pinned: true},
		{name: "non_pinned_readable_git_source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _ := additiveSafetyEnv(t)
			readableRoot := t.TempDir()
			snapshotRoot := t.TempDir()
			relativeDir := ".factory"
			if tc.registryPath != "" {
				relativeDir = filepath.ToSlash(filepath.Join(tc.registryPath, relativeDir))
			}
			relativeDefinition := filepath.ToSlash(filepath.Join(relativeDir, "hooks.json"))
			relativeScript := filepath.ToSlash(filepath.Join(relativeDir, "scripts", "check.sh"))
			relativeMCP := filepath.ToSlash(filepath.Join(relativeDir, "mcp.json"))
			command := "${DROID_PLUGIN_ROOT}/scripts/check.sh"
			definition := fmt.Sprintf(`{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":%q}]}]}`, command)
			writeSource := func(root, script string, mode os.FileMode) {
				t.Helper()
				additiveSafetyWrite(t, filepath.Join(root, filepath.FromSlash(relativeDefinition)), definition)
				scriptPath := filepath.Join(root, filepath.FromSlash(relativeScript))
				additiveSafetyWrite(t, scriptPath, script)
				if err := os.Chmod(scriptPath, mode); err != nil {
					t.Fatal(err)
				}
				additiveSafetyWrite(t, filepath.Join(root, filepath.FromSlash(relativeMCP)), `{"mcpServers":{"peer":{"command":"echo"}}}`)
			}
			writeSource(readableRoot, "echo WRONG unpinned\n", 0o644)
			writeSource(snapshotRoot, "echo pinned\n", 0o755)

			wantScript, wantMode := "echo WRONG unpinned\n", os.FileMode(0o644)
			if tc.pinned {
				wantScript, wantMode = "echo pinned\n", 0o755
			}
			ingestedRoot := readableRoot
			if tc.pinned {
				ingestedRoot = snapshotRoot
			}
			definitionPath := filepath.Join(ingestedRoot, filepath.FromSlash(relativeDefinition))
			catalogRoot := ingestedRoot
			if tc.registryPath != "" {
				catalogRoot = filepath.Join(ingestedRoot, filepath.FromSlash(tc.registryPath))
			}
			loaded, err := catalog.Load(catalogRoot, "factory")
			if err != nil {
				t.Fatalf("Factory source ingest: %v", err)
			}
			if len(loaded.Entries) == 0 {
				t.Fatal("Factory source ingest produced no entries")
			}
			catalogDefinition, err := filepath.Rel(catalogRoot, definitionPath)
			if err != nil {
				t.Fatal(err)
			}
			catalogDefinition = filepath.ToSlash(catalogDefinition)
			var entry catalog.Entry
			var hooks int
			for _, candidate := range loaded.Entries {
				if candidate.Kind == ir.KindHook && strings.HasSuffix(filepath.ToSlash(candidate.Path), catalogDefinition) {
					entry = candidate
					hooks++
				}
			}
			if hooks != 1 {
				t.Fatalf("Factory source hook count = %d, want one: %+v", hooks, loaded.Entries)
			}
			ingestedHook, ok := entry.Resource.(*ir.Hook)
			if !ok || ingestedHook.SourceDefinitionDigest() == "" {
				t.Fatalf("Factory source hook is not bound to its written definition: %#v", entry.Resource)
			}
			entry.Path = definitionPath
			provider := resolver.ResolvedArtifact{
				Entry:         entry,
				RegistryURL:   "https://github.com/Adyen/agent-marketplace.git",
				RegistryPath:  tc.registryPath,
				Ref:           "main",
				V4Kind:        config.V4SourceGit,
				Source:        config.RepoCommitted,
				CatalogKey:    ingestedRoot,
				InstalledName: "pinned-check",
				Harnesses:     []hcc.Target{hcc.TargetFactory},
			}
			before := nativeSupportSnapshot(t, readableRoot, snapshotRoot)
			report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
			if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
				t.Fatal("Git hook support materialization failed")
			}
			assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, readableRoot, snapshotRoot))

			loc := nativeSupportLocationFor(t, hcc.TargetFactory)
			hookFile := filepath.Join(repo, filepath.FromSlash(loc.repoFile))
			command = nativeSupportCommands(t, loc, hookFile, 1)[0]

			matches, err := filepath.Glob(filepath.Join(repo, filepath.FromSlash(loc.repoSupportRoot), "*", "pinned-check", "scripts", "check.sh"))
			if err != nil || len(matches) != 1 {
				t.Fatal("installed support script is missing")
			}
			installed := matches[0]
			data, err := os.ReadFile(installed)
			if err != nil || string(data) != wantScript {
				t.Fatal("installed support script did not come from the expected source")
			}
			info, err := os.Stat(installed)
			if err != nil || info.Mode().Perm() != wantMode {
				t.Fatal("installed support script mode did not come from the expected source")
			}
			rel, err := filepath.Rel(repo, installed)
			if err != nil || command != loc.repoBase+filepath.ToSlash(rel) {
				t.Fatal("Factory hook command did not point to its installed support script")
			}
			mcpMatches, err := filepath.Glob(filepath.Join(repo, filepath.FromSlash(loc.repoSupportRoot), "*", "pinned-check", "mcp.json"))
			if err != nil || len(mcpMatches) != 0 {
				t.Fatal("native MCP peer was installed as hook support")
			}
		})
	}
}

func TestNativeHookSupportWindsurfUsesGenericRootAndKeepsLegacyComponent(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	legacySupport := filepath.Join(repo, ".windsurf", "hook-support")
	sentinel := filepath.Join(legacySupport, "handwritten.txt")
	additiveSafetyWrite(t, sentinel, "keep legacy bytes")
	if err := os.Chmod(sentinel, 0o640); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(repo, ".windsurf", "workflows", "manual.md")
	additiveSafetyWrite(t, other, "keep workflow bytes")
	beforeLegacy := nativeSupportSnapshot(t, legacySupport, other)
	provider := nativeSupportSource(t, "claude", "hooks/hooks.json", nativeSupportClaudeFiles("${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"))
	resolved := nativeSupportResolve(t, hcc.TargetWindsurf, false, []resolver.ResolvedArtifact{provider})
	report, err := Run(context.Background(), repo, resolved)
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Windsurf support run: %v %v", err, report.Diags)
	}
	newRoot := filepath.Join(repo, "hook-support")
	matches, err := filepath.Glob(filepath.Join(newRoot, "*", "*", "scripts", "check.sh"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("Windsurf support file not installed under repository root: %v (%v)", matches, err)
	}
	commands := nativeSupportCommands(t, nativeSupportLocationFor(t, hcc.TargetWindsurf), filepath.Join(repo, ".devin", "hooks.json"), 1)
	if !strings.HasPrefix(commands[0], "hook-support/") {
		t.Fatalf("Windsurf repository command = %q, want repository-relative support path", commands[0])
	}
	if _, err := os.Lstat(filepath.Join(repo, filepath.FromSlash(commands[0]))); err != nil {
		t.Fatalf("Windsurf command target is missing: %v", err)
	}
	assertNativeSupportSnapshot(t, beforeLegacy, nativeSupportSnapshot(t, legacySupport, other))
}

func TestNativeHookSupportRequiresConfiguredIdentity(t *testing.T) {
	t.Run("plain command and token without identity", func(t *testing.T) {
		repo, home := additiveSafetyEnv(t)
		plainProvider := nativeSupportSource(t, "claude", "hooks/hooks.json", map[string]string{
			"hooks/hooks.json": `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo plain"}]}]}`,
		}, "plain")
		plain := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{plainProvider})
		report, err := Run(context.Background(), repo, plain)
		if err != nil || hasErrorDiagnostics(report.Diags) {
			t.Fatalf("plain source-backed hook: %v %v", err, report.Diags)
		}
		loc := nativeSupportLocationFor(t, hcc.TargetFactory)
		hookFile := filepath.Join(repo, filepath.FromSlash(loc.repoFile))
		commands := nativeSupportCommands(t, loc, hookFile, 1)
		if commands[0] != "echo plain" {
			t.Fatalf("plain command = %q, want unchanged", commands[0])
		}
		if _, err := os.Lstat(filepath.Join(repo, filepath.FromSlash(loc.repoSupportRoot))); !os.IsNotExist(err) {
			t.Fatalf("plain hook created support directory: %v", err)
		}

		before := nativeSupportPersistentSnapshot(t, repo, home)
		token := nativeSupportSource(t, "claude", "hooks/hooks.json", nativeSupportClaudeFiles("${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"), "token")
		token.Path = ""
		token.RegistryURL = ""
		token.CatalogKey = ""
		token.Source = config.RepoCommitted
		token.Harnesses = []hcc.Target{hcc.TargetFactory}
		report, err = Run(context.Background(), repo, []resolver.ResolvedArtifact{token})
		if err != nil {
			t.Fatalf("identityless token hook: %v", err)
		}
		var supportPathRefused bool
		for _, d := range report.Diags {
			if d.Code == "materialize.compile.error" && d.Resource == "token" && d.Severity == diag.SeverityError {
				supportPathRefused = true
			}
		}
		if !supportPathRefused {
			t.Fatalf("identityless token hook did not fail closed: %v", report.Diags)
		}
		assertNativeSupportSnapshot(t, before, nativeSupportPersistentSnapshot(t, repo, home))
	})

	t.Run("physical support requires identity", func(t *testing.T) {
		repo, home := additiveSafetyEnv(t)
		provider := nativeSupportSource(t, "claude", "hooks/hooks.json", nativeSupportClaudeFiles("${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"))
		resolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{provider})
		if _, err := Run(context.Background(), repo, resolved); err != nil {
			t.Fatalf("configured support control: %v", err)
		}
		loc := nativeSupportLocationFor(t, hcc.TargetFactory)
		installed, err := filepath.Glob(filepath.Join(repo, filepath.FromSlash(loc.repoSupportRoot), "*", "*", "scripts", "check.sh"))
		if err != nil || len(installed) != 1 {
			t.Fatalf("configured support control missing: %v (%v)", installed, err)
		}
		before := nativeSupportPersistentSnapshot(t, repo, home)
		unconfigured := provider
		unconfigured.RegistryURL = ""
		unconfigured.CatalogKey = ""
		unconfigured.Source = config.RepoCommitted
		unconfigured.Harnesses = []hcc.Target{hcc.TargetFactory}
		report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{unconfigured})
		if err != nil {
			t.Fatalf("unconfigured support source: %v", err)
		}
		requireNativeSupportCompileError(t, report, nativeSupportScriptBody)
		assertNativeSupportSnapshot(t, before, nativeSupportPersistentSnapshot(t, repo, home))
	})
}

func TestNativeHookSupportRefusesUnsafeSource(t *testing.T) {
	const protected = "support-payload-must-not-leak"
	cases := []struct {
		name   string
		mutate func(t *testing.T, root string) []string
	}{
		{
			name: "support symlink",
			mutate: func(t *testing.T, root string) []string {
				script := filepath.Join(root, "hooks", "scripts", "check.sh")
				outside := filepath.Join(t.TempDir(), "outside.sh")
				additiveSafetyWrite(t, outside, protected)
				if err := os.Remove(script); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, script); err != nil {
					t.Fatal(err)
				}
				return []string{outside}
			},
		},
		{
			name: "dangling support symlink",
			mutate: func(t *testing.T, root string) []string {
				script := filepath.Join(root, "hooks", "scripts", "check.sh")
				if err := os.Remove(script); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, "missing"), script); err != nil {
					t.Fatal(err)
				}
				return nil
			},
		},
		{
			name: "support directory symlink",
			mutate: func(t *testing.T, root string) []string {
				scripts := filepath.Join(root, "hooks", "scripts")
				outside := t.TempDir()
				outsideScript := filepath.Join(outside, "check.sh")
				additiveSafetyWrite(t, outsideScript, protected)
				if err := os.RemoveAll(scripts); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, scripts); err != nil {
					t.Fatal(err)
				}
				return []string{outside}
			},
		},
		{
			name: "definition ancestor symlink",
			mutate: func(t *testing.T, root string) []string {
				hooks := filepath.Join(root, "hooks")
				outside := t.TempDir()
				additiveSafetyWrite(t, filepath.Join(outside, "hooks.json"), `{"PreToolUse":[]}`)
				additiveSafetyWrite(t, filepath.Join(outside, "scripts", "check.sh"), protected)
				if err := os.RemoveAll(hooks); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, hooks); err != nil {
					t.Fatal(err)
				}
				return []string{outside}
			},
		},
		{
			name: "definition symlink",
			mutate: func(t *testing.T, root string) []string {
				definition := filepath.Join(root, "hooks", "hooks.json")
				data, err := os.ReadFile(definition)
				if err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "hooks.json")
				additiveSafetyWrite(t, outside, string(data))
				if err := os.Remove(definition); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, definition); err != nil {
					t.Fatal(err)
				}
				return []string{outside}
			},
		},
		{
			name: "support hard link",
			mutate: func(t *testing.T, root string) []string {
				script := filepath.Join(root, "hooks", "scripts", "check.sh")
				outside := filepath.Join(t.TempDir(), "outside.sh")
				additiveSafetyWrite(t, outside, protected)
				if err := os.Chmod(outside, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(script); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(outside, script); err != nil {
					t.Fatal(err)
				}
				return []string{outside}
			},
		},
		{
			name: "definition hard link",
			mutate: func(t *testing.T, root string) []string {
				definition := filepath.Join(root, "hooks", "hooks.json")
				data, err := os.ReadFile(definition)
				if err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "hooks.json")
				additiveSafetyWrite(t, outside, string(data))
				if err := os.Remove(definition); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(outside, definition); err != nil {
					t.Fatal(err)
				}
				return []string{outside}
			},
		},
		{
			name: "oversized support file",
			mutate: func(t *testing.T, root string) []string {
				file, err := os.Create(filepath.Join(root, "hooks", "large.bin"))
				if err != nil {
					t.Fatal(err)
				}
				if err := file.Truncate(hookSourceFingerprintMaxBytes + 1); err != nil {
					_ = file.Close()
					t.Fatal(err)
				}
				if err := file.Chmod(0o600); err != nil {
					_ = file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				return nil
			},
		},
		{
			name: "oversized definition",
			mutate: func(t *testing.T, root string) []string {
				definition := filepath.Join(root, "hooks", "hooks.json")
				if err := os.Remove(definition); err != nil {
					t.Fatal(err)
				}
				file, err := os.Create(definition)
				if err != nil {
					t.Fatal(err)
				}
				if err := file.Truncate(hookSourceFingerprintMaxBytes + 1); err != nil {
					_ = file.Close()
					t.Fatal(err)
				}
				if err := file.Chmod(0o600); err != nil {
					_ = file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				return nil
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, home := additiveSafetyEnv(t)
			provider := nativeSupportSource(t, "claude", "hooks/hooks.json", nativeSupportClaudeFiles("${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"))
			if err := os.WriteFile(filepath.Join(provider.RegistryURL, "hooks", "scripts", "check.sh"), []byte(protected), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(provider.RegistryURL, "hooks", "scripts", "check.sh"), 0o755); err != nil {
				t.Fatal(err)
			}
			resolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{provider})
			if _, err := Run(context.Background(), repo, resolved); err != nil {
				t.Fatalf("seed prior installation: %v", err)
			}
			before := nativeSupportPersistentSnapshot(t, repo, home)
			external := tc.mutate(t, provider.RegistryURL)
			externalBefore := nativeSupportSnapshot(t, external...)
			report, err := Run(context.Background(), repo, resolved)
			if err != nil {
				t.Fatalf("unsafe support run: %v", err)
			}
			requireNativeSupportCompileError(t, report, protected)
			assertNativeSupportSnapshot(t, before, nativeSupportPersistentSnapshot(t, repo, home))
			if len(external) > 0 {
				assertNativeSupportSnapshot(t, externalBefore, nativeSupportSnapshot(t, external...))
			}
		})
	}
}

func TestNativeHookSupportFIFOChild(t *testing.T) {
	if os.Getenv("NATIVE_HOOK_SUPPORT_FIFO_CHILD") != "1" {
		return
	}
	const protected = "fifo-source-payload-must-not-leak"
	repo, home := additiveSafetyEnv(t)
	provider := nativeSupportSource(t, "claude", "hooks/hooks.json", nativeSupportClaudeFiles("${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"))
	script := filepath.Join(provider.RegistryURL, "hooks", "scripts", "check.sh")
	if err := os.WriteFile(script, []byte(protected), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{provider})
	if _, err := Run(context.Background(), repo, resolved); err != nil {
		t.Fatalf("seed prior installation: %v", err)
	}
	before := nativeSupportPersistentSnapshot(t, repo, home)
	if err := os.Remove(script); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(script, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Run(context.Background(), repo, resolved)
	if err != nil {
		t.Fatalf("FIFO support run: %v", err)
	}
	requireNativeSupportCompileError(t, report, protected)
	assertNativeSupportSnapshot(t, before, nativeSupportPersistentSnapshot(t, repo, home))

	peerProvider := nativeSupportSource(t, "devin", ".devin/hooks.v1.json", map[string]string{
		".devin/hooks.v1.json":   `{"PreToolUse":[{"matcher":"exec","hooks":[{"type":"command","command":"./run.sh"}]}]}`,
		".devin/config.json":     `{"hooks":{"PreToolUse":[]}}`,
		".devin/mcp_config.json": `{"mcpServers":{}}`,
		".devin/run.sh":          "peer fifo control\n",
	})
	peerResolved := nativeSupportResolve(t, hcc.TargetDevin, false, []resolver.ResolvedArtifact{peerProvider})
	if _, err := Run(context.Background(), repo, peerResolved); err != nil {
		t.Fatalf("seed peer FIFO installation: %v", err)
	}
	peerBefore := nativeSupportPersistentSnapshot(t, repo, home)
	peer := filepath.Join(peerProvider.RegistryURL, ".devin", "config.json")
	if err := os.Remove(peer); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(peer, 0o600); err != nil {
		t.Fatal(err)
	}
	peerReport, err := Run(context.Background(), repo, peerResolved)
	if err != nil {
		t.Fatalf("excluded peer FIFO run: %v", err)
	}
	requireNativeSupportCompileError(t, peerReport, "")
	assertNativeSupportSnapshot(t, peerBefore, nativeSupportPersistentSnapshot(t, repo, home))
}

func TestNativeHookSupportFIFOIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeHookSupportFIFOChild$")
	cmd.Env = []string{"NATIVE_HOOK_SUPPORT_FIFO_CHILD=1", "TMPDIR=" + os.TempDir()}
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("FIFO support child exceeded deadline: %s", output)
	}
	if err != nil {
		t.Fatalf("FIFO support child: %v\n%s", err, output)
	}
}

func TestNativeHookSupportRefusesUnsafeExcludedNativePeer(t *testing.T) {
	repo, home := additiveSafetyEnv(t)
	provider := nativeSupportSource(t, "devin", ".devin/hooks.v1.json", map[string]string{
		".devin/hooks.v1.json":   `{"PreToolUse":[{"matcher":"exec","hooks":[{"type":"command","command":"./run.sh"}]}]}`,
		".devin/config.json":     `{"hooks":{}}`,
		".devin/mcp_config.json": `{"mcpServers":{}}`,
		".devin/run.sh":          "excluded peer control\n",
	})
	resolved := nativeSupportResolve(t, hcc.TargetDevin, false, []resolver.ResolvedArtifact{provider})
	if _, err := Run(context.Background(), repo, resolved); err != nil {
		t.Fatalf("seed prior installation: %v", err)
	}
	before := nativeSupportPersistentSnapshot(t, repo, home)
	outside := filepath.Join(t.TempDir(), "config.json")
	const protected = "excluded-peer-payload-must-not-leak"
	additiveSafetyWrite(t, outside, `{"hooks":{"sentinel":"`+protected+`"}}`)
	peer := filepath.Join(provider.RegistryURL, ".devin", "config.json")
	if err := os.Remove(peer); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, peer); err != nil {
		t.Fatal(err)
	}
	externalBefore := nativeSupportSnapshot(t, outside)
	report, err := Run(context.Background(), repo, resolved)
	if err != nil {
		t.Fatalf("unsafe excluded peer run: %v", err)
	}
	requireNativeSupportCompileError(t, report, protected)
	assertNativeSupportSnapshot(t, before, nativeSupportPersistentSnapshot(t, repo, home))
	assertNativeSupportSnapshot(t, externalBefore, nativeSupportSnapshot(t, outside))
}

func nativeHookSupportCollisionInputs(t *testing.T) (string, resolver.ResolvedArtifact, resolver.ResolvedArtifact, hookSupportPlan, string) {
	t.Helper()
	repo, _ := additiveSafetyEnv(t)
	provider := nativeSupportSource(t, "claude", "hooks/hooks.json", map[string]string{
		"hooks/hooks.json":       `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"}]}]}`,
		"hooks/scripts/check.sh": "#!/bin/sh\necho source support\n",
	}, "affected")
	if err := os.Chmod(filepath.Join(provider.RegistryURL, "hooks", "scripts", "check.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	factoryHook := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{provider})[0]
	supportPlan, supportPath := nativeHookActivationSupportPath(t, factoryHook, hcc.TargetFactory, false, filepath.Join(repo, ".factory"))
	sibling, _ := namedHookFixture(t, config.RepoCommitted, hcc.TargetClaude, "sibling")
	return repo, factoryHook, sibling, supportPlan, supportPath
}

func TestNativeHookSupportDestinationPreflightRefusesBeforePublication(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode), func(t *testing.T) {
			runNativeHookSupportDestinationPreflightCase(t, mode, false)
		})
	}
	t.Run("unsafe ancestor", func(t *testing.T) {
		runNativeHookSupportDestinationPreflightCase(t, config.MaterializationModeSymlink, true)
	})
}

func runNativeHookSupportDestinationPreflightCase(t *testing.T, mode config.MaterializationMode, unsafeAncestor bool) {
	t.Helper()
	repo, factoryHook, sibling, plan, supportPath := nativeHookSupportCollisionInputs(t)
	factoryRoot := filepath.Join(repo, ".factory")
	nativePath := filepath.Join(factoryRoot, "hooks.json")
	legacyPath := filepath.Join(factoryRoot, "hooks", "hooks.json")
	legacyBytes := []byte(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"}],"_managed_by":"agent-marketplace"}]}` + "\n")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, legacyBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(legacyPath, 0o640); err != nil {
		t.Fatal(err)
	}
	legacyBefore, legacyMode, legacyMtime := fileSnapshot(t, legacyPath)
	factoryStore, err := ownership.Open(factoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(factoryStore.StateFile()); !os.IsNotExist(err) {
		t.Fatalf("Factory ownership state unexpectedly exists: %v", err)
	}
	assertPathAbsent(t, nativePath)

	var supportBefore []byte
	var supportMode os.FileMode
	var supportMtime time.Time
	var supportRoot, outside string
	var unsafeBefore map[string]nativeSupportState
	if unsafeAncestor {
		supportRoot = filepath.Join(factoryRoot, "hook-support")
		outside = t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "sentinel"), []byte("outside unchanged\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, supportRoot); err != nil {
			t.Fatal(err)
		}
		unsafeBefore = nativeSupportSnapshot(t, supportRoot, outside)
	} else {
		if err := os.MkdirAll(filepath.Dir(supportPath), 0o755); err != nil {
			t.Fatal(err)
		}
		supportBefore = []byte("unowned support collision\n")
		if err := os.WriteFile(supportPath, supportBefore, 0o640); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(supportPath, 0o640); err != nil {
			t.Fatal(err)
		}
		supportBefore, supportMode, supportMtime = fileSnapshot(t, supportPath)
	}
	cacheBefore := nativeSupportSnapshot(t, cache.CacheDir())
	modes := config.MaterializationModes{Project: mode, Machine: mode}
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{factoryHook, sibling}, WithMaterializationModes(modes))
	if err != nil || report == nil {
		t.Fatalf("Run: %v / %v", err, report)
	}
	assertFileUnchanged(t, legacyPath, legacyBefore, legacyMode, legacyMtime)
	assertPathAbsent(t, nativePath)
	if _, err := os.Lstat(factoryStore.StateFile()); !os.IsNotExist(err) {
		t.Fatalf("Factory support refusal created ownership state: %v", err)
	}
	if unsafeAncestor {
		assertNativeSupportSnapshot(t, unsafeBefore, nativeSupportSnapshot(t, supportRoot, outside))
	} else {
		assertFileUnchanged(t, supportPath, supportBefore, supportMode, supportMtime)
	}
	assertNativeSupportSnapshot(t, cacheBefore, nativeSupportSnapshot(t, cache.CacheDir()))
	wantCode := diag.CodeMaterializeHookSupportConflict
	refused := false
	for _, diagnostic := range report.Diags {
		if diagnostic.Code == wantCode && diagnostic.Severity == diag.SeverityError && diagnostic.Target == string(hcc.TargetFactory) && diagnostic.Resource == factoryHook.Name {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("Factory support destination refusal = %+v, want %s for %q", report.Diags, wantCode, factoryHook.Name)
	}
	var factoryBlocked, siblingBlocked bool
	for _, resource := range report.BlockedResources {
		factoryBlocked = factoryBlocked || resource.Target == hcc.TargetFactory && resource.Name == factoryHook.Name
		siblingBlocked = siblingBlocked || resource.Target == hcc.TargetClaude && resource.Name == sibling.Name
	}
	if !factoryBlocked || siblingBlocked {
		t.Fatalf("blocked resources = %+v, want only Factory %q", report.BlockedResources, factoryHook.Name)
	}
	claudeFile := filepath.Join(repo, ".claude", "settings.json")
	commands := nativeSupportCommands(t, nativeSupportLocationFor(t, hcc.TargetClaude), claudeFile, 1)
	if len(commands) != 1 || commands[0] != sharedSourceCommand {
		t.Fatalf("independent Claude sibling commands = %v", commands)
	}
	if len(plan.artifacts) != 1 || plan.artifacts[0].Mode.Perm() != 0o755 {
		t.Fatalf("support preflight fixture changed: %+v", plan.artifacts)
	}
}

type nativeHookSupportPublicationFixture struct {
	repo           string
	factoryRoot    string
	nativePath     string
	statePath      string
	legacyPath     string
	oldSupportPath string
	manifestPath   string
	supportPath    string
	hook           resolver.ResolvedArtifact
	supportPlan    hookSupportPlan
	mode           config.MaterializationMode
}

type nativeHookSupportPublicationSnapshot struct {
	legacyBytes      []byte
	legacyMode       os.FileMode
	legacyMtime      time.Time
	manifestBytes    []byte
	manifestMode     os.FileMode
	manifestMtime    time.Time
	oldSupportBytes  []byte
	oldSupportMode   os.FileMode
	oldSupportMtime  time.Time
	oldSupportLink   nativeHookSymlinkSnapshot
	oldSupportIsLink bool
}

func newNativeHookSupportPublicationFixture(t *testing.T, mode config.MaterializationMode) nativeHookSupportPublicationFixture {
	t.Helper()
	repo, hook, _, supportPlan, supportPath := nativeHookSupportCollisionInputs(t)
	factoryRoot := filepath.Join(repo, ".factory")
	nativePath := filepath.Join(factoryRoot, "hooks.json")
	legacyPath := filepath.Join(factoryRoot, "hooks", "hooks.json")
	oldSupportPath := filepath.Join(factoryRoot, "hooks", "scripts", "old.sh")
	oldCachePath := filepath.Join(cache.CacheDir(), "native-hook-manifest-fixture", "old.sh")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy, _ := nativeHookLegacyDocument(hcc.TargetFactory, "", "PreToolUse", "Execute", "${CLAUDE_PLUGIN_ROOT}/scripts/old.sh", false, false)
	if err := os.WriteFile(legacyPath, legacy, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(legacyPath, 0o640); err != nil {
		t.Fatal(err)
	}
	oldSupportBytes := []byte("old legacy support bytes\n")
	var oldSupportDigest string
	if mode == config.MaterializationModeSymlink {
		if err := os.MkdirAll(filepath.Dir(oldCachePath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(oldCachePath, oldSupportBytes, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(oldCachePath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(oldSupportPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(oldCachePath, oldSupportPath); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(oldSupportPath)
		if err != nil {
			t.Fatal(err)
		}
		linkTarget, err := os.Readlink(oldSupportPath)
		if err != nil {
			t.Fatal(err)
		}
		oldSupportDigest = nativeHookSymlinkSnapshotDigest(info.Mode(), linkTarget)
	} else {
		if err := os.MkdirAll(filepath.Dir(oldSupportPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(oldSupportPath, oldSupportBytes, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(oldSupportPath, 0o755); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(oldSupportPath)
		if err != nil {
			t.Fatal(err)
		}
		oldSupportDigest = nativeHookRegularSnapshotDigest(oldSupportBytes, info.Mode())
	}
	manifest, err := loadPortableState(repo, false)
	if err != nil {
		t.Fatal(err)
	}
	manifest.add(hcc.TargetFactory, "hooks/scripts/old.sh", oldSupportDigest)
	if err := manifest.save(false); err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(factoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	return nativeHookSupportPublicationFixture{
		repo: repo, factoryRoot: factoryRoot, nativePath: nativePath, statePath: store.StateFile(),
		legacyPath: legacyPath, oldSupportPath: oldSupportPath,
		manifestPath: manifest.path, supportPath: supportPath, hook: hook, supportPlan: supportPlan, mode: mode,
	}
}

func snapshotNativeHookSupportPublication(t *testing.T, fixture nativeHookSupportPublicationFixture) nativeHookSupportPublicationSnapshot {
	t.Helper()
	legacyBytes, legacyMode, legacyMtime := fileSnapshot(t, fixture.legacyPath)
	manifestBytes, manifestMode, manifestMtime := fileSnapshot(t, fixture.manifestPath)
	snapshot := nativeHookSupportPublicationSnapshot{
		legacyBytes: legacyBytes, legacyMode: legacyMode, legacyMtime: legacyMtime,
		manifestBytes: manifestBytes, manifestMode: manifestMode, manifestMtime: manifestMtime,
	}
	if fixture.mode == config.MaterializationModeSymlink {
		snapshot.oldSupportIsLink = true
		snapshot.oldSupportLink = snapshotNativeHookLegacyCacheLink(t, fixture.oldSupportPath)
	} else {
		snapshot.oldSupportBytes, snapshot.oldSupportMode, snapshot.oldSupportMtime = fileSnapshot(t, fixture.oldSupportPath)
	}
	return snapshot
}

func nativeHookSupportPublicationFileUnchanged(path string, data []byte, mode os.FileMode, mtime time.Time) bool {
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode() == mode && info.ModTime().Equal(mtime)
}

func nativeHookSupportPublicationOldSupportUnchanged(fixture nativeHookSupportPublicationFixture, before nativeHookSupportPublicationSnapshot) bool {
	if !before.oldSupportIsLink {
		return nativeHookSupportPublicationFileUnchanged(fixture.oldSupportPath, before.oldSupportBytes, before.oldSupportMode, before.oldSupportMtime)
	}
	data, err := os.ReadFile(fixture.oldSupportPath)
	if err != nil || !bytes.Equal(data, before.oldSupportLink.bytes) {
		return false
	}
	linkTarget, err := os.Readlink(fixture.oldSupportPath)
	if err != nil || linkTarget != before.oldSupportLink.target {
		return false
	}
	linkInfo, err := os.Lstat(fixture.oldSupportPath)
	if err != nil || linkInfo.Mode() != before.oldSupportLink.linkInfo.Mode() || !linkInfo.ModTime().Equal(before.oldSupportLink.linkInfo.ModTime()) {
		return false
	}
	targetInfo, err := os.Stat(fixture.oldSupportPath)
	return err == nil && targetInfo.Mode() == before.oldSupportLink.targetInfo.Mode() && targetInfo.ModTime().Equal(before.oldSupportLink.targetInfo.ModTime())
}

func nativeHookSupportPublicationNativeLegacyOldSupportUnchanged(fixture nativeHookSupportPublicationFixture, before nativeHookSupportPublicationSnapshot) bool {
	for _, path := range []string{fixture.nativePath, fixture.statePath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return false
		}
	}
	return nativeHookSupportPublicationFileUnchanged(fixture.legacyPath, before.legacyBytes, before.legacyMode, before.legacyMtime) &&
		nativeHookSupportPublicationOldSupportUnchanged(fixture, before)
}

func nativeHookSupportPublicationInputsUnchanged(fixture nativeHookSupportPublicationFixture, before nativeHookSupportPublicationSnapshot) bool {
	return nativeHookSupportPublicationNativeLegacyOldSupportUnchanged(fixture, before) &&
		nativeHookSupportPublicationFileUnchanged(fixture.manifestPath, before.manifestBytes, before.manifestMode, before.manifestMtime)
}

func nativeHookSupportPublicationBeforeNativeCommit(t *testing.T, fixture nativeHookSupportPublicationFixture, before nativeHookSupportPublicationSnapshot, supportPath string) bool {
	t.Helper()
	if !nativeHookSupportPublicationNativeLegacyOldSupportUnchanged(fixture, before) {
		return false
	}
	data, err := os.ReadFile(supportPath)
	if err != nil || !bytes.Equal(data, []byte("#!/bin/sh\necho source support\n")) {
		return false
	}
	info, err := os.Stat(supportPath)
	if err != nil || info.Mode().Perm() != 0o755 {
		return false
	}
	manifest, err := loadPortableState(fixture.repo, false)
	if err != nil {
		return false
	}
	if _, ok := manifest.entries[portableManifestKey(hcc.TargetFactory, "hooks/scripts/old.sh")]; !ok {
		return false
	}
	if fixture.mode == config.MaterializationModeFile {
		rel, err := filepath.Rel(fixture.factoryRoot, supportPath)
		if err != nil {
			return false
		}
		if _, ok := manifest.entries[portableManifestKey(hcc.TargetFactory, filepath.ToSlash(rel))]; !ok {
			return false
		}
	}
	return true
}

func assertNativeHookSupportPublicationFailureState(t *testing.T, fixture nativeHookSupportPublicationFixture, before nativeHookSupportPublicationSnapshot) {
	t.Helper()
	if _, err := os.Lstat(fixture.nativePath); !os.IsNotExist(err) {
		t.Fatalf("failed support publication created Native output: %v", err)
	}
	if _, err := os.Lstat(fixture.statePath); !os.IsNotExist(err) {
		t.Fatalf("failed support publication created ownership state: %v", err)
	}
	assertFileUnchanged(t, fixture.legacyPath, before.legacyBytes, before.legacyMode, before.legacyMtime)
	assertFileUnchanged(t, fixture.manifestPath, before.manifestBytes, before.manifestMode, before.manifestMtime)
	if before.oldSupportIsLink {
		assertNativeHookLegacyCacheLinkUnchanged(t, fixture.oldSupportPath, before.oldSupportLink)
	} else {
		assertFileUnchanged(t, fixture.oldSupportPath, before.oldSupportBytes, before.oldSupportMode, before.oldSupportMtime)
	}
}

func requireNativeHookSupportWriteError(t *testing.T, report *Report) {
	t.Helper()
	for _, diagnostic := range report.Diags {
		if diagnostic.Code == "materialize.write.error" && diagnostic.Severity == diag.SeverityError && diagnostic.Target == string(hcc.TargetFactory) {
			return
		}
	}
	t.Fatalf("missing Factory materialize.write.error: %+v", report.Diags)
}

// spec: hook-resources/owned-factory-entries-move-to-the-native-file
func TestNativeHookSupportPublicationFailurePreventsNativeCommit(t *testing.T) {
	const childEnv = "AGENT_MARKETPLACE_NATIVE_HOOK_SUPPORT_FIFO_CHILD"
	const modeEnv = "AGENT_MARKETPLACE_NATIVE_HOOK_SUPPORT_FIFO_MODE"
	if os.Getenv(childEnv) == "1" {
		mode := config.MaterializationMode(os.Getenv(modeEnv))
		runNativeHookSupportLateFIFOSwapCase(t, mode)
		return
	}
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode)+"/cache preparation", func(t *testing.T) {
			fixture := newNativeHookSupportPublicationFixture(t, mode)
			before := snapshotNativeHookSupportPublication(t, fixture)
			cacheRoot := cache.CacheDir()
			if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			compiled := filepath.Join(cacheRoot, "compiled")
			if err := os.WriteFile(compiled, []byte("not a cache directory\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			compiledBefore, compiledMode, compiledMtime := fileSnapshot(t, compiled)
			report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}))
			if err != nil || report == nil {
				t.Fatalf("Run: %v / %v", err, report)
			}
			requireNativeHookSupportWriteError(t, report)
			assertNativeHookSupportPublicationFailureState(t, fixture, before)
			assertPathAbsent(t, fixture.supportPath)
			assertFileUnchanged(t, compiled, compiledBefore, compiledMode, compiledMtime)
		})
		t.Run(string(mode)+"/late output collision", func(t *testing.T) {
			fixture := newNativeHookSupportPublicationFixture(t, mode)
			before := snapshotNativeHookSupportPublication(t, fixture)
			var callbackCalled, prepublicationState bool
			var foreignBytes []byte
			var foreignMode os.FileMode
			var foreignMtime time.Time
			fault := func(stage, destination string) error {
				if stage != "output" || filepath.Clean(destination) != filepath.Clean(fixture.supportPath) {
					return nil
				}
				callbackCalled = true
				prepublicationState = nativeHookSupportPublicationInputsUnchanged(fixture, before)
				if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
					return err
				}
				foreignBytes = []byte("late competing support bytes\n")
				if err := os.WriteFile(destination, foreignBytes, 0o640); err != nil {
					return err
				}
				if err := os.Chmod(destination, 0o640); err != nil {
					return err
				}
				foreignBytes, foreignMode, foreignMtime = fileSnapshot(t, destination)
				return nil
			}
			report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}), withNativeHookSupportWriteFault(fault))
			if err != nil || report == nil {
				t.Fatalf("Run: %v / %v", err, report)
			}
			if !callbackCalled {
				t.Fatal("support output fault callback was not reached after preflight")
			}
			if !prepublicationState {
				t.Fatal("support output stage ran after Native, legacy, old-support, or manifest mutation")
			}
			requireNativeHookSupportWriteError(t, report)
			assertNativeHookSupportPublicationFailureState(t, fixture, before)
			assertFileUnchanged(t, fixture.supportPath, foreignBytes, foreignMode, foreignMtime)
			blocked := false
			for _, resource := range report.BlockedResources {
				blocked = blocked || resource.Target == hcc.TargetFactory && resource.Name == fixture.hook.Name
			}
			if !blocked {
				t.Fatalf("failed support hook was not blocked: %+v", report.BlockedResources)
			}
		})
		t.Run(string(mode)+"/successful publication", func(t *testing.T) {
			fixture := newNativeHookSupportPublicationFixture(t, mode)
			before := snapshotNativeHookSupportPublication(t, fixture)
			var callbackCalled, prepublicationState, supportVerifiedBeforeProof bool
			fault := func(stage, destination string) error {
				if filepath.Clean(destination) != filepath.Clean(fixture.supportPath) {
					return nil
				}
				switch stage {
				case "output":
					callbackCalled = true
					prepublicationState = nativeHookSupportPublicationInputsUnchanged(fixture, before)
				case "before-native-commit":
					supportVerifiedBeforeProof = nativeHookSupportPublicationBeforeNativeCommit(t, fixture, before, destination)
				}
				return nil
			}
			report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}), withNativeHookSupportWriteFault(fault))
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("Run: %v / %v", err, report)
			}
			if !callbackCalled || !prepublicationState || !supportVerifiedBeforeProof {
				t.Fatalf("support was not verified before Native publication: called=%v prepublication=%v verified=%v", callbackCalled, prepublicationState, supportVerifiedBeforeProof)
			}
			supportBytes, err := os.ReadFile(fixture.supportPath)
			if err != nil || !bytes.Equal(supportBytes, []byte("#!/bin/sh\necho source support\n")) {
				t.Fatalf("installed support bytes = %q / %v", supportBytes, err)
			}
			supportInfo, err := os.Stat(fixture.supportPath)
			if err != nil || supportInfo.Mode().Perm() != 0o755 {
				t.Fatalf("installed support mode = %v / %v", supportInfo, err)
			}
			if _, err := os.Lstat(fixture.nativePath); err != nil {
				t.Fatalf("Native Hook output was not published: %v", err)
			}
			commands := nativeSupportCommands(t, nativeSupportLocationFor(t, hcc.TargetFactory), fixture.nativePath, 1)
			if len(commands) != 1 || !strings.HasSuffix(commands[0], "/scripts/check.sh") {
				t.Fatalf("Native Hook command = %v, want the installed check.sh support path", commands)
			}
			store, err := ownership.Open(fixture.factoryRoot)
			if err != nil {
				t.Fatal(err)
			}
			proof, err := store.Proof("hooks.json")
			if err != nil || proof.OwnedCount() != 1 {
				t.Fatalf("published Native proof = %v / %+v", err, proof.OwnedIDs())
			}
			entry, ok := proof.Entry(proof.OwnedIDs()[0])
			if !ok || entry.Resource != fixture.hook.Name || entry.SourceDigest != fixture.supportPlan.sourceDigest {
				t.Fatalf("published Native proof entry = %+v", entry)
			}
			assertPathAbsent(t, fixture.legacyPath)
			assertPathAbsent(t, fixture.oldSupportPath)
			manifest, err := loadPortableState(fixture.repo, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := manifest.entries[portableManifestKey(hcc.TargetFactory, "hooks/scripts/old.sh")]; ok {
				t.Fatal("legacy support manifest entry survived successful cleanup")
			}
			if mode == config.MaterializationModeFile {
				rel, err := filepath.Rel(fixture.factoryRoot, fixture.supportPath)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := manifest.entries[portableManifestKey(hcc.TargetFactory, filepath.ToSlash(rel))]; !ok {
					t.Fatal("new file-mode support was not recorded in the portable manifest")
				}
			}
		})
	}
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode)+"/late_fifo_verification", func(t *testing.T) {
			childTempRoot := t.TempDir()
			t.Setenv("TMPDIR", childTempRoot)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeHookSupportPublicationFailurePreventsNativeCommit$", "-test.v")
			cmd.Env = append(os.Environ(), childEnv+"=1", modeEnv+"="+string(mode))
			output, err := cmd.CombinedOutput()
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("bounded %s verifier child timed out on FIFO: %v\n%s", mode, err, output)
			}
			if err != nil {
				t.Fatalf("bounded %s verifier child: %v\n%s", mode, err, output)
			}
		})
	}
}

func runNativeHookSupportLateFIFOSwapCase(t *testing.T, mode config.MaterializationMode) {
	t.Helper()
	if mode != config.MaterializationModeFile && mode != config.MaterializationModeSymlink {
		t.Fatalf("unsupported materialization mode %q", mode)
	}
	fixture := newNativeHookSupportPublicationFixture(t, mode)
	before := snapshotNativeHookSupportPublication(t, fixture)
	manifestBefore, err := loadPortableState(fixture.repo, false)
	if err != nil {
		t.Fatal(err)
	}
	oldManifestKey := portableManifestKey(hcc.TargetFactory, "hooks/scripts/old.sh")
	oldManifestEntry, ok := manifestBefore.entries[oldManifestKey]
	if !ok {
		t.Fatal("fixture is missing the old support manifest entry")
	}
	var callbackCalled, supportVerified bool
	var outputBefore map[string]nativeSupportState
	var foreignRoot, foreignFIFO string
	var foreignBefore map[string]nativeSupportState
	var foreignLinkTarget string
	fault := func(stage, destination string) error {
		if stage != "before-native-commit" {
			return nil
		}
		callbackCalled = true
		if filepath.Clean(destination) != filepath.Clean(fixture.supportPath) {
			return fmt.Errorf("support verification path differs from fixture")
		}
		supportVerified = nativeHookSupportPublicationBeforeNativeCommit(t, fixture, before, destination)
		if !supportVerified {
			return fmt.Errorf("captured support was not installed before verification")
		}
		if mode == config.MaterializationModeFile {
			if err := os.Remove(destination); err != nil {
				return err
			}
			if err := syscall.Mkfifo(destination, 0o600); err != nil {
				return err
			}
		} else {
			foreignRoot = t.TempDir()
			foreignFIFO = filepath.Join(foreignRoot, "foreign.fifo")
			if err := syscall.Mkfifo(foreignFIFO, 0o600); err != nil {
				return err
			}
			if err := os.Remove(destination); err != nil {
				return err
			}
			if err := os.Symlink(foreignFIFO, destination); err != nil {
				return err
			}
			foreignLinkTarget, err = os.Readlink(destination)
			if err != nil {
				return err
			}
			foreignBefore = nativeSupportSnapshot(t, foreignRoot)
		}
		outputBefore = nativeSupportSnapshot(t, destination)
		return nil
	}
	report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}), withNativeHookSupportWriteFault(fault))
	if err != nil || report == nil {
		t.Fatalf("Run: %v / %v", err, report)
	}
	if !callbackCalled || !supportVerified {
		t.Fatalf("late support swap was not reached after successful install: callback=%v verified=%v", callbackCalled, supportVerified)
	}
	requireNativeHookSupportWriteError(t, report)
	if !nativeHookSupportPublicationNativeLegacyOldSupportUnchanged(fixture, before) {
		t.Fatal("late support verification failure published Native/state or removed legacy/old support")
	}
	assertNativeSupportSnapshot(t, outputBefore, nativeSupportSnapshot(t, fixture.supportPath))
	manifestAfter, err := loadPortableState(fixture.repo, false)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := manifestAfter.entries[oldManifestKey]; !ok || got != oldManifestEntry {
		t.Fatalf("old support manifest entry changed or disappeared: %+v", got)
	}
	if mode == config.MaterializationModeFile {
		info, err := os.Lstat(fixture.supportPath)
		if err != nil || info.Mode()&os.ModeNamedPipe == 0 || info.Mode().Perm() != 0o600 {
			t.Fatalf("file-mode FIFO was not preserved: %v / %v", info, err)
		}
	} else {
		target, err := os.Readlink(fixture.supportPath)
		if err != nil || target != foreignFIFO || target != foreignLinkTarget {
			t.Fatalf("foreign symlink text changed: %q / %v", target, err)
		}
		info, err := os.Lstat(foreignFIFO)
		if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			t.Fatalf("foreign FIFO was not preserved: %v / %v", info, err)
		}
		assertNativeSupportSnapshot(t, foreignBefore, nativeSupportSnapshot(t, foreignRoot))
	}
	blocked := false
	for _, resource := range report.BlockedResources {
		blocked = blocked || resource.Target == hcc.TargetFactory && resource.Name == fixture.hook.Name
	}
	if !blocked {
		t.Fatalf("failed support hook was not blocked: %+v", report.BlockedResources)
	}
}

// spec: materialization/machine-scope-hook-command-uses-the-absolute-machine-root
func TestNativeHookRepoRootAliasPreservesMachineFactorySupport(t *testing.T) {
	_, home := additiveSafetyEnv(t)
	provider := nativeSupportSource(t, "claude", "hooks/hooks.json", nativeSupportClaudeFiles("${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"), "user-factory-hook")
	selection := nativeSupportResolve(t, hcc.TargetFactory, true, []resolver.ResolvedArtifact{provider})
	if len(selection) != 1 || selection[0].Source != config.UserPersonal {
		t.Fatalf("user Factory selection = %+v", selection)
	}
	modes := config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}
	report, err := Run(context.Background(), home, selection, WithMaterializationModes(modes))
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("first machine sync: %v / %v", err, report)
	}
	factoryRoot := filepath.Join(home, ".factory")
	nativePath := filepath.Join(factoryRoot, "hooks.json")
	commands := nativeSupportCommands(t, nativeSupportLocationFor(t, hcc.TargetFactory), nativePath, 1)
	if len(commands) != 1 || !strings.Contains(commands[0], "check.sh") {
		t.Fatalf("machine Native hook commands = %v, want the source-backed support script", commands)
	}
	supportPlan, supportPath := nativeHookActivationSupportPath(t, selection[0], hcc.TargetFactory, true, factoryRoot)
	if len(supportPlan.artifacts) == 0 {
		t.Fatal("source-backed user Factory hook produced no support artifacts")
	}
	if _, err := os.Lstat(supportPath); err != nil {
		t.Fatalf("machine support output is missing: %v", err)
	}
	snapshotRoots := []string{factoryRoot}
	for _, support := range supportPlan.artifacts {
		outputPath := filepath.Join(factoryRoot, filepath.FromSlash(support.Path))
		info, err := os.Lstat(outputPath)
		if err != nil {
			t.Fatalf("machine support artifact %s: %v", support.Path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(outputPath)
			if err != nil {
				t.Fatalf("machine support link %s: %v", support.Path, err)
			}
			snapshotRoots = append(snapshotRoots, target)
		}
	}
	before := nativeSupportSnapshot(t, snapshotRoots...)
	store, err := ownership.Open(factoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())

	_, autoOutputs, _, _ := buildWorkItems(selection, home)
	repoKey := outputKey{target: hcc.TargetFactory, machineScope: false}
	machineKey := outputKey{target: hcc.TargetFactory, machineScope: true}
	if autoOutputs[repoKey] || !autoOutputs[machineKey] {
		t.Fatalf("automatic Native output keys = repo %v / machine %v, want repo absent and machine present", autoOutputs[repoKey], autoOutputs[machineKey])
	}
	repoSelection := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{provider})
	_, explicitOutputs, _, _ := buildWorkItems(repoSelection, home)
	if !explicitOutputs[repoKey] {
		t.Fatal("explicit repo selection was dropped for its machine-root alias")
	}

	report, err = Run(context.Background(), home, selection, WithMaterializationModes(modes), WithDryRun())
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("second machine dry run: %v / %v", err, report)
	}
	assertNoMaterializeChanges(t, report)
	assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, snapshotRoots...))
	assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
	assertPathAbsent(t, filepath.Join(factoryRoot, "hooks", "hooks.json"))
}

func TestNativeHookSupportDryRunWritesNothing(t *testing.T) {
	t.Run("initial", func(t *testing.T) {
		repo, home := additiveSafetyEnv(t)
		provider := nativeSupportSource(t, "claude", "hooks/hooks.json", nativeSupportClaudeFiles("${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"))
		resolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{provider})
		before := nativeSupportSnapshot(t, repo, home)
		report, err := Run(context.Background(), repo, resolved, WithDryRun())
		if err != nil || hasErrorDiagnostics(report.Diags) {
			t.Fatalf("initial dry run: %v %v", err, report.Diags)
		}
		assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, repo, home))
	})

	t.Run("changed command and support", func(t *testing.T) {
		repo, home := additiveSafetyEnv(t)
		files := nativeSupportClaudeFiles("${CLAUDE_PLUGIN_ROOT}/scripts/check.sh")
		provider := nativeSupportSource(t, "claude", "hooks/hooks.json", files)
		resolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{provider})
		if _, err := Run(context.Background(), repo, resolved); err != nil {
			t.Fatalf("seed installed hook: %v", err)
		}
		hookFile := filepath.Join(repo, ".factory", "hooks.json")
		beforeCommands := nativeSupportCommands(t, nativeSupportLocationFor(t, hcc.TargetFactory), hookFile, 1)
		if !strings.HasSuffix(beforeCommands[0], "/scripts/check.sh") {
			t.Fatalf("installed command = %q, want check.sh path", beforeCommands[0])
		}
		definition := filepath.Join(provider.RegistryURL, "hooks", "hooks.json")
		updatedDefinition := `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/next.sh"}]}]}`
		if err := os.WriteFile(definition, []byte(updatedDefinition), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(provider.RegistryURL, "hooks", "scripts", "check.sh"), []byte("echo changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(provider.RegistryURL, "hooks", "scripts", "next.sh"), []byte("echo next\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		updatedCatalog, err := catalog.Load(provider.RegistryURL, "claude")
		if err != nil {
			t.Fatal(err)
		}
		updated := provider
		found := false
		for _, entry := range updatedCatalog.Entries {
			if entry.Kind == ir.KindHook {
				updated.Entry = entry
				found = true
				break
			}
		}
		if !found {
			t.Fatal("changed hook missing from source catalog")
		}
		changed := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{updated})
		nextScript, err := os.ReadFile(filepath.Join(provider.RegistryURL, "hooks", "scripts", "next.sh"))
		if err != nil || string(nextScript) != "echo next\n" {
			t.Fatalf("changed support fixture = %q (%v)", nextScript, err)
		}
		changedHook, ok := updated.Resource.(*ir.Hook)
		if !ok || changedHook.Command != "${CLAUDE_PLUGIN_ROOT}/scripts/next.sh" {
			t.Fatalf("changed hook command = %v, want next.sh", updated.Resource)
		}
		before := nativeSupportSnapshot(t, repo, home)
		report, err := Run(context.Background(), repo, changed, WithDryRun())
		if err != nil || hasErrorDiagnostics(report.Diags) {
			t.Fatalf("changed dry run: %v %v", err, report.Diags)
		}
		assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, repo, home))
	})

	t.Run("deselection", func(t *testing.T) {
		repo, home := additiveSafetyEnv(t)
		filesA := map[string]string{
			"hooks/hooks.json": `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/check.sh"}]}]}`,
			"hooks/check.sh":   "echo first\n",
		}
		filesB := map[string]string{
			"hooks/hooks.json": `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/check.sh"}]}]}`,
			"hooks/check.sh":   "echo second\n",
		}
		providerA := nativeSupportSource(t, "claude", "hooks/hooks.json", filesA)
		providerB := nativeSupportSource(t, "claude", "hooks/hooks.json", filesB)
		resolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{providerA, providerB})
		if _, err := Run(context.Background(), repo, resolved); err != nil {
			t.Fatal(err)
		}
		matches, err := filepath.Glob(filepath.Join(repo, ".factory", "hook-support", "*", "*", "check.sh"))
		if err != nil || len(matches) != 2 {
			t.Fatalf("deselection fixture has %d support files: %v (%v)", len(matches), matches, err)
		}
		selectedA := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{providerA})
		before := nativeSupportSnapshot(t, repo, home)
		report, err := Run(context.Background(), repo, selectedA, WithDryRun())
		if err != nil || hasErrorDiagnostics(report.Diags) {
			t.Fatalf("deselection dry run: %v %v", err, report.Diags)
		}
		assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, repo, home))
		for _, file := range matches {
			if _, err := os.Lstat(file); err != nil {
				t.Fatalf("dry-run removed support file %s: %v", file, err)
			}
		}
	})
}
