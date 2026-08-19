package analyze

import (
	"context"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestEngineAnalyzeSkill(t *testing.T) {
	ctx := context.Background()
	opts := DefaultOptions()
	opts.Policy = PublicPolicy()

	s := ir.NewSkill("review", "Review code", "Review carefully.", []string{"user"}, nil, ir.SourceRef{})
	s.AllowedTools = []string{"read_file"}

	engine := NewEngine()
	report, err := engine.Analyze(ctx, s, nil, opts)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	hasToolGrant := false
	for _, f := range report.Findings {
		if f.Code == FindingToolGrant {
			hasToolGrant = true
		}
	}
	if !hasToolGrant {
		t.Fatalf("expected tool grant finding")
	}
	if report.Rejected {
		t.Fatalf("tool grant should not trigger rejection")
	}
}

func TestEngineAnalyzeRejectsHiddenContent(t *testing.T) {
	ctx := context.Background()
	opts := DefaultOptions()
	opts.Policy = PublicPolicy()

	body := "Review <!-- hidden --> carefully"
	s := ir.NewSkill("review", "Review", body, nil, nil, ir.SourceRef{})

	engine := NewEngine()
	report, err := engine.Analyze(ctx, s, nil, opts)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !report.Rejected {
		t.Fatalf("expected public policy to reject HTML comment")
	}
}

func TestPrivatePolicyDowngradesHiddenContent(t *testing.T) {
	ctx := context.Background()
	opts := DefaultOptions()
	opts.Policy = PrivatePolicy()

	body := "Review <!-- hidden --> carefully"
	s := ir.NewSkill("review", "Review", body, nil, nil, ir.SourceRef{})

	engine := NewEngine()
	report, err := engine.Analyze(ctx, s, nil, opts)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if report.Rejected {
		t.Fatalf("expected private policy to downgrade to warn")
	}
}

func TestEngineAnalyzeMcpServer(t *testing.T) {
	ctx := context.Background()
	opts := DefaultOptions()
	opts.Policy = PublicPolicy()

	m := ir.NewMcpServer("server", "npx", []string{"-y", "@server"}, nil, nil, ir.SourceRef{})

	engine := NewEngine()
	report, err := engine.Analyze(ctx, m, nil, opts)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	hasMcp := false
	for _, f := range report.Findings {
		if f.Code == FindingMcpCapability {
			hasMcp = true
		}
		if f.PolicyOutcome != OutcomeWarn && f.Code == FindingMcpCapability {
			t.Errorf("MCP capability should warn, got %s", f.PolicyOutcome)
		}
	}
	if !hasMcp {
		t.Fatalf("expected MCP capability finding")
	}
}

func TestEngineAnalyzeCollection(t *testing.T) {
	ctx := context.Background()
	opts := DefaultOptions()

	c := ir.NewCollection("defaults", "1.0.0", "Default collection", []ir.CollectionMember{
		{Kind: ir.KindSkill, Name: "review"},
	}, []string{"base"}, nil, ir.SourceRef{})

	engine := NewEngine()
	_, err := engine.Analyze(ctx, c, nil, opts)
	if err != nil {
		t.Fatalf("Analyze collection: %v", err)
	}
}

func TestEngineNilResource(t *testing.T) {
	engine := NewEngine()
	_, err := engine.Analyze(context.Background(), nil, nil, DefaultOptions())
	if err == nil {
		t.Fatal("expected error for nil resource")
	}
}
