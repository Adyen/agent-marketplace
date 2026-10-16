package harnesscompile

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"gopkg.in/yaml.v3"
)

func requireError(t *testing.T, result *Result, code string) {
	t.Helper()
	for _, d := range result.Diagnostics {
		if d.Code == code && d.Severity == diag.SeverityError {
			return
		}
	}
	t.Fatalf("want error %s, got %+v", code, result.Diagnostics)
}

// spec: harness-compilation/gemini-inline-mcp-server-restrictions-round-trip
func TestGeminiInlineMCPServerRestrictionsRoundTrip(t *testing.T) {
	const agent = "---\nname: reviewer\ndescription: Restricted reviewer\nkind: local\nmcpServers:\n  srv:\n    command: /bin/echo\n    args: [inert]\n    includeTools: [safe_tool]\n    excludeTools: [unsafe_tool]\n    timeout: 30000\n---\nInert body.\n"
	src := fstest.MapFS{".gemini/agents/reviewer.md": {Data: []byte(agent)}}
	result, err := Convert(src, HarnessGemini, TargetGemini)
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() {
		t.Fatalf("Gemini round trip refused: %+v", result.Diagnostics)
	}
	var data []byte
	for _, a := range result.Artifacts {
		if strings.HasSuffix(a.Path, "agents/reviewer.md") {
			data = a.Bytes
		}
	}
	block, _, found := strings.Cut(strings.TrimPrefix(string(data), "---\n"), "\n---\n")
	if !found {
		t.Fatalf("subagent frontmatter missing: %q", data)
	}
	var fields struct {
		MCPServers map[string]map[string]any `yaml:"mcpServers"`
	}
	if err := yaml.Unmarshal([]byte(block), &fields); err != nil {
		t.Fatalf("native YAML parser rejected frontmatter: %v", err)
	}
	want := map[string]any{"command": "/bin/echo", "args": []any{"inert"}, "includeTools": []any{"safe_tool"}, "excludeTools": []any{"unsafe_tool"}, "timeout": 30000}
	if got := fields.MCPServers["srv"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("inline server = %#v, want %#v", got, want)
	}
}

const windsurfWorkingDirHooks = `{"hooks":{"pre_user_prompt":[{"command":"./run","working_directory":"/opt/reviewed-hook"}]}}`

// spec: harness-compilation/windsurf-hook-working-directory-round-trips
func TestWindsurfHookWorkingDirectoryRoundTrips(t *testing.T) {
	src := fstest.MapFS{".windsurf/hooks.json": {Data: []byte(windsurfWorkingDirHooks)}}
	result, err := Convert(src, HarnessWindsurf, TargetWindsurf)
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() {
		t.Fatalf("Windsurf round trip refused: %+v", result.Diagnostics)
	}
	for _, a := range result.Artifacts {
		if a.Path != ".windsurf/hooks.json" {
			continue
		}
		var document struct {
			Hooks map[string][]map[string]any `json:"hooks"`
		}
		if err := json.Unmarshal(a.Bytes, &document); err != nil {
			t.Fatal(err)
		}
		if got := document.Hooks["pre_user_prompt"][0]["working_directory"]; got != "/opt/reviewed-hook" {
			t.Fatalf("working_directory = %v, want /opt/reviewed-hook in %s", got, a.Bytes)
		}
		return
	}
	t.Fatalf("Windsurf hook missing: %+v", result.Artifacts)
}

// spec: harness-compilation/targets-without-a-hook-working-directory-refuse-the-hook
func TestTargetsWithoutHookWorkingDirectoryRefuseHook(t *testing.T) {
	src := fstest.MapFS{".windsurf/hooks.json": {Data: []byte(windsurfWorkingDirHooks)}}
	result, err := Convert(src, HarnessWindsurf, TargetClaude)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range result.Artifacts {
		if strings.Contains(string(a.Bytes), "./run") {
			t.Fatalf("hook emitted without its working directory: %s\n%s", a.Path, a.Bytes)
		}
	}
	requireError(t, result, diag.CodeHookWindsurfNativeFieldsUnsupported)
}

// spec: harness-compilation/devin-refuses-an-mcp-server-whose-argument-it-cannot-represent
func TestDevinRefusesMCPServerWithUnrepresentableArgument(t *testing.T) {
	src := fstest.MapFS{".windsurf/mcp_config.json": {Data: []byte(`{"mcpServers":{"policy-server":{"command":"/usr/local/bin/example-mcp","args":["--serve","--sandbox=${file:/repo/policy}"]},"plain":{"command":"/usr/local/bin/plain-mcp","args":["--serve"]}}}`)}}
	result, err := Convert(src, HarnessWindsurf, TargetDevin)
	if err != nil {
		t.Fatal(err)
	}
	requireError(t, result, diag.CodeMcpDevinInterpolationUnrepresentable)
	var plain bool
	for _, a := range result.Artifacts {
		if strings.Contains(string(a.Bytes), "policy-server") {
			t.Fatalf("server emitted without its argument: %s", a.Bytes)
		}
		plain = plain || strings.Contains(string(a.Bytes), "plain-mcp")
	}
	if !plain {
		t.Fatalf("unrelated server lost: %+v", result.Artifacts)
	}
}

func TestGeminiInlineMCPServerPreservedCredentialsAreReported(t *testing.T) {
	const agent = "---\nname: reviewer\ndescription: Restricted reviewer\nkind: local\nmcpServers:\n  srv:\n    httpUrl: https://example.invalid/mcp\n    headers:\n      Authorization: inert-literal-placeholder\n---\nInert body.\n"
	result, err := Convert(fstest.MapFS{".gemini/agents/reviewer.md": {Data: []byte(agent)}}, HarnessGemini, TargetGemini)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range result.Diagnostics {
		if d.Code == diag.CodeMcpLiteralCredential {
			if strings.Contains(d.Message, "inert-literal-placeholder") {
				t.Fatalf("credential diagnostic leaks the value: %+v", d)
			}
			return
		}
	}
	t.Fatalf("literal credential in a preserved header not reported: %+v", result.Diagnostics)
}
