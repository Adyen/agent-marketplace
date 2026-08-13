package claude

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Minimal Claude-plugin fixture for ingester tests.
var testPluginFS = fstest.MapFS{
	// Skill
	"skills/my-skill/SKILL.md": &fstest.MapFile{
		Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
	},
	// Command
	"commands/my-cmd.md": &fstest.MapFile{
		Data: []byte("---\nname: my-cmd\ndescription: A test command\n---\nRun the command"),
	},
	// Subagent
	"agents/my-agent.md": &fstest.MapFile{
		Data: []byte("---\nname: my-agent\ndescription: A test agent\nmodel: sonnet\ntools:\n  - Read\n  - Write\n---\nYou are an agent"),
	},
	// Hook
	"hooks/hooks.json": &fstest.MapFile{
		Data: []byte(`{"hooks":{"PreToolUse":[{"type":"command","command":"echo check","matcher":"Read"}]}}`),
	},
	// MCP server
	".mcp.json": &fstest.MapFile{
		Data: []byte(`{"mcpServers":{"my-server":{"command":"npx","args":["-y","my-mcp"]}}}`),
	},
}

func TestClaudeIngester_Source(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}
	if got := ing.Source(); got != ingest.HarnessClaude {
		t.Fatalf("Source() = %q, want %q", got, ingest.HarnessClaude)
	}
}

func TestClaudeIngester_Detect_PluginForm(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}

	// Bare plugin form (root-level skills/, commands/, etc.)
	if !ing.Detect(testPluginFS) {
		t.Fatal("Detect() = false for plugin form, want true")
	}

	// Explicit .claude-plugin/plugin.json
	pluginFS := fstest.MapFS{
		".claude-plugin/plugin.json": &fstest.MapFile{
			Data: []byte(`{"name":"test-plugin"}`),
		},
	}
	if !ing.Detect(pluginFS) {
		t.Fatal("Detect() = false for .claude-plugin/ form, want true")
	}
}

func TestClaudeIngester_Detect_WorkspaceForm(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}

	// Workspace form: .claude/ with resources
	workspaceFS := fstest.MapFS{
		".claude/skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\n---\nBody"),
		},
	}
	if !ing.Detect(workspaceFS) {
		t.Fatal("Detect() = false for workspace form, want true")
	}
}

func TestClaudeIngester_Detect_NoMatch(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}

	emptyFS := fstest.MapFS{}
	if ing.Detect(emptyFS) {
		t.Fatal("Detect() = true for empty FS, want false")
	}

	// Unrelated directory
	unrelatedFS := fstest.MapFS{
		"random/file.txt": &fstest.MapFile{Data: []byte("hello")},
	}
	if ing.Detect(unrelatedFS) {
		t.Fatal("Detect() = true for unrelated FS, want false")
	}
}

func TestClaudeIngester_LoadBundle_PluginForm(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}

	b, err := ing.LoadBundle(testPluginFS)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	// Expect 5 resources: 1 skill + 1 command + 1 subagent + 1 hook + 1 mcp server
	if len(b.Resources) != 5 {
		t.Fatalf("LoadBundle: got %d resources, want 5", len(b.Resources))
	}

	// Check resource kinds
	kinds := make(map[ir.ResourceKind]int)
	for _, r := range b.Resources {
		kinds[r.Kind()]++
	}
	if kinds[ir.KindSkill] != 1 {
		t.Errorf("skills = %d, want 1", kinds[ir.KindSkill])
	}
	if kinds[ir.KindCommand] != 1 {
		t.Errorf("commands = %d, want 1", kinds[ir.KindCommand])
	}
	if kinds[ir.KindSubagent] != 1 {
		t.Errorf("subagents = %d, want 1", kinds[ir.KindSubagent])
	}
	if kinds[ir.KindHook] != 1 {
		t.Errorf("hooks = %d, want 1", kinds[ir.KindHook])
	}
	if kinds[ir.KindMcpServer] != 1 {
		t.Errorf("mcp servers = %d, want 1", kinds[ir.KindMcpServer])
	}
}

func TestClaudeIngester_LoadBundle_WorkspaceForm(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}

	workspaceFS := fstest.MapFS{
		".claude/skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
		},
		".claude/commands/my-cmd.md": &fstest.MapFile{
			Data: []byte("---\nname: my-cmd\ndescription: A test command\n---\nRun the command"),
		},
		".claude/agents/my-agent.md": &fstest.MapFile{
			Data: []byte("---\nname: my-agent\ndescription: A test agent\nmodel: sonnet\n---\nYou are an agent"),
		},
		".claude/hooks/hooks.json": &fstest.MapFile{
			Data: []byte(`{"hooks":{"PreToolUse":[{"type":"command","command":"echo check"}]}}`),
		},
		".claude/.mcp.json": &fstest.MapFile{
			Data: []byte(`{"mcpServers":{"my-server":{"command":"npx","args":["-y","my-mcp"]}}}`),
		},
	}

	b, err := ing.LoadBundle(workspaceFS)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	// Expect 5 resources
	if len(b.Resources) != 5 {
		t.Fatalf("LoadBundle: got %d resources, want 5", len(b.Resources))
	}

	// Check resource kinds
	kinds := make(map[ir.ResourceKind]int)
	for _, r := range b.Resources {
		kinds[r.Kind()]++
	}
	if kinds[ir.KindSkill] != 1 {
		t.Errorf("skills = %d, want 1", kinds[ir.KindSkill])
	}
	if kinds[ir.KindCommand] != 1 {
		t.Errorf("commands = %d, want 1", kinds[ir.KindCommand])
	}
	if kinds[ir.KindSubagent] != 1 {
		t.Errorf("subagents = %d, want 1", kinds[ir.KindSubagent])
	}
	if kinds[ir.KindHook] != 1 {
		t.Errorf("hooks = %d, want 1", kinds[ir.KindHook])
	}
	if kinds[ir.KindMcpServer] != 1 {
		t.Errorf("mcp servers = %d, want 1", kinds[ir.KindMcpServer])
	}

	// Workspace form should produce a synthesized manifest info diagnostic
	hasSynthDiag := false
	for _, d := range b.Diagnostics {
		if d.Code == diag.CodeIngestClaudeManifestSynthesized {
			hasSynthDiag = true
		}
	}
	if !hasSynthDiag {
		t.Error("expected bundle-manifest-synthesized diagnostic for workspace form")
	}
}

func TestClaudeIngester_LoadBundle_EmptyFS(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}

	emptyFS := fstest.MapFS{}
	_, err := ing.LoadBundle(emptyFS)
	if err == nil {
		t.Fatal("expected error for empty FS")
	}
}

func TestClaudeIngester_LoadBundle_ResourceNames(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}

	b, err := ing.LoadBundle(testPluginFS)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	names := make(map[string]bool)
	for _, r := range b.Resources {
		names[r.Name()] = true
	}

	expected := []string{"my-skill", "my-cmd", "my-agent"}
	for _, n := range expected {
		if !names[n] {
			t.Errorf("missing resource %q", n)
		}
	}
}

func TestClaudeIngester_LoadBundle_WithPluginManifest(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}

	pluginFS := fstest.MapFS{
		".claude-plugin/plugin.json": &fstest.MapFile{
			Data: []byte(`{"name":"test-plugin","description":"A test plugin","version":"1.0.0"}`),
		},
		"skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\n---\nBody"),
		},
	}

	b, err := ing.LoadBundle(pluginFS)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	if b.Manifest.Name != "test-plugin" {
		t.Errorf("Manifest.Name = %q, want %q", b.Manifest.Name, "test-plugin")
	}
}

func TestClaudeIngester_Detect_BareSkillsDir(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}

	// Multi-skill: subdirectories with SKILL.md at root level
	multiSkillFS := fstest.MapFS{
		"brainstorming/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: brainstorming\ndescription: Think together\n---\nBody"),
		},
		"caveman/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: caveman\ndescription: Simple approach\n---\nBody"),
		},
	}
	if !ing.Detect(multiSkillFS) {
		t.Fatal("Detect() = false for bare skills directory, want true")
	}

	// Single skill: SKILL.md at root
	singleSkillFS := fstest.MapFS{
		"SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\ndescription: A single skill\n---\nBody"),
		},
	}
	if !ing.Detect(singleSkillFS) {
		t.Fatal("Detect() = false for single-skill directory, want true")
	}

	// No skills: should not detect
	noSkillsFS := fstest.MapFS{
		"README.md": &fstest.MapFile{Data: []byte("# Hello")},
	}
	if ing.Detect(noSkillsFS) {
		t.Fatal("Detect() = true for directory with no skills, want false")
	}

	// DetectStrict: must return false for both bare-skills forms
	// (used by sub-bundle scanning to avoid treating individual skill
	// directories as independent bundle roots).
	strictDetector, ok := ing.(ingest.StrictDetector)
	if !ok {
		t.Fatal("Claude ingester does not implement StrictDetector")
	}
	if strictDetector.DetectStrict(multiSkillFS) {
		t.Fatal("DetectStrict() = true for bare multi-skill directory, want false")
	}
	if strictDetector.DetectStrict(singleSkillFS) {
		t.Fatal("DetectStrict() = true for single-skill directory, want false")
	}

	// DetectStrict must still return true for a real Claude bundle root
	// (e.g., bare plugin form with skills/ directory).
	realBundleFS := fstest.MapFS{
		"skills/review/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: review\ndescription: Code review\n---\nBody"),
		},
	}
	if !strictDetector.DetectStrict(realBundleFS) {
		t.Fatal("DetectStrict() = false for real Claude bundle root, want true")
	}

}

func TestClaudeIngester_LoadBundle_BareSkillsDir(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}

	// Multi-skill: subdirectories with SKILL.md at root level
	multiSkillFS := fstest.MapFS{
		"brainstorming/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: brainstorming\ndescription: Think together\n---\nBrainstorm body"),
		},
		"caveman/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: caveman\ndescription: Simple approach\n---\nCaveman body"),
		},
	}

	b, err := ing.LoadBundle(multiSkillFS)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	if len(b.Resources) != 2 {
		t.Fatalf("LoadBundle: got %d resources, want 2", len(b.Resources))
	}

	names := make(map[string]bool)
	for _, r := range b.Resources {
		if r.Kind() != ir.KindSkill {
			t.Errorf("resource %q has kind %v, want Skill", r.Name(), r.Kind())
		}
		names[r.Name()] = true
	}
	if !names["brainstorming"] {
		t.Error("missing skill 'brainstorming'")
	}
	if !names["caveman"] {
		t.Error("missing skill 'caveman'")
	}
}

func TestClaudeIngester_LoadBundle_SingleSkillDir(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}

	// Single skill: SKILL.md at root with explicit name
	singleSkillFS := fstest.MapFS{
		"SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: firewall-rules\ndescription: Manage firewall rules\n---\nFirewall body"),
		},
	}

	b, err := ing.LoadBundle(singleSkillFS)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	if len(b.Resources) != 1 {
		t.Fatalf("LoadBundle: got %d resources, want 1", len(b.Resources))
	}

	skill := b.Resources[0]
	if skill.Name() != "firewall-rules" {
		t.Errorf("skill name = %q, want %q", skill.Name(), "firewall-rules")
	}
	if skill.Kind() != ir.KindSkill {
		t.Errorf("resource kind = %v, want Skill", skill.Kind())
	}
}

func TestClaudeIngester_LoadBundle_SingleSkillDir_NoName(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester not registered")
	}

	// Single skill at root without name field: should fail gracefully
	noNameFS := fstest.MapFS{
		"SKILL.md": &fstest.MapFile{
			Data: []byte("---\ndescription: A skill without a name\n---\nBody"),
		},
	}

	b, err := ing.LoadBundle(noNameFS)
	if err != nil {
		t.Fatalf("LoadBundle returned unexpected error: %v", err)
	}
	// Should have an error diagnostic for the root skill
	if len(b.Diagnostics) == 0 {
		t.Fatal("expected error diagnostic for root SKILL.md without name field")
	}
}
