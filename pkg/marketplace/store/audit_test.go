package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestFinalizeResourceAuditIsDeterministic(t *testing.T) {
	audit := validResourceAudit()
	first, err := store.FinalizeResourceAudit(audit)
	if err != nil {
		t.Fatalf("FinalizeResourceAudit: %v", err)
	}
	second, err := store.FinalizeResourceAudit(audit)
	if err != nil {
		t.Fatalf("FinalizeResourceAudit again: %v", err)
	}
	if first.ResultDigest != second.ResultDigest {
		t.Fatalf("digest changed between identical inputs: %q != %q", first.ResultDigest, second.ResultDigest)
	}
	if got := string(first.ResultDigest); !isSHA256Digest(got) {
		t.Fatalf("result digest = %q, want sha256:<64 lowercase hex>", got)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("finalized audit is invalid: %v", err)
	}
}

func TestResourceAuditIdentityExcludesAssessedAt(t *testing.T) {
	first, err := store.FinalizeResourceAudit(validResourceAudit())
	if err != nil {
		t.Fatal(err)
	}
	changed := validResourceAudit()
	changed.AssessedAt = changed.AssessedAt.Add(24 * time.Hour)
	second, err := store.FinalizeResourceAudit(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first.ResultDigest != second.ResultDigest {
		t.Fatalf("AssessedAt affected identity: %q != %q", first.ResultDigest, second.ResultDigest)
	}
}

func TestResourceAuditIdentityIsSensitiveToContent(t *testing.T) {
	base := validResourceAudit()
	finalized, err := store.FinalizeResourceAudit(base)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*store.ResourceAudit){
		"role":    func(a *store.ResourceAudit) { a.Role = "advisory" },
		"verdict": func(a *store.ResourceAudit) { a.Verdict = "warn" },
		"summary": func(a *store.ResourceAudit) { a.Summary = "changed" },
		"findings": func(a *store.ResourceAudit) {
			a.Findings = append([]store.Finding(nil), a.Findings...)
			a.Findings[0].Message = "changed"
		},
		"duration": func(a *store.ResourceAudit) { a.Duration = 2 * time.Second },
		"error": func(a *store.ResourceAudit) {
			a.Verdict = "error"
			a.Error = "changed"
			a.Summary = "audit unavailable"
			a.Findings = nil
		},
		"subject":  func(a *store.ResourceAudit) { a.SubjectDigest = store.RevisionDigest(sha256Text("b")) },
		"analyzer": func(a *store.ResourceAudit) { a.AnalyzerID = "marketplace.other" },
		"version":  func(a *store.ResourceAudit) { a.AnalyzerVersion = "2.0.0" },
		"config":   func(a *store.ResourceAudit) { a.ConfigDigest = store.PolicyDigest(sha256Text("d")) },
		"kind":     func(a *store.ResourceAudit) { a.Coordinate.Name = "other" },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := validResourceAudit()
			change(&candidate)
			got, err := store.FinalizeResourceAudit(candidate)
			if err != nil {
				t.Fatalf("FinalizeResourceAudit: %v", err)
			}
			if got.ResultDigest == finalized.ResultDigest {
				t.Fatalf("changing %s did not change result identity", name)
			}
		})
	}
}

func TestResourceAuditClonesFindingsBeforeReturn(t *testing.T) {
	falseVal := false
	audit := validResourceAudit()
	audit.Findings[0].Consentable = &falseVal
	finalized, err := store.FinalizeResourceAudit(audit)
	if err != nil {
		t.Fatalf("FinalizeResourceAudit: %v", err)
	}
	digestBefore := finalized.ResultDigest
	audit.Findings[0].Message = "mutated"
	audit.Findings[0].Location.Column = 999
	audit.Findings[0].Consentable = nil
	finalized2, err := store.FinalizeResourceAudit(audit)
	if err != nil {
		t.Fatalf("FinalizeResourceAudit after mutation: %v", err)
	}
	if finalized.ResultDigest != digestBefore {
		t.Fatalf("caller finding mutation changed finalized result digest")
	}
	if finalized2.ResultDigest == finalized.ResultDigest {
		t.Fatalf("mutated finding did not produce a different result digest")
	}
	if finalized.Findings[0].Message == "mutated" {
		t.Fatalf("finalized finding share backing array with caller")
	}
	if finalized.Findings[0].Location.Column == 999 {
		t.Fatalf("finalized finding location share backing data with caller")
	}
	if finalized.Findings[0].Consentable == nil || finalized.Findings[0].Consentable == &falseVal || *finalized.Findings[0].Consentable != false {
		t.Fatalf("finalized finding consentable not deep-cloned and preserved")
	}
}

func TestResourceAuditRejectsMismatchedDigest(t *testing.T) {
	audit := validResourceAudit()
	finalized, err := store.FinalizeResourceAudit(audit)
	if err != nil {
		t.Fatal(err)
	}
	mismatch := finalized
	mismatch.ResultDigest = store.AuditDigest("sha256:" + strings.Repeat("1", 64))
	if _, err := store.FinalizeResourceAudit(mismatch); err == nil {
		t.Fatal("FinalizeResourceAudit accepted a mismatched result digest")
	}
}

func TestResourceAuditInvalidFieldMatrix(t *testing.T) {
	base := validResourceAudit()
	valid, err := store.FinalizeResourceAudit(base)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		apply func(a *store.ResourceAudit)
	}{
		{"missing marketplace", func(a *store.ResourceAudit) { a.Coordinate.Marketplace = "" }},
		{"invalid kind", func(a *store.ResourceAudit) { a.Coordinate.Kind = "widget" }},
		{"missing name", func(a *store.ResourceAudit) { a.Coordinate.Name = "" }},
		{"invalid subject digest", func(a *store.ResourceAudit) { a.SubjectDigest = "sha256:bad" }},
		{"invalid config digest", func(a *store.ResourceAudit) { a.ConfigDigest = "sha256:bad" }},
		{"empty analyzer id", func(a *store.ResourceAudit) { a.AnalyzerID = "" }},
		{"unsafe analyzer id", func(a *store.ResourceAudit) { a.AnalyzerID = "Analyzer!" }},
		{"empty analyzer version", func(a *store.ResourceAudit) { a.AnalyzerVersion = "" }},
		{"unsafe analyzer version", func(a *store.ResourceAudit) { a.AnalyzerVersion = "1.0.0\n" }},
		{"invalid role", func(a *store.ResourceAudit) { a.Role = "other" }},
		{"invalid verdict", func(a *store.ResourceAudit) { a.Verdict = "other" }},
		{"negative duration", func(a *store.ResourceAudit) { a.Duration = -1 }},
		{"missing summary", func(a *store.ResourceAudit) { a.Summary = "" }},
		{"missing error for error verdict", func(a *store.ResourceAudit) { a.Verdict = "error"; a.Error = "" }},
		{"error with pass verdict", func(a *store.ResourceAudit) { a.Error = "oops" }},
		{"missing assessed at", func(a *store.ResourceAudit) { a.AssessedAt = time.Time{} }},
		{"finding missing code", func(a *store.ResourceAudit) { a.Findings[0].Code = "" }},
		{"finding missing severity", func(a *store.ResourceAudit) { a.Findings[0].Severity = "" }},
		{"finding invalid location", func(a *store.ResourceAudit) { a.Findings[0].Location.StartByte = -1 }},
		{"finding explicit true consentable", func(a *store.ResourceAudit) {
			trueVal := true
			a.Findings[0].Consentable = &trueVal
		}},
		{"finding invalid analyzer id", func(a *store.ResourceAudit) { a.Findings[0].AnalyzerID = "Analyzer!" }},
		{"finding oversize message", func(a *store.ResourceAudit) { a.Findings[0].Message = strings.Repeat("x", 5000) }},
		{"finding control character message", func(a *store.ResourceAudit) { a.Findings[0].Message = "bad\x00msg" }},
		{"finding invalid byte message", func(a *store.ResourceAudit) { a.Findings[0].Message = "bad\xC0msg" }},
		{"finding invalid location path", func(a *store.ResourceAudit) { a.Findings[0].Location.Path = "bad\x1bpath" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := valid
			tc.apply(&a)
			if _, err := store.FinalizeResourceAudit(a); err == nil {
				t.Fatalf("FinalizeResourceAudit accepted %s", tc.name)
			}
		})
	}
}

func TestResourceAuditValidateDetectsIdentityMismatch(t *testing.T) {
	audit, err := store.FinalizeResourceAudit(validResourceAudit())
	if err != nil {
		t.Fatal(err)
	}
	audit.Verdict = "warn"
	if err := audit.Validate(); err == nil {
		t.Fatal("Validate accepted a result digest for different content")
	}
}

func TestResourceAuditEqualIgnoresAssessedAt(t *testing.T) {
	first, err := store.FinalizeResourceAudit(validResourceAudit())
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.AssessedAt = first.AssessedAt.Add(time.Hour)
	if !first.Equal(second) {
		t.Fatal("Equal considered different for different AssessedAt")
	}
	second.Summary = "changed"
	if first.Equal(second) {
		t.Fatal("Equal ignored a changed field")
	}
}

func TestResourceAuditKeyValidate(t *testing.T) {
	if err := (store.ResourceAuditKey{}).Validate(); err == nil {
		t.Fatal("ResourceAuditKey.Validate accepted a zero key")
	}
	if err := validResourceAudit().ResourceAuditKey.Validate(); err != nil {
		t.Fatalf("ResourceAuditKey.Validate rejected a valid key: %v", err)
	}
}

func TestResourceAuditAcceptsBuildMetadataVersion(t *testing.T) {
	audit := validResourceAudit()
	audit.AnalyzerVersion = "1.0.0+build.1"
	audit.ResultDigest = ""
	if _, err := store.FinalizeResourceAudit(audit); err != nil {
		t.Fatalf("FinalizeResourceAudit rejected build metadata version: %v", err)
	}
}

func TestNewResourceAuditSummaryRejectsInvalidUnderlyingAudit(t *testing.T) {
	valid, err := store.FinalizeResourceAudit(validResourceAudit())
	if err != nil {
		t.Fatal(err)
	}

	// Missing result digest makes the underlying audit invalid.
	invalid := valid
	invalid.ResultDigest = ""
	if _, err := store.NewResourceAuditSummary(invalid); err == nil {
		t.Fatal("NewResourceAuditSummary accepted an invalid underlying audit")
	}

	// A projected summary field that does not match the audit is also rejected.
	mismatched := valid
	mismatched.Summary = strings.Repeat("x", analyze.MaxAuditSummaryBytes+1)
	if _, err := store.NewResourceAuditSummary(mismatched); err == nil {
		t.Fatal("NewResourceAuditSummary accepted an oversized summary")
	}
}

func validResourceAudit() store.ResourceAudit {
	return store.ResourceAudit{
		ResourceAuditKey: store.ResourceAuditKey{
			Coordinate: store.ResourceCoordinate{
				Marketplace: "marketplace",
				Registry:    "registry",
				Kind:        "skill",
				Name:        "review",
			},
			SubjectDigest:   store.RevisionDigest(sha256Text("a")),
			AnalyzerID:      "marketplace.assets",
			AnalyzerVersion: "1.0.0",
			ConfigDigest:    store.PolicyDigest(sha256Text("b")),
		},
		Role:       "gating",
		Verdict:    "pass",
		Summary:    "no concerning findings",
		Findings:   []store.Finding{{Code: "safe", Severity: "info", Policy: "allow", Message: "ok", Location: &store.FindingLocation{Path: "x", Line: 1, Column: 1, StartByte: 0, EndByte: 1}}},
		Duration:   123 * time.Millisecond,
		AssessedAt: time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC),
	}
}
