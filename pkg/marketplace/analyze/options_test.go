package analyze

import (
	"context"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestCanonicalDigestFormat(t *testing.T) {
	validHex := strings.Repeat("0", 64)
	cases := []struct {
		digest string
		wantOK bool
	}{
		{"sha256:" + validHex, true},
		{validHex, false},
		{"SHA256:" + validHex, false},
		{" sha256:" + validHex, false},
		{"sha256:" + validHex + "0", false},
		{"sha256:" + strings.Repeat("0", 63), false},
		{"md5:" + strings.Repeat("0", 32), false},
		{"sha256:" + strings.Repeat("A", 64), false},
		{"sha256:" + strings.Repeat("g", 64), false},
	}

	for _, tc := range cases {
		_, err := canonicalDigest(tc.digest)
		if tc.wantOK && err != nil {
			t.Errorf("canonicalDigest(%q) unexpected error: %v", tc.digest, err)
		}
		if !tc.wantOK && err == nil {
			t.Errorf("canonicalDigest(%q) should have been rejected", tc.digest)
		}
	}
}

func TestRegistrationValidation(t *testing.T) {
	if _, err := NewEngineWith(nil).Analyze(context.Background(), Subject{Resource: &fakeResource{name: "x"}, Limits: DefaultLimits()}); err != nil {
		t.Fatalf("empty engine should not error: %v", err)
	}

	_, err := NewEngineWith([]Registration{{Analyzer: &fakeResourceAnalyzer{}, Role: "owner"}}).Analyze(context.Background(), Subject{Resource: &fakeResource{name: "x"}, Limits: DefaultLimits()})
	if err == nil || !strings.Contains(err.Error(), "invalid role") {
		t.Fatalf("expected invalid role error, got %v", err)
	}

	_, err = NewEngineWith([]Registration{{Role: RoleGating}}).Analyze(context.Background(), Subject{Resource: &fakeResource{name: "x"}, Limits: DefaultLimits()})
	if err == nil || !strings.Contains(err.Error(), "nil analyzer") {
		t.Fatalf("expected nil analyzer error, got %v", err)
	}
}

func TestValidateSubjectBounds(t *testing.T) {
	s := &Subject{
		Resource: &fakeResource{kind: "skill", name: "x"},
		samples: []textSample{
			{path: "body", content: strings.Repeat("x", 10)},
		},
		Files:  nil,
		Limits: Limits{MaxFileBytes: 10, MaxTotalBytes: 10},
	}
	if err := validateSubject(s); err != nil {
		t.Fatalf("exact bound should pass: %v", err)
	}

	s.samples[0].content = strings.Repeat("x", 11)
	if err := validateSubject(s); err == nil || !strings.Contains(err.Error(), "MaxFileBytes") {
		t.Fatalf("expected MaxFileBytes error for one over, got %v", err)
	}

	s.samples[0].content = strings.Repeat("x", 10)
	s.Files = FileSet{"support.txt": []byte(strings.Repeat("x", 10))}
	s.Limits = Limits{MaxFileBytes: 100, MaxTotalBytes: 20}
	if err := validateSubject(s); err != nil {
		t.Fatalf("exact total bound should pass: %v", err)
	}

	s.Files["support.txt"] = []byte(strings.Repeat("x", 11))
	if err := validateSubject(s); err == nil || !strings.Contains(err.Error(), "MaxTotalBytes") {
		t.Fatalf("expected MaxTotalBytes error for one over, got %v", err)
	}
}

type fakeResourceAnalyzer struct{}

func (fakeResourceAnalyzer) ID() string               { return "fake" }
func (fakeResourceAnalyzer) Version() string          { return "1.0.0" }
func (fakeResourceAnalyzer) Kinds() []ir.ResourceKind { return nil }
func (fakeResourceAnalyzer) Analyze(context.Context, Subject, AnalyzerConfig) (Audit, error) {
	return Audit{Verdict: VerdictPass}, nil
}
