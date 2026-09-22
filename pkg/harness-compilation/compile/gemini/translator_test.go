package gemini_test

import (
	"encoding/json"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/gemini"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type geminiCommandDoc struct {
	Name        string   `toml:"name"`
	Description string   `toml:"description"`
	Prompt      string   `toml:"prompt"`
	Triggers    []string `toml:"triggers"`
}

func TestCompileSkillToSkillsDirSKILLMD(t *testing.T) {
	skill := ir.NewSkill(
		"deploy-service",
		"Deploy the service",
		"Run `${CLAUDE_PLUGIN_ROOT}/scripts/deploy.sh`.",
		[]string{"deploy"},
		map[string]any{"supporting_files": []string{"scripts/deploy.sh"}},
		ir.SourceRef{File: "skills/deploy-service/SKILL.md", Line: 1},
	)

	result, err := compile.Compile(skill, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(skill) error: %v", err)
	}

	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}
	wantPath := ".gemini/skills/deploy-service/SKILL.md"
	if got := result.Artifacts[0].Path; got != wantPath {
		t.Fatalf("artifact path = %q, want %q", got, wantPath)
	}

	body := string(result.Artifacts[0].Bytes)

	// Must contain YAML frontmatter
	if !strings.HasPrefix(body, "---\n") {
		t.Fatalf("skill artifact should start with frontmatter, got: %q", body[:min(len(body), 80)])
	}
	if !strings.Contains(body, "name: deploy-service") {
		t.Fatalf("skill frontmatter missing 'name: deploy-service', got: %q", body)
	}
	if !strings.Contains(body, "description: Deploy the service") {
		t.Fatalf("skill frontmatter missing 'description: Deploy the service', got: %q", body)
	}
	if !strings.Contains(body, "triggers:") || !strings.Contains(body, "  - deploy") {
		t.Fatalf("skill frontmatter missing triggers, got: %q", body)
	}
	if !strings.Contains(body, "Run `${CLAUDE_PLUGIN_ROOT}/scripts/deploy.sh`.") {
		t.Fatalf("skill body missing after frontmatter, got: %q", body)
	}

	if result.Artifacts[0].Fragment {
		t.Fatalf("artifact should not be a fragment")
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none (skill has native Gemini representation)", result.Diagnostics)
	}
}

func TestCompileSkill_FrontmatterExtraFieldsAreDeterministic(t *testing.T) {
	skill := ir.NewSkill("stable", "Stable", "body\n", nil, map[string]any{
		"zebra":  "z",
		"alpha":  "a",
		"middle": "m",
	}, ir.SourceRef{})
	const want = "---\nname: stable\ndescription: Stable\nalpha: a\nmiddle: m\nzebra: z\n---\nbody\n"

	for range 100 {
		result, err := compile.Compile(skill, compile.TargetGemini)
		if err != nil {
			t.Fatalf("Compile(skill): %v", err)
		}
		got := string(result.Artifacts[0].Bytes)
		if got != want {
			t.Fatalf("frontmatter = %q, want %q", got, want)
		}
	}
}

func TestCompileSkillWithoutSupportingFilesToSkillsDir(t *testing.T) {
	skill := ir.NewSkill(
		"plain-skill",
		"Pure markdown skill",
		"Use `${CLAUDE_PLUGIN_ROOT}` inline.",
		nil,
		nil,
		ir.SourceRef{File: "skills/plain-skill/SKILL.md", Line: 1},
	)

	result, err := compile.Compile(skill, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(skill) error: %v", err)
	}

	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}
	wantPath := ".gemini/skills/plain-skill/SKILL.md"
	if got := result.Artifacts[0].Path; got != wantPath {
		t.Fatalf("artifact path = %q, want %q", got, wantPath)
	}

	body := string(result.Artifacts[0].Bytes)
	if !strings.Contains(body, "${CLAUDE_PLUGIN_ROOT}") {
		t.Fatalf("artifact body = %q, want body containing ${CLAUDE_PLUGIN_ROOT}", body)
	}
	if !strings.HasPrefix(body, "---\n") {
		t.Fatalf("skill artifact should start with frontmatter, got: %q", body[:min(len(body), 80)])
	}

	if result.Artifacts[0].Fragment {
		t.Fatalf("artifact should not be a fragment")
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", result.Diagnostics)
	}
}

func TestCompileCommandToTOML(t *testing.T) {
	command := ir.NewCommand(
		"review",
		"Review changes",
		"Summarize diffs in `${CLAUDE_PLUGIN_ROOT}`.",
		nil,
		ir.SourceRef{File: "commands/review.md", Line: 1},
	)

	result, err := compile.Compile(command, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(command) error: %v", err)
	}

	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}
	if got := result.Artifacts[0].Path; got != "commands/review.toml" {
		t.Fatalf("artifact path = %q, want %q", got, "commands/review.toml")
	}

	doc := decodeCommandDoc(t, result.Artifacts[0].Bytes)
	if doc.Prompt != "Summarize diffs in `${extensionPath}`." {
		t.Fatalf("prompt = %q", doc.Prompt)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", result.Diagnostics)
	}
}

func TestCompileCommandPromptStaysTopLevelWithNestedMapExtras(t *testing.T) {
	command := ir.NewCommand(
		"demo",
		"Demo command",
		"Summarize the diff.",
		map[string]any{"meta": map[string]any{"owner": "team"}},
		ir.SourceRef{File: "commands/demo.md", Line: 1},
	)

	result, err := compile.Compile(command, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(command) error: %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", result.Diagnostics)
	}

	text := string(result.Artifacts[0].Bytes)
	promptIdx := strings.Index(text, `prompt = """`)
	tableIdx := strings.Index(text, "[meta]")
	if promptIdx == -1 {
		t.Fatalf("TOML missing prompt, got:\n%s", text)
	}
	if tableIdx == -1 {
		t.Fatalf("TOML missing nested [meta] table, got:\n%s", text)
	}
	if promptIdx > tableIdx {
		t.Fatalf("prompt must stay before nested table; TOML:\n%s", text)
	}

	doc := decodeCommandDoc(t, result.Artifacts[0].Bytes)
	if doc.Prompt != "Summarize the diff." {
		t.Fatalf("prompt = %q, want top-level value", doc.Prompt)
	}
}

func TestCompileSubagentEmitsAgentMD(t *testing.T) {
	subagent := ir.NewSubagent(
		"reviewer",
		"Review code",
		"Do review work.",
		"gemini-3-preview",
		[]string{"Read", "Write"},
		"",
		nil,
		"",
		nil,
		ir.SourceRef{File: "agents/reviewer.md", Line: 1},
	)

	result, err := compile.Compile(subagent, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(subagent) error: %v", err)
	}

	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}

	art := result.Artifacts[0]
	if got := art.Path; got != ".gemini/agents/reviewer.md" {
		t.Fatalf("artifact path = %q, want %q", got, ".gemini/agents/reviewer.md")
	}

	text := string(art.Bytes)

	// Must have YAML frontmatter delimiters
	if !strings.HasPrefix(text, "---\n") {
		t.Fatalf("missing opening frontmatter delimiter")
	}
	if !strings.Contains(text, "\n---\n") {
		t.Fatalf("missing closing frontmatter delimiter")
	}

	// Must have name, description, kind, model, tools in frontmatter
	if !strings.Contains(text, "name: reviewer") {
		t.Fatalf("missing name in frontmatter: %q", text)
	}
	if !strings.Contains(text, "description: Review code") {
		t.Fatalf("missing description in frontmatter: %q", text)
	}
	if !strings.Contains(text, "kind: local") {
		t.Fatalf("missing kind: local in frontmatter: %q", text)
	}
	// Model should be mapped via mapping table
	if !strings.Contains(text, "model: ") {
		t.Fatalf("missing model in frontmatter: %q", text)
	}
	if !strings.Contains(text, "tools:\n  - Read\n  - Write") {
		t.Fatalf("missing tools in frontmatter: %q", text)
	}

	// Body must follow frontmatter
	if !strings.Contains(text, "Do review work.") {
		t.Fatalf("missing body after frontmatter: %q", text)
	}

	// No diagnostics expected for a fully-supported subagent
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", result.Diagnostics)
	}
}

func TestCompileSubagentWithUnknownToolFailsClosed(t *testing.T) {
	subagent := ir.NewSubagent(
		"custom-agent",
		"Custom agent",
		"Custom work.",
		"gemini-3-preview",
		[]string{"Read", "UnknownTool"},
		"",
		nil,
		"",
		nil,
		ir.SourceRef{File: "agents/custom-agent.md", Line: 1},
	)

	result, err := compile.Compile(subagent, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(subagent) error: %v", err)
	}

	if len(result.Artifacts) != 0 {
		t.Fatalf("artifacts = %d, want 0", len(result.Artifacts))
	}

	if len(result.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %d, want 1", len(result.Diagnostics))
	}
	if result.Diagnostics[0].Code != diag.CodeSubagentGeminiToolUnknown || result.Diagnostics[0].Severity != diag.SeverityError {
		t.Fatalf("diagnostic = %#v, want Gemini unknown-tool error", result.Diagnostics[0])
	}
}

func TestCompileSubagentWithNoModel(t *testing.T) {
	subagent := ir.NewSubagent(
		"no-model-agent",
		"Agent without model",
		"Work without model.",
		"",
		[]string{"Read"},
		"",
		nil,
		"",
		nil,
		ir.SourceRef{File: "agents/no-model-agent.md", Line: 1},
	)

	result, err := compile.Compile(subagent, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(subagent) error: %v", err)
	}

	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}

	text := string(result.Artifacts[0].Bytes)
	// No model field in frontmatter when model is empty
	if strings.Contains(text, "model:") {
		t.Fatalf("unexpected model field in frontmatter: %q", text)
	}
}

func TestCompileHookToSettingsJSON(t *testing.T) {
	hook := ir.NewHook(
		"pre-commit",
		"PreToolUse",
		"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh",
		"command",
		nil,
		ir.SourceRef{File: "hooks/hooks.json", Line: 3},
	)

	result, err := compile.Compile(hook, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(hook) error: %v", err)
	}

	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}

	got := result.Artifacts[0]
	if got.Path != ".gemini/settings.json" {
		t.Fatalf("artifact path = %q, want %q", got.Path, ".gemini/settings.json")
	}
	if !got.Fragment {
		t.Fatalf("artifact Fragment = false, want true")
	}
	if got.FragmentKey != ".gemini/settings.json" {
		t.Fatalf("artifact FragmentKey = %q, want %q", got.FragmentKey, ".gemini/settings.json")
	}

	// Verify the JSON content
	var settings map[string]any
	if err := json.Unmarshal(got.Bytes, &settings); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("settings has no hooks map: %#v", settings)
	}
	beforeTool, ok := hooks["BeforeTool"].([]any)
	if !ok {
		t.Fatalf("hooks has no BeforeTool array: %#v", hooks)
	}
	if len(beforeTool) != 1 {
		t.Fatalf("BeforeTool has %d entries, want 1", len(beforeTool))
	}
	entry, ok := beforeTool[0].(map[string]any)
	if !ok {
		t.Fatalf("BeforeTool[0] is not a map: %#v", beforeTool[0])
	}
	groupHooks, ok := entry["hooks"].([]any)
	if !ok || len(groupHooks) != 1 {
		t.Fatalf("native group hooks = %#v", entry["hooks"])
	}
	entry, ok = groupHooks[0].(map[string]any)
	if !ok {
		t.Fatalf("native hook is not an object: %#v", groupHooks[0])
	}
	if entry["command"] != "${extensionPath}/scripts/check.sh" {
		t.Fatalf("command = %q, want %q", entry["command"], "${extensionPath}/scripts/check.sh")
	}
	if entry["type"] != "command" {
		t.Fatalf("type = %q, want %q", entry["type"], "command")
	}

	// No diagnostics for a supported hook
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", result.Diagnostics)
	}
}

func TestCompileHookEventMapping(t *testing.T) {
	tests := []struct {
		claudeEvent string
		geminiEvent string
		mapped      bool
	}{
		{"PreToolUse", "BeforeTool", true},
		{"PostToolUse", "AfterTool", true},
		{"PreCompact", "PreCompress", true},
		{"UserPromptSubmit", "BeforeAgent", true},
		{"Stop", "", false},
	}

	for _, tt := range tests {
		name := tt.claudeEvent + "->" + tt.geminiEvent
		if tt.geminiEvent == "" {
			name = tt.claudeEvent + "->unmapped"
		}
		t.Run(name, func(t *testing.T) {
			hook := ir.NewHook(
				"test-hook",
				tt.claudeEvent,
				"echo test",
				"command",
				nil,
				ir.SourceRef{File: "hooks/hooks.json", Line: 1},
			)

			result, err := compile.Compile(hook, compile.TargetGemini)
			if err != nil {
				t.Fatalf("Compile(hook) error: %v", err)
			}

			if !tt.mapped {
				if len(result.Artifacts) != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != diag.CodeHookGeminiEventUnmappable {
					t.Fatalf("unmapped event result = %#v, want stable loss diagnostic and no artifact", result)
				}
				return
			}
			if len(result.Artifacts) == 0 {
				t.Fatalf("no artifacts produced for event %q", tt.claudeEvent)
			}

			var settings map[string]any
			if err := json.Unmarshal(result.Artifacts[0].Bytes, &settings); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			hooks, ok := settings["hooks"].(map[string]any)
			if !ok {
				t.Fatalf("no hooks map in settings: %#v", settings)
			}
			if _, ok := hooks[tt.geminiEvent]; !ok {
				t.Fatalf("expected Gemini event %q in hooks, got keys: %v", tt.geminiEvent, keysOf(hooks))
			}
		})
	}
}

func TestCompileHookFragmentsMerge(t *testing.T) {
	hook1 := ir.NewHook("hook1", "PreToolUse", "echo pre", "command", nil, ir.SourceRef{File: "hooks/hooks.json", Line: 1})
	hook2 := ir.NewHook("hook2", "PostToolUse", "echo post", "command", nil, ir.SourceRef{File: "hooks/hooks.json", Line: 2})

	result1, err := compile.Compile(hook1, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(hook1) error: %v", err)
	}
	result2, err := compile.Compile(hook2, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(hook2) error: %v", err)
	}

	merged, err := artifact.MergeFragments(append(result1.Artifacts, result2.Artifacts...))
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	if len(merged) != 1 {
		t.Fatalf("merged artifacts = %d, want 1", len(merged))
	}

	var settings map[string]any
	if err := json.Unmarshal(merged[0].Bytes, &settings); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	hooks := settings["hooks"].(map[string]any)
	if _, ok := hooks["BeforeTool"]; !ok {
		t.Fatalf("missing BeforeTool in merged hooks: %v", hooks)
	}
	if _, ok := hooks["AfterTool"]; !ok {
		t.Fatalf("missing AfterTool in merged hooks: %v", hooks)
	}
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestCompileMcpServerToGeminiExtensionFragment(t *testing.T) {
	server := ir.NewMcpServer(
		"docs",
		"${CLAUDE_PLUGIN_ROOT}/bin/server",
		[]string{"--root", "${CLAUDE_PLUGIN_ROOT}/data"},
		map[string]string{"PLUGIN_ROOT": "${CLAUDE_PLUGIN_ROOT}"},
		nil,
		ir.SourceRef{File: ".mcp.json", Line: 1},
	)

	result, err := compile.Compile(server, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(mcp) error: %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}

	got := result.Artifacts[0]
	if got.Path != "gemini-extension.json" || !got.Fragment || got.FragmentKey != "gemini-extension.json" {
		t.Fatalf("unexpected fragment metadata: %#v", got)
	}

	merged, err := artifact.MergeFragments(result.Artifacts)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	var ext struct {
		McpServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged[0].Bytes, &ext); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if ext.McpServers["docs"].Command != "${extensionPath}/bin/server" {
		t.Fatalf("command = %q", ext.McpServers["docs"].Command)
	}
	if ext.McpServers["docs"].Args[1] != "${extensionPath}/data" {
		t.Fatalf("args = %#v", ext.McpServers["docs"].Args)
	}
	if ext.McpServers["docs"].Env["PLUGIN_ROOT"] != "${extensionPath}" {
		t.Fatalf("env = %#v", ext.McpServers["docs"].Env)
	}
}

func TestTOMLPromptRoundTripPreservesMarkdownBlocks(t *testing.T) {
	body := strings.Join([]string{
		"---",
		"title: example",
		"owner: team",
		"---",
		"",
		"Here is a fenced block:",
		"```bash",
		"printf 'hello'",
		"```",
		"",
		"    indented block",
		"",
		"Done.",
	}, "\n")

	command := ir.NewCommand(
		"round-trip",
		"Round trip markdown",
		body,
		nil,
		ir.SourceRef{File: "commands/round-trip.md", Line: 1},
	)

	result, err := compile.Compile(command, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(command) error: %v", err)
	}
	text := string(result.Artifacts[0].Bytes)
	if !strings.Contains(text, "prompt = \"\"\"") {
		t.Fatalf("TOML = %q, want multiline prompt", text)
	}

	doc := decodeCommandDoc(t, result.Artifacts[0].Bytes)
	if doc.Prompt != body {
		t.Fatalf("prompt mismatch:\n--- got ---\n%s\n--- want ---\n%s", doc.Prompt, body)
	}
}

func TestGeminiSkillsAreNotFragments(t *testing.T) {
	// Skills now emit to .gemini/skills/<n>/SKILL.md, not as GEMINI.md
	// fragments, so multiple skills should produce separate non-fragment
	// artifacts rather than a single merged file.
	first := ir.NewSkill("alpha", "Alpha", "First body", nil, nil, ir.SourceRef{File: "skills/alpha/SKILL.md", Line: 1})
	second := ir.NewSkill("beta", "Beta", "Second body", nil, nil, ir.SourceRef{File: "skills/beta/SKILL.md", Line: 1})

	firstResult, err := compile.Compile(first, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(first) error: %v", err)
	}
	secondResult, err := compile.Compile(second, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(second) error: %v", err)
	}

	allArtifacts := append(firstResult.Artifacts, secondResult.Artifacts...)
	for _, a := range allArtifacts {
		if a.Fragment {
			t.Fatalf("skill artifact %q should not be a fragment", a.Path)
		}
	}

	// MergeFragments should be a no-op (no fragments).
	merged, err := artifact.MergeFragments(allArtifacts)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("merged artifacts = %d, want 2 (one per skill)", len(merged))
	}
}

func TestCompileSkill_PreservesExplicitEmptyTriggers(t *testing.T) {
	skill := ir.NewSkill("empty-triggers", "Empty triggers", "body\n", []string{}, nil, ir.SourceRef{})
	result, err := compile.Compile(skill, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(skill) error: %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}
	if !strings.Contains(string(result.Artifacts[0].Bytes), "triggers: []") {
		t.Fatalf("skill frontmatter missing explicit empty triggers: %q", result.Artifacts[0].Bytes)
	}
}

func TestCompileSubagent_PreservesExplicitEmptyTools(t *testing.T) {
	subagent := ir.NewSubagent("empty-tools", "Empty tools", "body\n", "sonnet", []string{}, "", nil, "", nil, ir.SourceRef{})
	result, err := compile.Compile(subagent, compile.TargetGemini)
	if err != nil {
		t.Fatalf("Compile(subagent) error: %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}
	if !strings.Contains(string(result.Artifacts[0].Bytes), "tools: []") {
		t.Fatalf("subagent frontmatter missing explicit empty tools: %q", result.Artifacts[0].Bytes)
	}
}

func decodeCommandDoc(t *testing.T, data []byte) geminiCommandDoc {
	t.Helper()

	var doc geminiCommandDoc
	if err := toml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("toml.Unmarshal: %v\n%s", err, data)
	}
	return doc
}
