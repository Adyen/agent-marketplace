package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV4MarketplaceMCPPinKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: agent-marketplace/v4\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	editor, err := OpenLayerEditor(LayerTarget{Source: RepoCommitted, StagedPath: path, DeclaringPath: path})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := editor.EnsureMarketplaceSource("https://marketplace.example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	pin := MarketplaceResourcePin{
		RegistryID: "registry", RegistrySlug: "tools", Kind: "mcpserver", Name: "server",
		Revision: "sha256:" + strings.Repeat("a", 64), Digest: "sha256:" + strings.Repeat("a", 64),
		Acquisition: "mirror", Direct: true,
	}
	if err := editor.IncludeMarketplacePins(binding, []MarketplaceResourcePin{pin}); err != nil {
		t.Fatal(err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(candidate.Bytes), "tools/mcp/server") || strings.Contains(string(candidate.Bytes), "mcpserver") {
		t.Fatalf("MCP request used the wrong locator kind:\n%s", candidate.Bytes)
	}
	if err := editor.RemoveMarketplacePins(binding, []MarketplaceResourcePin{pin}); err != nil {
		t.Fatal(err)
	}
	candidate, err = editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(candidate.Bytes), "tools/mcp/server") || !strings.Contains(string(candidate.Bytes), "marketplace.example.invalid") {
		t.Fatalf("MCP removal failed to retain its source without the selection:\n%s", candidate.Bytes)
	}
}
