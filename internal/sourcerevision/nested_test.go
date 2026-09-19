package sourcerevision

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
)

func nestedRevisionFixture(t testing.TB, root string) {
	t.Helper()
	writeFixture(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"container","version":"1.0.0"}`)
	writeFixture(t, filepath.Join(root, "plugins", "audit", "hooks", "hooks.json"), `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo audit"}]}]}}`)
	writeFixture(t, filepath.Join(root, "plugins", "audit", ".mcp.json"), `{"mcpServers":{"nested-server":{"command":"fixture-server"}}}`)
}

func TestNestedRevisionInventoryMatchesDiscovery(t *testing.T) {
	for _, schema := range []string{"v1", "v2", "v3"} {
		for _, sourceHarness := range []string{"claude", ""} {
			t.Run(schema+"/"+sourceHarness, func(t *testing.T) {
				root := t.TempDir()
				nestedRevisionFixture(t, root)
				input := Input{SnapshotRoot: root, SourceHarness: sourceHarness, RevisionSchema: schema}
				loaded, err := catalog.LoadWithOptions(root, "claude", catalog.IngestOptions{})
				if err != nil || len(loaded.Entries) != 0 {
					t.Fatalf("fixture must load an empty parent: %v", err)
				}
				discovered, err := catalog.Discover(root, "claude")
				if err != nil || len(discovered.Entries) != 2 {
					t.Fatalf("fixture must discover hook and MCP: %v (%d entries)", err, len(discovered.Entries))
				}
				built, err := Build(input)
				if err != nil {
					t.Fatal(err)
				}
				if len(built.Manifest.Resources) != 2 {
					t.Fatalf("manifest omitted nested resources: %+v", built.Manifest.Resources)
				}
				seen := map[string]bool{}
				for _, resource := range built.Manifest.Resources {
					seen[resource.Kind] = true
					if resource.ContentDigest == "" || resource.ArchiveDigest == "" {
						t.Error("nested resource has no digest")
					}
				}
				if !seen["hook"] || !seen["mcpserver"] {
					t.Fatalf("wrong nested inventory: %v", seen)
				}
				if _, err := Verify(input, built.Projection()); err != nil {
					t.Fatal(err)
				}
				writeFixture(t, filepath.Join(root, "plugins", "audit", ".mcp.json"), `{"mcpServers":{"nested-server":{"command":"changed-server"}}}`)
				if _, err := Verify(input, built.Projection()); !errors.Is(err, ErrManifestMismatch) {
					t.Fatalf("changed nested resource escaped verification: %v", err)
				}
			})
		}
	}
}
