package analyze

import (
	"testing"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func TestSourceRangeAt(t *testing.T) {
	content := "line1\nline2\nline3"
	sr := SourceRangeAt("file", content, 12) // start of 'line3'
	if sr.File != "file" {
		t.Errorf("file = %q", sr.File)
	}
	if sr.Line != 3 {
		t.Errorf("line = %d, want 3", sr.Line)
	}
	if sr.Column != 1 {
		t.Errorf("column = %d, want 1", sr.Column)
	}
}

func TestToCanonicalFinding(t *testing.T) {
	f := Finding{
		Code:            FindingHTMLComment,
		Severity:        OutcomeReject,
		Message:         "hidden",
		Analyzer:        "marketplace.analyze",
		AnalyzerVersion: AnalyzerVersionV1,
		PolicyOutcome:   OutcomeReject,
		SourceRange:     &SourceRange{File: "skill/review/body", Line: 1, Column: 1},
	}
	cf := ToCanonicalFinding(f)
	if cf.Code != FindingHTMLComment {
		t.Errorf("code = %q", cf.Code)
	}
	if cf.PolicyOutcome != OutcomeReject {
		t.Errorf("policy_outcome = %q", cf.PolicyOutcome)
	}
	if cf.File != "skill/review/body" {
		t.Errorf("file = %q", cf.File)
	}
	if cf.AnalyzerVersion != AnalyzerVersionV1 {
		t.Errorf("analyzer_version = %q", cf.AnalyzerVersion)
	}
	_ = hccartifact.CanonicalFinding{}
}

func TestLineColumn(t *testing.T) {
	content := "a\nbcd\nef"
	line, col := lineColumn(content, 3) // index of 'c' on line 2
	if line != 2 || col != 2 {
		t.Fatalf("line=%d col=%d, want 2,2", line, col)
	}
}
