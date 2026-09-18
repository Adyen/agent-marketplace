package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestFinalizeResourceAssessmentIsDeterministic(t *testing.T) {
	assessment := validResourceAssessment()
	first, err := store.FinalizeResourceAssessment(assessment)
	if err != nil {
		t.Fatalf("FinalizeResourceAssessment: %v", err)
	}
	second, err := store.FinalizeResourceAssessment(assessment)
	if err != nil {
		t.Fatalf("FinalizeResourceAssessment again: %v", err)
	}

	if first.AssessmentDigest != second.AssessmentDigest {
		t.Fatalf("digest changed between identical inputs: %q != %q", first.AssessmentDigest, second.AssessmentDigest)
	}
	if got := string(first.AssessmentDigest); !isSHA256Digest(got) {
		t.Fatalf("assessment digest = %q, want sha256:<64 lowercase hex>", got)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("finalized assessment is invalid: %v", err)
	}
}

func TestResourceAssessmentIdentityExcludesAssessedAt(t *testing.T) {
	first, err := store.FinalizeResourceAssessment(validResourceAssessment())
	if err != nil {
		t.Fatal(err)
	}
	changed := validResourceAssessment()
	changed.AssessedAt = changed.AssessedAt.Add(24 * time.Hour)
	second, err := store.FinalizeResourceAssessment(changed)
	if err != nil {
		t.Fatal(err)
	}

	if first.AssessmentDigest != second.AssessmentDigest {
		t.Fatalf("AssessedAt affected identity: %q != %q", first.AssessmentDigest, second.AssessmentDigest)
	}
}

func TestResourceAssessmentIdentityIsSensitiveToEveryContentField(t *testing.T) {
	base := validResourceAssessment()
	finalized, err := store.FinalizeResourceAssessment(base)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]func(*store.ResourceAssessment){
		"coordinate": func(a *store.ResourceAssessment) { a.Coordinate.Name = "other" },
		"content digest": func(a *store.ResourceAssessment) {
			a.ContentDigest = store.RevisionDigest(sha256Text("b"))
		},
		"analyzer version": func(a *store.ResourceAssessment) { a.AnalyzerVersion = "analyzer/v2" },
		"policy digest": func(a *store.ResourceAssessment) {
			a.PolicyDigest = store.PolicyDigest(sha256Text("d"))
		},
		"decision": func(a *store.ResourceAssessment) { a.Decision = store.AssessmentDecisionWarn },
		"findings": func(a *store.ResourceAssessment) { a.Findings[0].Message = "changed" },
		"inspection manifest digest": func(a *store.ResourceAssessment) {
			a.InspectionManifestDigest = store.RevisionDigest(sha256Text("e"))
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := base
			candidate.Findings = append([]store.Finding(nil), base.Findings...)
			change(&candidate)
			got, err := store.FinalizeResourceAssessment(candidate)
			if err != nil {
				t.Fatalf("FinalizeResourceAssessment: %v", err)
			}
			if got.AssessmentDigest == finalized.AssessmentDigest {
				t.Fatalf("changing %s did not change assessment identity", name)
			}
		})
	}
}

func TestResourceAssessmentRejectsInvalidDecision(t *testing.T) {
	assessment := validResourceAssessment()
	assessment.Decision = store.AssessmentDecision("approve")
	if _, err := store.FinalizeResourceAssessment(assessment); err == nil {
		t.Fatal("FinalizeResourceAssessment accepted an invalid decision")
	}
}

func TestResourceAssessmentRejectsInvalidAnalyzerVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{"empty", ""},
		{"whitespace prefix", " 1.0.0"},
		{"whitespace suffix", "1.0.0 "},
		{"control character", "1.0.0\x00"},
		{"invalid utf-8", "1.0.0\xC0"},
		{"too long", strings.Repeat("x", 257)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := validResourceAssessment()
			a.AnalyzerVersion = tc.version
			if _, err := store.FinalizeResourceAssessment(a); err == nil {
				t.Fatalf("FinalizeResourceAssessment accepted analyzer version %q", tc.version)
			}
		})
	}
}

func TestResourceAssessmentAcceptsLegacyArbitraryPrintableVersion(t *testing.T) {
	versions := []string{"v1.2.3", "1.0.0-rc1+build.2", "my-analyzer/2025-09-16", "1.0.0-é"}
	for _, version := range versions {
		a := validResourceAssessment()
		a.AnalyzerVersion = version
		a.AssessmentDigest = ""
		if _, err := store.FinalizeResourceAssessment(a); err != nil {
			t.Fatalf("FinalizeResourceAssessment rejected legacy analyzer version %q: %v", version, err)
		}
	}
}

func TestResourceAssessmentRejectsInvalidDigests(t *testing.T) {
	tests := map[string]func(*store.ResourceAssessment){
		"content digest": func(a *store.ResourceAssessment) { a.ContentDigest = "sha256:not-hex" },
		"policy digest":  func(a *store.ResourceAssessment) { a.PolicyDigest = "sha256:not-hex" },
		"inspection manifest digest": func(a *store.ResourceAssessment) {
			a.InspectionManifestDigest = "sha256:not-hex"
		},
	}
	for name, invalidate := range tests {
		t.Run(name, func(t *testing.T) {
			assessment := validResourceAssessment()
			invalidate(&assessment)
			if _, err := store.FinalizeResourceAssessment(assessment); err == nil {
				t.Fatalf("FinalizeResourceAssessment accepted invalid %s", name)
			}
		})
	}

	finalized, err := store.FinalizeResourceAssessment(validResourceAssessment())
	if err != nil {
		t.Fatal(err)
	}
	finalized.AssessmentDigest = "sha256:not-hex"
	if err := finalized.Validate(); err == nil {
		t.Fatal("Validate accepted an invalid assessment digest")
	}
}

func TestResourceAssessmentValidateDetectsIdentityMismatch(t *testing.T) {
	assessment, err := store.FinalizeResourceAssessment(validResourceAssessment())
	if err != nil {
		t.Fatal(err)
	}
	assessment.AnalyzerVersion = "analyzer/v2"
	if err := assessment.Validate(); err == nil {
		t.Fatal("Validate accepted an assessment digest for different content")
	}
}

func TestFinalizeResourceAssessmentRejectsInvalidFinding(t *testing.T) {
	trueVal := true
	tests := []struct {
		name  string
		apply func(*store.ResourceAssessment)
	}{
		{"explicit true consentable", func(a *store.ResourceAssessment) { a.Findings[0].Consentable = &trueVal }},
		{"invalid analyzer id", func(a *store.ResourceAssessment) { a.Findings[0].AnalyzerID = "Analyzer!" }},
		{"oversize finding message", func(a *store.ResourceAssessment) { a.Findings[0].Message = strings.Repeat("x", 5000) }},
		{"control character in message", func(a *store.ResourceAssessment) { a.Findings[0].Message = "bad\x00msg" }},
		{"invalid byte in message", func(a *store.ResourceAssessment) { a.Findings[0].Message = "bad\xC0msg" }},
		{"invalid location path", func(a *store.ResourceAssessment) {
			a.Findings[0].Location = &store.FindingLocation{Path: "bad\x1bpath", Line: 1, Column: 1, StartByte: 0, EndByte: 1}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := validResourceAssessment()
			a.Findings = append([]store.Finding(nil), a.Findings...)
			tc.apply(&a)
			if _, err := store.FinalizeResourceAssessment(a); err == nil {
				t.Fatalf("FinalizeResourceAssessment accepted %s", tc.name)
			}
		})
	}
}

func TestFinalizeResourceAssessmentClonesFindings(t *testing.T) {
	falseVal := false
	a := validResourceAssessment()
	a.Findings = []store.Finding{{
		Code:        "safe",
		Severity:    "info",
		Policy:      "allow",
		Message:     "ok",
		Consentable: &falseVal,
		Location:    &store.FindingLocation{Path: "x", Line: 1, Column: 1, StartByte: 0, EndByte: 1},
	}}
	finalized, err := store.FinalizeResourceAssessment(a)
	if err != nil {
		t.Fatalf("FinalizeResourceAssessment: %v", err)
	}
	digestBefore := finalized.AssessmentDigest
	a.Findings[0].Message = "mutated"
	a.Findings[0].Consentable = nil
	a.Findings[0].Location.Column = 999
	finalized2, err := store.FinalizeResourceAssessment(a)
	if err != nil {
		t.Fatalf("FinalizeResourceAssessment after mutation: %v", err)
	}
	if finalized.AssessmentDigest != digestBefore {
		t.Fatalf("caller finding mutation changed finalized assessment digest")
	}
	if finalized2.AssessmentDigest == finalized.AssessmentDigest {
		t.Fatalf("mutated finding did not produce a different assessment digest")
	}
	if finalized.Findings[0].Message == "mutated" {
		t.Fatalf("finalized finding shares backing data with caller")
	}
	if finalized.Findings[0].Consentable == nil || finalized.Findings[0].Consentable == &falseVal || *finalized.Findings[0].Consentable != false {
		t.Fatalf("finalized finding consentable not deep-cloned and preserved")
	}
	if finalized.Findings[0].Location.Column == 999 {
		t.Fatalf("finalized finding location shares backing data with caller")
	}
}

func validResourceAssessment() store.ResourceAssessment {
	return store.ResourceAssessment{
		Coordinate: store.ResourceCoordinate{
			Marketplace: "marketplace",
			Registry:    "registry",
			Kind:        "skill",
			Name:        "review",
		},
		ContentDigest:   store.RevisionDigest(sha256Text("a")),
		AnalyzerVersion: "analyzer/v1",
		PolicyDigest:    store.PolicyDigest(sha256Text("c")),
		Decision:        store.AssessmentDecisionAllow,
		Findings: []store.Finding{{
			Code:     "safe",
			Severity: "info",
			Policy:   "allow",
			Message:  "No unsafe behavior found.",
		}},
		InspectionManifestDigest: store.RevisionDigest(sha256Text("f")),
		AssessedAt:               time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC),
	}
}

func sha256Text(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}

func isSHA256Digest(digest string) bool {
	if len(digest) != len("sha256:")+64 || !strings.HasPrefix(digest, "sha256:") {
		return false
	}
	for _, c := range digest[len("sha256:"):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
