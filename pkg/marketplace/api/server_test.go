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
		ID:          store.SourceID("src-test"),
		Slug:        "test-source",
		Ref:         "main",
		Origins:     []string{"https://example.com/repo.git"},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots: []store.SourceRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
		Listed:    true,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
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
			Commit:         "abc123",
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
			Commit:                       fmt.Sprintf("commit-%04d", i),
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

	// /api/v1/sources/{source}/history
	resp = mustGet(t, public.URL+"/api/v1/sources/test-source/history")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"commit":"abc123"`) {
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
	if boundedHistory[0].Commit != "commit-1000" || boundedHistory[len(boundedHistory)-1].Commit != "commit-0001" {
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
