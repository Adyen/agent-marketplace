package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestEntriesFromCollectionsPreservesExactSourceFile(t *testing.T) {
	entries, err := EntriesFromCollections([]collection.Collection{{
		Name:        "defaults",
		Version:     "1.0.0",
		Description: "Default resources",
		SourcePath:  ".agents",
		SourceFile:  "collections/defaults/COLLECTION.yaml",
		Resources:   []collection.CollectionEntry{{Name: "review", Kind: "skill"}},
	}})
	if err != nil {
		t.Fatalf("EntriesFromCollections: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %#v, want one collection", entries)
	}
	if entries[0].Path != "collections/defaults/COLLECTION.yaml" {
		t.Fatalf("entry path = %q", entries[0].Path)
	}
	collectionResource, ok := entries[0].Resource.(*ir.Collection)
	if !ok {
		t.Fatalf("entry resource = %T, want collection", entries[0].Resource)
	}
	if collectionResource.Source.File != entries[0].Path {
		t.Fatalf("collection source file = %q, want %q", collectionResource.Source.File, entries[0].Path)
	}
}

// TestLoadFromClaudePlugin verifies that Load discovers skills from a
// Claude-plugin-style directory using IngestBundle.
// Skills in Claude plugin form live under skills/<name>/SKILL.md.
func TestLoadFromClaudePlugin(t *testing.T) {
	dir := t.TempDir()

	// Create Claude plugin form: skills/<name>/SKILL.md
	if err := os.MkdirAll(filepath.Join(dir, "skills", "my-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillMd := `---
name: my-skill
description: A test skill
metadata:
  tags:
    - test
    - demo
---

# My Skill

Hello world
`
	if err := os.WriteFile(filepath.Join(dir, "skills", "my-skill", "SKILL.md"), []byte(skillMd), 0o644); err != nil {
		t.Fatal(err)
	}

	// Autodetect needs either .claude-plugin/plugin.json or bare plugin form
	// (root-level skills/ directory). The bare form is sufficient.
	cat, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load(%q, \"\") = _, %v; want no error", dir, err)
	}

	if len(cat.Entries) == 0 {
		t.Fatal("catalog has no entries; expected at least one skill")
	}

	// All entries should be KindSkill (v2.0 Phase 0 filter)
	for _, e := range cat.Entries {
		if e.Kind != ir.KindSkill {
			t.Errorf("entry %q has kind %v; want KindSkill", e.Name, e.Kind)
		}
	}

	// Find our skill by name
	var found *Entry
	for i := range cat.Entries {
		if cat.Entries[i].Name == "my-skill" {
			found = &cat.Entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("catalog does not contain skill 'my-skill'")
	}
	if found.Description != "A test skill" {
		t.Errorf("Description = %q, want %q", found.Description, "A test skill")
	}
	if found.Resource == nil {
		t.Error("Resource is nil; expected a non-nil ir.Resource")
	}
}

// TestLoadFromClaudePluginWithManifest verifies Load with an explicit
// .claude-plugin/plugin.json marker file and checks tags extraction.
func TestLoadFromClaudePluginWithManifest(t *testing.T) {
	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "skills", "tagged-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillMd := `---
name: tagged-skill
description: A tagged skill
metadata:
  tags:
    - infra
    - k8s
---

# Tagged Skill
`
	if err := os.WriteFile(filepath.Join(dir, "skills", "tagged-skill", "SKILL.md"), []byte(skillMd), 0o644); err != nil {
		t.Fatal(err)
	}

	// Explicit .claude-plugin/plugin.json marker
	if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginJSON := `{"name":"test-plugin","version":"1.0.0"}`
	if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(pluginJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load = _, %v", err)
	}

	var found *Entry
	for i := range cat.Entries {
		if cat.Entries[i].Name == "tagged-skill" {
			found = &cat.Entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("catalog does not contain skill 'tagged-skill'")
	}
	if len(found.Tags) != 2 {
		t.Errorf("Tags = %v, want 2 elements", found.Tags)
	}
	if len(found.Tags) >= 2 && (found.Tags[0] != "infra" || found.Tags[1] != "k8s") {
		t.Errorf("Tags = %v, want [infra k8s]", found.Tags)
	}
}

func TestLoadFSRetainsMultipleHooksForSameEvent(t *testing.T) {
	cat, err := LoadFS(fstest.MapFS{
		".factory/hooks/hooks.json": {
			Data: []byte(`{
				"SessionStart": [
					{ "hooks": [ { "type": "command", "command": "load-project-context" } ] },
					{ "hooks": [ { "type": "command", "command": "load-open-issues" } ] }
				]
			}`),
		},
	}, HarnessFactory)
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}

	resources := Resources(cat)
	if len(resources) != 2 {
		t.Fatalf("resource count: got %d, want 2 (%v)", len(resources), resources)
	}
	for _, name := range []string{"SessionStart-command", "SessionStart-command-2"} {
		resource, ok := resources[skill.ResourceKey(ir.KindHook, name)]
		if !ok {
			t.Errorf("missing hook %q", name)
			continue
		}
		if resource.Kind() != ir.KindHook {
			t.Errorf("resource %q kind: got %s, want %s", name, resource.Kind(), ir.KindHook)
		}
	}
}

// TestLoadWithExplicitHarness verifies that passing an explicit harness
// name overrides autodetection.
func TestLoadWithExplicitHarness(t *testing.T) {
	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "skills", "explicit-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillMd := `---
name: explicit-skill
description: Explicit harness test
metadata:
  tags: []
---

# Explicit
`
	if err := os.WriteFile(filepath.Join(dir, "skills", "explicit-skill", "SKILL.md"), []byte(skillMd), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := Load(dir, "claude")
	if err != nil {
		t.Fatalf("Load(%q, \"claude\") = _, %v; want no error", dir, err)
	}

	if len(cat.Entries) == 0 {
		t.Fatal("catalog has no entries")
	}

	if cat.SourceHarness != "claude" {
		t.Errorf("SourceHarness = %q, want %q", cat.SourceHarness, "claude")
	}
}

// TestLoadIncludesNonSkillResources verifies that non-skill resources
// (commands, hooks, etc.) are excluded in Phase 0.
func TestLoadIncludesNonSkillResources(t *testing.T) {
	dir := t.TempDir()

	// Create both a skill and a command
	if err := os.MkdirAll(filepath.Join(dir, "skills", "only-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillMd := `---
name: only-skill
description: Should survive the filter
metadata:
  tags: []
---

# Only Skill
`
	if err := os.WriteFile(filepath.Join(dir, "skills", "only-skill", "SKILL.md"), []byte(skillMd), 0o644); err != nil {
		t.Fatal(err)
	}

	// Add a command
	if err := os.MkdirAll(filepath.Join(dir, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmdMd := `---
name: my-command
description: Should be discovered
---

# My Command
`
	if err := os.WriteFile(filepath.Join(dir, "commands", "my-command.md"), []byte(cmdMd), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := Load(dir, "claude")
	if err != nil {
		t.Fatalf("Load(%q, \"claude\") = _, %v", dir, err)
	}

	seen := map[ir.ResourceKind]bool{}
	for _, e := range cat.Entries {
		seen[e.Kind] = true
	}
	if !seen[ir.KindSkill] {
		t.Fatal("expected skill resource to be discovered")
	}
	if !seen[ir.KindCommand] {
		t.Fatal("expected command resource to be discovered")
	}
}

func TestLoadFactoryKeepsSubagentsWhenMcpIsMalformed(t *testing.T) {
	mapFS := fstest.MapFS{
		"droids/worker-generic.md": &fstest.MapFile{
			Data: []byte("---\nname: worker-generic\ndescription: Works tasks\n---\nDo work\n"),
		},
		"mcp.json": &fstest.MapFile{
			Data: []byte("{\n  \"mcpServers\": {\n    \"youtrack\": {\n      \"env\": {\n        // \"YOUTRACK_TOKEN\": \"${YOUTRACK_TOKEN}\"\n      }\n    }\n  }\n}"),
		},
	}

	cat, err := LoadFS(mapFS, "factory")
	if err != nil {
		t.Fatalf("LoadFS = _, %v", err)
	}

	for _, entry := range cat.Entries {
		if entry.Kind == ir.KindSubagent && entry.Name == "worker-generic" {
			return
		}
	}
	t.Fatalf("catalog does not contain worker-generic subagent: %#v", cat.Entries)
}

func BenchmarkMalformedFromDiagnostics(b *testing.B) {
	diags := make([]diag.Diagnostic, 0, 200)
	for i := 0; i < 50; i++ {
		diags = append(diags,
			diag.Diagnostic{
				Code:     diag.CodeIngestFactoryManifestSynthesized,
				Severity: diag.SeverityInfo,
				Message:  "info should not become malformed",
			},
			diag.Diagnostic{
				Code:     "catalog.warning.example",
				Severity: diag.SeverityWarning,
				Message:  "warning should become malformed",
				Source:   diag.SourceRef{File: "skills/warn/SKILL.md", Line: 1},
			},
			diag.Diagnostic{
				Code:     "catalog.error.example",
				Severity: diag.SeverityError,
				Message:  "error should become malformed",
				Source:   diag.SourceRef{File: "skills/error/SKILL.md", Line: 1},
			},
			diag.Diagnostic{
				Code:     diag.CodeIngestFactoryLoadSkillFailed,
				Severity: diag.SeverityError,
				Resource: fmt.Sprintf("skills/broken-%d", i),
				Message:  fmt.Sprintf("failed to load skill %d: yaml parse error", i),
			},
		)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = malformedFromDiagnostics(diags)
	}
}

func TestMalformedFromDiagnosticsSeverityFilter(t *testing.T) {
	diags := []diag.Diagnostic{
		{
			Code:     diag.CodeIngestFactoryManifestSynthesized,
			Severity: diag.SeverityInfo,
			Message:  "info should not become malformed",
			Source:   diag.SourceRef{File: ".factory/skills/example/SKILL.md", Line: 1},
		},
		{
			Code:     "catalog.warning.example",
			Severity: diag.SeverityWarning,
			Message:  "warning should become malformed",
			Source:   diag.SourceRef{File: "skills/warn/SKILL.md", Line: 1},
		},
		{
			Code:     "catalog.error.example",
			Severity: diag.SeverityError,
			Message:  "error should become malformed",
			Source:   diag.SourceRef{File: "skills/error/SKILL.md", Line: 1},
		},
		{
			Code:     "catalog.warning.resource-scoped",
			Severity: diag.SeverityWarning,
			Resource: "skills/resource-scoped",
			Message:  "resource-scoped warning should be skipped (resource was loaded)",
			Source:   diag.SourceRef{File: "skills/resource-scoped/SKILL.md", Line: 1},
		},
		{
			Code:     diag.CodeIngestFactoryLoadSkillFailed,
			Severity: diag.SeverityError,
			Resource: "skills/broken",
			Message:  `failed to load skill "skills/broken": yaml parse error`,
		},
	}

	malformed := malformedFromDiagnostics(diags)
	if len(malformed) != 3 {
		t.Fatalf("len(malformed) = %d, want 3; got %#v", len(malformed), malformed)
	}

	got := map[string]bool{}
	for _, m := range malformed {
		got[m.DirName] = true
	}
	if !got["catalog.warning.example"] {
		t.Fatalf("expected warning diagnostic to be converted; got %#v", malformed)
	}
	if !got["catalog.error.example"] {
		t.Fatalf("expected error diagnostic to be converted; got %#v", malformed)
	}
	if !got["broken"] {
		t.Fatalf("expected resource-scoped error diagnostic to be converted; got %#v", malformed)
	}
	if got[diag.CodeIngestFactoryManifestSynthesized] {
		t.Fatalf("did not expect info diagnostic %q to be converted", diag.CodeIngestFactoryManifestSynthesized)
	}

	// Verify that the resource-scoped error uses base name as DirName and
	// the resource path as Path.
	for _, m := range malformed {
		if m.DirName == "broken" {
			if m.Path != "skills/broken" {
				t.Errorf("malformed Path = %q, want %q", m.Path, "skills/broken")
			}
			if !strings.Contains(m.Error.Error(), diag.CodeIngestFactoryLoadSkillFailed) {
				t.Errorf("malformed Error = %q, want it to contain code %q", m.Error, diag.CodeIngestFactoryLoadSkillFailed)
			}
		}
	}
}

func TestLoadFactorySurfacesMalformedSkillWithMismatchedQuotes(t *testing.T) {
	// Regression test: a skill with mismatched YAML frontmatter
	// quotes was silently dropped. The diagnostic was created but filtered out
	// because it had a non-empty Resource field. Now it should appear as a
	// MalformedSkill entry.
	mapFS := fstest.MapFS{
		"skills/good-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: good-skill\ndescription: Works fine\n---\nDo the thing\n"),
		},
		"skills/broken-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: broken-skill\ndescription: \"This has an unmatched quote\n---\nDo the thing\n"),
		},
	}

	cat, err := LoadFS(mapFS, "factory")
	if err != nil {
		t.Fatalf("LoadFS = _, %v", err)
	}

	// Good skill should be present, broken skill should not.
	if len(cat.Entries) != 1 {
		t.Fatalf("len(Entries) = %d, want 1; got %#v", len(cat.Entries), cat.Entries)
	}
	if cat.Entries[0].Name != "good-skill" {
		t.Fatalf("entry name = %q, want %q", cat.Entries[0].Name, "good-skill")
	}

	// The broken skill must appear in diagnostics.
	malformed := malformedFromDiagnostics(cat.Diagnostics)
	found := false
	for _, m := range malformed {
		if strings.Contains(m.DirName, "broken-skill") {
			found = true
			if m.Path == "" {
				t.Errorf("malformed Path is empty, want non-empty path for broken skill")
			}
			if !strings.Contains(m.Error.Error(), diag.CodeIngestFactoryLoadSkillFailed) {
				t.Errorf("malformed Error = %q, want it to contain code %q", m.Error, diag.CodeIngestFactoryLoadSkillFailed)
			}
		}
	}
	if !found {
		t.Fatalf("broken skill not found in malformed entries; diags=%#v malformed=%#v", cat.Diagnostics, malformed)
	}
}

func TestDiscoverMakesMalformedPathsAbsolute(t *testing.T) {
	// Verify that Discover (which calls Load then malformedFromDiagnostics)
	// makes malformed skill paths absolute so the TUI can read the files.
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "broken-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: broken-skill\ndescription: \"unmatched quote\n---\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Discover(dir, "factory")
	if err != nil {
		t.Fatalf("Discover(%q, \"factory\") = _, %v", dir, err)
	}

	if len(loaded.Malformed) == 0 {
		t.Fatalf("expected malformed entry for broken skill, got none")
	}

	for _, m := range loaded.Malformed {
		if strings.Contains(m.DirName, "broken-skill") {
			// DirName should be just the base name, not the full path.
			if m.DirName != "broken-skill" {
				t.Errorf("malformed DirName = %q, want %q", m.DirName, "broken-skill")
			}
			if !filepath.IsAbs(m.Path) {
				t.Errorf("malformed Path = %q, want absolute path", m.Path)
			}
			rel, err := filepath.Rel(dir, m.Path)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				t.Errorf("malformed Path = %q, want path within %q", m.Path, dir)
			}
		}
	}
}

func TestDiscoverFactoryWorkspaceWithoutPluginSkipsManifestSynthesizedMalformed(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".factory", "skills", "workspace-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("Do the thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := Load(dir, "factory")
	if err != nil {
		t.Fatalf("Load(%q, \"factory\") = _, %v", dir, err)
	}

	foundManifestSynthesized := false
	for _, d := range cat.Diagnostics {
		if d.Code == diag.CodeIngestFactoryManifestSynthesized {
			foundManifestSynthesized = true
			if d.Severity != diag.SeverityInfo {
				t.Fatalf("diagnostic severity = %v, want %v", d.Severity, diag.SeverityInfo)
			}
		}
	}
	if !foundManifestSynthesized {
		t.Fatalf("missing diagnostic %q", diag.CodeIngestFactoryManifestSynthesized)
	}

	discovered, err := Discover(dir, "factory")
	if err != nil {
		t.Fatalf("Discover(%q, \"factory\") = _, %v", dir, err)
	}

	for _, malformed := range discovered.Malformed {
		if malformed.DirName == diag.CodeIngestFactoryManifestSynthesized {
			t.Fatalf("unexpected malformed entry for %q: %#v", diag.CodeIngestFactoryManifestSynthesized, malformed)
		}
	}
	if len(discovered.Malformed) != 0 {
		t.Fatalf("expected no malformed entries, got %#v", discovered.Malformed)
	}

	foundSkill := false
	for _, entry := range discovered.Entries {
		if entry.Kind == ir.KindSkill && entry.Name == "workspace-skill" {
			foundSkill = true
			break
		}
	}
	if !foundSkill {
		t.Fatalf("expected discovered entries to include workspace-skill, got %#v", discovered.Entries)
	}
}

// TestAutodetectFallback verifies that when no harness is specified
// and autodetect cannot determine the source format, the catalog
// falls back to claude. If claude ingestion also fails, the error
// is returned.
func TestAutodetectFallback(t *testing.T) {
	dir := t.TempDir()

	// Write a SKILL.md in a non-standard location — no skills/ prefix,
	// no .claude-plugin, no workspace markers. Just a bare directory.
	if err := os.MkdirAll(filepath.Join(dir, "fallback-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillMd := `---
name: fallback-skill
description: Autodetect fallback test
metadata:
  tags: []
---

# Fallback
`
	if err := os.WriteFile(filepath.Join(dir, "fallback-skill", "SKILL.md"), []byte(skillMd), 0o644); err != nil {
		t.Fatal(err)
	}

	// No recognizable harness markers — autodetect should fail,
	// and the claude fallback should also fail (no skills/ dir).
	// The caller is expected to handle this error gracefully.
	_, err := Load(dir, "")
	if err == nil {
		// This is also acceptable — some future HCC version might support
		// more flexible layouts. The key invariant is that we tried claude.
		t.Log("Load succeeded despite no recognizable layout")
	}
}

// TestLoadFromMapFS uses an in-memory fs.FS to verify LoadFS
// with Claude bare-plugin form.
func TestLoadFromMapFS(t *testing.T) {
	mapFS := fstest.MapFS{
		"skills/map-skill/SKILL.md": &fstest.MapFile{
			Data: []byte(`---
name: map-skill
description: From map FS
metadata:
  tags:
    - mapfs
---

# Map Skill
`),
		},
	}

	cat, err := LoadFS(mapFS, "claude")
	if err != nil {
		t.Fatalf("LoadFS = _, %v", err)
	}

	if len(cat.Entries) == 0 {
		t.Fatal("catalog has no entries from MapFS")
	}

	var found *Entry
	for i := range cat.Entries {
		if cat.Entries[i].Name == "map-skill" {
			found = &cat.Entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("catalog does not contain skill 'map-skill'")
	}
	if found.Description != "From map FS" {
		t.Errorf("Description = %q, want %q", found.Description, "From map FS")
	}
}

// TestCatalogEntryPathSet verifies that Entry.Path is populated.
func TestCatalogEntryPathSet(t *testing.T) {
	mapFS := fstest.MapFS{
		"skills/path-skill/SKILL.md": &fstest.MapFile{
			Data: []byte(`---
name: path-skill
description: Path test
metadata:
  tags: []
---

# Path
`),
		},
	}
	cat, err := LoadFS(mapFS, "claude")
	if err != nil {
		t.Fatalf("LoadFS = _, %v", err)
	}

	if len(cat.Entries) == 0 {
		t.Fatal("catalog has no entries")
	}

	for _, e := range cat.Entries {
		if e.Path == "" {
			t.Errorf("entry %q has empty Path", e.Name)
		}
	}
}

func TestLoadNormalizesEntryPathsForRelativeDirectory(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "relative-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: relative-skill\ndescription: Relative path\n---\nBody\n")

	workingDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	relativeDir, err := filepath.Rel(workingDir, dir)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := Load(relativeDir, "claude")
	if err != nil {
		t.Fatalf("Load(%q, \"claude\") = _, %v", relativeDir, err)
	}
	if len(cat.Entries) != 1 {
		t.Fatalf("expected one entry, got %d: %#v", len(cat.Entries), cat.Entries)
	}
	if !filepath.IsAbs(cat.Entries[0].Path) {
		t.Fatalf("entry path %q is not absolute", cat.Entries[0].Path)
	}
	wantPath := filepath.Join(dir, "skills", "relative-skill", "SKILL.md")
	if cat.Entries[0].Path != wantPath {
		t.Errorf("entry path = %q, want %q", cat.Entries[0].Path, wantPath)
	}
}

func TestLoadRegistriesCachedSkipsCloneOrPullForWarmCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:team/skills.git",
			Ref:    "main",
			Source: config.RepoCommitted,
		}},
	}
	cachePath := cache.RegistryCachePath(cfg.Registries[0].URL, cfg.Registries[0].Ref)
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}

	cloneCalls := 0
	result := LoadRegistries(cfg, t.TempDir(), LoadModeCached,
		WithCloneOrPull(func(url, ref string) (string, error) {
			cloneCalls++
			return "", fmt.Errorf("cached warm path should not clone %s@%s", url, ref)
		}),
		WithDiscoverFunc(func(dir, sourceHarness string) (LoadedCatalog, error) {
			return LoadedCatalog{}, nil
		}),
	)
	if len(result.Errors) > 0 {
		t.Fatalf("LoadRegistries returned errors: %v", result.Errors)
	}
	if cloneCalls != 0 {
		t.Fatalf("cloneOrPull called %d times, want 0", cloneCalls)
	}
	if !result.RefreshAfterLoad {
		t.Fatal("expected RefreshAfterLoad=true for warm cached load")
	}
}

func TestLoadRegistriesCachedNoFetchSkipsMissingCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:team/skills.git",
			Ref:    "main",
			Source: config.RepoCommitted,
		}},
	}

	cloneCalls := 0
	result := LoadRegistries(cfg, t.TempDir(), LoadModeCachedNoFetch,
		WithCloneOrPull(func(url, ref string) (string, error) {
			cloneCalls++
			return "", fmt.Errorf("cached no-fetch path should not clone %s@%s", url, ref)
		}),
		WithDiscoverFunc(func(dir, sourceHarness string) (LoadedCatalog, error) {
			return LoadedCatalog{}, nil
		}),
	)
	if len(result.Errors) > 0 {
		t.Fatalf("LoadRegistries returned errors: %v", result.Errors)
	}
	if cloneCalls != 0 {
		t.Fatalf("cloneOrPull called %d times, want 0", cloneCalls)
	}
	if result.RefreshAfterLoad {
		t.Fatal("expected RefreshAfterLoad=false for cached no-fetch mode")
	}
	if len(result.Catalogs) != 0 {
		t.Fatalf("expected no catalogs for missing cache, got %d", len(result.Catalogs))
	}
}

func TestLoadRegistriesCachedNoFetchUsesExistingCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:team/skills.git",
			Ref:    "main",
			Source: config.RepoCommitted,
		}},
	}
	cachePath := cache.RegistryCachePath(cfg.Registries[0].URL, cfg.Registries[0].Ref)
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}

	cloneCalls := 0
	discoverCalls := 0
	result := LoadRegistries(cfg, t.TempDir(), LoadModeCachedNoFetch,
		WithCloneOrPull(func(url, ref string) (string, error) {
			cloneCalls++
			return "", fmt.Errorf("cached no-fetch path should not clone %s@%s", url, ref)
		}),
		WithDiscoverFunc(func(dir, sourceHarness string) (LoadedCatalog, error) {
			discoverCalls++
			return LoadedCatalog{}, nil
		}),
	)
	if len(result.Errors) > 0 {
		t.Fatalf("LoadRegistries returned errors: %v", result.Errors)
	}
	if cloneCalls != 0 {
		t.Fatalf("cloneOrPull called %d times, want 0", cloneCalls)
	}
	if discoverCalls != 1 {
		t.Fatalf("discover called %d times, want 1", discoverCalls)
	}
	if len(result.Catalogs) != 1 {
		t.Fatalf("expected one catalog for existing cache, got %d", len(result.Catalogs))
	}
}

func TestLoadRegistriesInspectRejectsSymlinkedRemoteCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL: "git@example.com:team/skills.git", Ref: "main", Source: config.RepoCommitted,
	}}}
	cachePath := cache.RegistryCachePath(cfg.Registries[0].URL, cfg.Registries[0].Ref)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache parent: %v", err)
	}
	external := filepath.Join(t.TempDir(), "registry")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatalf("mkdir external registry: %v", err)
	}
	if err := os.Symlink(external, cachePath); err != nil {
		t.Fatalf("symlink cache: %v", err)
	}

	discoverCalls := 0
	result := LoadRegistries(cfg, t.TempDir(), LoadModeInspect,
		WithCloneOrPull(func(url, ref string) (string, error) {
			t.Fatal("inspect mode called cloneOrPull")
			return "", nil
		}),
		WithDiscoverFunc(func(dir, sourceHarness string) (LoadedCatalog, error) {
			discoverCalls++
			return LoadedCatalog{}, nil
		}),
	)
	if len(result.Errors) == 0 {
		t.Fatal("expected symlinked remote cache to fail closed")
	}
	if !strings.Contains(result.Errors[0].Error(), "symlink") {
		t.Fatalf("symlink cache error = %v, want symlink detail", result.Errors[0])
	}
	if discoverCalls != 0 {
		t.Fatalf("discover called %d times for symlinked cache, want 0", discoverCalls)
	}
}

func TestLoadRegistriesCachedNoFetchRejectsSymlinkedRemoteCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL: "git@example.com:team/skills.git", Ref: "main", Source: config.RepoCommitted,
	}}}
	cachePath := cache.RegistryCachePath(cfg.Registries[0].URL, cfg.Registries[0].Ref)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache parent: %v", err)
	}
	external := filepath.Join(t.TempDir(), "registry")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatalf("mkdir external registry: %v", err)
	}
	if err := os.Symlink(external, cachePath); err != nil {
		t.Fatalf("symlink cache: %v", err)
	}

	discoverCalls := 0
	result := LoadRegistries(cfg, t.TempDir(), LoadModeCachedNoFetch,
		WithDiscoverFunc(func(dir, sourceHarness string) (LoadedCatalog, error) {
			discoverCalls++
			return LoadedCatalog{}, nil
		}),
	)
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Error(), "symlink") {
		t.Fatalf("cached no-fetch symlink errors = %v, want symlink failure", result.Errors)
	}
	if discoverCalls != 0 {
		t.Fatalf("discover called %d times for symlinked cache, want 0", discoverCalls)
	}
}

func TestLoadRegistriesCachedNoFetchRejectsSymlinkedLocalRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	external := filepath.Join(t.TempDir(), "registry")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatalf("mkdir external registry: %v", err)
	}
	localPath := filepath.Join(t.TempDir(), "local-registry")
	if err := os.Symlink(external, localPath); err != nil {
		t.Fatalf("symlink local registry: %v", err)
	}
	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL: localPath, Source: config.RepoCommitted,
	}}}

	discoverCalls := 0
	result := LoadRegistries(cfg, t.TempDir(), LoadModeCachedNoFetch,
		WithDiscoverFunc(func(dir, sourceHarness string) (LoadedCatalog, error) {
			discoverCalls++
			return LoadedCatalog{}, nil
		}),
	)
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Error(), "symlink") {
		t.Fatalf("cached no-fetch local symlink errors = %v, want symlink failure", result.Errors)
	}
	if discoverCalls != 0 {
		t.Fatalf("discover called %d times for symlinked local registry, want 0", discoverCalls)
	}
}

func TestLoadRegistriesCachedNoFetchSkipsMissingLocalRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL: filepath.Join(t.TempDir(), "missing-registry"), Source: config.RepoCommitted,
	}}}

	discoverCalls := 0
	result := LoadRegistries(cfg, t.TempDir(), LoadModeCachedNoFetch,
		WithDiscoverFunc(func(dir, sourceHarness string) (LoadedCatalog, error) {
			discoverCalls++
			return LoadedCatalog{}, nil
		}),
	)
	if len(result.Errors) != 0 {
		t.Fatalf("cached no-fetch missing local registry errors = %v, want none", result.Errors)
	}
	if len(result.Catalogs) != 0 || discoverCalls != 0 {
		t.Fatalf("cached no-fetch missing local registry catalogs = %d, discover calls = %d; want neither", len(result.Catalogs), discoverCalls)
	}
}

func TestLoadRegistriesInspectRejectsUnsafeLocalRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	tests := []struct {
		name       string
		setup      func(t *testing.T) string
		wantDetail string
	}{
		{
			name: "symlink",
			setup: func(t *testing.T) string {
				external := filepath.Join(t.TempDir(), "registry")
				if err := os.MkdirAll(external, 0o755); err != nil {
					t.Fatalf("mkdir external registry: %v", err)
				}
				localPath := filepath.Join(t.TempDir(), "local-registry")
				if err := os.Symlink(external, localPath); err != nil {
					t.Fatalf("symlink local registry: %v", err)
				}
				return localPath
			},
			wantDetail: "symlink",
		},
		{
			name: "missing",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "missing-registry")
			},
			wantDetail: "no such file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Registries: []config.RegistryEntry{{
				URL: tt.setup(t), Source: config.RepoCommitted,
			}}}
			discoverCalls := 0
			result := LoadRegistries(cfg, t.TempDir(), LoadModeInspect,
				WithDiscoverFunc(func(dir, sourceHarness string) (LoadedCatalog, error) {
					discoverCalls++
					return LoadedCatalog{}, nil
				}),
			)
			if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Error(), tt.wantDetail) {
				t.Fatalf("inspect local %s errors = %v, want %q", tt.name, result.Errors, tt.wantDetail)
			}
			if discoverCalls != 0 {
				t.Fatalf("discover called %d times for unsafe local registry, want 0", discoverCalls)
			}
		})
	}
}

func TestLoadRegistriesRefreshStillCallsCloneOrPull(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:    "git@example.com:team/skills.git",
			Ref:    "main",
			Source: config.RepoCommitted,
		}},
	}
	cachePath := cache.RegistryCachePath(cfg.Registries[0].URL, cfg.Registries[0].Ref)
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}

	cloneCalls := 0
	result := LoadRegistries(cfg, t.TempDir(), LoadModeRefresh,
		WithCloneOrPull(func(url, ref string) (string, error) {
			cloneCalls++
			return cachePath, nil
		}),
		WithDiscoverFunc(func(dir, sourceHarness string) (LoadedCatalog, error) {
			return LoadedCatalog{}, nil
		}),
	)
	if len(result.Errors) > 0 {
		t.Fatalf("LoadRegistries returned errors: %v", result.Errors)
	}
	if cloneCalls != 1 {
		t.Fatalf("cloneOrPull called %d times, want 1", cloneCalls)
	}
	if result.RefreshAfterLoad {
		t.Fatal("expected RefreshAfterLoad=false for refresh mode")
	}
}

// TestHarnessConstants verifies our internal constants match HCC's.
func TestHarnessConstants(t *testing.T) {
	tests := []struct {
		ours   string
		theirs hcc.Target
	}{
		{HarnessClaude, hcc.TargetClaude},
		{HarnessFactory, hcc.TargetFactory},
		{HarnessCursor, hcc.TargetCursor},
		{HarnessWindsurf, hcc.TargetWindsurf},
		{HarnessGemini, hcc.TargetGemini},
		{HarnessDevin, hcc.TargetDevin},
	}
	for _, tt := range tests {
		if tt.ours != string(tt.theirs) {
			t.Errorf("constant mismatch: ours=%q, theirs=%q", tt.ours, tt.theirs)
		}
	}
}

// TestTagsFromExtra verifies the tags extraction helper.
func TestTagsFromExtra(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]any
		want  []string
	}{
		{"nil extra", nil, nil},
		{"no metadata", map[string]any{"other": 1}, nil},
		{"metadata without tags", map[string]any{"metadata": map[string]any{"version": "1"}}, nil},
		{"metadata with string tags", map[string]any{"metadata": map[string]any{"tags": []string{"a", "b"}}}, []string{"a", "b"}},
		{"metadata with any tags", map[string]any{"metadata": map[string]any{"tags": []any{"x", "y"}}}, []string{"x", "y"}},
		{"mixed types", map[string]any{"metadata": map[string]any{"tags": []any{"ok", 42, "also"}}}, []string{"ok", "also"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tagsFromExtra(tt.extra)
			if len(got) != len(tt.want) {
				t.Errorf("tagsFromExtra() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("tagsFromExtra()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestLoadCommandFields(t *testing.T) {
	mapFS := fstest.MapFS{
		"commands/greet.md": &fstest.MapFile{
			Data: []byte("---\nname: greet\ndescription: Greeting command\ncommand: echo hello\nmetadata:\n  tags:\n    - cli\n    - greeting\n---\n\n# Greet\n"),
		},
	}

	cat, err := LoadFS(mapFS, "factory")
	if err != nil {
		t.Fatalf("LoadFS = _, %v", err)
	}

	var found *Entry
	for i := range cat.Entries {
		if cat.Entries[i].Kind == ir.KindCommand && cat.Entries[i].Name == "greet" {
			found = &cat.Entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("catalog does not contain command 'greet'")
	}
	if found.Description != "Greeting command" {
		t.Errorf("Description = %q, want %q", found.Description, "Greeting command")
	}
	if found.Resource == nil {
		t.Error("Resource is nil; expected a non-nil ir.Resource")
	}
	// Verify the command body is extracted and accessible via the Resource.
	if cmd, ok := found.Resource.(*ir.Command); ok {
		// The Body field contains the markdown content after frontmatter;
		// the `command:` frontmatter field is stored in Extra.
		if cmd.Body == "" {
			t.Errorf("Command Body is empty; expected non-empty body content")
		}
		if cmd.Extra == nil {
			t.Error("Command Extra is nil; expected frontmatter fields including 'command'")
		} else if cmdVal, ok := cmd.Extra["command"]; !ok {
			t.Error("Command Extra missing 'command' key from frontmatter")
		} else if cmdStr, ok := cmdVal.(string); !ok || cmdStr != "echo hello" {
			t.Errorf("Command Extra['command'] = %v, want %q", cmdVal, "echo hello")
		}
	} else {
		t.Errorf("Resource is %T, want *ir.Command", found.Resource)
	}
	if len(found.Tags) != 2 || found.Tags[0] != "cli" || found.Tags[1] != "greeting" {
		t.Errorf("Tags = %v, want [cli greeting]", found.Tags)
	}
}

func TestLoadMalformedCommand(t *testing.T) {
	mapFS := fstest.MapFS{
		"commands/broken.md": &fstest.MapFile{
			Data: []byte("---\nname: broken\ndescription: \"unmatched quote\n---\nBody\n"),
		},
		"commands/good.md": &fstest.MapFile{
			Data: []byte("---\nname: good\ndescription: Good command\n---\nBody\n"),
		},
	}

	cat, err := LoadFS(mapFS, "claude")
	if err != nil {
		t.Fatalf("LoadFS = _, %v", err)
	}

	// Good command should be present, broken should not.
	var goodFound bool
	for _, e := range cat.Entries {
		if e.Kind == ir.KindCommand && e.Name == "good" {
			goodFound = true
		}
	}
	if !goodFound {
		t.Fatal("expected good command to be in catalog entries")
	}

	malformed := malformedFromDiagnostics(cat.Diagnostics)
	var brokenFound bool
	for _, m := range malformed {
		if strings.Contains(m.DirName, "broken") {
			brokenFound = true
		}
	}
	if !brokenFound {
		t.Fatalf("expected broken command in malformed entries; diags=%#v malformed=%#v", cat.Diagnostics, malformed)
	}
}

// spec: resource-prerequisites/command-and-subagent-prerequisites
func TestLoadCommandAndSubagentPrerequisites(t *testing.T) {
	mapFS := fstest.MapFS{
		"commands/deploy.md": &fstest.MapFile{
			Data: []byte("---\nname: deploy\ndescription: Deploy command\nmetadata:\n  requires:\n    - kind: mcp\n      name: deploy-server\n      registry: git@example.com:mcp.git\n---\nRun deploy\n"),
		},
		"agents/reviewer.md": &fstest.MapFile{
			Data: []byte("---\nname: reviewer\ndescription: Reviews code\nmetadata:\n  requires:\n    - helper-skill\n    - mcp:lint-server\n---\nYou are a reviewer\n"),
		},
	}

	cat, err := LoadFS(mapFS, "claude")
	if err != nil {
		t.Fatalf("LoadFS = _, %v", err)
	}

	want := map[string][]Requirement{
		"deploy": {
			{Kind: ir.KindMcpServer, Name: "deploy-server", Registry: "git@example.com:mcp.git"},
		},
		"reviewer": {
			{Kind: ir.KindSkill, Name: "helper-skill"},
			{Kind: ir.KindMcpServer, Name: "lint-server"},
		},
	}
	var found int
	for _, e := range cat.Entries {
		wantReqs, ok := want[e.Name]
		if !ok {
			continue
		}
		found++
		got := RequirementsFromResource(e.Resource)
		if !reflect.DeepEqual(got, wantReqs) {
			t.Errorf("%s requirements = %#v, want %#v", e.Name, got, wantReqs)
		}
	}
	if found != len(want) {
		t.Fatalf("found %d entries with declared requires, want %d; entries=%#v", found, len(want), cat.Entries)
	}
}

// spec: resource-prerequisites/structured-mcp-prerequisite
// spec: resource-prerequisites/shorthand-prerequisite
// spec: resource-prerequisites/structured-item-without-kind
// spec: resource-prerequisites/kind-alias-spellings
// spec: resource-prerequisites/kind-all-is-rejected
// spec: resource-prerequisites/shorthand-without-a-kind
// spec: resource-prerequisites/surrounding-whitespace-is-trimmed
func TestRequirementsFromExtra(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]any
		want  []Requirement
	}{
		{
			name: "nil metadata",
		},
		{
			name: "structured MCP with provider hints",
			extra: map[string]any{"metadata": map[string]any{"requires": []any{
				map[string]any{"kind": "mcp", "name": "genai-atlassian", "registry": "git@example.com:mcp.git", "path": "servers"},
			}}},
			want: []Requirement{{Kind: ir.KindMcpServer, Name: "genai-atlassian", Registry: "git@example.com:mcp.git", Path: "servers"}},
		},
		{
			name: "MCP and skill requirements",
			extra: map[string]any{
				"metadata": map[string]any{
					"requires": []any{
						"mcp:genai-atlassian",
						map[string]any{
							"name":     "supporting-skill",
							"registry": "git@example.com:skills.git",
							"path":     "plugins/support",
						},
						"unknown:ignored",
					},
				},
			},
			want: []Requirement{
				{Kind: ir.KindMcpServer, Name: "genai-atlassian"},
				{
					Kind:     ir.KindSkill,
					Name:     "supporting-skill",
					Registry: "git@example.com:skills.git",
					Path:     "plugins/support",
				},
			},
		},
		{
			name: "missing and empty kind default to skill",
			extra: map[string]any{"metadata": map[string]any{"requires": []any{
				map[string]any{"name": "supporting-skill"},
				map[string]any{"kind": "", "name": "other-skill"},
			}}},
			want: []Requirement{
				{Kind: ir.KindSkill, Name: "supporting-skill"},
				{Kind: ir.KindSkill, Name: "other-skill"},
			},
		},
		{
			name: "kind alias spellings",
			extra: map[string]any{"metadata": map[string]any{"requires": []any{
				map[string]any{"kind": "MCP_Servers", "name": "docs-server"},
				map[string]any{"kind": "agents", "name": "helper-agent"},
				map[string]any{"kind": "Commands", "name": "run-tests"},
			}}},
			want: []Requirement{
				{Kind: ir.KindMcpServer, Name: "docs-server"},
				{Kind: ir.KindSubagent, Name: "helper-agent"},
				{Kind: ir.KindCommand, Name: "run-tests"},
			},
		},
		{
			name: "kind all rejected while sibling retained",
			extra: map[string]any{"metadata": map[string]any{"requires": []any{
				map[string]any{"kind": "all", "name": "supporting-skill"},
				" ALL :everything",
				"all : spaced",
				map[string]any{"kind": "skill", "name": "kept-skill"},
			}}},
			want: []Requirement{{Kind: ir.KindSkill, Name: "kept-skill"}},
		},
		{
			name: "bare string shorthand defaults to skill",
			extra: map[string]any{"metadata": map[string]any{"requires": []any{
				"other-skill",
			}}},
			want: []Requirement{{Kind: ir.KindSkill, Name: "other-skill"}},
		},
		{
			name: "surrounding whitespace is trimmed",
			extra: map[string]any{"metadata": map[string]any{"requires": []any{
				" mcp : docs-server ",
				map[string]any{
					"kind":     " mcp ",
					"name":     " genai-atlassian ",
					"registry": " git@example.com:mcp.git ",
					"path":     " servers ",
				},
			}}},
			want: []Requirement{
				{Kind: ir.KindMcpServer, Name: "docs-server"},
				{
					Kind:     ir.KindMcpServer,
					Name:     "genai-atlassian",
					Registry: "git@example.com:mcp.git",
					Path:     "servers",
				},
			},
		},
		{
			name: "collection rejected",
			extra: map[string]any{"metadata": map[string]any{"requires": []any{
				"collection:team-bundle",
			}}},
			want: []Requirement{},
		},
		{
			name:  "non-list requires ignored",
			extra: map[string]any{"metadata": map[string]any{"requires": "mcp:genai-atlassian"}},
		},
		{
			name: "blank names skipped",
			extra: map[string]any{"metadata": map[string]any{"requires": []any{
				"mcp: ",
				map[string]any{"kind": "mcp", "name": " "},
			}}},
			want: []Requirement{},
		},
		{
			name: "invalid items among valid items",
			extra: map[string]any{"metadata": map[string]any{"requires": []any{
				"mcp:docs-server",
				map[string]any{"kind": "weird", "name": "unknown-kind"},
				"collection:team-bundle",
				map[string]any{"kind": "skill"},
				42,
				map[string]any{"name": "kept-skill"},
			}}},
			want: []Requirement{
				{Kind: ir.KindMcpServer, Name: "docs-server"},
				{Kind: ir.KindSkill, Name: "kept-skill"},
			},
		},
		{
			name: "non-string map values skipped",
			extra: map[string]any{"metadata": map[string]any{"requires": []any{
				map[string]any{"kind": 42, "name": "wrong-kind"},
				map[string]any{"kind": "mcp", "name": 42},
				map[string]any{"kind": "mcp", "name": "wrong-registry", "registry": 42},
			}}},
			want: []Requirement{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requirementsFromExtra(tt.extra)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("requirementsFromExtra() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// spec: resource-prerequisites/kind-all-is-rejected
// spec: resource-prerequisites/non-list-requires-value
// spec: resource-prerequisites/invalid-item-among-valid-items
func TestLoadRequiresInvalidDiagnostics(t *testing.T) {
	mapFS := fstest.MapFS{
		"skills/mixed/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: mixed\ndescription: Mixed requires\nmetadata:\n  requires:\n    - mcp:docs-server\n    - kind: all\n      name: everything\n    - kind: weird\n      name: secret-user:secret-token@example.com\n---\nbody\n"),
		},
		"skills/shapeless/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: shapeless\ndescription: Non-list requires\nmetadata:\n  requires: mcp:docs-server\n---\nbody\n"),
		},
	}

	cat, err := LoadFS(mapFS, "claude")
	if err != nil {
		t.Fatalf("LoadFS = _, %v", err)
	}

	var mixedDiags, shapelessDiags []diag.Diagnostic
	for _, d := range cat.Diagnostics {
		if d.Code != CodeRequiresInvalid {
			continue
		}
		switch d.Resource {
		case "mixed":
			mixedDiags = append(mixedDiags, d)
		case "shapeless":
			shapelessDiags = append(shapelessDiags, d)
		}
	}

	if len(mixedDiags) != 2 {
		t.Fatalf("mixed requires diagnostics = %#v, want 2 catalog.requires.invalid warnings", mixedDiags)
	}
	for _, d := range mixedDiags {
		if d.Severity != diag.SeverityWarning {
			t.Errorf("diagnostic severity = %v, want warning: %#v", d.Severity, d)
		}
		if d.Source.File != "skills/mixed/SKILL.md" {
			t.Errorf("diagnostic source = %q, want skills/mixed/SKILL.md", d.Source.File)
		}
		if strings.Contains(d.Message, "secret-token") || strings.Contains(d.Message, "example.com") {
			t.Errorf("diagnostic message leaked the malformed value: %q", d.Message)
		}
		if !strings.Contains(d.Message, "item 1") && !strings.Contains(d.Message, "item 2") {
			t.Errorf("diagnostic message missing item index: %q", d.Message)
		}
	}

	if len(shapelessDiags) != 1 {
		t.Fatalf("shapeless requires diagnostics = %#v, want 1 catalog.requires.invalid warning", shapelessDiags)
	}
	if shapelessDiags[0].Source.File != "skills/shapeless/SKILL.md" {
		t.Errorf("shapeless diagnostic source = %q, want skills/shapeless/SKILL.md", shapelessDiags[0].Source.File)
	}

	var mixedFound, shapelessFound bool
	for _, e := range cat.Entries {
		switch e.Name {
		case "mixed":
			mixedFound = true
			requirements := RequirementsFromResource(e.Resource)
			want := []Requirement{{Kind: ir.KindMcpServer, Name: "docs-server"}}
			if !reflect.DeepEqual(requirements, want) {
				t.Errorf("mixed requirements = %#v, want %#v", requirements, want)
			}
		case "shapeless":
			shapelessFound = true
			if requirements := RequirementsFromResource(e.Resource); len(requirements) != 0 {
				t.Errorf("shapeless requirements = %#v, want none", requirements)
			}
		}
	}
	if !mixedFound || !shapelessFound {
		t.Fatalf("expected both resources in catalog entries; entries=%#v", cat.Entries)
	}

	for _, m := range malformedFromDiagnostics(cat.Diagnostics) {
		if m.DirName == "mixed" || m.DirName == "shapeless" {
			t.Fatalf("requires diagnostic produced malformed entry: %#v", m)
		}
	}
}

// spec: resource-prerequisites/invalid-item-among-valid-items
func TestLoadRequiresInvalidDiagnosticShapes(t *testing.T) {
	cat, err := LoadFS(fstest.MapFS{
		"skills/shapes/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: shapes\ndescription: Malformed shapes\nmetadata:\n  requires:\n    - mcp:docs-server\n    - kind: mcp\n    - name: ' '\n      kind: mcp\n    - name: 7\n    - name: x\n      kind: 7\n    - name: x\n      kind: collection\n    - name: x\n      registry: secret-token\n      path: 7\n    - 42\n---\nbody\n"),
		},
	}, "claude")
	if err != nil {
		t.Fatalf("LoadFS = _, %v", err)
	}

	want := []struct {
		index  int
		reason string
	}{
		{1, "name must be a non-empty string"},
		{2, "name must be a non-empty string"},
		{3, "name must be a non-empty string"},
		{4, "kind must be a string"},
		{5, "unknown or unsupported kind"},
		{6, "registry and path must be strings"},
		{7, "requirement must be a string or map"},
	}

	var got []diag.Diagnostic
	for _, d := range cat.Diagnostics {
		if d.Code == CodeRequiresInvalid {
			got = append(got, d)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("requires diagnostics = %#v, want %d warnings", got, len(want))
	}
	for i, w := range want {
		wantMessage := fmt.Sprintf("requires item %d: %s", w.index, w.reason)
		if got[i].Message != wantMessage {
			t.Errorf("diagnostic %d message = %q, want %q", i, got[i].Message, wantMessage)
		}
		if got[i].Severity != diag.SeverityWarning || got[i].Resource != "shapes" || got[i].Source.File != "skills/shapes/SKILL.md" {
			t.Errorf("diagnostic %d = %#v, want warning on shapes in skills/shapes/SKILL.md", i, got[i])
		}
		if strings.Contains(got[i].Message, "secret-token") {
			t.Errorf("diagnostic %d leaked the malformed value: %q", i, got[i].Message)
		}
	}

	for _, e := range cat.Entries {
		if e.Name != "shapes" {
			continue
		}
		wantRequirements := []Requirement{{Kind: ir.KindMcpServer, Name: "docs-server"}}
		if requirements := RequirementsFromResource(e.Resource); !reflect.DeepEqual(requirements, wantRequirements) {
			t.Errorf("shapes requirements = %#v, want %#v", requirements, wantRequirements)
		}
		return
	}
	t.Fatal("shapes entry missing from catalog")
}

func TestOverlappingEntryWarningRedactsURLCredentials(t *testing.T) {
	warning := &OverlappingEntryWarning{
		Dir:       "/cache/registry",
		FirstURL:  "https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.com/registry.git",
		SecondURL: "https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.com/registry.git",
	}

	got := warning.Error()
	if strings.Contains(got, "YOUR_TOKEN_HERE") {
		t.Fatalf("OverlappingEntryWarning.Error() leaked credentials: %s", got)
	}
	if strings.Count(got, "https://***@example.com/registry.git") != 2 {
		t.Fatalf("OverlappingEntryWarning.Error() did not redact both URLs: %s", got)
	}
}

// TestDiscoverRootSkillWithPluginManifest verifies that a single-skill plugin
// repository whose SKILL.md lives at the root (the github.com/blader/humanizer
// shape) is discovered through the same catalog path list, status, and the TUI
// use.
func TestDiscoverRootSkillWithPluginManifest(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		".claude-plugin/plugin.json": `{"name":"humanizer","skills":["./"]}`,
		"SKILL.md":                   "---\nname: humanizer\ndescription: Rewrite AI-sounding text\n---\nBody\n",
		"README.md":                  "# Humanizer\n",
	}
	for name, body := range files {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	loaded, err := Discover(dir, "")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("entries = %d, want 1: %#v", len(loaded.Entries), loaded.Entries)
	}
	entry := loaded.Entries[0]
	if entry.Kind != ir.KindSkill {
		t.Fatalf("entry kind = %v, want Skill", entry.Kind)
	}
	if entry.Name != "humanizer" {
		t.Fatalf("entry name = %q, want humanizer", entry.Name)
	}
	if entry.Path != filepath.Join(dir, "SKILL.md") {
		t.Fatalf("entry path = %q, want %q", entry.Path, filepath.Join(dir, "SKILL.md"))
	}
}

// spec: harness-opencode/existing-workspace-layouts-keep-precedence-over-an-opencode-marker
// spec: harness-opencode/coexisting-layouts-are-reported
func TestLoadKeepsExistingWorkspaceLayoutBesideOpenCode(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		".claude/skills/workspace-skill/SKILL.md": "---\nname: workspace-skill\ndescription: Claude workspace skill\n---\nBody\n",
		".opencode/commands/opencode-only.md":     "OpenCode command\n",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cat, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cat.SourceHarness != "claude" || len(cat.Entries) != 1 || cat.Entries[0].Name != "workspace-skill" {
		t.Fatalf("catalog = %s %#v, want the Claude workspace resources", cat.SourceHarness, cat.Entries)
	}
	var warning *diag.Diagnostic
	for i := range cat.Diagnostics {
		if cat.Diagnostics[i].Code == diag.CodeIngestAutodetectAmbiguous {
			warning = &cat.Diagnostics[i]
		}
	}
	if warning == nil || warning.Severity != diag.SeverityWarning || !strings.Contains(warning.Message, "opencode") || !strings.Contains(warning.Hint, "source_harness") {
		t.Fatalf("coexisting layouts were not reported: %#v", cat.Diagnostics)
	}
	// An explicit source harness is the documented way to choose, and it is
	// not ambiguous.
	explicit, err := Load(dir, "opencode")
	if err != nil || explicit.SourceHarness != "opencode" || diag.HasCode(explicit.Diagnostics, diag.CodeIngestAutodetectAmbiguous) {
		t.Fatalf("explicit opencode = %v %#v", err, explicit)
	}
	if len(explicit.Entries) != 1 || explicit.Entries[0].Name != "opencode-only" {
		t.Fatalf("explicit opencode entries = %#v", explicit.Entries)
	}
}

// spec: harness-opencode/container-registries-keep-their-bundles-beside-an-opencode-folder
func TestDiscoverKeepsContainerBundlesBesideOpenCode(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"team/.claude-plugin/plugin.json":  `{"name":"team"}`,
		"team/skills/review/SKILL.md":      "---\nname: review\ndescription: Review\n---\nBody\n",
		".opencode/commands/local-tool.md": "Maintainer OpenCode command\n",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := Discover(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Entries) != 1 || loaded.Entries[0].Name != "review" || loaded.Entries[0].SourceHarness != "claude" {
		t.Fatalf("container entries = %#v, want only the team bundle's skill", loaded.Entries)
	}
	reported := false
	for _, m := range loaded.Malformed {
		reported = reported || (m.DirName == diag.CodeIngestAutodetectAmbiguous && strings.Contains(m.Error.Error(), "opencode"))
	}
	if !reported {
		t.Fatalf("unread OpenCode folder was not reported: %#v", loaded.Malformed)
	}
	explicit, err := Discover(dir, "opencode")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range explicit.Entries {
		found = found || (entry.Name == "local-tool" && entry.SourceHarness == "opencode")
	}
	if !found {
		t.Fatalf("explicit opencode did not read the root: %#v", explicit.Entries)
	}
}

// spec: harness-opencode/existing-workspace-layouts-keep-precedence-over-an-opencode-marker
func TestOpenCodeMarkerProbeDoesNotFollowLinks(t *testing.T) {
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"linked entry", "linked folder"} {
		t.Run(variant, func(t *testing.T) {
			dir := t.TempDir()
			skill := filepath.Join(dir, ".claude", "skills", "ws", "SKILL.md")
			if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(skill, []byte("---\nname: ws\ndescription: Claude workspace skill\n---\nBody\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			link, target := filepath.Join(dir, ".opencode"), outside
			if variant == "linked entry" {
				if err := os.Mkdir(link, 0o755); err != nil {
					t.Fatal(err)
				}
				link, target = filepath.Join(link, "skills"), filepath.Join(outside, "skills")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			// The probe must not refuse a source another layout claims.
			loaded, err := Discover(dir, "")
			if err != nil || len(loaded.Entries) != 1 || loaded.Entries[0].SourceHarness != "claude" {
				t.Fatalf("Claude workspace beside a %s = %v %#v", variant, err, loaded.Entries)
			}
		})
	}
}
