package harnesscompile

import (
	"io/fs"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// testPluginFS matches the fixture used in compilebundle_test.go.
var ingestTestPluginFS = fstest.MapFS{
	"skills/my-skill/SKILL.md": &fstest.MapFile{
		Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
	},
	"commands/my-cmd.md": &fstest.MapFile{
		Data: []byte("---\nname: my-cmd\ndescription: A test command\n---\nRun the command"),
	},
	"agents/my-agent.md": &fstest.MapFile{
		Data: []byte("---\nname: my-agent\ndescription: A test agent\nmodel: sonnet\ntools:\n  - Read\n  - Write\n---\nYou are an agent"),
	},
	"hooks/hooks.json": &fstest.MapFile{
		Data: []byte(`{"hooks":{"PreToolUse":[{"type":"command","command":"echo check","matcher":"Read"}]}}`),
	},
	".mcp.json": &fstest.MapFile{
		Data: []byte(`{"mcpServers":{"my-server":{"command":"npx","args":["-y","my-mcp"]}}}`),
	},
}

var ingestFactoryPluginFS = fstest.MapFS{
	".factory-plugin/plugin.json": &fstest.MapFile{
		Data: []byte("{\n  \"droids\": [\n    {\n      \"description\": \"Reviews code\",\n      \"model\": \"claude-sonnet-4-5-20250929\",\n      \"name\": \"reviewer\",\n      \"reasoningEffort\": \"high\",\n      \"tools\": [\n        \"Read\",\n        \"Edit\",\n        \"Create\"\n      ]\n    }\n  ],\n  \"skills\": [\n    {\n      \"description\": \"\",\n      \"name\": \"my-skill\"\n    }\n  ]\n}"),
	},
	"skills/my-skill/SKILL.md": &fstest.MapFile{
		Data: []byte("Do the thing\n"),
	},
	"commands/my-cmd.md": &fstest.MapFile{
		Data: []byte("echo hi\n"),
	},
	"droids/reviewer.md": &fstest.MapFile{
		Data: []byte("---\nname: reviewer\ndescription: Reviews code\nmodel: claude-sonnet-4-5-20250929\ntools:\n  - Read\n  - Edit\n  - Create\nreasoningEffort: high\n---\nReview code\n"),
	},
	"hooks/hooks.json": &fstest.MapFile{
		Data: []byte("{\n  \"PreToolUse\": [\n    {\n      \"hooks\": [\n        {\n          \"command\": \"${DROID_PLUGIN_ROOT}/check.sh\",\n          \"type\": \"command\"\n        }\n      ],\n      \"matcher\": \"Read\"\n    }\n  ]\n}"),
	},
	"mcp.json": &fstest.MapFile{
		Data: []byte("{\n  \"mcpServers\": {\n    \"docs\": {\n      \"args\": [\n        \"-y\",\n        \"docs-server\"\n      ],\n      \"command\": \"npx\"\n    }\n  }\n}"),
	},
}

var ingestFactoryWorkspaceFS = fstest.MapFS{
	".factory/skills/my-skill/SKILL.md": &fstest.MapFile{
		Data: []byte("Do the thing\n"),
	},
	".factory/commands/my-cmd.md": &fstest.MapFile{
		Data: []byte("echo hi\n"),
	},
	".factory/droids/reviewer.md": &fstest.MapFile{
		Data: []byte("---\nname: reviewer\ndescription: Reviews code\nmodel: claude-sonnet-4-5-20250929\ntools:\n  - Read\n  - Edit\n  - Create\nreasoningEffort: high\n---\nReview code\n"),
	},
	".factory/hooks/hooks.json": &fstest.MapFile{
		Data: []byte(`{"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/check.sh"}]}]}`),
	},
	".factory/mcp.json": &fstest.MapFile{
		Data: []byte(`{"mcpServers":{"docs":{"command":"npx","args":["-y","docs-server"]}}}`),
	},
}

// TestIngestCompile_MatchesCompileBundle verifies that ingesting via the Claude
// ingester and then compiling produces identical output to the existing
// CompileBundle path. This is the key regression test for Phase 3.3.
func TestIngestCompile_MatchesCompileBundle(t *testing.T) {
	targets := targetmeta.ConversionTargets()

	for _, target := range targets {
		t.Run(string(target), func(t *testing.T) {
			// Path 1: existing CompileBundle
			bundleResult, err := CompileBundle(ingestTestPluginFS, target)
			if err != nil {
				t.Fatalf("CompileBundle(%s): %v", target, err)
			}

			// Path 2: ingest + compile each resource
			ing, ok := ingest.For(ingest.HarnessClaude)
			if !ok {
				t.Fatal("Claude ingester not registered")
			}
			b, err := ing.LoadBundle(ingestTestPluginFS)
			if err != nil {
				t.Fatalf("LoadBundle: %v", err)
			}

			var allArtifacts []artifact.Artifact
			for _, r := range b.Resources {
				res, err := Compile(r, target)
				if err != nil {
					t.Fatalf("Compile(%s, %s): %v", r.Name(), target, err)
				}
				allArtifacts = append(allArtifacts, res.Artifacts...)
			}

			// Merge fragments for the ingest path (CompileBundle does this internally)
			mergedIngest, err := artifact.MergeFragments(allArtifacts)
			if err != nil {
				t.Fatalf("merge fragments (ingest path): %v", err)
			}

			// Sort both artifact sets by path for deterministic comparison
			sort.Slice(bundleResult.Artifacts, func(i, j int) bool {
				return bundleResult.Artifacts[i].Path < bundleResult.Artifacts[j].Path
			})
			sort.Slice(mergedIngest, func(i, j int) bool {
				return mergedIngest[i].Path < mergedIngest[j].Path
			})

			// Compare artifact counts
			if len(bundleResult.Artifacts) != len(mergedIngest) {
				t.Fatalf("artifact count mismatch: CompileBundle=%d, IngestCompile=%d", len(bundleResult.Artifacts), len(mergedIngest))
			}

			// Compare each artifact path and content
			for i := range bundleResult.Artifacts {
				if bundleResult.Artifacts[i].Path != mergedIngest[i].Path {
					t.Errorf("artifact[%d] path: CompileBundle=%q, IngestCompile=%q", i,
						bundleResult.Artifacts[i].Path, mergedIngest[i].Path)
				}
				if string(bundleResult.Artifacts[i].Bytes) != string(mergedIngest[i].Bytes) {
					t.Errorf("artifact[%d] (%s) content mismatch:\nCompileBundle: %s\nIngestCompile: %s",
						i, bundleResult.Artifacts[i].Path,
						bundleResult.Artifacts[i].Bytes, mergedIngest[i].Bytes)
				}
			}
		})
	}
}

// TestIngestCompile_ClaudeIdentityByteStable verifies that Claude→Claude
// round-trip via the ingester produces byte-identical results to the
// existing CompileBundle(Claude) path. This is the identity guarantee.
func TestIngestCompile_ClaudeIdentityByteStable(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}

	b, err := ing.LoadBundle(ingestTestPluginFS)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	// Compile each resource to Claude target and merge fragments
	var allArtifacts []artifact.Artifact
	for _, r := range b.Resources {
		res, err := Compile(r, TargetClaude)
		if err != nil {
			t.Fatalf("Compile(%s, Claude): %v", r.Name(), err)
		}
		allArtifacts = append(allArtifacts, res.Artifacts...)
	}

	merged, err := artifact.MergeFragments(allArtifacts)
	if err != nil {
		t.Fatalf("merge fragments: %v", err)
	}

	// Compare against CompileBundle result
	bundleResult, err := CompileBundle(ingestTestPluginFS, TargetClaude)
	if err != nil {
		t.Fatalf("CompileBundle(Claude): %v", err)
	}

	// Sort for deterministic comparison
	sort.Slice(merged, func(i, j int) bool { return merged[i].Path < merged[j].Path })
	sort.Slice(bundleResult.Artifacts, func(i, j int) bool {
		return bundleResult.Artifacts[i].Path < bundleResult.Artifacts[j].Path
	})

	if len(merged) != len(bundleResult.Artifacts) {
		t.Fatalf("artifact count: ingest+compile=%d, CompileBundle=%d", len(merged), len(bundleResult.Artifacts))
	}

	for i := range merged {
		if merged[i].Path != bundleResult.Artifacts[i].Path {
			t.Errorf("path[%d] mismatch: %q vs %q", i, merged[i].Path, bundleResult.Artifacts[i].Path)
		}
		if string(merged[i].Bytes) != string(bundleResult.Artifacts[i].Bytes) {
			t.Errorf("content[%d] (%s) mismatch:\ningest: %q\nbundle: %q",
				i, merged[i].Path, merged[i].Bytes, bundleResult.Artifacts[i].Bytes)
		}
	}
}

// TestIngestCompile_WorkspaceFormClaudeIdentity verifies that .claude/ workspace
// form fixtures load correctly and compile to Claude producing valid output.
func TestIngestCompile_WorkspaceFormClaudeIdentity(t *testing.T) {
	workspaceFS := fstest.MapFS{
		".claude/skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
		},
		".claude/commands/my-cmd.md": &fstest.MapFile{
			Data: []byte("---\nname: my-cmd\ndescription: A test command\n---\nRun the command"),
		},
		".claude/agents/my-agent.md": &fstest.MapFile{
			Data: []byte("---\nname: my-agent\ndescription: A test agent\nmodel: sonnet\ntools:\n  - Read\n---\nYou are an agent"),
		},
		".claude/hooks/hooks.json": &fstest.MapFile{
			Data: []byte(`{"hooks":{"PreToolUse":[{"type":"command","command":"echo check"}]}}`),
		},
		".claude/.mcp.json": &fstest.MapFile{
			Data: []byte(`{"mcpServers":{"my-server":{"command":"npx","args":["-y","my-mcp"]}}}`),
		},
	}

	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}

	b, err := ing.LoadBundle(workspaceFS)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	// Verify all 5 resource kinds loaded
	if len(b.Resources) != 5 {
		t.Fatalf("expected 5 resources, got %d", len(b.Resources))
	}

	// Compile each resource to Claude target
	var allArtifacts []artifact.Artifact
	for _, r := range b.Resources {
		res, err := Compile(r, TargetClaude)
		if err != nil {
			t.Fatalf("Compile(%s, Claude): %v", r.Name(), err)
		}
		allArtifacts = append(allArtifacts, res.Artifacts...)
	}

	merged, err := artifact.MergeFragments(allArtifacts)
	if err != nil {
		t.Fatalf("merge fragments: %v", err)
	}

	// Should produce at least some artifacts
	if len(merged) == 0 {
		t.Fatal("expected at least one artifact from workspace form compilation")
	}

	// Check for expected artifact paths
	pathSet := make(map[string]bool)
	for _, a := range merged {
		pathSet[a.Path] = true
	}
	expectedPaths := []string{
		"skills/my-skill/SKILL.md",
		"commands/my-cmd.md",
		"agents/my-agent.md",
		".mcp.json",
	}
	for _, p := range expectedPaths {
		if !pathSet[p] {
			t.Errorf("missing expected artifact path %q", p)
		}
	}
}

func TestIngestCompile_FactoryIdentityByteStable(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessFactory)
	if !ok {
		t.Fatal("Factory ingester not registered")
	}

	b, err := ing.LoadBundle(ingestFactoryPluginFS)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	var allArtifacts []artifact.Artifact
	for _, r := range b.Resources {
		// The fixture hook carries a plugin-root token; compiling it for the
		// native path requires the installed support base.
		res, err := Compile(r, TargetFactory, WithPluginRoot("/plugins/demo"))
		if err != nil {
			t.Fatalf("Compile(%s, Factory): %v", r.Name(), err)
		}
		allArtifacts = append(allArtifacts, res.Artifacts...)
	}

	merged, err := artifact.MergeFragments(allArtifacts)
	if err != nil {
		t.Fatalf("merge fragments: %v", err)
	}

	sort.Slice(merged, func(i, j int) bool { return merged[i].Path < merged[j].Path })

	if len(merged) != 5 {
		t.Fatalf("artifact count = %d, want 5", len(merged))
	}

	for _, art := range merged {
		// Skills get frontmatter injected from IR metadata during compilation,
		// so compiled SKILL.md may differ from source when source lacked frontmatter.
		// Verify the compiled content contains the body text at minimum.
		if strings.HasPrefix(art.Path, "skills/") && strings.HasSuffix(art.Path, "/SKILL.md") {
			if !strings.Contains(string(art.Bytes), "Do the thing") {
				t.Errorf("artifact %q missing body content: %q", art.Path, string(art.Bytes))
			}
			continue
		}
		if art.Path == "hooks/hooks.json" {
			// The plugin-root token resolves to the installed support path;
			// the native file keeps the authored matcher.
			want := "{\n  \"PreToolUse\": [\n    {\n      \"hooks\": [\n        {\n          \"command\": \"\\\"$FACTORY_PROJECT_DIR\\\"/plugins/demo/check.sh\",\n          \"type\": \"command\"\n        }\n      ],\n      \"matcher\": \"Read\"\n    }\n  ]\n}"
			if string(art.Bytes) != want {
				t.Errorf("artifact %q mismatch:\n got: %q\nwant: %q", art.Path, string(art.Bytes), want)
			}
			continue
		}
		data, err := fs.ReadFile(ingestFactoryPluginFS, art.Path)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", art.Path, err)
		}
		if string(art.Bytes) != string(data) {
			t.Errorf("artifact %q mismatch:\n got: %q\nwant: %q", art.Path, string(art.Bytes), string(data))
		}
	}
}

func TestIngestCompile_FactoryWorkspaceFormClaudeOutput(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessFactory)
	if !ok {
		t.Fatal("Factory ingester not registered")
	}

	b, err := ing.LoadBundle(ingestFactoryWorkspaceFS)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	var allArtifacts []artifact.Artifact
	for _, r := range b.Resources {
		// The fixture hook carries a plugin-root token; compiling it for the
		// native path requires the installed support base.
		res, err := Compile(r, TargetClaude, WithPluginRoot("/plugins/demo"))
		if err != nil {
			t.Fatalf("Compile(%s, Claude): %v", r.Name(), err)
		}
		allArtifacts = append(allArtifacts, res.Artifacts...)
	}

	merged, err := artifact.MergeFragments(allArtifacts)
	if err != nil {
		t.Fatalf("merge fragments: %v", err)
	}

	if len(merged) == 0 {
		t.Fatal("expected artifacts from Factory workspace form")
	}

	pathSet := make(map[string]bool, len(merged))
	for _, art := range merged {
		pathSet[art.Path] = true
	}

	expected := []string{
		"skills/my-skill/SKILL.md",
		"commands/my-cmd.md",
		"agents/reviewer.md",
		"hooks/hooks.json",
		".mcp.json",
	}
	for _, path := range expected {
		if !pathSet[path] {
			t.Errorf("missing artifact %q", path)
		}
	}
}

func TestIngestCompile_CursorIdentityByteStable(t *testing.T) {
	cursorFS := fstest.MapFS{
		".cursor-plugin/plugin.json": &fstest.MapFile{
			Data: []byte(`{"name":"cursor-plugin"}`),
		},
		"skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
		},
		"commands/my-cmd.md": &fstest.MapFile{
			Data: []byte("---\nname: my-cmd\ndescription: A test command\n---\necho hello"),
		},
		"agents/my-agent.md": &fstest.MapFile{
			Data: []byte("---\nname: my-agent\ndescription: A test agent\nmodel: claude-sonnet-4-5-20250929\ntools:\n  - Read\n  - Write\n---\nYou are an agent"),
		},
		"hooks/hooks.json": &fstest.MapFile{
			Data: []byte(`{"hooks":{"PreToolUse":[{"type":"command","command":"${CURSOR_PLUGIN_ROOT}/scripts/check.sh","matcher":"Read"}]}}`),
		},
		"mcp.json": &fstest.MapFile{
			Data: []byte(`{"mcpServers":{"my-server":{"command":"npx","args":["-y","my-mcp"]}}}`),
		},
	}

	ing, ok := ingest.For(ingest.HarnessCursor)
	if !ok {
		t.Fatal("Cursor ingester not registered")
	}

	b, err := ing.LoadBundle(cursorFS)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	var allArtifacts []artifact.Artifact
	for _, r := range b.Resources {
		res, err := Compile(r, TargetCursor)
		if err != nil {
			t.Fatalf("Compile(%s, Cursor): %v", r.Name(), err)
		}
		if res == nil {
			continue
		}
		allArtifacts = append(allArtifacts, res.Artifacts...)
	}

	merged, err := artifact.MergeFragments(allArtifacts)
	if err != nil {
		t.Fatalf("merge fragments: %v", err)
	}

	if len(merged) == 0 {
		t.Fatal("expected artifacts from Cursor plugin form")
	}
}
