package harnesscompile

import (
	"regexp"
	"strconv"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// hookTimeoutToken matches an emitted JSON timeout member and captures its
// literal digits, so exactness is checked on the bytes rather than through a
// float64 decode.
var hookTimeoutToken = regexp.MustCompile(`"timeout":\s*(-?[0-9]+)`)

func compileTimeoutHook(t *testing.T, target Target, event string, timeout int, source string) *Result {
	t.Helper()
	matcher := "Bash"
	if event != "PreToolUse" {
		matcher = ""
	}
	options := []ir.HookOption{ir.WithHookTimeoutPtr(&timeout)}
	if source != "" {
		options = append(options, ir.WithHookSource(source))
	}
	hook := ir.NewHookWithMatcherOptions("guard", event, matcher, "echo check", "command", nil, ir.SourceRef{}, options...)
	result, err := Compile(hook, target)
	if err != nil {
		t.Fatalf("Compile(hook, %s): %v", target, err)
	}
	return result
}

func emittedTimeout(t *testing.T, result *Result) string {
	t.Helper()
	var tokens []string
	for _, artifact := range result.Artifacts {
		for _, match := range hookTimeoutToken.FindAllSubmatch(artifact.Bytes, -1) {
			tokens = append(tokens, string(match[1]))
		}
	}
	if len(tokens) != 1 {
		t.Fatalf("emitted timeout tokens = %q, want exactly one (diagnostics %+v)", tokens, result.Diagnostics)
	}
	return tokens[0]
}

// spec: hook-resources/zero-timeouts-pass-through-unchanged
func TestZeroHookTimeoutsPassThroughUnchanged(t *testing.T) {
	for _, target := range []Target{TargetClaude, TargetFactory, TargetCursor, TargetGemini, TargetDevin, TargetCodex} {
		t.Run(string(target), func(t *testing.T) {
			result := compileTimeoutHook(t, target, "PreToolUse", 0, "")
			if diag.HasErrors(result.Diagnostics) {
				t.Fatalf("zero timeout refused on %s: %+v", target, result.Diagnostics)
			}
			if got := emittedTimeout(t, result); got != "0" {
				t.Fatalf("emitted timeout = %s, want 0", got)
			}
		})
	}
}

// spec: hook-resources/timeouts-below-a-documented-minimum-are-raised
func TestCodexSessionEndTimeoutBelowMinimumIsRaised(t *testing.T) {
	result := compileTimeoutHook(t, TargetCodex, "SessionEnd", 0, "codex")
	if got := emittedTimeout(t, result); got != "1" {
		t.Fatalf("emitted SessionEnd timeout = %s, want the documented minimum 1", got)
	}
	if !diag.HasCode(result.Diagnostics, diag.CodeCodexFieldDropped) || diag.HasErrors(result.Diagnostics) {
		t.Fatalf("want only the %s warning; diagnostics = %+v", diag.CodeCodexFieldDropped, result.Diagnostics)
	}
}

// spec: hook-resources/the-largest-exact-timeout-is-emitted-exactly
func TestLargestExactHookTimeoutIsEmittedExactly(t *testing.T) {
	for _, tc := range []struct {
		target  Target
		timeout int
		want    string
	}{
		{TargetClaude, 9007199254740991, "9007199254740991"},
		{TargetFactory, 9007199254740991, "9007199254740991"},
		{TargetCursor, 9007199254740991, "9007199254740991"},
		{TargetDevin, 9007199254740991, "9007199254740991"},
		{TargetCodex, 9007199254740991, "9007199254740991"},
		{TargetGemini, 9007199254740, "9007199254740000"},
	} {
		t.Run(string(tc.target), func(t *testing.T) {
			result := compileTimeoutHook(t, tc.target, "PreToolUse", tc.timeout, "")
			if diag.HasErrors(result.Diagnostics) {
				t.Fatalf("representable timeout refused: %+v", result.Diagnostics)
			}
			if got := emittedTimeout(t, result); got != tc.want {
				t.Fatalf("emitted timeout = %s, want exactly %s", got, tc.want)
			}
		})
	}
}

// spec: hook-resources/timeouts-beyond-the-exact-integer-range-are-refused
func TestHookTimeoutsBeyondTheExactRangeAreRefused(t *testing.T) {
	for _, tc := range []struct {
		target  Target
		timeout int
	}{
		{TargetClaude, 9007199254740992},
		{TargetFactory, 9007199254740992},
		{TargetCursor, 9007199254740992},
		{TargetDevin, 9007199254740992},
		{TargetCodex, 9007199254740992},
		{TargetGemini, 9007199254741},
		{TargetGemini, 9223372036854776},
	} {
		t.Run(string(tc.target), func(t *testing.T) {
			control := compileTimeoutHook(t, tc.target, "PreToolUse", 30, "")
			if len(control.Artifacts) == 0 || diag.HasErrors(control.Diagnostics) {
				t.Fatalf("control timeout is not representable on %s: %+v", tc.target, control.Diagnostics)
			}
			result := compileTimeoutHook(t, tc.target, "PreToolUse", tc.timeout, "")
			if len(result.Artifacts) != 0 {
				t.Fatalf("timeout %d emitted %d artifacts, want the hook suppressed", tc.timeout, len(result.Artifacts))
			}
			if !diag.HasCode(result.Diagnostics, diag.CodeHookTimeoutUnsupported) {
				t.Fatalf("want %s; diagnostics = %+v", diag.CodeHookTimeoutUnsupported, result.Diagnostics)
			}
			assertHookDiagnosticsValueFree(t, result.Diagnostics, "echo check", strconv.Itoa(tc.timeout))
		})
	}
}
