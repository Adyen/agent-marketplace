package gemini_test

import (
	"encoding/json"
	"testing"
	"testing/fstest"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/claude"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/gemini"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestDetect(t *testing.T) {
	if !gemini.Detect(geminiPluginFS()) {
		t.Fatal("Detect() = false for plugin form, want true")
	}
	if !gemini.Detect(geminiWorkspaceFS()) {
		t.Fatal("Detect() = false for workspace form, want true")
	}
	if gemini.Detect(fstest.MapFS{}) {
		t.Fatal("Detect() = true for empty FS, want false")
	}
}

func TestLoadSkillPreservesSupportFileNames(t *testing.T) {
	src := fstest.MapFS{
		".gemini/skills/review/SKILL.md":          {Data: []byte("---\nname: review\n---\nBody\n")},
		".gemini/skills/review/scripts/run.sh":    {Data: []byte("#!/bin/sh\n")},
		".gemini/skills/review/notes/example.txt": {Data: []byte("note\n")},
	}
	skill, err := gemini.LoadSkill(src, ".gemini/skills/review")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"notes/example.txt", "scripts/run.sh"}
	if len(skill.SupportFiles) != len(want) {
		t.Fatalf("SupportFiles = %v, want %v", skill.SupportFiles, want)
	}
	for i := range want {
		if skill.SupportFiles[i] != want[i] {
			t.Fatalf("SupportFiles = %v, want %v", skill.SupportFiles, want)
		}
	}
}

func TestLoadCommand(t *testing.T) {
	command, diags, err := gemini.LoadCommand(geminiPluginFS(), "commands/review.toml")
	if err != nil {
		t.Fatalf("LoadCommand: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diags)
	}
	if command.Name() != "review" {
		t.Fatalf("Name() = %q, want %q", command.Name(), "review")
	}
	if command.Description != "Review changes" {
		t.Fatalf("Description = %q", command.Description)
	}
	if command.Body != "Summarize `${CLAUDE_PLUGIN_ROOT}` changes.\n" {
		t.Fatalf("Body = %q", command.Body)
	}
}

func TestLoadSubagent(t *testing.T) {
	subagent, diags, err := gemini.LoadSubagent(geminiPluginFS(), ".gemini/agents/reviewer.md")
	if err != nil {
		t.Fatalf("LoadSubagent: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diags)
	}
	if subagent.Name() != "reviewer" {
		t.Fatalf("Name() = %q, want %q", subagent.Name(), "reviewer")
	}
	if subagent.Model != "gemini-3-preview" {
		t.Fatalf("Model = %q", subagent.Model)
	}
	if len(subagent.Tools) != 2 || subagent.Tools[0] != "Read" || subagent.Tools[1] != "Write" {
		t.Fatalf("Tools = %#v", subagent.Tools)
	}
}

func TestLoadCommandPreservesTOMLMetadata(t *testing.T) {
	src := fstest.MapFS{"commands/review.toml": &fstest.MapFile{Data: []byte(`name = "review"
description = "Review changes"
prompt = "check"
triggers = ["review"]
custom = true
`)}}
	command, diags, err := gemini.LoadCommand(src, "commands/review.toml")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diags)
	}
	if command.Ext.Gemini == nil || command.Ext.Gemini.TOMLCommand == nil {
		t.Fatalf("missing Gemini TOML sidecar: %#v", command.Ext.Gemini)
	}
	metadata := command.Ext.Gemini.TOMLCommand
	if !metadata.TriggersSet || len(metadata.Triggers) != 1 || metadata.Triggers[0] != "review" {
		t.Fatalf("TOML triggers = %#v, want present review trigger", metadata)
	}
	if command.Extra["custom"] != true {
		t.Fatalf("Extra[custom] = %#v, want true", command.Extra["custom"])
	}
}

func TestLoadSubagentPreservesGeminiNativeFieldsAndCanonicalRestrictions(t *testing.T) {
	src := fstest.MapFS{".gemini/agents/reviewer.md": &fstest.MapFile{Data: []byte(`---
name: reviewer
kind: ""
temperature: 0
max_turns: 0
timeout_mins: 0
disallowedTools: []
isolation: worktree
mcpServers:
  docs:
    command: "npx"
    args: []
    env: {}
    url: ""
---
body
`)}}
	subagent, _, err := gemini.LoadSubagent(src, ".gemini/agents/reviewer.md")
	if err != nil {
		t.Fatal(err)
	}
	if subagent.DisallowedTools == nil || subagent.Isolation != "worktree" {
		t.Fatalf("canonical restrictions = %#v, %q", subagent.DisallowedTools, subagent.Isolation)
	}
	sidecar := subagent.Ext.Gemini
	if sidecar == nil || !sidecar.KindSet || sidecar.Kind != "" || sidecar.Temperature == nil || *sidecar.Temperature != 0 || sidecar.MaxTurns == nil || *sidecar.MaxTurns != 0 || sidecar.TimeoutMinutes == nil || *sidecar.TimeoutMinutes != 0 {
		t.Fatalf("native sidecar = %#v", sidecar)
	}
	if len(sidecar.InlineMCPServers) != 1 || sidecar.InlineMCPServers["docs"].Command != "npx" {
		t.Fatalf("inline MCP servers = %#v", sidecar.InlineMCPServers)
	}
}

func TestLoadSubagentRejectsMalformedGeminiNativeFields(t *testing.T) {
	for field, value := range map[string]string{
		"temperature":  `"zero"`,
		"max_turns":    `"many"`,
		"timeout_mins": `1.5`,
		"kind":         `[]`,
		"mcpServers":   `[]`,
	} {
		t.Run(field, func(t *testing.T) {
			src := fstest.MapFS{"agent.md": &fstest.MapFile{Data: []byte("---\n" + field + ": " + value + "\n---\nbody\n")}}
			if _, _, err := gemini.LoadSubagent(src, "agent.md"); err == nil {
				t.Fatalf("LoadSubagent(%s) error = nil, want malformed-field error", field)
			}
		})
	}
}

func TestLoadHooksRejectsDuplicateNestedKeys(t *testing.T) {
	src := fstest.MapFS{"settings.json": {Data: []byte(`{"hooks":{"BeforeTool":[{"name":"first","name":"second"}]}}`)}}
	if _, _, err := gemini.LoadHooks(src, "settings.json"); err == nil {
		t.Fatal("LoadHooks accepted duplicate nested JSON key")
	}
}

func TestLoadHooks(t *testing.T) {
	hooks, diags, err := gemini.LoadHooks(geminiPluginFS(), ".gemini/settings.json")
	if err != nil {
		t.Fatalf("LoadHooks: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diags)
	}
	if len(hooks) != 1 {
		t.Fatalf("hooks = %d, want 1", len(hooks))
	}
	if hooks[0].Event != "BeforeTool" || hooks[0].HookSource != "gemini" {
		t.Fatalf("Event = %q HookSource = %q, want native BeforeTool/gemini provenance", hooks[0].Event, hooks[0].HookSource)
	}
	if hooks[0].Command != "${extensionPath}/scripts/check.sh" {
		t.Fatalf("Command = %q", hooks[0].Command)
	}
}

// Native hook ingestion keeps the Gemini event name and records provenance;
// the catalog resolver translates it at compile time.
// spec: hook-resources/gemini-beforeagent-maps-to-userpromptsubmit
func TestLoadHooksKeepsNativeBeforeAgentEvent(t *testing.T) {
	src := fstest.MapFS{
		".gemini/settings.json": &fstest.MapFile{
			Data: []byte(`{
  "hooks": {
    "BeforeAgent": [
      {
        "type": "command",
        "command": "echo hi"
      }
    ]
  }
}`),
		},
	}

	hooks, diags, err := gemini.LoadHooks(src, ".gemini/settings.json")
	if err != nil {
		t.Fatalf("LoadHooks: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("hooks = %d, want 1", len(hooks))
	}
	if hooks[0].Event != "BeforeAgent" || hooks[0].HookSource != "gemini" {
		t.Fatalf("Event = %q HookSource = %q, want native BeforeAgent/gemini provenance", hooks[0].Event, hooks[0].HookSource)
	}
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none for a documented BeforeAgent event", diags)
	}

	result, err := hcc.Compile(hooks[0], hcc.TargetClaude)
	if err != nil {
		t.Fatalf("Compile for Claude Code: %v", err)
	}
	if diag.HasErrors(result.Diagnostics) {
		t.Fatalf("compile diagnostics = %#v", result.Diagnostics)
	}
	matched := false
	for _, art := range result.Artifacts {
		if !art.Fragment {
			continue
		}
		var emitted struct {
			Hooks map[string][]map[string]any `json:"hooks"`
		}
		if err := json.Unmarshal(art.Bytes, &emitted); err != nil {
			t.Fatalf("decode Claude hooks fragment %s: %v", art.Path, err)
		}
		for event, entries := range emitted.Hooks {
			if event != "UserPromptSubmit" {
				t.Fatalf("compiled Claude hook event = %q, want UserPromptSubmit", event)
			}
			for _, entry := range entries {
				if entry["type"] == "command" && entry["command"] == "echo hi" {
					matched = true
				}
			}
		}
	}
	if !matched {
		t.Fatalf("compiled Claude artifacts = %+v, want UserPromptSubmit command hook echo hi", result.Artifacts)
	}
}

func TestLoadHooksPartialMapping(t *testing.T) {
	src := fstest.MapFS{
		".gemini/settings.json": &fstest.MapFile{
			Data: []byte(`{"hooks":{"TotallyUnknownEvent":[{"type":"command","command":"echo hi"}]}}`),
		},
	}
	hooks, diags, err := gemini.LoadHooks(src, ".gemini/settings.json")
	if err != nil {
		t.Fatalf("LoadHooks: %v", err)
	}
	if len(hooks) != 1 || hooks[0].Event != "TotallyUnknownEvent" {
		t.Fatalf("hooks = %#v, want the original event preserved", hooks)
	}
	if hooks[0].HookSource != "gemini" {
		t.Fatalf("HookSource = %q, want gemini provenance", hooks[0].HookSource)
	}
	if len(diags) != 1 || diags[0].Code != diag.CodeIngestGeminiHookEventPartialMapping {
		t.Fatalf("diagnostics = %#v, want one partial-mapping diagnostic", diags)
	}
}

func TestLoadMcpServersRejectsDuplicateNestedKeys(t *testing.T) {
	src := fstest.MapFS{"mcp.json": {Data: []byte(`{"mcpServers":{"server":{"env":{"TOKEN":"first","TOKEN":"second"}}}}`)}}
	if _, _, err := gemini.LoadMcpServers(src, "mcp.json"); err == nil {
		t.Fatal("LoadMcpServers accepted duplicate nested JSON key")
	}
}

func TestLoadMcpServers(t *testing.T) {
	servers, diags, err := gemini.LoadMcpServers(geminiPluginFS(), "gemini-extension.json")
	if err != nil {
		t.Fatalf("LoadMcpServers: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diags)
	}
	if len(servers) != 1 {
		t.Fatalf("servers = %d, want 1", len(servers))
	}
	if servers[0].Name() != "docs" {
		t.Fatalf("Name() = %q, want %q", servers[0].Name(), "docs")
	}
	if servers[0].Command != "${CLAUDE_PLUGIN_ROOT}/bin/server" {
		t.Fatalf("Command = %q", servers[0].Command)
	}
}

func TestLoadBundlePluginForm(t *testing.T) {
	bundle, err := gemini.LoadBundle(geminiPluginFS())
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if bundle.Manifest.Name != "gemini-plugin" {
		t.Fatalf("Manifest.Name = %q, want %q", bundle.Manifest.Name, "gemini-plugin")
	}
	assertKinds(t, bundle.Resources, map[ir.ResourceKind]int{
		ir.KindSkill:     1,
		ir.KindCommand:   1,
		ir.KindSubagent:  1,
		ir.KindHook:      1,
		ir.KindMcpServer: 1,
	})
}

func TestLoadBundleWorkspaceFormSynthesizesManifest(t *testing.T) {
	bundle, err := gemini.LoadBundle(geminiWorkspaceFS())
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	assertKinds(t, bundle.Resources, map[ir.ResourceKind]int{
		ir.KindSkill:     1,
		ir.KindCommand:   1,
		ir.KindSubagent:  1,
		ir.KindHook:      1,
		ir.KindMcpServer: 1,
	})
	if !hasDiag(bundle.Diagnostics, diag.CodeIngestGeminiManifestSynthesized) {
		t.Fatalf("diagnostics = %#v, want synthesized-manifest diagnostic", bundle.Diagnostics)
	}
}

func TestLoadBundleDiscoversAgentsSkillsAlias(t *testing.T) {
	src := fstest.MapFS{
		".agents/skills/portable/SKILL.md": &fstest.MapFile{
			Data: []byte("---\ndescription: Portable skill\n---\nUse `${CLAUDE_PLUGIN_ROOT}` directly.\n"),
		},
	}

	bundle, err := gemini.LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if len(bundle.Resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(bundle.Resources))
	}
	skill, ok := bundle.Resources[0].(*ir.Skill)
	if !ok {
		t.Fatalf("resource type = %T, want *ir.Skill", bundle.Resources[0])
	}
	if skill.Name() != "portable" {
		t.Fatalf("Name() = %q, want portable", skill.Name())
	}
	if skill.Description != "Portable skill" {
		t.Fatalf("Description = %q", skill.Description)
	}
	if skill.Body != "Use `${CLAUDE_PLUGIN_ROOT}` directly.\n" {
		t.Fatalf("Body = %q", skill.Body)
	}
	if skill.Source.File != ".agents/skills/portable/SKILL.md" {
		t.Fatalf("Source.File = %q", skill.Source.File)
	}
}

func TestLoadBundleAgentsSkillsAliasOverridesGeminiSkills(t *testing.T) {
	src := fstest.MapFS{
		".gemini/skills/shared/SKILL.md": &fstest.MapFile{
			Data: []byte("---\ndescription: Gemini skill\n---\nGemini body.\n"),
		},
		".agents/skills/shared/SKILL.md": &fstest.MapFile{
			Data: []byte("---\ndescription: Agents alias skill\n---\nAgents alias body.\n"),
		},
	}

	bundle, err := gemini.LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if len(bundle.Resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(bundle.Resources))
	}
	skill, ok := bundle.Resources[0].(*ir.Skill)
	if !ok {
		t.Fatalf("resource type = %T, want *ir.Skill", bundle.Resources[0])
	}
	if skill.Description != "Agents alias skill" {
		t.Fatalf("Description = %q, want agents alias skill", skill.Description)
	}
	if skill.Body != "Agents alias body.\n" {
		t.Fatalf("Body = %q", skill.Body)
	}
	if skill.Source.File != ".agents/skills/shared/SKILL.md" {
		t.Fatalf("Source.File = %q", skill.Source.File)
	}
}

func TestIngesterRegistered(t *testing.T) {
	ing, ok := ingest.For(ingest.HarnessGemini)
	if !ok {
		t.Fatal("Gemini ingester not registered")
	}
	bundle, err := ing.LoadBundle(geminiPluginFS())
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if len(bundle.Resources) != 5 {
		t.Fatalf("resources = %d, want 5", len(bundle.Resources))
	}
}

func geminiPluginFS() fstest.MapFS {
	return fstest.MapFS{
		"gemini-extension.json": &fstest.MapFile{
			Data: []byte(`{
  "name": "gemini-plugin",
  "description": "Gemini plugin fixture",
  "version": "1.0.0",
  "mcpServers": {
    "docs": {
      "command": "${extensionPath}/bin/server",
      "args": [
        "--root",
        "${extensionPath}/data"
      ],
      "env": {
        "PLUGIN_ROOT": "${extensionPath}"
      }
    }
  }
}`),
		},
		"commands/review.toml": &fstest.MapFile{
			Data: []byte("name = \"review\"\ndescription = \"Review changes\"\nprompt = \"\"\"Summarize `${extensionPath}` changes.\n\"\"\"\n"),
		},
		".gemini/skills/review-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("Review the current changes.\n"),
		},
		".gemini/agents/reviewer.md": &fstest.MapFile{
			Data: []byte("---\nname: reviewer\ndescription: Review code\nkind: local\nmodel: gemini-3-preview\ntools:\n  - Read\n  - Write\n---\nYou are a reviewer.\n"),
		},
		".gemini/settings.json": &fstest.MapFile{
			Data: []byte(`{
  "hooks": {
    "BeforeTool": [
      {
        "type": "command",
        "command": "${extensionPath}/scripts/check.sh"
      }
    ]
  }
}`),
		},
	}
}

func geminiWorkspaceFS() fstest.MapFS {
	src := geminiPluginFS()
	delete(src, "gemini-extension.json")
	src[".gemini/mcp.json"] = &fstest.MapFile{
		Data: []byte(`{
  "mcpServers": {
    "docs": {
      "command": "${extensionPath}/bin/server",
      "args": [
        "--root",
        "${extensionPath}/data"
      ],
      "env": {
        "PLUGIN_ROOT": "${extensionPath}"
      }
    }
  }
}`),
	}
	return src
}

func assertKinds(t *testing.T, resources []ir.Resource, want map[ir.ResourceKind]int) {
	t.Helper()

	got := make(map[ir.ResourceKind]int)
	for _, resource := range resources {
		got[resource.Kind()]++
	}
	if len(got) != len(want) {
		t.Fatalf("resource kind counts = %#v, want %#v", got, want)
	}
	for kind, count := range want {
		if got[kind] != count {
			t.Fatalf("resource kind counts = %#v, want %#v", got, want)
		}
	}
}

func hasDiag(diags []diag.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestLoadBundleManifestExtrasPreserved(t *testing.T) {
	bundle, err := gemini.LoadBundle(geminiPluginFS())
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if bundle.Manifest.Extra == nil {
		return
	}
	raw, err := json.Marshal(bundle.Manifest.Extra)
	if err != nil {
		t.Fatalf("json.Marshal(extra): %v", err)
	}
	if string(raw) != "{}" {
		t.Fatalf("Manifest.Extra = %s, want {}", raw)
	}
}
