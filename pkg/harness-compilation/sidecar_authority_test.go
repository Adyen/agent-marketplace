package harnesscompile

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	toml "github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

func decodePayload(t *testing.T, kind, payload string) ir.Resource {
	t.Helper()
	resource, err := artifact.DecodeResource(kind, []byte(payload))
	if err != nil {
		t.Fatalf("decode %s payload: %v", kind, err)
	}
	return resource
}

func requireSuppressed(t *testing.T, result *Result, code string) {
	t.Helper()
	if len(result.Artifacts) != 0 {
		t.Fatalf("contradictory sidecar still published %d artifact(s): %s", len(result.Artifacts), result.Artifacts[0].Bytes)
	}
	for _, d := range result.Diagnostics {
		if d.Code == code && d.Severity == diag.SeverityError {
			return
		}
	}
	t.Fatalf("want error %s, got %+v", code, result.Diagnostics)
}

func subagentFrontmatterTools(t *testing.T, data []byte) []any {
	t.Helper()
	block, _, found := strings.Cut(strings.TrimPrefix(string(data), "---\n"), "\n---\n")
	if !found {
		t.Fatalf("frontmatter missing: %q", data)
	}
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(block), &fields); err != nil {
		t.Fatalf("native YAML parser rejected frontmatter: %v", err)
	}
	tools, _ := fields["tools"].([]any)
	return tools
}

// spec: harness-compilation/subagent-tool-sidecar-cannot-widen-the-canonical-tool-list
func TestSubagentToolSidecarCannotWidenCanonicalTools(t *testing.T) {
	resource := decodePayload(t, "subagent", `{"name":"reviewer","Description":"Review only","Body":"Inert body.","Tools":["Read"],"Ext":{"claude":{"tools":["Bash"]},"cursor":{"tools":["Bash"]}}}`)
	for _, target := range []Target{TargetClaude, TargetCursor} {
		t.Run(string(target), func(t *testing.T) {
			result, err := Compile(resource, target)
			if err != nil {
				t.Fatal(err)
			}
			requireSuppressed(t, result, diag.CodeSubagentSecurityRestrictionUnsupported)
		})
	}
}

// spec: harness-compilation/subagent-tool-sidecar-may-narrow-the-canonical-tool-list
func TestSubagentToolSidecarMayNarrowCanonicalTools(t *testing.T) {
	resource := decodePayload(t, "subagent", `{"name":"reviewer","Description":"Review only","Body":"Inert body.","Tools":["Read","Grep"],"Ext":{"claude":{"tools":["Read"]},"cursor":{"tools":["Read"]}}}`)
	for target, path := range map[Target]string{TargetClaude: "agents/reviewer.md", TargetCursor: "agents/reviewer.md"} {
		t.Run(string(target), func(t *testing.T) {
			result, err := Compile(resource, target)
			if err != nil {
				t.Fatal(err)
			}
			if result.HasErrors() {
				t.Fatalf("narrowing sidecar refused: %+v", result.Diagnostics)
			}
			var data []byte
			for _, a := range result.Artifacts {
				if a.Path == path {
					data = a.Bytes
				}
			}
			if data == nil {
				t.Fatalf("subagent missing: %+v", result.Artifacts)
			}
			if tools := subagentFrontmatterTools(t, data); !reflect.DeepEqual(tools, []any{"Read"}) {
				t.Fatalf("tools = %#v, want the narrowed [Read]", tools)
			}
		})
	}
}

// spec: harness-compilation/hook-type-sidecar-cannot-activate-a-command
func TestHookTypeSidecarCannotActivateCommand(t *testing.T) {
	resource := decodePayload(t, "hook", `{"name":"guard","Event":"PreToolUse","Type":"prompt","Command":"/bin/echo inert","Ext":{"claude":{"hookType":"command"},"cursor":{"hookType":"command"}}}`)
	for target, code := range map[Target]string{TargetClaude: diag.CodeHookClaudeUnsupportedType, TargetCursor: diag.CodeHookCursorUnsupportedType} {
		t.Run(string(target), func(t *testing.T) {
			result, err := Compile(resource, target)
			if err != nil {
				t.Fatal(err)
			}
			requireSuppressed(t, result, code)
		})
	}

	equal := decodePayload(t, "hook", `{"name":"guard","Event":"PreToolUse","Type":"command","Command":"/bin/echo inert","Ext":{"claude":{"hookType":"command"},"cursor":{"hookType":"command"}}}`)
	for _, target := range []Target{TargetClaude, TargetCursor} {
		result, err := Compile(equal, target)
		if err != nil || result.HasErrors() || len(result.Artifacts) == 0 {
			t.Fatalf("%s: equal hook-type sidecar refused: %v %+v", target, err, result)
		}
	}
}

// spec: harness-compilation/codex-mcp-sidecar-must-match-the-canonical-server
func TestCodexMCPSidecarMustMatchCanonicalServer(t *testing.T) {
	resource := decodePayload(t, "mcpserver", `{"name":"docs","Command":"safe-mcp","Args":["--serve"],"Ext":{"codex":{"mcp":{"command":"/bin/echo","args":["inert"],"envVars":["DUMMY_FORWARDED"]}}}}`)
	result, err := Compile(resource, TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	requireSuppressed(t, result, diag.CodeMcpCodexSecurityUnsupported)
}

// spec: harness-compilation/codex-native-mcp-servers-still-round-trip
func TestCodexNativeMCPServersStillRoundTrip(t *testing.T) {
	src := fstest.MapFS{".codex/config.toml": {Data: []byte("[mcp_servers.docs]\ncommand = 'docs-mcp'\nargs = ['--serve']\nenv_vars = ['DUMMY_FORWARDED']\n[mcp_servers.docs.env]\nMODE = 'inert'\n")}}
	result, err := Convert(src, HarnessCodex, TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() {
		t.Fatalf("native Codex MCP server refused: %+v", result.Diagnostics)
	}
	var server map[string]any
	for _, a := range result.Artifacts {
		if a.Path != ".codex/config.toml" {
			continue
		}
		var decoded map[string]map[string]map[string]any
		if err := toml.Unmarshal(a.Bytes, &decoded); err != nil {
			t.Fatalf("native TOML parser rejected MCP output: %v\n%s", err, a.Bytes)
		}
		server = decoded["mcp_servers"]["docs"]
	}
	if server == nil {
		t.Fatalf("Codex MCP server missing: %+v", result.Artifacts)
	}
	want := map[string]any{"command": "docs-mcp", "args": []any{"--serve"}, "env_vars": []any{"DUMMY_FORWARDED"}, "env": map[string]any{"MODE": "inert"}}
	if !reflect.DeepEqual(server, want) {
		t.Fatalf("Codex MCP round trip = %#v, want %#v", server, want)
	}
}

// spec: harness-compilation/modeled-mcp-fields-cannot-arrive-through-extension-fields
func TestModeledMCPFieldsCannotArriveThroughExtensionFields(t *testing.T) {
	server := ir.NewMcpServer("docs", "", nil, nil, map[string]any{"command": "/bin/echo", "args": []any{"inert"}, "URL": "https://example.invalid/mcp", "disabled": true, "keep": "me"}, ir.SourceRef{})
	for _, target := range []Target{TargetClaude, TargetCursor, TargetFactory, TargetGemini} {
		t.Run(string(target), func(t *testing.T) {
			result, err := Compile(server, target)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("artifacts = %+v", result.Artifacts)
			}
			var document struct {
				Servers map[string]map[string]any `json:"mcpServers"`
			}
			if err := json.Unmarshal(result.Artifacts[0].Bytes, &document); err != nil {
				t.Fatalf("native JSON parser rejected MCP output: %v", err)
			}
			entry := document.Servers["docs"]
			for key := range entry {
				if strings.EqualFold(key, "command") || strings.EqualFold(key, "args") || strings.EqualFold(key, "url") {
					t.Fatalf("extension field %q became an active MCP setting: %#v", key, entry)
				}
			}
			if entry["keep"] != "me" || entry["disabled"] != true {
				t.Fatalf("unrelated or disabling extension field lost: %#v", entry)
			}
			if !diag.HasCode(result.Diagnostics, diag.CodeCanonicalFieldOverridesExtra) {
				t.Fatalf("reserved MCP fields dropped without a diagnostic: %+v", result.Diagnostics)
			}
		})
	}
}
