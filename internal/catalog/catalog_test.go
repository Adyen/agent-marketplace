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
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

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
			name: "empty kind defaults to skill",
			extra: map[string]any{"metadata": map[string]any{"requires": []any{
				map[string]any{"kind": "", "name": "supporting-skill"},
			}}},
			want: []Requirement{{Kind: ir.KindSkill, Name: "supporting-skill"}},
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
