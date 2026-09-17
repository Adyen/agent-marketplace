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

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
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
	root := t.TempDir()
	additiveSafetyWrite(t, filepath.Join(root, "hooks", "hooks.json"), fmt.Sprintf(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":%q}]}]}`, command))
	if script != "" {
		additiveSafetyWrite(t, filepath.Join(root, "hooks", "shared.sh"), script)
	}
	cat, err := catalog.Load(root, "factory")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Entries) != 1 {
		t.Fatalf("expected one source hook, got %v", cat.Entries)
	}
	source := config.RepoCommitted
	if machine {
		source = config.UserPersonal
	}
	return resolver.ResolvedArtifact{Entry: cat.Entries[0], RegistryURL: root, CatalogKey: root, Source: source, Harnesses: []hcc.Target{target}}
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

func TestAdditiveHooksAllTargetsScopesAndModes(t *testing.T) {
	targets := []struct {
		target                               hcc.Target
		repoPath, userPath, container, event string
	}{
		{hcc.TargetFactory, ".factory/hooks/hooks.json", ".factory/hooks/hooks.json", "", "PreToolUse"},
		{hcc.TargetClaude, ".claude/hooks/hooks.json", ".claude/hooks/hooks.json", "hooks", "PreToolUse"},
		{hcc.TargetCursor, ".cursor/hooks/hooks.json", ".cursor/hooks/hooks.json", "hooks", "PreToolUse"},
		{hcc.TargetGemini, ".gemini/settings.json", ".gemini/settings.json", "hooks", "BeforeTool"},
		{hcc.TargetDevin, ".devin/hooks.v1.json", ".config/devin/config.json", "", "PreToolUse"},
		{hcc.Target("codex"), ".codex/hooks.json", ".codex/hooks.json", "hooks", "PreToolUse"},
		{hcc.TargetWindsurf, ".windsurf/hooks.json", ".codeium/windsurf/hooks.json", "hooks", "pre_run_command"},
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
					opts := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
					report, err := Run(context.Background(), repo, resolved, opts...)
					if err != nil {
						t.Fatal(err)
					}
					if hasErrorDiagnostics(report.Diags) {
						t.Fatal(report.Diags)
					}
					file, container := filepath.Join(repo, target.repoPath), target.container
					if machine {
						file = filepath.Join(home, target.userPath)
					}
					if machine && target.target == hcc.TargetDevin {
						container = "hooks"
					}
					if got := additiveSafetyCommands(t, file, container, target.event); !reflect.DeepEqual(got, []string{"echo first", "echo second"}) {
						t.Fatalf("commands = %v", got)
					}
					before, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := Run(context.Background(), repo, resolved, opts...); err != nil {
						t.Fatal(err)
					}
					after, err := os.ReadFile(file)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatalf("non-idempotent repeat sync: %v", err)
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
					first := additiveSafetyProvider(t, target, machine, "${CLAUDE_PLUGIN_ROOT}/hooks/shared.sh", "echo first-script")
					second := additiveSafetyProvider(t, target, machine, "${CLAUDE_PLUGIN_ROOT}/hooks/shared.sh", "echo second-script")
					root, native, support := filepath.Join(repo, ".factory"), "hooks/hooks.json", "hooks/shared.sh"
					if machine {
						root = filepath.Join(home, ".factory")
					}
					if target == hcc.TargetWindsurf {
						root, native, support = filepath.Join(repo, ".windsurf"), "hooks.json", "hook-support/shared.sh"
						if machine {
							root = filepath.Join(home, ".codeium", "windsurf")
						}
					}
					native, support = filepath.Join(root, native), filepath.Join(root, support)
					opts := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
					report, _ := Run(context.Background(), repo, []resolver.ResolvedArtifact{first, second}, opts...)
					assertSupportConflictDiagnostics(t, report)
					for _, file := range []string{native, support} {
						if _, err := os.Lstat(file); !os.IsNotExist(err) {
							t.Fatalf("conflicting fresh output exists at %s: %v", file, err)
						}
					}
					if report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{first}, opts...); err != nil || hasErrorDiagnostics(report.Diags) {
						t.Fatalf("seed: %v, %v", err, report)
					}
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

func TestWindsurfMalformedHooksPreserveBytes(t *testing.T) {
	for _, body := range []string{`{"hooks":null}`, `{"hooks":[]}`, `{"hooks":{"pre_run_command":{}}}`, `{"hooks":{"pre_run_command":["invalid"]}}`} {
		t.Run(body, func(t *testing.T) {
			repo, _ := additiveSafetyEnv(t)
			file := filepath.Join(repo, ".windsurf", "hooks.json")
			additiveSafetyWrite(t, file, body)
			provider := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo wanted", "")
			report, _ := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
			if !hasErrorDiagnostics(report.Diags) {
				t.Fatalf("expected refusal: %v", report.Diags)
			}
			after, err := os.ReadFile(file)
			if err != nil || string(after) != body {
				t.Fatalf("malformed file changed: %s, %v", after, err)
			}
		})
	}
}

func TestWindsurfIncompleteHooksRetainManagedSiblings(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	file := filepath.Join(repo, ".windsurf", "hooks.json")
	initial := "{\n// keep this comment\n\"unrelated\":true,\"hooks\":{\"pre_run_command\":[{\"command\":\"echo handwritten\"}]}}"
	additiveSafetyWrite(t, file, initial)
	old := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo old", "")
	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{old}); err != nil {
		t.Fatal(err)
	}
	invalid := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo invalid", "")
	timeout := 30
	invalid.Resource.(*ir.Hook).Timeout = &timeout
	valid := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo new", "")
	report, _ := Run(context.Background(), repo, []resolver.ResolvedArtifact{invalid, valid})
	found := false
	for _, diagnostic := range report.Diags {
		if diagnostic.Code == diag.CodeHookWindsurfTimeoutUnsupported && diagnostic.Severity == diag.SeverityError {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing timeout refusal: %v", report.Diags)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "// keep this comment") || !strings.Contains(string(data), `"unrelated":true`) {
		t.Fatalf("handwritten bytes lost: %s", data)
	}
	clean := strings.ReplaceAll(string(data), "// keep this comment", "")
	var document struct {
		Hooks map[string][]struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(clean), &document); err != nil {
		t.Fatal(err)
	}
	commands := map[string]int{}
	for _, hook := range document.Hooks["pre_run_command"] {
		commands[hook.Command]++
	}
	if !reflect.DeepEqual(commands, map[string]int{"echo handwritten": 1, "echo old": 1, "echo new": 1}) {
		t.Fatalf("incomplete output = %v", commands)
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
