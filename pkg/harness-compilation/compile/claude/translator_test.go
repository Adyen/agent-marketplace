package claude

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func findArtifactByPath(arts []artifact.Artifact, path string) *artifact.Artifact {
	for i := range arts {
		if arts[i].Path == path {
			return &arts[i]
		}
	}
	return nil
}

// --- Claude Skill test: frontmatter rendered in SKILL.md ---
func TestClaudeSkill_FrontmatterRendered(t *testing.T) {
	c := &claudeCompiler{}
	skill := ir.NewSkill("my-skill", "A test skill", "# My Skill Body\nHello world", []string{"click", "mention"}, map[string]any{"custom": 42}, ir.SourceRef{File: "skills/my-skill/SKILL.md", Line: 1})

	arts, diags, err := c.Translate(skill)
	if err != nil {
		t.Fatalf("Translate(skill) error: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(skill) diagnostics = %v, want 0", diags)
	}

	// Expect: skills/my-skill/SKILL.md with frontmatter
	skillArt := findArtifactByPath(arts, "skills/my-skill/SKILL.md")
	if skillArt == nil {
		t.Fatal("missing artifact skills/my-skill/SKILL.md")
	}

	body := string(skillArt.Bytes)
	if !strings.HasPrefix(body, "---\n") {
		t.Fatalf("skill artifact should start with frontmatter, got: %q", body[:min(len(body), 80)])
	}
	if !strings.Contains(body, "name: my-skill") {
		t.Errorf("skill frontmatter missing 'name: my-skill', got: %q", body)
	}
	if !strings.Contains(body, "description: A test skill") {
		t.Errorf("skill frontmatter missing 'description: A test skill', got: %q", body)
	}
	if !strings.Contains(body, "triggers:") {
		t.Errorf("skill frontmatter missing triggers, got: %q", body)
	}
	if !strings.Contains(body, "# My Skill Body\nHello world") {
		t.Errorf("skill artifact missing body content after frontmatter, got: %q", body)
	}

	// Expect: .claude-plugin/plugin.json fragment
	pluginArt := findArtifactByPath(arts, ".claude-plugin/plugin.json")
	if pluginArt == nil {
		t.Fatal("missing artifact .claude-plugin/plugin.json")
	}
	if !pluginArt.Fragment {
		t.Error("plugin.json artifact should be a fragment")
	}
	if pluginArt.FragmentKey != "plugin.json" {
		t.Errorf("plugin.json FragmentKey = %q, want %q", pluginArt.FragmentKey, "plugin.json")
	}

	// Verify plugin.json content has skills entry
	var pluginData map[string]any
	if err := json.Unmarshal(pluginArt.Bytes, &pluginData); err != nil {
		t.Fatalf("plugin.json is not valid JSON: %v", err)
	}
	skillsArr, ok := pluginData["skills"].([]any)
	if !ok || len(skillsArr) != 1 {
		t.Fatalf("plugin.json skills = %v, want array of 1", pluginData["skills"])
	}
	skillEntry, ok := skillsArr[0].(map[string]any)
	if !ok {
		t.Fatal("skill entry is not a map")
	}
	if skillEntry["name"] != "my-skill" {
		t.Errorf("skill entry name = %v, want %q", skillEntry["name"], "my-skill")
	}
}

func TestClaudeSkill_PreservesExplicitEmptyTriggers(t *testing.T) {
	skill := ir.NewSkill("empty-triggers", "Skill", "body\n", []string{}, nil, ir.SourceRef{})
	arts, _, err := (&claudeCompiler{}).Translate(skill)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(findArtifactByPath(arts, "skills/empty-triggers/SKILL.md").Bytes); !strings.Contains(got, "triggers: []") {
		t.Fatalf("skill frontmatter = %q, want explicit empty triggers", got)
	}
}

func TestClaudeSkill_FrontmatterExtraFieldsAreDeterministic(t *testing.T) {
	c := &claudeCompiler{}
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

// --- Claude Command test (✓ passthrough) ---
func TestClaudeCommand_Passthrough(t *testing.T) {
	c := &claudeCompiler{}
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
	if !strings.Contains(string(cmdArt.Bytes), "description: A test command") {
		t.Errorf("command frontmatter missing description, got %q", string(cmdArt.Bytes))
	}
	if !strings.HasSuffix(string(cmdArt.Bytes), "echo hello") {
		t.Errorf("command body missing, got %q", string(cmdArt.Bytes))
	}
	if cmdArt.Fragment {
		t.Error("command artifact should not be a fragment")
	}
}

// --- Claude Subagent test (✓ passthrough) ---
func TestClaudeSubagent_Passthrough(t *testing.T) {
	c := &claudeCompiler{}
	sub := ir.NewSubagent("my-agent", "A test agent", "You are helpful", "sonnet", []string{"Read", "Write"}, "high", []string{"Bash"}, "worktree", map[string]any{}, ir.SourceRef{File: "agents/my-agent.md", Line: 1})

	arts, diags, err := c.Translate(sub)
	if err != nil {
		t.Fatalf("Translate(subagent) error: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(subagent) diagnostics = %v, want 0", diags)
	}

	agentArt := findArtifactByPath(arts, "agents/my-agent.md")
	if agentArt == nil {
		t.Fatal("missing artifact agents/my-agent.md")
	}

	// Verify the subagent markdown has frontmatter + body
	body := string(agentArt.Bytes)
	if !strings.Contains(body, "You are helpful") {
		t.Errorf("subagent body missing expected content, got: %q", body)
	}
	if !strings.Contains(body, "model: sonnet") {
		t.Errorf("subagent frontmatter missing model, got: %q", body)
	}
	if !strings.Contains(body, "effort: high") {
		t.Errorf("subagent frontmatter missing effort, got: %q", body)
	}

	// Should also have a plugin.json fragment
	pluginArt := findArtifactByPath(arts, ".claude-plugin/plugin.json")
	if pluginArt == nil {
		t.Fatal("missing artifact .claude-plugin/plugin.json")
	}
	if !pluginArt.Fragment {
		t.Error("plugin.json artifact should be a fragment")
	}
}

func TestClaudeSubagent_FrontmatterExtraFieldsAreDeterministic(t *testing.T) {
	c := &claudeCompiler{}
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
		got := string(findArtifactByPath(arts, "agents/stable.md").Bytes)
		if got != want {
			t.Fatalf("frontmatter = %q, want %q", got, want)
		}
	}
}

// --- Claude Hook test ---
// The baseline annotations `claude-code-hooks-use-the-plugin-hook-file` and
// `claude-code-emits-unknown-events-unchanged` are retired: native-hook-
// integration removes both requirements. Unknown events are now refused by
// hook-resources/claude-code-refuses-unknown-events (see
// hook_claude_catalog_test.go), and a catalog-declared event keeps the
// fragment shape asserted here.
func TestClaudeHook_Passthrough(t *testing.T) {
	c := &claudeCompiler{}
	hook := ir.NewHook("pre-commit-command", "PreToolUse", "echo test", "command", map[string]any{}, ir.SourceRef{File: "hooks/hooks.json", Line: 0})

	arts, diags, err := c.Translate(hook)
	if err != nil {
		t.Fatalf("Translate(hook) error: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(hook) diagnostics = %v, want 0", diags)
	}

	hookArt := findArtifactByPath(arts, "hooks/hooks.json")
	if hookArt == nil {
		t.Fatal("missing artifact hooks/hooks.json")
	}
	if !hookArt.Fragment {
		t.Error("hooks.json artifact should be a fragment")
	}
	if hookArt.FragmentKey != "hooks.json" {
		t.Errorf("hooks.json FragmentKey = %q, want %q", hookArt.FragmentKey, "hooks.json")
	}

	// Verify the JSON structure
	var hookData map[string]any
	if err := json.Unmarshal(hookArt.Bytes, &hookData); err != nil {
		t.Fatalf("hooks.json is not valid JSON: %v", err)
	}
	hooksMap, ok := hookData["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks.json missing 'hooks' key or not a map, got: %v", hookData)
	}
	rules, ok := hooksMap["PreToolUse"].([]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("hooks.json PreToolUse rules = %v, want array of 1", hooksMap["PreToolUse"])
	}
	rule, ok := rules[0].(map[string]any)
	if !ok {
		t.Fatal("hook rule is not a map")
	}
	if rule["type"] != "command" {
		t.Errorf("hook type = %v, want %q", rule["type"], "command")
	}
	if rule["command"] != "echo test" {
		t.Errorf("hook command = %v, want %q", rule["command"], "echo test")
	}
}

// --- Claude McpServer test (✓ passthrough) ---
func TestClaudeMcpServer_Passthrough(t *testing.T) {
	c := &claudeCompiler{}
	mcp := ir.NewMcpServer("my-server", "npx", []string{"-y", "some-mcp"}, map[string]string{"KEY": "val"}, map[string]any{}, ir.SourceRef{File: ".mcp.json", Line: 0})

	arts, diags, err := c.Translate(mcp)
	if err != nil {
		t.Fatalf("Translate(mcpserver) error: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(mcpserver) diagnostics = %v, want 0", diags)
	}

	mcpArt := findArtifactByPath(arts, ".mcp.json")
	if mcpArt == nil {
		t.Fatal("missing artifact .mcp.json")
	}
	if !mcpArt.Fragment {
		t.Error(".mcp.json artifact should be a fragment")
	}
	if mcpArt.FragmentKey != ".mcp.json" {
		t.Errorf(".mcp.json FragmentKey = %q, want %q", mcpArt.FragmentKey, ".mcp.json")
	}

	// Verify JSON structure
	var mcpData map[string]any
	if err := json.Unmarshal(mcpArt.Bytes, &mcpData); err != nil {
		t.Fatalf(".mcp.json is not valid JSON: %v", err)
	}
	serversMap, ok := mcpData["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf(".mcp.json missing 'mcpServers' key, got: %v", mcpData)
	}
	serverEntry, ok := serversMap["my-server"].(map[string]any)
	if !ok {
		t.Fatalf("my-server entry not a map, got: %v", serversMap["my-server"])
	}
	if serverEntry["command"] != "npx" {
		t.Errorf("mcp command = %v, want %q", serverEntry["command"], "npx")
	}
}

func TestClaudeFrontmatterQuotesExtraKeysAndScalars(t *testing.T) {
	skill := ir.NewSkill("safe", "description", "body\n", nil, map[string]any{
		"extra: key": "true",
	}, ir.SourceRef{})
	arts, _, err := (&claudeCompiler{}).Translate(skill)
	if err != nil {
		t.Fatal(err)
	}
	body := string(findArtifactByPath(arts, "skills/safe/SKILL.md").Bytes)
	if !strings.Contains(body, "'extra: key': 'true'\n") {
		t.Fatalf("frontmatter did not quote extra key/scalar: %q", body)
	}
}

func TestClaudeFrontmatterReturnsRenderErrors(t *testing.T) {
	skill := ir.NewSkill("unsupported", "description", "body\n", nil, map[string]any{"extra": make(chan int)}, ir.SourceRef{})
	if _, _, err := (&claudeCompiler{}).Translate(skill); err == nil {
		t.Fatal("Translate accepted an unsupported YAML value")
	}
}

// --- Claude Compiler registration test ---
func TestClaudeCompiler_Registered(t *testing.T) {
	c, ok := getCompiler()
	if !ok {
		t.Fatal("Claude compiler not registered")
	}
	if c.Target() != mapping.TargetClaude {
		t.Errorf("Target() = %q, want %q", c.Target(), mapping.TargetClaude)
	}
}
