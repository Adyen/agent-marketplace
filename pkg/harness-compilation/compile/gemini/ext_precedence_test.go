package gemini_test

import (
	"encoding/json"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/gemini"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

func TestGeminiCommandExtPrecedence(t *testing.T) {
	t.Run("uses canonical command fields when ext TOML metadata is present", func(t *testing.T) {
		cmd := ir.NewCommand("review", "Review changes", "canonical prompt", nil, ir.SourceRef{})
		cmd.Ext.Gemini = &extpkg.Gemini{
			TOMLCommand: &extpkg.GeminiTOMLCommand{
				Description: "Override description",
				Prompt:      "override prompt",
				Triggers:    []string{"slash"},
			},
		}

		result, err := compile.Compile(cmd, compile.TargetGemini)
		if err != nil {
			t.Fatalf("Compile() error: %v", err)
		}

		var doc struct {
			Description string   `toml:"description"`
			Prompt      string   `toml:"prompt"`
			Triggers    []string `toml:"triggers"`
		}
		if err := toml.Unmarshal(result.Artifacts[0].Bytes, &doc); err != nil {
			t.Fatalf("toml.Unmarshal: %v", err)
		}
		if doc.Description != "Review changes" || doc.Prompt != "canonical prompt" || len(doc.Triggers) != 1 || doc.Triggers[0] != "slash" {
			t.Fatalf("unexpected canonical TOML command: %#v", doc)
		}
		if len(result.Diagnostics) != 0 {
			t.Fatalf("diagnostics = %#v, want none", result.Diagnostics)
		}
	})

	t.Run("falls back to canonical command when ext missing", func(t *testing.T) {
		cmd := ir.NewCommand("review", "Review changes", "canonical prompt", nil, ir.SourceRef{})

		result, err := compile.Compile(cmd, compile.TargetGemini)
		if err != nil {
			t.Fatalf("Compile() error: %v", err)
		}

		var doc struct {
			Description string `toml:"description"`
			Prompt      string `toml:"prompt"`
		}
		if err := toml.Unmarshal(result.Artifacts[0].Bytes, &doc); err != nil {
			t.Fatalf("toml.Unmarshal: %v", err)
		}
		if doc.Description != "Review changes" || doc.Prompt != "canonical prompt" {
			t.Fatalf("unexpected canonical TOML command: %#v", doc)
		}
	})
}

func TestGeminiCommandPreservesExtrasAndCanonicalPrecedence(t *testing.T) {
	cmd := ir.NewCommand("review", "Canonical description", "Canonical prompt", map[string]any{
		"description": "stale extra",
		"custom":      true,
	}, ir.SourceRef{})
	cmd.Ext.Gemini = &extpkg.Gemini{TOMLCommand: &extpkg.GeminiTOMLCommand{Triggers: []string{"review"}, TriggersSet: true}}

	result, err := compile.Compile(cmd, compile.TargetGemini)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := toml.Unmarshal(result.Artifacts[0].Bytes, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["description"] != "Canonical description" || doc["prompt"] != "Canonical prompt" || doc["custom"] != true {
		t.Fatalf("TOML document = %#v", doc)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != diag.CodeCanonicalFieldOverridesExtra {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
}

func TestGeminiSubagentAppliesMappingOverrides(t *testing.T) {
	sub := ir.NewSubagent("override-agent", "Agent", "Work", "sonnet", []string{"Write"}, "", nil, "", nil, ir.SourceRef{})
	result, err := compile.Compile(sub, compile.TargetGemini,
		compile.WithModelOverride(map[string]string{"sonnet": "custom-model"}),
		compile.WithToolOverride(map[string]string{"Write": "CustomWrite"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 || len(result.Artifacts) != 1 {
		t.Fatalf("result = %#v, want one artifact without diagnostics", result)
	}
	body := string(result.Artifacts[0].Bytes)
	if !strings.Contains(body, "model: custom-model") || !strings.Contains(body, "- CustomWrite") {
		t.Fatalf("mapping overrides not applied:\n%s", body)
	}
}

func TestGeminiSubagentExtPrecedence(t *testing.T) {
	sub := ir.NewSubagent("reviewer", "Review code", "Body", "sonnet", []string{"Read"}, "", nil, "", nil, ir.SourceRef{})
	temp := 0.75
	maxTurns := 9
	timeout := 4
	sub.Ext.Gemini = &extpkg.Gemini{
		Kind:           "remote",
		Temperature:    &temp,
		MaxTurns:       &maxTurns,
		TimeoutMinutes: &timeout,
		InlineMCPServers: map[string]extpkg.GeminiMCPServer{
			"docs": {Command: "npx", Args: []string{"docs-server"}},
		},
	}

	result, err := compile.Compile(sub, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}

	text := string(result.Artifacts[0].Bytes)
	for _, want := range []string{
		"kind: remote",
		"temperature: 0.75",
		"max_turns: 9",
		"timeout_mins: 4",
		"mcpServers:",
		"docs:",
		"command: npx",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("subagent artifact missing %q:\n%s", want, text)
		}
	}
}

func TestGeminiInlineMCPCredentialDiagnostics(t *testing.T) {
	sub := ir.NewSubagent("reviewer", "Review code", "Body", "sonnet", nil, "", nil, "", nil, ir.SourceRef{})
	sub.Ext.Gemini = &extpkg.Gemini{InlineMCPServers: map[string]extpkg.GeminiMCPServer{
		"private": {Command: "server", Env: map[string]string{"API_TOKEN": "$literal"}},
	}}
	result, err := compile.Compile(sub, compile.TargetGemini)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != diag.CodeMcpLiteralCredential {
		t.Fatalf("diagnostics = %#v, want inline MCP credential diagnostic", result.Diagnostics)
	}
	if strings.Contains(result.Diagnostics[0].Message, "$literal") || strings.Contains(result.Diagnostics[0].Hint, "$literal") {
		t.Fatal("inline MCP credential value leaked into diagnostic")
	}
}

func TestGeminiHookExtPrecedence(t *testing.T) {
	hook := ir.NewHook("pre-commit", "PreToolUse", "echo test", "command", nil, ir.SourceRef{})
	timeout := 12
	hook.Ext.Gemini = &extpkg.Gemini{
		HookName:    "friendly-name",
		HookTimeout: &timeout,
	}

	result, err := compile.Compile(hook, compile.TargetGemini, compile.WithManagedBy("managed-by", "marketplace"))
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if result.Artifacts[0].ManagedByField != "managed-by" || result.Artifacts[0].ManagedByValue != "marketplace" || !result.Artifacts[0].MergeWithExisting {
		t.Fatalf("managed-by metadata = %#v", result.Artifacts[0])
	}

	var settings map[string]any
	if err := json.Unmarshal(result.Artifacts[0].Bytes, &settings); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	group := settings["hooks"].(map[string]any)["BeforeTool"].([]any)[0].(map[string]any)
	entries, ok := group["hooks"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("native group hooks = %#v", group["hooks"])
	}
	entry := entries[0].(map[string]any)
	if entry["name"] != "friendly-name" {
		t.Fatalf("hook name = %#v, want %q", entry["name"], "friendly-name")
	}
	if int(entry["timeout"].(float64)) != 12 {
		t.Fatalf("hook timeout = %#v, want 12", entry["timeout"])
	}
}
