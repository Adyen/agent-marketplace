package cursor

import (
	"testing"
	"testing/fstest"
)

func TestLoadMcpServers_WorkspacePathNormalizesSource(t *testing.T) {
	servers, err := LoadMcpServers(cursorWorkspaceFS, ".cursor/mcp.json")
	if err != nil {
		t.Fatalf("LoadMcpServers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("LoadMcpServers = %d, want 1", len(servers))
	}
	if servers[0].Source.File != "mcp.json" {
		t.Fatalf("Source.File = %q, want %q", servers[0].Source.File, "mcp.json")
	}
}

// The Cursor-native ${CURSOR_PLUGIN_ROOT} token must normalize to the
// canonical IR spelling across command, args, env, URL, and headers, the same
// root-reference normalization hook ingestion already performs.
func TestLoadMcpServers_NormalizesCursorRootToken(t *testing.T) {
	src := fstest.MapFS{
		"mcp.json": {
			Data: []byte(`{"mcpServers":{"docs":{` +
				`"command":"${CURSOR_PLUGIN_ROOT}/bin/run.sh",` +
				`"args":["--env","${CURSOR_PLUGIN_ROOT}/config/.env"],` +
				`"env":{"CFG":"${CURSOR_PLUGIN_ROOT}/config/.env"},` +
				`"url":"https://example.com/${CURSOR_PLUGIN_ROOT}/sse",` +
				`"headers":{"X-Root":"${CURSOR_PLUGIN_ROOT}/tok"}` +
				`}}}`),
		},
	}

	servers, err := LoadMcpServers(src, "mcp.json")
	if err != nil {
		t.Fatalf("LoadMcpServers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("len(servers) = %d, want 1", len(servers))
	}
	server := servers[0]
	want := map[string]string{
		"command":        "${CLAUDE_PLUGIN_ROOT}/bin/run.sh",
		"arg[1]":         "${CLAUDE_PLUGIN_ROOT}/config/.env",
		"env.CFG":        "${CLAUDE_PLUGIN_ROOT}/config/.env",
		"url":            "https://example.com/${CLAUDE_PLUGIN_ROOT}/sse",
		"headers.X-Root": "${CLAUDE_PLUGIN_ROOT}/tok",
	}
	got := map[string]string{
		"command":        server.Command,
		"arg[1]":         server.Args[1],
		"env.CFG":        server.Env["CFG"],
		"url":            server.URL,
		"headers.X-Root": server.Headers["X-Root"],
	}
	for field, expected := range want {
		if got[field] != expected {
			t.Errorf("%s = %q, want %q", field, got[field], expected)
		}
	}
}
