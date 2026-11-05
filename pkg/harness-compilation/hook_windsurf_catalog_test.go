package harnesscompile

// Catalog-driven Windsurf hook translation (OpenSpec change
// native-hook-integration, task 3.3). These tests exercise the public
// Compile path: PreToolUse splits by the translated tool-matcher concept
// set and explicit timeouts are refused.

import (
	"encoding/json"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func windsurfHookEvents(t *testing.T, result *Result) map[string]bool {
	t.Helper()
	events := map[string]bool{}
	for _, a := range result.Artifacts {
		var doc struct {
			Hooks map[string]json.RawMessage `json:"hooks"`
		}
		if err := json.Unmarshal(a.Bytes, &doc); err != nil {
			continue
		}
		for event := range doc.Hooks {
			events[event] = true
		}
	}
	return events
}

// spec: hook-resources/windsurf-splits-pretooluse-by-translated-tool-matcher
func TestWindsurfSplitsPreToolUseByTranslatedToolMatcher(t *testing.T) {
	t.Run("Bash selects the shell event", func(t *testing.T) {
		hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Bash", "echo check", "command", nil, ir.SourceRef{})
		result, err := Compile(hook, TargetWindsurf)
		if err != nil {
			t.Fatalf("Compile(hook, windsurf): %v", err)
		}
		if len(result.Artifacts) != 1 {
			t.Fatalf("want one artifact, got %d (diags: %+v)", len(result.Artifacts), result.Diagnostics)
		}
		events := windsurfHookEvents(t, result)
		if !events["pre_run_command"] || len(events) != 1 {
			t.Fatalf("want only pre_run_command event; events = %v", events)
		}
	})

	t.Run("Write and Edit select the write event", func(t *testing.T) {
		hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Write|Edit", "echo check", "command", nil, ir.SourceRef{})
		result, err := Compile(hook, TargetWindsurf)
		if err != nil {
			t.Fatalf("Compile(hook, windsurf): %v", err)
		}
		if len(result.Artifacts) != 1 {
			t.Fatalf("want one artifact, got %d (diags: %+v)", len(result.Artifacts), result.Diagnostics)
		}
		events := windsurfHookEvents(t, result)
		if !events["pre_write_code"] || len(events) != 1 {
			t.Fatalf("want only pre_write_code event; events = %v", events)
		}
	})

	t.Run("an empty matcher is suppressed", func(t *testing.T) {
		hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "", "echo check", "command", nil, ir.SourceRef{})
		result, err := Compile(hook, TargetWindsurf)
		if err != nil {
			t.Fatalf("Compile(hook, windsurf): %v", err)
		}
		if len(result.Artifacts) != 0 {
			t.Fatalf("empty matcher must suppress the hook; artifacts = %d", len(result.Artifacts))
		}
		if !diag.HasCode(result.Diagnostics, diag.CodeHookWindsurfEventUnmappable) {
			t.Fatalf("want %s diagnostic; diagnostics = %+v", diag.CodeHookWindsurfEventUnmappable, result.Diagnostics)
		}
	})

	t.Run("a matcher with no exact event is suppressed", func(t *testing.T) {
		// Glob selects only search.glob; no Windsurf event covers exactly
		// that concept set, so the hook must not be widened onto a read or
		// write event.
		hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Glob", "echo check", "command", nil, ir.SourceRef{})
		result, err := Compile(hook, TargetWindsurf)
		if err != nil {
			t.Fatalf("Compile(hook, windsurf): %v", err)
		}
		if len(result.Artifacts) != 0 {
			t.Fatalf("untranslatable matcher must suppress the hook; artifacts = %d", len(result.Artifacts))
		}
		if !diag.HasCode(result.Diagnostics, diag.CodeHookWindsurfEventUnmappable) {
			t.Fatalf("want %s diagnostic; diagnostics = %+v", diag.CodeHookWindsurfEventUnmappable, result.Diagnostics)
		}
	})
}

// spec: hook-resources/windsurf-refuses-explicit-timeouts
func TestWindsurfRefusesExplicitTimeouts(t *testing.T) {
	timeout := 5
	hook := ir.NewHookWithMatcherOptions("guard", "Stop", "", "echo check", "command", nil, ir.SourceRef{}, ir.WithHookTimeoutPtr(&timeout))
	result, err := Compile(hook, TargetWindsurf)
	if err != nil {
		t.Fatalf("Compile(hook, windsurf): %v", err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("explicit timeout must suppress the hook; artifacts = %d", len(result.Artifacts))
	}
	if !diag.HasCode(result.Diagnostics, diag.CodeHookWindsurfTimeoutUnsupported) {
		t.Fatalf("want %s diagnostic; diagnostics = %+v", diag.CodeHookWindsurfTimeoutUnsupported, result.Diagnostics)
	}
	assertHookDiagnosticsValueFree(t, result.Diagnostics, "echo check", "5")
}
