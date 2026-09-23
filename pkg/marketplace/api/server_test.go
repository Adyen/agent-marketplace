package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	marketplaceartifact "github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
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

	src := store.Registry{
		ID:             store.RegistryID("src-test"),
		Slug:           "test-source",
		Ref:            "main",
		Origins:        []string{"https://user:origin-secret@example.com/repo.git?token=query-secret"},
		Acquisition:    store.AcquisitionMirror,
		License:        "MIT",
		CredentialsRef: "env:MARKETPLACE_GIT_TOKEN",
		RefreshStatus:  store.RegistryRefreshStatusFailed,
		Roots: []store.RegistryRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
		Listed:       true,
		RefreshError: "clone failed with token=public-api-secret",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := meta.PutRegistry(ctx, src); err != nil {
		t.Fatalf("put source: %v", err)
	}

	canonicalArtifact, err := hccartifact.FromIR(ir.NewSkill("review", "Review", "review body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("build canonical artifact: %v", err)
	}
	artifactData, proofData, artifactDigest, err := hccartifact.EncodeWithProof(canonicalArtifact)
	if err != nil {
		t.Fatalf("encode canonical artifact: %v", err)
	}
	rev := store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID(cfg.Instance.ID),
			Registry:    src.ID,
			Kind:        KindSkill,
			Name:        "review",
		},
		ContentDigest: store.RevisionDigest(canonicalArtifact.Manifest.ContentDigest),
		ArchiveDigest: store.ArchiveDigest(artifactDigest),
		License:       "MIT",
	}
	manifestData, manifestDigest, err := marketplaceartifact.CanonicalRegistryRevisionPayload(marketplaceartifact.RegistryRevisionManifest{
		SchemaVersion: marketplaceartifact.RegistryRevisionSchemaV1,
		Generator:     "test source revision",
		Resources: []marketplaceartifact.RevisionResource{{
			Kind:          KindSkill,
			Name:          "review",
			ContentDigest: string(rev.ContentDigest),
			ArchiveDigest: string(rev.ArchiveDigest),
		}},
	})
	if err != nil {
		t.Fatalf("build source manifest: %v", err)
	}
	manifest := store.RevisionDigest(manifestDigest)
	commit := strings.Repeat("a", 40)
	if err := meta.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("put resource revision: %v", err)
	}

	if err := blobs.Put(ctx, rev.ArchiveDigest, artifactData); err != nil {
		t.Fatalf("put artifact blob: %v", err)
	}
	if err := blobs.Put(ctx, store.ArtifactProofBlobKey(rev.ArchiveDigest), proofData); err != nil {
		t.Fatalf("put artifact proof blob: %v", err)
	}

	if err := blobs.Put(ctx, store.ArchiveDigest(manifest), manifestData); err != nil {
		t.Fatalf("put manifest blob: %v", err)
	}

	occ := store.RegistryOccurrence{
		RegistryID:                     src.ID,
		Ref:                            src.Ref,
		Roots:                          src.Roots,
		Commit:                         commit,
		RegistryRevisionManifestDigest: manifest,
		Acquisition:                    src.Acquisition,
		OccurredAt:                     time.Now().UTC(),
	}
	if err := meta.PutRegistryOccurrence(ctx, occ); err != nil {
		t.Fatalf("put registry occurrence: %v", err)
	}
	if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, Commit: commit, ManifestDigest: manifest}); err != nil {
		t.Fatalf("put resource occurrence: %v", err)
	}
	inspectionBody := []byte("# Review\n")
	projection, err := inspection.Build(rev.Coordinate, string(rev.ContentDigest), inspectionBody, nil, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, AnalyzerVersion: "1.0.0",
		PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest(projection.Manifest.ManifestDigest), AssessedAt: time.Now().UTC(),
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
	if err := blobs.Put(ctx, store.ArchiveDigest(assessment.InspectionManifestDigest), projection.Payload); err != nil {
		t.Fatal(err)
	}
	if err := blobs.Put(ctx, store.ArchiveDigest(inspection.Digest(inspectionBody)), inspectionBody); err != nil {
		t.Fatal(err)
	}

	emptyRegistry := src
	emptyRegistry.ID = "src-empty"
	emptyRegistry.Slug = "empty-source"
	if err := meta.PutRegistry(ctx, emptyRegistry); err != nil {
		t.Fatalf("put empty source: %v", err)
	}

	boundedRegistry := src
	boundedRegistry.ID = "src-bounded"
	boundedRegistry.Slug = "bounded-source"
	if err := meta.PutRegistry(ctx, boundedRegistry); err != nil {
		t.Fatalf("put bounded source: %v", err)
	}
	for i := 0; i < MaxRegistryHistoryEntries+1; i++ {
		if err := meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
			RegistryID:                     boundedRegistry.ID,
			Ref:                            boundedRegistry.Ref,
			Roots:                          boundedRegistry.Roots,
			Commit:                         fmt.Sprintf("%040x", i+1),
			RegistryRevisionManifestDigest: manifest,
			Acquisition:                    boundedRegistry.Acquisition,
			OccurredAt:                     time.Unix(int64(i+1), 0).UTC(),
		}); err != nil {
			t.Fatalf("put bounded registry occurrence %d: %v", i, err)
		}
	}

	server := NewServer(cfg, meta, blobs, WithVersion("v4.0.0-test"))
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
	if !strings.Contains(body, `"version":"v4.0.0-test"`) {
		t.Fatalf("well-known missing running version: %s", body)
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
	if !strings.Contains(body, `"version":"v4.0.0-test"`) {
		t.Fatalf("instance missing running version: %s", body)
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

	resp = mustGet(t, public.URL+"/api/v1/search?registry=missing&registry=test-source")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"total":1`) || !strings.Contains(body, `"name":"review"`) {
		t.Fatalf("multi-registry search status/body = %d/%s", resp.StatusCode, body)
	}

	resp = mustGet(t, public.URL+"/api/v1/registries")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("registries status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `{"id":"src-test","slug":"test-source"}`) {
		t.Fatalf("registries missing registry summary: %s", body)
	}
	for _, secret := range []string{"public-api-secret", "origin-secret", "query-secret", "credentials_ref", "refresh_error", "refresh_status", "refreshed_at"} {
		if strings.Contains(body, secret) {
			t.Fatalf("registries response leaked private registry field %q: %s", secret, body)
		}
	}

	// /api/v1/registries/{registry}
	resp = mustGet(t, public.URL+"/api/v1/registries/test-source")
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

	// /api/v1/registries/{registry}/history
	resp = mustGet(t, public.URL+"/api/v1/registries/test-source/history")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`) {
		t.Fatalf("history missing commit: %s", body)
	}

	// Exact registry occurrence includes the current approved acquisition origin.
	resp = mustGet(t, public.URL+"/api/v1/registries/test-source/occurrences/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK ||
		!strings.Contains(body, `"registry_id":"src-test"`) ||
		!strings.Contains(body, `"ref":"main"`) ||
		!strings.Contains(body, `"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`) ||
		!strings.Contains(body, `"acquisition_origin":"https://example.com/repo.git?token=%5Bredacted%5D"`) {
		t.Fatalf("exact occurrence status/body = %d/%s", resp.StatusCode, body)
	}

	// Empty source history is a JSON array, not null.
	resp = mustGet(t, public.URL+"/api/v1/registries/empty-source/history")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty history status %d: %s", resp.StatusCode, body)
	}
	if body != "[]\n" {
		t.Fatalf("empty history body = %q, want []", body)
	}

	// History is bounded before it is serialized.
	resp = mustGet(t, public.URL+"/api/v1/registries/bounded-source/history")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bounded history status %d: %s", resp.StatusCode, body)
	}
	var boundedHistory []store.RegistryOccurrence
	if err := json.Unmarshal([]byte(body), &boundedHistory); err != nil {
		t.Fatalf("decode bounded history: %v", err)
	}
	if len(boundedHistory) != MaxRegistryHistoryEntries {
		t.Fatalf("bounded history entries = %d, want %d", len(boundedHistory), MaxRegistryHistoryEntries)
	}
	if boundedHistory[0].Commit != fmt.Sprintf("%040x", MaxRegistryHistoryEntries+1) || boundedHistory[len(boundedHistory)-1].Commit != fmt.Sprintf("%040x", 2) {
		t.Fatalf("bounded history ordering/truncation = first %q last %q", boundedHistory[0].Commit, boundedHistory[len(boundedHistory)-1].Commit)
	}

	// /api/v1/resources/{registry}/{kind}/{name}
	resp = mustGet(t, public.URL+"/api/v1/resources/test-source/skill/review")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resource status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, fmt.Sprintf(`"content_digest":"%s"`, rev.ContentDigest)) {
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
	resp = mustGet(t, public.URL+"/api/v1/resources/test-source/skill/review/revisions/"+string(rev.ContentDigest))
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revision status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"content_digest"`) {
		t.Fatalf("revision missing content_digest: %s", body)
	}

	// manifest
	resp = mustGet(t, public.URL+"/api/v1/manifests/"+string(manifest))
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manifest status %d: %s", resp.StatusCode, body)
	}
	if body != string(manifestData) {
		t.Fatalf("manifest unexpected body: %s", body)
	}

	// artifact
	resp = mustGet(t, public.URL+"/api/v1/artifacts/"+strings.Replace(string(rev.ArchiveDigest), ":", "/", 1))
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("artifact status %d: %s", resp.StatusCode, body)
	}
	if body != string(artifactData) {
		t.Fatalf("artifact body does not match the canonical artifact payload")
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/vnd.agent-marketplace.artifact.v1.tar+gzip" {
		t.Fatalf("artifact content-type = %q", ct)
	}

	// artifact proof
	resp = mustGet(t, public.URL+"/api/v1/artifact-proofs/"+strings.Replace(string(rev.ArchiveDigest), ":", "/", 1))
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("artifact proof status %d: %s", resp.StatusCode, body)
	}
	if body != string(proofData) {
		t.Fatal("artifact proof body does not match the canonical placeholder archive")
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/vnd.agent-marketplace.artifact-proof.v1.tar+gzip" {
		t.Fatalf("artifact proof content-type = %q", ct)
	}

	// A byte-different gzip stream remains valid when its decoded content is
	// bound to the proof. This models a final archive produced by another Go
	// compressor implementation.
	alternateArtifactData := bytes.Clone(artifactData)
	if len(alternateArtifactData) < 10 {
		t.Fatal("artifact fixture is shorter than a gzip header")
	}
	alternateArtifactData[9] ^= 1
	if err := blobs.Put(ctx, rev.ArchiveDigest, alternateArtifactData); err != nil {
		t.Fatalf("put alternate artifact blob: %v", err)
	}
	resp = mustGet(t, public.URL+"/api/v1/artifacts/"+strings.Replace(string(rev.ArchiveDigest), ":", "/", 1))
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("alternate artifact status %d: %s", resp.StatusCode, body)
	}
	if body != string(alternateArtifactData) {
		t.Fatal("alternate artifact body was not served byte-for-byte")
	}

	// A valid final archive stored under the wrong digest must not borrow a
	// valid proof for different content.
	tamperedArtifact := canonicalArtifact
	tamperedArtifact.Payload = bytes.Clone(canonicalArtifact.Payload)
	tamperedArtifact.Payload[len(tamperedArtifact.Payload)-1] ^= 1
	tamperedArtifact.Manifest.ContentDigest = hccartifact.ContentDigest(tamperedArtifact.Payload)
	tamperedData, _, err := hccartifact.Encode(tamperedArtifact)
	if err != nil {
		t.Fatalf("encode tampered artifact: %v", err)
	}
	if err := blobs.Put(ctx, rev.ArchiveDigest, tamperedData); err != nil {
		t.Fatalf("put tampered artifact blob: %v", err)
	}
	resp = mustGet(t, public.URL+"/api/v1/artifacts/"+strings.Replace(string(rev.ArchiveDigest), ":", "/", 1))
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("tampered artifact status %d: %s", resp.StatusCode, body)
	}
	if err := blobs.Put(ctx, rev.ArchiveDigest, artifactData); err != nil {
		t.Fatalf("restore artifact blob: %v", err)
	}

	// Digest route parameters reject malformed, uppercase, and traversal-shaped
	// values before consulting the blob store.
	invalidDigestURLs := []string{
		public.URL + "/api/v1/manifests/sha256:" + strings.Repeat("a", 63),
		public.URL + "/api/v1/manifests/sha256:" + strings.Repeat("A", 64),
		public.URL + "/api/v1/artifacts/sha256/" + strings.Repeat("a", 63),
		public.URL + "/api/v1/artifacts/sha256/" + strings.Repeat("A", 64),
		public.URL + "/api/v1/artifacts/sha256/" + strings.Repeat("a", 64) + "/extra",
		public.URL + "/api/v1/artifact-proofs/sha256/" + strings.Repeat("a", 63),
		public.URL + "/api/v1/artifact-proofs/sha256/" + strings.Repeat("A", 64),
		public.URL + "/api/v1/artifact-proofs/sha256/" + strings.Repeat("a", 64) + "/extra",
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

	for _, path := range []string{
		"/r/test-source/skill/review",
		"/r/test-source/skill/review/revisions/" + strings.ReplaceAll(string(rev.ContentDigest), ":", "%3A"),
	} {
		resp = mustGet(t, public.URL+path)
		body = mustRead(t, resp)
		if resp.StatusCode != http.StatusNotFound || strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
			t.Fatalf("public browser %s status/content-type/body = %d/%s/%s", path, resp.StatusCode, resp.Header.Get("Content-Type"), body)
		}
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

type probingBlobStore struct {
	store.BlobStore
	probeErr      error
	probes        int
	gets          int
	exists        int
	probeDeadline time.Time
}

func (s *probingBlobStore) Probe(ctx context.Context) error {
	s.probes++
	s.probeDeadline, _ = ctx.Deadline()
	return s.probeErr
}

func (s *probingBlobStore) Get(ctx context.Context, digest store.ArchiveDigest) ([]byte, error) {
	s.gets++
	return s.BlobStore.Get(ctx, digest)
}

func (s *probingBlobStore) Exists(ctx context.Context, digest store.ArchiveDigest) (bool, error) {
	s.exists++
	return s.BlobStore.Exists(ctx, digest)
}

func setupReadyzServer(t *testing.T, revisions int) (*probingBlobStore, http.Handler) {
	t.Helper()
	ctx := context.Background()

	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })

	cfg := config.Config{Instance: config.Instance{ID: "mp-readyz", Name: "Readyz"}}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: store.MarketplaceID(cfg.Instance.ID), Name: cfg.Instance.Name}); err != nil {
		t.Fatalf("put marketplace: %v", err)
	}
	src := store.Registry{
		ID:          store.RegistryID("src-readyz"),
		Slug:        "readyz-source",
		Ref:         "main",
		Origins:     []string{"https://example.com/repo.git"},
		Acquisition: store.AcquisitionMirror,
		Listed:      true,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := meta.PutRegistry(ctx, src); err != nil {
		t.Fatalf("put registry: %v", err)
	}
	for i := 0; i < revisions; i++ {
		rev := store.ResourceRevision{
			Coordinate: store.ResourceCoordinate{
				Marketplace: store.MarketplaceID(cfg.Instance.ID),
				Registry:    src.ID,
				Kind:        KindSkill,
				Name:        fmt.Sprintf("skill-%04d", i),
			},
			ContentDigest: store.RevisionDigest(fmt.Sprintf("sha256:%064x", i+1)),
			ArchiveDigest: store.ArchiveDigest(fmt.Sprintf("sha256:%064x", i+1001)),
		}
		if err := meta.PutResourceRevision(ctx, rev); err != nil {
			t.Fatalf("put resource revision %d: %v", i, err)
		}
	}

	// The blob root intentionally stays empty: readiness must not enumerate
	// catalog blobs.
	blobs := &probingBlobStore{BlobStore: fsblob.New(t.TempDir())}
	return blobs, NewServer(cfg, meta, blobs).OpsHandler()
}

func TestReadyzProbesBlobStoreOnce(t *testing.T) {
	blobs, handler := setupReadyzServer(t, 100)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("readyz status %d: %s", recorder.Code, recorder.Body.String())
	}
	if blobs.probes != 1 {
		t.Fatalf("readyz probed %d times, want 1", blobs.probes)
	}
	if blobs.probeDeadline.IsZero() {
		t.Fatalf("probe context has no deadline")
	}
	if d := time.Until(blobs.probeDeadline); d > blobProbeTimeout {
		t.Fatalf("probe deadline %s exceeds %s", d, blobProbeTimeout)
	}
	if blobs.gets != 0 || blobs.exists != 0 {
		t.Fatalf("readyz read catalog blobs: gets=%d exists=%d, want 0/0", blobs.gets, blobs.exists)
	}
}

func TestReadyzFailsWhenBlobProbeFails(t *testing.T) {
	blobs, handler := setupReadyzServer(t, 1)
	blobs.probeErr = errors.New("blob backend unavailable")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status %d: %s", recorder.Code, recorder.Body.String())
	}
	if blobs.probes != 1 {
		t.Fatalf("readyz probed %d times, want 1", blobs.probes)
	}
	if blobs.gets != 0 || blobs.exists != 0 {
		t.Fatalf("readyz read catalog blobs: gets=%d exists=%d, want 0/0", blobs.gets, blobs.exists)
	}
}

type countingBlobStore struct {
	store.BlobStore
	gets int
}

func (s *countingBlobStore) Get(ctx context.Context, digest store.ArchiveDigest) ([]byte, error) {
	s.gets++
	return s.BlobStore.Get(ctx, digest)
}

func TestPublicManifestRejectsArchiveBlob(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	blobs := fsblob.New(t.TempDir())
	data := []byte("not a manifest or an archive format")
	digest := inspection.Digest(data)
	if err := blobs.Put(ctx, store.ArchiveDigest(digest), data); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(config.Config{Instance: config.Instance{ID: "mp"}}, meta, blobs).PublicHandler())
	defer server.Close()

	resp := mustGet(t, server.URL+"/api/v1/manifests/"+digest)
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("arbitrary manifest status/body = %d/%s", resp.StatusCode, body)
	}
}

func TestPublicArtifactDoesNotReadUnownedBlob(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	base := fsblob.New(t.TempDir())
	data := []byte("orphaned artifact")
	digest := inspection.Digest(data)
	if err := base.Put(ctx, store.ArchiveDigest(digest), data); err != nil {
		t.Fatal(err)
	}
	blobs := &countingBlobStore{BlobStore: base}
	server := httptest.NewServer(NewServer(config.Config{Instance: config.Instance{ID: "mp"}}, meta, blobs).PublicHandler())
	defer server.Close()

	resp := mustGet(t, server.URL+"/api/v1/artifacts/"+strings.Replace(digest, ":", "/", 1))
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unowned artifact status/body = %d/%s", resp.StatusCode, body)
	}
	if blobs.gets != 0 {
		t.Fatalf("unowned artifact caused %d blob reads, want 0", blobs.gets)
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
	validRegistry := RegistryDTO{
		ID: "source", Slug: "source", Origins: []string{"https://example.com/repo.git"},
		Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionIndexOnly,
	}
	if err := validatePublicRegistry(validRegistry); err != nil {
		t.Fatalf("canonical source DTO rejected: %v", err)
	}
	for _, invalid := range []RegistryDTO{
		{ID: validRegistry.ID, Slug: validRegistry.Slug, Origins: validRegistry.Origins, Ref: validRegistry.Ref, Acquisition: validRegistry.Acquisition},
		{ID: validRegistry.ID, Slug: validRegistry.Slug, Origins: validRegistry.Origins, Roots: validRegistry.Roots, Ref: validRegistry.Ref, Acquisition: "index"},
	} {
		if err := validatePublicRegistry(invalid); err == nil {
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
	occurrence := store.RegistryOccurrence{
		RegistryID:                     "source",
		Ref:                            "main",
		Roots:                          []store.RegistryRoot{{Path: ".agents"}},
		Commit:                         strings.Repeat("a", 40),
		RegistryRevisionManifestDigest: store.RevisionDigest(validDigest),
		Acquisition:                    store.AcquisitionIndexOnly,
		OccurredAt:                     time.Now().UTC(),
	}
	if err := validatePublicRegistryOccurrence(occurrence); err != nil {
		t.Fatalf("canonical registry occurrence rejected: %v", err)
	}
	for _, invalid := range []store.RegistryOccurrence{
		{RegistryID: "source", Ref: "main", Commit: strings.Repeat("a", 40), RegistryRevisionManifestDigest: "sha256:opaque", Acquisition: store.AcquisitionIndexOnly, OccurredAt: time.Now().UTC()},
		{RegistryID: "source", Ref: "main", Commit: strings.Repeat("a", 40), RegistryRevisionManifestDigest: store.RevisionDigest(validDigest), Acquisition: "index", OccurredAt: time.Now().UTC()},
		{RegistryID: "source", Ref: "main", Commit: "abc123", RegistryRevisionManifestDigest: store.RevisionDigest(validDigest), Acquisition: store.AcquisitionIndexOnly, OccurredAt: time.Now().UTC()},
	} {
		if err := validatePublicRegistryOccurrence(invalid); err == nil {
			t.Fatalf("non-canonical registry occurrence accepted: %#v", invalid)
		}
	}

	coordinate := store.ResourceCoordinate{Marketplace: "marketplace", Registry: "source", Kind: KindSkill, Name: "review"}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coordinate, ContentDigest: store.RevisionDigest(validDigest), AnalyzerVersion: "test/v1",
		PolicyDigest: store.PolicyDigest(validDigest), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest(validDigest), AssessedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	revision := store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{Coordinate: coordinate, ContentDigest: store.RevisionDigest(validDigest)},
		Assessment:       assessment,
		Occurrence:       store.ResourceOccurrence{Coordinate: coordinate, ContentDigest: store.RevisionDigest(validDigest), Commit: strings.Repeat("a", 40), ManifestDigest: store.RevisionDigest(validDigest)},
	}
	if err := validatePublicResourceRevision(revision); err != nil {
		t.Fatalf("canonical resource revision rejected: %v", err)
	}
	revision.Coordinate.Name = ""
	if err := validatePublicResourceRevision(revision); err == nil {
		t.Fatal("resource revision with incomplete coordinate accepted")
	}
	revision.Coordinate = coordinate
	revision.Occurrence.Commit = "refs/heads/main"
	if err := validatePublicResourceRevision(revision); err == nil {
		t.Fatal("resource revision with symbolic occurrence commit accepted")
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
	if err := meta.PutRegistry(ctx, store.Registry{ID: "source-id", Slug: "source-slug", Origins: []string{"https://example.com/repo"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatalf("put source: %v", err)
	}
	cfg := config.Config{Instance: config.Instance{ID: "marketplace"}}
	intrinsic := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "marketplace", Registry: "source-id", Kind: KindSkill, Name: "review"},
		ContentDigest: digest,
	}
	otherCoordinate := intrinsic
	otherCoordinate.Coordinate.Name = "other"
	badCoordinate := testAssessedRevision(t, otherCoordinate, store.AssessmentDecisionAllow)
	otherDigest := intrinsic
	otherDigest.ContentDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	badDigest := testAssessedRevision(t, otherDigest, store.AssessmentDecisionAllow)

	for _, tc := range []struct {
		name string
		path string
		meta store.MetadataStore
	}{
		{name: "latest", path: "/api/v1/resources/source-slug/skill/review", meta: &mismatchedResourceStore{MetadataStore: meta, latest: badCoordinate}},
		{name: "list", path: "/api/v1/resources/source-slug/skill/review/revisions", meta: &mismatchedResourceStore{MetadataStore: meta, revisions: []store.AssessedResourceRevision{badCoordinate}}},
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
	latest    store.AssessedResourceRevision
	exact     store.AssessedResourceRevision
	revisions []store.AssessedResourceRevision
}

func (s *mismatchedResourceStore) GetLatestResourceRevision(context.Context, store.ResourceCoordinate) (store.AssessedResourceRevision, error) {
	return s.latest, nil
}

func (s *mismatchedResourceStore) GetResourceRevision(context.Context, store.ResourceCoordinate, store.RevisionDigest) (store.AssessedResourceRevision, error) {
	return s.exact, nil
}

func (s *mismatchedResourceStore) ListResourceRevisions(context.Context, store.ResourceCoordinate) ([]store.AssessedResourceRevision, error) {
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

func TestServerAppliesLiveExactRevocationToDiscoveryAndArtifacts(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	blobs := fsblob.New(t.TempDir())
	digest := "sha256:" + strings.Repeat("a", 64)
	archive := store.ArchiveDigest("sha256:" + strings.Repeat("b", 64))
	cfg := config.Config{
		Instance:    config.Instance{ID: "mp", Name: "Marketplace", CanonicalURL: "https://example.com"},
		Revocations: config.Revocations{Revisions: []string{"mp/src/skill/review@" + digest}},
	}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Marketplace", CanonicalURL: cfg.Instance.CanonicalURL}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutRegistry(ctx, store.Registry{ID: "src", Slug: "source", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}); err != nil {
		t.Fatal(err)
	}
	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: KindSkill, Name: "review"},
		ContentDigest: store.RevisionDigest(digest), ArchiveDigest: archive,
	}
	inspectionBody := []byte("# Review\n")
	projection, err := inspection.Build(revision.Coordinate, string(revision.ContentDigest), inspectionBody, nil, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	assessment := publishAssessedTestRevision(ctx, t, meta, revision, store.AssessmentDecisionAllow, store.RevisionDigest(projection.Manifest.ManifestDigest), strings.Repeat("e", 40))
	if err := blobs.Put(ctx, store.ArchiveDigest(assessment.InspectionManifestDigest), projection.Payload); err != nil {
		t.Fatal(err)
	}
	if err := blobs.Put(ctx, store.ArchiveDigest(inspection.Digest(inspectionBody)), inspectionBody); err != nil {
		t.Fatal(err)
	}
	if err := blobs.Put(ctx, archive, []byte("artifact")); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(cfg, meta, blobs).PublicHandler())
	defer server.Close()

	for _, path := range []string{
		"/api/v1/resources/source/skill/review",
		"/api/v1/resources/source/skill/review/revisions/" + digest,
		"/api/v1/resources/source/skill/review/revisions",
	} {
		resp := mustGet(t, server.URL+path)
		body := mustRead(t, resp)
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"target_id":"mp/src/skill/review@`+digest+`"`) {
			t.Fatalf("%s status/body = %d/%s", path, resp.StatusCode, body)
		}
	}
	resp := mustGet(t, server.URL+"/api/v1/search?q=review")
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"total":0`) {
		t.Fatalf("search status/body = %d/%s", resp.StatusCode, body)
	}
	resp = mustGet(t, server.URL+"/api/v1/artifacts/sha256/"+strings.Repeat("b", 64))
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusGone || !strings.Contains(body, "revoked") {
		t.Fatalf("artifact status/body = %d/%s", resp.StatusCode, body)
	}

	ops := httptest.NewServer(NewServer(cfg, meta, blobs).OpsHandler())
	defer ops.Close()
	resp = mustGet(t, ops.URL+"/readyz")
	_ = mustRead(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unreconciled readyz status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	if err := meta.PutRevocation(ctx, store.Revocation{Kind: store.RevocationRevision, TargetID: "mp/src/skill/review@" + digest}); err != nil {
		t.Fatal(err)
	}
	resp = mustGet(t, ops.URL+"/readyz")
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reconciled readyz status/body = %d/%s", resp.StatusCode, body)
	}
}
