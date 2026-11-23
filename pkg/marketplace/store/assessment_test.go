package store_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestPlaceResourceAssessmentBindsExactProfile(t *testing.T) {
	a := validResourceAssessment()
	a.Coordinate.Kind = "herdr-app/mod"
	binding := &targetprofile.Binding{
		PackageContentDigest:  string(a.ContentDigest),
		DeliveryMappingDigest: sha256Text("d"),
		ProfileDigest:         sha256Text("e"),
		ProfileIdentity: targetprofile.Identity{
			TargetID:       "herdr-app",
			SourceID:       "builtin:agent-marketplace",
			RevisionDigest: sha256Text("e"),
		},
	}
	a.HostPackageIdentity = binding
	base, err := store.FinalizeResourceAssessment(a)
	if err != nil {
		t.Fatalf("FinalizeResourceAssessment: %v", err)
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("finalized assessment is invalid: %v", err)
	}

	clone := func() store.ResourceAssessment {
		candidate := a
		copy := *binding
		candidate.HostPackageIdentity = &copy
		return candidate
	}
	assertIdentityChange := func(name string, change func(*targetprofile.Binding)) {
		t.Run(name, func(t *testing.T) {
			candidate := clone()
			change(candidate.HostPackageIdentity)
			updated, err := store.FinalizeResourceAssessment(candidate)
			if err != nil {
				t.Fatalf("FinalizeResourceAssessment: %v", err)
			}
			if updated.AssessmentDigest == base.AssessmentDigest {
				t.Fatal("changing exact profile binding did not change assessment identity")
			}
			candidate.AssessmentDigest = base.AssessmentDigest
			if err := candidate.Validate(); err == nil {
				t.Fatal("Validate accepted the stale assessment digest")
			}
		})
	}
	assertIdentityChange("mapping digest", func(binding *targetprofile.Binding) {
		binding.DeliveryMappingDigest = sha256Text("c")
	})
	assertIdentityChange("same revision from another profile source", func(binding *targetprofile.Binding) {
		binding.ProfileIdentity.SourceID = "directory:%2Fprofiles%2Fherdr-app"
	})

	t.Run("missing binding", func(t *testing.T) {
		candidate := clone()
		candidate.HostPackageIdentity = nil
		if _, err := store.FinalizeResourceAssessment(candidate); err == nil {
			t.Fatal("FinalizeResourceAssessment accepted a missing host package identity")
		}
	})
	t.Run("wrong payload digest", func(t *testing.T) {
		candidate := clone()
		candidate.HostPackageIdentity.PackageContentDigest = sha256Text("b")
		if _, err := store.FinalizeResourceAssessment(candidate); err == nil {
			t.Fatal("FinalizeResourceAssessment accepted a binding for different package content")
		}
	})
	t.Run("legacy kind with binding", func(t *testing.T) {
		candidate := clone()
		candidate.Coordinate.Kind = "skill"
		if _, err := store.FinalizeResourceAssessment(candidate); err == nil {
			t.Fatal("FinalizeResourceAssessment accepted a host package identity on a legacy kind")
		}
	})

	binding.DeliveryMappingDigest = sha256Text("b")
	if base.HostPackageIdentity.DeliveryMappingDigest != sha256Text("d") {
		t.Fatal("FinalizeResourceAssessment retained the caller's mutable binding pointer")
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("mutating caller's binding invalidated finalized assessment: %v", err)
	}
}

func TestPlaceAssessmentStorage(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			meta := openCatalogTestStore(t, backend)
			ctx := context.Background()

			a := validResourceAssessment()
			a.Coordinate.Kind = "herdr-app/mod"
			a.HostPackageIdentity = &targetprofile.Binding{
				PackageContentDigest:  string(a.ContentDigest),
				DeliveryMappingDigest: sha256Text("d"),
				ProfileDigest:         sha256Text("e"),
				ProfileIdentity: targetprofile.Identity{
					TargetID:       "herdr-app",
					SourceID:       "builtin:agent-marketplace",
					RevisionDigest: sha256Text("e"),
				},
			}
			manifest := store.RevisionDigest(sha256Text("f"))
			if err := meta.PutResourceRevision(ctx, store.ResourceRevision{
				Coordinate: a.Coordinate, ContentDigest: a.ContentDigest,
			}); err != nil {
				t.Fatalf("PutResourceRevision: %v", err)
			}
			if err := meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
				RegistryID: a.Coordinate.Registry, Ref: "main", Commit: "place-commit",
				RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror,
			}); err != nil {
				t.Fatalf("PutRegistryOccurrence: %v", err)
			}
			if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{
				Coordinate: a.Coordinate, ContentDigest: a.ContentDigest, Commit: "place-commit",
				ManifestDigest: manifest,
			}); err != nil {
				t.Fatalf("PutResourceOccurrence: %v", err)
			}

			a, err := store.FinalizeResourceAssessment(a)
			if err != nil {
				t.Fatalf("FinalizeResourceAssessment: %v", err)
			}
			for i := 0; i < 2; i++ {
				if err := meta.PutResourceAssessment(ctx, a); err != nil {
					t.Fatalf("PutResourceAssessment retry %d: %v", i+1, err)
				}
			}
			if err := meta.SetCurrentResourceAssessment(ctx, a); err != nil {
				t.Fatalf("SetCurrentResourceAssessment: %v", err)
			}

			assertStored := func(t *testing.T, assessment store.ResourceAssessment) {
				t.Helper()
				if assessment.HostPackageIdentity == nil {
					t.Fatal("stored host package identity is nil")
				}
				if !reflect.DeepEqual(*assessment.HostPackageIdentity, *a.HostPackageIdentity) {
					t.Fatalf("stored host package identity = %#v, want %#v", *assessment.HostPackageIdentity, *a.HostPackageIdentity)
				}
				if err := assessment.Validate(); err != nil {
					t.Fatalf("stored assessment is invalid: %v", err)
				}
			}

			got, err := meta.GetResourceAssessment(ctx, a.AssessmentDigest)
			if err != nil {
				t.Fatalf("GetResourceAssessment: %v", err)
			}
			assertStored(t, got)
			listed, err := meta.ListResourceAssessments(ctx, a.Coordinate, a.ContentDigest)
			if err != nil {
				t.Fatalf("ListResourceAssessments: %v", err)
			}
			if len(listed) != 1 {
				t.Fatalf("ListResourceAssessments returned %d records, want 1", len(listed))
			}
			assertStored(t, listed[0])
			revision, err := meta.GetResourceRevision(ctx, a.Coordinate, a.ContentDigest)
			if err != nil {
				t.Fatalf("GetResourceRevision: %v", err)
			}
			assertStored(t, revision.Assessment)
			latest, err := meta.GetLatestResourceRevision(ctx, a.Coordinate)
			if err != nil {
				t.Fatalf("GetLatestResourceRevision: %v", err)
			}
			assertStored(t, latest.Assessment)

			legacy := validResourceAssessment()
			legacy.Coordinate.Name = "legacy-review"
			if err := meta.PutResourceRevision(ctx, store.ResourceRevision{
				Coordinate: legacy.Coordinate, ContentDigest: legacy.ContentDigest,
			}); err != nil {
				t.Fatalf("PutResourceRevision legacy: %v", err)
			}
			if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{
				Coordinate: legacy.Coordinate, ContentDigest: legacy.ContentDigest, Commit: "place-commit",
				ManifestDigest: manifest,
			}); err != nil {
				t.Fatalf("PutResourceOccurrence legacy: %v", err)
			}
			legacy, err = store.FinalizeResourceAssessment(legacy)
			if err != nil {
				t.Fatalf("FinalizeResourceAssessment legacy: %v", err)
			}
			if err := meta.PutResourceAssessment(ctx, legacy); err != nil {
				t.Fatalf("PutResourceAssessment legacy: %v", err)
			}
			storedLegacy, err := meta.GetResourceAssessment(ctx, legacy.AssessmentDigest)
			if err != nil {
				t.Fatalf("GetResourceAssessment legacy: %v", err)
			}
			if storedLegacy.HostPackageIdentity != nil {
				t.Fatalf("legacy host package identity = %#v, want nil", storedLegacy.HostPackageIdentity)
			}
			if err := storedLegacy.Validate(); err != nil {
				t.Fatalf("stored legacy assessment is invalid: %v", err)
			}
		})
	}
}

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
