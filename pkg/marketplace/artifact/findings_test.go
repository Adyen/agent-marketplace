package artifact

import (
	"testing"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
)

func TestToCanonicalFinding(t *testing.T) {
	f := analyze.Finding{
		Code:            analyze.FindingToolGrant,
		Severity:        analyze.OutcomeWarn,
		Message:         "tool grant",
		Analyzer:        "marketplace.analyze",
		AnalyzerVersion: analyze.AnalyzerVersionV1,
		PolicyOutcome:   analyze.OutcomeWarn,
		SourceRange:     &analyze.SourceRange{File: "skill/review", Line: 1, Column: 1},
	}
	cf := ToCanonicalFinding(f)
	if cf.Code != analyze.FindingToolGrant {
		t.Errorf("code = %q", cf.Code)
	}
	if cf.PolicyOutcome != analyze.OutcomeWarn {
		t.Errorf("policy_outcome = %q", cf.PolicyOutcome)
	}
	if cf.File != "skill/review" {
		t.Errorf("file = %q", cf.File)
	}
	_ = hccartifact.CanonicalFinding{}
}

func TestToCanonicalFindingsEmpty(t *testing.T) {
	got := ToCanonicalFindings(nil)
	if got != nil {
		t.Fatalf("expected nil for nil input, got %v", got)
	}
}
