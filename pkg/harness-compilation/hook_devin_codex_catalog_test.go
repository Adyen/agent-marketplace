package harnesscompile

// Catalog-driven Devin CLI and Codex hook translation (OpenSpec change
// native-hook-integration, task 3.4). These tests exercise the public
// Compile path. Claude Code, Factory Droid, and Cursor rows join the same
// scenario annotations when their translators move to hookpolicy
// (tasks 3.5-3.7).

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: hook-resources/tool-names-are-translated-per-target
func TestToolNamesAreTranslatedPerTarget(t *testing.T) {
	cases := []struct {
		name        string
		target      Target
		wantMatcher string
	}{
		{"Devin CLI anchors the exec tool", TargetDevin, "^exec$"},
		{"Codex anchors the Bash tool", TargetCodex, "^Bash$"},
		{"Gemini CLI anchors run_shell_command", TargetGemini, "^run_shell_command$"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Bash", "echo check", "command", nil, ir.SourceRef{})
			result, err := Compile(hook, tc.target)
			if err != nil {
				t.Fatalf("Compile(hook, %s): %v", tc.target, err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("want one artifact, got %d (diags: %+v)", len(result.Artifacts), result.Diagnostics)
			}
			scalars := hookJSONScalars(t, result)
			if !containsScalar(scalars, tc.wantMatcher) {
				t.Fatalf("want matcher %q; scalars = %v", tc.wantMatcher, scalars)
			}
			if containsScalar(scalars, "Bash") {
				t.Fatalf("untranslated matcher reached the artifact; scalars = %v", scalars)
			}
		})
	}
}

// spec: hook-resources/negative-timeouts-are-refused-on-every-target
func TestNegativeTimeoutsAreRefusedOnEveryTarget(t *testing.T) {
	timeout := -1
	for _, target := range []Target{TargetDevin, TargetCodex, TargetGemini, TargetWindsurf} {
		t.Run(string(target), func(t *testing.T) {
			hook := ir.NewHookWithMatcherOptions("guard", "Stop", "", "echo check", "command", nil, ir.SourceRef{}, ir.WithHookTimeoutPtr(&timeout))
			result, err := Compile(hook, target)
			if err != nil {
				t.Fatalf("Compile(hook, %s): %v", target, err)
			}
			if len(result.Artifacts) != 0 {
				t.Fatalf("a negative timeout must suppress the hook on %s; artifacts = %d", target, len(result.Artifacts))
			}
			if !diag.HasErrors(result.Diagnostics) {
				t.Fatalf("want an error diagnostic on %s; diagnostics = %+v", target, result.Diagnostics)
			}
			assertHookDiagnosticsValueFree(t, result.Diagnostics, "echo check", "-1")
		})
	}
}

// spec: hook-resources/codex-caps-sessionend-timeouts
func TestCodexCapsSessionEndTimeouts(t *testing.T) {
	timeout := 30
	// Codex's SessionEnd is native-only (its documented one-to-three-second
	// bound means the canonical SessionEnd is never equated with it), so the
	// cap is exercised by a Codex-provenance hook compiled for Codex.
	hook := ir.NewHookWithMatcherOptions("cleanup", "SessionEnd", "", "./cleanup", "command", nil, ir.SourceRef{}, ir.WithHookTimeoutPtr(&timeout), ir.WithHookSource("codex"))
	result, err := Compile(hook, TargetCodex)
	if err != nil {
		t.Fatalf("Compile(hook, codex): %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("want one artifact, got %d (diags: %+v)", len(result.Artifacts), result.Diagnostics)
	}
	scalars := hookJSONScalars(t, result)
	if !containsScalar(scalars, "SessionEnd") || !containsScalar(scalars, "3") {
		t.Fatalf("want SessionEnd event and timeout 3; scalars = %v", scalars)
	}
	if containsScalar(scalars, "30") {
		t.Fatalf("timeout above the limit reached the artifact; scalars = %v", scalars)
	}
	if !diag.HasCode(result.Diagnostics, diag.CodeCodexFieldDropped) {
		t.Fatalf("want %s diagnostic; diagnostics = %+v", diag.CodeCodexFieldDropped, result.Diagnostics)
	}
}

// Edge: a matcher on an event the Devin catalog declares without matchers is
// refused rather than dropped, because emitting it would widen activation.
func TestDevinRefusesMatcherOnNonToolEvent(t *testing.T) {
	hook := ir.NewHookWithMatcherOptions("guard", "Stop", "Bash", "echo check", "command", nil, ir.SourceRef{})
	result, err := Compile(hook, TargetDevin)
	if err != nil {
		t.Fatalf("Compile(hook, devin): %v", err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("a matcher on a non-tool event must suppress the hook; artifacts = %d", len(result.Artifacts))
	}
	if !diag.HasCode(result.Diagnostics, diag.CodeHookMatcherUntranslatable) {
		t.Fatalf("want %s diagnostic; diagnostics = %+v", diag.CodeHookMatcherUntranslatable, result.Diagnostics)
	}
}

// Edge: project-directory references follow the target catalog's documented
// repository base on Devin and Codex.
func TestDevinAndCodexRewriteProjectDirectoryReferences(t *testing.T) {
	cases := []struct {
		name   string
		target Target
		want   string
	}{
		{"Devin uses DEVIN_PROJECT_DIR", TargetDevin, `"$DEVIN_PROJECT_DIR"/bin/x`},
		{"Codex uses the git root", TargetCodex, `"$(git rev-parse --show-toplevel)"/bin/x`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hook := ir.NewHookWithMatcherOptions("guard", "Stop", "", `"$CLAUDE_PROJECT_DIR"/bin/x`, "command", nil, ir.SourceRef{})
			result, err := Compile(hook, tc.target)
			if err != nil {
				t.Fatalf("Compile(hook, %s): %v", tc.target, err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("want one artifact, got %d (diags: %+v)", len(result.Artifacts), result.Diagnostics)
			}
			scalars := hookJSONScalars(t, result)
			if !containsScalar(scalars, tc.want) {
				t.Fatalf("want command %q; scalars = %v", tc.want, scalars)
			}
		})
	}
}
