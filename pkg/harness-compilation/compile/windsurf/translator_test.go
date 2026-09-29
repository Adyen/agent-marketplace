package windsurf

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	compilepkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

func TestTranslateSkill_NativeSKILLMd(t *testing.T) {
	translator := Compiler{}
	skill := ir.NewSkill(
		"deploy",
		"Deploy the app",
		"# Deploy\n\nUse the workflow.\n",
		[]string{"mention"},
		nil,
		ir.SourceRef{File: "skills/deploy/SKILL.md", Line: 1},
	)

	artifacts, diags, err := translator.Translate(skill)
	if err != nil {
		t.Fatalf("Translate(skill): %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(skill) diags = %d, want 0", len(diags))
	}

	artifact := singleArtifact(t, artifacts)
	if artifact.Path != ".windsurf/skills/deploy/SKILL.md" {
		t.Fatalf("artifact path = %q, want %q", artifact.Path, ".windsurf/skills/deploy/SKILL.md")
	}
	assertGoldenFile(t, "testdata/skill_native.golden", artifact.Bytes)
}

func TestCompilerDirectTranslationGuardsNativeCodexSecurity(t *testing.T) {
	server := ir.NewMcpServerWithOptions("native", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"))
	server.Ext.Codex = &extpkg.Codex{BearerTokenEnvVar: "TOKEN"}

	artifacts, diagnostics, err := (Compiler{}).Translate(server)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("artifacts = %d, want none for dropped native authentication", len(artifacts))
	}
	if !diag.HasErrors(diagnostics) || !diag.HasCode(diagnostics, diag.CodeMcpSecurityRestrictionUnsupported) {
		t.Fatalf("diagnostics = %#v, want %s error", diagnostics, diag.CodeMcpSecurityRestrictionUnsupported)
	}
}

func TestTranslateSkill_PreservesExplicitEmptyTriggers(t *testing.T) {
	skill := ir.NewSkill("empty-triggers", "Skill", "body\n", []string{}, nil, ir.SourceRef{})
	artifacts, _, err := (Compiler{}).Translate(skill)
	if err != nil {
		t.Fatal(err)
	}
	art := singleArtifact(t, artifacts)
	if !bytes.Contains(art.Bytes, []byte("triggers: []")) {
		t.Fatalf("skill frontmatter = %q, want explicit empty triggers", art.Bytes)
	}
}

func TestTranslateSkill_FrontmatterExtraFieldsAreDeterministic(t *testing.T) {
	translator := Compiler{}
	skill := ir.NewSkill("stable", "Stable", "body\n", nil, map[string]any{
		"zebra":  "z",
		"alpha":  "a",
		"middle": "m",
	}, ir.SourceRef{})
	const want = "---\nname: stable\ndescription: Stable\nalpha: a\nmiddle: m\nzebra: z\n---\nbody\n"

	for range 100 {
		artifacts, _, err := translator.Translate(skill)
		if err != nil {
			t.Fatalf("Translate(skill): %v", err)
		}
		got := string(singleArtifact(t, artifacts).Bytes)
		if got != want {
			t.Fatalf("frontmatter = %q, want %q", got, want)
		}
	}
}

func TestTranslateSkill_NoScriptsFlattenedDiagnostic(t *testing.T) {
	// Skills with support files should no longer emit the
	// skill.windsurf.scripts-flattened diagnostic; Windsurf now
	// supports the agentskills.io format with native skill directories.
	translator := Compiler{}
	skill := ir.NewSkill(
		"deploy",
		"Deploy the app",
		"# Deploy\n\nUse the workflow.\n",
		[]string{"mention"},
		nil,
		ir.SourceRef{File: "skills/deploy/SKILL.md", Line: 1},
	)
	skill.SupportFiles = []string{"scripts/bootstrap.sh"}

	artifacts, diags, err := translator.Translate(skill)
	if err != nil {
		t.Fatalf("Translate(skill with scripts): %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("Translate(skill with scripts) artifacts = %d, want 1", len(artifacts))
	}
	if len(diags) != 1 || diags[0].Code != diag.CodeSkillSupportFilesDeferred {
		t.Fatalf("Translate(skill with scripts) diags = %#v, want support-file deferral diagnostic", diags)
	}
}

func TestTranslateCommand_Workflow(t *testing.T) {
	translator := Compiler{}
	command := ir.NewCommand(
		"format",
		"Format the project",
		"make fmt\n",
		nil,
		ir.SourceRef{File: "commands/format.md", Line: 1},
	)

	artifacts, diags, err := translator.Translate(command)
	if err != nil {
		t.Fatalf("Translate(command): %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(command) diags = %d, want 0", len(diags))
	}

	artifact := singleArtifact(t, artifacts)
	if artifact.Path != ".windsurf/workflows/format.md" {
		t.Fatalf("artifact path = %q, want %q", artifact.Path, ".windsurf/workflows/format.md")
	}
	assertGoldenFile(t, "testdata/command_workflow.golden", artifact.Bytes)
}

func TestTranslateSubagent_NoPrimitive(t *testing.T) {
	translator := Compiler{}
	subagent := ir.NewSubagent(
		"reviewer",
		"Reviews changes",
		"You review changes carefully.\n",
		"sonnet",
		[]string{"Read", "Write"},
		"high",
		nil,
		"worktree",
		nil,
		ir.SourceRef{File: "agents/reviewer.md", Line: 1},
	)

	artifacts, diags, err := translator.Translate(subagent)
	if err != nil {
		t.Fatalf("Translate(subagent): %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("Translate(subagent) artifacts = %d, want 0", len(artifacts))
	}
	if len(diags) != 1 {
		t.Fatalf("Translate(subagent) diags = %d, want 1", len(diags))
	}
	if diags[0].Code != diag.CodeSubagentSecurityRestrictionUnsupported {
		t.Fatalf("diag code = %q, want %q", diags[0].Code, diag.CodeSubagentSecurityRestrictionUnsupported)
	}
	if diags[0].Severity != diag.SeverityError {
		t.Fatalf("diag severity = %v, want %v", diags[0].Severity, diag.SeverityError)
	}
}

func TestTranslateHook_PreservesWindsurfSidecar(t *testing.T) {
	showOutput := false
	hook := ir.NewHook("native", "post_setup_worktree", "scripts/setup.sh", "command", nil, ir.SourceRef{})
	hook.Ext.Windsurf = &extpkg.Windsurf{
		OriginalHookEvent: "post_setup_worktree",
		HookPowerShell:    "Write-Host setup",
		HookShowOutput:    &showOutput,
		HookWorkingDir:    "/tmp/project",
	}

	artifacts, diags, err := (Compiler{}).Translate(hook)
	if err != nil {
		t.Fatalf("Translate(hook): %v", err)
	}
	if len(diags) != 0 || len(artifacts) != 1 {
		t.Fatalf("Translate(hook) = artifacts %d, diags %#v", len(artifacts), diags)
	}
	var doc map[string]any
	if err := json.Unmarshal(artifacts[0].Bytes, &doc); err != nil {
		t.Fatalf("unmarshal hook: %v", err)
	}
	hooks := doc["hooks"].(map[string]any)
	groups := hooks["post_setup_worktree"].([]any)
	body := groups[0].(map[string]any)
	if body["powershell"] != "Write-Host setup" || body["show_output"] != false || body["working_directory"] != "/tmp/project" {
		t.Fatalf("native sidecar fields = %#v", body)
	}
}

func TestTranslateHook_SidecarWorkdirConsumesIdenticalExtra(t *testing.T) {
	hook := ir.NewHookWithMatcher("native", "PreToolUse", "Read", "scripts/setup.sh", "command", map[string]any{"working_directory": "/tmp/project"}, ir.SourceRef{})
	hook.Ext.Windsurf = &extpkg.Windsurf{OriginalHookEvent: "PreToolUse", HookWorkingDir: "/tmp/project"}
	artifacts, diags, err := (Compiler{}).Translate(hook)
	if err != nil || len(artifacts) != 1 || len(diags) != 0 {
		t.Fatalf("Translate(hook) = artifacts %d diags %#v err=%v", len(artifacts), diags, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(artifacts[0].Bytes, &doc); err != nil {
		t.Fatal(err)
	}
	hooksDoc := doc["hooks"].(map[string]any)
	var body map[string]any
	for _, raw := range hooksDoc {
		groups, ok := raw.([]any)
		if ok && len(groups) > 0 {
			body = groups[0].(map[string]any)
			break
		}
	}
	if body == nil {
		t.Fatalf("hooks document = %#v", doc)
	}
	if body["working_directory"] != "/tmp/project" || hook.Extra["working_directory"] != "/tmp/project" {
		t.Fatalf("workdir output/extra = %#v / %#v", body, hook.Extra)
	}
}

func TestTranslateHook_SidecarWorkdirMismatchFails(t *testing.T) {
	hook := ir.NewHookWithMatcher("native", "PreToolUse", "Read", "scripts/setup.sh", "command", map[string]any{"working_directory": "/tmp/other"}, ir.SourceRef{})
	hook.Ext.Windsurf = &extpkg.Windsurf{OriginalHookEvent: "PreToolUse", HookWorkingDir: "/tmp/project"}
	_, diags, err := (Compiler{}).Translate(hook)
	if err != nil || !diag.HasCode(diags, diag.CodeHookWindsurfNativeFieldsUnsupported) {
		t.Fatalf("mismatch result err=%v diags=%#v", err, diags)
	}
}

func TestTranslateHook_AbsentSidecarWorkdirRemainsBlocked(t *testing.T) {
	hook := ir.NewHookWithMatcher("native", "PreToolUse", "Read", "scripts/setup.sh", "command", map[string]any{"working_directory": "/tmp/project"}, ir.SourceRef{})
	_, diags, err := (Compiler{}).Translate(hook)
	if err != nil || !diag.HasCode(diags, diag.CodeCanonicalFieldOverridesExtra) {
		t.Fatalf("absent sidecar result err=%v diags=%#v", err, diags)
	}
}

// spec: hook-resources/codex-devin-and-windsurf-receive-the-source-bundle-root
func TestTranslateHook_WithPluginRoot(t *testing.T) {
	hook := ir.NewHookWithMatcher(
		"PreToolUse-Read-command",
		"PreToolUse",
		"Read",
		"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh",
		"command",
		nil,
		ir.SourceRef{File: "hooks/hooks.json", Line: 0},
	)

	result, err := compilepkg.Compile(hook, compilepkg.TargetWindsurf, compilepkg.WithPluginRoot("/plugins/acme"))
	if err != nil {
		t.Fatalf("compile.Compile(hook): %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("compile.Compile(hook) diags = %d, want 0", len(result.Diagnostics))
	}

	artifact := singleArtifact(t, result.Artifacts)
	if artifact.Path != ".windsurf/hooks.json" {
		t.Fatalf("artifact path = %q, want %q", artifact.Path, ".windsurf/hooks.json")
	}
	assertGoldenFile(t, "testdata/hook_with_plugin_root.golden", artifact.Bytes)
}

// spec: hook-resources/missing-plugin-root-leaves-the-token-with-a-warning
func TestTranslateHook_WithoutPluginRoot(t *testing.T) {
	translator := Compiler{}
	hook := ir.NewHookWithMatcher(
		"PreToolUse-Read-command",
		"PreToolUse",
		"Read",
		"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh",
		"command",
		nil,
		ir.SourceRef{File: "hooks/hooks.json", Line: 0},
	)

	artifacts, diags, err := translator.TranslateWithOptions(hook, compilepkg.Options{})
	if err != nil {
		t.Fatalf("TranslateWithOptions(hook): %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("TranslateWithOptions(hook) diags = %d, want 1", len(diags))
	}
	if diags[0].Code != diag.CodeHookWindsurfEnvTokenExpanded {
		t.Fatalf("diag code = %q, want %q", diags[0].Code, diag.CodeHookWindsurfEnvTokenExpanded)
	}
	if diags[0].Severity != diag.SeverityWarning {
		t.Fatalf("diag severity = %v, want %v", diags[0].Severity, diag.SeverityWarning)
	}

	artifact := singleArtifact(t, artifacts)
	assertGoldenFile(t, "testdata/hook_without_plugin_root.golden", artifact.Bytes)
}

func TestTranslateHook_PreToolUse_Write(t *testing.T) {
	translator := Compiler{}
	hook := ir.NewHookWithMatcher(
		"PreToolUse-Write-command",
		"PreToolUse",
		"Write",
		"scripts/check-write.sh",
		"command",
		nil,
		ir.SourceRef{File: "hooks/hooks.json", Line: 0},
	)

	artifacts, diags, err := translator.Translate(hook)
	if err != nil {
		t.Fatalf("Translate(hook): %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(hook) diags = %d, want 0", len(diags))
	}

	artifact := singleArtifact(t, artifacts)
	if artifact.Path != ".windsurf/hooks.json" {
		t.Fatalf("artifact path = %q, want %q", artifact.Path, ".windsurf/hooks.json")
	}
	assertGoldenFile(t, "testdata/hook_pre_write_code.golden", artifact.Bytes)
}

// spec: hook-resources/windsurf-splits-pretooluse-by-tool-matcher
func TestTranslateHook_PreToolUse_Bash(t *testing.T) {
	translator := Compiler{}
	hook := ir.NewHookWithMatcher(
		"PreToolUse-Bash-command",
		"PreToolUse",
		"Bash",
		"scripts/check-bash.sh",
		"command",
		nil,
		ir.SourceRef{File: "hooks/hooks.json", Line: 0},
	)

	artifacts, diags, err := translator.Translate(hook)
	if err != nil {
		t.Fatalf("Translate(hook): %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(hook) diags = %d, want 0", len(diags))
	}

	artifact := singleArtifact(t, artifacts)
	if artifact.Path != ".windsurf/hooks.json" {
		t.Fatalf("artifact path = %q, want %q", artifact.Path, ".windsurf/hooks.json")
	}
	assertGoldenFile(t, "testdata/hook_pre_run_command.golden", artifact.Bytes)
}

func TestTranslateHook_PreToolUse_MCP(t *testing.T) {
	translator := Compiler{}
	hook := ir.NewHookWithMatcher(
		"PreToolUse-mcp-command",
		"PreToolUse",
		"mcp__myserver__mytool",
		"scripts/check-mcp.sh",
		"command",
		nil,
		ir.SourceRef{File: "hooks/hooks.json", Line: 0},
	)

	artifacts, diags, err := translator.Translate(hook)
	if err != nil {
		t.Fatalf("Translate(hook): %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(hook) diags = %d, want 0", len(diags))
	}

	artifact := singleArtifact(t, artifacts)
	if artifact.Path != ".windsurf/hooks.json" {
		t.Fatalf("artifact path = %q, want %q", artifact.Path, ".windsurf/hooks.json")
	}
	assertGoldenFile(t, "testdata/hook_pre_mcp_tool_use.golden", artifact.Bytes)
}

func TestTranslateHook_UserPromptSubmit(t *testing.T) {
	translator := Compiler{}
	hook := ir.NewHook(
		"UserPromptSubmit-command",
		"UserPromptSubmit",
		"scripts/on-prompt.sh",
		"command",
		nil,
		ir.SourceRef{File: "hooks/hooks.json", Line: 0},
	)

	artifacts, diags, err := translator.Translate(hook)
	if err != nil {
		t.Fatalf("Translate(hook): %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(hook) diags = %d, want 0", len(diags))
	}

	artifact := singleArtifact(t, artifacts)
	if artifact.Path != ".windsurf/hooks.json" {
		t.Fatalf("artifact path = %q, want %q", artifact.Path, ".windsurf/hooks.json")
	}
	assertGoldenFile(t, "testdata/hook_pre_user_prompt.golden", artifact.Bytes)
}

func TestTranslateHook_Stop(t *testing.T) {
	translator := Compiler{}
	hook := ir.NewHook(
		"Stop-command",
		"Stop",
		"scripts/on-stop.sh",
		"command",
		nil,
		ir.SourceRef{File: "hooks/hooks.json", Line: 0},
	)

	artifacts, diags, err := translator.Translate(hook)
	if err != nil {
		t.Fatalf("Translate(hook): %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(hook) diags = %d, want 0", len(diags))
	}

	artifact := singleArtifact(t, artifacts)
	if artifact.Path != ".windsurf/hooks.json" {
		t.Fatalf("artifact path = %q, want %q", artifact.Path, ".windsurf/hooks.json")
	}
	assertGoldenFile(t, "testdata/hook_post_cascade_response.golden", artifact.Bytes)
}

func TestTranslateHook_SubagentStop(t *testing.T) {
	translator := Compiler{}
	hook := ir.NewHook(
		"SubagentStop-command",
		"SubagentStop",
		"scripts/on-subagent-stop.sh",
		"command",
		nil,
		ir.SourceRef{File: "hooks/hooks.json", Line: 0},
	)

	artifacts, diags, err := translator.Translate(hook)
	if err != nil {
		t.Fatalf("Translate(hook): %v", err)
	}
	if len(artifacts) != 0 || len(diags) != 1 || diags[0].Code != diag.CodeHookWindsurfEventUnmappable || diags[0].Severity != diag.SeverityWarning {
		t.Fatalf("SubagentStop must be suppressed with an unmappable warning: artifacts=%v diagnostics=%v", artifacts, diags)
	}
}

// spec: hook-resources/gemini-and-windsurf-suppress-unmappable-events-with-a-warning
func TestTranslateHook_UnmappableEvent(t *testing.T) {
	translator := Compiler{}
	hook := ir.NewHook(
		"Notification-command",
		"Notification",
		"scripts/notify.sh",
		"command",
		nil,
		ir.SourceRef{File: "hooks/hooks.json", Line: 0},
	)

	artifacts, diags, err := translator.Translate(hook)
	if err != nil {
		t.Fatalf("Translate(hook): %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("Translate(hook) artifacts = %d, want 0 (unmappable)", len(artifacts))
	}
	if len(diags) != 1 {
		t.Fatalf("Translate(hook) diags = %d, want 1", len(diags))
	}
	if diags[0].Code != diag.CodeHookWindsurfEventUnmappable {
		t.Fatalf("diag code = %q, want %q", diags[0].Code, diag.CodeHookWindsurfEventUnmappable)
	}
	if diags[0].Severity != diag.SeverityWarning {
		t.Fatalf("diag severity = %v, want %v", diags[0].Severity, diag.SeverityWarning)
	}
}

// spec: hook-resources/windsurf-splits-pretooluse-by-tool-matcher
func TestTranslateHook_PreToolUse_EmptyMatcher(t *testing.T) {
	translator := Compiler{}
	hook := ir.NewHook(
		"PreToolUse-command",
		"PreToolUse",
		"scripts/check-all.sh",
		"command",
		nil,
		ir.SourceRef{File: "hooks/hooks.json", Line: 0},
	)

	artifacts, diags, err := translator.Translate(hook)
	if err != nil {
		t.Fatalf("Translate(hook): %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("Translate(hook) artifacts = %d, want 0 (unmappable)", len(artifacts))
	}
	if len(diags) != 1 {
		t.Fatalf("Translate(hook) diags = %d, want 1", len(diags))
	}
	if diags[0].Code != diag.CodeHookWindsurfEventUnmappable {
		t.Fatalf("diag code = %q, want %q", diags[0].Code, diag.CodeHookWindsurfEventUnmappable)
	}
}

func TestTranslateMcpServer_Fragment(t *testing.T) {
	translator := Compiler{}
	server := ir.NewMcpServer(
		"github",
		"npx",
		[]string{"-y", "@modelcontextprotocol/server-github"},
		map[string]string{
			"GITHUB_TOKEN": "${GITHUB_TOKEN}",
			"SETTINGS":     "${file:./settings.json}",
			"LITERAL":      "keep-me",
		},
		nil,
		ir.SourceRef{File: ".mcp.json", Line: 0},
	)

	artifacts, diags, err := translator.Translate(server)
	if err != nil {
		t.Fatalf("Translate(mcp): %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(mcp) diags = %d, want 0", len(diags))
	}

	artifact := singleArtifact(t, artifacts)
	if artifact.Path != ".windsurf/mcp_config.json" {
		t.Fatalf("artifact path = %q, want %q", artifact.Path, ".windsurf/mcp_config.json")
	}
	if !artifact.Fragment {
		t.Fatal("artifact.Fragment = false, want true")
	}
	if artifact.FragmentKey != "windsurf-mcp" {
		t.Fatalf("artifact.FragmentKey = %q, want %q", artifact.FragmentKey, "windsurf-mcp")
	}
	assertGoldenFile(t, "testdata/mcp_config_fragment.golden", artifact.Bytes)
}

func TestTranslateMcpServer_PreservesExplicitEmptyValues(t *testing.T) {
	server := ir.NewMcpServerWithOptions("empty", "npx", []string{}, map[string]string{}, nil, ir.SourceRef{}, ir.WithMcpHeaders(map[string]string{}))
	artifacts, _, err := (Compiler{}).Translate(server)
	if err != nil {
		t.Fatal(err)
	}
	art := singleArtifact(t, artifacts)
	var cfg struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(art.Bytes, &cfg); err != nil {
		t.Fatal(err)
	}
	entry := cfg.McpServers["empty"]
	if args, ok := entry["args"].([]any); !ok || len(args) != 0 {
		t.Fatalf("args = %#v, want explicit empty array", entry["args"])
	}
	if env, ok := entry["env"].(map[string]any); !ok || len(env) != 0 {
		t.Fatalf("env = %#v, want explicit empty object", entry["env"])
	}
	if headers, ok := entry["headers"].(map[string]any); !ok || len(headers) != 0 {
		t.Fatalf("headers = %#v, want explicit empty object", entry["headers"])
	}
}

func TestTranslateMcpServer_HttpWithExtraFields(t *testing.T) {
	translator := Compiler{}
	server := ir.NewMcpServer(
		"genai-grafana",
		"",
		nil,
		nil,
		map[string]any{
			"type":     "http",
			"url":      "https://genai-platform.example.com/mcp/",
			"headers":  map[string]any{"x-litellm-api-key": "Bearer ${GENAI_API_KEY}", "x-mcp-servers": "grafana"},
			"disabled": false,
		},
		ir.SourceRef{File: "mcp.json", Line: 0},
	)

	artifacts, diags, err := translator.Translate(server)
	if err != nil {
		t.Fatalf("Translate(mcp http): %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("Translate(mcp http) diags = %d, want 0 when canonical fields are absent", len(diags))
	}

	artifact := singleArtifact(t, artifacts)
	if artifact.Path != ".windsurf/mcp_config.json" {
		t.Fatalf("artifact path = %q, want %q", artifact.Path, ".windsurf/mcp_config.json")
	}
	assertGoldenFile(t, "testdata/mcp_http_fragment.golden", artifact.Bytes)
}

func TestTranslateMcpServer_EmptyCommandNoNullFields(t *testing.T) {
	translator := Compiler{}
	server := ir.NewMcpServer(
		"remote-mcp",
		"",
		nil,
		nil,
		map[string]any{
			"serverUrl": "https://example.com/mcp",
		},
		ir.SourceRef{File: "mcp.json", Line: 0},
	)

	artifacts, _, err := translator.Translate(server)
	if err != nil {
		t.Fatalf("Translate(mcp empty): %v", err)
	}

	artifact := singleArtifact(t, artifacts)
	var cfg struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(artifact.Bytes, &cfg); err != nil {
		t.Fatalf("unmarshal mcp config: %v", err)
	}
	entry, ok := cfg.McpServers["remote-mcp"]
	if !ok {
		t.Fatalf("missing remote-mcp entry: %v", cfg.McpServers)
	}
	if _, ok := entry["command"]; ok {
		t.Fatalf("empty command should be omitted, got: %#v", entry)
	}
	if _, ok := entry["args"]; ok {
		t.Fatalf("nil args should be omitted, got: %#v", entry)
	}
	if _, ok := entry["env"]; ok {
		t.Fatalf("nil env should be omitted, got: %#v", entry)
	}
	if _, ok := entry["serverUrl"]; !ok {
		t.Fatalf("extra field serverUrl should be preserved, got: %#v", entry)
	}
}

func singleArtifact(t *testing.T, artifacts []artifact.Artifact) artifact.Artifact {
	t.Helper()
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(artifacts))
	}
	return artifacts[0]
}

func assertGoldenFile(t *testing.T, relativePath string, got []byte) {
	t.Helper()
	path := filepath.Join(relativePath)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
