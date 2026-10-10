package harnesscompile

import (
	"encoding/json"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

// decodeObject unmarshals an artifact body into a JSON object.
func decodeObject(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil || document == nil {
		t.Fatalf("artifact body is not a JSON object: %v\n%s", err, data)
	}
	return document
}

// wantProjectionArtifact asserts the fragment contract every projected-target
// control shares: the declared path, private mode 0600, an externally
// reconciled fragment carrying no ownership marker.
func wantProjectionArtifact(t *testing.T, res *Result, path string) Artifact {
	t.Helper()
	if len(res.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(res.Artifacts))
	}
	art := res.Artifacts[0]
	if art.Path != path {
		t.Fatalf("path = %q, want %q", art.Path, path)
	}
	if art.Mode != 0o600 {
		t.Fatalf("mode = %o, want 600", art.Mode)
	}
	if !art.Fragment || art.FragmentKey != path || art.Reconciliation.Mode != ReconciliationExternal || !art.ExternalReconciliationRequired {
		t.Fatalf("fragment/reconciliation = %+v, want external fragment keyed by path", art)
	}
	if art.ManagedByField != "" || art.ManagedByValue != "" {
		t.Fatalf("managed marker set on native projection: %+v", art)
	}
	return art
}

// errorCodeDiagnostics returns the error-severity diagnostics carrying code.
func errorCodeDiagnostics(diagnostics []diag.Diagnostic, code string) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range diagnostics {
		if d.Code == code && d.Severity == diag.SeverityError {
			out = append(out, d)
		}
	}
	return out
}

// wantExactRefusal asserts the projection emitted no artifact and carries
// exactly one error diagnostic with the stable code.
func wantExactRefusal(t *testing.T, res *Result, err error, code string) {
	t.Helper()
	if err != nil {
		t.Fatalf("CompileNativeHookProjection: %v", err)
	}
	if len(res.Artifacts) != 0 {
		t.Fatalf("artifacts = %+v, want none on refusal", res.Artifacts)
	}
	matching := errorCodeDiagnostics(res.Diagnostics, code)
	if len(matching) != 1 {
		t.Fatalf("diagnostics = %+v, want exactly one error %q", res.Diagnostics, code)
	}
	d := matching[0]
	if d.Target == "" || d.Resource == "" {
		t.Fatalf("diagnostic %+v lacks target/resource attribution", d)
	}
}

// TestIndependentNativeHookProjectionUsesCatalogs proves the pure native hook
// projection resolves event, matcher, type, timeout and command through the
// shipped hook/tool catalogs and serializes the exact native rule under each
// target's declared envelope and shape. All expected values are independently
// authored literals taken from the documented harness layouts, not derived by
// calling the projection.
func TestIndependentNativeHookProjectionUsesCatalogs(t *testing.T) {
	source := ir.SourceRef{File: "hooks/hooks.json"}

	t.Run("factory matcher-groups native file", func(t *testing.T) {
		hook := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "echo witness", "command", nil, source)
		res, err := CompileNativeHookProjection(hook, TargetFactory, ScopeProject, "hook-support/guard")
		if err != nil {
			t.Fatalf("CompileNativeHookProjection: %v", err)
		}
		if res.HasErrors() {
			t.Fatalf("unexpected error diagnostics: %+v", res.Diagnostics)
		}
		art := wantProjectionArtifact(t, res, "hooks.json")
		document := decodeObject(t, art.Bytes)
		if len(document) != 1 {
			t.Fatalf("document keys = %v, want only PreToolUse", document)
		}
		groups, ok := document["PreToolUse"].([]any)
		if !ok || len(groups) != 1 {
			t.Fatalf("PreToolUse = %v, want exactly one rule group", document["PreToolUse"])
		}
		group, ok := groups[0].(map[string]any)
		if !ok {
			t.Fatalf("rule group is %T, want object", groups[0])
		}
		if group["matcher"] != "Execute" {
			t.Fatalf("group matcher = %v, want native Execute", group["matcher"])
		}
		handlers, ok := group["hooks"].([]any)
		if !ok || len(handlers) != 1 {
			t.Fatalf("group hooks = %v, want exactly one handler", group["hooks"])
		}
		handler, ok := handlers[0].(map[string]any)
		if !ok {
			t.Fatalf("handler is %T, want object", handlers[0])
		}
		want := map[string]any{"type": "command", "command": "echo witness"}
		if len(handler) != len(want) {
			t.Fatalf("handler = %v, want exactly %v", handler, want)
		}
		for key, value := range want {
			if handler[key] != value {
				t.Fatalf("handler[%q] = %v, want %v (handler %v)", key, handler[key], value, handler)
			}
		}
	})

	t.Run("claude settings hooks member", func(t *testing.T) {
		hook := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "echo witness", "command", nil, source)
		res, err := CompileNativeHookProjection(hook, TargetClaude, ScopeProject, "hook-support/guard")
		if err != nil {
			t.Fatalf("CompileNativeHookProjection: %v", err)
		}
		if res.HasErrors() {
			t.Fatalf("unexpected error diagnostics: %+v", res.Diagnostics)
		}
		art := wantProjectionArtifact(t, res, "settings.json")
		document := decodeObject(t, art.Bytes)
		if len(document) != 1 {
			t.Fatalf("document keys = %v, want only hooks", document)
		}
		hooks, ok := document["hooks"].(map[string]any)
		if !ok || len(hooks) != 1 {
			t.Fatalf("hooks member = %v, want one event", document["hooks"])
		}
		groups, ok := hooks["PreToolUse"].([]any)
		if !ok || len(groups) != 1 {
			t.Fatalf("PreToolUse = %v, want one rule group", hooks["PreToolUse"])
		}
		group := groups[0].(map[string]any)
		if group["matcher"] != "Bash" {
			t.Fatalf("group matcher = %v, want Bash", group["matcher"])
		}
		handlers := group["hooks"].([]any)
		if len(handlers) != 1 {
			t.Fatalf("handlers = %v, want one", handlers)
		}
		handler := handlers[0].(map[string]any)
		if handler["type"] != "command" || handler["command"] != "echo witness" || len(handler) != 2 {
			t.Fatalf("handler = %v, want {type:command, command:echo witness}", handler)
		}
	})

	t.Run("cursor versioned envelope with flat handler", func(t *testing.T) {
		hook := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "echo witness", "command", nil, source)
		res, err := CompileNativeHookProjection(hook, TargetCursor, ScopeProject, "hook-support/guard")
		if err != nil {
			t.Fatalf("CompileNativeHookProjection: %v", err)
		}
		if res.HasErrors() {
			t.Fatalf("unexpected error diagnostics: %+v", res.Diagnostics)
		}
		art := wantProjectionArtifact(t, res, "hooks.json")
		document := decodeObject(t, art.Bytes)
		if document["version"] != float64(1) {
			t.Fatalf("version = %v, want 1", document["version"])
		}
		if len(document) != 2 {
			t.Fatalf("document keys = %v, want version+hooks", document)
		}
		hooks, ok := document["hooks"].(map[string]any)
		if !ok || len(hooks) != 1 {
			t.Fatalf("hooks = %v, want one event", document["hooks"])
		}
		handlers, ok := hooks["preToolUse"].([]any)
		if !ok || len(handlers) != 1 {
			t.Fatalf("preToolUse = %v, want one handler", hooks["preToolUse"])
		}
		handler := handlers[0].(map[string]any)
		want := map[string]any{"type": "command", "command": "echo witness", "matcher": "^Shell$"}
		if len(handler) != len(want) {
			t.Fatalf("handler = %v, want %v", handler, want)
		}
		for key, value := range want {
			if handler[key] != value {
				t.Fatalf("handler[%q] = %v, want %v (handler %v)", key, handler[key], value, handler)
			}
		}
	})

	t.Run("windsurf event encodes the tool", func(t *testing.T) {
		hook := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "echo witness", "command", nil, source)
		res, err := CompileNativeHookProjection(hook, TargetWindsurf, ScopeProject, "hook-support/guard")
		if err != nil {
			t.Fatalf("CompileNativeHookProjection: %v", err)
		}
		if res.HasErrors() {
			t.Fatalf("unexpected error diagnostics: %+v", res.Diagnostics)
		}
		art := wantProjectionArtifact(t, res, ".devin/hooks.json")
		document := decodeObject(t, art.Bytes)
		hooks, ok := document["hooks"].(map[string]any)
		if !ok || len(hooks) != 1 {
			t.Fatalf("hooks = %v, want one event", document["hooks"])
		}
		handlers, ok := hooks["pre_run_command"].([]any)
		if !ok || len(handlers) != 1 {
			t.Fatalf("pre_run_command = %v, want one handler (document %v)", hooks["pre_run_command"], document)
		}
		handler := handlers[0].(map[string]any)
		if len(handler) != 1 || handler["command"] != "echo witness" {
			t.Fatalf("handler = %v, want only {command: echo witness}", handler)
		}
	})

	t.Run("factory-native source keeps group extras", func(t *testing.T) {
		extra := map[string]any{"commandRegex": "^make"}
		hook := ir.NewHookWithMatcherOptions("native", "PreToolUse", "Execute", "make lint", "command", extra, source, ir.WithHookSource("factory"))
		res, err := CompileNativeHookProjection(hook, TargetFactory, ScopeProject, "hook-support/native")
		if err != nil {
			t.Fatalf("CompileNativeHookProjection: %v", err)
		}
		if res.HasErrors() {
			t.Fatalf("unexpected error diagnostics: %+v", res.Diagnostics)
		}
		art := wantProjectionArtifact(t, res, "hooks.json")
		document := decodeObject(t, art.Bytes)
		groups := document["PreToolUse"].([]any)
		group := groups[0].(map[string]any)
		if group["matcher"] != "Execute" {
			t.Fatalf("group matcher = %v, want native Execute retained", group["matcher"])
		}
		if group["commandRegex"] != "^make" {
			t.Fatalf("group commandRegex = %v, want ^make preserved at group level", group["commandRegex"])
		}
		handlers := group["hooks"].([]any)
		handler := handlers[0].(map[string]any)
		if _, leaked := handler["commandRegex"]; leaked {
			t.Fatalf("commandRegex leaked into handler %v", handler)
		}
		if handler["command"] != "make lint" || handler["type"] != "command" {
			t.Fatalf("handler = %v, want command make lint", handler)
		}
	})

	t.Run("unrepresentable source refuses without artifacts", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			hook *ir.Hook
			code string
		}{
			{"negative timeout", ir.NewHookWithOptions("bad", "PreToolUse", "echo x", "command", nil, source, ir.WithHookTimeout(-5)), diag.CodeHookTimeoutUnsupported},
			{"undeclared canonical event", ir.NewHook("bad", "NoSuchEvent", "echo x", "command", nil, source), diag.CodeHookEventUnmappable},
			{"prompt payload on command-only target", ir.NewHookWithOptions("bad", "PreToolUse", "echo x", "command", nil, source, ir.WithHookPrompt("secret prompt")), diag.CodeHookPromptUnsupported},
			{"variable with no target equivalent", ir.NewHook("bad", "PreToolUse", "echo $CLAUDE_CODE_REMOTE", "command", nil, source), diag.CodeHookEnvUnrepresentable},
		} {
			t.Run(tc.name, func(t *testing.T) {
				res, err := CompileNativeHookProjection(tc.hook, TargetFactory, ScopeProject, "hook-support/bad")
				wantExactRefusal(t, res, err, tc.code)
			})
		}
		t.Run("windsurf variable with no target equivalent", func(t *testing.T) {
			hook := ir.NewHookWithMatcher("bad", "PreToolUse", "Bash", "echo $CLAUDE_CODE_REMOTE", "command", nil, source)
			res, err := CompileNativeHookProjection(hook, TargetWindsurf, ScopeProject, "hook-support/bad")
			wantExactRefusal(t, res, err, diag.CodeHookEnvUnrepresentable)
		})
	})

	t.Run("plugin-root reference without a support base is refused", func(t *testing.T) {
		hook := ir.NewHookWithMatcher("bad", "PreToolUse", "Bash", "${CLAUDE_PLUGIN_ROOT}/run.sh", "command", nil, source)
		res, err := CompileNativeHookProjection(hook, TargetFactory, ScopeProject, "")
		wantExactRefusal(t, res, err, diag.CodeHookSupportPathUnrepresentable)
	})

	t.Run("invalid resource and cross-source codex guards apply", func(t *testing.T) {
		bad := ir.NewHook("ok", "PreToolUse", "echo x", "command", nil, source)
		bad.SetName("9invalid")
		if _, err := CompileNativeHookProjection(bad, TargetFactory, ScopeProject, "hook-support/ok"); err == nil {
			t.Fatal("invalid resource name must fail at the projection boundary")
		}
		windows := `cmd /c echo x`
		codexOnly := ir.NewHook("ok", "PreToolUse", "echo x", "command", nil, source)
		codexOnly.Ext.Codex = &extpkg.Codex{CommandWindows: &windows}
		res, err := CompileNativeHookProjection(codexOnly, TargetFactory, ScopeProject, "hook-support/ok")
		wantExactRefusal(t, res, err, diag.CodeHookCodexNativeFieldsUnsupported)
		literal := ir.NewHook("ok", "Stop", "run ${TOKEN}", "command", nil, source)
		literal.Ext.Codex = &extpkg.Codex{NativeHook: true}
		res, err = CompileNativeHookProjection(literal, TargetClaude, ScopeProject, "hook-support/ok")
		wantExactRefusal(t, res, err, diag.CodeHookNativeLiteralUnsupported)
	})

	t.Run("contradictory preserved sidecar execution values are refused", func(t *testing.T) {
		claude := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "echo witness", "command", nil, source)
		claude.Ext.Claude = &extpkg.Claude{HookType: "http"}
		res, err := CompileNativeHookProjection(claude, TargetClaude, ScopeProject, "hook-support/guard")
		wantExactRefusal(t, res, err, diag.CodeHookClaudeUnsupportedType)

		cursor := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "echo witness", "command", nil, source)
		cursor.Ext.Cursor = &extpkg.Cursor{HookType: "prompt"}
		res, err = CompileNativeHookProjection(cursor, TargetCursor, ScopeProject, "hook-support/guard")
		wantExactRefusal(t, res, err, diag.CodeHookCursorUnsupportedType)

		timeout := 5
		gemini := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "echo witness", "command", nil, source)
		gemini.Ext.Gemini = &extpkg.Gemini{HookTimeout: &timeout}
		res, err = CompileNativeHookProjection(gemini, TargetGemini, ScopeProject, "hook-support/guard")
		wantExactRefusal(t, res, err, diag.CodeHookTimeoutUnsupported)
	})

	t.Run("identical supported sidecar values are preserved", func(t *testing.T) {
		claude := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "echo witness", "command", nil, source)
		claude.Ext.Claude = &extpkg.Claude{HookType: "command"}
		res, err := CompileNativeHookProjection(claude, TargetClaude, ScopeProject, "hook-support/guard")
		if err != nil || res.HasErrors() {
			t.Fatalf("CompileNativeHookProjection: %v %+v", err, res.Diagnostics)
		}
		art := wantProjectionArtifact(t, res, "settings.json")
		document := decodeObject(t, art.Bytes)
		handler := document["hooks"].(map[string]any)["PreToolUse"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
		if handler["type"] != "command" {
			t.Fatalf("handler type = %v, want command", handler)
		}

		timeout := 30000
		gemini := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Bash", "echo witness", "command", nil, source, ir.WithHookTimeout(30))
		gemini.Ext.Gemini = &extpkg.Gemini{HookTimeout: &timeout}
		res, err = CompileNativeHookProjection(gemini, TargetGemini, ScopeProject, "hook-support/guard")
		if err != nil || res.HasErrors() {
			t.Fatalf("CompileNativeHookProjection: %v %+v", err, res.Diagnostics)
		}
		art = wantProjectionArtifact(t, res, "settings.json")
		document = decodeObject(t, art.Bytes)
		handler = document["hooks"].(map[string]any)["BeforeTool"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
		if handler["timeout"] != float64(30000) {
			t.Fatalf("handler = %v, want preserved identical timeout 30000", handler)
		}
	})

	t.Run("support base must be a clean scope-shaped path", func(t *testing.T) {
		hook := func() *ir.Hook {
			return ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "echo witness", "command", nil, source)
		}
		for _, bad := range []string{"/abs/base", "../escape", "a//b", `a\b`, "a\x00b", "."} {
			if _, err := CompileNativeHookProjection(hook(), TargetFactory, ScopeProject, bad); err == nil {
				t.Fatalf("repository support base %q accepted", bad)
			}
		}
		for _, bad := range []string{"rel/base", "/abs/../x", `/abs\bad`, "/x\x00y"} {
			if _, err := CompileNativeHookProjection(hook(), TargetFactory, ScopeUser, bad); err == nil {
				t.Fatalf("machine support base %q accepted", bad)
			}
		}
		res, err := CompileNativeHookProjection(hook(), TargetFactory, ScopeUser, "/opt/hook-support")
		if err != nil || res.HasErrors() || len(res.Artifacts) != 1 {
			t.Fatalf("machine absolute support base: err=%v diags=%+v artifacts=%+v", err, res.Diagnostics, res.Artifacts)
		}
		res, err = CompileNativeHookProjection(hook(), TargetFactory, ScopeProject, "")
		if err != nil || res.HasErrors() || len(res.Artifacts) != 1 {
			t.Fatalf("empty support base without a plugin reference: err=%v diags=%+v artifacts=%+v", err, res.Diagnostics, res.Artifacts)
		}
	})

	t.Run("reserved and case-colliding extras cannot override typed fields", func(t *testing.T) {
		extra := map[string]any{
			"Command": "echo injected",
			"MATCHER": "Write",
			"note":    "kept",
		}
		hook := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "echo witness", "command", extra, source)
		res, err := CompileNativeHookProjection(hook, TargetClaude, ScopeProject, "hook-support/guard")
		if err != nil {
			t.Fatalf("CompileNativeHookProjection: %v", err)
		}
		if len(res.Artifacts) != 1 {
			t.Fatalf("artifacts = %d, want 1", len(res.Artifacts))
		}
		document := decodeObject(t, res.Artifacts[0].Bytes)
		hooks := document["hooks"].(map[string]any)
		group := hooks["PreToolUse"].([]any)[0].(map[string]any)
		if group["matcher"] != "Bash" {
			t.Fatalf("group matcher = %v, want typed matcher Bash; case-colliding MATCHER extra must not override", group["matcher"])
		}
		handler := group["hooks"].([]any)[0].(map[string]any)
		if handler["command"] != "echo witness" {
			t.Fatalf("handler command = %v, want echo witness; case-colliding Command extra must not override", handler["command"])
		}
		if handler["note"] != "kept" {
			t.Fatalf("meaningful extra note missing from handler %v", handler)
		}
	})

	t.Run("reserved ownership metadata is refused", func(t *testing.T) {
		for _, key := range []string{"_managed_by", "_Managed_By"} {
			hook := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "echo witness", "command", map[string]any{key: "x"}, source)
			res, err := CompileNativeHookProjection(hook, TargetClaude, ScopeProject, "hook-support/guard")
			if err == nil && len(res.Artifacts) != 0 {
				t.Fatalf("extra %q produced artifact %+v; reserved ownership metadata must never be emitted", key, res.Artifacts)
			}
		}
	})

	t.Run("unknown target and missing hook are errors", func(t *testing.T) {
		if _, err := CompileNativeHookProjection(nil, TargetClaude, ScopeProject, ""); err == nil {
			t.Fatal("nil hook must fail")
		}
		if _, err := CompileNativeHookProjection(ir.NewHook("h", "Stop", "echo x", "command", nil, source), Target("nosuch"), ScopeProject, ""); err == nil {
			t.Fatal("unknown target must fail")
		}
	})

	t.Run("default Compile output is unchanged", func(t *testing.T) {
		hook := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", "echo witness", "command", nil, source)
		res, err := Compile(hook, TargetFactory)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if len(res.Artifacts) != 1 {
			t.Fatalf("default artifacts = %+v, want the single legacy fragment", res.Artifacts)
		}
		art := res.Artifacts[0]
		if art.Path != "hooks/hooks.json" {
			t.Fatalf("default Compile path = %q, want the legacy hooks/hooks.json fragment", art.Path)
		}
		document := decodeObject(t, art.Bytes)
		if _, ok := document["PreToolUse"]; !ok {
			t.Fatalf("legacy fragment = %v, want top-level event map", document)
		}
	})
}
