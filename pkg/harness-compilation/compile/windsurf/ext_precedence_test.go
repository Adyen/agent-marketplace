package windsurf

import (
	"encoding/json"
	"strings"
	"testing"

	compilepkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"

	"gopkg.in/yaml.v3"
)

func TestWindsurfCommandExtPrecedence(t *testing.T) {
	t.Run("uses ext workflow override when present", func(t *testing.T) {
		cmd := ir.NewCommand("format", "Format project", "make fmt", nil, ir.SourceRef{})
		cmd.Ext.Windsurf = &extpkg.Windsurf{AutoExecutionMode: "always"}

		arts, diags, err := Compiler{}.Translate(cmd)
		if err != nil {
			t.Fatalf("Translate() error: %v", err)
		}
		if len(diags) != 0 {
			t.Fatalf("diagnostics = %#v, want none", diags)
		}

		text := string(arts[0].Bytes)
		if !strings.Contains(text, "auto_execution_mode: always") {
			t.Fatalf("workflow missing ext auto_execution_mode override:\n%s", text)
		}
	})

	t.Run("falls back to canonical workflow when ext missing", func(t *testing.T) {
		cmd := ir.NewCommand("format", "Format project", "make fmt", nil, ir.SourceRef{})

		arts, diags, err := Compiler{}.Translate(cmd)
		if err != nil {
			t.Fatalf("Translate() error: %v", err)
		}
		if len(diags) != 0 {
			t.Fatalf("diagnostics = %#v, want none", diags)
		}

		text := string(arts[0].Bytes)
		if strings.Contains(text, "auto_execution_mode:") {
			t.Fatalf("workflow unexpectedly included auto_execution_mode without ext override:\n%s", text)
		}
	})
}

func TestWindsurfHookExtPrecedence(t *testing.T) {
	showOutput := true
	hook := ir.NewHook("hook", "PreToolUse", "echo test", "command", nil, ir.SourceRef{})
	hook.Ext.Windsurf = &extpkg.Windsurf{
		OriginalHookEvent: "post_setup_worktree",
		HookPowerShell:    "Write-Host test",
		HookShowOutput:    &showOutput,
		HookWorkingDir:    "/tmp/project",
	}

	result, err := compilepkg.Compile(hook, compilepkg.TargetWindsurf)
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", result.Diagnostics)
	}

	if got := result.Artifacts[0].Path; got != ".windsurf/hooks.json" {
		t.Fatalf("artifact path = %q, want %q", got, ".windsurf/hooks.json")
	}

	var doc map[string]any
	if err := json.Unmarshal(result.Artifacts[0].Bytes, &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	hooks := doc["hooks"].(map[string]any)
	groups := hooks["post_setup_worktree"].([]any)
	entry := groups[0].(map[string]any)
	if entry["powershell"] != "Write-Host test" {
		t.Fatalf("powershell = %#v", entry["powershell"])
	}
	if entry["working_directory"] != "/tmp/project" {
		t.Fatalf("working_directory = %#v", entry["working_directory"])
	}
	if entry["show_output"] != true {
		t.Fatalf("show_output = %#v", entry["show_output"])
	}
}

func TestFormatYAMLKey_ComplexExtraValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value any
		want  string // expected YAML fragment (one key line or multi-line)
	}{
		{name: "bool true", key: "enabled", value: true, want: "enabled: true\n"},
		{name: "bool false", key: "enabled", value: false, want: "enabled: false\n"},
		{name: "int", key: "count", value: 42, want: "count: 42\n"},
		{name: "float", key: "ratio", value: 3.14, want: "ratio: 3.14\n"},
		{name: "slice of any", key: "items", value: []any{"a", "b"}, want: "items:\n  - a\n  - b\n"},
		{name: "map", key: "coords", value: map[string]any{"x": 1, "y": 2}, want: "coords:\n  x: 1\n  \"y\": 2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := compilepkg.RenderYAMLKey(tt.key, tt.value)
			if err != nil {
				t.Fatalf("RenderYAMLKey: %v", err)
			}
			if got != tt.want {
				t.Fatalf("RenderYAMLKey(%q) =\n%q\nwant\n%q", tt.key, got, tt.want)
			}
			// Round-trip: parse the generated YAML and verify the value survived.
			doc := "---\n" + got + "---\n"
			var fm map[string]any
			if err := yaml.Unmarshal([]byte(doc), &fm); err != nil {
				t.Fatalf("invalid YAML produced:\n%s\nerror: %v", got, err)
			}
		})
	}
}

func TestCommandExtraMapSliceRountripsYAML(t *testing.T) {
	cmd := ir.NewCommand("demo", "Demo command", "echo hi\n",
		map[string]any{
			"enabled": true,
			"count":   42,
			"tags":    []any{"alpha", "beta"},
			"config":  map[string]any{"verbose": true, "level": 3},
		},
		ir.SourceRef{},
	)

	arts, _, err := Compiler{}.Translate(cmd)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	text := string(arts[0].Bytes)

	// Extract frontmatter and parse as YAML.
	fmStart := strings.Index(text, "---\n")
	fmEnd := strings.Index(text[fmStart+4:], "---\n")
	if fmStart < 0 || fmEnd < 0 {
		t.Fatalf("no frontmatter found in:\n%s", text)
	}
	fmText := text[fmStart+4 : fmStart+4+fmEnd]

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		t.Fatalf("invalid YAML frontmatter:\n%s\nerror: %v", fmText, err)
	}

	// Verify complex values survived the round-trip.
	if fm["enabled"] != true {
		t.Fatalf("enabled = %v, want true", fm["enabled"])
	}
	if fm["count"] != 42 {
		t.Fatalf("count = %v, want 42", fm["count"])
	}
	tags, ok := fm["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "alpha" || tags[1] != "beta" {
		t.Fatalf("tags = %v, want [alpha beta]", fm["tags"])
	}
	cfg, ok := fm["config"].(map[string]any)
	if !ok || cfg["verbose"] != true || cfg["level"] != 3 {
		t.Fatalf("config = %v, want {verbose:true level:3}", fm["config"])
	}
}
