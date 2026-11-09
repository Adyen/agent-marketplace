package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func writePluginFixture(t *testing.T, root, format, name string, members map[string][]string) {
	t.Helper()
	manifestDir := map[string]string{
		"claude-plugin":  ".claude-plugin",
		"factory-plugin": ".factory-plugin",
		"codex-plugin":   ".codex-plugin",
		"agent-plugin":   ".",
	}[format]
	dir := filepath.Join(root, manifestDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name": "` + name + `"}`
	if format == "agent-plugin" {
		manifest = `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "` + name + `"}`
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	for component, names := range members {
		for _, member := range names {
			dir := filepath.Join(root, component, member)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+member+"\n---\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func formatSourceResult(t *testing.T, dir, format string) LoadResult {
	t.Helper()
	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:    dir,
		V4Kind: config.V4SourceDirectory,
		Format: format,
	}}}
	return LoadRegistries(cfg, "", LoadModeInspect)
}

func pluginEntries(loaded LoadResult, key string) []Entry {
	var out []Entry
	for _, e := range loaded.Catalogs[key].Entries {
		if e.Kind == ir.KindPlugin {
			out = append(out, e)
		}
	}
	return out
}

func TestFormatPackageSourceYieldsOnePluginEntry(t *testing.T) {
	dir := t.TempDir()
	writePluginFixture(t, dir, "agent-plugin", "1password.tools", map[string][]string{"skills": {"vault"}})

	result := formatSourceResult(t, dir, "agent-plugin")
	if len(result.Errors) != 0 {
		t.Fatalf("load errors: %v", result.Errors)
	}
	key := result.Config.Registries[0].CatalogKey
	entries := pluginEntries(result, key)
	if len(entries) != 1 {
		t.Fatalf("expected one plugin entry, got %#v", result.Catalogs[key].Entries)
	}
	e := entries[0]
	if e.Name != "1password.tools" || e.Plugin == nil {
		t.Fatalf("plugin entry missing name/metadata: %#v", e)
	}
	if e.Plugin.Refusal != nil {
		t.Fatalf("plugin entry refused: %v", e.Plugin.Refusal)
	}
	if len(e.Plugin.Members) != 1 || e.Plugin.Members[0].Key() != "skill/vault" {
		t.Fatalf("plugin members = %#v", e.Plugin.Members)
	}
	if _, ok := e.Resource.(*ir.Plugin); !ok {
		t.Fatalf("plugin entry lacks decoded *ir.Plugin resource: %#v", e.Resource)
	}
}

func TestInvalidPluginMemberIsExcludedByItsSourceKey(t *testing.T) {
	dir := t.TempDir()
	writePluginFixture(t, dir, "agent-plugin", "kit", map[string][]string{"skills": {"good"}})
	badSkill := filepath.Join(dir, "skills", "bad")
	if err := os.MkdirAll(badSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badSkill, "SKILL.md"), []byte("---\nname: bad\ninvalid: [\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := formatSourceResult(t, dir, "agent-plugin")
	if len(result.Errors) != 0 {
		t.Fatalf("catalog load errors: %v", result.Errors)
	}
	key := result.Config.Registries[0].CatalogKey
	entries := pluginEntries(result, key)
	if len(entries) != 1 || entries[0].Plugin == nil {
		t.Fatalf("plugin entry = %#v", entries)
	}
	if !PluginInvalidMemberExclusionMatch("skill/bad", entries[0].Plugin.Invalid) {
		t.Fatalf("decoded invalid records do not recognize the member exclusion: %#v", entries[0].Plugin.Invalid)
	}
}

func TestFormatSourceSkipsLooseIngestion(t *testing.T) {
	dir := t.TempDir()
	writePluginFixture(t, dir, "agent-plugin", "kit", map[string][]string{"skills": {"vault"}})
	// A loose skill directory alongside the package must not be ingested.
	loose := filepath.Join(dir, "loose")
	if err := os.MkdirAll(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loose, "SKILL.md"), []byte("---\nname: stray\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := formatSourceResult(t, dir, "agent-plugin")
	key := result.Config.Registries[0].CatalogKey
	for _, e := range result.Catalogs[key].Entries {
		if e.Kind != ir.KindPlugin {
			t.Fatalf("format source produced a loose %s entry %q", e.Kind, e.Name)
		}
	}
	if len(pluginEntries(result, key)) != 1 {
		t.Fatalf("expected exactly the plugin entry, got %#v", result.Catalogs[key].Entries)
	}
}

func marketplaceFixture(t *testing.T, format, manifest string) (dir, manifestDir string) {
	t.Helper()
	dir = t.TempDir()
	manifestDir = map[string]string{
		"claude-marketplace":  ".claude-plugin",
		"factory-marketplace": ".factory-plugin",
		"codex-marketplace":   ".agents/plugins",
	}[format]
	if err := os.MkdirAll(filepath.Join(dir, manifestDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestDir, "marketplace.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, manifestDir
}

func TestFormatCatalogSourceListsEntries(t *testing.T) {
	dir, _ := marketplaceFixture(t, "claude-marketplace", `{"name":"team","plugins":[
		{"name":"review-kit","source":"./plugins/review-kit"},
		{"name":"npm-kit","source":{"source":"npm","package":"npm-kit"}},
		{"name":"mirror-kit","source":{"source":"url","url":"https://git.example.com/mirror.git","ref":"main"}}
	]}`)
	writePluginFixture(t, filepath.Join(dir, "plugins", "review-kit"), "claude-plugin", "review-kit", map[string][]string{"skills": {"review"}})

	result := formatSourceResult(t, dir, "claude-marketplace")
	if len(result.Errors) != 0 {
		t.Fatalf("load errors: %v", result.Errors)
	}
	key := result.Config.Registries[0].CatalogKey
	entries := pluginEntries(result, key)
	if len(entries) != 3 {
		t.Fatalf("expected three listed plugin entries, got %#v", entries)
	}
	byName := map[string]Entry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if byName["review-kit"].Plugin == nil || byName["review-kit"].Plugin.Refusal != nil {
		t.Fatalf("relative entry should be installable: %#v", byName["review-kit"])
	}
	if len(byName["review-kit"].Plugin.Members) != 1 || byName["review-kit"].Plugin.Members[0].Key() != "skill/review" {
		t.Fatalf("relative entry members = %#v", byName["review-kit"].Plugin.Members)
	}
	refused := byName["npm-kit"]
	if refused.Plugin == nil || refused.Plugin.Refusal == nil {
		t.Fatalf("npm entry must stay listed with a refusal: %#v", refused)
	}
	if refused.Plugin.Refusal.Code != diag.CodePluginCatalogSourceUnsupported {
		t.Fatalf("npm entry refusal = %v", refused.Plugin.Refusal)
	}
	external := byName["mirror-kit"]
	if external.Plugin == nil || external.Plugin.Source.Kind != "git" || external.Plugin.Source.URL != "https://git.example.com/mirror.git" {
		t.Fatalf("external entry source = %#v", external.Plugin)
	}
}

func TestFormatCatalogDuplicateEntriesStayListed(t *testing.T) {
	dir, _ := marketplaceFixture(t, "claude-marketplace", `{"name":"team","plugins":[
		{"name":"dup-kit","source":"./plugins/a"},
		{"name":"dup-kit","source":"./plugins/b"}
	]}`)
	writePluginFixture(t, filepath.Join(dir, "plugins", "a"), "claude-plugin", "dup-kit", nil)
	writePluginFixture(t, filepath.Join(dir, "plugins", "b"), "claude-plugin", "dup-kit", nil)

	result := formatSourceResult(t, dir, "claude-marketplace")
	key := result.Config.Registries[0].CatalogKey
	entries := pluginEntries(result, key)
	if len(entries) != 2 {
		t.Fatalf("duplicate entries must stay listed: %#v", entries)
	}
	for _, e := range entries {
		if e.Plugin == nil || e.Plugin.Refusal == nil || e.Plugin.Refusal.Code != diag.CodePluginCatalogDuplicateEntry {
			t.Fatalf("duplicate entry refusal = %#v", e.Plugin)
		}
	}
}
