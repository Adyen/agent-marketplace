package cursor

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var cursorPluginFS = fstest.MapFS{
	".cursor-plugin/plugin.json": &fstest.MapFile{
		Data: []byte(`{"name":"cursor-plugin","description":"Cursor plugin","version":"1.0.0"}`),
	},
	"skills/my-skill/SKILL.md": &fstest.MapFile{
		Data: []byte("---\nname: my-skill\ndescription: A test skill\ntriggers:\n  - click\n---\nDo the thing"),
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
		Data: []byte(`{"mcpServers":{"my-server":{"command":"npx","args":["-y","my-mcp"],"env":{"FOO":"bar"}}}}`),
	},
}

var cursorWorkspaceFS = fstest.MapFS{
	".cursor/skills/my-skill/SKILL.md": &fstest.MapFile{
		Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
	},
	".cursor/commands/my-cmd.md": &fstest.MapFile{
		Data: []byte("---\nname: my-cmd\ndescription: A test command\n---\necho hello"),
	},
	".cursor/agents/my-agent.md": &fstest.MapFile{
		Data: []byte("---\nname: my-agent\ndescription: A test agent\nmodel: claude-sonnet-4-5-20250929\ntools:\n  - Read\n  - Write\n---\nYou are an agent"),
	},
	".cursor/hooks/hooks.json": &fstest.MapFile{
		Data: []byte(`{"hooks":{"PreToolUse":[{"type":"command","command":"${CURSOR_PLUGIN_ROOT}/scripts/check.sh","matcher":"Read"}]}}`),
	},
	".cursor/mcp.json": &fstest.MapFile{
		Data: []byte(`{"mcpServers":{"my-server":{"command":"npx","args":["-y","my-mcp"]}}}`),
	},
}

func TestCursorIngester_Source(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessCursor)
	if !ok {
		t.Fatal("Cursor ingester not registered")
	}
	if got := ing.Source(); got != ingest.HarnessCursor {
		t.Fatalf("Source() = %q, want %q", got, ingest.HarnessCursor)
	}
}

func TestCursorIngester_Detect_PluginForm(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessCursor)
	if !ok {
		t.Fatal("Cursor ingester not registered")
	}
	if !ing.Detect(cursorPluginFS) {
		t.Fatal("Detect() = false for plugin form, want true")
	}
}

func TestCursorIngester_Detect_WorkspaceForm(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessCursor)
	if !ok {
		t.Fatal("Cursor ingester not registered")
	}
	if !ing.Detect(cursorWorkspaceFS) {
		t.Fatal("Detect() = false for workspace form, want true")
	}
}

func TestCursorIngester_LoadBundle_RulesOnlyPlugin(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessCursor)
	if !ok {
		t.Fatal("Cursor ingester not registered")
	}
	src := fstest.MapFS{
		".cursor-plugin/plugin.json": {Data: []byte(`{"name":"rules-only"}`)},
		"rules/secure-coding.mdc":    {Data: []byte("---\ndescription: Secure coding\nalwaysApply: true\n---\n# Secure\n")},
	}
	if !ing.Detect(src) {
		t.Fatal("Detect() = false for rules-only plugin, want true")
	}
	bundle, err := ing.LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if rule := findRule(bundle.Resources, "secure-coding"); rule == nil {
		t.Fatalf("expected rules-only plugin resource, got %v", bundle.Resources)
	} else if rule.Source.File != "rules/secure-coding.mdc" {
		t.Fatalf("rule source = %q, want rules/secure-coding.mdc", rule.Source.File)
	}
}

func TestCursorIngester_LoadBundle_PluginForm(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessCursor)
	if !ok {
		t.Fatal("Cursor ingester not registered")
	}

	b, err := ing.LoadBundle(cursorPluginFS)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	if b.Manifest.Name != "cursor-plugin" {
		t.Fatalf("Manifest.Name = %q, want %q", b.Manifest.Name, "cursor-plugin")
	}
	if len(b.Resources) != 5 {
		t.Fatalf("LoadBundle resources = %d, want 5", len(b.Resources))
	}

	kinds := make(map[ir.ResourceKind]int)
	for _, r := range b.Resources {
		kinds[r.Kind()]++
	}
	if kinds[ir.KindSkill] != 1 || kinds[ir.KindCommand] != 1 || kinds[ir.KindSubagent] != 1 || kinds[ir.KindHook] != 1 || kinds[ir.KindMcpServer] != 1 {
		t.Fatalf("unexpected kind counts: %#v", kinds)
	}
}

func TestCursorIngester_LoadBundle_WorkspaceForm(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessCursor)
	if !ok {
		t.Fatal("Cursor ingester not registered")
	}

	b, err := ing.LoadBundle(cursorWorkspaceFS)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	if len(b.Resources) != 5 {
		t.Fatalf("LoadBundle resources = %d, want 5", len(b.Resources))
	}

	hasSynthDiag := false
	for _, d := range b.Diagnostics {
		if d.Code == diag.CodeIngestCursorManifestSynthesized {
			hasSynthDiag = true
		}
	}
	if !hasSynthDiag {
		t.Fatal("expected manifest synthesized diagnostic for workspace form")
	}
}
