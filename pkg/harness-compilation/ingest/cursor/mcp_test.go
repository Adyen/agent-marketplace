package cursor

import "testing"

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
