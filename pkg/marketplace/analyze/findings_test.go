package analyze

import (
	"reflect"
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
	falseVal := false
	f := Finding{
		Code:            FindingHTMLComment,
		Severity:        OutcomeReject,
		Message:         "hidden",
		Analyzer:        "marketplace.analyze",
		AnalyzerVersion: AnalyzerVersionV1,
		PolicyOutcome:   OutcomeReject,
		SourceRange:     &SourceRange{File: "skill/review/body", Line: 1, Column: 1},
		Consentable:     &falseVal,
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
	if cf.Consentable == nil || *cf.Consentable {
		t.Errorf("consentable should be preserved as false")
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

func TestTruncateFindings(t *testing.T) {
	mk := func(code, severity string) Finding {
		return Finding{Code: code, Severity: severity, PolicyOutcome: severity}
	}

	reject1 := mk("reject-1", OutcomeReject)
	reject2 := mk("reject-2", OutcomeReject)
	warn1 := mk("warn-1", OutcomeWarn)
	warn2 := mk("warn-2", OutcomeWarn)
	info1 := mk("info-1", "info")
	info2 := mk("info-2", "info")

	cases := []struct {
		name string
		in   []Finding
		max  int
		want []Finding
	}{
		{"no limit", []Finding{reject1, warn1, info1}, 0, []Finding{reject1, warn1, info1}},
		{"no truncation", []Finding{reject1, warn1, info1}, 10, []Finding{reject1, warn1, info1}},
		{"keep reject drop others", []Finding{warn1, reject1, info1, warn2, info2}, 2, []Finding{reject1, warn1}},
		{"reject exceeds max", []Finding{reject1, reject2}, 1, []Finding{reject1, reject2}},
		{"warn before info", []Finding{warn1, info1, warn2}, 2, []Finding{warn1, warn2}},
		{"info only", []Finding{info1, info2}, 1, []Finding{info1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateFindings(tc.in, tc.max)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("truncateFindings(%v, %d) = %v, want %v", tc.in, tc.max, got, tc.want)
			}
		})
	}
}
