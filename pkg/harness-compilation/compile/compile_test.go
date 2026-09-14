package compile

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

// mockCompiler is a test Compiler that returns fixed artifacts.
type mockCompiler struct {
	target Target
}

func (m *mockCompiler) Target() Target { return m.target }
func (m *mockCompiler) Translate(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
	return []artifact.Artifact{
		{Path: r.Name() + ".out", Bytes: []byte("mock")},
	}, nil, nil
}

type optionAwareMockCompiler struct {
	mockCompiler
	pluginRoot string
}

func (m *optionAwareMockCompiler) TranslateWithOptions(r ir.Resource, opts Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	m.pluginRoot = opts.PluginRoot()
	return m.Translate(r)
}

// mockResource is a minimal ir.Resource for testing.
type mockResource struct {
	kind ir.ResourceKind
	name string
}

func (m *mockResource) Kind() ir.ResourceKind { return m.kind }
func (m *mockResource) Name() string          { return m.name }

func TestRegisterAndCompile(t *testing.T) {
	// Use a separate registry for this test to avoid polluting the global one.
	orig := registry
	registry = map[Target]Compiler{}
	defer func() { registry = orig }()

	c := &mockCompiler{target: TargetClaude}
	Register(c)

	r := &mockResource{kind: ir.KindSkill, name: "test-skill"}
	res, err := Compile(r, TargetClaude)
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("Compile() artifacts = %d, want 1", len(res.Artifacts))
	}
	if res.Artifacts[0].Path != "test-skill.out" {
		t.Errorf("Compile() artifact path = %q, want %q", res.Artifacts[0].Path, "test-skill.out")
	}
}

func TestRegister_DuplicatePanics(t *testing.T) {
	orig := registry
	registry = map[Target]Compiler{}
	defer func() { registry = orig }()

	c1 := &mockCompiler{target: TargetFactory}
	c2 := &mockCompiler{target: TargetFactory}
	Register(c1)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Register(duplicate) did not panic")
		}
	}()
	Register(c2)
}

func TestCompile_UnregisteredTarget(t *testing.T) {
	orig := registry
	registry = map[Target]Compiler{}
	defer func() { registry = orig }()

	r := &mockResource{kind: ir.KindSkill, name: "test"}
	_, err := Compile(r, TargetGemini)
	if err == nil {
		t.Fatal("Compile(unregistered) should return an error")
	}
}

func TestCompile_WithOptions(t *testing.T) {
	orig := registry
	registry = map[Target]Compiler{}
	defer func() { registry = orig }()

	c := &mockCompiler{target: TargetClaude}
	Register(c)

	r := &mockResource{kind: ir.KindCommand, name: "cmd"}
	res, err := Compile(r, TargetClaude,
		WithModelOverride(map[string]string{"sonnet": "custom-model"}),
		WithToolOverride(map[string]string{"Write": "CustomWrite"}),
	)
	if err != nil {
		t.Fatalf("Compile() with options error: %v", err)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("Compile() with options artifacts = %d, want 1", len(res.Artifacts))
	}
}

func TestLookupPreservesOptionAwareCompiler(t *testing.T) {
	orig := registry
	underlying := &optionAwareMockCompiler{mockCompiler: mockCompiler{target: TargetClaude}}
	registry = map[Target]Compiler{TargetClaude: underlying}
	defer func() { registry = orig }()

	lookedUp, ok := Lookup(TargetClaude)
	if !ok {
		t.Fatal("Lookup(TargetClaude) did not find compiler")
	}
	configurable, ok := lookedUp.(OptionAwareCompiler)
	if !ok {
		t.Fatalf("Lookup() compiler type = %T, want OptionAwareCompiler", lookedUp)
	}
	resource := ir.NewSubagent("reviewer", "Review", "Review.", "", nil, "", nil, "", nil, ir.SourceRef{})
	resource.Ext.Codex = &ext.Codex{ReasoningEffort: "xhigh"}
	var opts Options
	WithPluginRoot("/plugins/demo")(&opts)
	artifacts, diagnostics, err := configurable.TranslateWithOptions(resource, opts)
	if err != nil {
		t.Fatal(err)
	}
	if underlying.pluginRoot != "/plugins/demo" || len(artifacts) != 1 || !diag.HasCode(diagnostics, diag.CodeSubagentCodexEffortDropped) {
		t.Fatalf("TranslateWithOptions() = %#v, %#v, plugin root %q", artifacts, diagnostics, underlying.pluginRoot)
	}
}

func TestCompileSuppressesNativeCodexAuthorityLoss(t *testing.T) {
	tests := []struct {
		name     string
		resource ir.Resource
		code     string
	}{
		{
			name: "subagent sandbox",
			resource: func() ir.Resource {
				r := ir.NewSubagent("restricted", "Restricted", "Review.", "", nil, "", nil, "", nil, ir.SourceRef{})
				r.Ext.Codex = &ext.Codex{SandboxMode: "read-only"}
				return r
			}(),
			code: diag.CodeSubagentSecurityRestrictionUnsupported,
		},
		{
			name: "MCP environment-backed authentication",
			resource: func() ir.Resource {
				r := ir.NewMcpServerWithOptions("restricted", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"))
				r.Ext.Codex = &ext.Codex{BearerTokenEnvVar: "TOKEN"}
				return r
			}(),
			code: diag.CodeMcpSecurityRestrictionUnsupported,
		},
		{
			name: "native MCP literal interpolation",
			resource: func() ir.Resource {
				r := ir.NewMcpServer("native", "${env:COMMAND}", nil, nil, nil, ir.SourceRef{})
				r.Ext.Codex = &ext.Codex{MCP: &ext.CodexMCP{Command: "${env:COMMAND}"}}
				return r
			}(),
			code: diag.CodeMcpSecurityRestrictionUnsupported,
		},
		{
			name: "native MCP cwd literal interpolation",
			resource: func() ir.Resource {
				r := ir.NewMcpServer("native", "server", nil, nil, map[string]any{"cwd": "${env:CWD}"}, ir.SourceRef{})
				r.Ext.Codex = &ext.Codex{MCP: &ext.CodexMCP{Command: "server", CWD: "${env:CWD}"}}
				return r
			}(),
			code: diag.CodeMcpSecurityRestrictionUnsupported,
		},
		{
			name: "native MCP remote environment source",
			resource: func() ir.Resource {
				r := ir.NewMcpServer("native", "server", nil, nil, nil, ir.SourceRef{})
				r.Ext.Codex = &ext.Codex{MCP: &ext.CodexMCP{Command: "server", RemoteEnvVars: []string{"REMOTE_TOKEN"}}}
				return r
			}(),
			code: diag.CodeMcpSecurityRestrictionUnsupported,
		},
		{
			name: "native MCP remote execution environment",
			resource: func() ir.Resource {
				r := ir.NewMcpServer("native", "server", nil, nil, nil, ir.SourceRef{})
				r.Ext.Codex = &ext.Codex{MCP: &ext.CodexMCP{Command: "server", ExperimentalEnvironment: "remote"}}
				return r
			}(),
			code: diag.CodeMcpSecurityRestrictionUnsupported,
		},
		{
			name: "native MCP execution sidecar mismatch",
			resource: func() ir.Resource {
				r := ir.NewMcpServer("native", "canonical", nil, nil, nil, ir.SourceRef{})
				r.Ext.Codex = &ext.Codex{MCP: &ext.CodexMCP{Command: "native"}}
				return r
			}(),
			code: diag.CodeMcpSecurityRestrictionUnsupported,
		},
		{
			name: "native MCP authentication sidecar mismatch",
			resource: func() ir.Resource {
				r := ir.NewMcpServerWithOptions("native", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"))
				r.Ext.Codex = &ext.Codex{MCP: &ext.CodexMCP{URL: r.URL, HTTPHeaders: map[string]string{"Authorization": "secret"}}}
				return r
			}(),
			code: diag.CodeMcpSecurityRestrictionUnsupported,
		},
		{
			name: "native MCP environment sidecar mismatch",
			resource: func() ir.Resource {
				r := ir.NewMcpServer("native", "server", nil, nil, nil, ir.SourceRef{})
				r.Ext.Codex = &ext.Codex{MCP: &ext.CodexMCP{Command: r.Command, EnvVars: []string{"TOKEN"}}}
				return r
			}(),
			code: diag.CodeMcpSecurityRestrictionUnsupported,
		},
		{
			name: "native hook execution and context fields",
			resource: func() ir.Resource {
				commandWindows := "check.cmd"
				contextLimit := 1024
				r := ir.NewHook("native-hook", "PreToolUse", "./check", "command", nil, ir.SourceRef{})
				r.Ext.Codex = &ext.Codex{NativeHook: true, CommandWindows: &commandWindows, AdditionalContextLimit: &contextLimit}
				return r
			}(),
			code: diag.CodeHookCodexNativeFieldsUnsupported,
		},
		{
			name: "native asynchronous hook",
			resource: func() ir.Resource {
				async := true
				r := ir.NewHook("native-hook", "PreToolUse", "./check", "command", nil, ir.SourceRef{})
				r.Ext.Codex = &ext.Codex{NativeHook: true, HookAsync: &async}
				return r
			}(),
			code: diag.CodeHookCodexNativeFieldsUnsupported,
		},
	}
	for _, tt := range tests {
		for _, target := range []Target{TargetClaude, TargetFactory, TargetCursor, TargetWindsurf, TargetGemini, TargetDevin} {
			t.Run(tt.name+"/"+string(target), func(t *testing.T) {
				orig := registry
				registry = map[Target]Compiler{target: &mockCompiler{target: target}}
				defer func() { registry = orig }()

				result, err := Compile(tt.resource, target)
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Artifacts) != 0 || !diag.HasCode(result.Diagnostics, tt.code) || !diag.HasErrors(result.Diagnostics) {
					t.Fatalf("Compile() = %#v, want fail-closed %q diagnostic", result, tt.code)
				}
			})
		}
	}
}

func TestLookupCompilerSuppressesNativeCodexAuthorityLoss(t *testing.T) {
	orig := registry
	registry = map[Target]Compiler{TargetClaude: &mockCompiler{target: TargetClaude}}
	defer func() { registry = orig }()

	resource := ir.NewSubagent("restricted", "Restricted", "Review.", "", nil, "", nil, "", nil, ir.SourceRef{})
	resource.Ext.Codex = &ext.Codex{SandboxMode: "read-only"}
	compiler, ok := Lookup(TargetClaude)
	if !ok {
		t.Fatal("Lookup(TargetClaude) did not find compiler")
	}
	artifacts, diagnostics, err := compiler.Translate(resource)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 || !diag.HasCode(diagnostics, diag.CodeSubagentSecurityRestrictionUnsupported) {
		t.Fatalf("Translate() = %#v, %#v, want fail-closed diagnostic", artifacts, diagnostics)
	}
}

func TestLookupCompilerRejectsTypedNilBeforeNativeCodexGuard(t *testing.T) {
	orig := registry
	registry = map[Target]Compiler{TargetDevin: &mockCompiler{target: TargetDevin}}
	defer func() { registry = orig }()

	compiler, ok := Lookup(TargetDevin)
	if !ok {
		t.Fatal("Lookup(TargetDevin) did not find compiler")
	}
	artifacts, diagnostics, err := compiler.Translate((*ir.Subagent)(nil))
	if err == nil {
		t.Fatal("Translate returned nil error, want typed-nil validation error")
	}
	if artifacts != nil || diagnostics != nil {
		t.Fatalf("Translate() = %#v, %#v, want no output", artifacts, diagnostics)
	}
}

func TestCompileWarnsWhenDroppingNativeCodexEffort(t *testing.T) {
	const wantCode = diag.CodeSubagentCodexEffortDropped
	for _, target := range []Target{TargetClaude, TargetFactory, TargetCursor, TargetWindsurf, TargetGemini, TargetDevin} {
		t.Run(string(target), func(t *testing.T) {
			orig := registry
			registry = map[Target]Compiler{target: &mockCompiler{target: target}}
			defer func() { registry = orig }()

			resource := ir.NewSubagent("reviewer", "Review", "Review.", "", nil, "", nil, "", nil, ir.SourceRef{})
			resource.Ext.Codex = &ext.Codex{ReasoningEffort: "xhigh"}
			result, err := Compile(resource, target)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Artifacts) != 1 || !diag.HasCode(result.Diagnostics, wantCode) || diag.HasErrors(result.Diagnostics) {
				t.Fatalf("Compile() = %#v, want artifact plus %q warning", result, wantCode)
			}
		})
	}
}

func TestCompileWarnsWhenDroppingNativeCodexLocalMCPPlacement(t *testing.T) {
	for _, target := range []Target{TargetClaude, TargetFactory, TargetCursor, TargetWindsurf, TargetGemini, TargetDevin} {
		t.Run(string(target), func(t *testing.T) {
			orig := registry
			registry = map[Target]Compiler{target: &mockCompiler{target: target}}
			defer func() { registry = orig }()

			resource := ir.NewMcpServer("local", "server", nil, nil, nil, ir.SourceRef{})
			resource.Ext.Codex = &ext.Codex{MCP: &ext.CodexMCP{Command: "server", ExperimentalEnvironment: "local"}}
			result, err := Compile(resource, target)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Artifacts) != 1 || !diag.HasCode(result.Diagnostics, diag.CodeCodexFieldDropped) || diag.HasErrors(result.Diagnostics) {
				t.Fatalf("Compile() = %#v, want artifact plus benign placement warning", result)
			}
		})
	}
}

func TestLookupCompilerWarnsWhenDroppingNativeCodexEffort(t *testing.T) {
	const wantCode = diag.CodeSubagentCodexEffortDropped
	orig := registry
	registry = map[Target]Compiler{TargetClaude: &mockCompiler{target: TargetClaude}}
	defer func() { registry = orig }()

	resource := ir.NewSubagent("reviewer", "Review", "Review.", "", nil, "", nil, "", nil, ir.SourceRef{})
	resource.Ext.Codex = &ext.Codex{ReasoningEffort: "minimal"}
	compiler, ok := Lookup(TargetClaude)
	if !ok {
		t.Fatal("Lookup(TargetClaude) did not find compiler")
	}
	artifacts, diagnostics, err := compiler.Translate(resource)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 || !diag.HasCode(diagnostics, wantCode) || diag.HasErrors(diagnostics) {
		t.Fatalf("Translate() = %#v, %#v, want artifact plus %q warning", artifacts, diagnostics, wantCode)
	}
}

func TestCompileWarnsWhenDroppingNativeCodexModelAndEffort(t *testing.T) {
	for _, target := range []Target{TargetClaude, TargetFactory, TargetCursor, TargetWindsurf, TargetGemini, TargetDevin} {
		t.Run(string(target), func(t *testing.T) {
			orig := registry
			registry = map[Target]Compiler{target: &mockCompiler{target: target}}
			defer func() { registry = orig }()

			resource := ir.NewSubagent("reviewer", "Review", "Review.", "", nil, "", nil, "", nil, ir.SourceRef{})
			resource.Ext.Codex = &ext.Codex{Model: "gpt-native", ReasoningEffort: "xhigh"}
			result, err := Compile(resource, target)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Artifacts) != 1 || !diag.HasCode(result.Diagnostics, diag.CodeSubagentCodexModelDropped) || !diag.HasCode(result.Diagnostics, diag.CodeSubagentCodexEffortDropped) || diag.HasErrors(result.Diagnostics) {
				t.Fatalf("Compile() = %#v, want artifact plus native model and effort warnings", result)
			}
		})
	}
}

func TestCompileSuppressesUnmappableAndNativeLiteralHooks(t *testing.T) {
	tests := []struct {
		name string
		hook *ir.Hook
		code string
	}{
		{
			name: "Codex-only event with benign metadata",
			hook: func() *ir.Hook {
				status := "Compacted"
				r := ir.NewHook("post-compact", "PostCompact", "check", "command", nil, ir.SourceRef{})
				r.Ext.Codex = &ext.Codex{NativeHook: true, HookStatusMessage: &status}
				return r
			}(),
			code: diag.CodeHookEventUnmappable,
		},
		{
			name: "same name but different PreCompact lifecycle",
			hook: func() *ir.Hook {
				r := ir.NewHook("pre-compact", "PreCompact", "check", "command", nil, ir.SourceRef{})
				r.Ext.Codex = &ext.Codex{NativeHook: true}
				return r
			}(),
			code: diag.CodeHookEventUnmappable,
		},
		{
			name: "same name but different SubagentStop lifecycle",
			hook: func() *ir.Hook {
				r := ir.NewHook("subagent-stop", "SubagentStop", "check", "command", nil, ir.SourceRef{})
				r.Ext.Codex = &ext.Codex{NativeHook: true}
				return r
			}(),
			code: diag.CodeHookEventUnmappable,
		},
		{
			name: "native literal plugin token with benign scheduling metadata",
			hook: func() *ir.Hook {
				async := false
				r := ir.NewHook("native", "PreToolUse", "${CLAUDE_PLUGIN_ROOT}/native", "command", nil, ir.SourceRef{})
				r.Ext.Codex = &ext.Codex{NativeHook: true, HookAsync: &async}
				return r
			}(),
			code: diag.CodeHookNativeLiteralUnsupported,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := registry
			registry = map[Target]Compiler{TargetClaude: &mockCompiler{target: TargetClaude}}
			defer func() { registry = orig }()

			result, err := Compile(tt.hook, TargetClaude, WithPluginRoot("/plugins/demo"))
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Artifacts) != 0 || !diag.HasCode(result.Diagnostics, tt.code) || !diag.HasErrors(result.Diagnostics) {
				t.Fatalf("Compile() = %#v, want fail-closed %q diagnostic", result, tt.code)
			}
		})
	}
}

func TestCompileWarnsWhenDroppingBenignNativeCodexHookMetadata(t *testing.T) {
	async := false
	status := "Checking"
	hook := ir.NewHook("native", "PreToolUse", "check", "command", nil, ir.SourceRef{})
	hook.Ext.Codex = &ext.Codex{NativeHook: true, HookAsync: &async, HookStatusMessage: &status}
	orig := registry
	registry = map[Target]Compiler{TargetClaude: &mockCompiler{target: TargetClaude}}
	defer func() { registry = orig }()

	result, err := Compile(hook, TargetClaude)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 || !diag.HasCode(result.Diagnostics, diag.CodeCodexFieldDropped) || diag.HasErrors(result.Diagnostics) {
		t.Fatalf("Compile() = %#v, want artifact plus benign field-loss warning", result)
	}
}

func TestCompilerInterface(t *testing.T) {
	// Verify the Compiler interface is satisfied by mockCompiler.
	var _ Compiler = &mockCompiler{}
}

func TestMcpCredentialDiagnostics_StrictInterpolation(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		wantSecret bool
	}{
		{name: "environment token", value: "${API_TOKEN}", wantSecret: false},
		{name: "embedded environment token", value: "Bearer ${API_TOKEN}", wantSecret: false},
		{name: "env interpolation", value: "${env:API_TOKEN}", wantSecret: false},
		{name: "file interpolation", value: "${file:/run/secrets/token}", wantSecret: false},
		{name: "bare dollar literal", value: "$literal", wantSecret: true},
		{name: "malformed interpolation", value: "${API TOKEN}", wantSecret: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := ir.NewMcpServer("credentials", "server", nil, map[string]string{"API_TOKEN": tt.value}, nil, ir.SourceRef{})
			diags := McpCredentialDiagnostics(server, TargetClaude)
			if got := len(diags) > 0; got != tt.wantSecret {
				t.Fatalf("diagnostics = %#v, want literal=%v", diags, tt.wantSecret)
			}
			if len(diags) == 0 {
				return
			}
			if diags[0].Code != diag.CodeMcpLiteralCredential {
				t.Fatalf("code = %q, want %q", diags[0].Code, diag.CodeMcpLiteralCredential)
			}
			if strings.Contains(diags[0].Message, tt.value) || strings.Contains(diags[0].Hint, tt.value) {
				t.Fatal("credential value leaked into the diagnostic")
			}
		})
	}
}

func TestMcpCredentialDiagnostics_CanonicalExtras(t *testing.T) {
	const literalSecret = "literal-extension-secret"
	server := ir.NewMcpServer("credentials", "server", nil, nil, map[string]any{
		"env": map[string]any{
			"API_TOKEN": literalSecret,
		},
		"headers": map[string]any{
			"Authorization": "Bearer " + literalSecret,
		},
		"oauthClientSecret": literalSecret,
		"x-extension":       map[string]any{"preserve": true},
	}, ir.SourceRef{})

	for _, target := range []Target{TargetClaude, TargetFactory, TargetCursor, TargetWindsurf, TargetGemini, TargetDevin, TargetCodex} {
		t.Run(string(target), func(t *testing.T) {
			diags := McpCredentialDiagnostics(server, target)
			if len(diags) != 1 || diags[0].Code != diag.CodeMcpLiteralCredential {
				t.Fatalf("diagnostics = %#v, want one literal-secret diagnostic", diags)
			}
			if strings.Contains(diags[0].Message, literalSecret) || strings.Contains(diags[0].Hint, literalSecret) {
				t.Fatal("credential value leaked into the diagnostic")
			}
		})
	}
}
