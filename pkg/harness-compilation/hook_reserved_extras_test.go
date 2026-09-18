package harnesscompile_test

import (
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

var hookReservationTargets = []struct {
	name   string
	target hcc.Target
}{
	{name: "claude", target: hcc.TargetClaude},
	{name: "factory", target: hcc.TargetFactory},
	{name: "cursor", target: hcc.TargetCursor},
	{name: "windsurf", target: hcc.TargetWindsurf},
	{name: "gemini", target: hcc.TargetGemini},
	{name: "devin", target: hcc.TargetDevin},
	{name: "codex", target: hcc.TargetCodex},
}

func newReservedExtrasHook() *ir.Hook {
	marker := "unreviewed-extra-value"
	extra := map[string]any{
		"event":    marker,
		"EVENT":    marker,
		"type":     marker,
		"TYPE":     marker,
		"matcher":  marker,
		"MATCHER":  marker,
		"command":  marker,
		"COMMAND":  marker,
		"prompt":   marker,
		"PROMPT":   marker,
		"timeout":  marker,
		"TIMEOUT":  marker,
		"hooks":    marker,
		"HOOKS":    marker,
		"x-custom": "custom-value",
	}
	return ir.NewHookWithMatcher("reserved", "PreToolUse", "Read", "echo typed", "command", extra, ir.SourceRef{File: "hooks.json", Line: 1})
}

func artifactText(result *hcc.Result) string {
	var text strings.Builder
	for _, artifact := range result.Artifacts {
		text.Write(artifact.Bytes)
	}
	return text.String()
}

func TestHookReservedExtrasAcrossTargets(t *testing.T) {
	for _, target := range hookReservationTargets {
		t.Run(target.name, func(t *testing.T) {
			baseline, err := hcc.Compile(ir.NewHookWithMatcher("baseline", "PreToolUse", "Read", "echo typed", "command", nil, ir.SourceRef{}), target.target)
			if err != nil {
				t.Fatalf("baseline compile: %v", err)
			}
			if len(baseline.Artifacts) == 0 {
				t.Fatalf("baseline artifacts = 0, diagnostics = %#v", baseline.Diagnostics)
			}

			result, err := hcc.Compile(newReservedExtrasHook(), target.target)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if target.target == hcc.TargetWindsurf && len(result.Artifacts) == 0 {
				if !diag.HasCode(result.Diagnostics, diag.CodeHookWindsurfNativeFieldsUnsupported) {
					t.Fatalf("diagnostics = %#v, want Windsurf native-field diagnostic", result.Diagnostics)
				}
				return
			}
			text := artifactText(result)
			if strings.Contains(text, "unreviewed-extra-value") || strings.Contains(text, `"COMMAND"`) || strings.Contains(text, `"EVENT"`) || strings.Contains(text, `"MATCHER"`) || strings.Contains(text, `"TYPE"`) || strings.Contains(text, `"PROMPT"`) || strings.Contains(text, `"TIMEOUT"`) || strings.Contains(text, `"HOOKS"`) {
				t.Fatalf("reserved extra leaked in artifacts: %s", text)
			}
			baselineText := artifactText(baseline)
			if strings.Contains(baselineText, "echo typed") && !strings.Contains(text, "echo typed") {
				t.Fatalf("canonical command disappeared: %s", text)
			}
			if target.target == hcc.TargetCodex {
				if !diag.HasCode(result.Diagnostics, diag.CodeCodexFieldDropped) {
					t.Fatalf("diagnostics = %#v, want Codex extra-loss diagnostic", result.Diagnostics)
				}
			} else if !strings.Contains(text, `"x-custom":"custom-value"`) && !strings.Contains(text, `"x-custom": "custom-value"`) {
				t.Fatalf("unrelated extra missing from artifacts: %s", text)
			}
		})
	}
}

func TestHookReservedExtrasWindsurfNativeFields(t *testing.T) {
	cases := []struct {
		name string
		key  string
	}{
		{name: "command", key: "COMMAND"},
		{name: "powershell", key: "powershell"},
		{name: "powershell alias", key: "POWERSHELL"},
		{name: "show output", key: "show_output"},
		{name: "show output alias", key: "SHOW_OUTPUT"},
		{name: "working directory", key: "working_directory"},
		{name: "working directory alias", key: "WORKING_DIRECTORY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hook := ir.NewHookWithMatcher("windsurf", "PreToolUse", "Read", "echo typed", "command", map[string]any{tc.key: "unreviewed-extra-value", "x-custom": "custom-value"}, ir.SourceRef{})
			result, err := hcc.Compile(hook, hcc.TargetWindsurf)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if len(result.Artifacts) == 0 {
				t.Fatalf("artifacts = 0, diagnostics = %#v", result.Diagnostics)
			}
			text := artifactText(result)
			if !strings.Contains(text, "echo typed") || strings.Contains(text, "unreviewed-extra-value") || (!strings.Contains(text, `"x-custom": "custom-value"`) && !strings.Contains(text, `"x-custom":"custom-value"`)) {
				t.Fatalf("artifact text = %s", text)
			}
			if !diag.HasCode(result.Diagnostics, diag.CodeCanonicalFieldOverridesExtra) {
				t.Fatalf("diagnostics = %#v, want canonical-field diagnostic", result.Diagnostics)
			}
		})
	}
}

func TestHookEmptyCommandExtraCannotCreateExecution(t *testing.T) {
	targets := []struct {
		name   string
		target hcc.Target
	}{
		{name: "claude", target: hcc.TargetClaude},
		{name: "factory", target: hcc.TargetFactory},
		{name: "cursor", target: hcc.TargetCursor},
		{name: "gemini", target: hcc.TargetGemini},
	}
	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			hook := ir.NewHook("empty", "PreToolUse", "", "command", map[string]any{"command": "unreviewed-extra-value"}, ir.SourceRef{})
			result, err := hcc.Compile(hook, target.target)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if text := artifactText(result); strings.Contains(text, "unreviewed-extra-value") {
				t.Fatalf("extra command became executable output: %s", text)
			}
		})
	}
}

func TestHookGroupReservedExtrasAndSequentialExtension(t *testing.T) {
	hook := ir.NewHook("grouped", "PreToolUse", "echo typed", "command", nil, ir.SourceRef{})
	hook.Ext.Gemini = &ext.Gemini{HookGroupExtra: map[string]any{
		"matcher":    "unreviewed-extra-value",
		"Matcher":    "unreviewed-extra-value",
		"hooks":      "unreviewed-extra-value",
		"Hooks":      "unreviewed-extra-value",
		"sequential": true,
	}}
	result, err := hcc.Compile(hook, hcc.TargetGemini)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	text := artifactText(result)
	if strings.Contains(text, "unreviewed-extra-value") || !strings.Contains(text, `"sequential": true`) {
		t.Fatalf("group extras = %s", text)
	}
}

func BenchmarkHookReservedExtras(b *testing.B) {
	hook := ir.NewHookWithMatcher("benchmark", "PreToolUse", "Read", "echo typed", "command", map[string]any{
		"x-one":   "one",
		"x-two":   "two",
		"x-three": "three",
		"x-four":  "four",
		"x-five":  "five",
	}, ir.SourceRef{})
	b.ReportAllocs()
	for b.Loop() {
		result, err := hcc.Compile(hook, hcc.TargetClaude)
		if err != nil || len(result.Artifacts) == 0 {
			b.Fatalf("compile result: %v", err)
		}
	}
}
