package artifact

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func benchmarkDecodeResource(b *testing.B, r ir.Resource) {
	b.Helper()
	a, err := FromIR(r)
	if err != nil {
		b.Fatalf("FromIR: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := DecodeResource(a.Manifest.Kind, a.Payload)
		if err != nil {
			b.Fatalf("DecodeResource: %v", err)
		}
	}
}

func BenchmarkDecodeResourceSkill(b *testing.B) {
	benchmarkDecodeResource(b, ir.NewSkillWithOptions(
		"review",
		"Review code",
		"Review the code carefully.",
		[]string{"user", "model"},
		map[string]any{"category": "review"},
		ir.SourceRef{File: "skills/review.md", Line: 12},
		ir.WithSkillArgumentHint("<path>"),
		ir.WithSkillModel("sonnet"),
		ir.WithSkillAllowedTools([]string{"@read-file"}),
		ir.WithSkillPermissions(ir.Permissions{Allow: []string{"@read-file"}}),
	))
}

func BenchmarkDecodeResourceCommand(b *testing.B) {
	benchmarkDecodeResource(b, ir.NewCommand(
		"lint",
		"Run linter",
		"golangci-lint run ./...",
		map[string]any{"shell": "bash"},
		ir.SourceRef{File: "commands/lint.md", Line: 5},
	))
}

func BenchmarkDecodeResourceSubagent(b *testing.B) {
	benchmarkDecodeResource(b, ir.NewSubagentWithOptions(
		"planner",
		"Plan tasks",
		"Break the request into steps.",
		"sonnet",
		[]string{"@plan"},
		"high",
		nil,
		"worktree",
		map[string]any{"owner": "platform"},
		ir.SourceRef{File: "subagents/planner.md", Line: 8},
		ir.WithSubagentMaxNesting(3),
	))
}

func BenchmarkDecodeResourceHook(b *testing.B) {
	benchmarkDecodeResource(b, ir.NewHookWithOptions(
		"pre-tool",
		"PreToolUse",
		"echo pre-tool",
		"command",
		map[string]any{"priority": float64(1)},
		ir.SourceRef{File: "hooks/pre-tool.md", Line: 3},
		ir.WithHookPrompt("Review the tool call."),
		ir.WithHookTimeout(30),
	))
}

func BenchmarkDecodeResourceMcpServer(b *testing.B) {
	benchmarkDecodeResource(b, ir.NewMcpServerWithOptions(
		"server",
		"npx",
		[]string{"-y", "@server/mcp"},
		map[string]string{"API_KEY": "${API_KEY}"},
		map[string]any{"transport": "stdio"},
		ir.SourceRef{File: "mcps/server.md", Line: 7},
		ir.WithMcpURL("https://example.com/mcp"),
		ir.WithMcpTransport("sse"),
	))
}
