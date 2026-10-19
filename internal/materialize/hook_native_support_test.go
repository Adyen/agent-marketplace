package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
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
					if len(elements) != 5 || elements[0] == "" || elements[1] != installedName ||
						elements[2] != "hooks" || elements[3] != "scripts" || elements[4] != "check.sh" {
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
					wantLink := machine && mode == config.MaterializationModeSymlink
					if wantLink && linkInfo.Mode()&os.ModeSymlink == 0 || !wantLink && !linkInfo.Mode().IsRegular() {
						if wantLink {
							t.Fatalf("support path mode = %v, want symlink", linkInfo.Mode())
						}
						t.Fatalf("support path mode = %v, want regular copy", linkInfo.Mode())
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

// spec: hook-resources/proven-repository-support-links-convert-with-recovery
func TestNativeRepositoryHookSupportInterruptedConversionRecovers(t *testing.T) {
	for _, stage := range []dependencytxn.FaultStage{dependencytxn.FaultAfterJournal, dependencytxn.FaultAfterPublish} {
		t.Run(string(stage), func(t *testing.T) {
			fixture, cachePath := seedNativeRepositorySupportLink(t, config.MaterializationModeSymlink)
			canonicalParent, err := filepath.EvalSymlinks(filepath.Dir(fixture.supportPath))
			if err != nil {
				t.Fatal(err)
			}
			canonicalSupportPath := filepath.Join(canonicalParent, filepath.Base(fixture.supportPath))
			nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, fixture.nativePath)
			ownerBefore, ownerMode, ownerMtime := fileSnapshot(t, fixture.statePath)
			stateDir, err := dependencytxn.StateDir()
			if err != nil {
				t.Fatal(err)
			}
			interrupted := errors.New("interrupted repository support producer")
			var faultCalled bool
			fault := func(point dependencytxn.FaultPoint) error {
				if point.Stage != stage || stage == dependencytxn.FaultAfterPublish && filepath.Clean(point.Path) != filepath.Clean(canonicalSupportPath) {
					return nil
				}
				faultCalled = true
				return interrupted
			}
			report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook},
				WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}),
				withNativeHookSupportCommitFault(fault))
			if err != nil || report == nil || !hasErrorDiagnostics(report.Diags) {
				t.Fatalf("interrupted repository support producer: %v / %v", err, report)
			}
			if !faultCalled {
				t.Fatal("dependency transaction fault hook did not reach the repository support producer")
			}
			repositoryBefore := nativeSupportSnapshot(t, fixture.repo, stateDir)
			supportInfo := nativeSupportFileInfo(t, fixture.supportPath)
			nativeInfo := nativeSupportFileInfo(t, fixture.nativePath)
			ownerInfo := nativeSupportFileInfo(t, fixture.statePath)
			cacheInfo := nativeSupportFileInfo(t, cachePath)
			cacheBefore, cacheMode, cacheMtime := fileSnapshot(t, cachePath)
			readOnly, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}), WithDryRun())
			if err != nil || readOnly == nil || !hasErrorDiagnostics(readOnly.Diags) {
				t.Fatalf("read-only pending conversion = %v / %v, want refusal", err, readOnly)
			}
			assertNativeSupportSnapshot(t, repositoryBefore, nativeSupportSnapshot(t, fixture.repo, stateDir))
			assertFileUnchanged(t, fixture.nativePath, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, fixture.statePath, ownerBefore, ownerMode, ownerMtime)
			assertFileUnchanged(t, cachePath, cacheBefore, cacheMode, cacheMtime)
			assertNativeSupportSameFile(t, fixture.supportPath, supportInfo)
			assertNativeSupportSameFile(t, fixture.nativePath, nativeInfo)
			assertNativeSupportSameFile(t, fixture.statePath, ownerInfo)
			assertNativeSupportSameFile(t, cachePath, cacheInfo)
			report, err = Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}))
			if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("mutating recovery: %v / %v", err, report)
			}
			if err := dependencytxn.CheckPending(stateDir); err != nil {
				t.Fatalf("recovered conversion still pending: %v", err)
			}
			data, mode, _ := fileSnapshot(t, fixture.supportPath)
			info, err := os.Lstat(fixture.supportPath)
			if err != nil || !info.Mode().IsRegular() || !bytes.Equal(data, fixture.supportPlan.artifacts[0].Bytes) || mode.Perm() != portableArtifactMode(fixture.supportPlan.artifacts[0].Mode) {
				t.Fatalf("recovered support copy = %q / %v / %v", data, info, err)
			}
			assertFileUnchanged(t, cachePath, cacheBefore, cacheMode, cacheMtime)
			assertFileUnchanged(t, fixture.nativePath, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, fixture.statePath, ownerBefore, ownerMode, ownerMtime)
			assertNativeSupportSameFile(t, fixture.nativePath, nativeInfo)
			assertNativeSupportSameFile(t, fixture.statePath, ownerInfo)
			assertNativeSupportSameFile(t, cachePath, cacheInfo)
			manifest, err := loadPortableState(fixture.repo, false)
			if err != nil {
				t.Fatal(err)
			}
			rel, err := filepath.Rel(fixture.factoryRoot, fixture.supportPath)
			if err != nil {
				t.Fatal(err)
			}
			entry, ok := manifest.entry(hcc.TargetFactory, filepath.ToSlash(rel))
			if !ok || entry.Digest != portableArtifactDigest(fixture.supportPlan.artifacts[0]) {
				t.Fatalf("recovered repository support manifest entry = %+v, found=%v", entry, ok)
			}
			if info, err := os.Stat(fixture.manifestPath); err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("recovered new manifest mode = %v / %v, want 0600", info, err)
			}
			beforeRepeat := nativeSupportStableSnapshot(t, fixture.repo, filepath.Dir(fixture.statePath), cache.CacheDir(), stateDir)
			repeat, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}))
			if err != nil || repeat == nil || hasErrorDiagnostics(repeat.Diags) || reportHasHookSupportWrite(repeat) {
				t.Fatalf("repeat after producer recovery = %v / %v", err, repeat)
			}
			if err := dependencytxn.CheckPending(stateDir); err != nil {
				t.Fatalf("repeat sync retained dependency journal: %v", err)
			}
			assertNativeSupportSnapshot(t, beforeRepeat, nativeSupportStableSnapshot(t, fixture.repo, filepath.Dir(fixture.statePath), cache.CacheDir(), stateDir))
		})
	}
}

// spec: hook-resources/repository-support-copies-preserve-machine-and-non-hook-policy
func TestNativeRepositoryHookSupportUsesPortableCopies(t *testing.T) {
	t.Run("repository copies and non-hook symlink", func(t *testing.T) {
		repo, home := additiveSafetyEnv(t)
		filesA := map[string]string{
			"hooks/hooks.json":       `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"}]}]}`,
			"hooks/scripts/check.sh": "first support script\n",
		}
		filesB := map[string]string{
			"hooks/hooks.json":       `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"}]}]}`,
			"hooks/scripts/check.sh": "second support script\n",
		}
		providerA := nativeSupportSource(t, "claude", "hooks/hooks.json", filesA)
		providerB := nativeSupportSource(t, "claude", "hooks/hooks.json", filesB)
		if err := os.Chmod(filepath.Join(providerA.RegistryURL, "hooks", "scripts", "check.sh"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(providerB.RegistryURL, "hooks", "scripts", "check.sh"), 0o640); err != nil {
			t.Fatal(err)
		}
		resolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{providerA, providerB})
		skillDir := t.TempDir()
		skillDefinition := filepath.Join(skillDir, "SKILL.md")
		additiveSafetyWrite(t, skillDefinition, "---\nname: portable-control\ndescription: control\n---\n\nbody\n")
		additiveSafetyWrite(t, filepath.Join(skillDir, "helper.sh"), "skill helper\n")
		skill := ir.NewSkill("portable-control", "control", "body", nil, nil, ir.SourceRef{File: skillDefinition, Line: 1})
		resolved = append(resolved, resolver.ResolvedArtifact{
			Entry:     catalog.Entry{Name: "portable-control", Path: skillDir, Resource: skill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		})
		sourcesBefore := nativeSupportSnapshot(t, providerA.RegistryURL, providerB.RegistryURL)
		modes := config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}
		report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes))
		if err != nil {
			t.Fatalf("repository symlink-mode Run: %v", err)
		}
		if hasErrorDiagnostics(report.Diags) {
			t.Fatalf("repository hook support diagnostics: %v", report.Diags)
		}

		loc := nativeSupportLocationFor(t, hcc.TargetFactory)
		hookFile := filepath.Join(repo, filepath.FromSlash(loc.repoFile))
		commands := nativeSupportCommands(t, loc, hookFile, 2)
		wantModes := map[string]os.FileMode{
			"first support script\n":  0o755,
			"second support script\n": 0o640,
		}
		seen := make(map[string]bool, len(wantModes))
		for _, command := range commands {
			installed, _ := loc.installedRef(t, command, repo, home, false)
			info, err := os.Lstat(installed)
			if err != nil {
				t.Fatalf("Lstat repository support %s: %v", installed, err)
			}
			data, err := os.ReadFile(installed)
			if err != nil {
				t.Fatalf("read repository support %s: %v", installed, err)
			}
			wantMode, ok := wantModes[string(data)]
			if !ok {
				t.Errorf("unexpected support bytes at %s: %q", installed, data)
				continue
			}
			seen[string(data)] = true
			if !info.Mode().IsRegular() {
				t.Errorf("repository support path %s mode = %v, want regular copy", installed, info.Mode())
			} else if info.Mode().Perm() != wantMode {
				t.Errorf("repository support path %s mode = %o, want %o", installed, info.Mode().Perm(), wantMode)
			}
			resolvedInfo, err := os.Stat(installed)
			if err != nil || resolvedInfo.Mode().Perm() != wantMode {
				t.Errorf("repository support target mode = %v, %v, want %o", resolvedInfo, err, wantMode)
			}
		}
		if len(seen) != len(wantModes) {
			t.Errorf("installed support contents = %v, want both registry scripts", seen)
		}
		skillOutput := filepath.Join(repo, ".factory", "skills", "portable-control")
		if info, err := os.Lstat(skillOutput); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("non-hook skill output mode = %v, %v, want symlink", info, err)
		}
		supportWork := []skillTarget{{skill: resolved[0], target: hcc.TargetFactory}, {skill: resolved[1], target: hcc.TargetFactory}}
		supportPlans, conflicts := prepareHookSupport(supportWork)
		if len(conflicts) != 0 || supportPlans[0].err != nil || supportPlans[1].err != nil {
			t.Fatalf("captured support plans = %+v / %v", supportPlans, conflicts)
		}
		var supportArtifacts []artifact.Artifact
		for i := range supportWork {
			supportArtifacts = append(supportArtifacts, supportPlans[i].artifacts...)
		}
		for _, plan := range materializePlans(hcc.TargetFactory, filepath.Join(repo, ".factory"), supportArtifacts, nil, false) {
			cacheDir := cachedPlanDirectoryForOutput(outputKey{target: hcc.TargetFactory}, filepath.Join(repo, ".factory"), plan, nil)
			for _, a := range plan.artifacts {
				cachePath := filepath.Join(cacheDir, filepath.FromSlash(a.Path))
				if _, err := os.Lstat(cachePath); !os.IsNotExist(err) {
					t.Fatalf("repository hook support cache entry %s exists after copy mode: %v", cachePath, err)
				}
			}
		}
		assertNativeSupportSnapshot(t, sourcesBefore, nativeSupportSnapshot(t, providerA.RegistryURL, providerB.RegistryURL))
	})

	t.Run("machine hook support remains symlinked", func(t *testing.T) {
		repo, home := additiveSafetyEnv(t)
		provider := nativeSupportSource(t, "claude", "hooks/hooks.json", nativeSupportClaudeFiles("${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"))
		if err := os.Chmod(filepath.Join(provider.RegistryURL, "hooks", "scripts", "check.sh"), 0o755); err != nil {
			t.Fatal(err)
		}
		sourcesBefore := nativeSupportSnapshot(t, provider.RegistryURL)
		resolved := nativeSupportResolve(t, hcc.TargetFactory, true, []resolver.ResolvedArtifact{provider})
		modes := config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}
		report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes))
		if err != nil {
			t.Fatalf("machine symlink-mode Run: %v", err)
		}
		if hasErrorDiagnostics(report.Diags) {
			t.Fatalf("machine hook support diagnostics: %v", report.Diags)
		}
		loc := nativeSupportLocationFor(t, hcc.TargetFactory)
		hookFile := filepath.Join(home, filepath.FromSlash(loc.userFile))
		commands := nativeSupportCommands(t, loc, hookFile, 1)
		installed, _ := loc.installedRef(t, commands[0], repo, home, true)
		info, err := os.Lstat(installed)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("machine hook support mode = %v, %v, want symlink", info, err)
		}
		if got, err := os.ReadFile(installed); err != nil || string(got) != nativeSupportScriptBody {
			t.Errorf("machine hook support bytes = %q, %v", got, err)
		}
		if targetInfo, err := os.Stat(installed); err != nil || targetInfo.Mode().Perm() != 0o755 {
			t.Errorf("machine hook support target mode = %v, %v, want 755", targetInfo, err)
		}
		assertNativeSupportSnapshot(t, sourcesBefore, nativeSupportSnapshot(t, provider.RegistryURL))
	})
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
				matches, err := filepath.Glob(filepath.Join(supportRoot, "*", "*", "hooks", "check.sh"))
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
					wantLink := machine && mode == config.MaterializationModeSymlink
					if wantLink && linkInfo.Mode()&os.ModeSymlink == 0 || !wantLink && !linkInfo.Mode().IsRegular() {
						if wantLink {
							t.Fatalf("support path %s mode = %v, want symlink", installed, linkInfo.Mode())
						}
						t.Fatalf("support path %s mode = %v, want regular copy", installed, linkInfo.Mode())
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

				sentinel := filepath.Join(supportRoot, "handwritten.json")
				additiveSafetyWrite(t, sentinel, `{"_managed_by":"agent-marketplace","sentinel":"keep me"}`+"\n")
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
			matches, err := filepath.Glob(filepath.Join(supportRoot, "*", "guard", "hooks", "check.sh"))
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
			wantFiles:   []string{".devin/run.sh", ".devin/payload.json", ".devin/nested/tool.sh", ".devin/nested/hooks.v1.json", ".devin/nested/config.json"},
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
			wantFiles:   []string{".codex/run.sh", ".codex/payload.json", ".codex/nested/tool.sh", ".codex/nested/hooks.json", ".codex/nested/config.toml"},
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
			wantFiles:   []string{".factory/run.sh", ".factory/payload.json", ".factory/nested/tool.sh", ".factory/nested/hooks.json", ".factory/nested/mcp.json"},
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
			wantFiles:   []string{"hooks/mcp.json", "hooks/run.sh", "hooks/payload.json", "hooks/nested/tool.sh", "hooks/nested/hooks.json"},
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

			matches, err := filepath.Glob(filepath.Join(repo, filepath.FromSlash(loc.repoSupportRoot), "*", "pinned-check", ".factory", "scripts", "check.sh"))
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
	matches, err := filepath.Glob(filepath.Join(newRoot, "*", "*", "hooks", "scripts", "check.sh"))
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
		installed, err := filepath.Glob(filepath.Join(repo, filepath.FromSlash(loc.repoSupportRoot), "*", "*", "hooks", "scripts", "check.sh"))
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

func seedNativeRepositorySupportLink(t *testing.T, mode config.MaterializationMode) (nativeHookSupportPublicationFixture, string) {
	t.Helper()
	fixture := newNativeHookSupportPublicationFixture(t, mode)
	report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}))
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed source-owned native hook: %v / %v", err, report)
	}
	store, err := ownership.Open(fixture.factoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof("hooks.json")
	if err != nil || proof.OwnedCount() != 1 || len(proof.OwnedIDs()) != 1 {
		t.Fatalf("seed native source proof = %v / %+v", err, proof.OwnedIDs())
	}
	entry, ok := proof.Entry(proof.OwnedIDs()[0])
	if !ok || entry.Resource != fixture.hook.Name || entry.SourceDigest != fixture.supportPlan.sourceDigest {
		t.Fatalf("seed native source proof entry = %+v", entry)
	}
	manifestState, err := loadPortableState(fixture.repo, false)
	if err != nil {
		t.Fatal(err)
	}
	manifestRel, err := filepath.Rel(fixture.factoryRoot, fixture.supportPath)
	if err != nil {
		t.Fatal(err)
	}
	manifestKey := filepath.ToSlash(manifestRel)
	manifestState.remove(hcc.TargetFactory, manifestKey)
	if _, ok := manifestState.entry(hcc.TargetFactory, manifestKey); ok {
		t.Fatalf("repository symlink fixture still has portable copy ownership for %q", manifestKey)
	}
	if err := manifestState.save(false); err != nil {
		t.Fatal(err)
	}
	plans := materializePlans(hcc.TargetFactory, fixture.factoryRoot, fixture.supportPlan.artifacts, nil, false)
	var supportPlan *materializePlan
	var supportArtifact artifact.Artifact
	for i := range plans {
		for _, candidate := range plans[i].artifacts {
			if filepath.ToSlash(path.Join(plans[i].reportPrefix, candidate.Path)) == fixture.supportPlan.artifacts[0].Path {
				supportPlan = &plans[i]
				supportArtifact = candidate
				break
			}
		}
	}
	if supportPlan == nil {
		t.Fatal("captured support artifact has no Factory output plan")
	}
	cacheDir := cachedPlanDirectoryForOutput(outputKey{target: hcc.TargetFactory}, fixture.factoryRoot, *supportPlan, nil)
	if _, err := materializeArtifactLinkCache(cacheDir, supportPlan.artifacts, false, false); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(cacheDir, filepath.FromSlash(supportArtifact.Path))
	outputPath := filepath.Join(supportPlan.outDir, filepath.FromSlash(supportArtifact.Path))
	if filepath.Clean(outputPath) != filepath.Clean(fixture.supportPath) {
		t.Fatalf("Factory support output = %q, want %q", outputPath, fixture.supportPath)
	}
	cacheBytes, cacheMode, _ := fileSnapshot(t, cachePath)
	if !bytes.Equal(cacheBytes, fixture.supportPlan.artifacts[0].Bytes) || cacheMode.Perm() != portableArtifactMode(fixture.supportPlan.artifacts[0].Mode) {
		t.Fatalf("generated cache file differs from captured support: mode=%o", cacheMode.Perm())
	}
	if err := os.Remove(fixture.supportPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cachePath, fixture.supportPath); err != nil {
		t.Fatal(err)
	}
	return fixture, cachePath
}

func driftNativeRepositorySupportOwnershipValue(t *testing.T, fixture nativeHookSupportPublicationFixture) {
	t.Helper()
	nativeBytes, err := os.ReadFile(fixture.nativePath)
	if err != nil {
		t.Fatal(err)
	}
	drifted := bytes.Replace(nativeBytes, []byte("/scripts/check.sh"), []byte("/scripts/drift.sh"), 1)
	if bytes.Equal(nativeBytes, drifted) {
		t.Fatal("Factory Native command did not contain the captured support path")
	}
	info := nativeSupportFileInfo(t, fixture.nativePath)
	if err := os.WriteFile(fixture.nativePath, drifted, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture.nativePath, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
}

func replaceNativeRepositorySupportProofWithSatisfaction(t *testing.T, fixture nativeHookSupportPublicationFixture) {
	t.Helper()
	nativeBytes, err := os.ReadFile(fixture.nativePath)
	if err != nil {
		t.Fatal(err)
	}
	commands := nativeSupportCommands(t, nativeSupportLocationFor(t, hcc.TargetFactory), fixture.nativePath, 1)
	layoutCase := nativeHookJSONCaseByName(t, "factory_repo")
	rule := nativeHookMetadataRule(t, layoutCase, commands[0])
	satisfaction := nativeHookSatisfaction(t, layoutCase.event, rule, []ownership.HookSourceEvidence{{
		Resource: fixture.hook.Name, SourceDigest: fixture.supportPlan.sourceDigest,
	}})
	layout, ok := nativeHookLayoutFor(hcc.TargetFactory, false)
	if !ok {
		t.Fatal("Factory repository hook layout is missing")
	}
	writeNativeSatisfactionState(t, fixture.factoryRoot, layout, nativeBytes, []ownership.Entry{}, []ownership.HookSatisfaction{satisfaction})
}

func replaceNativeRepositorySupportProofWithForeignSource(t *testing.T, fixture nativeHookSupportPublicationFixture) {
	t.Helper()
	store, err := ownership.Open(fixture.factoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	fileState := record.Files["hooks.json"]
	if len(fileState.Entries) != 1 {
		t.Fatalf("Factory proof entries = %d, want one", len(fileState.Entries))
	}
	entry := fileState.Entries[0]
	entry.Resource = "foreign-source"
	entry.SourceDigest = "sha256:" + strings.Repeat("f", 64)
	entry.SourceEvidence = nil
	nativeBytes, err := os.ReadFile(fixture.nativePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(ownership.Mutation{
		Rel: "hooks.json", AbsFile: fixture.nativePath, NextBytes: nativeBytes,
		Entries: []ownership.Entry{entry}, Reason: ownership.ReasonSync,
		ExpectPriorGeneration: record.Generation, ExpectPriorDigest: ownership.Digest(nativeBytes),
	}); err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof("hooks.json")
	if err != nil || proof.OwnedCount() != 1 {
		t.Fatalf("foreign-source proof = %v / %+v", err, proof.OwnedIDs())
	}
	id := proof.OwnedIDs()[0]
	if !proof.HasHookSource(id, "foreign-source", entry.SourceDigest) || proof.HasHookSource(id, fixture.hook.Name, fixture.supportPlan.sourceDigest) ||
		proof.Classify(id, entry.ValueDigest) != ownership.StatusOwnedClean {
		t.Fatal("fixture did not retain an owned but unrelated clean source proof")
	}
}

func snapshotNativeRepositorySupportLink(t *testing.T, fixture nativeHookSupportPublicationFixture, extraRoots ...string) map[string]nativeSupportState {
	t.Helper()
	roots := []string{fixture.repo, filepath.Dir(fixture.statePath), cache.CacheDir()}
	roots = append(roots, extraRoots...)
	if target, err := os.Readlink(fixture.supportPath); err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(fixture.supportPath), target)
		}
		roots = append(roots, target)
	} else if !os.IsNotExist(err) && !errors.Is(err, syscall.EINVAL) {
		t.Fatal(err)
	}
	return nativeSupportSnapshot(t, roots...)
}

func nativeSupportFileInfo(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func assertNativeSupportSameFile(t *testing.T, path string, before os.FileInfo) {
	t.Helper()
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("filesystem identity changed for %s: before=%v after=%v err=%v", path, before, after, err)
	}
}

func optionalNativeSupportFileSnapshot(t *testing.T, path string) ([]byte, os.FileMode, time.Time, bool) {
	t.Helper()
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil, 0, time.Time{}, false
	} else if err != nil {
		t.Fatal(err)
	}
	data, mode, mtime := fileSnapshot(t, path)
	return data, mode, mtime, true
}

func assertNativeRepositorySupportLinkSnapshot(t *testing.T, fixture nativeHookSupportPublicationFixture, before map[string]nativeSupportState, extraRoots ...string) {
	t.Helper()
	roots := []string{fixture.repo, filepath.Dir(fixture.statePath), cache.CacheDir()}
	roots = append(roots, extraRoots...)
	if link, ok := before[filepath.Clean(fixture.supportPath)]; ok && link.linkTarget != "" {
		target := link.linkTarget
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(fixture.supportPath), target)
		}
		roots = append(roots, filepath.Clean(target))
	}
	assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, roots...))
}

func requireNativeHookSupportConflict(t *testing.T, report *Report) {
	t.Helper()
	for _, diagnostic := range report.Diags {
		if diagnostic.Code == diag.CodeMaterializeHookSupportConflict && diagnostic.Severity == diag.SeverityError && diagnostic.Target == string(hcc.TargetFactory) {
			return
		}
	}
	t.Fatalf("missing Factory %s refusal: %+v", diag.CodeMaterializeHookSupportConflict, report.Diags)
}

func reportHasHookSupportWrite(report *Report) bool {
	if report == nil {
		return false
	}
	for _, targetReport := range report.PerTarget {
		for _, path := range targetReport.Written {
			if strings.Contains(filepath.ToSlash(path), "hook-support/") {
				return true
			}
		}
	}
	for _, targetReport := range report.PerRoot {
		for _, path := range targetReport.Written {
			if strings.Contains(filepath.ToSlash(path), "hook-support/") {
				return true
			}
		}
	}
	return false
}

// spec: hook-resources/proven-repository-support-links-convert-with-recovery
func TestNativeRepositoryHookSupportProvenLinkConvertsToPortableCopy(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode), func(t *testing.T) {
			fixture, cachePath := seedNativeRepositorySupportLink(t, mode)
			beforeLink := snapshotNativeHookLegacyCacheLink(t, fixture.supportPath)
			if beforeLink.target != cachePath || !bytes.Equal(beforeLink.bytes, fixture.supportPlan.artifacts[0].Bytes) {
				t.Fatalf("seed cache link = %q, want authorized target %q", beforeLink.target, cachePath)
			}
			nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, fixture.nativePath)
			stateBefore, stateMode, stateMtime := fileSnapshot(t, fixture.statePath)
			cacheBefore, cacheMode, cacheMtime := fileSnapshot(t, cachePath)
			_, manifestMode, _, manifestExists := optionalNativeSupportFileSnapshot(t, fixture.manifestPath)
			report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}))
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("proven support conversion: %v / %v", err, report)
			}
			data, installedMode, installedMtime := fileSnapshot(t, fixture.supportPath)
			info, err := os.Lstat(fixture.supportPath)
			if err != nil || !info.Mode().IsRegular() || !bytes.Equal(data, fixture.supportPlan.artifacts[0].Bytes) || installedMode.Perm() != portableArtifactMode(fixture.supportPlan.artifacts[0].Mode) {
				t.Fatalf("converted repository support = %q / %v / %v", data, info, err)
			}
			assertFileUnchanged(t, fixture.nativePath, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, fixture.statePath, stateBefore, stateMode, stateMtime)
			assertFileUnchanged(t, cachePath, cacheBefore, cacheMode, cacheMtime)
			assertPathAbsent(t, fixture.legacyPath)
			assertPathAbsent(t, fixture.oldSupportPath)
			manifest, err := loadPortableState(fixture.repo, false)
			if err != nil {
				t.Fatal(err)
			}
			rel, err := filepath.Rel(fixture.factoryRoot, fixture.supportPath)
			if err != nil {
				t.Fatal(err)
			}
			entry, ok := manifest.entry(hcc.TargetFactory, filepath.ToSlash(rel))
			if !ok || entry.Digest != portableArtifactDigest(fixture.supportPlan.artifacts[0]) {
				t.Fatalf("portable copy manifest entry = %+v, found=%v", entry, ok)
			}
			manifestAfter, manifestModeAfter, manifestMtimeAfter := fileSnapshot(t, fixture.manifestPath)
			if !manifestExists && manifestModeAfter.Perm() != 0o600 {
				t.Fatalf("new portable manifest mode = %o, want 0600", manifestModeAfter.Perm())
			}
			if manifestExists && manifestModeAfter.Perm() != manifestMode.Perm() {
				t.Fatalf("portable manifest mode changed: before=%o after=%o", manifestMode.Perm(), manifestModeAfter.Perm())
			}
			if _, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})); err != nil {
				t.Fatalf("repeat converted support sync: %v", err)
			}
			assertFileUnchanged(t, fixture.supportPath, data, installedMode, installedMtime)
			assertFileUnchanged(t, fixture.nativePath, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, fixture.statePath, stateBefore, stateMode, stateMtime)
			assertFileUnchanged(t, cachePath, cacheBefore, cacheMode, cacheMtime)
			assertFileUnchanged(t, fixture.manifestPath, manifestAfter, manifestModeAfter, manifestMtimeAfter)
		})
	}
}

// spec: hook-resources/repository-support-conversion-check-writes-nothing
func TestNativeRepositoryHookSupportConversionCheckWritesNothing(t *testing.T) {
	fixture, cachePath := seedNativeRepositorySupportLink(t, config.MaterializationModeSymlink)
	before := snapshotNativeRepositorySupportLink(t, fixture)
	supportInfo := nativeSupportFileInfo(t, fixture.supportPath)
	nativeInfo := nativeSupportFileInfo(t, fixture.nativePath)
	ownerInfo := nativeSupportFileInfo(t, fixture.statePath)
	cacheInfo := nativeSupportFileInfo(t, cachePath)
	report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}), WithDryRun())
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("conversion dry run: %v / %v", err, report)
	}
	if !reportHasHookSupportWrite(report) {
		t.Fatalf("conversion dry run did not report a pending support copy: %+v", report.PerTarget)
	}
	assertNativeRepositorySupportLinkSnapshot(t, fixture, before)
	assertNativeSupportSameFile(t, fixture.supportPath, supportInfo)
	assertNativeSupportSameFile(t, fixture.nativePath, nativeInfo)
	assertNativeSupportSameFile(t, fixture.statePath, ownerInfo)
	assertNativeSupportSameFile(t, cachePath, cacheInfo)
}

// spec: hook-resources/unproven-or-unsafe-repository-support-links-are-preserved
func TestNativeRepositoryHookSupportRefusesUnprovenLinks(t *testing.T) {
	for _, tc := range []struct {
		name              string
		mutate            func(*testing.T, nativeHookSupportPublicationFixture, string)
		wantCode          bool
		snapshotProofFile bool
	}{
		{
			name: "missing ownership proof",
			mutate: func(t *testing.T, fixture nativeHookSupportPublicationFixture, _ string) {
				if err := os.Remove(fixture.statePath); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "drifted ownership value",
			mutate: func(t *testing.T, fixture nativeHookSupportPublicationFixture, _ string) {
				driftNativeRepositorySupportOwnershipValue(t, fixture)
			},
			wantCode:          true,
			snapshotProofFile: true,
		},
		{
			name: "satisfaction-only evidence",
			mutate: func(t *testing.T, fixture nativeHookSupportPublicationFixture, _ string) {
				replaceNativeRepositorySupportProofWithSatisfaction(t, fixture)
			},
			wantCode: true,
		},
		{
			name: "corrupt ownership proof",
			mutate: func(t *testing.T, fixture nativeHookSupportPublicationFixture, _ string) {
				if err := os.WriteFile(fixture.statePath, []byte("{corrupt ownership state"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "different cache target",
			mutate: func(t *testing.T, fixture nativeHookSupportPublicationFixture, _ string) {
				foreign := filepath.Join(t.TempDir(), "foreign.sh")
				if err := os.WriteFile(foreign, fixture.supportPlan.artifacts[0].Bytes, portableArtifactMode(fixture.supportPlan.artifacts[0].Mode)); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(fixture.supportPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreign, fixture.supportPath); err != nil {
					t.Fatal(err)
				}
			},
			wantCode: true,
		},
		{
			name: "changed cache referent",
			mutate: func(t *testing.T, _ nativeHookSupportPublicationFixture, cachePath string) {
				if err := os.WriteFile(cachePath, []byte("foreign cache bytes\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantCode: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, cachePath := seedNativeRepositorySupportLink(t, config.MaterializationModeSymlink)
			tc.mutate(t, fixture, cachePath)
			var proofBytes []byte
			var proofMode os.FileMode
			var proofMtime time.Time
			var proofInfo os.FileInfo
			if tc.snapshotProofFile {
				proofBytes, proofMode, proofMtime = fileSnapshot(t, fixture.statePath)
				proofInfo = nativeSupportFileInfo(t, fixture.statePath)
			}
			before := snapshotNativeRepositorySupportLink(t, fixture)
			report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}))
			if err != nil || report == nil || !hasErrorDiagnostics(report.Diags) {
				t.Fatalf("unproven support conversion: %v / %v", err, report)
			}
			if tc.wantCode {
				requireNativeHookSupportConflict(t, report)
			}
			blocked := false
			for _, resource := range report.BlockedResources {
				blocked = blocked || resource.Target == hcc.TargetFactory && resource.Name == fixture.hook.Name
			}
			if !blocked {
				t.Fatalf("unproven support hook was not blocked: %+v", report.BlockedResources)
			}
			if tc.snapshotProofFile {
				assertFileUnchanged(t, fixture.statePath, proofBytes, proofMode, proofMtime)
				assertNativeSupportSameFile(t, fixture.statePath, proofInfo)
			}
			assertNativeRepositorySupportLinkSnapshot(t, fixture, before)
		})
	}
}

func seedNativeHookSupportForeignManifest(t *testing.T, fixture nativeHookSupportPublicationFixture) (string, portableManifestEntry, []byte, os.FileMode, time.Time) {
	t.Helper()
	const foreignPath = "skills/foreign-control/SKILL.md"
	path := filepath.Join(fixture.repo, ".claude", filepath.FromSlash(foreignPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("foreign handwritten output\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := loadPortableState(fixture.repo, false)
	if err != nil {
		t.Fatal(err)
	}
	state.add(hcc.TargetClaude, foreignPath, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := state.save(false); err != nil {
		t.Fatal(err)
	}
	persisted, err := loadPortableState(fixture.repo, false)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := persisted.entry(hcc.TargetClaude, foreignPath)
	if !ok {
		t.Fatalf("foreign manifest entry was not persisted: %s", persisted.raw)
	}
	data, mode, mtime := fileSnapshot(t, path)
	return path, entry, data, mode, mtime
}

func replaceNativeManifestParentWithIdenticalClone(t *testing.T, fixture nativeHookSupportPublicationFixture) string {
	t.Helper()
	manifestBytes, manifestMode, _ := fileSnapshot(t, fixture.manifestPath)
	parent := filepath.Dir(fixture.manifestPath)
	parentInfo := nativeSupportFileInfo(t, parent)
	retained := filepath.Join(fixture.repo, "retained-agents")
	if err := os.Rename(parent, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, parentInfo.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, parentInfo.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.manifestPath, manifestBytes, manifestMode.Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture.manifestPath, manifestMode.Perm()); err != nil {
		t.Fatal(err)
	}
	return retained
}

// spec: hook-resources/unproven-or-unsafe-repository-support-links-are-preserved
func TestNativeRepositoryHookSupportManifestGenerationSwapRefusesCopy(t *testing.T) {
	fixture, _ := seedNativeRepositorySupportLink(t, config.MaterializationModeFile)
	seedNativeHookSupportForeignManifest(t, fixture)
	var callbackCalled bool
	var externalState map[string]nativeSupportState
	publicationHook := func(path string) {
		if filepath.Clean(path) != filepath.Clean(fixture.manifestPath) || callbackCalled {
			return
		}
		callbackCalled = true
		if info, err := os.Lstat(fixture.supportPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("manifest publication hook ran after Native support mutation: %v / %v", info, err)
		}
		state, err := loadPortableState(fixture.repo, false)
		if err != nil {
			t.Fatal(err)
		}
		state.add(hcc.TargetClaude, "skills/external-sentinel/SKILL.md", portableArtifactDigest(artifact.Artifact{Bytes: []byte("foreign manifest entry\n"), Mode: 0o600}))
		if err := state.save(false); err != nil {
			t.Fatal(err)
		}
		externalState = nativeSupportSnapshot(t, fixture.repo, filepath.Dir(fixture.statePath), cache.CacheDir())
	}
	report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}), func(cfg *runConfig) {
		cfg.manifestPublicationHook = publicationHook
	})
	if err != nil || report == nil || !hasErrorDiagnostics(report.Diags) {
		t.Fatalf("manifest-generation conversion refusal: %v / %v", err, report)
	}
	if !callbackCalled || !diag.HasCode(report.Diags, diag.CodeMaterializeHookSupportConflict) {
		t.Fatalf("manifest generation swap was not refused: called=%v diagnostics=%+v", callbackCalled, report.Diags)
	}
	assertNativeSupportSnapshot(t, externalState, nativeSupportSnapshot(t, fixture.repo, filepath.Dir(fixture.statePath), cache.CacheDir()))
}

func TestNativeRepositoryHookSupportManifestPinRefusesNamespaceRaces(t *testing.T) {
	for _, tc := range []struct {
		name           string
		manifestAbsent bool
		withForeign    bool
		mutate         func(*testing.T, nativeHookSupportPublicationFixture)
	}{
		{
			name:        "captured-parent-replaced-by-identical-clone",
			withForeign: true,
			mutate: func(t *testing.T, fixture nativeHookSupportPublicationFixture) {
				replaceNativeManifestParentWithIdenticalClone(t, fixture)
			},
		},
		{
			name:           "manifest-appears-at-captured-absent-leaf",
			manifestAbsent: true,
			mutate: func(t *testing.T, fixture nativeHookSupportPublicationFixture) {
				foreign := []byte("version: 1\nentries: []\n")
				if err := os.WriteFile(fixture.manifestPath, foreign, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(fixture.manifestPath, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, _ := seedNativeRepositorySupportLink(t, config.MaterializationModeFile)
			if tc.withForeign {
				seedNativeHookSupportForeignManifest(t, fixture)
			}
			if tc.manifestAbsent {
				parent := filepath.Dir(fixture.manifestPath)
				if err := os.MkdirAll(parent, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(fixture.manifestPath); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if _, err := os.Lstat(fixture.manifestPath); !os.IsNotExist(err) {
					t.Fatalf("manifest leaf is not absent at capture: %v", err)
				}
			}
			var callbackCalled bool
			var externalState map[string]nativeSupportState
			publicationHook := func(path string) {
				if filepath.Clean(path) != filepath.Clean(fixture.manifestPath) || callbackCalled {
					return
				}
				callbackCalled = true
				if info, err := os.Lstat(fixture.supportPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("manifest hook ran after Native support mutation: %v / %v", info, err)
				}
				tc.mutate(t, fixture)
				externalState = snapshotNativeRepositorySupportLink(t, fixture)
			}
			report, err := Run(t.Context(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}), func(cfg *runConfig) {
				cfg.manifestPublicationHook = publicationHook
			})
			if !callbackCalled {
				t.Fatalf("Native manifest publication hook did not run: err=%v report=%+v", err, report)
			}
			if err != nil || report == nil || !diag.HasCode(report.Diags, diag.CodeMaterializeHookSupportConflict) {
				t.Fatalf("captured manifest namespace replacement was accepted: err=%v report=%+v", err, report)
			}
			assertNativeSupportSnapshot(t, externalState, snapshotNativeRepositorySupportLink(t, fixture))
		})
	}
}

// spec: materialization/portable-manifest-staging-keeps-captured-namespace-authority
func TestNativeRepositoryHookSupportTransactionPinsManifestNamespace(t *testing.T) {
	for _, stage := range []dependencytxn.FaultStage{dependencytxn.FaultAfterJournal, dependencytxn.FaultBeforePublish} {
		t.Run(string(stage), func(t *testing.T) {
			fixture := newNativeHookSupportPublicationFixture(t, config.MaterializationModeFile)
			assertPathAbsent(t, fixture.supportPath)
			manifestBefore, manifestMode, manifestMtime := fileSnapshot(t, fixture.manifestPath)
			manifestSnapshot, err := readPortableManifestSnapshot(fixture.manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			assertPathAbsent(t, fixture.nativePath)
			assertPathAbsent(t, fixture.statePath)
			legacyBefore, legacyMode, legacyMtime := fileSnapshot(t, fixture.legacyPath)
			oldSupportBefore, oldSupportMode, oldSupportMtime := fileSnapshot(t, fixture.oldSupportPath)
			sourceDefinition := filepath.Join(fixture.hook.RegistryURL, "hooks", "hooks.json")
			sourceSupport := filepath.Join(fixture.hook.RegistryURL, "hooks", "scripts", "check.sh")
			sourceDefinitionBefore, sourceDefinitionMode, sourceDefinitionMtime := fileSnapshot(t, sourceDefinition)
			sourceSupportBefore, sourceSupportMode, sourceSupportMtime := fileSnapshot(t, sourceSupport)
			var faultReached bool
			var retained string
			var cloneBefore []byte
			var cloneMode os.FileMode
			var cloneMtime time.Time
			fault := dependencytxn.FaultHook(func(point dependencytxn.FaultPoint) error {
				if point.Stage == stage && !faultReached {
					faultReached = true
					retained = replaceNativeManifestParentWithIdenticalClone(t, fixture)
					cloneBefore, cloneMode, cloneMtime = fileSnapshot(t, fixture.manifestPath)
				}
				return nil
			})
			report, runErr := Run(t.Context(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook},
				WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}),
				func(cfg *runConfig) { cfg.supportCommitFault = fault })
			if !faultReached {
				t.Fatalf("support producer did not reach %s: err=%v report=%+v", stage, runErr, report)
			}
			if runErr != nil || report == nil || !diag.HasCode(report.Diags, diag.CodeMaterializeHookSupportConflict) {
				t.Fatalf("producer accepted cloned manifest namespace at %s: err=%v report=%+v", stage, runErr, report)
			}
			assertPathAbsent(t, fixture.supportPath)
			assertPathAbsent(t, fixture.nativePath)
			assertPathAbsent(t, fixture.statePath)
			assertFileUnchanged(t, fixture.legacyPath, legacyBefore, legacyMode, legacyMtime)
			assertFileUnchanged(t, fixture.oldSupportPath, oldSupportBefore, oldSupportMode, oldSupportMtime)
			assertFileUnchanged(t, filepath.Join(retained, filepath.Base(fixture.manifestPath)), manifestBefore, manifestMode, manifestMtime)
			assertFileUnchanged(t, fixture.manifestPath, cloneBefore, cloneMode, cloneMtime)
			assertFileUnchanged(t, sourceDefinition, sourceDefinitionBefore, sourceDefinitionMode, sourceDefinitionMtime)
			assertFileUnchanged(t, sourceSupport, sourceSupportBefore, sourceSupportMode, sourceSupportMtime)
			stateDir, err := dependencytxn.StateDir()
			if err != nil {
				t.Fatal(err)
			}
			pending, err := filepath.Glob(filepath.Join(stateDir, ".dependencytxn-*.json"))
			if err != nil || len(pending) != 1 {
				t.Fatalf("namespace transaction journal = %v, %v", pending, err)
			}
			type journalParticipant struct {
				Path      string                              `json:"path"`
				Namespace *dependencytxn.NamespaceExpectation `json:"namespace,omitempty"`
			}
			var journal struct {
				Version    int                  `json:"version"`
				Files      []journalParticipant `json:"files"`
				Assertions []journalParticipant `json:"assertions"`
			}
			rawJournal, err := os.ReadFile(pending[0])
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(rawJournal, &journal); err != nil {
				t.Fatal(err)
			}
			manifestPath, err := filepath.EvalSymlinks(fixture.manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			var manifestNamespace *dependencytxn.NamespaceExpectation
			for _, participant := range append(journal.Files, journal.Assertions...) {
				if participant.Path == manifestPath {
					manifestNamespace = participant.Namespace
				}
			}
			scopePath, err := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(fixture.manifestPath)))
			if err != nil {
				t.Fatal(err)
			}
			if journal.Version != 1 || manifestNamespace == nil ||
				manifestNamespace.ScopePath != scopePath || manifestNamespace.ScopeDevice != manifestSnapshot.scope.Device ||
				manifestNamespace.ScopeInode != manifestSnapshot.scope.Inode || manifestNamespace.ParentDevice != manifestSnapshot.parent.Device ||
				manifestNamespace.ParentInode != manifestSnapshot.parent.Inode {
				t.Fatalf("producer journal namespace/version = %d / %+v", journal.Version, manifestNamespace)
			}
			if err := os.RemoveAll(filepath.Dir(fixture.manifestPath)); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(retained, filepath.Dir(fixture.manifestPath)); err != nil {
				t.Fatal(err)
			}
			if err := dependencytxn.Recover(t.Context(), stateDir); err != nil {
				t.Fatalf("Recover after restoring captured manifest namespace: %v", err)
			}
			support := fixture.supportPlan.artifacts[0]
			supportBytes, supportMode, _ := fileSnapshot(t, fixture.supportPath)
			if !bytes.Equal(supportBytes, support.Bytes) || supportMode.Perm() != portableArtifactMode(support.Mode) {
				t.Fatalf("recovered support copy = %q / %v", supportBytes, supportMode)
			}
			manifestAfter, err := loadPortableState(fixture.repo, false)
			if err != nil {
				t.Fatal(err)
			}
			oldRel, err := filepath.Rel(fixture.factoryRoot, fixture.oldSupportPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := manifestAfter.entry(hcc.TargetFactory, filepath.ToSlash(oldRel)); !ok {
				t.Fatalf("recovery lost retained legacy support ownership: %s", manifestAfter.raw)
			}
			supportRel, err := filepath.Rel(fixture.factoryRoot, fixture.supportPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := manifestAfter.entry(hcc.TargetFactory, filepath.ToSlash(supportRel)); !ok {
				t.Fatalf("recovery did not record copied support %q: %s", supportRel, manifestAfter.raw)
			}
			if manifestAfter.mode.Perm() != manifestMode.Perm() {
				t.Fatalf("recovered manifest mode = %v, want %v", manifestAfter.mode, manifestMode)
			}
			if err := dependencytxn.CheckPending(stateDir); err != nil {
				t.Fatalf("pending after producer recovery: %v", err)
			}
		})
	}
}

func TestNativeHookManifestReceiptRejectsMatchingParentCloneWithoutStateUpdate(t *testing.T) {
	fixture := newNativeHookSupportPublicationFixture(t, config.MaterializationModeFile)
	state, err := loadPortableState(fixture.repo, false)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := state.captureManifestParent(false)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := state.manifestNamespaceExpectation(parent)
	if closeErr := parent.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	manifestBefore, manifestMode, manifestMtime := fileSnapshot(t, fixture.manifestPath)
	pinBefore := state.pin
	rawBefore := append([]byte(nil), state.raw...)
	entriesBefore := make(map[string]portableManifestEntry, len(state.entries))
	for key, entry := range state.entries {
		entriesBefore[key] = entry
	}
	nextState := *state
	nextState.entries = make(map[string]portableManifestEntry, len(state.entries))
	for key, entry := range state.entries {
		nextState.entries[key] = entry
	}
	nextState.add(hcc.TargetFactory, "hook-support/receipt-control.sh", nativeHookRegularSnapshotDigest([]byte("receipt bytes\n"), 0o600))
	nextBytes, hasEntries, err := nextState.serialized()
	if err != nil || !hasEntries {
		t.Fatalf("serialize receipt bytes: %v / %v", hasEntries, err)
	}
	participant := dependencytxn.DesiredFile{
		Path: fixture.manifestPath, Expected: dependencytxn.Generation{Exists: state.exists},
		ExpectedMode: state.mode, Namespace: namespace, Bytes: nextBytes, Mode: state.mode,
	}
	if state.exists {
		participant.Expected.Digest = dependencytxn.Digest(state.raw)
	}
	retained := replaceNativeManifestParentWithIdenticalClone(t, fixture)
	if err := os.WriteFile(fixture.manifestPath, nextBytes, state.mode.Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture.manifestPath, state.mode.Perm()); err != nil {
		t.Fatal(err)
	}
	cloneBytes, cloneMode, cloneMtime := fileSnapshot(t, fixture.manifestPath)
	err = refreshNativeHookManifestCommit(nativeHookManifestCommit{state: state, participant: participant})
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("receipt for matching-bytes parent clone = %v, want ErrGenerationConflict", err)
	}
	assertFileUnchanged(t, filepath.Join(retained, filepath.Base(fixture.manifestPath)), manifestBefore, manifestMode, manifestMtime)
	assertFileUnchanged(t, fixture.manifestPath, cloneBytes, cloneMode, cloneMtime)
	if !bytes.Equal(state.raw, rawBefore) || state.exists != (pinBefore.data != nil) || state.mode != pinBefore.file.Mode.Perm() || !portableManifestSnapshotMatches(pinBefore, state.pin) {
		t.Fatal("refused receipt refreshed captured manifest state")
	}
	if len(state.entries) != len(entriesBefore) {
		t.Fatalf("refused receipt changed entry count: %d, want %d", len(state.entries), len(entriesBefore))
	}
	for key, entry := range entriesBefore {
		if state.entries[key] != entry {
			t.Fatalf("refused receipt changed entry %q", key)
		}
	}
}

func TestNativeRepositoryHookSupportManifestRefreshRetainsForeignEntries(t *testing.T) {
	fixture, _ := seedNativeRepositorySupportLink(t, config.MaterializationModeFile)
	foreignFile, foreignBefore, foreignBytes, foreignMode, foreignMtime := seedNativeHookSupportForeignManifest(t, fixture)
	laterSkill := resolver.ResolvedArtifact{
		Entry: catalog.Entry{
			Kind: ir.KindSkill, Name: "later-generic",
			Resource: ir.NewSkill("later-generic", "Later generic manifest writer", "inert later generic body", nil, nil, ir.SourceRef{}),
		},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}
	report, err := Run(t.Context(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook, laterSkill}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}))
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("native support CAS followed by generic manifest save failed: err=%v report=%+v", err, report)
	}
	if !reportHasHookSupportWrite(report) {
		t.Fatalf("Native support copy was not published: %+v", report.PerTarget)
	}
	if info, err := os.Lstat(fixture.supportPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("Native support output is not a regular copy: %v / %v", info, err)
	}
	if data, err := os.ReadFile(filepath.Join(fixture.factoryRoot, "skills", "later-generic", "SKILL.md")); err != nil || !bytes.Contains(data, []byte("inert later generic body")) {
		t.Fatalf("later generic skill output missing: %q / %v", data, err)
	}
	if data, mode, mtime := fileSnapshot(t, foreignFile); !bytes.Equal(data, foreignBytes) || mode.Perm() != foreignMode.Perm() || !mtime.Equal(foreignMtime) {
		t.Fatalf("foreign manifest output changed: %q / %v / %v", data, mode, mtime)
	}
	updated, err := loadPortableState(fixture.repo, false)
	if err != nil {
		t.Fatal(err)
	}
	if entry, ok := updated.entry(hcc.TargetClaude, foreignBefore.Path); !ok || entry != foreignBefore {
		t.Fatalf("generic save lost or changed unrelated manifest entry: %+v / %t / %s", entry, ok, updated.raw)
	}
	supportRel, err := filepath.Rel(fixture.factoryRoot, fixture.supportPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := updated.entry(hcc.TargetFactory, filepath.ToSlash(supportRel)); !ok {
		t.Fatalf("generic save lost Native support ownership for %q", supportRel)
	}
	if !bytes.Contains(updated.raw, []byte("later-generic")) {
		t.Fatalf("generic manifest save did not record the later resource: %s", updated.raw)
	}
}

// spec: hook-resources/proven-repository-support-links-convert-with-recovery
func TestNativeRepositoryHookSupportCASRevalidatesAllAuthority(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, nativeHookSupportPublicationFixture, string) error
	}{
		{
			name: "cache bytes same size",
			mutate: func(t *testing.T, _ nativeHookSupportPublicationFixture, cachePath string) error {
				data, mode, _ := fileSnapshot(t, cachePath)
				data[0] ^= 1
				if err := os.WriteFile(cachePath, data, mode.Perm()); err != nil {
					return err
				}
				return os.Chmod(cachePath, mode.Perm())
			},
		},
		{
			name: "cache mode",
			mutate: func(t *testing.T, _ nativeHookSupportPublicationFixture, cachePath string) error {
				info := nativeSupportFileInfo(t, cachePath)
				mode := os.FileMode(0o600)
				if info.Mode().Perm() == mode {
					mode = 0o640
				}
				return os.Chmod(cachePath, mode)
			},
		},
		{
			name: "Native aggregate",
			mutate: func(t *testing.T, fixture nativeHookSupportPublicationFixture, _ string) error {
				data, mode, _ := fileSnapshot(t, fixture.nativePath)
				drifted := bytes.Replace(data, []byte("/scripts/check.sh"), []byte("/scripts/other.sh"), 1)
				if bytes.Equal(data, drifted) {
					return errors.New("Factory Native command does not contain the expected support path")
				}
				if err := os.WriteFile(fixture.nativePath, drifted, mode.Perm()); err != nil {
					return err
				}
				return os.Chmod(fixture.nativePath, mode.Perm())
			},
		},
		{
			name: "ownership proof state",
			mutate: func(t *testing.T, fixture nativeHookSupportPublicationFixture, _ string) error {
				store, err := ownership.Open(fixture.factoryRoot)
				if err != nil {
					return err
				}
				record, err := store.Load()
				if err != nil {
					return err
				}
				record.Generation++
				data, err := json.Marshal(record)
				if err != nil {
					return err
				}
				info := nativeSupportFileInfo(t, fixture.statePath)
				data = append(data, '\n')
				if err := os.WriteFile(fixture.statePath, data, info.Mode().Perm()); err != nil {
					return err
				}
				return os.Chmod(fixture.statePath, info.Mode().Perm())
			},
		},
		{
			name: "legacy aggregate",
			mutate: func(t *testing.T, fixture nativeHookSupportPublicationFixture, _ string) error {
				data := []byte(`{"PostToolUse":[{"matcher":"Manual","hooks":[{"type":"command","command":"echo concurrent"}]}]}`)
				if err := os.MkdirAll(filepath.Dir(fixture.legacyPath), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(fixture.legacyPath, data, 0o640); err != nil {
					return err
				}
				return os.Chmod(fixture.legacyPath, 0o640)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, cachePath := seedNativeRepositorySupportLink(t, config.MaterializationModeSymlink)
			supportInfo := nativeSupportFileInfo(t, fixture.supportPath)
			var callbackCalled bool
			var externalState map[string]nativeSupportState
			fault := func(stage, destination string) error {
				if stage != "output" || filepath.Clean(destination) != filepath.Clean(fixture.supportPath) || callbackCalled {
					return nil
				}
				callbackCalled = true
				if err := tc.mutate(t, fixture, cachePath); err != nil {
					return err
				}
				externalState = snapshotNativeRepositorySupportLink(t, fixture)
				return nil
			}
			report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}), withNativeHookSupportWriteFault(fault))
			if err != nil || report == nil || !hasErrorDiagnostics(report.Diags) {
				t.Fatalf("authority-generation refusal: %v / %v", err, report)
			}
			if !callbackCalled {
				t.Fatal("support callback did not run after authority planning")
			}
			requireNativeHookSupportConflict(t, report)
			assertNativeRepositorySupportLinkSnapshot(t, fixture, externalState)
			assertNativeSupportSameFile(t, fixture.supportPath, supportInfo)
			stateDir, err := dependencytxn.StateDir()
			if err != nil {
				t.Fatal(err)
			}
			if err := dependencytxn.CheckPending(stateDir); err != nil {
				t.Fatalf("authority refusal left a dependency transaction pending: %v", err)
			}
		})
	}
}

// spec: hook-resources/proven-repository-support-links-convert-with-recovery
func TestNativeRepositoryHookSupportConversionPreservesNativeGenerationSwap(t *testing.T) {
	fixture, _ := seedNativeRepositorySupportLink(t, config.MaterializationModeSymlink)
	_, nativeMode, _ := fileSnapshot(t, fixture.nativePath)
	foreignNative := []byte(`{"SessionStart":[]}` + "\n")
	var externalState map[string]nativeSupportState
	var callbackCalled bool
	fault := func(stage, destination string) error {
		if stage != "before-native-commit" || filepath.Clean(destination) != filepath.Clean(fixture.supportPath) {
			return nil
		}
		callbackCalled = true
		info, err := os.Lstat(destination)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("repository support was not copied before native revalidation")
		}
		if err := os.WriteFile(fixture.nativePath, foreignNative, nativeMode.Perm()); err != nil {
			return err
		}
		if err := os.Chmod(fixture.nativePath, nativeMode.Perm()); err != nil {
			return err
		}
		externalState = nativeSupportSnapshot(t, fixture.repo, filepath.Dir(fixture.statePath), cache.CacheDir())
		return nil
	}
	report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}), withNativeHookSupportWriteFault(fault))
	if err != nil || report == nil || !hasErrorDiagnostics(report.Diags) {
		t.Fatalf("native-generation conversion refusal: %v / %v", err, report)
	}
	if !callbackCalled || !diag.HasCode(report.Diags, diag.CodeOwnershipGenerationConflict) {
		t.Fatalf("native generation swap was not refused: called=%v diagnostics=%+v", callbackCalled, report.Diags)
	}
	nativeAfter, err := os.ReadFile(fixture.nativePath)
	if err != nil || !bytes.Equal(nativeAfter, foreignNative) {
		t.Fatalf("foreign Native generation changed after refusal: %q / %v", nativeAfter, err)
	}
	assertNativeSupportSnapshot(t, externalState, nativeSupportSnapshot(t, fixture.repo, filepath.Dir(fixture.statePath), cache.CacheDir()))
	copyBytes, copyMode, _ := fileSnapshot(t, fixture.supportPath)
	if !bytes.Equal(copyBytes, fixture.supportPlan.artifacts[0].Bytes) || copyMode.Perm() != portableArtifactMode(fixture.supportPlan.artifacts[0].Mode) {
		t.Fatalf("support copy after native-generation refusal = %q / %o", copyBytes, copyMode.Perm())
	}
	manifest, err := loadPortableState(fixture.repo, false)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(fixture.factoryRoot, fixture.supportPath)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := manifest.entry(hcc.TargetFactory, filepath.ToSlash(rel))
	if !ok || entry.Digest != portableArtifactDigest(fixture.supportPlan.artifacts[0]) {
		t.Fatalf("support-first copy manifest entry = %+v, found=%v", entry, ok)
	}
}

func TestOpenHookSupportDirDuplicateIsCloseOnExec(t *testing.T) {
	rootFD, err := unix.Open(t.TempDir(), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Close(rootFD) }()
	dirFD, err := openHookSupportDirAt(rootFD, ".")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Close(dirFD) }()
	flags, err := unix.FcntlInt(uintptr(dirFD), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("duplicated hook source directory descriptor is not close-on-exec")
	}
}

// spec: hook-resources/unproven-or-unsafe-repository-support-links-are-preserved
func TestNativeRepositoryHookSupportSourceGenerationChangeRefusesOldLink(t *testing.T) {
	fixture, _ := seedNativeRepositorySupportLink(t, config.MaterializationModeSymlink)
	sourcePath := filepath.Join(fixture.hook.RegistryURL, "hooks", "scripts", "check.sh")
	changedSource := []byte("#!/bin/sh\necho source generation changed\n")
	if err := os.WriteFile(sourcePath, changedSource, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sourcePath, 0o755); err != nil {
		t.Fatal(err)
	}
	before := nativeSupportSnapshot(t, fixture.repo, filepath.Dir(fixture.statePath), cache.CacheDir(), fixture.hook.RegistryURL)
	report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}))
	if err != nil || report == nil || !hasErrorDiagnostics(report.Diags) {
		t.Fatalf("source-generation conversion refusal: %v / %v", err, report)
	}
	requireNativeHookSupportConflict(t, report)
	assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, fixture.repo, filepath.Dir(fixture.statePath), cache.CacheDir(), fixture.hook.RegistryURL))
	blocked := false
	for _, resource := range report.BlockedResources {
		blocked = blocked || resource.Target == hcc.TargetFactory && resource.Name == fixture.hook.Name
	}
	if !blocked {
		t.Fatalf("source-generation-changed hook was not blocked: %+v", report.BlockedResources)
	}
}

// spec: hook-resources/unproven-or-unsafe-repository-support-links-are-preserved
func TestNativeRepositoryHookSupportRevalidatesCapturedSourceSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(nativeHookSupportPublicationFixture) error
	}{
		{
			name: "definition bytes same size",
			mutate: func(fixture nativeHookSupportPublicationFixture) error {
				path := filepath.Join(fixture.hook.RegistryURL, "hooks", "hooks.json")
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				changed := bytes.Replace(data, []byte("check.sh"), []byte("other.sh"), 1)
				if len(changed) != len(data) || bytes.Equal(changed, data) {
					return errors.New("definition mutation did not preserve its size")
				}
				if err := os.WriteFile(path, changed, 0o644); err != nil {
					return err
				}
				return os.Chmod(path, 0o644)
			},
		},
		{
			name: "support bytes same size",
			mutate: func(fixture nativeHookSupportPublicationFixture) error {
				path := filepath.Join(fixture.hook.RegistryURL, "hooks", "scripts", "check.sh")
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if len(data) == 0 {
					return errors.New("support mutation needs a non-empty file")
				}
				data[0] ^= 1
				if err := os.WriteFile(path, data, 0o755); err != nil {
					return err
				}
				return os.Chmod(path, 0o755)
			},
		},
		{
			name: "support mode",
			mutate: func(fixture nativeHookSupportPublicationFixture) error {
				path := filepath.Join(fixture.hook.RegistryURL, "hooks", "scripts", "check.sh")
				info, err := os.Stat(path)
				if err != nil {
					return err
				}
				mode := info.Mode().Perm() ^ 0o100
				if mode == info.Mode().Perm() {
					return errors.New("support mode mutation did not change permissions")
				}
				return os.Chmod(path, mode)
			},
		},
		{
			name: "support inventory addition",
			mutate: func(fixture nativeHookSupportPublicationFixture) error {
				path := filepath.Join(fixture.hook.RegistryURL, "hooks", "scripts", "added.sh")
				return os.WriteFile(path, []byte("additional support\n"), 0o644)
			},
		},
		{
			name: "support inventory removal",
			mutate: func(fixture nativeHookSupportPublicationFixture) error {
				return os.Remove(filepath.Join(fixture.hook.RegistryURL, "hooks", "scripts", "check.sh"))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, _ := seedNativeRepositorySupportLink(t, config.MaterializationModeSymlink)
			supportInfo := nativeSupportFileInfo(t, fixture.supportPath)
			var callbackCalled bool
			var externalState map[string]nativeSupportState
			fault := func(stage, destination string) error {
				if stage != "output" || filepath.Clean(destination) != filepath.Clean(fixture.supportPath) || callbackCalled {
					return nil
				}
				callbackCalled = true
				if err := tc.mutate(fixture); err != nil {
					return err
				}
				externalState = snapshotNativeRepositorySupportLink(t, fixture, fixture.hook.RegistryURL)
				return nil
			}
			report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}), withNativeHookSupportWriteFault(fault))
			if err != nil || report == nil || !hasErrorDiagnostics(report.Diags) {
				t.Fatalf("captured-source generation change: %v / %v", err, report)
			}
			if !callbackCalled {
				t.Fatal("source mutation callback did not run after canonical capture")
			}
			requireNativeHookSupportConflict(t, report)
			assertNativeRepositorySupportLinkSnapshot(t, fixture, externalState, fixture.hook.RegistryURL)
			assertNativeSupportSameFile(t, fixture.supportPath, supportInfo)
			blocked := false
			for _, resource := range report.BlockedResources {
				blocked = blocked || resource.Target == hcc.TargetFactory && resource.Name == fixture.hook.Name
			}
			if !blocked {
				t.Fatalf("source-changed hook was not blocked: %+v", report.BlockedResources)
			}
		})
	}
}

// spec: hook-resources/unproven-or-unsafe-repository-support-links-are-preserved
func TestNativeRepositoryHookSupportFirstInstallSourceMutationCreatesNoNamespace(t *testing.T) {
	fixture := newNativeHookSupportPublicationFixture(t, config.MaterializationModeSymlink)
	supportNamespace := filepath.Join(fixture.factoryRoot, "hook-support")
	if _, err := os.Lstat(fixture.supportPath); !os.IsNotExist(err) {
		t.Fatalf("support destination exists before first install: %v", err)
	}
	if _, err := os.Lstat(supportNamespace); !os.IsNotExist(err) {
		t.Fatalf("support namespace exists before first install: %v", err)
	}
	if _, err := os.Lstat(fixture.nativePath); !os.IsNotExist(err) {
		t.Fatalf("Native output exists before first install: %v", err)
	}
	if _, err := os.Lstat(fixture.statePath); !os.IsNotExist(err) {
		t.Fatalf("Native ownership state exists before first install: %v", err)
	}
	sourcePath := filepath.Join(fixture.hook.RegistryURL, "hooks", "scripts", "check.sh")
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	var callbackCalled bool
	var externalState map[string]nativeSupportState
	fault := func(stage, destination string) error {
		if stage != "output" || filepath.Clean(destination) != filepath.Clean(fixture.supportPath) || callbackCalled {
			return nil
		}
		callbackCalled = true
		data, mode, _ := fileSnapshot(t, sourcePath)
		if len(data) == 0 {
			return errors.New("source mutation needs a non-empty support file")
		}
		data[0] ^= 1
		if err := os.WriteFile(sourcePath, data, mode.Perm()); err != nil {
			return err
		}
		if err := os.Chmod(sourcePath, mode.Perm()); err != nil {
			return err
		}
		externalState = nativeSupportSnapshot(t, fixture.repo, filepath.Dir(fixture.statePath), cache.CacheDir(), fixture.hook.RegistryURL, stateDir, supportNamespace)
		return nil
	}
	report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook},
		WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}),
		withNativeHookSupportWriteFault(fault))
	if err != nil || report == nil || !hasErrorDiagnostics(report.Diags) {
		t.Fatalf("first-install source mutation refusal: %v / %v", err, report)
	}
	if !callbackCalled {
		t.Fatal("first-install output callback did not run after source capture")
	}
	requireNativeHookSupportConflict(t, report)
	assertNativeRepositorySupportLinkSnapshot(t, fixture, externalState, fixture.hook.RegistryURL, stateDir, supportNamespace)
	assertPathAbsent(t, fixture.supportPath)
	assertPathAbsent(t, supportNamespace)
	assertPathAbsent(t, fixture.nativePath)
	assertPathAbsent(t, fixture.statePath)
	if err := dependencytxn.CheckPending(stateDir); err != nil {
		t.Fatalf("source mutation refusal left a dependency transaction pending: %v", err)
	}
	blocked := false
	for _, resource := range report.BlockedResources {
		blocked = blocked || resource.Target == hcc.TargetFactory && resource.Name == fixture.hook.Name
	}
	if !blocked {
		t.Fatalf("source-mutated first-install hook was not blocked: %+v", report.BlockedResources)
	}
}

// spec: hook-resources/unproven-or-unsafe-repository-support-links-are-preserved
func TestNativeRepositoryHookSupportMatchingForeignRegularFileIsNotAdopted(t *testing.T) {
	fixture, cachePath := seedNativeRepositorySupportLink(t, config.MaterializationModeSymlink)
	support := fixture.supportPlan.artifacts[0]
	if err := os.Remove(fixture.supportPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.supportPath, support.Bytes, portableArtifactMode(support.Mode)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture.supportPath, portableArtifactMode(support.Mode)); err != nil {
		t.Fatal(err)
	}
	state, err := loadPortableState(fixture.repo, false)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(fixture.factoryRoot, fixture.supportPath)
	if err != nil {
		t.Fatal(err)
	}
	manifestKey := filepath.ToSlash(rel)
	state.remove(hcc.TargetFactory, manifestKey)
	if err := state.save(false); err != nil {
		t.Fatal(err)
	}
	if _, owned := state.entry(hcc.TargetFactory, manifestKey); owned {
		t.Fatal("matching foreign file retained a manifest claim in the fixture")
	}
	foreignBytes, foreignMode, foreignMtime := fileSnapshot(t, fixture.supportPath)
	foreignInfo := nativeSupportFileInfo(t, fixture.supportPath)
	nativeBytes, nativeMode, nativeMtime := fileSnapshot(t, fixture.nativePath)
	ownerBytes, ownerMode, ownerMtime := fileSnapshot(t, fixture.statePath)
	cacheBytes, cacheMode, cacheMtime := fileSnapshot(t, cachePath)
	manifestBytes, manifestMode, manifestMtime, manifestExists := optionalNativeSupportFileSnapshot(t, fixture.manifestPath)
	report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}))
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) || reportHasHookSupportWrite(report) {
		t.Fatalf("matching foreign support satisfaction: %v / %v", err, report)
	}
	assertFileUnchanged(t, fixture.supportPath, foreignBytes, foreignMode, foreignMtime)
	assertNativeSupportSameFile(t, fixture.supportPath, foreignInfo)
	assertFileUnchanged(t, fixture.nativePath, nativeBytes, nativeMode, nativeMtime)
	assertFileUnchanged(t, fixture.statePath, ownerBytes, ownerMode, ownerMtime)
	assertFileUnchanged(t, cachePath, cacheBytes, cacheMode, cacheMtime)
	if manifestExists {
		assertFileUnchanged(t, fixture.manifestPath, manifestBytes, manifestMode, manifestMtime)
	} else {
		assertPathAbsent(t, fixture.manifestPath)
	}
	manifest, err := loadPortableState(fixture.repo, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, owned := manifest.entry(hcc.TargetFactory, manifestKey); owned {
		t.Fatal("matching foreign support file was adopted into the portable manifest")
	}
}

type nativeRepositorySupportMultiLinkFixture struct {
	repo        string
	factoryRoot string
	nativePath  string
	hooks       []resolver.ResolvedArtifact
	plans       []hookSupportPlan
}

func seedNativeRepositorySupportMultiLinkFixture(t *testing.T, mode config.MaterializationMode) nativeRepositorySupportMultiLinkFixture {
	t.Helper()
	repo, _ := additiveSafetyEnv(t)
	definition := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo shared-proof"}]}]}}`)
	supportFiles := map[string][]byte{"hooks/scripts/check.sh": []byte("#!/bin/sh\necho shared support\n")}
	alpha, _ := sourceBackedHookFixtures(t, "claude", config.RepoCommitted, hcc.TargetFactory, "alpha", definition, supportFiles)
	beta, _ := sourceBackedHookFixtures(t, "claude", config.RepoCommitted, hcc.TargetFactory, "beta", definition, supportFiles)
	if len(alpha) != 1 || len(beta) != 1 {
		t.Fatalf("source-backed contributors = %d/%d, want one each", len(alpha), len(beta))
	}
	hooks := []resolver.ResolvedArtifact{alpha[0], beta[0]}
	work := []skillTarget{{skill: alpha[0], target: hcc.TargetFactory}, {skill: beta[0], target: hcc.TargetFactory}}
	plans, conflicts := prepareHookSupport(work)
	if len(conflicts) != 0 || plans[0].err != nil || plans[1].err != nil || len(plans[0].artifacts) == 0 || len(plans[1].artifacts) == 0 {
		t.Fatalf("support plans = %+v conflicts=%v", plans, conflicts)
	}
	artifacts := append(append([]artifact.Artifact(nil), plans[0].artifacts...), plans[1].artifacts...)
	artifactOwners := make(map[string]string, len(artifacts))
	for i, plan := range []hookSupportPlan{plans[0], plans[1]} {
		for _, a := range plan.artifacts {
			artifactOwners[a.Path] = hooks[i].Name
		}
	}
	factoryRoot := filepath.Join(repo, ".factory")
	report, err := Run(context.Background(), repo, hooks, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}))
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed multi-source native proof: %v / %v", err, report)
	}
	nativePath := filepath.Join(factoryRoot, "hooks.json")
	store, err := ownership.Open(factoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof("hooks.json")
	if err != nil || proof.OwnedCount() != 1 || len(proof.OwnedIDs()) != 1 {
		t.Fatalf("multi-source Native proof = %v / %+v", err, proof.OwnedIDs())
	}
	id := proof.OwnedIDs()[0]
	if !proof.HasHookSource(id, alpha[0].Name, plans[0].sourceDigest) || !proof.HasHookSource(id, beta[0].Name, plans[1].sourceDigest) {
		t.Fatalf("multi-source Native proof lacks both sources: %+v", proof.OwnedIDs())
	}
	outPlans := materializePlans(hcc.TargetFactory, factoryRoot, artifacts, nil, false)
	var outputPlan *materializePlan
	for i := range outPlans {
		if len(outPlans[i].artifacts) != len(artifacts) {
			continue
		}
		allOwned := true
		for _, candidate := range outPlans[i].artifacts {
			if artifactOwners[filepath.ToSlash(path.Join(outPlans[i].reportPrefix, candidate.Path))] == "" {
				allOwned = false
			}
		}
		if allOwned {
			outputPlan = &outPlans[i]
			break
		}
	}
	if outputPlan == nil {
		t.Fatalf("support artifacts have no single Factory output plan: %+v", outPlans)
	}
	cacheDir := cachedPlanDirectoryForOutput(outputKey{target: hcc.TargetFactory}, factoryRoot, *outputPlan, nil)
	if _, err := materializeArtifactLinkCache(cacheDir, outputPlan.artifacts, false, false); err != nil {
		t.Fatal(err)
	}
	manifestState, err := loadPortableState(repo, false)
	if err != nil {
		t.Fatal(err)
	}
	fixture := nativeRepositorySupportMultiLinkFixture{
		repo: repo, factoryRoot: factoryRoot, nativePath: nativePath, hooks: hooks, plans: []hookSupportPlan{plans[0], plans[1]},
	}
	for _, candidate := range outputPlan.artifacts {
		relative := filepath.ToSlash(path.Join(outputPlan.reportPrefix, candidate.Path))
		ownerName := artifactOwners[relative]
		if ownerName == "" {
			t.Fatalf("no source owner for support artifact %q", relative)
		}
		outputPath := filepath.Join(outputPlan.outDir, filepath.FromSlash(candidate.Path))
		cachePath := filepath.Join(cacheDir, filepath.FromSlash(candidate.Path))
		if info, err := os.Lstat(outputPath); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("seed repository support copy %s: %v / %v", outputPath, info, err)
		}
		manifestState.remove(hcc.TargetFactory, relative)
		if err := os.Remove(outputPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(cachePath, outputPath); err != nil {
			t.Fatal(err)
		}
	}
	if err := manifestState.save(false); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// spec: hook-resources/unproven-or-unsafe-repository-support-links-are-preserved
func TestNativeRepositoryHookSupportPartialSourceProofPreservesLinks(t *testing.T) {
	fixture := seedNativeRepositorySupportMultiLinkFixture(t, config.MaterializationModeSymlink)
	store, err := ownership.Open(fixture.factoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	fileState := record.Files["hooks.json"]
	if len(fileState.Entries) != 1 {
		t.Fatalf("Factory proof entries = %d, want one collapsed entry", len(fileState.Entries))
	}
	entry := fileState.Entries[0]
	sources := append([]ownership.HookSourceEvidence{{Resource: entry.Resource, SourceDigest: entry.SourceDigest}}, entry.SourceEvidence...)
	filtered := make([]ownership.HookSourceEvidence, 0, len(sources))
	removed := false
	for _, source := range sources {
		if source.Resource == fixture.hooks[1].Name && source.SourceDigest == fixture.plans[1].sourceDigest {
			removed = true
			continue
		}
		filtered = append(filtered, source)
	}
	if !removed || len(filtered) == 0 {
		t.Fatal("fixture did not contain both contributor proof pairs")
	}
	entry.Resource = filtered[0].Resource
	entry.SourceDigest = filtered[0].SourceDigest
	entry.SourceEvidence = append([]ownership.HookSourceEvidence(nil), filtered[1:]...)
	nativeBytes, err := os.ReadFile(fixture.nativePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(ownership.Mutation{
		Rel: "hooks.json", AbsFile: fixture.nativePath, NextBytes: nativeBytes,
		Entries: []ownership.Entry{entry}, Reason: ownership.ReasonSync,
		ExpectPriorGeneration: record.Generation, ExpectPriorDigest: ownership.Digest(nativeBytes),
	}); err != nil {
		t.Fatal(err)
	}
	alphaArtifact := fixture.plans[0].artifacts[0]
	betaArtifact := fixture.plans[1].artifacts[0]
	alphaPath, err := portableOutputPath(fixture.factoryRoot, alphaArtifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	betaPath, err := portableOutputPath(fixture.factoryRoot, betaArtifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(alphaPath) == filepath.Clean(betaPath) {
		t.Fatalf("support contributors unexpectedly share one path: %q", alphaPath)
	}
	alphaCachePath, err := os.Readlink(alphaPath)
	if err != nil {
		t.Fatal(err)
	}
	betaCachePath, err := os.Readlink(betaPath)
	if err != nil {
		t.Fatal(err)
	}
	alphaLinkInfo := nativeSupportFileInfo(t, alphaPath)
	betaLinkInfo := nativeSupportFileInfo(t, betaPath)
	alphaCacheInfo := nativeSupportFileInfo(t, alphaCachePath)
	betaCacheInfo := nativeSupportFileInfo(t, betaCachePath)
	alphaCacheBytes, alphaCacheMode, alphaCacheMtime := fileSnapshot(t, alphaCachePath)
	betaCacheBytes, betaCacheMode, betaCacheMtime := fileSnapshot(t, betaCachePath)
	beforeNative, nativeMode, nativeMtime := fileSnapshot(t, fixture.nativePath)
	proof, err := store.Proof("hooks.json")
	if err != nil || proof.OwnedCount() != 1 || !proof.HasHookSource(proof.OwnedIDs()[0], fixture.hooks[0].Name, fixture.plans[0].sourceDigest) ||
		proof.HasHookSource(proof.OwnedIDs()[0], fixture.hooks[1].Name, fixture.plans[1].sourceDigest) {
		t.Fatalf("partial Native proof does not isolate the selected sources: %v / %+v", err, proof.OwnedIDs())
	}
	report, err := Run(context.Background(), fixture.repo, fixture.hooks, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}))
	if err != nil || report == nil || !hasErrorDiagnostics(report.Diags) {
		t.Fatalf("partial source-proof conversion: %v / %v", err, report)
	}
	requireNativeHookSupportConflict(t, report)
	blockedAlpha, blockedBeta := false, false
	for _, resource := range report.BlockedResources {
		blockedAlpha = blockedAlpha || resource.Target == hcc.TargetFactory && resource.Name == fixture.hooks[0].Name
		blockedBeta = blockedBeta || resource.Target == hcc.TargetFactory && resource.Name == fixture.hooks[1].Name
	}
	if blockedAlpha || !blockedBeta {
		t.Fatalf("partial-source blocked resources = %+v, want only %q", report.BlockedResources, fixture.hooks[1].Name)
	}
	alphaInfo := nativeSupportFileInfo(t, alphaPath)
	alphaBytes, alphaMode, _ := fileSnapshot(t, alphaPath)
	if !alphaInfo.Mode().IsRegular() || os.SameFile(alphaLinkInfo, alphaInfo) || !bytes.Equal(alphaBytes, alphaArtifact.Bytes) || alphaMode.Perm() != portableArtifactMode(alphaArtifact.Mode) {
		t.Fatalf("proven sibling support copy = %q / %o / %v", alphaBytes, alphaMode.Perm(), alphaInfo.Mode())
	}
	betaInfo := nativeSupportFileInfo(t, betaPath)
	if betaInfo.Mode()&os.ModeSymlink == 0 || !os.SameFile(betaLinkInfo, betaInfo) {
		t.Fatalf("unproven sibling support link changed: %v", betaInfo.Mode())
	}
	if target, err := os.Readlink(betaPath); err != nil || filepath.Clean(target) != filepath.Clean(betaCachePath) {
		t.Fatalf("unproven support link = %q / %v, want %q", target, err, betaCachePath)
	}
	assertNativeSupportSameFile(t, alphaCachePath, alphaCacheInfo)
	assertNativeSupportSameFile(t, betaCachePath, betaCacheInfo)
	assertFileUnchanged(t, alphaCachePath, alphaCacheBytes, alphaCacheMode, alphaCacheMtime)
	assertFileUnchanged(t, betaCachePath, betaCacheBytes, betaCacheMode, betaCacheMtime)
	assertFileUnchanged(t, fixture.nativePath, beforeNative, nativeMode, nativeMtime)
	manifest, err := loadPortableState(fixture.repo, false)
	if err != nil {
		t.Fatal(err)
	}
	alphaRel, err := filepath.Rel(fixture.factoryRoot, alphaPath)
	if err != nil {
		t.Fatal(err)
	}
	alphaEntry, owned := manifest.entry(hcc.TargetFactory, filepath.ToSlash(alphaRel))
	if !owned || alphaEntry.Digest != portableArtifactDigest(alphaArtifact) {
		t.Fatalf("proven sibling manifest entry = %+v, owned=%v", alphaEntry, owned)
	}
	betaRel, err := filepath.Rel(fixture.factoryRoot, betaPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, owned := manifest.entry(hcc.TargetFactory, filepath.ToSlash(betaRel)); owned {
		t.Fatal("unproven support link acquired a portable manifest claim")
	}
}

// spec: hook-resources/unproven-or-unsafe-repository-support-links-are-preserved
func TestNativeRepositoryHookSupportUnprovenLinkDoesNotBlockValidSibling(t *testing.T) {
	for _, tc := range []struct {
		name             string
		unprovenSource   bool
		mutateCacheEntry func(*testing.T, string)
	}{
		{name: "unrelated source proof", unprovenSource: true},
		{
			name: "cache bytes same size",
			mutateCacheEntry: func(t *testing.T, path string) {
				data, mode, _ := fileSnapshot(t, path)
				if len(data) == 0 {
					t.Fatal("cache bytes mutation needs a non-empty referent")
				}
				data[0] ^= 1
				if err := os.WriteFile(path, data, mode.Perm()); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, mode.Perm()); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "cache mode",
			mutateCacheEntry: func(t *testing.T, path string) {
				info := nativeSupportFileInfo(t, path)
				mode := info.Mode().Perm() ^ 0o100
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "cache FIFO",
			mutateCacheEntry: func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, alphaCachePath := seedNativeRepositorySupportLink(t, config.MaterializationModeSymlink)
			if tc.unprovenSource {
				replaceNativeRepositorySupportProofWithForeignSource(t, fixture)
			}
			if tc.mutateCacheEntry != nil {
				tc.mutateCacheEntry(t, alphaCachePath)
			}
			store, err := ownership.Open(fixture.factoryRoot)
			if err != nil {
				t.Fatal(err)
			}
			proof, err := store.Proof("hooks.json")
			if err != nil || proof.OwnedCount() != 1 {
				t.Fatalf("Factory source proof = %v / %+v", err, proof.OwnedIDs())
			}
			id := proof.OwnedIDs()[0]
			if clean := proof.HasHookSource(id, fixture.hook.Name, fixture.supportPlan.sourceDigest); clean == tc.unprovenSource {
				t.Fatalf("selected proof state is inconsistent with %q: clean=%v", tc.name, clean)
			}
			_, nativeSpec, err := nativeHookJSONLayout(outputKey{target: hcc.TargetFactory}, fixture.repo)
			if err != nil {
				t.Fatal(err)
			}
			nativeRaw, err := ownership.ReadAggregateFileAt(fixture.factoryRoot, nativeSpec.rel)
			if err != nil {
				t.Fatal(err)
			}
			cleanSources, err := nativeHookCleanSupportSources(nativeSpec, nativeRaw, proof, nil)
			if err != nil || cleanSources[hookSupportOwnerID(fixture.hook.Name, fixture.supportPlan.sourceDigest)] == tc.unprovenSource {
				t.Fatalf("Native proof/document state is inconsistent with %q: clean=%v err=%v", tc.name, cleanSources, err)
			}
			siblingFiles := map[string]string{
				"hooks/hooks.json":         `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/sibling.sh"}]}]}`,
				"hooks/scripts/sibling.sh": "#!/bin/sh\necho independent sibling\n",
			}
			siblingProvider := nativeSupportSource(t, "claude", "hooks/hooks.json", siblingFiles, "independent-sibling")
			siblingResolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{siblingProvider})
			if len(siblingResolved) != 1 {
				t.Fatalf("independent sibling selection = %d, want one", len(siblingResolved))
			}
			siblingPlans, siblingConflicts := prepareHookSupport([]skillTarget{{skill: siblingResolved[0], target: hcc.TargetFactory}})
			siblingPlan := siblingPlans[0]
			if len(siblingConflicts) != 0 || siblingPlan.err != nil || len(siblingPlan.artifacts) != 1 {
				t.Fatalf("independent sibling support plan = %+v / %v", siblingPlan, siblingConflicts)
			}
			siblingArtifact := siblingPlan.artifacts[0]
			siblingPath, err := portableOutputPath(fixture.factoryRoot, siblingArtifact.Path)
			if err != nil {
				t.Fatal(err)
			}
			alphaHook, alphaOK := fixture.hook.Resource.(*ir.Hook)
			siblingHook, siblingOK := siblingResolved[0].Resource.(*ir.Hook)
			if !alphaOK || !siblingOK || alphaHook.Command == siblingHook.Command || filepath.Clean(siblingPath) == filepath.Clean(fixture.supportPath) {
				t.Fatalf("selected hooks are not independent: alpha=%+v sibling=%+v support=%q/%q", alphaHook, siblingHook, fixture.supportPath, siblingPath)
			}
			if _, err := os.Lstat(siblingPath); !os.IsNotExist(err) {
				t.Fatalf("valid sibling support destination exists before Run: %v", err)
			}
			if !isManagedSymlink(fixture.supportPath) {
				t.Fatal("unproven fixture link does not take the managed-cache preflight path")
			}
			alphaLinkInfo := nativeSupportFileInfo(t, fixture.supportPath)
			alphaCacheInfo := nativeSupportFileInfo(t, alphaCachePath)
			beforeAlphaLink := nativeSupportSnapshot(t, fixture.supportPath, alphaCachePath)
			resolved := append([]resolver.ResolvedArtifact{fixture.hook}, siblingResolved...)
			report, err := Run(context.Background(), fixture.repo, resolved, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeSymlink}))
			if err != nil || report == nil || !hasErrorDiagnostics(report.Diags) {
				t.Fatalf("unproven link plus valid sibling Run: %v / %v", err, report)
			}
			requireNativeHookSupportConflict(t, report)
			blockedAlpha, blockedSibling := false, false
			for _, resource := range report.BlockedResources {
				blockedAlpha = blockedAlpha || resource.Target == hcc.TargetFactory && resource.Name == fixture.hook.Name
				blockedSibling = blockedSibling || resource.Target == hcc.TargetFactory && resource.Name == siblingResolved[0].Name
			}
			if !blockedAlpha || blockedSibling {
				t.Fatalf("blocked resources = %+v, want only the unproven source %q", report.BlockedResources, fixture.hook.Name)
			}
			assertNativeSupportSnapshot(t, beforeAlphaLink, nativeSupportSnapshot(t, fixture.supportPath, alphaCachePath))
			assertNativeSupportSameFile(t, fixture.supportPath, alphaLinkInfo)
			assertNativeSupportSameFile(t, alphaCachePath, alphaCacheInfo)
			if target, err := os.Readlink(fixture.supportPath); err != nil || filepath.Clean(target) != filepath.Clean(alphaCachePath) {
				t.Fatalf("unproven support link = %q / %v, want %q", target, err, alphaCachePath)
			}
			siblingBytes, siblingMode, _ := fileSnapshot(t, siblingPath)
			if !bytes.Equal(siblingBytes, siblingArtifact.Bytes) || siblingMode.Perm() != portableArtifactMode(siblingArtifact.Mode) {
				t.Fatalf("valid sibling copy = %q / %o, want captured bytes/mode", siblingBytes, siblingMode.Perm())
			}
			siblingInfo := nativeSupportFileInfo(t, siblingPath)
			if !siblingInfo.Mode().IsRegular() {
				t.Fatalf("valid sibling support is not a regular copy: %v", siblingInfo.Mode())
			}
			commands := nativeSupportCommands(t, nativeSupportLocationFor(t, hcc.TargetFactory), fixture.nativePath, 2)
			alphaPublished, siblingPublished := false, false
			for _, command := range commands {
				alphaPublished = alphaPublished || strings.HasSuffix(command, "/scripts/check.sh")
				siblingPublished = siblingPublished || strings.HasSuffix(command, "/scripts/sibling.sh")
			}
			if !alphaPublished || !siblingPublished {
				t.Fatalf("Native sibling commands = %v, want prior hook and new independent sibling", commands)
			}
			manifest, err := loadPortableState(fixture.repo, false)
			if err != nil {
				t.Fatal(err)
			}
			alphaRel, err := filepath.Rel(fixture.factoryRoot, fixture.supportPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, owned := manifest.entry(hcc.TargetFactory, filepath.ToSlash(alphaRel)); owned {
				t.Fatal("unproven link acquired a portable manifest claim")
			}
			siblingRel, err := filepath.Rel(fixture.factoryRoot, siblingPath)
			if err != nil {
				t.Fatal(err)
			}
			entry, owned := manifest.entry(hcc.TargetFactory, filepath.ToSlash(siblingRel))
			if !owned || entry.Digest != portableArtifactDigest(siblingArtifact) {
				t.Fatalf("valid sibling manifest entry = %+v, owned=%v", entry, owned)
			}
		})
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
	linkInfo, err := os.Lstat(supportPath)
	if err != nil || !linkInfo.Mode().IsRegular() {
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
	rel, err := filepath.Rel(fixture.factoryRoot, supportPath)
	if err != nil {
		return false
	}
	if _, ok := manifest.entries[portableManifestKey(hcc.TargetFactory, filepath.ToSlash(rel))]; !ok {
		return false
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
		if (diagnostic.Code == "materialize.write.error" || diagnostic.Code == diag.CodeMaterializeHookSupportConflict) && diagnostic.Severity == diag.SeverityError && diagnostic.Target == string(hcc.TargetFactory) {
			return
		}
	}
	t.Fatalf("missing Factory materialize.write.error: %+v", report.Diags)
}

// spec: hook-resources/owned-factory-entries-move-to-the-native-file
func TestNativeHookSupportPublicationFailurePreventsNativeCommit(t *testing.T) {
	const childEnv = "AGENT_MARKETPLACE_NATIVE_HOOK_SUPPORT_FIFO_CHILD"
	const modeEnv = "AGENT_MARKETPLACE_NATIVE_HOOK_SUPPORT_FIFO_MODE"
	const swapEnv = "AGENT_MARKETPLACE_NATIVE_HOOK_SUPPORT_FIFO_KIND"
	if os.Getenv(childEnv) == "1" {
		mode := config.MaterializationMode(os.Getenv(modeEnv))
		runNativeHookSupportLateFIFOSwapCase(t, mode, os.Getenv(swapEnv))
		return
	}
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode)+"/copy preparation failure", func(t *testing.T) {
			fixture := newNativeHookSupportPublicationFixture(t, mode)
			before := snapshotNativeHookSupportPublication(t, fixture)
			var callbackCalled bool
			fault := func(stage, destination string) error {
				if stage == "output" && filepath.Clean(destination) == filepath.Clean(fixture.supportPath) {
					callbackCalled = true
					return errors.New("injected support copy failure")
				}
				return nil
			}
			report, err := Run(context.Background(), fixture.repo, []resolver.ResolvedArtifact{fixture.hook}, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}), withNativeHookSupportWriteFault(fault))
			if err != nil || report == nil {
				t.Fatalf("Run: %v / %v", err, report)
			}
			if !callbackCalled {
				t.Fatal("support copy fault callback was not reached")
			}
			requireNativeHookSupportWriteError(t, report)
			assertNativeHookSupportPublicationFailureState(t, fixture, before)
			assertPathAbsent(t, fixture.supportPath)
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
			supportLinkInfo, err := os.Lstat(fixture.supportPath)
			if err != nil || !supportLinkInfo.Mode().IsRegular() {
				t.Fatalf("repository support is not a regular copy: %v / %v", supportLinkInfo, err)
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
			rel, err := filepath.Rel(fixture.factoryRoot, fixture.supportPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := manifest.entries[portableManifestKey(hcc.TargetFactory, filepath.ToSlash(rel))]; !ok {
				t.Fatal("repository support copy was not recorded in the portable manifest")
			}
		})
	}
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		for _, swap := range []string{"direct_fifo", "foreign_symlink_to_fifo"} {
			t.Run(string(mode)+"/late_"+swap+"_verification", func(t *testing.T) {
				childTempRoot := t.TempDir()
				t.Setenv("TMPDIR", childTempRoot)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeHookSupportPublicationFailurePreventsNativeCommit$", "-test.v")
				cmd.Env = append(os.Environ(), childEnv+"=1", modeEnv+"="+string(mode), swapEnv+"="+swap)
				output, err := cmd.CombinedOutput()
				if ctx.Err() == context.DeadlineExceeded {
					t.Fatalf("bounded %s verifier child timed out on %s: %v\n%s", mode, swap, err, output)
				}
				if err != nil {
					t.Fatalf("bounded %s verifier child on %s: %v\n%s", mode, swap, err, output)
				}
			})
		}
	}
}

func runNativeHookSupportLateFIFOSwapCase(t *testing.T, mode config.MaterializationMode, swapKind string) {
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
	var outputInfo, foreignFIFOInfo os.FileInfo
	var outputRoots []string
	var foreignFIFO string
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
		if err := os.Remove(destination); err != nil {
			return err
		}
		switch swapKind {
		case "direct_fifo":
			if err := syscall.Mkfifo(destination, 0o600); err != nil {
				return err
			}
			outputRoots = []string{destination}
		case "foreign_symlink_to_fifo":
			foreignFIFO = filepath.Join(t.TempDir(), "foreign.fifo")
			if err := syscall.Mkfifo(foreignFIFO, 0o600); err != nil {
				return err
			}
			if err := os.Symlink(foreignFIFO, destination); err != nil {
				return err
			}
			outputRoots = []string{destination, foreignFIFO}
		default:
			return fmt.Errorf("unknown late support swap %q", swapKind)
		}
		outputInfo, err = os.Lstat(destination)
		if err != nil {
			return err
		}
		if foreignFIFO != "" {
			foreignFIFOInfo, err = os.Lstat(foreignFIFO)
			if err != nil {
				return err
			}
		}
		outputBefore = nativeSupportSnapshot(t, outputRoots...)
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
	assertNativeSupportSnapshot(t, outputBefore, nativeSupportSnapshot(t, outputRoots...))
	if !os.SameFile(outputInfo, nativeSupportFileInfo(t, fixture.supportPath)) {
		t.Fatal("late support swap changed the destination inode after refusal")
	}
	manifestAfter, err := loadPortableState(fixture.repo, false)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := manifestAfter.entries[oldManifestKey]; !ok || got != oldManifestEntry {
		t.Fatalf("old support manifest entry changed or disappeared: %+v", got)
	}
	info, err := os.Lstat(fixture.supportPath)
	if err != nil {
		t.Fatalf("late support destination disappeared: %v", err)
	}
	switch swapKind {
	case "direct_fifo":
		if info.Mode()&os.ModeNamedPipe == 0 || info.Mode().Perm() != 0o600 {
			t.Fatalf("repository support FIFO was not preserved: %v", info.Mode())
		}
	case "foreign_symlink_to_fifo":
		if info.Mode()&os.ModeSymlink == 0 || !os.SameFile(foreignFIFOInfo, nativeSupportFileInfo(t, foreignFIFO)) {
			t.Fatalf("foreign FIFO referent was not preserved: link=%v referent=%v", info.Mode(), foreignFIFOInfo)
		}
		if target, err := os.Readlink(fixture.supportPath); err != nil || filepath.Clean(target) != filepath.Clean(foreignFIFO) {
			t.Fatalf("foreign support symlink = %q / %v, want %q", target, err, foreignFIFO)
		}
	default:
		t.Fatalf("unknown late support swap %q", swapKind)
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
		matches, err := filepath.Glob(filepath.Join(repo, ".factory", "hook-support", "*", "*", "hooks", "check.sh"))
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
