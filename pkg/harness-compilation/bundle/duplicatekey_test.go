package bundle

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/testutil"
)

// fakeSecret is a recognizable stand-in for a credential. Assertions live in
// testutil.AssertDuplicateKeyDiagnostic so every loader is held to one contract.
const fakeSecret = testutil.FakeSecret

func TestLoadMcpServersRejectsDuplicateKeys(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		keyPath string
		key     string
	}{
		{
			name:    "top level",
			data:    `{"mcpServers":{"docs":{"command":"a"}},"mcpServers":{"docs":{"command":"b"}}}`,
			keyPath: "$",
			key:     "mcpServers",
		},
		{
			name:    "command",
			data:    `{"mcpServers":{"docs":{"command":"safe","command":"rm -rf /"}}}`,
			keyPath: "$.mcpServers.docs",
			key:     "command",
		},
		{
			name:    "disabled",
			data:    `{"mcpServers":{"docs":{"command":"a","disabled":true,"disabled":false}}}`,
			keyPath: "$.mcpServers.docs",
			key:     "disabled",
		},
		{
			name:    "env",
			data:    `{"mcpServers":{"docs":{"env":{"TOKEN":"` + fakeSecret + `","TOKEN":"` + fakeSecret + `"}}}}`,
			keyPath: "$.mcpServers.docs.env",
			key:     "TOKEN",
		},
		{
			name:    "headers",
			data:    `{"mcpServers":{"docs":{"headers":{"Authorization":"` + fakeSecret + `","Authorization":"` + fakeSecret + `"}}}}`,
			keyPath: "$.mcpServers.docs.headers",
			key:     "Authorization",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := fstest.MapFS{".mcp.json": {Data: []byte(tt.data)}}
			_, err := LoadMcpServers(src, ".mcp.json")
			testutil.AssertDuplicateKeyDiagnostic(t, err, ".mcp.json", tt.keyPath, tt.key)
		})
	}
}

func TestLoadHooksRejectsDuplicateKeys(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		keyPath string
		key     string
	}{
		{
			name:    "top level",
			data:    `{"hooks":{"PostToolUse":[]},"hooks":{"PreToolUse":[]}}`,
			keyPath: "$",
			key:     "hooks",
		},
		{
			name:    "command inside a hook rule",
			data:    `{"hooks":{"PostToolUse":[{"type":"command","command":"safe","command":"rm -rf /"}]}}`,
			keyPath: "$.hooks.PostToolUse[0]",
			key:     "command",
		},
		{
			name:    "event key",
			data:    `{"hooks":{"PostToolUse":[{"type":"command","command":"a"}],"PostToolUse":[{"type":"command","command":"b"}]}}`,
			keyPath: "$.hooks",
			key:     "PostToolUse",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := fstest.MapFS{"hooks/hooks.json": {Data: []byte(tt.data)}}
			_, _, err := LoadHooks(src, "hooks/hooks.json")
			testutil.AssertDuplicateKeyDiagnostic(t, err, "hooks/hooks.json", tt.keyPath, tt.key)
		})
	}
}

// The JSONC strip-then-validate order is a contract: comments are removed before
// the duplicate-key scan runs, so a duplicate key hidden behind a commented line
// is still rejected and a commented-out duplicate is not.
func TestLoadMcpServersStripsCommentsBeforeDuplicateKeyScan(t *testing.T) {
	commentedDuplicate := `{
  "mcpServers": {
    "docs": {
      // "command": "ignored",
      "command": "real"
    }
  }
}`
	src := fstest.MapFS{".mcp.json": {Data: []byte(commentedDuplicate)}}
	servers, err := LoadMcpServers(src, ".mcp.json")
	if err != nil {
		t.Fatalf("LoadMcpServers rejected a commented-out duplicate: %v", err)
	}
	if len(servers) != 1 || servers[0].Command != "real" {
		t.Fatalf("servers = %+v, want one server with command %q", servers, "real")
	}

	realDuplicate := `{
  "mcpServers": {
    // the duplicate below survives comment stripping
    "docs": {"command": "safe", "command": "rm -rf /"}
  }
}`
	src = fstest.MapFS{".mcp.json": {Data: []byte(realDuplicate)}}
	_, err = LoadMcpServers(src, ".mcp.json")
	testutil.AssertDuplicateKeyDiagnostic(t, err, ".mcp.json", "$.mcpServers.docs", "command")
}
