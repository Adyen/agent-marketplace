package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func TestServerRoutes(t *testing.T) {
	ctx := context.Background()

	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })

	blobsDir := t.TempDir()
	blobs := fsblob.New(blobsDir)

	cfg := config.Config{
		Instance: config.Instance{
			ID:           "mp-test",
			Name:         "Test Marketplace",
			CanonicalURL: "https://example.com",
		},
		Listen:           "127.0.0.1:0",
		OperationsListen: "127.0.0.1:0",
	}

	if err := meta.PutMarketplace(ctx, store.Marketplace{
		ID:           store.MarketplaceID(cfg.Instance.ID),
		Name:         cfg.Instance.Name,
		CanonicalURL: cfg.Instance.CanonicalURL,
	}); err != nil {
		t.Fatalf("put marketplace: %v", err)
	}

	src := store.Source{
		ID:             store.SourceID("src-test"),
		Slug:           "test-source",
		Ref:            "main",
		Origins:        []string{"https://user:origin-secret@example.com/repo.git?token=query-secret"},
		Acquisition:    store.AcquisitionMirror,
		License:        "MIT",
		CredentialsRef: "env:MARKETPLACE_GIT_TOKEN",
		RefreshStatus:  store.SourceRefreshStatusFailed,
		Roots: []store.SourceRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
		Listed:       true,
		RefreshError: "clone failed with token=public-api-secret",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := meta.PutSource(ctx, src); err != nil {
		t.Fatalf("put source: %v", err)
	}

	rev := store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID(cfg.Instance.ID),
			Source:      src.ID,
			Kind:        KindSkill,
			Name:        "review",
		},
		ContentDigest:   "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		ArchiveDigest:   "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		AnalyzerVersion: "1.0.0",
		License:         "MIT",
		Provenance: &store.Provenance{
			SourceID:       src.ID,
			Commit:         strings.Repeat("a", 40),
			ManifestDigest: "sha256:3333333333333333333333333333333333333333333333333333333333333333",
		},
	}
	if err := meta.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("put resource revision: %v", err)
	}

	artifactData := []byte("fake tar.gz artifact")
	if err := blobs.Put(ctx, rev.ArchiveDigest, artifactData); err != nil {
		t.Fatalf("put artifact blob: %v", err)
	}

	manifestData := []byte(`{"schema_version":"v1"}`)
	if err := blobs.Put(ctx, store.ArchiveDigest(rev.Provenance.ManifestDigest), manifestData); err != nil {
		t.Fatalf("put manifest blob: %v", err)
	}

	occ := store.SourceOccurrence{
		SourceID:                     src.ID,
		Ref:                          src.Ref,
		Commit:                       rev.Provenance.Commit,
		SourceRevisionManifestDigest: rev.Provenance.ManifestDigest,
		Acquisition:                  src.Acquisition,
		OccurredAt:                   time.Now().UTC(),
	}
	if err := meta.PutSourceOccurrence(ctx, occ); err != nil {
		t.Fatalf("put source occurrence: %v", err)
	}

	emptySource := src
	emptySource.ID = "src-empty"
	emptySource.Slug = "empty-source"
	if err := meta.PutSource(ctx, emptySource); err != nil {
		t.Fatalf("put empty source: %v", err)
	}

	boundedSource := src
	boundedSource.ID = "src-bounded"
	boundedSource.Slug = "bounded-source"
	if err := meta.PutSource(ctx, boundedSource); err != nil {
		t.Fatalf("put bounded source: %v", err)
	}
	for i := 0; i < MaxSourceHistoryEntries+1; i++ {
		if err := meta.PutSourceOccurrence(ctx, store.SourceOccurrence{
			SourceID:                     boundedSource.ID,
			Ref:                          boundedSource.Ref,
			Commit:                       fmt.Sprintf("%040x", i+1),
			SourceRevisionManifestDigest: rev.Provenance.ManifestDigest,
			Acquisition:                  boundedSource.Acquisition,
			OccurredAt:                   time.Unix(int64(i+1), 0).UTC(),
		}); err != nil {
			t.Fatalf("put bounded source occurrence %d: %v", i, err)
		}
	}

	server := NewServer(cfg, meta, blobs)
	public := httptest.NewServer(server.PublicHandler())
	defer public.Close()
	ops := httptest.NewServer(server.OpsHandler())
	defer ops.Close()

	// /.well-known/agent-marketplace.json
	resp := mustGet(t, public.URL+"/.well-known/agent-marketplace.json")
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("well-known status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"instance_id":"mp-test"`) {
		t.Fatalf("well-known missing instance_id: %s", body)
	}

	// /api/v1/capabilities
	resp = mustGet(t, public.URL+"/api/v1/capabilities")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("capabilities status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"api_versions"`) {
		t.Fatalf("capabilities missing api_versions: %s", body)
	}

	// /api/v1/instance
	resp = mustGet(t, public.URL+"/api/v1/instance")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("instance status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"canonical_url":"https://example.com"`) {
		t.Fatalf("instance missing canonical_url: %s", body)
	}

	// /api/v1/search
	resp = mustGet(t, public.URL+"/api/v1/search")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"name":"review"`) {
		t.Fatalf("search missing review: %s", body)
	}

	// /api/v1/sources/{source}
	resp = mustGet(t, public.URL+"/api/v1/sources/test-source")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("source status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"slug":"test-source"`) {
		t.Fatalf("source missing slug: %s", body)
	}
	for _, secret := range []string{"public-api-secret", "origin-secret", "query-secret", "credentials_ref", "refresh_error", "refresh_status", "refreshed_at"} {
		if strings.Contains(body, secret) {
			t.Fatalf("source response leaked private source field %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, "https://example.com/repo.git?token=%5Bredacted%5D") {
		t.Fatalf("source response did not preserve a sanitized origin: %s", body)
	}

	// /api/v1/sources/{source}/history
	resp = mustGet(t, public.URL+"/api/v1/sources/test-source/history")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`) {
		t.Fatalf("history missing commit: %s", body)
	}

	// Empty source history is a JSON array, not null.
	resp = mustGet(t, public.URL+"/api/v1/sources/empty-source/history")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty history status %d: %s", resp.StatusCode, body)
	}
	if body != "[]\n" {
		t.Fatalf("empty history body = %q, want []", body)
	}

	// History is bounded before it is serialized.
	resp = mustGet(t, public.URL+"/api/v1/sources/bounded-source/history")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bounded history status %d: %s", resp.StatusCode, body)
	}
	var boundedHistory []store.SourceOccurrence
	if err := json.Unmarshal([]byte(body), &boundedHistory); err != nil {
		t.Fatalf("decode bounded history: %v", err)
	}
	if len(boundedHistory) != MaxSourceHistoryEntries {
		t.Fatalf("bounded history entries = %d, want %d", len(boundedHistory), MaxSourceHistoryEntries)
	}
	if boundedHistory[0].Commit != fmt.Sprintf("%040x", MaxSourceHistoryEntries+1) || boundedHistory[len(boundedHistory)-1].Commit != fmt.Sprintf("%040x", 2) {
		t.Fatalf("bounded history ordering/truncation = first %q last %q", boundedHistory[0].Commit, boundedHistory[len(boundedHistory)-1].Commit)
	}

	// /api/v1/resources/{source}/{kind}/{name}
	resp = mustGet(t, public.URL+"/api/v1/resources/test-source/skill/review")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resource status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"content_digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111"`) {
		t.Fatalf("resource missing content_digest: %s", body)
	}

	// revisions
	resp = mustGet(t, public.URL+"/api/v1/resources/test-source/skill/review/revisions")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revisions status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"name":"review"`) {
		t.Fatalf("revisions missing resource: %s", body)
	}

	// specific revision
	resp = mustGet(t, public.URL+"/api/v1/resources/test-source/skill/review/revisions/sha256:1111111111111111111111111111111111111111111111111111111111111111")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revision status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"content_digest"`) {
		t.Fatalf("revision missing content_digest: %s", body)
	}

	// manifest
	resp = mustGet(t, public.URL+"/api/v1/manifests/sha256:3333333333333333333333333333333333333333333333333333333333333333")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manifest status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `{"schema_version":"v1"}`) {
		t.Fatalf("manifest unexpected body: %s", body)
	}

	// artifact
	resp = mustGet(t, public.URL+"/api/v1/artifacts/sha256/2222222222222222222222222222222222222222222222222222222222222222")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("artifact status %d: %s", resp.StatusCode, body)
	}
	if body != "fake tar.gz artifact" {
		t.Fatalf("artifact body = %q, want fake tar.gz artifact", body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/vnd.agent-marketplace.artifact.v1.tar+gzip" {
		t.Fatalf("artifact content-type = %q", ct)
	}

	// Digest route parameters reject malformed, uppercase, and traversal-shaped
	// values before consulting the blob store.
	invalidDigestURLs := []string{
		public.URL + "/api/v1/manifests/sha256:" + strings.Repeat("a", 63),
		public.URL + "/api/v1/manifests/sha256:" + strings.Repeat("A", 64),
		public.URL + "/api/v1/artifacts/sha256/" + strings.Repeat("a", 63),
		public.URL + "/api/v1/artifacts/sha256/" + strings.Repeat("A", 64),
		public.URL + "/api/v1/artifacts/sha256/" + strings.Repeat("a", 64) + "/extra",
		public.URL + "/api/v1/resources/test-source/skill/review/revisions/sha256:" + strings.Repeat("A", 64),
	}
	for _, url := range invalidDigestURLs {
		resp = mustGet(t, url)
		body = mustRead(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid digest URL %s status %d: %s", url, resp.StatusCode, body)
		}
	}
	resp = mustGet(t, public.URL+"/api/v1/manifests/../secret")
	body = mustRead(t, resp)
	if resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("traversal-shaped manifest URL status %d: %s", resp.StatusCode, body)
	}

	// healthz
	resp = mustGet(t, public.URL+"/healthz")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status %d: %s", resp.StatusCode, body)
	}

	// Operations endpoints are never available on the public listener.
	for _, path := range []string{"/readyz", "/metrics"} {
		resp = mustGet(t, public.URL+path)
		body = mustRead(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("public %s status %d: %s", path, resp.StatusCode, body)
		}
	}

	// readyz
	resp = mustGet(t, ops.URL+"/readyz")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readyz status %d: %s", resp.StatusCode, body)
	}

	// metrics
	resp = mustGet(t, ops.URL+"/metrics")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "agent_marketplace_requests_total") {
		t.Fatalf("metrics missing requests_total: %s", body)
	}
}

func TestServerSanitizesLegacyCanonicalURLInPublicResponses(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = meta.Close() }()

	const unsafe = "https://user:canonical-secret@example.com/marketplace?X-Amz-Security-Token=canonical-secret#fragment"
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "legacy", Name: "Legacy", CanonicalURL: unsafe}); err != nil {
		t.Fatalf("put marketplace: %v", err)
	}
	cfg := config.Config{Instance: config.Instance{ID: "legacy", CanonicalURL: unsafe}}
	server := NewServer(cfg, meta, fsblob.New(t.TempDir()))
	public := httptest.NewServer(server.PublicHandler())
	defer public.Close()

	for _, path := range []string{"/.well-known/agent-marketplace.json", "/api/v1/instance"} {
		resp := mustGet(t, public.URL+path)
		body := mustRead(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status %d: %s", path, resp.StatusCode, body)
		}
		if strings.Contains(body, "canonical-secret") || strings.Contains(body, "user@") {
			t.Fatalf("%s leaked unsafe canonical URL: %s", path, body)
		}
		if strings.Contains(body, "X-Amz-Security-Token") || strings.Contains(body, "fragment") {
			t.Fatalf("%s retained unsafe canonical URL components: %s", path, body)
		}
	}
}

func TestPublicDTOValidationRequiresDomainInvariantFields(t *testing.T) {
	validSource := SourceDTO{
		ID: "source", Slug: "source", Origins: []string{"https://example.com/repo.git"},
		Ref: "main", Roots: []store.SourceRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionIndexOnly,
	}
	if err := validatePublicSource(validSource); err != nil {
		t.Fatalf("canonical source DTO rejected: %v", err)
	}
	for _, invalid := range []SourceDTO{
		{ID: validSource.ID, Slug: validSource.Slug, Origins: validSource.Origins, Ref: validSource.Ref, Acquisition: validSource.Acquisition},
		{ID: validSource.ID, Slug: validSource.Slug, Origins: validSource.Origins, Roots: validSource.Roots, Ref: validSource.Ref, Acquisition: "index"},
	} {
		if err := validatePublicSource(invalid); err == nil {
			t.Fatalf("invalid source DTO accepted: %#v", invalid)
		}
	}

	if err := validatePublicMarketplace(store.Marketplace{ID: "marketplace", Name: "Marketplace", CanonicalURL: "https://example.com"}); err != nil {
		t.Fatalf("canonical Marketplace DTO rejected: %v", err)
	}
	if err := validatePublicMarketplace(store.Marketplace{ID: "marketplace", Name: "Marketplace"}); err == nil {
		t.Fatal("Marketplace DTO without canonical URL accepted")
	}
}

func TestPublicBoundaryValidationRejectsDevelopmentFormats(t *testing.T) {
	validDigest := "sha256:" + strings.Repeat("a", 64)
	occurrence := store.SourceOccurrence{
		SourceID:                     "source",
		Ref:                          "main",
		Commit:                       strings.Repeat("a", 40),
		SourceRevisionManifestDigest: store.RevisionDigest(validDigest),
		Acquisition:                  store.AcquisitionIndexOnly,
		OccurredAt:                   time.Now().UTC(),
	}
	if err := validatePublicSourceOccurrence(occurrence); err != nil {
		t.Fatalf("canonical source occurrence rejected: %v", err)
	}
	for _, invalid := range []store.SourceOccurrence{
		{SourceID: "source", Ref: "main", Commit: strings.Repeat("a", 40), SourceRevisionManifestDigest: "sha256:opaque", Acquisition: store.AcquisitionIndexOnly, OccurredAt: time.Now().UTC()},
		{SourceID: "source", Ref: "main", Commit: strings.Repeat("a", 40), SourceRevisionManifestDigest: store.RevisionDigest(validDigest), Acquisition: "index", OccurredAt: time.Now().UTC()},
		{SourceID: "source", Ref: "main", Commit: "abc123", SourceRevisionManifestDigest: store.RevisionDigest(validDigest), Acquisition: store.AcquisitionIndexOnly, OccurredAt: time.Now().UTC()},
	} {
		if err := validatePublicSourceOccurrence(invalid); err == nil {
			t.Fatalf("non-canonical source occurrence accepted: %#v", invalid)
		}
	}

	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "marketplace", Source: "source", Kind: KindSkill, Name: "review"},
		ContentDigest: store.RevisionDigest(validDigest),
	}
	if err := validatePublicResourceRevision(revision); err != nil {
		t.Fatalf("canonical resource revision rejected: %v", err)
	}
	revision.Coordinate.Name = ""
	if err := validatePublicResourceRevision(revision); err == nil {
		t.Fatal("resource revision with incomplete coordinate accepted")
	}
	revision.Coordinate.Name = "review"
	revision.Provenance = &store.Provenance{Commit: "refs/heads/main"}
	if err := validatePublicResourceRevision(revision); err == nil {
		t.Fatal("resource revision with symbolic provenance commit accepted")
	}
}

func TestResourceResponsesBindRequestedCoordinateAndDigest(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = meta.Close() }()

	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := meta.PutSource(ctx, store.Source{ID: "source-id", Slug: "source-slug", Origins: []string{"https://example.com/repo"}, Ref: "main", Roots: []store.SourceRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatalf("put source: %v", err)
	}
	cfg := config.Config{Instance: config.Instance{ID: "marketplace"}}
	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "marketplace", Source: "source-id", Kind: KindSkill, Name: "review"},
		ContentDigest: digest,
	}

	badCoordinate := revision
	badCoordinate.Coordinate.Name = "other"
	badDigest := revision
	badDigest.ContentDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	for _, tc := range []struct {
		name string
		path string
		meta store.MetadataStore
	}{
		{name: "latest", path: "/api/v1/resources/source-slug/skill/review", meta: &mismatchedResourceStore{MetadataStore: meta, latest: badCoordinate}},
		{name: "list", path: "/api/v1/resources/source-slug/skill/review/revisions", meta: &mismatchedResourceStore{MetadataStore: meta, revisions: []store.ResourceRevision{badCoordinate}}},
		{name: "exact coordinate", path: "/api/v1/resources/source-slug/skill/review/revisions/" + digest, meta: &mismatchedResourceStore{MetadataStore: meta, exact: badCoordinate}},
		{name: "exact digest", path: "/api/v1/resources/source-slug/skill/review/revisions/" + digest, meta: &mismatchedResourceStore{MetadataStore: meta, exact: badDigest}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(NewServer(cfg, tc.meta, fsblob.New(t.TempDir())).PublicHandler())
			defer server.Close()
			resp := mustGet(t, server.URL+tc.path)
			body := mustRead(t, resp)
			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusInternalServerError, body)
			}
		})
	}
}

type mismatchedResourceStore struct {
	store.MetadataStore
	latest    store.ResourceRevision
	exact     store.ResourceRevision
	revisions []store.ResourceRevision
}

func (s *mismatchedResourceStore) GetLatestResourceRevision(context.Context, store.ResourceCoordinate) (store.ResourceRevision, error) {
	return s.latest, nil
}

func (s *mismatchedResourceStore) GetResourceRevision(context.Context, store.ResourceCoordinate, store.RevisionDigest) (store.ResourceRevision, error) {
	return s.exact, nil
}

func (s *mismatchedResourceStore) ListResourceRevisions(context.Context, store.ResourceCoordinate) ([]store.ResourceRevision, error) {
	return s.revisions, nil
}

func mustGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func mustRead(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
