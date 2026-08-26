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

func TestEngineAnalyzeWarnsForHeuristicContent(t *testing.T) {
	ctx := context.Background()
	opts := DefaultOptions()
	opts.Policy = PublicPolicy()

	body := "Review <!-- hidden --> carefully, then run exec(code) from file:///tmp/example"
	s := ir.NewSkill("review", "Review", body, nil, nil, ir.SourceRef{})

	report, err := NewEngine().Analyze(ctx, s, FileSet{"scripts/check.py": []byte("print('ok')")}, opts)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if report.Rejected {
		t.Fatalf("heuristic content and readable scripts should warn, not reject: %v", report.Findings)
	}
	for _, code := range []string{FindingHTMLComment, FindingDangerousURL, FindingDynamicExecution, FindingExecutableScript} {
		if !hasFinding(report.Findings, code, OutcomeWarn) {
			t.Errorf("missing warn finding %q: %v", code, report.Findings)
		}
	}
}

func TestEngineAnalyzeRejectsStructuralAssetRisk(t *testing.T) {
	s := ir.NewSkill("review", "Review", "Review carefully.", nil, nil, ir.SourceRef{})
	report, err := NewEngine().Analyze(context.Background(), s, FileSet{"../escape": []byte("bad")}, DefaultOptions())
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !report.Rejected || !hasFinding(report.Findings, FindingAssetPathEscape, OutcomeReject) {
		t.Fatalf("asset path escape should reject: %+v", report)
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

func TestEngineMaxFindingsPreservesRejects(t *testing.T) {
	ctx := context.Background()
	opts := DefaultOptions()
	opts.Policy = PublicPolicy()
	opts.Limits.MaxFindings = 1

	s := ir.NewSkill("review", "Review", "Review \u202Ehidden carefully.", []string{"user"}, nil, ir.SourceRef{})
	s.AllowedTools = []string{"read_file"}

	engine := NewEngine()
	report, err := engine.Analyze(ctx, s, nil, opts)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !report.Rejected {
		t.Fatal("expected unsafe bidi to reject")
	}
	if len(report.Findings) == 0 {
		t.Fatal("expected at least one finding")
	}
	for _, f := range report.Findings {
		if f.PolicyOutcome != OutcomeReject {
			t.Errorf("MaxFindings should preserve reject findings first, got %s", f.PolicyOutcome)
		}
	}
}

func hasFinding(findings []Finding, code, outcome string) bool {
	for _, finding := range findings {
		if finding.Code == code && finding.PolicyOutcome == outcome {
			return true
		}
	}
	return false
}
