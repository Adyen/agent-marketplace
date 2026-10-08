package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func additiveSafetyEnv(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	return t.TempDir(), home
}

func additiveSafetyWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func additiveSafetyProvider(t *testing.T, target hcc.Target, machine bool, command, script string) resolver.ResolvedArtifact {
	t.Helper()
	return additiveSafetyProviderInRegistry(t, t.TempDir(), "", target, machine, command, script)
}

func additiveSafetyProviderWithTimeout(t *testing.T, target hcc.Target, machine bool, command, script string, timeout int) resolver.ResolvedArtifact {
	t.Helper()
	return additiveSafetyProviderInRegistryWithTimeout(t, t.TempDir(), "", target, machine, command, script, &timeout)
}

func additiveSafetyProviderInRegistry(t *testing.T, registryRoot, sourceSubdir string, target hcc.Target, machine bool, command, script string) resolver.ResolvedArtifact {
	t.Helper()
	return additiveSafetyProviderInRegistryWithTimeout(t, registryRoot, sourceSubdir, target, machine, command, script, nil)
}

func additiveSafetyProviderInRegistryWithTimeout(t *testing.T, registryRoot, sourceSubdir string, target hcc.Target, machine bool, command, script string, timeout *int) resolver.ResolvedArtifact {
	t.Helper()
	sourceRoot := registryRoot
	if sourceSubdir != "" {
		sourceRoot = filepath.Join(registryRoot, filepath.FromSlash(sourceSubdir))
	}
	handler := map[string]any{"type": "command", "command": command}
	if timeout != nil {
		handler["timeout"] = *timeout
	}
	definition, err := json.Marshal(map[string]any{"PreToolUse": []any{map[string]any{
		"matcher": "Execute",
		"hooks":   []any{handler},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	additiveSafetyWrite(t, filepath.Join(sourceRoot, "hooks", "hooks.json"), string(definition))
	if script != "" {
		additiveSafetyWrite(t, filepath.Join(sourceRoot, "hooks", "shared.sh"), script)
	}
	cat, err := catalog.Load(sourceRoot, "factory")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Entries) != 1 {
		t.Fatalf("expected one source hook, got %v", cat.Entries)
	}
	entry := cat.Entries[0]
	entry.Name = "guard"
	if _, ok := entry.Resource.(*ir.Hook); !ok {
		t.Fatal("source entry is not a hook")
	}
	source := config.RepoCommitted
	if machine {
		source = config.UserPersonal
	}
	return resolver.ResolvedArtifact{
		Entry:       entry,
		RegistryURL: registryRoot,
		CatalogKey:  registryRoot,
		Source:      source,
		Harnesses:   []hcc.Target{target},
	}
}

func additiveSafetyCommands(t *testing.T, file, container, event string) []string {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if container != "" {
		var ok bool
		document, ok = document[container].(map[string]any)
		if !ok {
			t.Fatalf("missing container %q in %s", container, data)
		}
	}
	rules, ok := document[event].([]any)
	if !ok {
		t.Fatalf("missing event array %q in %s", event, data)
	}
	var commands []string
	for _, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("invalid rule: %#v", raw)
		}
		if command, ok := rule["command"].(string); ok {
			commands = append(commands, command)
		}
		if nested, ok := rule["hooks"].([]any); ok {
			for _, handler := range nested {
				value, ok := handler.(map[string]any)
				if !ok {
					t.Fatalf("invalid handler: %#v", handler)
				}
				command, ok := value["command"].(string)
				if !ok {
					t.Fatalf("missing command: %#v", value)
				}
				commands = append(commands, command)
			}
		}
	}
	return commands
}

func additiveSafetyNativeEntry(target hcc.Target, grouped bool, matcher, command string) string {
	if grouped {
		return fmt.Sprintf(`{ "matcher" : %q , "hooks" : [ { "type" : "command" , "command" : %q } ] }`, matcher, command)
	}
	fields := make([]string, 0, 3)
	if matcher != "" {
		fields = append(fields, fmt.Sprintf(`"matcher" : %q`, matcher))
	}
	if target == hcc.TargetCursor {
		fields = append(fields, `"type" : "command"`)
	}
	fields = append(fields, fmt.Sprintf(`"command" : %q`, command))
	return "{ " + strings.Join(fields, " , ") + " }"
}

func additiveSafetyNativeSeed(target hcc.Target, container, event string, grouped bool, matcher string, version int) ([]byte, []string) {
	manual := []string{
		additiveSafetyNativeEntry(target, grouped, matcher, "echo handwritten-first"),
		additiveSafetyNativeEntry(target, grouped, matcher, "echo handwritten-second"),
	}
	eventEntries := fmt.Sprintf("%q : [\n      %s,\n      %s\n    ]", event, manual[0], manual[1])
	member := eventEntries
	if container != "" {
		member = fmt.Sprintf("%q : {\n    %s\n  }", container, eventEntries)
	}
	fields := make([]string, 0, 3)
	if version != 0 {
		fields = append(fields, fmt.Sprintf(`"version" : %d`, version))
	}
	if target == hcc.TargetClaude {
		fields = append(fields, `"permissions" : { "allow" : ["Bash"] }`)
	}
	if target == hcc.TargetGemini {
		fields = append(fields, `"mcpServers" : { "handwritten" : { "command" : "keep" } }`)
	}
	fields = append(fields, member)
	return []byte("{\n  " + strings.Join(fields, ",\n  ") + "\n}\n"), manual
}

// spec: hook-resources/every-harness-target-receives-hooks-in-its-native-file
// spec: hook-resources/new-entries-carry-no-in-file-marker
// spec: hook-resources/repeat-sync-is-byte-identical
// spec: hook-resources/deselected-hook-is-removed-from-the-merged-file
func TestAdditiveHooksAllTargetsScopesAndModes(t *testing.T) {
	targets := []struct {
		target                               hcc.Target
		repoPath, userPath, container, event string
		matcher                              string
		grouped                              bool
		version                              int
		legacyRepo, legacyUser               string
	}{
		{target: hcc.TargetFactory, repoPath: ".factory/hooks.json", userPath: ".factory/hooks.json", event: "PreToolUse", matcher: "Execute", grouped: true, legacyRepo: ".factory/hooks/hooks.json", legacyUser: ".factory/hooks/hooks.json"},
		{target: hcc.TargetClaude, repoPath: ".claude/settings.json", userPath: ".claude/settings.json", container: "hooks", event: "PreToolUse", matcher: "Bash", grouped: true, legacyRepo: ".claude/hooks/hooks.json", legacyUser: ".claude/hooks/hooks.json"},
		{target: hcc.TargetCursor, repoPath: ".cursor/hooks.json", userPath: ".cursor/hooks.json", container: "hooks", event: "preToolUse", matcher: "^Shell$", version: 1, legacyRepo: ".cursor/hooks/hooks.json", legacyUser: ".cursor/hooks/hooks.json"},
		{target: hcc.TargetGemini, repoPath: ".gemini/settings.json", userPath: ".gemini/settings.json", container: "hooks", event: "BeforeTool", matcher: "^run_shell_command$", grouped: true},
		{target: hcc.TargetDevin, repoPath: ".devin/hooks.v1.json", userPath: ".config/devin/config.json", event: "PreToolUse", matcher: "^exec$", grouped: true},
		{target: hcc.TargetCodex, repoPath: ".codex/hooks.json", userPath: ".codex/hooks.json", container: "hooks", event: "PreToolUse", matcher: "^Bash$", grouped: true},
		{target: hcc.TargetWindsurf, repoPath: ".devin/hooks.json", userPath: ".codeium/windsurf/hooks.json", container: "hooks", event: "pre_run_command", legacyRepo: ".windsurf/hooks.json"},
	}
	for _, target := range targets {
		for _, machine := range []bool{false, true} {
			for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
				t.Run(fmt.Sprintf("%s/machine=%t/%s", target.target, machine, mode), func(t *testing.T) {
					repo, home := additiveSafetyEnv(t)
					providers := []resolver.ResolvedArtifact{
						additiveSafetyProvider(t, target.target, machine, "echo first", ""),
						additiveSafetyProvider(t, target.target, machine, "echo second", ""),
					}
					cfg := &config.Config{}
					catalogs := map[string][]catalog.Entry{}
					for _, provider := range providers {
						cfg.Registries = append(cfg.Registries, config.RegistryEntry{URL: provider.RegistryURL, CatalogKey: provider.CatalogKey, Source: provider.Source, Default: true})
						catalogs[provider.CatalogKey] = []catalog.Entry{provider.Entry}
					}
					resolved, errs := resolver.Resolve(cfg, catalogs, nil, resolver.WithTargetHarnesses([]hcc.Target{target.target}))
					if len(errs) != 0 || len(resolved) != 2 {
						t.Fatalf("Resolve: %v, %v", resolved, errs)
					}
					file, container := filepath.Join(repo, target.repoPath), target.container
					legacyRoot, legacy := repo, target.legacyRepo
					if machine {
						file = filepath.Join(home, target.userPath)
						legacyRoot, legacy = home, target.legacyUser
					}
					if machine && target.target == hcc.TargetDevin {
						container = "hooks"
					}
					seed, manual := additiveSafetyNativeSeed(target.target, container, target.event, target.grouped, target.matcher, target.version)
					additiveSafetyWrite(t, file, string(seed))
					if err := os.Chmod(file, 0o640); err != nil {
						t.Fatal(err)
					}
					opts := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
					report, err := Run(context.Background(), repo, resolved, opts...)
					if err != nil {
						t.Fatal(err)
					}
					if hasErrorDiagnostics(report.Diags) {
						t.Fatal(report.Diags)
					}
					want := []string{"echo handwritten-first", "echo handwritten-second", "echo first", "echo second"}
					if got := additiveSafetyCommands(t, file, container, target.event); !reflect.DeepEqual(got, want) {
						t.Fatalf("commands = %v, want %v", got, want)
					}
					before, modeBefore, mtimeBefore := fileSnapshot(t, file)
					if modeBefore.Perm() != 0o640 {
						t.Fatalf("native file mode = %o, want preserved 0640", modeBefore.Perm())
					}
					if bytes.Contains(before, []byte("_managed_by")) {
						t.Fatalf("native entries contain an in-file marker: %s", before)
					}
					firstPos, secondPos := bytes.Index(before, []byte(manual[0])), bytes.Index(before, []byte(manual[1]))
					if firstPos < 0 || secondPos <= firstPos {
						t.Fatalf("handwritten entries changed order or bytes: %s", before)
					}
					if legacy != "" {
						if _, err := os.Lstat(filepath.Join(legacyRoot, legacy)); !os.IsNotExist(err) {
							t.Fatalf("plugin-layout output was written at %s: %v", legacy, err)
						}
					}
					report, err = Run(context.Background(), repo, resolved, opts...)
					if err != nil || hasErrorDiagnostics(report.Diags) {
						t.Fatalf("repeat sync: %v / %v", err, report)
					}
					assertFileUnchanged(t, file, before, modeBefore, mtimeBefore)
					report, err = Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved[0]}, opts...)
					if err != nil || hasErrorDiagnostics(report.Diags) {
						t.Fatalf("deselect sync: %v / %v", err, report)
					}
					want = []string{"echo handwritten-first", "echo handwritten-second", "echo first"}
					if got := additiveSafetyCommands(t, file, container, target.event); !reflect.DeepEqual(got, want) {
						t.Fatalf("commands after deselection = %v, want %v", got, want)
					}
				})
			}
		}
	}
}

func TestAdditiveHookSupportConflictPreservesInstalledBytes(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetFactory, hcc.TargetWindsurf} {
		for _, machine := range []bool{false, true} {
			for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
				t.Run(fmt.Sprintf("%s/machine=%t/%s", target, machine, mode), func(t *testing.T) {
					repo, home := additiveSafetyEnv(t)
					registryRoot := t.TempDir()
					first := additiveSafetyProviderInRegistry(t, registryRoot, "first", target, machine, "echo first", "echo first-script")
					second := additiveSafetyProviderInRegistry(t, registryRoot, "second", target, machine, "echo second", "echo second-script")
					// Both hooks share one configured source identity and
					// installed name, so their differing shared.sh bytes
					// claim the same namespaced support path.
					root, native := filepath.Join(repo, ".factory"), "hooks.json"
					if machine {
						root = filepath.Join(home, ".factory")
					}
					supportRoot := filepath.Join(root, "hook-support")
					native = filepath.Join(root, native)
					if target == hcc.TargetWindsurf {
						if machine {
							root = filepath.Join(home, ".codeium", "windsurf")
							native = filepath.Join(root, "hooks.json")
							supportRoot = filepath.Join(root, "hook-support")
						} else {
							native = filepath.Join(repo, ".devin", "hooks.json")
							supportRoot = filepath.Join(repo, "hook-support")
						}
					}
					supportOf := func(t *testing.T) string {
						t.Helper()
						matches, err := filepath.Glob(filepath.Join(supportRoot, "*", "*", "shared.sh"))
						if err != nil || len(matches) != 1 {
							t.Fatalf("installed support script under %s: %v %v", supportRoot, matches, err)
						}
						return matches[0]
					}
					opts := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
					report, _ := Run(context.Background(), repo, []resolver.ResolvedArtifact{first, second}, opts...)
					assertSupportConflictDiagnostics(t, report)
					for _, file := range []string{native} {
						if _, err := os.Lstat(file); !os.IsNotExist(err) {
							t.Fatalf("conflicting fresh output exists at %s: %v", file, err)
						}
					}
					if matches, _ := filepath.Glob(filepath.Join(supportRoot, "*", "*", "shared.sh")); len(matches) != 0 {
						t.Fatalf("conflicting support tree installed: %v", matches)
					}
					if report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{first}, opts...); err != nil || hasErrorDiagnostics(report.Diags) {
						t.Fatalf("seed: %v, %v", err, report)
					}
					support := supportOf(t)
					nativeBefore, err := os.ReadFile(native)
					if err != nil {
						t.Fatal(err)
					}
					supportBefore, err := os.ReadFile(support)
					if err != nil {
						t.Fatal(err)
					}
					if string(supportBefore) != "echo first-script" {
						t.Fatalf("unexpected seeded script: %s", supportBefore)
					}
					report, _ = Run(context.Background(), repo, []resolver.ResolvedArtifact{first, second}, opts...)
					assertSupportConflictDiagnostics(t, report)
					for file, before := range map[string][]byte{native: nativeBefore, support: supportBefore} {
						after, err := os.ReadFile(file)
						if err != nil || !bytes.Equal(before, after) {
							t.Fatalf("conflict changed installed %s: %v\nbefore %s\nafter %s", file, err, before, after)
						}
					}
					container, event := "", "PreToolUse"
					if target == hcc.TargetWindsurf {
						container, event = "hooks", "pre_run_command"
					}
					prior := additiveSafetyCommands(t, native, container, event)
					if len(prior) != 1 {
						t.Fatalf("expected one seeded handler, got %v", prior)
					}
					independent := additiveSafetyProvider(t, target, machine, "echo independent", "")
					report, _ = Run(context.Background(), repo, []resolver.ResolvedArtifact{first, second, independent}, opts...)
					assertSupportConflictDiagnostics(t, report)
					got := additiveSafetyCommands(t, native, container, event)
					if !reflect.DeepEqual(got, []string{prior[0], "echo independent"}) {
						t.Fatalf("mixed valid/blocked handlers = %v", got)
					}
					after, err := os.ReadFile(support)
					if err != nil || !bytes.Equal(supportBefore, after) {
						t.Fatalf("mixed sync changed blocked script: %v", err)
					}
				})
			}
		}
	}
}

func assertSupportConflictDiagnostics(t *testing.T, report *Report) {
	t.Helper()
	if report == nil {
		t.Fatal("missing conflict report")
	}
	count := 0
	for _, diagnostic := range report.Diags {
		if diagnostic.Code == diag.CodeMaterializeHookSupportConflict && diagnostic.Severity == diag.SeverityError {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected both conflicting hook producers to be refused, got %v", report.Diags)
	}
}

// spec: hook-resources/malformed-legacy-files-block-migration-without-changes
func TestWindsurfMalformedHooksPreserveBytes(t *testing.T) {
	for _, body := range []string{`{"hooks":null}`, `{"hooks":[]}`, `{"hooks":{"pre_run_command":{}}}`, `{"hooks":{"pre_run_command":["invalid"]}}`} {
		for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
			t.Run(fmt.Sprintf("%s/%s", body, mode), func(t *testing.T) {
				repo, home := additiveSafetyEnv(t)
				legacyFile := filepath.Join(repo, ".windsurf", "hooks.json")
				nativeFile := filepath.Join(repo, ".devin", "hooks.json")
				provider := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo wanted", "#!/bin/sh\necho old support bytes\n")
				sourceSupport := filepath.Join(provider.RegistryURL, "hooks", "shared.sh")
				if err := os.Chmod(sourceSupport, 0o755); err != nil {
					t.Fatal(err)
				}
				_, supportPath := nativeHookActivationSupportPath(t, provider, hcc.TargetWindsurf, false, repo)
				opts := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
				report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider}, opts...)
				if err != nil || hasErrorDiagnostics(report.Diags) {
					t.Fatalf("seed native hook and support: %v / %v", err, report)
				}
				if _, err := os.Lstat(nativeFile); err != nil {
					t.Fatalf("native support control missing: %v", err)
				}
				supportBefore := nativeHookActivationSupportSnapshotAt(t, supportPath)
				if err := os.WriteFile(sourceSupport, []byte("#!/bin/sh\necho new support bytes\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				additiveSafetyWrite(t, legacyFile, body)
				legacyBefore, legacyMode, legacyMtime := fileSnapshot(t, legacyFile)
				nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativeFile)
				nativeStore := nativeHookMetadataStore(t, repo)
				stateBefore, stateMode, stateMtime := fileSnapshot(t, nativeStore.StateFile())
				repoBefore := satisfactionPathTree(t, repo)
				stateTreeBefore := satisfactionPathTree(t, filepath.Join(home, "state"))
				report, err = Run(context.Background(), repo, []resolver.ResolvedArtifact{provider}, opts...)
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				if !diag.HasCode(report.Diags, diag.CodeMaterializeHookLegacyMalformed) {
					t.Fatalf("missing legacy-malformed diagnostic: %v", report.Diags)
				}
				assertFileUnchanged(t, legacyFile, legacyBefore, legacyMode, legacyMtime)
				assertFileUnchanged(t, nativeFile, nativeBefore, nativeMode, nativeMtime)
				assertFileUnchanged(t, nativeStore.StateFile(), stateBefore, stateMode, stateMtime)
				assertNativeHookActivationSupportSnapshot(t, supportPath, supportBefore)
				if !reflect.DeepEqual(repoBefore, satisfactionPathTree(t, repo)) || !reflect.DeepEqual(stateTreeBefore, satisfactionPathTree(t, filepath.Join(home, "state"))) {
					t.Fatal("malformed migration left filesystem or state residue")
				}
			})
		}
	}
}

func TestWindsurfIncompleteHooksRetainManagedSiblings(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	legacyFile := filepath.Join(repo, ".windsurf", "hooks.json")
	nativeFile := filepath.Join(repo, ".devin", "hooks.json")
	initial := "{\n// keep this comment\n\"unrelated\":true,\"hooks\":{\"pre_run_command\":[{\"command\":\"echo handwritten\"}]}}"
	additiveSafetyWrite(t, legacyFile, initial)
	legacyBefore, legacyMode, legacyMtime := fileSnapshot(t, legacyFile)
	old := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo old", "")
	if report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{old}); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed native hook: %v / %v", err, report)
	}
	invalid := additiveSafetyProviderWithTimeout(t, hcc.TargetWindsurf, false, "echo invalid", "", 30)
	valid := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo new", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{invalid, valid})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, diagnostic := range report.Diags {
		if diagnostic.Code == diag.CodeHookWindsurfTimeoutUnsupported && diagnostic.Severity == diag.SeverityError {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing timeout refusal: %v", report.Diags)
	}
	assertFileUnchanged(t, legacyFile, legacyBefore, legacyMode, legacyMtime)
	if got := additiveSafetyCommands(t, nativeFile, "hooks", "pre_run_command"); !reflect.DeepEqual(got, []string{"echo old", "echo new"}) {
		t.Fatalf("incomplete native output = %v", got)
	}
	if data, err := os.ReadFile(nativeFile); err != nil || bytes.Contains(data, []byte("_managed_by")) {
		t.Fatalf("native hook marker/output error: %v / %s", err, data)
	}
}

// spec: hook-resources/legacy-windsurf-cache-links-remain-preserved
func TestWindsurfLegacyCacheLinksArePreserved(t *testing.T) {
	for _, machine := range []bool{false, true} {
		for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
			t.Run(fmt.Sprintf("machine=%t/%s", machine, mode), func(t *testing.T) {
				repo, home := additiveSafetyEnv(t)
				cacheTarget := filepath.Join(cache.CacheDir(), "registry", "pre_run_command.json")
				additiveSafetyWrite(t, cacheTarget, `{"command":"echo old-cached"}`)
				oldDir := filepath.Join(repo, ".windsurf", "hooks")
				staleDir := filepath.Join(repo, ".windsurf", "workflows")
				native := filepath.Join(repo, ".devin", "hooks.json")
				if machine {
					oldDir = filepath.Join(home, ".codeium", "windsurf", "hooks")
					staleDir = filepath.Join(home, ".codeium", "windsurf", "global_workflows")
					native = filepath.Join(home, ".codeium", "windsurf", "hooks.json")
				}
				for _, dir := range []string{oldDir, staleDir} {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				for _, event := range []string{"pre_run_command", "pre_user_prompt"} {
					if err := os.Symlink(cacheTarget, filepath.Join(oldDir, event+".json")); err != nil {
						t.Fatal(err)
					}
				}
				additiveSafetyWrite(t, filepath.Join(oldDir, "handwritten.txt"), "handwritten old hook notes")
				stale := filepath.Join(staleDir, "stale.json")
				if err := os.Symlink(cacheTarget, stale); err != nil {
					t.Fatal(err)
				}
				provider := additiveSafetyProvider(t, hcc.TargetWindsurf, machine, "echo wanted", "")
				opts := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
				seedNativeHookJSONLock(t, filepath.Dir(native))
				before := nativeSupportSnapshot(t, oldDir)
				stateBefore := nativeSupportPersistentSnapshot(t, repo, home)
				preview, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider}, append(opts, WithDryRun())...)
				if err != nil || hasErrorDiagnostics(preview.Diags) {
					t.Fatalf("dry run: %v / %v", err, preview)
				}
				if !diag.HasCode(preview.Diags, diag.CodeMaterializeHookReviewPending) {
					t.Fatalf("dry-run diagnostics = %v, want %s", preview.Diags, diag.CodeMaterializeHookReviewPending)
				}
				base := repo
				if machine {
					base = home
				}
				nativeRel, err := filepath.Rel(base, native)
				if err != nil {
					t.Fatal(err)
				}
				wantFile := filepath.Join(evalPath(t, base), nativeRel)
				additions := 0
				for _, change := range preview.HookChanges {
					if change.Target == hcc.TargetWindsurf && change.File == wantFile && change.After != nil &&
						bytes.Contains(change.After, []byte(`"command":"echo wanted"`)) {
						additions++
					}
				}
				if additions != 1 {
					t.Fatalf("dry-run pending native additions = %d, want 1: %+v", additions, preview.HookChanges)
				}
				assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, oldDir))
				assertPathAbsent(t, native)
				if _, err := os.Lstat(stale); err != nil {
					t.Fatalf("dry run touched stale control: %v", err)
				}
				assertNativeSupportSnapshot(t, stateBefore, nativeSupportPersistentSnapshot(t, repo, home))
				report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider}, opts...)
				if err != nil || hasErrorDiagnostics(report.Diags) {
					t.Fatalf("sync: %v / %v", err, report)
				}
				assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, oldDir))
				if _, err := os.Lstat(stale); !os.IsNotExist(err) {
					t.Fatalf("stale cache link outside preserved dirs survived: %v", err)
				}
				if got := additiveSafetyCommands(t, native, "hooks", "pre_run_command"); !reflect.DeepEqual(got, []string{"echo wanted"}) {
					t.Fatalf("native commands = %v", got)
				}
				if data, err := os.ReadFile(native); err != nil || bytes.Contains(data, []byte("_managed_by")) {
					t.Fatalf("native hook marker/output error: %v / %s", err, data)
				}
				nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, native)
				report, err = Run(context.Background(), repo, []resolver.ResolvedArtifact{provider}, opts...)
				if err != nil || hasErrorDiagnostics(report.Diags) {
					t.Fatalf("repeat sync: %v / %v", err, report)
				}
				if len(report.HookChanges) != 0 {
					t.Fatalf("repeat sync reported hook changes: %+v", report.HookChanges)
				}
				assertFileUnchanged(t, native, nativeBefore, nativeMode, nativeMtime)
				assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, oldDir))
				if report, err := Run(context.Background(), repo, nil, opts...); err != nil || hasErrorDiagnostics(report.Diags) {
					t.Fatalf("deselect sync: %v / %v", err, report)
				}
				assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, oldDir))
			})
		}
	}
}

func TestNativeHookLegacySupportDirsExcludePreservedDirs(t *testing.T) {
	for _, tc := range []struct {
		target  hcc.Target
		machine bool
		want    []string
	}{
		{hcc.TargetWindsurf, false, nil},
		{hcc.TargetWindsurf, true, nil},
		{hcc.TargetClaude, false, []string{"hooks"}},
		{hcc.TargetClaude, true, []string{"hooks"}},
		{hcc.TargetFactory, false, []string{"hooks"}},
		{hcc.TargetFactory, true, []string{"hooks"}},
	} {
		t.Run(fmt.Sprintf("%s/machine=%t", tc.target, tc.machine), func(t *testing.T) {
			key := outputKey{target: tc.target, machineScope: tc.machine}
			declaration, err := nativeHookAggregateForScope(targetmeta.Target(tc.target), tc.machine)
			if err != nil {
				t.Fatal(err)
			}
			if got := nativeHookLegacySupportDirs(key, declaration); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("legacy support dirs = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWindsurfLegacyPortableOwnershipIsPreserved(t *testing.T) {
	for _, machine := range []bool{false, true} {
		t.Run(fmt.Sprint(machine), func(t *testing.T) {
			repo, home := additiveSafetyEnv(t)
			root, legacyRel, staleRel := repo, ".windsurf/hooks/pre_run_command.json", ".windsurf/hook-support/stale.sh"
			if machine {
				root, legacyRel, staleRel = filepath.Join(home, ".codeium", "windsurf"), "hooks/pre_run_command.json", "hook-support/stale.sh"
			}
			legacy, stale := filepath.Join(root, legacyRel), filepath.Join(root, staleRel)
			additiveSafetyWrite(t, legacy, `{"command":"echo legacy","_managed_by":"agent-marketplace"}`)
			additiveSafetyWrite(t, stale, "echo stale")
			state, err := loadPortableState(repo, machine)
			if err != nil {
				t.Fatal(err)
			}
			for _, rel := range []string{legacyRel, staleRel} {
				digest, err := portablePathDigest(filepath.Join(root, rel))
				if err != nil {
					t.Fatal(err)
				}
				state.add(hcc.TargetWindsurf, rel, digest)
			}
			if err := state.save(false); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(legacy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Run(context.Background(), repo, nil, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile})); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(legacy)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("owned legacy bytes changed: %v, %s", err, after)
			}
			if _, err := os.Stat(stale); !os.IsNotExist(err) {
				t.Fatalf("positive stale prune control survived: %v", err)
			}
			state, err = loadPortableState(repo, machine)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := state.entry(hcc.TargetWindsurf, legacyRel); !ok {
				t.Fatal("legacy ownership record disappeared")
			}
		})
	}
}
