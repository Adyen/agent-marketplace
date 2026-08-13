package factory_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	harnesscompile "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/factory"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// This file lives in package factory_test on purpose: it exercises the Factory
// ingester through the root harnesscompile facade, which registers every
// built-in compiler and ingester. An internal (package factory) test could not
// import the root package without an import cycle.

func TestDetect_PluginAndWorkspaceForms(t *testing.T) {
	loader := factory.New()

	if !loader.Detect(fstest.MapFS{
		".factory-plugin/plugin.json": {Data: []byte(`{"skills":[]}`)},
	}) {
		t.Fatal("Detect should match Factory plugin form")
	}

	if !loader.Detect(fstest.MapFS{
		".factory/skills/my-skill/SKILL.md": {Data: []byte("Do the thing\n")},
	}) {
		t.Fatal("Detect should match Factory workspace form")
	}
}

func TestLoadBundle_PluginFormIdentityCompilesToFactory(t *testing.T) {
	src := fstest.MapFS{
		".factory-plugin/plugin.json": {
			Data: []byte("{\n  \"droids\": [\n    {\n      \"description\": \"Reviews code\",\n      \"model\": \"claude-sonnet-4-5-20250929\",\n      \"name\": \"reviewer\",\n      \"reasoningEffort\": \"high\",\n      \"tools\": [\n        \"Read\",\n        \"Edit\",\n        \"Create\"\n      ]\n    }\n  ],\n  \"skills\": [\n    {\n      \"description\": \"\",\n      \"name\": \"my-skill\"\n    }\n  ]\n}"),
		},
		"skills/my-skill/SKILL.md": {
			Data: []byte("Do the thing\n"),
		},
		"commands/my-cmd.md": {
			Data: []byte("echo hi\n"),
		},
		"droids/reviewer.md": {
			Data: []byte("---\nname: reviewer\ndescription: Reviews code\nmodel: claude-sonnet-4-5-20250929\ntools:\n  - Read\n  - Edit\n  - Create\nreasoningEffort: high\n---\nReview code\n"),
		},
		"hooks/hooks.json": {
			Data: []byte("{\n  \"PreToolUse\": [\n    {\n      \"hooks\": [\n        {\n          \"command\": \"${DROID_PLUGIN_ROOT}/check.sh\",\n          \"type\": \"command\"\n        }\n      ],\n      \"matcher\": \"Read\"\n    }\n  ]\n}"),
		},
		"mcp.json": {
			Data: []byte("{\n  \"mcpServers\": {\n    \"docs\": {\n      \"args\": [\n        \"-y\",\n        \"docs-server\"\n      ],\n      \"command\": \"npx\"\n    }\n  }\n}"),
		},
	}

	bundle, err := factory.New().LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if len(bundle.Resources) != 5 {
		t.Fatalf("len(Resources) = %d, want 5", len(bundle.Resources))
	}

	var arts []artifact.Artifact
	for _, resource := range bundle.Resources {
		result, err := harnesscompile.Compile(resource, harnesscompile.TargetFactory)
		if err != nil {
			t.Fatalf("Compile(%s, factory): %v", resource.Name(), err)
		}
		arts = append(arts, result.Artifacts...)
	}

	merged, err := artifact.MergeFragments(arts)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Path < merged[j].Path })

	expectedPaths := []string{
		"commands/my-cmd.md",
		"droids/reviewer.md",
		"hooks/hooks.json",
		"mcp.json",
		"skills/my-skill/SKILL.md",
	}
	if len(merged) != len(expectedPaths) {
		t.Fatalf("len(merged) = %d, want %d", len(merged), len(expectedPaths))
	}

	for i, path := range expectedPaths {
		if merged[i].Path != path {
			t.Fatalf("artifact[%d].Path = %q, want %q", i, merged[i].Path, path)
		}
		// Skills get frontmatter injected from IR metadata during compilation,
		// so the compiled SKILL.md will differ from the source when the source
		// lacked frontmatter. Verify the compiled content contains the body.
		if path == "skills/my-skill/SKILL.md" {
			if !strings.Contains(string(merged[i].Bytes), "Do the thing") {
				t.Fatalf("artifact %q missing body content: %q", path, string(merged[i].Bytes))
			}
			if !strings.Contains(string(merged[i].Bytes), "name: my-skill") {
				t.Fatalf("artifact %q missing frontmatter name: %q", path, string(merged[i].Bytes))
			}
			continue
		}
		want, err := fs.ReadFile(src, path)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", path, err)
		}
		if string(merged[i].Bytes) != string(want) {
			t.Fatalf("artifact %q mismatch:\n got: %q\nwant: %q", path, string(merged[i].Bytes), string(want))
		}
	}
}

func TestLoadBundle_InvalidMcpDoesNotDropDroids(t *testing.T) {
	src := fstest.MapFS{
		"droids/worker-generic.md": {
			Data: []byte("---\nname: worker-generic\ndescription: Works tasks\n---\nDo work\n"),
		},
		"mcp.json": {
			Data: []byte("{\n  \"mcpServers\": {\n    \"youtrack\": {\n      \"env\": {\n        // \"YOUTRACK_TOKEN\": \"${YOUTRACK_TOKEN}\"\n      }\n    }\n  }\n}"),
		},
	}

	bundle, err := factory.New().LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	// JavaScript-style comments are stripped before parsing, so the MCP
	// server should now load successfully. The key invariant remains:
	// subagents are discovered even when the mcp.json contains comments.
	for _, resource := range bundle.Resources {
		if resource.Kind() == ir.KindSubagent && resource.Name() == "worker-generic" {
			// Also verify the youtrack MCP server was discovered.
			for _, r2 := range bundle.Resources {
				if r2.Kind() == ir.KindMcpServer && r2.Name() == "youtrack" {
					return
				}
			}
			t.Fatalf("missing youtrack MCP server in resources: %#v", bundle.Resources)
		}
	}
	t.Fatalf("missing worker-generic subagent in resources: %#v", bundle.Resources)
}

// TestLoadBundle_SymlinkSkillDirs tests that skills installed as symlinks
// (as done by agent-marketplace sync for workspace/.factory/skills/<name>) are
// correctly discovered by IngestBundle. Prior to the listSubdirs fix,
// os.DirFS entries for symlinks reported ModeSymlink rather than IsDir,
// so listSubdirs returned empty and convert produced 0 files.
func TestLoadBundle_SymlinkSkillDirs(t *testing.T) {
	// Create a real cache directory that holds the actual skill content.
	cacheDir := t.TempDir()
	skillCache := filepath.Join(cacheDir, "my-skill")
	if err := os.MkdirAll(skillCache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillCache, "SKILL.md"), []byte("Do the thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a workspace with .factory/skills/my-skill → symlink to cache.
	workDir := t.TempDir()
	skillsDir := filepath.Join(workDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkPath := filepath.Join(skillsDir, "my-skill")
	if err := os.Symlink(skillCache, symlinkPath); err != nil {
		t.Fatal(err)
	}

	src := os.DirFS(workDir)
	bundle, err := factory.New().LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if len(bundle.Resources) == 0 {
		t.Fatal("expected at least one resource from symlinked skills dir, got 0")
	}
	found := false
	for _, r := range bundle.Resources {
		if r.Kind() == ir.KindSkill && r.Name() == "my-skill" {
			found = true
		}
	}
	if !found {
		t.Fatalf("skill 'my-skill' not found in resources: %v", bundle.Resources)
	}
}

func TestLoadBundle_WorkspaceFormCompilesToClaude(t *testing.T) {
	src := fstest.MapFS{
		".factory/skills/my-skill/SKILL.md": {
			Data: []byte("Do the thing\n"),
		},
		".factory/commands/my-cmd.md": {
			Data: []byte("echo hi\n"),
		},
		".factory/droids/reviewer.md": {
			Data: []byte("---\nname: reviewer\ndescription: Reviews code\nmodel: claude-sonnet-4-5-20250929\ntools:\n  - Read\n  - Edit\n  - Create\nreasoningEffort: high\n---\nReview code\n"),
		},
		".factory/hooks/hooks.json": {
			Data: []byte(`{"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/check.sh"}]}]}`),
		},
		".factory/mcp.json": {
			Data: []byte(`{"mcpServers":{"docs":{"command":"npx","args":["-y","docs-server"]}}}`),
		},
	}

	bundle, err := factory.New().LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if len(bundle.Resources) != 5 {
		t.Fatalf("len(Resources) = %d, want 5", len(bundle.Resources))
	}
	if !diag.HasCode(bundle.Diagnostics, diag.CodeIngestFactoryManifestSynthesized) {
		t.Fatalf("missing diagnostic %q", diag.CodeIngestFactoryManifestSynthesized)
	}

	var arts []artifact.Artifact
	for _, resource := range bundle.Resources {
		result, err := harnesscompile.Compile(resource, harnesscompile.TargetClaude)
		if err != nil {
			t.Fatalf("Compile(%s, claude): %v", resource.Name(), err)
		}
		arts = append(arts, result.Artifacts...)
	}

	merged, err := artifact.MergeFragments(arts)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}

	pathSet := make(map[string]bool, len(merged))
	for _, art := range merged {
		pathSet[art.Path] = true
	}

	expectedPaths := []string{
		".claude-plugin/plugin.json",
		".mcp.json",
		"agents/reviewer.md",
		"commands/my-cmd.md",
		"hooks/hooks.json",
		"skills/my-skill/SKILL.md",
	}
	for _, path := range expectedPaths {
		if !pathSet[path] {
			t.Fatalf("missing artifact %q", path)
		}
	}
}
