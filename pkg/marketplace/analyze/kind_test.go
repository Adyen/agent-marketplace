package analyze

import (
	"context"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestSkillAnalyzer(t *testing.T) {
	s := ir.NewSkill("review", "Review", "Body", nil, nil, ir.SourceRef{})
	s.AllowedTools = []string{"read"}
	a := skillAnalyzer{}
	report, err := a.Analyze(context.Background(), s, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(report.Findings) == 0 {
		t.Fatal("expected findings")
	}
}

func TestCommandAnalyzer(t *testing.T) {
	c := ir.NewCommand("lint", "Lint", "echo lint", nil, ir.SourceRef{})
	a := commandAnalyzer{}
	report, err := a.Analyze(context.Background(), c, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if report.Rejected {
		t.Fatal("clean command should not reject")
	}
}

func TestSubagentAnalyzer(t *testing.T) {
	s := ir.NewSubagent("planner", "Plan", "body", "sonnet", []string{"read"}, "high", nil, "", nil, ir.SourceRef{})
	a := subagentAnalyzer{}
	report, err := a.Analyze(context.Background(), s, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	hasTool := false
	for _, f := range report.Findings {
		if f.Code == FindingToolGrant {
			hasTool = true
		}
	}
	if !hasTool {
		t.Fatal("expected tool grant finding")
	}
}

func TestHookAnalyzer(t *testing.T) {
	h := ir.NewHook("pre-tool", "PreToolUse", "echo", "command", nil, ir.SourceRef{})
	a := hookAnalyzer{}
	report, err := a.Analyze(context.Background(), h, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	hasHook := false
	for _, f := range report.Findings {
		if f.Code == FindingHookCapability {
			hasHook = true
		}
	}
	if !hasHook {
		t.Fatal("expected hook capability finding")
	}
}

func TestMcpServerAnalyzer(t *testing.T) {
	m := ir.NewMcpServer("server", "npx", []string{"-y"}, nil, nil, ir.SourceRef{})
	a := mcpServerAnalyzer{}
	report, err := a.Analyze(context.Background(), m, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	hasMcp := false
	for _, f := range report.Findings {
		if f.Code == FindingMcpCapability {
			hasMcp = true
		}
	}
	if !hasMcp {
		t.Fatal("expected MCP capability finding")
	}
}

func TestCollectionAnalyzer(t *testing.T) {
	c := ir.NewCollection("defaults", "1.0.0", "Desc", nil, []string{"base"}, nil, ir.SourceRef{})
	a := collectionAnalyzer{}
	report, err := a.Analyze(context.Background(), c, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if report.Rejected {
		t.Fatal("clean collection should not reject")
	}
}
