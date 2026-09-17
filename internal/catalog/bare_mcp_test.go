package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestDiscoverBareBundleMCPAndLegacyCompatibility(t *testing.T) {
	root := t.TempDir()
	writeBareBundleFixture(t, root, "demo", "hi", "probe")

	current, err := Discover(root, "")
	if err != nil {
		t.Fatalf("Discover root: %v", err)
	}
	assertEntries(t, current, map[string]ir.ResourceKind{
		"skill/demo":      ir.KindSkill,
		"command/hi":      ir.KindCommand,
		"mcpserver/probe": ir.KindMcpServer,
	})
	if len(current.Malformed) != 0 {
		t.Fatalf("current malformed = %#v", current.Malformed)
	}

	legacy, err := DiscoverWithOptions(root, "", IngestOptions{LegacyBareMCP: true})
	if err != nil {
		t.Fatalf("Discover legacy root: %v", err)
	}
	assertEntries(t, legacy, map[string]ir.ResourceKind{
		"skill/demo": ir.KindSkill,
		"command/hi": ir.KindCommand,
	})
	if hasCatalogEntry(legacy, ir.KindMcpServer, "probe") {
		t.Fatal("legacy root retained bare MCP")
	}

	container := t.TempDir()
	nested := filepath.Join(container, "nested")
	writeBareBundleFixture(t, nested, "nested-demo", "nested-hi", "nested-probe")
	currentNested, err := Discover(container, "")
	if err != nil {
		t.Fatalf("Discover nested container: %v", err)
	}
	assertEntries(t, currentNested, map[string]ir.ResourceKind{
		"skill/nested-demo":      ir.KindSkill,
		"command/nested-hi":      ir.KindCommand,
		"mcpserver/nested-probe": ir.KindMcpServer,
	})
	legacyNested, err := DiscoverWithOptions(container, "", IngestOptions{LegacyBareMCP: true})
	if err != nil {
		t.Fatalf("Discover legacy nested container: %v", err)
	}
	assertEntries(t, legacyNested, map[string]ir.ResourceKind{
		"skill/nested-demo": ir.KindSkill,
		"command/nested-hi": ir.KindCommand,
	})
}

func TestDiscoverExplicitClaudeForeignMCPBecomesMalformed(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"demo"}`)
	writeFile(t, filepath.Join(root, "skills", "demo", "SKILL.md"), "---\nname: demo\ndescription: Demo\n---\nDemo\n")
	writeFile(t, filepath.Join(root, ".mcp.json"), `{"mcpServers":{"native":{"command":"npx"}}}`)
	writeFile(t, filepath.Join(root, "mcp.json"), `{"mcpServers":{"foreign":{"command":"npx"}}}`)

	loaded, err := Discover(root, "")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if !hasCatalogEntry(loaded, ir.KindMcpServer, "native") {
		t.Fatalf("native MCP missing: %#v", loaded.Entries)
	}
	var found bool
	for _, malformed := range loaded.Malformed {
		if filepath.Base(malformed.Path) == "mcp.json" && malformed.DirName == diag.CodeIngestClaudeUnconsumedConfig {
			found = true
			if !strings.Contains(malformed.Error.Error(), "For Claude plugins, rename") {
				t.Fatalf("malformed remediation = %v", malformed.Error)
			}
		}
	}
	if !found {
		t.Fatalf("foreign MCP malformed entry missing: %#v", loaded.Malformed)
	}
}

func writeBareBundleFixture(t *testing.T, root, skillName, commandName, mcpName string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills", skillName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "skills", skillName, "SKILL.md"), fmt.Sprintf("---\nname: %s\ndescription: Demo\n---\nDemo\n", skillName))
	writeFile(t, filepath.Join(root, "commands", commandName+".md"), fmt.Sprintf("---\nname: %s\ndescription: Hi\n---\nHi\n", commandName))
	writeFile(t, filepath.Join(root, "mcp.json"), fmt.Sprintf(`{"mcpServers":{"%s":{"command":"npx"}}}`, mcpName))
}

func assertEntries(t *testing.T, catalog LoadedCatalog, want map[string]ir.ResourceKind) {
	t.Helper()
	if len(catalog.Entries) != len(want) {
		t.Fatalf("entries = %#v, want %d", catalog.Entries, len(want))
	}
	for _, entry := range catalog.Entries {
		delete(want, entry.Kind.String()+"/"+entry.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing entries = %#v", want)
	}
}

func hasCatalogEntry(catalog LoadedCatalog, kind ir.ResourceKind, name string) bool {
	for _, entry := range catalog.Entries {
		if entry.Kind == kind && entry.Name == name {
			return true
		}
	}
	return false
}
