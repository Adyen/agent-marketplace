package harnesscompile

// Catalog-driven Gemini hook translation (OpenSpec change
// native-hook-integration, tasks 3.1-3.2). These tests exercise the public
// Compile path, including hooks ingested from a native .gemini/settings.json.

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	ingestgemini "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/gemini"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func hookJSONScalars(t *testing.T, r *Result) []string {
	t.Helper()
	var scalars []string
	var walk func(v any)
	walk = func(v any) {
		switch typed := v.(type) {
		case map[string]any:
			for key, child := range typed {
				scalars = append(scalars, key)
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case string:
			scalars = append(scalars, typed)
		case float64:
			scalars = append(scalars, strconv.FormatFloat(typed, 'f', -1, 64))
		}
	}
	for _, a := range r.Artifacts {
		var doc any
		if err := json.Unmarshal(a.Bytes, &doc); err != nil {
			continue
		}
		walk(doc)
	}
	return scalars
}

func containsScalar(scalars []string, want string) bool {
	for _, s := range scalars {
		if s == want {
			return true
		}
	}
	return false
}

func ingestGeminiHook(t *testing.T, settings string) *ir.Hook {
	t.Helper()
	src := fstest.MapFS{".gemini/settings.json": &fstest.MapFile{Data: []byte(settings)}}
	hooks, diagnostics, err := ingestgemini.LoadHooks(src, ".gemini/settings.json")
	if err != nil || len(hooks) != 1 {
		t.Fatalf("LoadHooks: %v hooks=%+v diags=%+v", err, hooks, diagnostics)
	}
	return hooks[0]
}

// spec: hook-resources/gemini-timeouts-are-converted-to-milliseconds
func TestGeminiTimeoutsAreConvertedToMilliseconds(t *testing.T) {
	timeout := 30
	hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Bash", "echo check", "command", nil, ir.SourceRef{}, ir.WithHookTimeoutPtr(&timeout))
	result, err := Compile(hook, TargetGemini)
	if err != nil {
		t.Fatalf("Compile(hook, gemini): %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("want one artifact, got %d (diags: %+v)", len(result.Artifacts), result.Diagnostics)
	}
	scalars := hookJSONScalars(t, result)
	if !containsScalar(scalars, "BeforeTool") || !containsScalar(scalars, "30000") {
		t.Fatalf("want BeforeTool event and timeout 30000; scalars = %v", scalars)
	}
}

// spec: hook-resources/mcp-matchers-follow-the-target-s-mcp-naming
func TestMCPMatchersFollowTheTargetsMCPNaming(t *testing.T) {
	hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "mcp__github__create_issue", "echo check", "command", nil, ir.SourceRef{})
	result, err := Compile(hook, TargetGemini)
	if err != nil {
		t.Fatalf("Compile(hook, gemini): %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("want one artifact, got %d (diags: %+v)", len(result.Artifacts), result.Diagnostics)
	}
	scalars := hookJSONScalars(t, result)
	if !containsScalar(scalars, "^mcp_github_create_issue$") {
		t.Fatalf("want matcher ^mcp_github_create_issue$; scalars = %v", scalars)
	}
}

// spec: hook-resources/gemini-millisecond-timeouts-round-up-on-ingestion
func TestGeminiMillisecondTimeoutsRoundUpOnIngestion(t *testing.T) {
	hook := ingestGeminiHook(t, `{"hooks":{"BeforeTool":[{"matcher":"run_shell_command","hooks":[{"type":"command","command":"echo a-fairly-distinctive-payload","timeout":2500}]}]}}`)
	result, err := Compile(hook, TargetClaude)
	if err != nil {
		t.Fatalf("Compile(hook, claude): %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("want one artifact, got %d (diags: %+v)", len(result.Artifacts), result.Diagnostics)
	}
	scalars := hookJSONScalars(t, result)
	if !containsScalar(scalars, "PreToolUse") || !containsScalar(scalars, "3") {
		t.Fatalf("want PreToolUse event and timeout 3; scalars = %v", scalars)
	}
	if !diag.HasCode(result.Diagnostics, diag.CodeHookTimeoutRounded) {
		t.Fatalf("want %s diagnostic; diagnostics = %+v", diag.CodeHookTimeoutRounded, result.Diagnostics)
	}
	assertHookDiagnosticsValueFree(t, result.Diagnostics, "a-fairly-distinctive-payload", "2500", "run_shell_command")
}

// spec: hook-resources/gemini-hooks-keep-millisecond-timeouts
func TestGeminiHooksKeepMillisecondTimeouts(t *testing.T) {
	hook := ingestGeminiHook(t, `{"hooks":{"BeforeTool":[{"matcher":"run_shell_command","hooks":[{"type":"command","command":"echo hi","timeout":2500}]}]}}`)
	result, err := Compile(hook, TargetGemini)
	if err != nil {
		t.Fatalf("Compile(hook, gemini): %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("want one artifact, got %d (diags: %+v)", len(result.Artifacts), result.Diagnostics)
	}
	scalars := hookJSONScalars(t, result)
	if !containsScalar(scalars, "BeforeTool") || !containsScalar(scalars, "2500") || !containsScalar(scalars, "run_shell_command") {
		t.Fatalf("want native BeforeTool/matcher/2500 kept; scalars = %v", scalars)
	}
	if diag.HasCode(result.Diagnostics, diag.CodeHookTimeoutRounded) {
		t.Fatalf("unexpected rounding diagnostic: %+v", result.Diagnostics)
	}
}

// assertHookDiagnosticsValueFree proves no diagnostic message or hint quotes a
// hook payload value (native spec: hook diagnostics are value-free).
func assertHookDiagnosticsValueFree(t *testing.T, diagnostics []diag.Diagnostic, payloads ...string) {
	t.Helper()
	for _, d := range diagnostics {
		for _, payload := range payloads {
			if payload == "" {
				continue
			}
			if strings.Contains(d.Message, payload) || strings.Contains(d.Hint, payload) {
				t.Fatalf("diagnostic %s leaks payload %q: %+v", d.Code, payload, d)
			}
		}
	}
}
