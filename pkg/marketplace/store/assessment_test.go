package store_test

import (
	"reflect"
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

func TestResourceAssessmentValidationGolden(t *testing.T) {
	populated, err := store.FinalizeResourceAssessment(validResourceAssessment())
	if err != nil {
		t.Fatal(err)
	}
	const populatedDigest = store.AssessmentDigest("sha256:2e7c339d14f903ba227bc73206d33e4c4e781971477f61d294805840546f83f9")
	if populated.AssessmentDigest != populatedDigest {
		t.Fatalf("populated digest = %q, want %q", populated.AssessmentDigest, populatedDigest)
	}
	if err := populated.Validate(); err != nil {
		t.Fatalf("populated Validate: %v", err)
	}

	emptyNil := validResourceAssessment()
	emptyNil.Findings = nil
	finalizedNil, err := store.FinalizeResourceAssessment(emptyNil)
	if err != nil {
		t.Fatal(err)
	}
	const emptyDigest = store.AssessmentDigest("sha256:6be64d613f910e1c4c280c9b99e192bf4f7d79f3748f3abef917ee5196d42c7c")
	if finalizedNil.AssessmentDigest != emptyDigest {
		t.Fatalf("empty digest = %q, want %q", finalizedNil.AssessmentDigest, emptyDigest)
	}
	emptySlice := validResourceAssessment()
	emptySlice.Findings = []store.Finding{}
	finalizedSlice, err := store.FinalizeResourceAssessment(emptySlice)
	if err != nil {
		t.Fatal(err)
	}
	if finalizedSlice.AssessmentDigest != finalizedNil.AssessmentDigest {
		t.Fatalf("nil and empty findings digest differ: %q != %q", finalizedSlice.AssessmentDigest, finalizedNil.AssessmentDigest)
	}
	for _, assessment := range []store.ResourceAssessment{finalizedNil, finalizedSlice} {
		if err := assessment.Validate(); err != nil {
			t.Fatalf("empty findings Validate: %v", err)
		}
	}

	rawNil := validResourceAssessment()
	rawNil.Findings = nil
	rawNil.AssessmentDigest = emptyDigest
	if err := rawNil.Validate(); err != nil {
		t.Fatalf("raw nil findings Validate: %v", err)
	}
	if rawNil.Findings != nil {
		t.Fatal("Validate changed nil findings")
	}
	rawEmpty := validResourceAssessment()
	rawEmpty.Findings = []store.Finding{}
	rawEmpty.AssessmentDigest = emptyDigest
	if err := rawEmpty.Validate(); err != nil {
		t.Fatalf("raw empty findings Validate: %v", err)
	}
	if rawEmpty.Findings == nil {
		t.Fatal("Validate changed empty findings")
	}

	tampered := populated
	tampered.AssessmentDigest = emptyDigest
	if err := tampered.Validate(); err == nil {
		t.Fatal("Validate accepted a digest computed for different findings")
	}
}

func TestResourceAssessmentValidationPreservesInput(t *testing.T) {
	assessment := validResourceAssessment()
	assessment.Findings[0].Location = &store.FindingLocation{Path: "resource/skill.json", Line: 2, Column: 1, StartByte: 3, EndByte: 9}
	finalized, err := store.FinalizeResourceAssessment(assessment)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := finalized
	snapshot.Findings = append([]store.Finding(nil), finalized.Findings...)
	for i := range snapshot.Findings {
		if snapshot.Findings[i].Location != nil {
			location := *snapshot.Findings[i].Location
			snapshot.Findings[i].Location = &location
		}
	}
	if err := finalized.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !reflect.DeepEqual(finalized, snapshot) {
		t.Fatalf("Validate mutated its input: %+v != %+v", finalized, snapshot)
	}

	caller := validResourceAssessment()
	caller.Findings[0].Location = &store.FindingLocation{Path: "resource/skill.json", Line: 2}
	final, err := store.FinalizeResourceAssessment(caller)
	if err != nil {
		t.Fatal(err)
	}
	caller.Findings[0].Message = "mutated after finalize"
	caller.Findings[0].Location.Path = "mutated/path.json"
	if final.Findings[0].Message == "mutated after finalize" || final.Findings[0].Location.Path == "mutated/path.json" {
		t.Fatal("Finalize retained caller-owned findings or location memory")
	}

	badLocation := finalized
	badLocation.Findings = append([]store.Finding(nil), finalized.Findings...)
	badLocation.Findings[0].Location = &store.FindingLocation{}
	if err := badLocation.Validate(); err == nil {
		t.Fatal("Validate accepted a malformed finding location")
	}
	zeroTime := finalized
	zeroTime.AssessedAt = time.Time{}
	if err := zeroTime.Validate(); err == nil {
		t.Fatal("Validate accepted a zero assessed_at")
	}
}
