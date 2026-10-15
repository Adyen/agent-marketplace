package harnesscompile

// Catalog-driven Claude Code hook translation (OpenSpec change
// native-hook-integration, task 3.5). These tests exercise the public
// Compile path and prove the shipped Claude hook catalog gates events and
// handler types, declares prompt handlers, and accepts renamed tools through
// aliases.

import (
	"encoding/json"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// claudeHookRules decodes the hooks.json fragment and returns the rule
// objects under the given native event.
func claudeHookRules(t *testing.T, r *Result, event string) []map[string]any {
	t.Helper()
	if len(r.Artifacts) != 1 {
		t.Fatalf("want one artifact, got %d (diags: %+v)", len(r.Artifacts), r.Diagnostics)
	}
	var doc map[string]any
	if err := json.Unmarshal(r.Artifacts[0].Bytes, &doc); err != nil {
		t.Fatalf("artifact is not valid JSON: %v", err)
	}
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("artifact has no hooks map: %v", doc)
	}
	entries, ok := hooks[event].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("hooks[%q] = %v, want one rule", event, hooks[event])
	}
	rules := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		rule, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("hook rule is not an object: %v", entry)
		}
		rules = append(rules, rule)
	}
	return rules
}

// spec: hook-resources/claude-code-refuses-unknown-events
func TestClaudeCodeRefusesUnknownEvents(t *testing.T) {
	hook := ir.NewHook("guard", "NoSuchCatalogEvent", "echo check", "command", nil, ir.SourceRef{})
	result, err := Compile(hook, TargetClaude)
	if err != nil {
		t.Fatalf("Compile(hook, claude): %v", err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("unknown event emitted %d artifacts, want 0", len(result.Artifacts))
	}
	if !diag.HasCode(result.Diagnostics, diag.CodeHookEventUnmappable) {
		t.Fatalf("want %s diagnostic; diagnostics = %+v", diag.CodeHookEventUnmappable, result.Diagnostics)
	}
	if !diag.HasErrors(result.Diagnostics) {
		t.Fatalf("want error severity; diagnostics = %+v", result.Diagnostics)
	}
	assertHookDiagnosticsValueFree(t, result.Diagnostics, "echo check", "NoSuchCatalogEvent")
}

// spec: hook-resources/unknown-handler-types-are-refused-on-claude-code
func TestUnknownHandlerTypesAreRefusedOnClaudeCode(t *testing.T) {
	hook := ir.NewHook("guard", "PreToolUse", "echo check", "webhook", nil, ir.SourceRef{})
	result, err := Compile(hook, TargetClaude)
	if err != nil {
		t.Fatalf("Compile(hook, claude): %v", err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("undeclared handler type emitted %d artifacts, want 0", len(result.Artifacts))
	}
	if !diag.HasCode(result.Diagnostics, diag.CodeHookTypeUnsupported) {
		t.Fatalf("want %s diagnostic; diagnostics = %+v", diag.CodeHookTypeUnsupported, result.Diagnostics)
	}
	if !diag.HasErrors(result.Diagnostics) {
		t.Fatalf("want error severity; diagnostics = %+v", result.Diagnostics)
	}
	assertHookDiagnosticsValueFree(t, result.Diagnostics, "echo check", "webhook")
}

// spec: hook-resources/prompt-hooks-compile-where-the-catalog-declares-them
func TestPromptHooksCompileWhereTheCatalogDeclaresThemClaude(t *testing.T) {
	hook := ir.NewHookWithOptions("advise", "UserPromptSubmit", "", "prompt", nil, ir.SourceRef{}, ir.WithHookPrompt("review the request"))
	result, err := Compile(hook, TargetClaude)
	if err != nil {
		t.Fatalf("Compile(hook, claude): %v", err)
	}
	rules := claudeHookRules(t, result, "UserPromptSubmit")
	rule := rules[0]
	if rule["prompt"] != "review the request" {
		t.Fatalf("rule prompt = %v, want the prompt payload", rule["prompt"])
	}
	if _, present := rule["command"]; present {
		t.Fatalf("prompt hook must not carry a command member: %v", rule)
	}
	if rule["type"] != "prompt" {
		t.Fatalf("rule type = %v, want prompt", rule["type"])
	}
}

// spec: hook-resources/renamed-tools-are-accepted-through-aliases
func TestRenamedToolsAreAcceptedThroughAliases(t *testing.T) {
	hook := ir.NewHookWithMatcher("guard", "PreToolUse", "Task", "echo check", "command", nil, ir.SourceRef{})
	result, err := Compile(hook, TargetClaude)
	if err != nil {
		t.Fatalf("Compile(hook, claude): %v", err)
	}
	rules := claudeHookRules(t, result, "PreToolUse")
	if rules[0]["matcher"] != "Agent" {
		t.Fatalf("matcher = %v, want the canonical Agent name for the Task alias", rules[0]["matcher"])
	}
}
