package harnesscompile

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: hook-resources/every-harness-target-receives-hooks-in-its-native-file
// spec: hook-resources/new-entries-carry-no-in-file-marker
func TestNativeHookOutputAllTargetsAndScopes(t *testing.T) {
	tests := []struct {
		target                 Target
		projectPath, userPath  string
		member, event, matcher string
		groups                 bool
		version                int
		wantType               bool
	}{
		{target: TargetClaude, projectPath: "settings.json", userPath: "settings.json", member: "hooks", event: "PreToolUse", matcher: "Bash", groups: true, wantType: true},
		{target: TargetFactory, projectPath: "hooks.json", userPath: "hooks.json", event: "PreToolUse", matcher: "Execute", groups: true, wantType: true},
		{target: TargetCursor, projectPath: "hooks.json", userPath: "hooks.json", member: "hooks", event: "preToolUse", matcher: "^Shell$", version: 1, wantType: true},
		{target: TargetGemini, projectPath: "settings.json", userPath: "settings.json", member: "hooks", event: "BeforeTool", matcher: "^run_shell_command$", groups: true, wantType: true},
		{target: TargetWindsurf, projectPath: ".devin/hooks.json", userPath: "hooks.json", member: "hooks", event: "pre_run_command"},
		{target: TargetDevin, projectPath: "hooks.v1.json", userPath: "config.json", event: "PreToolUse", matcher: "^exec$", groups: true, wantType: true},
		{target: TargetCodex, projectPath: ".codex/hooks.json", userPath: ".codex/hooks.json", member: "hooks", event: "PreToolUse", matcher: "^Bash$", groups: true, wantType: true},
	}
	for _, tc := range tests {
		for _, machine := range []bool{false, true} {
			scope := ScopeProject
			wantPath := tc.projectPath
			if machine {
				scope = ScopeUser
				wantPath = tc.userPath
			}
			t.Run(string(tc.target)+"/machine="+map[bool]string{false: "false", true: "true"}[machine], func(t *testing.T) {
				hook := ir.NewHookWithMatcher("deploy", "PreToolUse", "Bash", "echo native-hook", "command", nil, ir.SourceRef{})
				hook.HookSource = string(TargetClaude)
				result, err := Compile(hook, tc.target, WithScope(scope), WithManagedBy("_managed_by", "agent-marketplace"), WithNativeHookOutput())
				if err != nil {
					t.Fatalf("Compile: %v", err)
				}
				pure, err := CompileNativeHookProjection(hook, tc.target, scope, "")
				if err != nil || pure.HasErrors() || len(pure.Artifacts) != 1 {
					t.Fatalf("CompileNativeHookProjection: err=%v result=%+v", err, pure)
				}
				if len(result.Artifacts) != 1 {
					t.Fatalf("artifacts = %#v, want one native fragment", result.Artifacts)
				}
				a := result.Artifacts[0]
				if pure.Artifacts[0].Path != a.Path || pure.Artifacts[0].Mode != a.Mode || !bytes.Equal(pure.Artifacts[0].Bytes, a.Bytes) {
					t.Fatalf("native flag output differs from pure projection: flag=%+v pure=%+v", a, pure.Artifacts[0])
				}
				if a.Path != wantPath || a.Mode.Perm() != 0o600 {
					t.Fatalf("artifact path/mode = %q/%#o, want %q/0600", a.Path, a.Mode.Perm(), wantPath)
				}
				if !a.Fragment || !a.ExternalReconciliationRequired || a.Reconciliation.Mode != ReconciliationExternal || a.ManagedByField != "" || a.ManagedByValue != "" || a.MergeWithExisting {
					t.Fatalf("native reconciliation metadata = %+v", a)
				}
				if bytes.Contains(a.Bytes, []byte("_managed_by")) {
					t.Fatalf("native fragment contains a marker: %s", a.Bytes)
				}
				var document map[string]any
				if err := json.Unmarshal(a.Bytes, &document); err != nil {
					t.Fatalf("decode native fragment: %v", err)
				}
				if tc.version != 0 && document["version"] != float64(tc.version) {
					t.Fatalf("version = %#v, want %d", document["version"], tc.version)
				}
				entries := document
				member := tc.member
				if tc.target == TargetDevin && machine {
					member = "hooks"
				}
				if member != "" {
					var ok bool
					entries, ok = document[member].(map[string]any)
					if !ok {
						t.Fatalf("member %q = %#v", member, document[member])
					}
				}
				values, ok := entries[tc.event].([]any)
				if !ok || len(values) != 1 {
					t.Fatalf("event %q = %#v, want one entry", tc.event, entries[tc.event])
				}
				entry, ok := values[0].(map[string]any)
				if !ok {
					t.Fatalf("event entry = %#v", values[0])
				}
				handler := entry
				if tc.groups {
					if entry["matcher"] != tc.matcher {
						t.Fatalf("matcher group = %#v, want %q", entry["matcher"], tc.matcher)
					}
					handlers, ok := entry["hooks"].([]any)
					if !ok || len(handlers) != 1 {
						t.Fatalf("handlers = %#v, want one", entry["hooks"])
					}
					handler, ok = handlers[0].(map[string]any)
					if !ok {
						t.Fatalf("handler = %#v", handlers[0])
					}
				} else if tc.matcher != "" && entry["matcher"] != tc.matcher {
					t.Fatalf("matcher = %#v, want %q", entry["matcher"], tc.matcher)
				}
				if handler["command"] != "echo native-hook" {
					t.Fatalf("command = %#v", handler["command"])
				}
				if tc.wantType && handler["type"] != "command" {
					t.Fatalf("type = %#v, want command", handler["type"])
				}
				if tc.target == TargetCursor {
					if _, exists := handler["event"]; exists {
						t.Fatalf("Cursor handler redundantly contains event: %#v", handler)
					}
				}
			})
		}
	}
}

func nativeHookOutputIngestedHook(t *testing.T, definition string) *ir.Hook {
	t.Helper()
	bundle, err := IngestBundle(fstest.MapFS{
		".claude/settings.json": &fstest.MapFile{Data: []byte(definition)},
	}, HarnessClaude)
	if err != nil {
		t.Fatalf("IngestBundle: %v", err)
	}
	for _, resource := range bundle.Resources {
		if hook, ok := resource.(*ir.Hook); ok {
			if hook.SourceDefinitionDigest() == "" {
				t.Fatal("ingested hook has no source-definition digest")
			}
			return hook
		}
	}
	t.Fatal("ingest produced no hook")
	return nil
}

// spec: hook-resources/new-entries-carry-no-in-file-marker
func TestNativeHookOutputRejectsAuthoredManagedByExtras(t *testing.T) {
	const markerValue = "value-that-must-not-appear-in-errors"
	cases := []struct {
		name, key, definition string
		group                 bool
	}{
		{
			name:       "handler exact",
			key:        "_managed_by",
			definition: `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo marker","_managed_by":"` + markerValue + `"}]}]}}`,
		},
		{
			name:       "handler case alias",
			key:        "_Managed_By",
			definition: `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo marker","_Managed_By":"` + markerValue + `"}]}]}}`,
		},
		{
			name:       "group exact",
			key:        "_managed_by",
			group:      true,
			definition: `{"hooks":{"PreToolUse":[{"matcher":"Bash","_managed_by":"` + markerValue + `","hooks":[{"type":"command","command":"echo marker"}]}]}}`,
		},
		{
			name:       "group case alias",
			key:        "_Managed_By",
			group:      true,
			definition: `{"hooks":{"PreToolUse":[{"matcher":"Bash","_Managed_By":"` + markerValue + `","hooks":[{"type":"command","command":"echo marker"}]}]}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hook := nativeHookOutputIngestedHook(t, tc.definition)
			if got := hook.GroupExtraKeys[tc.key]; got != tc.group {
				t.Fatalf("group-extra provenance for %q = %v, want %v", tc.key, got, tc.group)
			}
			pluginBefore, err := Compile(hook, TargetFactory)
			if err != nil || pluginBefore == nil || len(pluginBefore.Artifacts) != 1 || pluginBefore.Artifacts[0].Path != "hooks/hooks.json" {
				t.Fatalf("default plugin compilation path changed: result=%t err=%v", pluginBefore != nil, err)
			}
			native, err := Compile(hook, TargetFactory, WithNativeHookOutput())
			if err == nil || native != nil {
				t.Fatalf("authored marker native output returned result=%t, err=%v; want refusal without artifacts", native != nil, err)
			}
			if strings.Contains(err.Error(), markerValue) {
				t.Fatalf("native refusal leaked marker value: %v", err)
			}
			pluginAfter, err := Compile(hook, TargetFactory)
			if err != nil || pluginAfter == nil || len(pluginAfter.Artifacts) != 1 || pluginAfter.Artifacts[0].Path != pluginBefore.Artifacts[0].Path || !bytes.Equal(pluginAfter.Artifacts[0].Bytes, pluginBefore.Artifacts[0].Bytes) {
				t.Fatalf("native refusal changed default plugin output: before=%+v after=%+v err=%v", pluginBefore, pluginAfter, err)
			}
		})
	}
}

// spec: hook-resources/new-entries-carry-no-in-file-marker
func TestNativeHookOutputPreservesNormalExtrasAndRawNumbers(t *testing.T) {
	hook := nativeHookOutputIngestedHook(t, `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo normal","userExtra":{"nested":[1.0,"keep"]}}]}]}}`)
	result, err := Compile(hook, TargetFactory, WithNativeHookOutput())
	if err != nil || result == nil || len(result.Artifacts) != 1 {
		t.Fatalf("Compile normal extra: result=%+v err=%v", result, err)
	}
	if bytes.Contains(result.Artifacts[0].Bytes, []byte("_managed_by")) || !bytes.Contains(result.Artifacts[0].Bytes, []byte("userExtra")) || !bytes.Contains(result.Artifacts[0].Bytes, []byte("keep")) {
		t.Fatalf("native extra output = %s", result.Artifacts[0].Bytes)
	}
	pure, err := CompileNativeHookProjection(hook, TargetFactory, ScopeProject, "")
	if err != nil || pure.HasErrors() || len(pure.Artifacts) != 1 || !bytes.Equal(pure.Artifacts[0].Bytes, result.Artifacts[0].Bytes) {
		t.Fatalf("native flag extra output differs from pure projection: pure=%+v err=%v", pure, err)
	}
	raw, err := compile.NativeHookArtifact(compile.TargetFactory, compile.ScopeProject, "PreToolUse", json.RawMessage(`{"matcher":"Execute","hooks":[{"type":"command","command":"echo raw","userExtra":1.0}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw.Bytes, []byte(`"userExtra":1.0`)) {
		t.Fatalf("raw extra numeric spelling changed: %s", raw.Bytes)
	}
}

// spec: hook-resources/gemini-timeouts-are-converted-to-milliseconds
func TestNativeHookOutputFlagMatchesPureProjectionTimeout(t *testing.T) {
	hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Bash", "echo timeout", "command", nil,
		ir.SourceRef{File: "hooks/hooks.json"}, ir.WithHookSource("claude"), ir.WithHookTimeout(30))
	withFlag, err := Compile(hook, TargetGemini, WithNativeHookOutput(), WithScope(ScopeProject))
	if err != nil || withFlag == nil || withFlag.HasErrors() || len(withFlag.Artifacts) != 1 {
		t.Fatalf("native Compile: result=%+v err=%v", withFlag, err)
	}
	pure, err := CompileNativeHookProjection(hook, TargetGemini, ScopeProject, "")
	if err != nil || pure == nil || pure.HasErrors() || len(pure.Artifacts) != 1 {
		t.Fatalf("pure projection: result=%+v err=%v", pure, err)
	}
	if withFlag.Artifacts[0].Path != "settings.json" || pure.Artifacts[0].Path != withFlag.Artifacts[0].Path ||
		!bytes.Equal(withFlag.Artifacts[0].Bytes, pure.Artifacts[0].Bytes) {
		t.Fatalf("native Compile output = %+v, pure projection = %+v", withFlag.Artifacts[0], pure.Artifacts[0])
	}
	var document map[string]any
	if err := json.Unmarshal(withFlag.Artifacts[0].Bytes, &document); err != nil {
		t.Fatal(err)
	}
	group := document["hooks"].(map[string]any)["BeforeTool"].([]any)[0].(map[string]any)
	if group["matcher"] != "^run_shell_command$" {
		t.Fatalf("Gemini matcher = %v", group["matcher"])
	}
	handler := group["hooks"].([]any)[0].(map[string]any)
	if handler["type"] != "command" || handler["command"] != "echo timeout" || handler["timeout"] != float64(30000) {
		t.Fatalf("Gemini handler = %v, want command with 30000ms timeout", handler)
	}
}
