package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func TestResourceAuditEndpointStrictErrors(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	blobs := fsblob.New(t.TempDir())
	cfg := config.Config{Instance: config.Instance{ID: "mp"}}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Test"}); err != nil {
		t.Fatal(err)
	}
	src := store.Registry{ID: "src", Slug: "source", Ref: "main", Origins: []string{"https://example.com/repo"}, Acquisition: store.AcquisitionMirror, Listed: true}
	if err := meta.PutRegistry(ctx, src); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: KindSkill, Name: "review"},
		ContentDigest: store.RevisionDigest(digest),
	}
	if err := meta.PutResourceRevision(ctx, revision); err != nil {
		t.Fatal(err)
	}
	occ := store.RegistryOccurrence{RegistryID: src.ID, Ref: src.Ref, Commit: strings.Repeat("e", 40), RegistryRevisionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64)), Acquisition: src.Acquisition, OccurredAt: time.Now().UTC()}
	if err := meta.PutRegistryOccurrence(ctx, occ); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest, Commit: occ.Commit, ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64))}); err != nil {
		t.Fatal(err)
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest, AnalyzerVersion: "1.0.0",
		PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)), AssessedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	if err := meta.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	audit, err := store.FinalizeResourceAudit(store.ResourceAudit{
		ResourceAuditKey: store.ResourceAuditKey{
			Coordinate:      revision.Coordinate,
			SubjectDigest:   store.RevisionDigest("sha256:" + strings.Repeat("f", 64)),
			AnalyzerID:      "marketplace.static",
			AnalyzerVersion: "1.0.0",
			ConfigDigest:    store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
		},
		Role: analyze.RoleGating, Verdict: analyze.VerdictPass, Summary: "ok", Findings: []store.Finding{},
		Duration: 1, AssessedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceAudit(ctx, audit); err != nil {
		t.Fatal(err)
	}
	if err := meta.LinkResourceAssessmentAudit(ctx, store.AssessmentAuditLink{AssessmentDigest: assessment.AssessmentDigest, Ordinal: 0, AuditKey: audit.ResourceAuditKey, ResultDigest: audit.ResultDigest}); err != nil {
		t.Fatal(err)
	}

	public := httptest.NewServer(NewServer(cfg, meta, blobs).PublicHandler())
	defer public.Close()

	// detail with support-file subject returns 200 from the exact link.
	detailURL := public.URL + "/api/v1/resources/source/skill/review/revisions/" + digest + "/audits/marketplace.static"
	resp := mustGet(t, detailURL)
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit detail status %d: %s", resp.StatusCode, body)
	}
	var detail resourceAuditDetail
	if err := json.Unmarshal([]byte(body), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.AnalyzerID != "marketplace.static" || detail.ResultDigest != audit.ResultDigest {
		t.Fatalf("detail mismatch: %+v", detail)
	}

	// unknown safe analyzer id returns 404.
	resp = mustGet(t, public.URL+"/api/v1/resources/source/skill/review/revisions/"+digest+"/audits/marketplace.unknown")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown analyzer status %d", resp.StatusCode)
	}

	// malformed analyzer id returns 400.
	resp = mustGet(t, public.URL+"/api/v1/resources/source/skill/review/revisions/"+digest+"/audits/Analyzer!")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed analyzer status %d", resp.StatusCode)
	}

	// malformed revision digest returns 400.
	resp = mustGet(t, public.URL+"/api/v1/resources/source/skill/review/revisions/sha256:bad/audits/marketplace.static")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed digest status %d", resp.StatusCode)
	}

	// legacy no-links: latest/exact revision with no audit links omits audits.
	legacyMeta := sqlite.New(":memory:")
	if err := legacyMeta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = legacyMeta.Close() }()
	if err := legacyMeta.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Test"}); err != nil {
		t.Fatal(err)
	}
	if err := legacyMeta.PutRegistry(ctx, src); err != nil {
		t.Fatal(err)
	}
	if err := legacyMeta.PutResourceRevision(ctx, revision); err != nil {
		t.Fatal(err)
	}
	if err := legacyMeta.PutRegistryOccurrence(ctx, occ); err != nil {
		t.Fatal(err)
	}
	if err := legacyMeta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest, Commit: occ.Commit, ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64))}); err != nil {
		t.Fatal(err)
	}
	legacyAssessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest, AnalyzerVersion: "1.0.0",
		PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)), AssessedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := legacyMeta.PutResourceAssessment(ctx, legacyAssessment); err != nil {
		t.Fatal(err)
	}
	if err := legacyMeta.SetCurrentResourceAssessment(ctx, legacyAssessment); err != nil {
		t.Fatal(err)
	}
	legacyPublic := httptest.NewServer(NewServer(cfg, legacyMeta, blobs).PublicHandler())
	defer legacyPublic.Close()
	resp = mustGet(t, legacyPublic.URL+"/api/v1/resources/source/skill/review")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("legacy resource status %d: %s", resp.StatusCode, body)
	}
	var legacy store.AssessedResourceRevision
	if err := json.Unmarshal([]byte(body), &legacy); err != nil {
		t.Fatalf("decode legacy: %v", err)
	}
	if len(legacy.Audits) != 0 {
		t.Fatalf("legacy revision should have no audits, got %v", legacy.Audits)
	}
	if strings.Contains(body, `"audits"`) {
		t.Fatalf("legacy response should omit empty audits field: %s", body)
	}
}

func setupResourceAuditEndpoint(t *testing.T) (context.Context, store.MetadataStore, config.Config, store.ResourceRevision, store.ResourceAssessment) {
	t.Helper()
	ctx := context.Background()
	base := sqlite.New(":memory:")
	if err := base.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	cfg := config.Config{Instance: config.Instance{ID: "mp"}}
	if err := base.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Test"}); err != nil {
		t.Fatal(err)
	}
	src := store.Registry{ID: "src", Slug: "source", Ref: "main", Origins: []string{"https://example.com/repo"}, Acquisition: store.AcquisitionMirror, Listed: true}
	if err := base.PutRegistry(ctx, src); err != nil {
		t.Fatal(err)
	}
	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: KindSkill, Name: "review"},
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
	}
	if err := base.PutResourceRevision(ctx, revision); err != nil {
		t.Fatal(err)
	}
	occ := store.RegistryOccurrence{RegistryID: src.ID, Ref: src.Ref, Commit: strings.Repeat("e", 40), RegistryRevisionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64)), Acquisition: src.Acquisition, OccurredAt: time.Now().UTC()}
	if err := base.PutRegistryOccurrence(ctx, occ); err != nil {
		t.Fatal(err)
	}
	if err := base.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest, Commit: occ.Commit, ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64))}); err != nil {
		t.Fatal(err)
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest, AnalyzerVersion: "1.0.0",
		PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)), AssessedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := base.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	if err := base.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	return ctx, base, cfg, revision, assessment
}

func TestResourceAuditEndpointCorruptAuditReturns500(t *testing.T) {
	_, base, cfg, revision, assessment := setupResourceAuditEndpoint(t)
	digest := string(revision.ContentDigest)

	validAudit := func() store.ResourceAudit {
		a, err := store.FinalizeResourceAudit(store.ResourceAudit{
			ResourceAuditKey: store.ResourceAuditKey{
				Coordinate:      revision.Coordinate,
				SubjectDigest:   store.RevisionDigest("sha256:" + strings.Repeat("f", 64)),
				AnalyzerID:      "marketplace.static",
				AnalyzerVersion: "1.0.0",
				ConfigDigest:    store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
			},
			Role: analyze.RoleGating, Verdict: analyze.VerdictPass, Summary: "ok", Findings: []store.Finding{},
			Duration: 1, AssessedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}

	tests := []struct {
		name  string
		audit store.ResourceAudit
	}{
		{
			name: "coordinate mismatch",
			audit: func() store.ResourceAudit {
				a := validAudit()
				a.Coordinate.Name = "other"
				return a
			}(),
		},
		{
			name: "analyzer mismatch",
			audit: func() store.ResourceAudit {
				a := validAudit()
				a.AnalyzerID = "marketplace.other"
				a.ResultDigest = ""
				a, err := store.FinalizeResourceAudit(a)
				if err != nil {
					t.Fatal(err)
				}
				return a
			}(),
		},
		{
			name: "audit validate failure",
			audit: func() store.ResourceAudit {
				a := validAudit()
				a.Summary = ""
				return a
			}(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeGetAuditStore{MetadataStore: base, audit: tc.audit}
			public := httptest.NewServer(NewServer(cfg, store, fsblob.New(t.TempDir())).PublicHandler())
			defer public.Close()

			resp := mustGet(t, public.URL+"/api/v1/resources/source/skill/review/revisions/"+digest+"/audits/marketplace.static")
			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("corrupt audit status %d", resp.StatusCode)
			}
		})
	}

	_ = assessment
}

func TestResourceAuditEndpointCorruptAuditListReturns500(t *testing.T) {
	_, base, cfg, revision, assessment := setupResourceAuditEndpoint(t)

	validAudit := func(id string) store.ResourceAudit {
		a, err := store.FinalizeResourceAudit(store.ResourceAudit{
			ResourceAuditKey: store.ResourceAuditKey{
				Coordinate:      revision.Coordinate,
				SubjectDigest:   store.RevisionDigest("sha256:" + strings.Repeat("f", 64)),
				AnalyzerID:      id,
				AnalyzerVersion: "1.0.0",
				ConfigDigest:    store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
			},
			Role: analyze.RoleGating, Verdict: analyze.VerdictPass, Summary: "ok", Findings: []store.Finding{},
			Duration: 1, AssessedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}

	tests := []struct {
		name   string
		audits []store.ResourceAudit
	}{
		{
			name:   "invalid underlying audit",
			audits: []store.ResourceAudit{func() store.ResourceAudit { a := validAudit("marketplace.static"); a.ResultDigest = ""; return a }()},
		},
		{
			name: "duplicate analyzer ids",
			audits: []store.ResourceAudit{
				validAudit("marketplace.static"),
				func() store.ResourceAudit {
					a := validAudit("marketplace.static")
					a.Summary = "another"
					a.ResultDigest = ""
					a, err := store.FinalizeResourceAudit(a)
					if err != nil {
						t.Fatal(err)
					}
					return a
				}(),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeListAuditStore{MetadataStore: base, audits: tc.audits}
			public := httptest.NewServer(NewServer(cfg, store, fsblob.New(t.TempDir())).PublicHandler())
			defer public.Close()

			resp := mustGet(t, public.URL+"/api/v1/resources/source/skill/review")
			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("corrupt audit list status %d", resp.StatusCode)
			}
		})
	}

	_ = assessment
}

type fakeGetAuditStore struct {
	store.MetadataStore
	audit store.ResourceAudit
}

func (s *fakeGetAuditStore) GetResourceAuditForAssessment(ctx context.Context, digest store.AssessmentDigest, analyzerID string) (store.ResourceAudit, error) {
	return s.audit, nil
}

type fakeListAuditStore struct {
	store.MetadataStore
	audits []store.ResourceAudit
}

func (s *fakeListAuditStore) ListResourceAuditsForAssessment(ctx context.Context, digest store.AssessmentDigest) ([]store.ResourceAudit, error) {
	return s.audits, nil
}
