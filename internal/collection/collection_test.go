package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/registryfs"
)

func TestLabelIncludesVersionWhenSet(t *testing.T) {
	c := Collection{Name: "core", Version: "1.2.3"}
	if got, want := c.Label(), "core@1.2.3"; got != want {
		t.Fatalf("Label() = %q, want %q", got, want)
	}
}

func TestLabelUsesNameOnlyWhenVersionEmpty(t *testing.T) {
	c := Collection{Name: "core"}
	if got, want := c.Label(), "core"; got != want {
		t.Fatalf("Label() = %q, want %q", got, want)
	}
}

func TestFindReturnsNilWhenNotFound(t *testing.T) {
	collections := []Collection{{Name: "alpha"}, {Name: "beta"}}
	if got := Find(collections, "gamma"); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}

func TestFindMatchesByNameTrimmingWhitespace(t *testing.T) {
	collections := []Collection{{Name: "alpha"}, {Name: "beta"}}
	got := Find(collections, "  beta  ")
	if got == nil {
		t.Fatalf("expected to find beta, got nil")
	}
	if got.Name != "beta" {
		t.Fatalf("expected beta, got %q", got.Name)
	}
}

func TestFindReturnsFirstMatch(t *testing.T) {
	collections := []Collection{
		{Name: "alpha", Version: "1.0"},
		{Name: "alpha", Version: "2.0"},
	}
	got := Find(collections, "alpha")
	if got == nil || got.Version != "1.0" {
		t.Fatalf("expected alpha@1.0 (first match), got %#v", got)
	}
}

func TestDiscoverCollectionsReturnsNilWhenCollectionsDirMissing(t *testing.T) {
	skillDir := t.TempDir()
	// No collections/ subdir exists.
	got, err := DiscoverCollections(registryfs.Local(skillDir), "key", "git@example.com:r.git", "main", "")
	if err != nil {
		t.Fatalf("expected nil error when collections dir missing, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil collections, got %#v", got)
	}
}

func TestDiscoverCollectionsIgnoresEmptyDir(t *testing.T) {
	skillDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(skillDir, "collections"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := DiscoverCollections(registryfs.Local(skillDir), "key", "git@example.com:r.git", "main", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no collections, got %#v", got)
	}
}

func TestDiscoverCollectionsIgnoresNonDirectoryEntries(t *testing.T) {
	skillDir := t.TempDir()
	collectionsDir := filepath.Join(skillDir, "collections")
	if err := os.MkdirAll(collectionsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A file directly under collections/ should be ignored.
	if err := os.WriteFile(filepath.Join(collectionsDir, "stray.yaml"), []byte("name: stray\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := DiscoverCollections(registryfs.Local(skillDir), "key", "git@example.com:r.git", "main", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no collections when only files present, got %#v", got)
	}
}

func TestDiscoverCollectionsSkipsSubdirsWithoutCollectionFile(t *testing.T) {
	skillDir := t.TempDir()
	subdir := filepath.Join(skillDir, "collections", "empty-collection")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := DiscoverCollections(registryfs.Local(skillDir), "key", "git@example.com:r.git", "main", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no collections when COLLECTION.yaml missing, got %#v", got)
	}
}

func TestDiscoverCollectionsParsesValidFile(t *testing.T) {
	skillDir := t.TempDir()
	writeCollection(t, skillDir, "core-dev", `name: core-dev
version: 1.2.3
description: Developer baseline
skills:
  - name: skill-a
  - name: skill-b
    registry: git@example.com:other.git
    ref: main
includes:
  - name: security
    version: 2.0.0
`)

	got, err := DiscoverCollections(registryfs.Local(skillDir), "cache-key", "git@example.com:source.git", "v1.0", "skills")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 collection, got %d: %#v", len(got), got)
	}

	c := got[0]
	if c.Name != "core-dev" || c.Version != "1.2.3" {
		t.Fatalf("wrong collection: %#v", c)
	}
	if c.Description != "Developer baseline" {
		t.Fatalf("expected description populated, got %q", c.Description)
	}
	if len(c.Skills) != 2 {
		t.Fatalf("expected 2 skill entries, got %#v", c.Skills)
	}
	if c.Skills[0].Name != "skill-a" || c.Skills[0].Registry != "" {
		t.Fatalf("expected bare skill-a, got %#v", c.Skills[0])
	}
	if c.Skills[1].Name != "skill-b" || c.Skills[1].Registry != "git@example.com:other.git" || c.Skills[1].Ref != "main" {
		t.Fatalf("expected qualified skill-b, got %#v", c.Skills[1])
	}
	if len(c.Includes) != 1 || c.Includes[0].Name != "security" || c.Includes[0].Version != "2.0.0" {
		t.Fatalf("expected include security@2.0.0, got %#v", c.Includes)
	}

	// Provenance fields must be stamped.
	if c.SourceRegistryURL != "git@example.com:source.git" {
		t.Fatalf("expected SourceRegistryURL stamped, got %q", c.SourceRegistryURL)
	}
	if c.SourceRef != "v1.0" {
		t.Fatalf("expected SourceRef stamped, got %q", c.SourceRef)
	}
	if c.SourceCatalogKey != "cache-key" {
		t.Fatalf("expected SourceCatalogKey stamped, got %q", c.SourceCatalogKey)
	}
	if c.SourcePath != "skills" {
		t.Fatalf("expected SourcePath stamped, got %q", c.SourcePath)
	}
}

func TestDiscoverCollectionsAcceptsMinimalSkillEntry(t *testing.T) {
	// The CollectionSkill struct only unmarshals from mapping form; this verifies
	// that a mapping with just `name` populates only the Name field.
	skillDir := t.TempDir()
	writeCollection(t, skillDir, "simple", `name: simple
skills:
  - name: bare-skill
`)

	got, err := DiscoverCollections(registryfs.Local(skillDir), "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(got))
	}
	if len(got[0].Skills) != 1 || got[0].Skills[0].Name != "bare-skill" {
		t.Fatalf("expected bare-skill entry, got %#v", got[0].Skills)
	}
	if got[0].Skills[0].Registry != "" || got[0].Skills[0].Ref != "" || got[0].Skills[0].Path != "" {
		t.Fatalf("expected empty registry/ref/path, got %#v", got[0].Skills[0])
	}
}

func TestDiscoverCollectionsReportsInvalidYAML(t *testing.T) {
	skillDir := t.TempDir()
	dir := filepath.Join(skillDir, "collections", "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, collectionFileName), []byte("not: [valid yaml"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := DiscoverCollections(registryfs.Local(skillDir), "", "", "", "")
	if err == nil {
		t.Fatalf("expected error on invalid YAML")
	}
	if !strings.Contains(err.Error(), "parse collection file") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestDiscoverCollectionsRejectsMissingName(t *testing.T) {
	skillDir := t.TempDir()
	writeCollection(t, skillDir, "no-name", `description: missing name
`)

	_, err := DiscoverCollections(registryfs.Local(skillDir), "", "", "", "")
	if err == nil {
		t.Fatalf("expected error when name missing")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("expected 'name is required' error, got %v", err)
	}
}

func TestDiscoverCollectionsReturnsMultiple(t *testing.T) {
	skillDir := t.TempDir()
	writeCollection(t, skillDir, "first", "name: first\n")
	writeCollection(t, skillDir, "second", "name: second\nversion: 2.0\n")

	got, err := DiscoverCollections(registryfs.Local(skillDir), "", "git@example.com:r.git", "main", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 collections, got %d: %#v", len(got), got)
	}

	// Order is filesystem-dependent; validate both are present.
	seen := map[string]bool{}
	for _, c := range got {
		seen[c.Name] = true
	}
	if !seen["first"] || !seen["second"] {
		t.Fatalf("expected both collections, got %#v", seen)
	}
}

func TestDiscoverCollectionsStampsSourcePath(t *testing.T) {
	skillDir := t.TempDir()
	writeCollection(t, skillDir, "tools", `name: tools
skills:
  - name: efficient-go
    path: skills
`)

	got, err := DiscoverCollections(registryfs.Local(skillDir), "cache-key", "git@example.com:source.git", "main", ".factory/skills")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(got))
	}
	c := got[0]
	// SourcePath should match the path argument passed to DiscoverCollections.
	if c.SourcePath != ".factory/skills" {
		t.Fatalf("expected SourcePath .factory/skills, got %q", c.SourcePath)
	}
	// Collection skill's own path field should be parsed from YAML.
	if len(c.Skills) != 1 || c.Skills[0].Path != "skills" {
		t.Fatalf("expected skill with path 'skills', got %#v", c.Skills)
	}
}

// writeCollection helper creates skillDir/collections/<name>/COLLECTION.yaml.
func writeCollection(t *testing.T, skillDir, name, yaml string) {
	t.Helper()
	dir := filepath.Join(skillDir, "collections", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, collectionFileName), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write %q: %v", dir, err)
	}
}
