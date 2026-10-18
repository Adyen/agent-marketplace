package plugindecode_test

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
)

func decodeCatalog(t *testing.T, files map[string]string, format string) (*plugindecode.Catalog, error) {
	t.Helper()
	catalog, _, err := plugindecode.DecodeCatalog(writePackage(t, files), format)
	return catalog, err
}

// spec: plugin-packages/relative-entry-resolves-against-the-catalog-root
func TestDecodeCatalogRelativeEntryResolvesAgainstCatalogRoot(t *testing.T) {
	catalog, err := decodeCatalog(t, map[string]string{
		".claude-plugin/marketplace.json": `{"name": "team", "owner": {"name": "t"}, "plugins": [
			{"name": "review-kit", "source": "./plugins/review-kit"}]}`,
		"plugins/review-kit/.claude-plugin/plugin.json": `{"name": "review-kit"}`,
	}, "claude-marketplace")
	if err != nil {
		t.Fatalf("DecodeCatalog() error = %v", err)
	}
	if len(catalog.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(catalog.Entries))
	}
	entry := catalog.Entries[0]
	if !entry.Installable || entry.Source.Kind != "path" || entry.Source.Path != "plugins/review-kit" {
		t.Fatalf("entry = %#v, want installable relative path plugins/review-kit", entry)
	}
}

// spec: plugin-packages/duplicate-catalog-entry-names-are-refused
func TestDecodeCatalogDuplicateEntryNamesAreRefused(t *testing.T) {
	catalog, err := decodeCatalog(t, map[string]string{
		".claude-plugin/marketplace.json": `{"name": "team", "owner": {"name": "t"}, "plugins": [
			{"name": "dup", "source": "./plugins/one"},
			{"name": "dup", "source": {"source": "github", "repo": "org/two"}},
			{"name": "ok", "source": "./plugins/ok"}]}`,
	}, "claude-marketplace")
	if err != nil {
		t.Fatalf("DecodeCatalog() error = %v", err)
	}
	if len(catalog.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(catalog.Entries))
	}
	duplicates := 0
	for _, entry := range catalog.Entries {
		if entry.Name == "dup" {
			duplicates++
			if entry.Installable {
				t.Fatalf("duplicate entry %q stayed installable: %#v", entry.Name, entry)
			}
			if entry.Diagnostic == nil || entry.Diagnostic.Code != "plugin.catalog.duplicate-entry" {
				t.Fatalf("duplicate entry %q diagnostic = %v, want plugin.catalog.duplicate-entry", entry.Name, entry.Diagnostic)
			}
		}
	}
	if duplicates != 2 {
		t.Fatalf("duplicate copies reported = %d, want 2", duplicates)
	}
	if !catalog.Entries[2].Installable {
		t.Fatal("the non-duplicate sibling entry must stay installable")
	}
}

func TestDecodeCatalogRefusedSourceTypeBlocksOnlyThatEntry(t *testing.T) {
	catalog, err := decodeCatalog(t, map[string]string{
		".claude-plugin/marketplace.json": `{"name": "team", "owner": {"name": "t"}, "plugins": [
			{"name": "remote", "source": {"source": "github", "repo": "org/remote", "ref": "main"}},
			{"name": "packed", "source": {"source": "npm", "package": "@org/packed"}}]}`,
	}, "claude-marketplace")
	if err != nil {
		t.Fatalf("DecodeCatalog() error = %v", err)
	}
	if len(catalog.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(catalog.Entries))
	}
	git, npm := catalog.Entries[0], catalog.Entries[1]
	if !git.Installable || git.Source.Kind != "git" || git.Source.URL != "https://github.com/org/remote.git" || git.Source.Ref != "main" {
		t.Fatalf("git entry = %#v, want installable exact git requirement", git)
	}
	if npm.Installable || npm.Diagnostic == nil || npm.Diagnostic.Code != "plugin.catalog.source-unsupported" {
		t.Fatalf("npm entry = %#v, want plugin.catalog.source-unsupported and not installable", npm)
	}
}

func TestDecodeCatalogGitSubdirectoryEntryIsExactRequirement(t *testing.T) {
	catalog, err := decodeCatalog(t, map[string]string{
		".claude-plugin/marketplace.json": `{"name": "team", "owner": {"name": "t"}, "plugins": [
			{"name": "sub", "source": {"source": "git-subdir", "url": "https://git.example/mono.git", "ref": "main", "sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "path": "tools/sub"}},
			{"name": "remote", "source": {"source": "url", "url": "https://git.example/remote.git"}}]}`,
	}, "claude-marketplace")
	if err != nil {
		t.Fatalf("DecodeCatalog() error = %v", err)
	}
	sub := catalog.Entries[0]
	if !sub.Installable || sub.Source.Kind != "git" || sub.Source.URL != "https://git.example/mono.git" ||
		sub.Source.Ref != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || sub.Source.Subdirectory != "tools/sub" {
		t.Fatalf("git-subdir entry = %#v, want exact git requirement with subdirectory", sub)
	}
	remote := catalog.Entries[1]
	if !remote.Installable || remote.Source.Kind != "git" || remote.Source.URL != "https://git.example/remote.git" || remote.Source.Subdirectory != "" {
		t.Fatalf("url entry = %#v, want git requirement without subdirectory", remote)
	}
}

func TestDecodeCatalogEntryDefinitionDigest(t *testing.T) {
	catalog, err := decodeCatalog(t, map[string]string{
		".claude-plugin/marketplace.json": `{"name": "team", "owner": {"name": "t"}, "plugins": [
			{"name": "kit", "source": "./plugins/kit", "description": "d", "strict": false}]}`,
	}, "claude-marketplace")
	if err != nil {
		t.Fatalf("DecodeCatalog() error = %v", err)
	}
	entry := catalog.Entries[0]
	if len(entry.DefinitionDigest) != 64 {
		t.Fatalf("definition digest = %q, want sha256 hex", entry.DefinitionDigest)
	}
	if entry.Strict {
		t.Fatal("entry strict flag was not carried")
	}
	if entry.Overlay["description"] != "d" {
		t.Fatalf("entry overlay = %#v, want entry fields beyond name/source/strict", entry.Overlay)
	}
}

func TestDecodeCatalogFactoryAndCodex(t *testing.T) {
	factory, err := decodeCatalog(t, map[string]string{
		".factory-plugin/marketplace.json": `{"name": "f", "owner": {"name": "t"}, "plugins": [
			{"name": "kit", "source": {"source": "github", "repo": "org/kit"}},
			{"name": "ext", "source": {"source": "url", "url": "https://git.example/ext.git"}}]}`,
	}, "factory-marketplace")
	if err != nil {
		t.Fatalf("factory DecodeCatalog() error = %v", err)
	}
	if !factory.Entries[0].Installable {
		t.Fatal("factory github entry must be installable")
	}
	if factory.Entries[1].Installable || factory.Entries[1].Diagnostic.Code != "plugin.catalog.source-unsupported" {
		t.Fatalf("factory url entry = %#v, want refused", factory.Entries[1])
	}
	codex, err := decodeCatalog(t, map[string]string{
		".agents/plugins/marketplace.json": `{"name": "c", "interface": {}, "plugins": [
			{"name": "kit", "source": {"source": "git", "url": "https://git.example/kit.git", "path": "plugins/kit"}},
			{"name": "hub", "source": {"source": "github", "repo": "org/hub"}}]}`,
	}, "codex-marketplace")
	if err != nil {
		t.Fatalf("codex DecodeCatalog() error = %v", err)
	}
	if !codex.Entries[0].Installable || codex.Entries[0].Source.Subdirectory != "plugins/kit" {
		t.Fatalf("codex git entry = %#v", codex.Entries[0])
	}
	if codex.Entries[1].Installable || codex.Entries[1].Diagnostic.Code != "plugin.catalog.source-unsupported" {
		t.Fatalf("codex github entry = %#v, want refused", codex.Entries[1])
	}
}

func TestDecodeCatalogEscapingPathEntryNotInstallable(t *testing.T) {
	catalog, err := decodeCatalog(t, map[string]string{
		".claude-plugin/marketplace.json": `{"name": "team", "owner": {"name": "t"}, "plugins": [
			{"name": "escape", "source": "../outside"},
			{"name": "ok", "source": "./ok"}]}`,
	}, "claude-marketplace")
	if err != nil {
		t.Fatalf("DecodeCatalog() error = %v", err)
	}
	if catalog.Entries[0].Installable {
		t.Fatal("an entry source escaping the catalog root must not be installable")
	}
	if !catalog.Entries[1].Installable {
		t.Fatal("the valid sibling must stay installable")
	}
}

func TestDecodeCatalogMissingManifestRefused(t *testing.T) {
	catalog, err := decodeCatalog(t, map[string]string{}, "claude-marketplace")
	if err == nil || catalog != nil {
		t.Fatalf("a catalog without a manifest must be refused, catalog=%v err=%v", catalog, err)
	}
	if !strings.Contains(err.Error(), "plugin.format.manifest-invalid") {
		t.Fatalf("want plugin.format.manifest-invalid, got %v", err)
	}
}

func TestDecodeCatalogPackageFormatRefused(t *testing.T) {
	catalog, err := decodeCatalog(t, map[string]string{
		"plugin.json": `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "demo"}`,
	}, "agent-plugin")
	if err == nil || catalog != nil {
		t.Fatalf("a package format must not decode as a catalog, catalog=%v err=%v", catalog, err)
	}
}
