package cursor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// --- Skill matrix cell ---

func TestTranslate_Skill(t *testing.T) {
	c := newCursorCompiler()
	skill := ir.NewSkill(
		"my-skill",
		"A test skill",
		"Do the thing",
		[]string{"click", "mention"},
		map[string]any{"custom": true},
		ir.SourceRef{File: "skills/my-skill/SKILL.md", Line: 1},
	)

	arts, diags, err := c.Translate(skill)
	if err != nil {
		t.Fatalf("Translate(skill) error: %v", err)
	}

	if len(arts) != 1 {
		t.Fatalf("Translate(skill) artifacts = %d, want 1", len(arts))
	}

	wantPath := "skills/my-skill/SKILL.md"
	if arts[0].Path != wantPath {
		t.Errorf("artifact path = %q, want %q", arts[0].Path, wantPath)
	}
	if arts[0].Fragment {
		t.Errorf("skill artifact should not be a fragment")
	}
	if !strings.Contains(string(arts[0].Bytes), "Do the thing") {
		t.Errorf("skill body not found in artifact bytes")
	}
	if !strings.Contains(string(arts[0].Bytes), "A test skill") {
		t.Errorf("skill description not found in artifact bytes")
	}
	if len(diags) != 0 {
		t.Errorf("Translate(skill) diagnostics = %d, want 0", len(diags))
	}
}

func TestTranslate_Skill_PreservesExplicitEmptyTriggers(t *testing.T) {
	skill := ir.NewSkill("empty-triggers", "Skill", "body\n", []string{}, nil, ir.SourceRef{})
	arts, _, err := newCursorCompiler().Translate(skill)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(arts))
	}
	if !strings.Contains(string(arts[0].Bytes), "triggers: []") {
		t.Fatalf("skill frontmatter = %q, want explicit empty triggers", arts[0].Bytes)
	}
}

// --- Command matrix cell ---

func TestTranslate_Command(t *testing.T) {
	c := newCursorCompiler()
	cmd := ir.NewCommand(
		"my-cmd",
		"A test command",
		"echo hello",
		map[string]any{},
		ir.SourceRef{File: "commands/my-cmd.md", Line: 1},
	)

	arts, diags, err := c.Translate(cmd)
	if err != nil {
		t.Fatalf("Translate(command) error: %v", err)
	}

	if len(arts) != 1 {
		t.Fatalf("Translate(command) artifacts = %d, want 1", len(arts))
	}

	wantPath := "commands/my-cmd.md"
	if arts[0].Path != wantPath {
		t.Errorf("artifact path = %q, want %q", arts[0].Path, wantPath)
	}
	if arts[0].Fragment {
		t.Errorf("command artifact should not be a fragment")
	}
	if !strings.Contains(string(arts[0].Bytes), "echo hello") {
		t.Errorf("command body not found in artifact bytes")
	}
	if !strings.Contains(string(arts[0].Bytes), "A test command") {
		t.Errorf("command description not found in artifact bytes")
	}
	if len(diags) != 0 {
		t.Errorf("Translate(command) diagnostics = %d, want 0", len(diags))
	}
}

// --- Subagent matrix cell ---

func TestTranslate_Subagent(t *testing.T) {
	c := newCursorCompiler()
	sub := ir.NewSubagent(
		"my-agent",
		"A test subagent",
		"You are a helpful assistant",
		"sonnet",
		[]string{"Read", "Write", "Bash"},
		"high",
		nil, // no disallowedTools for Cursor (not relevant)
		"",  // no isolation
		map[string]any{},
		ir.SourceRef{File: "agents/my-agent.md", Line: 1},
	)

	arts, diags, err := c.Translate(sub)
	if err != nil {
		t.Fatalf("Translate(subagent) error: %v", err)
	}

	if len(arts) != 1 {
		t.Fatalf("Translate(subagent) artifacts = %d, want 1", len(arts))
	}

	wantPath := "agents/my-agent.md"
	if arts[0].Path != wantPath {
		t.Errorf("artifact path = %q, want %q", arts[0].Path, wantPath)
	}
	if arts[0].Fragment {
		t.Errorf("subagent artifact should not be a fragment")
	}

	body := string(arts[0].Bytes)

	// Verify frontmatter present
	if !strings.HasPrefix(body, "---\n") {
		t.Fatalf("subagent artifact should start with YAML frontmatter, got:\n%s", body[:min(80, len(body))])
	}

	// Verify model mapping: "sonnet" → "claude-sonnet-4-5-20250929" for Cursor
	if !strings.Contains(body, "claude-sonnet-4-5-20250929") {
		t.Errorf("subagent artifact should contain mapped model ID 'claude-sonnet-4-5-20250929', got:\n%s", body)
	}

	// Verify tool mapping: "Write" stays "Write" for Cursor (not split like Factory)
	if !strings.Contains(body, "- Read") {
		t.Errorf("subagent artifact should contain mapped tool 'Read'")
	}
	if !strings.Contains(body, "- Write") {
		t.Errorf("subagent artifact should contain mapped tool 'Write'")
	}
	if !strings.Contains(body, "- Bash") {
		t.Errorf("subagent artifact should contain mapped tool 'Bash'")
	}

	// Verify body content present
	if !strings.Contains(body, "You are a helpful assistant") {
		t.Errorf("subagent body not found in artifact")
	}

	// Family pinning and unrepresentable reasoning are distinct losses.
	if len(diags) != 2 || !diag.HasCode(diags, diag.CodeModelSelectorPinned) || !diag.HasCode(diags, diag.CodeModelReasoningOmitted) {
		t.Errorf("Translate(subagent) expected pin and effort diagnostics: %+v", diags)
	}
	if strings.Contains(body, "\neffort:") {
		t.Fatal("Cursor emitted unsupported effort field")
	}
}

func TestTranslate_Subagent_AppliesMappingOverrides(t *testing.T) {
	sub := ir.NewSubagent("override-agent", "Agent", "Work", "sonnet", []string{"Write"}, "", nil, "", nil, ir.SourceRef{})
	result, err := compile.Compile(sub, compile.TargetCursor,
		compile.WithModelOverride(map[string]string{"sonnet": "custom-model"}),
		compile.WithToolOverride(map[string]string{"Write": "CustomWrite"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 || len(result.Artifacts) != 1 {
		t.Fatalf("result = %#v, want one artifact without diagnostics", result)
	}
	body := string(result.Artifacts[0].Bytes)
	if !strings.Contains(body, "model: custom-model") || !strings.Contains(body, "- CustomWrite") {
		t.Fatalf("mapping overrides not applied:\n%s", body)
	}
}

func TestTranslate_Subagent_UnknownModel(t *testing.T) {
	c := newCursorCompiler()
	sub := ir.NewSubagent(
		"weird-agent",
		"Agent with unknown model",
		"Do stuff",
		"future-model-x",
		[]string{"Read"},
		"",
		nil,
		"",
		map[string]any{},
		ir.SourceRef{File: "agents/weird-agent.md", Line: 1},
	)

	arts, diags, err := c.Translate(sub)
	if err != nil {
		t.Fatalf("Translate(subagent) error: %v", err)
	}

	// Unknown models are omitted without suppressing the resource.
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
	body := string(arts[0].Bytes)
	if strings.Contains(body, "future-model-x") || strings.Contains(body, "\nmodel:") {
		t.Errorf("unknown model should be omitted, got:\n%s", body)
	}

	// The field omission is reported as a warning.
	found := false
	for _, d := range diags {
		if d.Severity == diag.SeverityWarning && d.Code == diag.CodeModelUnresolved {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unresolved-model warning, got: %+v", diags)
	}
}

func TestTranslate_Subagent_UnknownTool(t *testing.T) {
	c := newCursorCompiler()
	sub := ir.NewSubagent(
		"odd-agent",
		"Agent with unknown tool",
		"Work",
		"sonnet",
		[]string{"Read", "MysteryTool"},
		"",
		nil,
		"",
		map[string]any{},
		ir.SourceRef{File: "agents/odd-agent.md", Line: 1},
	)

	arts, diags, err := c.Translate(sub)
	if err != nil {
		t.Fatalf("Translate(subagent) error: %v", err)
	}

	// Unknown explicit tools must fail closed rather than widening permissions.
	if len(arts) != 0 {
		t.Fatalf("expected no artifact, got %d", len(arts))
	}

	found := false
	for _, d := range diags {
		if d.Severity == diag.SeverityError && d.Code == diag.CodeSubagentCursorToolUnknown {
			found = true
		}
	}
	if !found {
		t.Errorf("expected tool-unknown error diagnostic, got: %+v", diags)
	}
}

// --- Hook matrix cell ---

// spec: hook-resources/cursor-and-gemini-receive-harness-variables
func TestTranslate_Hook(t *testing.T) {
	c := newCursorCompiler()
	hook := ir.NewHook(
		"pre-commit",
		"pre-commit",
		"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh",
		"command",
		map[string]any{},
		ir.SourceRef{File: "hooks/hooks.json", Line: 5},
	)

	arts, diags, err := c.Translate(hook)
	if err != nil {
		t.Fatalf("Translate(hook) error: %v", err)
	}

	if len(arts) != 1 {
		t.Fatalf("Translate(hook) artifacts = %d, want 1", len(arts))
	}

	wantPath := "hooks/hooks.json"
	if arts[0].Path != wantPath {
		t.Errorf("artifact path = %q, want %q", arts[0].Path, wantPath)
	}
	if !arts[0].Fragment {
		t.Errorf("hook artifact should be a fragment")
	}

	// Verify JSON is valid
	var parsed map[string]any
	if err := json.Unmarshal(arts[0].Bytes, &parsed); err != nil {
		t.Fatalf("hook artifact is not valid JSON: %v\n%s", err, arts[0].Bytes)
	}

	// Verify env token swap: ${CLAUDE_PLUGIN_ROOT} → ${CURSOR_PLUGIN_ROOT}
	body := string(arts[0].Bytes)
	if !strings.Contains(body, "${CURSOR_PLUGIN_ROOT}") {
		t.Errorf("hook artifact should contain ${CURSOR_PLUGIN_ROOT}, got:\n%s", body)
	}
	if strings.Contains(body, "${CLAUDE_PLUGIN_ROOT}") {
		t.Errorf("hook artifact should NOT contain ${CLAUDE_PLUGIN_ROOT}, got:\n%s", body)
	}

	if len(diags) != 0 {
		t.Errorf("Translate(hook) diagnostics = %d, want 0", len(diags))
	}
}

func TestTranslate_Hook_NonCommandType(t *testing.T) {
	c := newCursorCompiler()
	hook := ir.NewHook(
		"http-hook",
		"post-commit",
		"",
		"http",
		map[string]any{"url": "https://example.com"},
		ir.SourceRef{File: "hooks/hooks.json", Line: 10},
	)

	arts, diags, err := c.Translate(hook)
	if err != nil {
		t.Fatalf("Translate(hook) error: %v", err)
	}

	// Non-command hooks should be dropped (no artifact)
	if len(arts) != 0 {
		t.Errorf("Translate(non-command hook) artifacts = %d, want 0", len(arts))
	}

	// Should emit a diagnostic about unsupported type
	found := false
	for _, d := range diags {
		if d.Severity == diag.SeverityWarning && d.Code == diag.CodeHookCursorUnsupportedType {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unsupported-type warning diagnostic for non-command hook, got: %+v", diags)
	}
}

// --- McpServer matrix cell ---

func TestTranslate_McpServer(t *testing.T) {
	c := newCursorCompiler()
	mcp := ir.NewMcpServer(
		"my-mcp",
		"npx",
		[]string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
		map[string]string{"FOO": "bar"},
		map[string]any{},
		ir.SourceRef{File: ".mcp.json", Line: 1},
	)

	arts, diags, err := c.Translate(mcp)
	if err != nil {
		t.Fatalf("Translate(mcpserver) error: %v", err)
	}

	if len(arts) != 1 {
		t.Fatalf("Translate(mcpserver) artifacts = %d, want 1", len(arts))
	}

	wantPath := "mcp.json"
	if arts[0].Path != wantPath {
		t.Errorf("artifact path = %q, want %q", arts[0].Path, wantPath)
	}
	if !arts[0].Fragment {
		t.Errorf("mcpserver artifact should be a fragment")
	}

	// Verify JSON is valid
	var parsed map[string]any
	if err := json.Unmarshal(arts[0].Bytes, &parsed); err != nil {
		t.Fatalf("mcpserver artifact is not valid JSON: %v\n%s", err, arts[0].Bytes)
	}

	// Verify mcpServers key with server name
	servers, ok := parsed["mcpServers"]
	if !ok {
		t.Fatal("mcp.json fragment should have 'mcpServers' key")
	}
	serverMap, ok := servers.(map[string]any)
	if !ok {
		t.Fatal("mcpServers value should be a map")
	}
	entry, ok := serverMap["my-mcp"]
	if !ok {
		t.Fatal("mcpServers should contain 'my-mcp' key")
	}
	entryMap, ok := entry.(map[string]any)
	if !ok {
		t.Fatal("server entry should be a map")
	}
	if entryMap["command"] != "npx" {
		t.Errorf("mcp server command = %v, want 'npx'", entryMap["command"])
	}

	if len(diags) != 0 {
		t.Errorf("Translate(mcpserver) diagnostics = %d, want 0", len(diags))
	}
}

// --- Registration test ---

func TestCursorCompiler_Registered(t *testing.T) {
	// Verify the cursor compiler is registered in the global compile registry.
	c, ok := compile.Lookup(mapping.TargetCursor)
	if !ok {
		t.Fatal("cursor compiler not registered in compile registry")
	}
	if c.Target() != mapping.TargetCursor {
		t.Errorf("Target() = %q, want %q", c.Target(), mapping.TargetCursor)
	}
}

// --- Target test ---

func TestCursorCompiler_Target(t *testing.T) {
	c := newCursorCompiler()
	if c.Target() != mapping.TargetCursor {
		t.Errorf("Target() = %q, want %q", c.Target(), mapping.TargetCursor)
	}
}

// --- Unknown resource kind test ---

func TestTranslate_UnknownKind(t *testing.T) {
	c := newCursorCompiler()
	// Use a mock resource with an unknown kind
	r := &testResource{kind: ir.ResourceKind(99), name: "unknown"}
	_, _, err := c.Translate(r)
	if err == nil {
		t.Error("Translate(unknown kind) should return an error")
	}
}

// --- Helpers ---

type testResource struct {
	kind ir.ResourceKind
	name string
}

func (t *testResource) Kind() ir.ResourceKind { return t.kind }
func (t *testResource) Name() string          { return t.name }
