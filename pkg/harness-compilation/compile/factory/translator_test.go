package factory

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// --- Helper: find artifact by path ---
func findArtifactByPath(arts []artifact.Artifact, path string) *artifact.Artifact {
	for i := range arts {
		if arts[i].Path == path {
			return &arts[i]
		}
	}
	return nil
}

// hasDiagnostic returns true if diags contains a diagnostic with the given code.
func hasDiagnostic(diags []diag.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

// --- Factory Skill test: frontmatter rendered in SKILL.md ---
func TestFactorySkill_FrontmatterRendered(t *testing.T) {
	c := &factoryCompiler{}
	skill := ir.NewSkill("my-skill", "A test skill", "# My Skill Body\nHello world", []string{"click", "mention"}, map[string]any{"custom": 42}, ir.SourceRef{File: "skills/my-skill/SKILL.md", Line: 1})

	arts, diags, err := c.Translate(skill)
	if err != nil {
		t.Fatalf("Translate(skill) error: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(skill) diagnostics = %v, want 0", diags)
	}

	// Factory skill goes to skills/<name>/SKILL.md
	skillArt := findArtifactByPath(arts, "skills/my-skill/SKILL.md")
	if skillArt == nil {
		t.Fatal("missing artifact skills/my-skill/SKILL.md")
	}

	body := string(skillArt.Bytes)

	// Must contain YAML frontmatter delimiters
	if !strings.HasPrefix(body, "---\n") {
		t.Fatalf("skill artifact should start with frontmatter, got: %q", body[:min(len(body), 80)])
	}
	if !strings.Contains(body, "---\n# My Skill Body") {
		t.Fatalf("skill artifact should have closing frontmatter before body, got: %q", body)
	}

	// Must contain name and description in frontmatter
	if !strings.Contains(body, "name: my-skill") {
		t.Errorf("skill frontmatter missing 'name: my-skill', got: %q", body)
	}
	if !strings.Contains(body, "description: A test skill") {
		t.Errorf("skill frontmatter missing 'description: A test skill', got: %q", body)
	}

	// Must contain triggers in frontmatter
	if !strings.Contains(body, "triggers:") {
		t.Errorf("skill frontmatter missing triggers, got: %q", body)
	}
	if !strings.Contains(body, "  - click") || !strings.Contains(body, "  - mention") {
		t.Errorf("skill frontmatter missing trigger entries, got: %q", body)
	}

	// Must contain extra fields in frontmatter
	if !strings.Contains(body, "custom: 42") {
		t.Errorf("skill frontmatter missing extra field 'custom: 42', got: %q", body)
	}

	// Must contain the body after frontmatter
	if !strings.Contains(body, "# My Skill Body\nHello world") {
		t.Errorf("skill artifact missing body content, got: %q", body)
	}

	// Factory discovers skills from the filesystem, not plugin.json arrays.
	// No plugin.json fragment should be generated.
	if pluginArt := findArtifactByPath(arts, ".factory-plugin/plugin.json"); pluginArt != nil {
		t.Error("unexpected .factory-plugin/plugin.json artifact; Factory discovers skills from filesystem")
	}
}

func TestFactorySkill_PreservesExplicitEmptyTriggers(t *testing.T) {
	skill := ir.NewSkill("empty-triggers", "Skill", "body\n", []string{}, nil, ir.SourceRef{})
	arts, _, err := (&factoryCompiler{}).Translate(skill)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(arts[0].Bytes), "triggers: []") {
		t.Fatalf("skill frontmatter = %q, want explicit empty triggers", arts[0].Bytes)
	}
}

// --- Factory Command test (✓ same as Claude) ---
func TestFactoryCommand_RendersFrontmatter(t *testing.T) {
	c := &factoryCompiler{}
	cmd := ir.NewCommand("my-cmd", "A test command", "echo hello", map[string]any{}, ir.SourceRef{File: "commands/my-cmd.md", Line: 1})

	arts, diags, err := c.Translate(cmd)
	if err != nil {
		t.Fatalf("Translate(command) error: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(command) diagnostics = %v, want 0", diags)
	}

	cmdArt := findArtifactByPath(arts, "commands/my-cmd.md")
	if cmdArt == nil {
		t.Fatal("missing artifact commands/my-cmd.md")
	}
	want := "---\ndescription: A test command\n---\necho hello"
	if string(cmdArt.Bytes) != want {
		t.Errorf("command output = %q, want %q", string(cmdArt.Bytes), want)
	}
	if cmdArt.Fragment {
		t.Error("command artifact should not be a fragment")
	}
}

// A command with no description and no extra fields stays body-only.
func TestFactoryCommand_BodyOnlyWhenNoFrontmatter(t *testing.T) {
	c := &factoryCompiler{}
	cmd := ir.NewCommand("my-cmd", "", "echo hello", nil, ir.SourceRef{File: "commands/my-cmd.md", Line: 1})

	arts, _, err := c.Translate(cmd)
	if err != nil {
		t.Fatalf("Translate(command) error: %v", err)
	}
	cmdArt := findArtifactByPath(arts, "commands/my-cmd.md")
	if cmdArt == nil {
		t.Fatal("missing artifact commands/my-cmd.md")
	}
	if string(cmdArt.Bytes) != "echo hello" {
		t.Errorf("command body = %q, want %q", string(cmdArt.Bytes), "echo hello")
	}
}

// --- Factory Subagent security policy ---
func TestFactorySubagent_SecurityRestrictionsSuppressArtifact(t *testing.T) {
	sub := ir.NewSubagent("my-agent", "A test agent", "You are helpful", "sonnet", []string{"Read", "Write"}, "high", []string{"Bash"}, "worktree", map[string]any{}, ir.SourceRef{File: "agents/my-agent.md", Line: 1})
	arts, diags, err := (&factoryCompiler{}).Translate(sub)
	if err != nil {
		t.Fatalf("Translate(subagent) error: %v", err)
	}
	if !hasDiagnostic(diags, diag.CodeSubagentFactoryDisallowedToolsDropped) || !hasDiagnostic(diags, diag.CodeSubagentFactoryIsolationDropped) {
		t.Fatalf("missing security diagnostics: %#v", diags)
	}
	if len(arts) != 0 {
		t.Fatalf("artifacts = %#v, want none", arts)
	}
}

func TestFactorySubagent_AppliesMappingOverrides(t *testing.T) {
	sub := ir.NewSubagent("override-agent", "Agent", "Work", "sonnet", []string{"Write"}, "", nil, "", nil, ir.SourceRef{})
	result, err := compile.Compile(sub, compile.TargetFactory,
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

func TestFactorySubagent_FrontmatterExtraFieldsAreDeterministic(t *testing.T) {
	c := &factoryCompiler{}
	subagent := ir.NewSubagent("stable", "Stable", "body\n", "", nil, "", nil, "", map[string]any{
		"zebra":  "z",
		"alpha":  "a",
		"middle": "m",
	}, ir.SourceRef{})
	const want = "---\nname: stable\ndescription: Stable\nalpha: a\nmiddle: m\nzebra: z\n---\nbody\n"

	for range 100 {
		arts, _, err := c.Translate(subagent)
		if err != nil {
			t.Fatalf("Translate(subagent): %v", err)
		}
		got := string(findArtifactByPath(arts, "droids/stable.md").Bytes)
		if got != want {
			t.Fatalf("frontmatter = %q, want %q", got, want)
		}
	}
}

// --- Factory Subagent: unknown tools suppress the restricted artifact ---
func TestFactorySubagent_UnknownToolSuppressesArtifact(t *testing.T) {
	sub := ir.NewSubagent("unknown-tool", "Agent", "Work", "sonnet", []string{"MysteryTool"}, "", nil, "", nil, ir.SourceRef{})
	arts, diags, err := (&factoryCompiler{}).Translate(sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 0 {
		t.Fatalf("artifacts = %d, want none for an unrepresentable restriction", len(arts))
	}
	if len(diags) != 1 || diags[0].Code != diag.CodeSubagentFactoryToolUnknown || diags[0].Severity != diag.SeverityError {
		t.Fatalf("diagnostics = %#v, want factory unknown-tool error", diags)
	}

}

func TestFactorySubagent_MixedToolSuppressesArtifact(t *testing.T) {
	sub := ir.NewSubagent("mixed-tool", "Agent", "Work", "sonnet", []string{"Read", "MysteryTool"}, "", nil, "", nil, ir.SourceRef{})
	arts, diags, err := (&factoryCompiler{}).Translate(sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 0 {
		t.Fatalf("artifacts = %d, want none for an unrepresentable restriction", len(arts))
	}
	if len(diags) != 1 || diags[0].Code != diag.CodeSubagentFactoryToolUnknown || diags[0].Severity != diag.SeverityError {
		t.Fatalf("diagnostics = %#v, want factory unknown-tool error", diags)
	}

}

func TestFactorySubagent_NoLossyFields(t *testing.T) {
	c := &factoryCompiler{}
	sub := ir.NewSubagent("clean-agent", "A clean agent", "Be helpful", "sonnet", []string{"Read"}, "", nil, "", map[string]any{}, ir.SourceRef{File: "agents/clean-agent.md", Line: 1})

	_, diags, err := c.Translate(sub)
	if err != nil {
		t.Fatalf("Translate(subagent) error: %v", err)
	}

	if hasDiagnostic(diags, diag.CodeSubagentFactoryDisallowedToolsDropped) {
		t.Error("should not emit disallowed-tools-dropped when no disallowedTools")
	}
	if hasDiagnostic(diags, diag.CodeSubagentFactoryIsolationDropped) {
		t.Error("should not emit isolation-dropped when no isolation")
	}
}

// --- Factory Hook test (~ translated) ---
func TestFactoryHook_CommandType(t *testing.T) {
	c := &factoryCompiler{}
	hook := ir.NewHook("pre-tool-command", "PreToolUse", "${CLAUDE_PLUGIN_ROOT}/run.sh", "command", map[string]any{}, ir.SourceRef{File: "hooks/hooks.json", Line: 0})

	arts, diags, err := c.Translate(hook)
	if err != nil {
		t.Fatalf("Translate(hook) error: %v", err)
	}
	if hasDiagnostic(diags, diag.CodeHookFactoryNonCommandTypeDropped) {
		t.Error("command-type hook should not emit non-command-type-dropped diagnostic")
	}

	hookArt := findArtifactByPath(arts, "hooks/hooks.json")
	if hookArt == nil {
		t.Fatal("missing artifact hooks/hooks.json")
	}
	if !hookArt.Fragment {
		t.Error("hooks.json artifact should be a fragment")
	}

	// Verify ${CLAUDE_PLUGIN_ROOT} → ${DROID_PLUGIN_ROOT}
	// Factory hooks.json format: { "<event>": [{ "matcher": ..., "hooks": [{ "type": ..., "command": ... }] }] }
	var hookData map[string]any
	if err := json.Unmarshal(hookArt.Bytes, &hookData); err != nil {
		t.Fatalf("hooks.json is not valid JSON: %v", err)
	}
	rules := hookData["PreToolUse"].([]any)
	ruleGroup := rules[0].(map[string]any)
	hooksArr := ruleGroup["hooks"].([]any)
	hookEntry := hooksArr[0].(map[string]any)
	cmd := hookEntry["command"].(string)
	if !strings.Contains(cmd, "${DROID_PLUGIN_ROOT}") {
		t.Errorf("hook command = %q, want ${DROID_PLUGIN_ROOT}", cmd)
	}
	if strings.Contains(cmd, "${CLAUDE_PLUGIN_ROOT}") {
		t.Errorf("hook command should not contain ${CLAUDE_PLUGIN_ROOT}, got: %q", cmd)
	}
}

// --- Factory Hook: non-command type dropped ---
// spec: hook-resources/factory-drops-other-non-command-types-with-a-warning
func TestFactoryHook_NonCommandTypeDropped(t *testing.T) {
	c := &factoryCompiler{}
	hook := ir.NewHook("post-commit-http", "post-commit", "http://example.com", "http", map[string]any{}, ir.SourceRef{File: "hooks/hooks.json", Line: 0})

	arts, diags, err := c.Translate(hook)
	if err != nil {
		t.Fatalf("Translate(hook) error: %v", err)
	}

	if !hasDiagnostic(diags, diag.CodeHookFactoryNonCommandTypeDropped) {
		t.Error("missing diagnostic hook.factory.non-command-type-dropped for http hook type")
	}

	// Non-command hooks should produce no artifact
	if len(arts) != 0 {
		t.Errorf("non-command hook should produce 0 artifacts, got %d", len(arts))
	}
}

// --- Factory McpServer test (~ translated) ---
func TestFactoryMcpServer_Translated(t *testing.T) {
	c := &factoryCompiler{}
	mcp := ir.NewMcpServer("my-server", "npx", []string{"-y", "some-mcp"}, map[string]string{"KEY": "val"}, map[string]any{}, ir.SourceRef{File: ".mcp.json", Line: 0})

	arts, diags, err := c.Translate(mcp)
	if err != nil {
		t.Fatalf("Translate(mcpserver) error: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(mcpserver) diagnostics = %v, want 0", diags)
	}

	// Factory emits mcp.json (not .mcp.json)
	mcpArt := findArtifactByPath(arts, "mcp.json")
	if mcpArt == nil {
		t.Fatal("missing artifact mcp.json")
	}
	if !mcpArt.Fragment {
		t.Error("mcp.json artifact should be a fragment")
	}
	if mcpArt.FragmentKey != "mcp.json" {
		t.Errorf("mcp.json FragmentKey = %q, want %q", mcpArt.FragmentKey, "mcp.json")
	}

	// Verify JSON structure
	var mcpData map[string]any
	if err := json.Unmarshal(mcpArt.Bytes, &mcpData); err != nil {
		t.Fatalf("mcp.json is not valid JSON: %v", err)
	}
	serversMap, ok := mcpData["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.json missing 'mcpServers' key, got: %v", mcpData)
	}
	serverEntry, ok := serversMap["my-server"].(map[string]any)
	if !ok {
		t.Fatalf("my-server entry not a map, got: %v", serversMap["my-server"])
	}
	if serverEntry["command"] != "npx" {
		t.Errorf("mcp command = %v, want %q", serverEntry["command"], "npx")
	}
}

func TestFactoryFrontmatterQuotesExtraKeysAndScalars(t *testing.T) {
	skill := ir.NewSkill("safe", "description", "body\n", nil, map[string]any{
		"extra: key": "true",
	}, ir.SourceRef{})
	arts, _, err := (&factoryCompiler{}).Translate(skill)
	if err != nil {
		t.Fatal(err)
	}
	body := string(findArtifactByPath(arts, "skills/safe/SKILL.md").Bytes)
	if !strings.Contains(body, "'extra: key': 'true'\n") {
		t.Fatalf("frontmatter did not quote extra key/scalar: %q", body)
	}
}

func TestFactoryFrontmatterReturnsRenderErrors(t *testing.T) {
	skill := ir.NewSkill("unsupported", "description", "body\n", nil, map[string]any{"extra": make(chan int)}, ir.SourceRef{})
	if _, _, err := (&factoryCompiler{}).Translate(skill); err == nil {
		t.Fatal("Translate accepted an unsupported YAML value")
	}
}

// --- Factory Compiler registration test ---
func TestFactoryCompiler_Registered(t *testing.T) {
	c, ok := getCompiler()
	if !ok {
		t.Fatal("Factory compiler not registered")
	}
	if c.Target() != mapping.TargetFactory {
		t.Errorf("Target() = %q, want %q", c.Target(), mapping.TargetFactory)
	}
}

// --- YAML frontmatter: metadata (map[string]any) renders as valid YAML ---
func TestFormatYAMLKey_MetadataTags(t *testing.T) {
	c := &factoryCompiler{}
	skill := ir.NewSkill("tagged-skill", "A skill with tags", "# Body",
		nil,
		map[string]any{
			"metadata": map[string]any{
				"tags": []string{"cli", "setup"},
			},
		},
		ir.SourceRef{File: "skills/tagged-skill/SKILL.md", Line: 1},
	)

	arts, diags, err := c.Translate(skill)
	if err != nil {
		t.Fatalf("Translate(skill) error: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	skillArt := findArtifactByPath(arts, "skills/tagged-skill/SKILL.md")
	if skillArt == nil {
		t.Fatal("missing artifact skills/tagged-skill/SKILL.md")
	}

	body := string(skillArt.Bytes)

	// Metadata should render as nested YAML, not as Go map format.
	if strings.Contains(body, "map[") {
		t.Errorf("metadata should not contain Go map formatting, got: %q", body)
	}
	if !strings.Contains(body, "metadata:\n") {
		t.Errorf("metadata key should be present, got: %q", body)
	}
	if !strings.Contains(body, "  tags:\n") {
		t.Errorf("tags should be nested under metadata, got: %q", body)
	}
	if !strings.Contains(body, "    - cli") || !strings.Contains(body, "    - setup") {
		t.Errorf("tag values should be listed, got: %q", body)
	}
	if !strings.HasPrefix(body, "---\n") {
		t.Fatalf("should start with frontmatter delimiter, got: %q", body)
	}
}

func TestFormatYAMLKey_EmptyMetadata(t *testing.T) {
	c := &factoryCompiler{}
	skill := ir.NewSkill("empty-skill", "No tags", "# Body",
		nil,
		map[string]any{
			"metadata": map[string]any{},
		},
		ir.SourceRef{File: "skills/empty-skill/SKILL.md", Line: 1},
	)

	arts, _, err := c.Translate(skill)
	if err != nil {
		t.Fatalf("Translate(skill) error: %v", err)
	}

	skillArt := findArtifactByPath(arts, "skills/empty-skill/SKILL.md")
	if skillArt == nil {
		t.Fatal("missing artifact")
	}

	body := string(skillArt.Bytes)
	if !strings.Contains(body, "metadata: {}") {
		t.Errorf("empty metadata should render as {}, got: %q", body)
	}
	if strings.Contains(body, "map[") {
		t.Errorf("empty metadata should not use Go map format, got: %q", body)
	}
}

func TestFormatYAMLKey_BoolIntFloat(t *testing.T) {
	c := &factoryCompiler{}
	skill := ir.NewSkill("typed-skill", "Scalars", "# Body",
		nil,
		map[string]any{
			"enabled":  true,
			"priority": int(5),
			"weight":   float64(3.14),
			"nilval":   nil,
		},
		ir.SourceRef{File: "skills/typed-skill/SKILL.md", Line: 1},
	)

	arts, _, err := c.Translate(skill)
	if err != nil {
		t.Fatalf("Translate(skill) error: %v", err)
	}

	skillArt := findArtifactByPath(arts, "skills/typed-skill/SKILL.md")
	if skillArt == nil {
		t.Fatal("missing artifact")
	}

	body := string(skillArt.Bytes)
	if !strings.Contains(body, "enabled: true") {
		t.Errorf("bool should render as 'true', got: %q", body)
	}
	if !strings.Contains(body, "priority: 5") {
		t.Errorf("int should render as '5', got: %q", body)
	}
	if !strings.Contains(body, "weight: 3.14") {
		t.Errorf("float64 should render as '3.14', got: %q", body)
	}
	if !strings.Contains(body, "nilval: null") {
		t.Errorf("nil should render as 'null', got: %q", body)
	}
}

func TestFormatYAMLKey_ListOfMaps(t *testing.T) {
	c := &factoryCompiler{}
	skill := ir.NewSkill("list-skill", "Lists", "# Body",
		nil,
		map[string]any{
			"entries": []any{
				map[string]any{"key": "a", "val": int(1)},
				map[string]any{"key": "b", "val": int(2)},
			},
		},
		ir.SourceRef{File: "skills/list-skill/SKILL.md", Line: 1},
	)

	arts, _, err := c.Translate(skill)
	if err != nil {
		t.Fatalf("Translate(skill) error: %v", err)
	}

	skillArt := findArtifactByPath(arts, "skills/list-skill/SKILL.md")
	if skillArt == nil {
		t.Fatal("missing artifact")
	}

	body := string(skillArt.Bytes)

	// Should NOT contain Go map formatting.
	if strings.Contains(body, "map[") {
		t.Errorf("list-of-maps should not use Go map format, got: %q", body)
	}

	// Should contain properly indented YAML.
	if !strings.Contains(body, "entries:\n") {
		t.Errorf("entries key missing, got: %q", body)
	}
	for _, expected := range []string{"  - key: a\n    val: 1", "  - key: b\n    val: 2"} {
		if !strings.Contains(body, expected) {
			t.Errorf("list item %q missing, got: %q", expected, body)
		}
	}
}

func TestFactorySkill_FrontmatterExtraFieldsAreDeterministic(t *testing.T) {
	c := &factoryCompiler{}
	skill := ir.NewSkill("stable", "Stable", "body\n", nil, map[string]any{
		"zebra":  "z",
		"alpha":  "a",
		"middle": "m",
	}, ir.SourceRef{})
	const want = "---\nname: stable\ndescription: Stable\nalpha: a\nmiddle: m\nzebra: z\n---\nbody\n"

	for range 100 {
		arts, _, err := c.Translate(skill)
		if err != nil {
			t.Fatalf("Translate(skill): %v", err)
		}
		got := string(findArtifactByPath(arts, "skills/stable/SKILL.md").Bytes)
		if got != want {
			t.Fatalf("frontmatter = %q, want %q", got, want)
		}
	}
}
