package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const targetProfileSourceID = "git:https%3A%2F%2Fgit.example.com%2Ftools@main#profiles%2Finvented-app.yaml"

type targetProfileAcquisitionResponse struct {
	revision     store.AssessedResourceRevision
	availability string
	manifest     []byte
	body         []byte
}

type targetProfileAcquisitionCounts struct {
	revisions   atomic.Int32
	inspections atomic.Int32
	bodies      atomic.Int32
}

type targetProfileAcquisitionSnapshot struct {
	revisions   int32
	inspections int32
	bodies      int32
}

func TestAcquireTargetProfileVerifiesExactEvidence(t *testing.T) {
	profileBytes := []byte(placeAcquireProfileYAML)
	identity := placeAcquireProfileIdentity(profileBytes)
	identity.SourceID = targetProfileSourceID
	if err := identity.Validate(); err != nil {
		t.Fatalf("validate profile identity: %v", err)
	}
	coordinate := store.ResourceCoordinate{
		Marketplace: "mp", Registry: "reg", Kind: "target-profile", Name: identity.TargetID,
	}
	base, err := buildTargetProfileAcquisitionResponse(t, coordinate, identity, profileBytes)
	if err != nil {
		t.Fatalf("build profile response: %v", err)
	}

	var current atomic.Pointer[targetProfileAcquisitionResponse]
	current.Store(base)
	var counts targetProfileAcquisitionCounts
	revisionPath := "/api/v1/resources/reg/target-profile/invented-app/revisions/" + string(identity.RevisionDigest)
	manifestPath := revisionPath + "/inspection"
	bodyPath := manifestPath + "/content"
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("X-Agent-Marketplace-Features")
		if len(values) != 1 || values[0] != mpapi.TargetProfilePlaceFeature {
			t.Errorf("feature headers = %q, want exactly %q", values, mpapi.TargetProfilePlaceFeature)
		}
		response := current.Load()
		switch r.URL.Path {
		case revisionPath:
			counts.revisions.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				store.AssessedResourceRevision
				CurrentAvailability struct {
					Availability string `json:"availability"`
				} `json:"current_availability"`
			}{
				AssessedResourceRevision: response.revision,
				CurrentAvailability: struct {
					Availability string `json:"availability"`
				}{Availability: response.availability},
			})
		case manifestPath:
			counts.inspections.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(response.manifest)
		case bodyPath:
			counts.bodies.Add(1)
			w.Header().Set("Content-Type", inspection.MediaYAML)
			_, _ = w.Write(response.body)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer peer.Close()
	client, err := NewClient(peer.URL)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name         string
		expected     *targetprofile.Identity
		availability string
		decision     store.AssessmentDecision
	}{
		{name: "explicit digest bootstrap", availability: "available", decision: store.AssessmentDecisionAllow},
		{name: "locked identity and operator exposure", expected: &identity, availability: "operator-exposed", decision: store.AssessmentDecisionReject},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := cloneTargetProfileAcquisitionResponse(base)
			response.availability = test.availability
			if test.decision != store.AssessmentDecisionAllow {
				response.revision = finalizeTargetProfileDecision(t, response.revision, test.decision)
			}
			current.Store(response)
			before := counts.snapshot()
			got, err := client.AcquireTargetProfile(context.Background(), coordinate, store.RevisionDigest(identity.RevisionDigest), test.expected)
			if err != nil {
				t.Fatalf("AcquireTargetProfile: %v", err)
			}
			wantRevision := response.revision
			wantRevision.EffectiveAvailability = test.availability
			wantRevision.Assessment.Findings = append([]store.Finding(nil), response.revision.Assessment.Findings...)
			if !reflect.DeepEqual(got.Revision, wantRevision) {
				t.Fatalf("revision = %+v, want %+v", got.Revision, wantRevision)
			}
			if got.Identity != identity || !bytes.Equal(got.Manifest, base.manifest) ||
				!bytes.Equal(got.Bytes, profileBytes) || got.Revision.ArchiveDigest != "" {
				t.Fatalf("evidence = %+v, want exact profile identity, manifest and bytes with no archive", got)
			}
			counts.assertDelta(t, before, 1, 1, 1)
		})
	}

	t.Run("same digest with different valid source stops before body", func(t *testing.T) {
		expected := identity
		expected.SourceID = "git:https%3A%2F%2Fgit.example.com%2Fother@main#profiles%2Finvented-app.yaml"
		if err := expected.Validate(); err != nil {
			t.Fatalf("validate alternate identity: %v", err)
		}
		current.Store(cloneTargetProfileAcquisitionResponse(base))
		before := counts.snapshot()
		got, err := client.AcquireTargetProfile(context.Background(), coordinate, store.RevisionDigest(identity.RevisionDigest), &expected)
		assertTargetProfileAcquisitionFailure(t, got, err, "marketplace.profile.revision-mismatch")
		counts.assertDelta(t, before, 1, 1, 0)
	})

	t.Run("modified manifest stops before body", func(t *testing.T) {
		response := cloneTargetProfileAcquisitionResponse(base)
		response.manifest = append(response.manifest, ' ')
		current.Store(response)
		before := counts.snapshot()
		got, err := client.AcquireTargetProfile(context.Background(), coordinate, store.RevisionDigest(identity.RevisionDigest), nil)
		assertTargetProfileAcquisitionFailure(t, got, err, "")
		counts.assertDelta(t, before, 1, 1, 0)
	})

	t.Run("changed manifest assessment digest stops before body", func(t *testing.T) {
		response := cloneTargetProfileAcquisitionResponse(base)
		assessment := response.revision.Assessment
		assessment.InspectionManifestDigest = store.RevisionDigest("sha256:" + strings.Repeat("f", 64))
		assessment.AssessmentDigest = ""
		finalized, err := store.FinalizeResourceAssessment(assessment)
		if err != nil {
			t.Fatalf("finalize changed assessment: %v", err)
		}
		response.revision.Assessment = finalized
		current.Store(response)
		before := counts.snapshot()
		got, err := client.AcquireTargetProfile(context.Background(), coordinate, store.RevisionDigest(identity.RevisionDigest), nil)
		assertTargetProfileAcquisitionFailure(t, got, err, "")
		counts.assertDelta(t, before, 1, 1, 0)
	})

	t.Run("modified raw profile body fails after body request", func(t *testing.T) {
		response := cloneTargetProfileAcquisitionResponse(base)
		response.body = bytes.Replace(response.body, []byte("Invented App"), []byte("Invented Bob"), 1)
		current.Store(response)
		before := counts.snapshot()
		got, err := client.AcquireTargetProfile(context.Background(), coordinate, store.RevisionDigest(identity.RevisionDigest), nil)
		assertTargetProfileAcquisitionFailure(t, got, err, "content digest does not match manifest")
		counts.assertDelta(t, before, 1, 1, 1)
	})

	t.Run("unavailable revision stops before inspection", func(t *testing.T) {
		response := cloneTargetProfileAcquisitionResponse(base)
		response.availability = "unavailable"
		current.Store(response)
		before := counts.snapshot()
		got, err := client.AcquireTargetProfile(context.Background(), coordinate, store.RevisionDigest(identity.RevisionDigest), nil)
		assertTargetProfileAcquisitionFailure(t, got, err, "")
		counts.assertDelta(t, before, 1, 0, 0)
	})

	t.Run("revoked revision stops before inspection", func(t *testing.T) {
		response := cloneTargetProfileAcquisitionResponse(base)
		response.revision.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: identity.TargetID, Reason: "withdrawn"}
		current.Store(response)
		before := counts.snapshot()
		got, err := client.AcquireTargetProfile(context.Background(), coordinate, store.RevisionDigest(identity.RevisionDigest), nil)
		assertTargetProfileAcquisitionFailure(t, got, err, "")
		counts.assertDelta(t, before, 1, 0, 0)
	})

	t.Run("rejected assessment stops before inspection", func(t *testing.T) {
		response := cloneTargetProfileAcquisitionResponse(base)
		response.revision = finalizeTargetProfileDecision(t, response.revision, store.AssessmentDecisionReject)
		response.availability = "available"
		current.Store(response)
		before := counts.snapshot()
		got, err := client.AcquireTargetProfile(context.Background(), coordinate, store.RevisionDigest(identity.RevisionDigest), nil)
		assertTargetProfileAcquisitionFailure(t, got, err, "")
		counts.assertDelta(t, before, 1, 0, 0)
	})

	t.Run("invalid input and changed locked digest stop before HTTP", func(t *testing.T) {
		invalidIdentity := identity
		invalidIdentity.SourceID = "not-a-source"
		otherTarget := identity
		otherTarget.TargetID = "other-app"
		differentDigest := identity
		differentDigest.RevisionDigest = "sha256:" + strings.Repeat("f", 64)
		badMarketplace := coordinate
		badMarketplace.Marketplace = "bad/marketplace"
		badRegistry := coordinate
		badRegistry.Registry = "bad/registry"
		wrongKind := coordinate
		wrongKind.Kind = "skill"
		badTarget := coordinate
		badTarget.Name = "Invented-App"
		for _, test := range []struct {
			name       string
			coordinate store.ResourceCoordinate
			revision   store.RevisionDigest
			expected   *targetprofile.Identity
		}{
			{name: "marketplace segment", coordinate: badMarketplace, revision: store.RevisionDigest(identity.RevisionDigest)},
			{name: "registry segment", coordinate: badRegistry, revision: store.RevisionDigest(identity.RevisionDigest)},
			{name: "wrong kind", coordinate: wrongKind, revision: store.RevisionDigest(identity.RevisionDigest)},
			{name: "malformed target", coordinate: badTarget, revision: store.RevisionDigest(identity.RevisionDigest)},
			{name: "malformed revision", coordinate: coordinate, revision: "bad"},
			{name: "invalid expected identity", coordinate: coordinate, revision: store.RevisionDigest(identity.RevisionDigest), expected: &invalidIdentity},
			{name: "expected target differs", coordinate: coordinate, revision: store.RevisionDigest(identity.RevisionDigest), expected: &otherTarget},
			{name: "locked profile digest differs", coordinate: coordinate, revision: store.RevisionDigest(identity.RevisionDigest), expected: &differentDigest},
		} {
			t.Run(test.name, func(t *testing.T) {
				before := counts.snapshot()
				got, err := client.AcquireTargetProfile(context.Background(), test.coordinate, test.revision, test.expected)
				assertTargetProfileAcquisitionFailure(t, got, err, "")
				counts.assertDelta(t, before, 0, 0, 0)
			})
		}
	})
}

func buildTargetProfileAcquisitionResponse(t *testing.T, coordinate store.ResourceCoordinate, identity targetprofile.Identity, body []byte) (*targetProfileAcquisitionResponse, error) {
	t.Helper()
	projection, err := inspection.BuildTargetProfile(coordinate, identity, body, 1, targetprofile.MaxProfileBytes, targetprofile.MaxProfileBytes)
	if err != nil {
		return nil, err
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coordinate, ContentDigest: store.RevisionDigest(identity.RevisionDigest),
		AnalyzerVersion: "test/v1", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("a", 64)),
		Decision: store.AssessmentDecisionAllow, InspectionManifestDigest: store.RevisionDigest(inspection.Digest(projection.Payload)),
		AssessedAt: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		return nil, err
	}
	revision := store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{Coordinate: coordinate, ContentDigest: store.RevisionDigest(identity.RevisionDigest)},
		Assessment:       assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate: coordinate, ContentDigest: store.RevisionDigest(identity.RevisionDigest),
			Commit: strings.Repeat("e", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
		},
	}
	return &targetProfileAcquisitionResponse{
		revision: revision, availability: "available",
		manifest: append([]byte(nil), projection.Payload...),
		body:     append([]byte(nil), projection.Bodies[projection.Manifest.Files[0].ContentDigest]...),
	}, nil
}

func cloneTargetProfileAcquisitionResponse(response *targetProfileAcquisitionResponse) *targetProfileAcquisitionResponse {
	clone := *response
	clone.manifest = append([]byte(nil), response.manifest...)
	clone.body = append([]byte(nil), response.body...)
	return &clone
}

func finalizeTargetProfileDecision(t *testing.T, revision store.AssessedResourceRevision, decision store.AssessmentDecision) store.AssessedResourceRevision {
	t.Helper()
	assessment := revision.Assessment
	assessment.Decision = decision
	assessment.AssessmentDigest = ""
	if decision == store.AssessmentDecisionReject {
		assessment.Findings = []store.Finding{{Code: "test-rejection", Severity: "high"}}
	}
	finalized, err := store.FinalizeResourceAssessment(assessment)
	if err != nil {
		t.Fatalf("finalize assessment decision %q: %v", decision, err)
	}
	revision.Assessment = finalized
	return revision
}

func (c *targetProfileAcquisitionCounts) snapshot() targetProfileAcquisitionSnapshot {
	return targetProfileAcquisitionSnapshot{
		revisions: c.revisions.Load(), inspections: c.inspections.Load(), bodies: c.bodies.Load(),
	}
}

func (c *targetProfileAcquisitionCounts) assertDelta(t *testing.T, before targetProfileAcquisitionSnapshot, revisions, inspections, bodies int32) {
	t.Helper()
	after := c.snapshot()
	got := targetProfileAcquisitionSnapshot{
		revisions: after.revisions - before.revisions, inspections: after.inspections - before.inspections,
		bodies: after.bodies - before.bodies,
	}
	want := targetProfileAcquisitionSnapshot{revisions: revisions, inspections: inspections, bodies: bodies}
	if got != want {
		t.Fatalf("request counts = %+v, want delta %+v", got, want)
	}
}

func assertTargetProfileAcquisitionFailure(t *testing.T, evidence TargetProfileEvidence, err error, want string) {
	t.Helper()
	if err == nil || !reflect.DeepEqual(evidence, TargetProfileEvidence{}) {
		t.Fatalf("failure result = (%+v, %v), want zero evidence and an error", evidence, err)
	}
	if want != "" && !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want %q", err, want)
	}
}
