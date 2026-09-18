package marketplace

import (
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestCanonicalFindingsFromRevisionDeepClonesConsentable(t *testing.T) {
	falseVal := false
	trueVal := true
	rev := store.AssessedResourceRevision{
		Assessment: store.ResourceAssessment{
			AnalyzerVersion: "1.0.0",
			Findings: []store.Finding{
				{Code: "a", Severity: "info", Message: "one"},
				{Code: "b", Severity: "warn", Message: "two", Consentable: &falseVal},
				{Code: "c", Severity: "info", Message: "three", Consentable: &trueVal},
			},
		},
	}

	got := canonicalFindingsFromRevision(rev)
	if len(got) != 3 {
		t.Fatalf("got %d findings, want 3", len(got))
	}
	if got[0].Consentable != nil {
		t.Fatalf("nil consentable should stay nil")
	}
	if got[1].Consentable == nil || *got[1].Consentable != false {
		t.Fatalf("false consentable should be preserved")
	}
	if got[1].Consentable == &falseVal {
		t.Fatalf("false consentable should be deep-cloned")
	}
	if got[2].Consentable != nil {
		t.Fatalf("true consentable should normalize to nil")
	}
}

func TestValidateAPIResourceRevisionAuditSummaries(t *testing.T) {
	rev := assessedRevision(store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: "mp", Registry: "registry", Kind: "skill", Name: "review",
		},
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
	})

	// Legacy absent audits are accepted.
	rev.Audits = nil
	if err := validateAPIResourceRevision(rev); err != nil {
		t.Fatalf("legacy absent audits should be accepted: %v", err)
	}

	// Valid summaries with analyzer_id and consentable false are accepted.
	valid := rev
	valid.Audits = []store.ResourceAuditSummary{
		{AnalyzerID: "marketplace.static", AnalyzerVersion: "1.0.0", Role: "gating", Verdict: "pass", Summary: "ok", FindingCount: 0, DurationNS: 1, ResultDigest: store.AuditDigest("sha256:" + strings.Repeat("b", 64))},
		{AnalyzerID: "marketplace.assets", AnalyzerVersion: "1.0.0", Role: "advisory", Verdict: "warn", Summary: "watch", FindingCount: 1, DurationNS: 2, ResultDigest: store.AuditDigest("sha256:" + strings.Repeat("c", 64))},
	}
	if err := validateAPIResourceRevision(valid); err != nil {
		t.Fatalf("valid summaries rejected: %v", err)
	}

	// Malformed summary is rejected.
	malformed := rev
	malformed.Audits = []store.ResourceAuditSummary{
		{AnalyzerID: "bad id!", AnalyzerVersion: "1.0.0", Role: "gating", Verdict: "pass", Summary: "ok", FindingCount: 0, DurationNS: 1, ResultDigest: store.AuditDigest("sha256:" + strings.Repeat("b", 64))},
	}
	if err := validateAPIResourceRevision(malformed); err == nil {
		t.Fatal("malformed audit summary should be rejected")
	}

	// Duplicate analyzer IDs are rejected.
	duplicate := rev
	duplicate.Audits = []store.ResourceAuditSummary{
		{AnalyzerID: "marketplace.static", AnalyzerVersion: "1.0.0", Role: "gating", Verdict: "pass", Summary: "ok", FindingCount: 0, DurationNS: 1, ResultDigest: store.AuditDigest("sha256:" + strings.Repeat("b", 64))},
		{AnalyzerID: "marketplace.static", AnalyzerVersion: "1.0.0", Role: "advisory", Verdict: "warn", Summary: "again", FindingCount: 0, DurationNS: 2, ResultDigest: store.AuditDigest("sha256:" + strings.Repeat("c", 64))},
	}
	if err := validateAPIResourceRevision(duplicate); err == nil {
		t.Fatal("duplicate analyzer ids should be rejected")
	}

	_ = time.Now()
}
