package codex

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

type secretMarshalError struct{}

func (secretMarshalError) MarshalYAML() (any, error) {
	return nil, errors.New("SUPER_SECRET_TRANSLATION_ERROR")
}

func TestCompilerDirectLogsCompletionAndErrorWithoutPayloads(t *testing.T) {
	previous := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	compiler := Compiler{}
	success := ir.NewSkill("success", "Successful skill", "SUPER_SECRET_SUCCESS_BODY", nil, nil, ir.SourceRef{})
	if _, _, err := compiler.Translate(success); err != nil {
		t.Fatal(err)
	}
	warning := ir.NewSkillWithOptions("warning", "Warning skill", "SUPER_SECRET_WARNING_BODY", nil, nil, ir.SourceRef{}, ir.WithSkillArgumentHint("SUPER_SECRET_ARGUMENT_HINT"))
	if _, diagnostics, err := compiler.Translate(warning); err != nil || !hasDiagnostic(diagnostics, diag.CodeCodexFieldDropped) {
		t.Fatalf("Translate(warning) = %#v, %v", diagnostics, err)
	}
	broken := ir.NewSkill("broken", "Broken skill", "SUPER_SECRET_ERROR_BODY", nil, map[string]any{"broken": secretMarshalError{}}, ir.SourceRef{})
	if _, _, err := compiler.Translate(broken); err == nil {
		t.Fatal("Translate(broken) returned nil error")
	}

	output := logs.String()
	for _, message := range []string{"codex compile start", "codex compile complete", "codex compile failed"} {
		if !strings.Contains(output, message) {
			t.Errorf("logs missing %q:\n%s", message, output)
		}
	}
	if !strings.Contains(output, diag.CodeCodexFieldDropped) {
		t.Errorf("logs missing mapping diagnostic code %q:\n%s", diag.CodeCodexFieldDropped, output)
	}
	for _, secret := range []string{"SUPER_SECRET_SUCCESS_BODY", "SUPER_SECRET_WARNING_BODY", "SUPER_SECRET_ARGUMENT_HINT", "SUPER_SECRET_ERROR_BODY", "SUPER_SECRET_TRANSLATION_ERROR"} {
		if strings.Contains(output, secret) {
			t.Errorf("logs leaked %q:\n%s", secret, output)
		}
	}
}

func TestCompilerNativePaths(t *testing.T) {
	tests := []struct {
		name     string
		resource ir.Resource
		wantPath string
		contains []string
	}{
		{name: "skill", resource: ir.NewSkill("demo", "Demo skill", "Do the work.\n", nil, nil, ir.SourceRef{}), wantPath: ".agents/skills/demo/SKILL.md", contains: []string{"name: demo", "description: Demo skill", "Do the work."}},
		{name: "command", resource: ir.NewCommand("deploy", "Deploy safely", "Deploy now.\n", nil, ir.SourceRef{}), wantPath: ".agents/skills/deploy/SKILL.md", contains: []string{"name: deploy", "x-agent-marketplace-kind: command", "Deploy now."}},
		{name: "subagent", resource: ir.NewSubagent("reviewer", "Review changes", "Review like an owner.", "gpt-5.6-terra", nil, "high", nil, "", nil, ir.SourceRef{}), wantPath: ".codex/agents/reviewer.toml", contains: []string{`name = 'reviewer'`, `model = 'gpt-5.6-terra'`, `model_reasoning_effort = 'high'`, `developer_instructions = 'Review like an owner.'`}},
		{name: "hook", resource: ir.NewHookWithMatcherOptions("check", "PreToolUse", "Bash", "./check.sh", "command", nil, ir.SourceRef{}, ir.WithHookTimeout(9)), wantPath: ".codex/hooks.json", contains: []string{`"PreToolUse"`, `"matcher":"Bash"`, `"command":"./check.sh"`, `"timeout":9`}},
		{name: "mcp", resource: ir.NewMcpServer("docs", "npx", []string{"-y", "docs-mcp"}, map[string]string{"TOKEN": "${TOKEN}"}, nil, ir.SourceRef{}), wantPath: ".codex/config.toml", contains: []string{`[mcp_servers.docs]`, `command = 'npx'`, `args = ['-y', 'docs-mcp']`, `env_vars = ['TOKEN']`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compile.Compile(tt.resource, compile.TargetCodex)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Artifacts) != 1 || result.Artifacts[0].Path != tt.wantPath {
				t.Fatalf("artifacts = %#v, want path %q", result.Artifacts, tt.wantPath)
			}
			if (tt.name == "hook" || tt.name == "mcp") && !result.Artifacts[0].ExternalReconciliationRequired {
				t.Errorf("%s aggregate does not require ownership-safe external reconciliation", tt.name)
			}
			text := string(result.Artifacts[0].Bytes)
			for _, want := range tt.contains {
				if !strings.Contains(text, want) {
					t.Errorf("artifact missing %q:\n%s", want, text)
				}
			}
		})
	}
}

func TestSubagentPreservesNativeReasoningEffort(t *testing.T) {
	subagent := ir.NewSubagent("reviewer", "Review", "Review.", "", nil, "", nil, "", nil, ir.SourceRef{})
	subagent.Ext.Codex = &ext.Codex{ReasoningEffort: "future-effort"}

	result, err := compile.Compile(subagent, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 || len(result.Diagnostics) != 0 {
		t.Fatalf("result = %#v, want native effort preserved without diagnostics", result)
	}
	if !strings.Contains(string(result.Artifacts[0].Bytes), "model_reasoning_effort = 'future-effort'") {
		t.Fatalf("native effort was not emitted:\n%s", result.Artifacts[0].Bytes)
	}
}

func TestSubagentSuppressesInvalidNativeSandboxMode(t *testing.T) {
	subagent := ir.NewSubagent("reviewer", "Review", "Review.", "", nil, "", nil, "", nil, ir.SourceRef{})
	subagent.Ext.Codex = &ext.Codex{SandboxMode: "host-root"}

	result, err := compile.Compile(subagent, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 0 || !hasDiagnostic(result.Diagnostics, diag.CodeSubagentSecurityRestrictionUnsupported) || !diag.HasErrors(result.Diagnostics) {
		t.Fatalf("result = %#v, want invalid sandbox mode suppressed with security diagnostic", result)
	}
}

func TestCompilerSuppressesAuthorityBroadeningLosses(t *testing.T) {
	restrictedSkill := ir.NewSkillWithOptions("restricted", "Restricted", "Body", nil, nil, ir.SourceRef{}, ir.WithSkillAllowedTools([]string{"read"}))
	restrictedAgent := ir.NewSubagent("restricted", "Restricted", "Body", "gpt-5.6-terra", nil, "", []string{"exec"}, "", nil, ir.SourceRef{})
	authenticatedMCP := ir.NewMcpServerWithOptions("private", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"), ir.WithMcpOAuth("client", "secret", "resource"))
	stdioAsHTTP := ir.NewMcpServerWithOptions("stdio-as-http", "server", nil, nil, nil, ir.SourceRef{}, ir.WithMcpTransport("streamable-http"))
	httpAsStdio := ir.NewMcpServerWithOptions("http-as-stdio", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"), ir.WithMcpTransport("stdio"))
	stdioWithHeaders := ir.NewMcpServerWithOptions("stdio-with-headers", "server", nil, nil, nil, ir.SourceRef{}, ir.WithMcpHeaders(map[string]string{"Authorization": "secret"}))
	remoteWithStdioFields := ir.NewMcpServerWithOptions("remote-with-env", "", []string{"--unsafe"}, map[string]string{"TOKEN": "secret"}, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"))
	remoteWithCWD := ir.NewMcpServerWithOptions("remote-with-cwd", "", nil, nil, map[string]any{"cwd": "/srv"}, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"))
	stdioWithOAuthResource := ir.NewMcpServerWithOptions("stdio-with-oauth-resource", "server", nil, nil, nil, ir.SourceRef{}, ir.WithMcpOAuth("", "", "https://example.test/resource"))
	stopWithMatcher := ir.NewHookWithMatcher("stop-filtered", "Stop", "Bash", "./check", "command", nil, ir.SourceRef{})
	promptWithMatcher := ir.NewHookWithMatcher("prompt-filtered", "UserPromptSubmit", "prompt", "./check", "command", nil, ir.SourceRef{})
	for _, resource := range []ir.Resource{restrictedSkill, restrictedAgent, authenticatedMCP, stdioAsHTTP, httpAsStdio, stdioWithHeaders, remoteWithStdioFields, remoteWithCWD, stdioWithOAuthResource, stopWithMatcher, promptWithMatcher} {
		result, err := compile.Compile(resource, compile.TargetCodex)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Artifacts) != 0 || !diag.HasErrors(result.Diagnostics) {
			t.Errorf("%s result = %#v, want suppressed artifact and error diagnostic", resource.Kind(), result)
		}
	}
}

func TestMCPOutputIsValidTOMLAndPreservesDisabledAndHeaders(t *testing.T) {
	server := ir.NewMcpServerWithOptions("docs", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"), ir.WithMcpHeaders(map[string]string{"X.Header": "value"}), ir.WithMcpDisabled(true))
	result, err := compile.Compile(server, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("result = %#v", result)
	}
	var decoded map[string]any
	if err := toml.Unmarshal(result.Artifacts[0].Bytes, &decoded); err != nil {
		t.Fatalf("invalid TOML: %v\n%s", err, result.Artifacts[0].Bytes)
	}
	text := string(result.Artifacts[0].Bytes)
	for _, want := range []string{"enabled = false", `'X.Header' = 'value'`} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}

func TestMCPMapsEnvironmentBackedHeaders(t *testing.T) {
	server := ir.NewMcpServerWithOptions("docs", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"), ir.WithMcpHeaders(map[string]string{"Authorization": "${env:DOCS_TOKEN}", "X-Literal": "value"}))
	result, err := compile.Compile(server, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 || diag.HasErrors(result.Diagnostics) {
		t.Fatalf("result = %#v", result)
	}
	text := string(result.Artifacts[0].Bytes)
	for _, want := range []string{"[mcp_servers.docs.env_http_headers]", "Authorization = 'DOCS_TOKEN'", "[mcp_servers.docs.http_headers]", "X-Literal = 'value'"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "${env:DOCS_TOKEN}") {
		t.Fatalf("output retained unsupported inline header interpolation:\n%s", text)
	}
}

func TestMCPSuppressesUnrepresentableInterpolation(t *testing.T) {
	servers := []*ir.McpServer{
		ir.NewMcpServerWithOptions("private", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"), ir.WithMcpHeaders(map[string]string{"Authorization": "${file:/secret/token}"})),
		ir.NewMcpServer("cwd", "server", nil, nil, map[string]any{"cwd": "${env:WORK_DIR}"}, ir.SourceRef{}),
	}
	for _, server := range servers {
		result, err := compile.Compile(server, compile.TargetCodex)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Artifacts) != 0 || !diag.HasErrors(result.Diagnostics) || !hasDiagnostic(result.Diagnostics, diag.CodeMcpCodexSecurityUnsupported) {
			t.Fatalf("%s result = %#v, want suppressed MCP with security diagnostic", server.Name(), result)
		}
	}
}

func TestMCPSuppressesInconsistentNativeSidecar(t *testing.T) {
	server := ir.NewMcpServer("mixed", "server", nil, nil, nil, ir.SourceRef{})
	server.Ext.Codex = &ext.Codex{MCP: &ext.CodexMCP{Command: "server", URL: "https://example.test/mcp"}}
	result, err := compile.Compile(server, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 0 || !diag.HasErrors(result.Diagnostics) || !hasDiagnostic(result.Diagnostics, diag.CodeMcpCodexSecurityUnsupported) {
		t.Fatalf("result = %#v, want inconsistent native MCP sidecar suppressed", result)
	}
}

func TestMCPSuppressesExperimentalEnvironmentForURL(t *testing.T) {
	server := ir.NewMcpServerWithOptions("remote", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"))
	server.Ext.Codex = &ext.Codex{MCP: &ext.CodexMCP{URL: "https://example.test/mcp", ExperimentalEnvironment: "local"}}
	result, err := compile.Compile(server, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 0 || !diag.HasErrors(result.Diagnostics) || !hasDiagnostic(result.Diagnostics, diag.CodeMcpCodexSecurityUnsupported) {
		t.Fatalf("result = %#v, want URL placement mismatch suppressed", result)
	}
}

func TestMCPSuppressesInvalidNativeEnvVar(t *testing.T) {
	tests := []struct {
		name   string
		native *ext.CodexMCP
	}{
		{name: "empty local name", native: &ext.CodexMCP{Command: "server", EnvVars: []string{""}}},
		{name: "empty remote name", native: &ext.CodexMCP{Command: "server", RemoteEnvVars: []string{""}}},
		{name: "invalid name", native: &ext.CodexMCP{Command: "server", RemoteEnvVars: []string{"NOT-AN-ENV"}}},
		{name: "literal collision", native: &ext.CodexMCP{Command: "server", Env: map[string]string{"TOKEN": "literal"}, RemoteEnvVars: []string{"TOKEN"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := ir.NewMcpServer("invalid-env", "server", nil, nil, nil, ir.SourceRef{})
			server.Ext.Codex = &ext.Codex{MCP: tt.native}
			result, err := compile.Compile(server, compile.TargetCodex)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Artifacts) != 0 || !diag.HasErrors(result.Diagnostics) || !hasDiagnostic(result.Diagnostics, diag.CodeMcpCodexSecurityUnsupported) {
				t.Fatalf("result = %#v, want invalid native remote env var suppressed", result)
			}
		})
	}
}

func TestMCPRemoteEnvVarsRequireRemoteExecution(t *testing.T) {
	tests := []struct {
		name   string
		native *ext.CodexMCP
		want   string
	}{
		{
			name: "remote source without remote execution",
			native: &ext.CodexMCP{
				Command:       "server",
				RemoteEnvVars: []string{"REMOTE_TOKEN"},
			},
		},
		{
			name: "unsupported execution environment",
			native: &ext.CodexMCP{
				Command:                 "server",
				ExperimentalEnvironment: "elsewhere",
			},
		},
		{
			name: "local execution",
			native: &ext.CodexMCP{
				Command:                 "server",
				ExperimentalEnvironment: "local",
			},
			want: "experimental_environment = 'local'",
		},
		{
			name: "remote execution",
			native: &ext.CodexMCP{
				Command:                 "server",
				ExperimentalEnvironment: "remote",
				RemoteEnvVars:           []string{"REMOTE_TOKEN"},
			},
			want: "experimental_environment = 'remote'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := ir.NewMcpServer("remote-env", "server", nil, nil, nil, ir.SourceRef{})
			server.Ext.Codex = &ext.Codex{MCP: tt.native}
			result, err := compile.Compile(server, compile.TargetCodex)
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				if len(result.Artifacts) != 0 || !diag.HasErrors(result.Diagnostics) || !hasDiagnostic(result.Diagnostics, diag.CodeMcpCodexSecurityUnsupported) {
					t.Fatalf("result = %#v, want invalid native remote execution suppressed", result)
				}
				return
			}
			if len(result.Artifacts) != 1 || diag.HasErrors(result.Diagnostics) || !strings.Contains(string(result.Artifacts[0].Bytes), tt.want) {
				t.Fatalf("result = %#v, want %q", result, tt.want)
			}
		})
	}
}

func TestMCPPreservesOAuthResource(t *testing.T) {
	server := ir.NewMcpServerWithOptions("private", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"), ir.WithMcpOAuth("", "", "https://example.test/resource"))
	result, err := compile.Compile(server, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 || diag.HasErrors(result.Diagnostics) || !strings.Contains(string(result.Artifacts[0].Bytes), "oauth_resource = 'https://example.test/resource'") {
		t.Fatalf("result = %#v, want represented OAuth resource", result)
	}
}

func TestMCPPreservesWorkingDirectory(t *testing.T) {
	server := ir.NewMcpServer("docs", "docs-server", nil, nil, map[string]any{"cwd": "/srv/docs"}, ir.SourceRef{})
	result, err := compile.Compile(server, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 || diag.HasErrors(result.Diagnostics) || !strings.Contains(string(result.Artifacts[0].Bytes), "cwd = '/srv/docs'") {
		t.Fatalf("result = %#v, want represented working directory", result)
	}
	if hasDiagnostic(result.Diagnostics, diag.CodeCodexFieldDropped) {
		t.Fatalf("represented working directory reported as dropped: %#v", result.Diagnostics)
	}
}

func TestUnsupportedRuleAndHookProduceStableDiagnostics(t *testing.T) {
	resources := []ir.Resource{
		ir.NewRule("rule", "", "Body", nil, false, nil, ir.SourceRef{}),
		ir.NewHook("prompt", "PreToolUse", "", "prompt", nil, ir.SourceRef{}),
		ir.NewHook("event", "MadeUpEvent", "./check", "command", nil, ir.SourceRef{}),
	}
	for _, resource := range resources {
		result, err := compile.Compile(resource, compile.TargetCodex)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Artifacts) != 0 || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code == "" {
			t.Errorf("result = %#v, want stable diagnostic and no artifact", result)
		}
	}
}

// spec: hook-resources/factory-devin-and-codex-suppress-unmappable-events-with-an-error
func TestHookEventsMatchCodexDocumentation(t *testing.T) {
	supported := []string{
		"PreToolUse",
		"PostToolUse",
		"UserPromptSubmit",
		"Stop",
		"SessionStart",
	}
	for _, event := range supported {
		t.Run(event, func(t *testing.T) {
			result, err := compile.Compile(ir.NewHook("test-hook", event, "./check", "command", nil, ir.SourceRef{}), compile.TargetCodex)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Artifacts) != 1 || diag.HasErrors(result.Diagnostics) {
				t.Fatalf("result = %#v, want one artifact and no error diagnostic", result)
			}
			if !strings.Contains(string(result.Artifacts[0].Bytes), `"`+event+`"`) {
				t.Errorf("artifact does not contain event %q: %s", event, result.Artifacts[0].Bytes)
			}
		})
	}

	for _, event := range []string{"PermissionRequest", "SubagentStart", "SubagentStop", "PreCompact", "PostCompact", "SessionEnd"} {
		t.Run(event+"_native_only", func(t *testing.T) {
			hook := ir.NewHook("test-hook", event, "./check", "command", nil, ir.SourceRef{})
			generic, err := compile.Compile(hook, compile.TargetCodex)
			if err != nil {
				t.Fatal(err)
			}
			if len(generic.Artifacts) != 0 || !diag.HasErrors(generic.Diagnostics) {
				t.Fatalf("generic result = %#v, want semantic mismatch suppressed", generic)
			}
			hook.Ext.Codex = &ext.Codex{NativeHook: true}
			native, err := compile.Compile(hook, compile.TargetCodex)
			if err != nil {
				t.Fatal(err)
			}
			if len(native.Artifacts) != 1 || diag.HasErrors(native.Diagnostics) || !strings.Contains(string(native.Artifacts[0].Bytes), `"`+event+`"`) {
				t.Fatalf("native result = %#v, want native event preserved", native)
			}
		})
	}

	for _, event := range []string{"Notification", "PostCompaction", "MadeUpEvent"} {
		t.Run(event+"_unsupported", func(t *testing.T) {
			result, err := compile.Compile(ir.NewHook("test-hook", event, "./check", "command", nil, ir.SourceRef{}), compile.TargetCodex)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Artifacts) != 0 || !diag.HasErrors(result.Diagnostics) {
				t.Fatalf("result = %#v, want suppressed artifact and error diagnostic", result)
			}
			if result.Diagnostics[0].Code != diag.CodeHookCodexUnsupported {
				t.Errorf("diagnostic code = %q, want %q", result.Diagnostics[0].Code, diag.CodeHookCodexUnsupported)
			}
		})
	}
}

func TestHookSuppressesInvalidNativeAdditionalContextLimit(t *testing.T) {
	invalid := -1
	hook := ir.NewHook("check", "PreToolUse", "./check", "command", nil, ir.SourceRef{})
	hook.Ext.Codex = &ext.Codex{NativeHook: true, AdditionalContextLimit: &invalid}
	result, err := compile.Compile(hook, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 0 || !diag.HasErrors(result.Diagnostics) || !hasDiagnostic(result.Diagnostics, diag.CodeHookCodexUnsupported) {
		t.Fatalf("result = %#v, want invalid native context limit suppressed", result)
	}
}

// spec: hook-resources/negative-timeouts-are-refused-for-devin-and-codex
func TestHookSuppressesNegativeTimeout(t *testing.T) {
	hook := ir.NewHookWithMatcherOptions("check", "PreToolUse", "", "./check", "command", nil, ir.SourceRef{}, ir.WithHookTimeout(-1))
	result, err := compile.Compile(hook, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 0 || !diag.HasErrors(result.Diagnostics) || !hasDiagnostic(result.Diagnostics, diag.CodeHookCodexUnsupported) {
		t.Fatalf("result = %#v, want negative timeout suppressed", result)
	}
}

func TestNativeSessionEndDoesNotEmitAsyncTrue(t *testing.T) {
	async := true
	hook := ir.NewHook("cleanup", "SessionEnd", "./cleanup", "command", nil, ir.SourceRef{})
	hook.Ext.Codex = &ext.Codex{NativeHook: true, HookAsync: &async}
	result, err := compile.Compile(hook, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 || strings.Contains(string(result.Artifacts[0].Bytes), `"async"`) || !hasDiagnostic(result.Diagnostics, diag.CodeCodexFieldDropped) || diag.HasErrors(result.Diagnostics) {
		t.Fatalf("result = %#v, want synchronous output plus normalization warning", result)
	}
}

// spec: hook-resources/codex-caps-sessionend-timeouts
func TestNativeSessionEndCapsTimeout(t *testing.T) {
	hook := ir.NewHookWithMatcherOptions("cleanup", "SessionEnd", "", "./cleanup", "command", nil, ir.SourceRef{}, ir.WithHookTimeout(30))
	hook.Ext.Codex = &ext.Codex{NativeHook: true}
	result, err := compile.Compile(hook, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 || !strings.Contains(string(result.Artifacts[0].Bytes), `"timeout":3`) || !hasDiagnostic(result.Diagnostics, diag.CodeCodexFieldDropped) || diag.HasErrors(result.Diagnostics) {
		t.Fatalf("result = %#v, want timeout capped at three seconds plus normalization warning", result)
	}
}

func TestSubagentOmitsKnownUnmappableModel(t *testing.T) {
	agent := ir.NewSubagent("reviewer", "Review", "Review carefully.", "sonnet", nil, "", nil, "", nil, ir.SourceRef{})
	result, err := compile.Compile(agent, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 || strings.Contains(string(result.Artifacts[0].Bytes), "model =") {
		t.Fatalf("known cross-vendor model was emitted as a Codex model: %#v", result.Artifacts)
	}
	if !hasDiagnostic(result.Diagnostics, diag.CodeModelUnresolved) {
		t.Fatalf("diagnostics = %#v, want %q", result.Diagnostics, diag.CodeModelUnresolved)
	}
}

// spec: hook-resources/codex-devin-and-windsurf-receive-the-source-bundle-root
// spec: hook-resources/missing-plugin-root-leaves-the-token-with-a-warning
func TestHookPluginRootResolution(t *testing.T) {
	hook := ir.NewHook("check", "PreToolUse", "${CLAUDE_PLUGIN_ROOT}/check.sh", "command", nil, ir.SourceRef{})
	resolved, err := compile.Compile(hook, compile.TargetCodex, compile.WithPluginRoot("/plugins/demo"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(resolved.Artifacts[0].Bytes); !strings.Contains(got, "/plugins/demo/check.sh") || strings.Contains(got, "CLAUDE_PLUGIN_ROOT") {
		t.Fatalf("resolved hook = %s", got)
	}

	unresolved, err := compile.Compile(hook, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(unresolved.Diagnostics, "hook.codex.plugin-root-unresolved") || !strings.Contains(string(unresolved.Artifacts[0].Bytes), "${CLAUDE_PLUGIN_ROOT}") {
		t.Fatalf("unresolved hook result = %#v", unresolved)
	}
}

func TestMCPPluginRootResolution(t *testing.T) {
	server := ir.NewMcpServer("demo", "${CLAUDE_PLUGIN_ROOT}/server", []string{"--root", "${CLAUDE_PLUGIN_ROOT}/data"}, map[string]string{"ROOT": "${CLAUDE_PLUGIN_ROOT}/env"}, map[string]any{"cwd": "${CLAUDE_PLUGIN_ROOT}/work"}, ir.SourceRef{})
	resolved, err := compile.Compile(server, compile.TargetCodex, compile.WithPluginRoot("/plugins/demo"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(resolved.Artifacts[0].Bytes)
	for _, want := range []string{"/plugins/demo/server", "/plugins/demo/data", "/plugins/demo/env", "/plugins/demo/work"} {
		if !strings.Contains(got, want) {
			t.Errorf("resolved MCP missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "CLAUDE_PLUGIN_ROOT") {
		t.Fatalf("resolved MCP retained plugin-root token:\n%s", got)
	}

	unresolved, err := compile.Compile(server, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(unresolved.Diagnostics, "mcp.codex.plugin-root-unresolved") {
		t.Fatalf("diagnostics = %#v", unresolved.Diagnostics)
	}
}

func hasDiagnostic(diagnostics []diag.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
