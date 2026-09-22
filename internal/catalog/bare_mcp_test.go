package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
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

func TestDiscoverUnconsumedContainerPreservesChildren(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
	}{
		{name: "root", prefix: ""},
		{name: "nested", prefix: "group"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeUnconsumedContainerFixture(t, root, tt.prefix, false)
			loaded, err := Discover(root, "")
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			assertUnconsumedContainerCatalog(t, loaded, root, tt.prefix)

			result := LoadRegistries(&config.Config{Registries: []config.RegistryEntry{{URL: root, Default: true}}}, "", LoadModeInspect)
			if len(result.Errors) != 0 {
				t.Fatalf("LoadRegistries errors: %v", result.Errors)
			}
			if len(result.Config.Registries) != 1 || result.Config.Registries[0].Path != filepath.Join(tt.prefix, "child") {
				t.Fatalf("registries = %#v", result.Config.Registries)
			}
			catalog, ok := result.Catalogs[result.Config.Registries[0].CatalogKey]
			if !ok || len(catalog.Entries) != 1 || catalog.Entries[0].Name != "demo" {
				t.Fatalf("catalogs = %#v", result.Catalogs)
			}
			if len(catalog.Malformed) != 1 || catalog.Malformed[0].DirName != diag.CodeIngestFactoryUnconsumedConfig || filepath.Clean(catalog.Malformed[0].Path) != filepath.Join(root, tt.prefix, ".mcp.json") {
				t.Fatalf("child catalog malformed = %#v", catalog.Malformed)
			}
			if result.MalformedCount != 1 || len(result.Malformed) != 1 || result.Malformed[0].DirName != diag.CodeIngestFactoryUnconsumedConfig || filepath.Clean(result.Malformed[0].Path) != filepath.Join(root, tt.prefix, ".mcp.json") {
				t.Fatalf("malformed count=%d malformed=%#v", result.MalformedCount, result.Malformed)
			}
		})
	}

	t.Run("root resource binds traversal", func(t *testing.T) {
		root := t.TempDir()
		writeUnconsumedContainerFixture(t, root, "", true)
		loaded, err := Discover(root, "")
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if len(loaded.Entries) != 1 || loaded.Entries[0].Name != "parent" || loaded.Entries[0].Kind != ir.KindCommand {
			t.Fatalf("entries = %#v", loaded.Entries)
		}
		foundIgnored := false
		for _, malformed := range loaded.Malformed {
			if malformed.DirName == "catalog.discover.sub-bundles-ignored" {
				foundIgnored = true
			}
		}
		if !foundIgnored {
			t.Fatalf("malformed = %#v", loaded.Malformed)
		}
	})

	t.Run("load error binds traversal", func(t *testing.T) {
		root := t.TempDir()
		writeUnconsumedContainerFixture(t, root, "group", false)
		writeFile(t, filepath.Join(root, "group", ".factory-plugin", "plugin.json"), `{"name":`)
		loaded, err := Discover(root, "")
		if err == nil {
			t.Fatalf("Discover returned catalog: %#v", loaded)
		}
		if len(loaded.Entries) != 0 {
			t.Fatalf("load error leaked child: catalog=%#v error=%v", loaded, err)
		}
	})
}

func TestLoadRegistriesUnconsumedWarningsStayWithinContainer(t *testing.T) {
	root := t.TempDir()
	writeUnconsumedContainerFixture(t, root, "a-audit", false)
	writeUnconsumedContainerFixture(t, root, "z-audit", false)
	writeFile(t, filepath.Join(root, "z-audit", "child", "skills", "demo", "SKILL.md"), "---\nname: z-demo\ndescription: Demo\n---\nDemo\n")
	if err := os.MkdirAll(filepath.Join(root, "m-clean", "skills", "clean"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "m-clean", "skills", "clean", "SKILL.md"), "---\nname: clean\ndescription: Clean\n---\nClean\n")

	result := LoadRegistries(&config.Config{Registries: []config.RegistryEntry{{URL: root, Default: true}}}, "", LoadModeInspect)
	if len(result.Errors) != 0 {
		t.Fatalf("LoadRegistries errors: %v", result.Errors)
	}
	wantPaths := map[string]string{
		"a-audit/child": filepath.Join(root, "a-audit", ".mcp.json"),
		"m-clean":       "",
		"z-audit/child": filepath.Join(root, "z-audit", ".mcp.json"),
	}
	if len(result.Config.Registries) != len(wantPaths) {
		t.Fatalf("registries = %#v", result.Config.Registries)
	}
	for _, entry := range result.Config.Registries {
		wantMalformed, ok := wantPaths[entry.Path]
		if !ok {
			t.Fatalf("unexpected registry path %q in %#v", entry.Path, result.Config.Registries)
		}
		catalog, ok := result.Catalogs[entry.CatalogKey]
		if !ok {
			t.Fatalf("missing catalog for %q", entry.Path)
		}
		if wantMalformed == "" {
			if len(catalog.Malformed) != 0 {
				t.Fatalf("clean catalog malformed = %#v", catalog.Malformed)
			}
			continue
		}
		if len(catalog.Malformed) != 1 || catalog.Malformed[0].DirName != diag.CodeIngestFactoryUnconsumedConfig || filepath.Clean(catalog.Malformed[0].Path) != wantMalformed {
			t.Fatalf("catalog %q malformed = %#v", entry.Path, catalog.Malformed)
		}
	}
	if len(result.Malformed) != 2 || result.MalformedCount != 2 {
		t.Fatalf("global malformed count=%d malformed=%#v", result.MalformedCount, result.Malformed)
	}
}

func TestLoadRegistriesTerminalUnconsumedContainer(t *testing.T) {
	writeTerminal := func(t *testing.T, root string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, "group", ".factory-plugin"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(root, "group", ".factory-plugin", "plugin.json"), `{"name":"group"}`)
		writeFile(t, filepath.Join(root, "group", ".mcp.json"), `{"mcpServers":{"foreign":{"command":"npx"}}}`)
	}

	t.Run("terminal", func(t *testing.T) {
		root := t.TempDir()
		writeTerminal(t, root)
		result := LoadRegistries(&config.Config{Registries: []config.RegistryEntry{{URL: root, Default: true}}}, "", LoadModeInspect)
		if len(result.Errors) != 0 {
			t.Fatalf("LoadRegistries errors: %v", result.Errors)
		}
		if len(result.Config.Registries) != 1 || result.Config.Registries[0].Path != "group" {
			t.Fatalf("registries = %#v", result.Config.Registries)
		}
		catalog, ok := result.Catalogs[result.Config.Registries[0].CatalogKey]
		if !ok || len(catalog.Entries) != 0 || len(catalog.Malformed) != 1 || catalog.Malformed[0].DirName != diag.CodeIngestFactoryUnconsumedConfig || filepath.Clean(catalog.Malformed[0].Path) != filepath.Join(root, "group", ".mcp.json") {
			t.Fatalf("terminal catalog = %#v", catalog)
		}
		if len(result.Malformed) != 1 || result.MalformedCount != 1 {
			t.Fatalf("global malformed count=%d malformed=%#v", result.MalformedCount, result.Malformed)
		}
	})

	t.Run("terminal with resource sibling", func(t *testing.T) {
		root := t.TempDir()
		writeTerminal(t, root)
		if err := os.MkdirAll(filepath.Join(root, "other", "skills", "demo"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(root, "other", "skills", "demo", "SKILL.md"), "---\nname: demo\ndescription: Demo\n---\nDemo\n")
		result := LoadRegistries(&config.Config{Registries: []config.RegistryEntry{{URL: root, Default: true}}}, "", LoadModeInspect)
		if len(result.Errors) != 0 {
			t.Fatalf("LoadRegistries errors: %v", result.Errors)
		}
		if len(result.Config.Registries) != 2 {
			t.Fatalf("registries = %#v", result.Config.Registries)
		}
		for _, entry := range result.Config.Registries {
			catalog, ok := result.Catalogs[entry.CatalogKey]
			if !ok {
				t.Fatalf("missing catalog for %q", entry.Path)
			}
			switch entry.Path {
			case "group":
				if len(catalog.Entries) != 0 || len(catalog.Malformed) != 1 || catalog.Malformed[0].DirName != diag.CodeIngestFactoryUnconsumedConfig || filepath.Clean(catalog.Malformed[0].Path) != filepath.Join(root, "group", ".mcp.json") {
					t.Fatalf("terminal catalog = %#v", catalog)
				}
			case "other":
				if len(catalog.Entries) != 1 || catalog.Entries[0].Name != "demo" || len(catalog.Malformed) != 0 {
					t.Fatalf("sibling catalog = %#v", catalog)
				}
			default:
				t.Fatalf("unexpected registry path %q", entry.Path)
			}
		}
		if len(result.Malformed) != 1 || result.MalformedCount != 1 {
			t.Fatalf("global malformed count=%d malformed=%#v", result.MalformedCount, result.Malformed)
		}
	})
}

func writeUnconsumedContainerFixture(t *testing.T, root, prefix string, parentResource bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, prefix, ".factory-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, prefix, "child", "skills", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if parentResource {
		if err := os.MkdirAll(filepath.Join(root, prefix, "commands"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(root, prefix, ".factory-plugin", "plugin.json"), `{"name":"group"}`)
	writeFile(t, filepath.Join(root, prefix, ".mcp.json"), `{"mcpServers":{"foreign":{"command":"npx"}}}`)
	writeFile(t, filepath.Join(root, prefix, "child", "skills", "demo", "SKILL.md"), "---\nname: demo\ndescription: Demo\n---\nDemo\n")
	if parentResource {
		writeFile(t, filepath.Join(root, prefix, "commands", "parent.md"), "---\nname: parent\ndescription: Parent\n---\nParent\n")
	}
}

func assertUnconsumedContainerCatalog(t *testing.T, loaded LoadedCatalog, root, prefix string) {
	t.Helper()
	if len(loaded.Entries) != 1 || loaded.Entries[0].Kind != ir.KindSkill || loaded.Entries[0].Name != "demo" {
		t.Fatalf("entries = %#v", loaded.Entries)
	}
	if len(loaded.Malformed) != 1 {
		t.Fatalf("malformed = %#v", loaded.Malformed)
	}
	malformed := loaded.Malformed[0]
	if malformed.DirName != diag.CodeIngestFactoryUnconsumedConfig || filepath.Clean(malformed.Path) != filepath.Join(root, prefix, ".mcp.json") {
		t.Fatalf("malformed = %#v", malformed)
	}
	if len(loaded.Diagnostics) == 0 {
		t.Fatal("missing discovery diagnostics")
	}
	foundWarning := false
	for _, diagnostic := range loaded.Diagnostics {
		if diagnostic.Code == diag.CodeIngestFactoryUnconsumedConfig && diagnostic.Severity == diag.SeverityWarning && filepath.Base(diagnostic.Source.File) == ".mcp.json" {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("diagnostics = %#v", loaded.Diagnostics)
	}
	for _, diagnostic := range loaded.Malformed {
		if diagnostic.DirName == "catalog.discover.sub-bundles-ignored" {
			t.Fatalf("unexpected ignored warning: %#v", loaded.Malformed)
		}
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
