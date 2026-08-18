package codex

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
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
		{name: "subagent", resource: ir.NewSubagent("reviewer", "Review changes", "Review like an owner.", "gpt-5.6-terra", nil, "high", nil, "", nil, ir.SourceRef{}), wantPath: ".codex/agents/reviewer.toml", contains: []string{`name = "reviewer"`, `model = "gpt-5.6-terra"`, `model_reasoning_effort = "high"`, `developer_instructions = "Review like an owner."`}},
		{name: "hook", resource: ir.NewHookWithMatcher("check", "PreToolUse", "Bash", "./check.sh", "command", nil, ir.SourceRef{}), wantPath: ".codex/hooks.json", contains: []string{`"PreToolUse"`, `"matcher":"Bash"`, `"command":"./check.sh"`}},
		{name: "mcp", resource: ir.NewMcpServer("docs", "npx", []string{"-y", "docs-mcp"}, map[string]string{"TOKEN": "${TOKEN}"}, nil, ir.SourceRef{}), wantPath: ".codex/config.toml", contains: []string{`[mcp_servers."docs"]`, `command = "npx"`, `args = ["-y", "docs-mcp"]`, `[mcp_servers."docs".env]`, `TOKEN = "${TOKEN}"`}},
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
