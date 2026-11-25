package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: KindSkill, Name: "review"},
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
	return publishSourcedAssessedTestRevision(ctx, t, meta, revision, decision, manifest, commit, "", "")
}

func publishSourcedAssessedTestRevision(ctx context.Context, t *testing.T, meta store.MetadataStore, revision store.ResourceRevision, decision store.AssessmentDecision, manifest store.RevisionDigest, commit, sourceOrigin, sourcePath string) store.ResourceAssessment {
	t.Helper()
	source, err := meta.GetRegistry(ctx, revision.Coordinate.Registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceRevision(ctx, revision); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: source.ID, Ref: source.Ref, Commit: commit, RegistryRevisionManifestDigest: manifest,
		Acquisition: source.Acquisition, SourceOrigin: sourceOrigin, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest, Commit: commit, ManifestDigest: manifest, SourcePath: sourcePath}); err != nil {
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

// spec: marketplace-api/both-resource-assessment-reads-carry-identical-advisory-evidence
// spec: marketplace-api/historical-assessment-remains-readable-without-rewritten-identity
func TestResourceAdvisoryAssessmentReads(t *testing.T) {
	ctx := context.Background()
	meta := newManagedSQLite(t)
	coordinate := store.ResourceCoordinate{Marketplace: "marketplace", Registry: "registry", Kind: KindSkill, Name: "review"}
	revision := store.ResourceRevision{Coordinate: coordinate, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64))}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(meta.PutMarketplace(ctx, store.Marketplace{ID: coordinate.Marketplace, Name: "Marketplace", CanonicalURL: "https://marketplace.test"}))
	must(meta.PutRegistry(ctx, store.Registry{ID: coordinate.Registry, Slug: "source", Ref: "main", Listed: true, Acquisition: store.AcquisitionMirror}))
	must(meta.PutResourceRevision(ctx, revision))
	manifest := store.RevisionDigest("sha256:" + strings.Repeat("1", 64))
	commit := strings.Repeat("e", 40)
	must(meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: coordinate.Registry, Ref: "main", Commit: commit,
		RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror,
		OccurredAt: time.Unix(1, 0).UTC(),
	}))
	must(meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: coordinate, ContentDigest: revision.ContentDigest, Commit: commit, ManifestDigest: manifest}))

	builtInOnly := testRevisionAssessment(t, revision, store.AssessmentDecisionAllow)
	builtInOnly.AnalyzerVersion = "analyzer/v1"
	builtInOnly.PolicyDigest = store.PolicyDigest("sha256:" + strings.Repeat("c", 64))
	builtInOnly.Findings = []store.Finding{{Code: "safe", Severity: "info", Policy: "allow", Message: "No unsafe behavior found."}}
	builtInOnly.InspectionManifestDigest = store.RevisionDigest("sha256:" + strings.Repeat("f", 64))
	builtInOnly.AssessedAt = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	const historicalDigest = store.AssessmentDigest("sha256:2e7c339d14f903ba227bc73206d33e4c4e781971477f61d294805840546f83f9")
	builtInOnly.AssessmentDigest = historicalDigest
	if err := builtInOnly.Validate(); err != nil {
		t.Fatalf("historical built-in-only fixture no longer has its pre-change identity: %v", err)
	}
	must(meta.PutResourceAssessment(ctx, builtInOnly))
	must(meta.SetCurrentResourceAssessment(ctx, builtInOnly))

	expectedReports := []store.AdvisoryReport{{
		Tool: "skillspector", Version: "2.13.6", Status: store.AdvisoryPartial, Reason: "coverage_gaps",
		Findings: []store.AdvisoryFinding{{Code: "unsafe-process", Severity: "high", Summary: "Avoid process execution.", Path: "SKILL.md"}},
	}}
	withAdvisory := builtInOnly
	withAdvisory.AssessmentDigest = ""
	withAdvisory.AdvisoryReports = expectedReports
	withAdvisory.AssessedAt = builtInOnly.AssessedAt.Add(time.Minute)
	withAdvisory, err := store.FinalizeResourceAssessment(withAdvisory)
	must(err)
	if withAdvisory.AssessmentDigest == historicalDigest {
		t.Fatal("advisory evidence did not change assessment identity")
	}
	must(meta.PutResourceAssessment(ctx, withAdvisory))
	must(meta.SetCurrentResourceAssessment(ctx, withAdvisory))

	handler := NewServer(config.Config{
		Instance:   config.Instance{ID: string(coordinate.Marketplace)},
		Registries: []config.Registry{{ID: string(coordinate.Registry), Slug: "source", Acquisition: string(store.AcquisitionMirror)}},
	}, meta, nil).PublicHandler()
	get := func(path string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, response.Code, response.Body.String())
		}
		return response
	}
	assessmentPath := func(digest store.AssessmentDigest) string {
		return "/api/v1/assessments/sha256/" + strings.TrimPrefix(string(digest), "sha256:")
	}

	historicalResponse := get(assessmentPath(historicalDigest))
	var historical struct {
		store.ResourceAssessment
		AdvisoryReports json.RawMessage `json:"advisory_reports"`
	}
	must(json.Unmarshal(historicalResponse.Body.Bytes(), &historical))
	if historical.AssessmentDigest != historicalDigest || !reflect.DeepEqual(historical.Findings, builtInOnly.Findings) {
		t.Fatalf("historical assessment = %#v, want the pinned built-in-only assessment", historical)
	}
	if historical.AdvisoryReports != nil {
		t.Fatal("historical assessment fabricated advisory evidence")
	}

	digestResponse := get(assessmentPath(withAdvisory.AssessmentDigest))
	var byDigest store.ResourceAssessment
	must(json.Unmarshal(digestResponse.Body.Bytes(), &byDigest))
	revisionPath := "/api/v1/resources/source/skill/review/revisions/" + string(revision.ContentDigest)
	revisionResponse := get(revisionPath)
	var byRevision struct {
		Assessment store.ResourceAssessment `json:"assessment"`
	}
	must(json.Unmarshal(revisionResponse.Body.Bytes(), &byRevision))
	if !reflect.DeepEqual(byDigest, byRevision.Assessment) ||
		byDigest.AssessmentDigest != withAdvisory.AssessmentDigest ||
		byDigest.ContentDigest != builtInOnly.ContentDigest ||
		byDigest.InspectionManifestDigest != builtInOnly.InspectionManifestDigest ||
		!reflect.DeepEqual(byDigest.Findings, builtInOnly.Findings) ||
		!reflect.DeepEqual(byDigest.AdvisoryReports, expectedReports) {
		t.Fatalf("route assessments disagree or differ from the expected evidence: digest=%+v revision=%+v", byDigest, byRevision.Assessment)
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
