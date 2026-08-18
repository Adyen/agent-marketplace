package codex

import (
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestCompilerNativePaths(t *testing.T) {
	tests := []struct {
		name     string
		resource ir.Resource
		wantPath string
		contains []string
	}{
		{name: "skill", resource: ir.NewSkill("demo", "Demo skill", "Do the work.\n", nil, nil, ir.SourceRef{}), wantPath: ".agents/skills/demo/SKILL.md", contains: []string{"name: demo", "description: Demo skill", "Do the work."}},
		{name: "command", resource: ir.NewCommand("deploy", "Deploy safely", "Deploy now.\n", nil, ir.SourceRef{}), wantPath: ".agents/skills/deploy/SKILL.md", contains: []string{"name: deploy", "x-agent-marketplace-kind: command", "Deploy now."}},
		{name: "subagent", resource: ir.NewSubagent("reviewer", "Review changes", "Review like an owner.", "gpt-5.6-terra", nil, "high", nil, "", nil, ir.SourceRef{}), wantPath: ".codex/agents/reviewer.toml", contains: []string{`name = 'reviewer'`, `model = 'gpt-5.6-terra'`, `model_reasoning_effort = 'high'`, `developer_instructions = 'Review like an owner.'`}},
		{name: "hook", resource: ir.NewHookWithMatcher("check", "PreToolUse", "Bash", "./check.sh", "command", nil, ir.SourceRef{}), wantPath: ".codex/hooks.json", contains: []string{`"PreToolUse"`, `"matcher":"Bash"`, `"command":"./check.sh"`}},
		{name: "mcp", resource: ir.NewMcpServer("docs", "npx", []string{"-y", "docs-mcp"}, map[string]string{"TOKEN": "${TOKEN}"}, nil, ir.SourceRef{}), wantPath: ".codex/config.toml", contains: []string{`[mcp_servers.docs]`, `command = 'npx'`, `args = ['-y', 'docs-mcp']`, `[mcp_servers.docs.env]`, `TOKEN = '${TOKEN}'`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compile.Compile(tt.resource, compile.TargetCodex)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Artifacts) != 1 || result.Artifacts[0].Path != tt.wantPath {
				t.Fatalf("artifacts = %#v, want path %q", result.Artifacts, tt.wantPath)
			}
			text := string(result.Artifacts[0].Bytes)
			for _, want := range tt.contains {
				if !strings.Contains(text, want) {
					t.Errorf("artifact missing %q:\n%s", want, text)
				}
			}
		})
	}
}

func TestCompilerSuppressesAuthorityBroadeningLosses(t *testing.T) {
	restrictedSkill := ir.NewSkillWithOptions("restricted", "Restricted", "Body", nil, nil, ir.SourceRef{}, ir.WithSkillAllowedTools([]string{"read"}))
	restrictedAgent := ir.NewSubagent("restricted", "Restricted", "Body", "gpt-5.6-terra", nil, "", []string{"exec"}, "", nil, ir.SourceRef{})
	authenticatedMCP := ir.NewMcpServerWithOptions("private", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"), ir.WithMcpOAuth("client", "secret", "resource"))
	for _, resource := range []ir.Resource{restrictedSkill, restrictedAgent, authenticatedMCP} {
		result, err := compile.Compile(resource, compile.TargetCodex)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Artifacts) != 0 || !diag.HasErrors(result.Diagnostics) {
			t.Errorf("%s result = %#v, want suppressed artifact and error diagnostic", resource.Kind(), result)
		}
	}
}

func TestMCPOutputIsValidTOMLAndPreservesDisabledAndHeaders(t *testing.T) {
	server := ir.NewMcpServerWithOptions("docs", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.test/mcp"), ir.WithMcpHeaders(map[string]string{"X.Header": "value"}), ir.WithMcpDisabled(true))
	result, err := compile.Compile(server, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("result = %#v", result)
	}
	var decoded map[string]any
	if err := toml.Unmarshal(result.Artifacts[0].Bytes, &decoded); err != nil {
		t.Fatalf("invalid TOML: %v\n%s", err, result.Artifacts[0].Bytes)
	}
	text := string(result.Artifacts[0].Bytes)
	for _, want := range []string{"enabled = false", `'X.Header' = 'value'`} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}

func TestUnsupportedRuleAndHookProduceStableDiagnostics(t *testing.T) {
	resources := []ir.Resource{
		ir.NewRule("rule", "", "Body", nil, false, nil, ir.SourceRef{}),
		ir.NewHook("prompt", "PreToolUse", "", "prompt", nil, ir.SourceRef{}),
		ir.NewHook("event", "MadeUpEvent", "./check", "command", nil, ir.SourceRef{}),
	}
	for _, resource := range resources {
		result, err := compile.Compile(resource, compile.TargetCodex)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Artifacts) != 0 || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code == "" {
			t.Errorf("result = %#v, want stable diagnostic and no artifact", result)
		}
	}
}
