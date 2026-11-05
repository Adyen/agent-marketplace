package hookpolicy_test

import (
	"testing"

	harnesscompile "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: hook-resources/shipped-catalogs-pass-the-check
func TestCursorCatalogMatcherKindsMatchDocumentedMatcherConfiguration(t *testing.T) {
	want := map[string]string{
		"beforeReadFile":     hookpolicy.MatcherKindTool,
		"afterFileEdit":      hookpolicy.MatcherKindTool,
		"beforeTabFileRead":  hookpolicy.MatcherKindLifecycle,
		"afterTabFileEdit":   hookpolicy.MatcherKindLifecycle,
		"beforeSubmitPrompt": hookpolicy.MatcherKindLifecycle,
		"stop":               hookpolicy.MatcherKindLifecycle,
		"afterAgentResponse": hookpolicy.MatcherKindLifecycle,
		"afterAgentThought":  hookpolicy.MatcherKindLifecycle,
	}
	for event, kind := range want {
		if got := hookpolicy.NativeEventMatcherKind(harnesscompile.TargetCursor, event); got != kind {
			t.Errorf("NativeEventMatcherKind(cursor, %q) = %q, want %q", event, got, kind)
		}
	}
}

// spec: hook-resources/cursor-receives-native-event-names
func TestCursorNativeMatcherStringsPassThroughSameHarness(t *testing.T) {
	for _, tc := range []struct{ event, matcher string }{
		{"beforeReadFile", "Read"},
		{"afterFileEdit", "Write"},
		{"beforeTabFileRead", "TabRead"},
		{"afterTabFileEdit", "TabWrite"},
		{"beforeSubmitPrompt", "UserPromptSubmit"},
		{"stop", "Stop"},
		{"afterAgentResponse", "AgentResponse"},
		{"afterAgentThought", "AgentThought"},
	} {
		t.Run(tc.event, func(t *testing.T) {
			hook := ir.NewHookWithMatcherOptions("fixture", tc.event, tc.matcher, "echo fixture-cursor-matcher", "command", nil, ir.SourceRef{}, ir.WithHookSource("cursor"))
			res, err := harnesscompile.Compile(hook, harnesscompile.TargetCursor)
			if err != nil {
				t.Fatal(err)
			}
			if diag.HasErrors(res.Diagnostics) {
				t.Fatalf("unexpected error diagnostics: %+v", res.Diagnostics)
			}
			if len(res.Artifacts) != 1 {
				t.Fatalf("want exactly one artifact, got %d", len(res.Artifacts))
			}
			got := decodeFixtureHook(t, "cursor", res.Artifacts[0].Bytes)
			if got.event != tc.event || got.matcher != tc.matcher {
				t.Fatalf("native cursor event/matcher = %q/%q, want %q/%q", got.event, got.matcher, tc.event, tc.matcher)
			}
		})
	}
}

// spec: hook-resources/untranslatable-matchers-suppress-the-hook
func TestCrossHarnessMatcherOnUndocumentedSourceEventIsRefused(t *testing.T) {
	for _, event := range []string{"UserPromptSubmit", "Stop"} {
		t.Run("refused/"+event, func(t *testing.T) {
			hook := ir.NewHookWithMatcherOptions("fixture", event, "Bash", "echo fixture-ignored-matcher", "command", nil, ir.SourceRef{}, ir.WithHookSource("claude"))
			res, err := harnesscompile.Compile(hook, harnesscompile.TargetCursor)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Artifacts) != 0 {
				t.Fatalf("a refused hook must emit zero artifacts, got %d", len(res.Artifacts))
			}
			if !hasFixtureErrorCode(res, diag.CodeHookMatcherUntranslatable) {
				t.Fatalf("want %q, diagnostics = %+v", diag.CodeHookMatcherUntranslatable, res.Diagnostics)
			}
		})
		t.Run("accepted-empty-matcher/"+event, func(t *testing.T) {
			hook := ir.NewHookWithMatcherOptions("fixture", event, "", "echo fixture-no-matcher", "command", nil, ir.SourceRef{}, ir.WithHookSource("claude"))
			res, err := harnesscompile.Compile(hook, harnesscompile.TargetCursor)
			if err != nil {
				t.Fatal(err)
			}
			if diag.HasErrors(res.Diagnostics) || len(res.Artifacts) != 1 {
				t.Fatalf("empty matcher must compile: %+v", res.Diagnostics)
			}
		})
	}
}
