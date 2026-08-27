package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type assessmentRouteStore struct {
	store.MetadataStore
	assessment store.ResourceAssessment
	owners     []store.AssessedResourceRevision
}

func (s *assessmentRouteStore) GetResourceAssessment(context.Context, store.AssessmentDigest) (store.ResourceAssessment, error) {
	return s.assessment, nil
}

func (s *assessmentRouteStore) ListResourceRevisionsByArchiveDigest(context.Context, store.MarketplaceID, store.ArchiveDigest) ([]store.AssessedResourceRevision, error) {
	return s.owners, nil
}

func testAssessment(t *testing.T, decision store.AssessmentDecision) store.ResourceAssessment {
	t.Helper()
	return testRevisionAssessment(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: KindSkill, Name: "review"},
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
	}, decision)
}

func testRevisionAssessment(t *testing.T, revision store.ResourceRevision, decision store.AssessmentDecision) store.ResourceAssessment {
	t.Helper()
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate:               revision.Coordinate,
		ContentDigest:            revision.ContentDigest,
		AnalyzerVersion:          "analyzer/v2",
		PolicyDigest:             store.PolicyDigest("sha256:" + strings.Repeat("b", 64)),
		Decision:                 decision,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("c", 64)),
		AssessedAt:               time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return assessment
}

func publishAssessedTestRevision(ctx context.Context, t *testing.T, meta store.MetadataStore, revision store.ResourceRevision, decision store.AssessmentDecision, manifest store.RevisionDigest, commit string) store.ResourceAssessment {
	t.Helper()
	source, err := meta.GetSource(ctx, revision.Coordinate.Source)
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceRevision(ctx, revision); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutSourceOccurrence(ctx, store.SourceOccurrence{
		SourceID: source.ID, Ref: source.Ref, Commit: commit, SourceRevisionManifestDigest: manifest,
		Acquisition: source.Acquisition, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest, Commit: commit, ManifestDigest: manifest}); err != nil {
		t.Fatal(err)
	}
	assessment := testRevisionAssessment(t, revision, decision)
	assessment.AssessmentDigest = ""
	assessment.InspectionManifestDigest = manifest
	assessment, err = store.FinalizeResourceAssessment(assessment)
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	if err := meta.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	return assessment
}

func testAssessedRevision(t *testing.T, revision store.ResourceRevision, decision store.AssessmentDecision) store.AssessedResourceRevision {
	t.Helper()
	return store.AssessedResourceRevision{
		ResourceRevision: revision,
		Assessment:       testRevisionAssessment(t, revision, decision),
		Occurrence: store.ResourceOccurrence{
			Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest,
			Commit: strings.Repeat("e", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("f", 64)),
		},
	}
}

func TestAssessmentDigestRouteIsStrictAndImmutable(t *testing.T) {
	assessment := testAssessment(t, store.AssessmentDecisionWarn)
	meta := &assessmentRouteStore{assessment: assessment}
	handler := NewServer(config.Config{Instance: config.Instance{ID: "mp"}}, meta, nil).PublicHandler()
	hex := strings.TrimPrefix(string(assessment.AssessmentDigest), "sha256:")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/assessments/sha256/"+hex, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("assessment status = %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("assessment headers = %#v", response.Header())
	}
	var got store.ResourceAssessment
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AssessmentDigest != assessment.AssessmentDigest || got.Decision != store.AssessmentDecisionWarn {
		t.Fatalf("assessment response = %#v", got)
	}

	for _, path := range []string{
		"/api/v1/assessments/" + string(assessment.AssessmentDigest),
		"/api/v1/assessments/sha256/" + strings.ToUpper(hex),
		"/api/v1/assessments/sha256/" + hex + "/extra",
	} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid path %q status = %d", path, response.Code)
		}
	}
}

func TestArtifactIsBlockedByAnyRejectingCurrentAssessment(t *testing.T) {
	assessment := testAssessment(t, store.AssessmentDecisionReject)
	archive := store.ArchiveDigest("sha256:" + strings.Repeat("d", 64))
	owner := store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{Coordinate: assessment.Coordinate, ContentDigest: assessment.ContentDigest, ArchiveDigest: archive},
		Assessment:       assessment,
	}
	meta := &assessmentRouteStore{owners: []store.AssessedResourceRevision{owner}}
	handler := NewServer(config.Config{Instance: config.Instance{ID: "mp"}}, meta, nil).PublicHandler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/sha256/"+strings.Repeat("d", 64), nil))
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "blocked") {
		t.Fatalf("blocked artifact response = %d/%s", response.Code, response.Body.String())
	}
}
