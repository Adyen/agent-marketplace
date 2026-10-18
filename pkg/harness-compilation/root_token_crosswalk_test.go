package harnesscompile

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func mcpServerFromArtifacts(t *testing.T, result *Result, path, name string) map[string]any {
	t.Helper()
	for _, a := range result.Artifacts {
		if a.Path != path {
			continue
		}
		var document struct {
			Servers map[string]map[string]any `json:"mcpServers"`
		}
		if err := json.Unmarshal(a.Bytes, &document); err != nil {
			t.Fatalf("native JSON parser rejected %s: %v", path, err)
		}
		if server, ok := document.Servers[name]; ok {
			return server
		}
	}
	t.Fatalf("MCP server %q missing from %s: %+v", name, path, result.Artifacts)
	return nil
}

// spec: harness-compilation/gemini-project-directory-never-becomes-the-plugin-root
func TestGeminiProjectDirectoryNeverBecomesPluginRoot(t *testing.T) {
	src := fstest.MapFS{".gemini/settings.json": {Data: []byte(`{"mcpServers":{"proj":{"command":"node","args":["${GEMINI_PROJECT_DIR}/tools/server.js","${CLAUDE_PROJECT_DIR}/tools/legacy.js","${extensionPath}/bin/helper"]}}}`)}}
	for _, tc := range []struct {
		target Target
		path   string
		opts   []Option
		want   []any
	}{
		{TargetGemini, "gemini-extension.json", nil, []any{"${GEMINI_PROJECT_DIR}/tools/server.js", "${CLAUDE_PROJECT_DIR}/tools/legacy.js", "${extensionPath}/bin/helper"}},
		{TargetClaude, ".mcp.json", nil, []any{"${GEMINI_PROJECT_DIR}/tools/server.js", "${CLAUDE_PROJECT_DIR}/tools/legacy.js", "${CLAUDE_PLUGIN_ROOT}/bin/helper"}},
	} {
		t.Run(string(tc.target), func(t *testing.T) {
			result, err := Convert(src, HarnessGemini, tc.target, tc.opts...)
			if err != nil {
				t.Fatal(err)
			}
			if result.HasErrors() {
				t.Fatalf("conversion refused: %+v", result.Diagnostics)
			}
			if args := mcpServerFromArtifacts(t, result, tc.path, "proj")["args"]; !reflect.DeepEqual(args, tc.want) {
				t.Fatalf("args = %#v, want %#v", args, tc.want)
			}
		})
	}

	t.Run("devin with a literal plugin root", func(t *testing.T) {
		const pluginRoot = "/opt/inert-plugin-root"
		result, err := Convert(src, HarnessGemini, TargetDevin, WithPluginRoot(pluginRoot))
		if err != nil {
			t.Fatal(err)
		}
		args, _ := mcpServerFromArtifacts(t, result, "mcp_config.json", "proj")["args"].([]any)
		if len(args) != 3 {
			t.Fatalf("args = %#v, want three values", args)
		}
		for i, arg := range args[:2] {
			if strings.Contains(arg.(string), pluginRoot) {
				t.Fatalf("project reference %d resolved inside the plugin root: %q", i, arg)
			}
		}
		if args[2] != pluginRoot+"/bin/helper" {
			t.Fatalf("extension reference = %q, want the configured plugin root", args[2])
		}
	})
}

// spec: harness-compilation/factory-plugin-root-round-trips-through-mcp-servers
func TestFactoryPluginRootRoundTripsThroughMCPServers(t *testing.T) {
	src := fstest.MapFS{
		".factory-plugin/plugin.json": {Data: []byte(`{"name":"inert","version":"0.0.1"}`)},
		"mcp.json":                    {Data: []byte(`{"mcpServers":{"docs":{"command":"${DROID_PLUGIN_ROOT}/bin/server","args":["--config","${DROID_PLUGIN_ROOT}/config.json"],"env":{"DATA":"${DROID_PLUGIN_ROOT}/data"}}}}`)},
	}
	for target, tc := range map[Target]struct{ path, token string }{
		TargetFactory: {"mcp.json", "${DROID_PLUGIN_ROOT}"},
		TargetClaude:  {".mcp.json", "${CLAUDE_PLUGIN_ROOT}"},
	} {
		t.Run(string(target), func(t *testing.T) {
			result, err := Convert(src, HarnessFactory, target)
			if err != nil {
				t.Fatal(err)
			}
			server := mcpServerFromArtifacts(t, result, tc.path, "docs")
			want := map[string]any{"command": tc.token + "/bin/server", "args": []any{"--config", tc.token + "/config.json"}, "env": map[string]any{"DATA": tc.token + "/data"}}
			if !reflect.DeepEqual(server, want) {
				t.Fatalf("server = %#v, want %#v", server, want)
			}
		})
	}
}

// spec: harness-compilation/canonical-plugin-root-compiles-to-the-factory-token
func TestCanonicalPluginRootCompilesToFactoryToken(t *testing.T) {
	src := fstest.MapFS{
		".claude-plugin/plugin.json": {Data: []byte(`{"name":"inert","version":"0.0.1"}`)},
		".mcp.json":                  {Data: []byte(`{"mcpServers":{"docs":{"command":"${CLAUDE_PLUGIN_ROOT}/bin/server","args":["--config","${CLAUDE_PLUGIN_ROOT}/config.json"],"env":{"DATA":"${CLAUDE_PLUGIN_ROOT}/data"}}}}`)},
	}
	result, err := Convert(src, HarnessClaude, TargetFactory)
	if err != nil {
		t.Fatal(err)
	}
	server := mcpServerFromArtifacts(t, result, "mcp.json", "docs")
	want := map[string]any{"command": "${DROID_PLUGIN_ROOT}/bin/server", "args": []any{"--config", "${DROID_PLUGIN_ROOT}/config.json"}, "env": map[string]any{"DATA": "${DROID_PLUGIN_ROOT}/data"}}
	if !reflect.DeepEqual(server, want) {
		t.Fatalf("server = %#v, want %#v", server, want)
	}
}

// spec: harness-compilation/gemini-project-directory-never-becomes-the-plugin-root
func TestGeminiProjectDirectoryStaysInPromptsAndBodies(t *testing.T) {
	src := fstest.MapFS{
		"commands/review.toml":       {Data: []byte("description = \"Review\"\nprompt = \"Read ${GEMINI_PROJECT_DIR}/notes.md and ${extensionPath}/guide.md\"\n")},
		".gemini/agents/reviewer.md": {Data: []byte("---\nname: reviewer\ndescription: Reviewer\nkind: local\n---\nRead ${GEMINI_PROJECT_DIR}/notes.md and ${extensionPath}/guide.md\n")},
	}
	for _, target := range []Target{TargetGemini, TargetClaude} {
		t.Run(string(target), func(t *testing.T) {
			result, err := Convert(src, HarnessGemini, target)
			if err != nil {
				t.Fatal(err)
			}
			var prompt, body bool
			for _, a := range result.Artifacts {
				text := string(a.Bytes)
				if !strings.Contains(text, "/notes.md") {
					continue
				}
				if !strings.Contains(text, "${GEMINI_PROJECT_DIR}/notes.md") {
					t.Fatalf("%s: project reference rewritten:\n%s", a.Path, text)
				}
				prompt = prompt || strings.Contains(a.Path, "review.")
				body = body || strings.Contains(a.Path, "reviewer.")
			}
			if !prompt || !body {
				t.Fatalf("prompt=%t body=%t artifacts=%+v", prompt, body, result.Artifacts)
			}
		})
	}
}
