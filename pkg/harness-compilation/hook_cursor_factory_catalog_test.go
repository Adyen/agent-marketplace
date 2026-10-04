package harnesscompile

// Catalog-driven Cursor and Factory Droid hook translation (OpenSpec change
// native-hook-integration, tasks 3.6-3.7). These tests exercise the public
// Compile path.

import (
	"encoding/json"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	factoryingest "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/factory"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: hook-resources/cursor-receives-native-event-names
func TestCursorReceivesNativeEventNames(t *testing.T) {
	cases := []struct {
		canonical string
		native    string
	}{
		{"PreToolUse", "preToolUse"},
		{"UserPromptSubmit", "beforeSubmitPrompt"},
		{"Stop", "stop"},
	}
	for _, tc := range cases {
		t.Run(tc.canonical, func(t *testing.T) {
			hook := ir.NewHookWithMatcherOptions("guard", tc.canonical, "", "echo check", "command", nil, ir.SourceRef{})
			result, err := Compile(hook, TargetCursor)
			if err != nil {
				t.Fatalf("Compile(hook, cursor): %v", err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("want one artifact, got %d (diags: %+v)", len(result.Artifacts), result.Diagnostics)
			}
			scalars := hookJSONScalars(t, result)
			if !containsScalar(scalars, tc.native) {
				t.Fatalf("want native event %q; scalars = %v", tc.native, scalars)
			}
			if containsScalar(scalars, tc.canonical) && tc.canonical != tc.native {
				t.Fatalf("canonical event %q reached the artifact; scalars = %v", tc.canonical, scalars)
			}
		})
	}
}

// spec: hook-resources/factory-hooks-keep-native-tool-names
func TestFactoryHooksKeepNativeToolNames(t *testing.T) {
	src := fstest.MapFS{
		".factory/hooks.json": &fstest.MapFile{Data: []byte(`{
			"PreToolUse": [
				{
					"matcher": "Execute",
					"commandRegex": "^git",
					"hooks": [{"type": "command", "command": "echo ok"}]
				}
			]
		}`)},
	}
	hooks, _, err := factoryingest.LoadHooks(src, ".factory/hooks.json")
	if err != nil {
		t.Fatalf("factory LoadHooks: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("want one ingested hook, got %d", len(hooks))
	}
	result, err := Compile(hooks[0], TargetFactory)
	if err != nil {
		t.Fatalf("Compile(hook, factory): %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("want one artifact, got %d (diags: %+v)", len(result.Artifacts), result.Diagnostics)
	}
	var doc map[string][]map[string]any
	if err := json.Unmarshal(result.Artifacts[0].Bytes, &doc); err != nil {
		t.Fatalf("unmarshal artifact: %v", err)
	}
	groups := doc["PreToolUse"]
	if len(groups) != 1 {
		t.Fatalf("want one PreToolUse group, got %d (doc: %s)", len(groups), result.Artifacts[0].Bytes)
	}
	if groups[0]["matcher"] != "Execute" {
		t.Fatalf("native matcher must be kept as authored; group = %v", groups[0])
	}
	if groups[0]["commandRegex"] != "^git" {
		t.Fatalf("commandRegex must be kept at group level; group = %v", groups[0])
	}
}

// Edge: a handler type the Cursor catalog does not declare suppresses only
// that hook with the cursor-specific type code.
func TestCursorRefusesUndeclaredHandlerType(t *testing.T) {
	hook := ir.NewHookWithMatcherOptions("guard", "preToolUse", "", "echo check", "http", nil, ir.SourceRef{}, ir.WithHookSource("cursor"))
	result, err := Compile(hook, TargetCursor)
	if err != nil {
		t.Fatalf("Compile(hook, cursor): %v", err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("an undeclared handler type must suppress the hook; artifacts = %d", len(result.Artifacts))
	}
	if !diag.HasCode(result.Diagnostics, diag.CodeHookCursorUnsupportedType) {
		t.Fatalf("want %s diagnostic; diagnostics = %+v", diag.CodeHookCursorUnsupportedType, result.Diagnostics)
	}
}

// Edge: Factory declares only command handlers, so a prompt hook is refused
// with the shared prompt code.
func TestFactoryRefusesPromptHooks(t *testing.T) {
	hook := ir.NewHookWithMatcherOptions("guard", "Stop", "", "", "prompt", nil, ir.SourceRef{}, ir.WithHookPrompt("explain"))
	result, err := Compile(hook, TargetFactory)
	if err != nil {
		t.Fatalf("Compile(hook, factory): %v", err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("a prompt hook must be suppressed on Factory; artifacts = %d", len(result.Artifacts))
	}
	if !diag.HasCode(result.Diagnostics, diag.CodeHookPromptUnsupported) {
		t.Fatalf("want %s diagnostic; diagnostics = %+v", diag.CodeHookPromptUnsupported, result.Diagnostics)
	}
}

// Edge: a matcher with no exact tool coverage on Factory suppresses the hook
// rather than widening to an unmatched rule.
func TestFactorySuppressesUntranslatableMatcher(t *testing.T) {
	hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "NoSuchTool", "echo check", "command", nil, ir.SourceRef{})
	result, err := Compile(hook, TargetFactory)
	if err != nil {
		t.Fatalf("Compile(hook, factory): %v", err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("an untranslatable matcher must suppress the hook; artifacts = %d", len(result.Artifacts))
	}
	if !diag.HasCode(result.Diagnostics, diag.CodeHookMatcherUntranslatable) {
		t.Fatalf("want %s diagnostic; diagnostics = %+v", diag.CodeHookMatcherUntranslatable, result.Diagnostics)
	}
}
