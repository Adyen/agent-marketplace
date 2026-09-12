package claude

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestNativeMCPWithWorkspaceResources(t *testing.T) {
	for _, mcpPath := range []string{".mcp.json", ".claude.json", ".claude/.mcp.json"} {
		t.Run(mcpPath, func(t *testing.T) {
			src := fstest.MapFS{
				mcpPath:                        &fstest.MapFile{Data: []byte(`{"theme":"dark","mcpServers":{"first":{"command":"first"},"second":{"command":"second"}}}`)},
				".claude/skills/demo/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: demo\ndescription: Demo\n---\nbody\n")},
			}
			ingester := &claudeIngester{}
			if !ingester.Detect(src) {
				t.Fatal("native layout not detected")
			}
			bundle, err := ingester.LoadBundle(src)
			if err != nil {
				t.Fatal(err)
			}
			counts := map[ir.ResourceKind]int{}
			for _, resource := range bundle.Resources {
				counts[resource.Kind()]++
			}
			if counts[ir.KindSkill] != 1 || counts[ir.KindMcpServer] != 2 {
				t.Fatalf("resource kinds: %v", counts)
			}
		})
	}
}

func TestNativeUserMCPOnly(t *testing.T) {
	src := fstest.MapFS{".claude.json": &fstest.MapFile{Data: []byte(`{"mcpServers":{"only":{"command":"only"}}}`)}}
	ingester := &claudeIngester{}
	if !ingester.Detect(src) {
		t.Fatal("user MCP layout not detected")
	}
	bundle, err := ingester.LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 1 || bundle.Resources[0].Kind() != ir.KindMcpServer {
		t.Fatalf("resources = %v", bundle.Resources)
	}
}
