package gemini

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestGeminiRule_NoPrimitiveDiagnostic(t *testing.T) {
	rule := ir.NewRule("typescript", "TS", "body\n", nil, false, nil, ir.SourceRef{File: "rules/typescript.md", Line: 1})

	arts, diags, err := compiler{}.Translate(rule)
	if err != nil {
		t.Fatalf("Translate(rule) error: %v", err)
	}
	if len(arts) != 0 {
		t.Fatalf("expected no artifacts for Gemini rule, got %v", arts)
	}
	if len(diags) != 1 || diags[0].Code != diag.CodeRuleGeminiNoPrimitive {
		t.Fatalf("expected single %q diagnostic, got %v", diag.CodeRuleGeminiNoPrimitive, diags)
	}
	if diags[0].Severity != diag.SeverityWarning {
		t.Errorf("expected warning severity, got %v", diags[0].Severity)
	}
}
