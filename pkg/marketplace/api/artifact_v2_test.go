package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

type countedArtifactBlobs struct {
	*fsblob.Store
	reads   int
	corrupt bool
}

func (b *countedArtifactBlobs) GetBounded(ctx context.Context, d store.ArchiveDigest, limit int64) ([]byte, error) {
	b.reads++
	raw, err := b.Store.GetBounded(ctx, d, limit)
	if b.corrupt && len(raw) > 0 {
		raw[0] ^= 1
	}
	return raw, err
}

func TestArtifactV2RouteAuthorizationAndIntegrity(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	blobs := &countedArtifactBlobs{Store: fsblob.New(t.TempDir())}
	registry := store.Registry{ID: "reg", Slug: "reg", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}
	if err := meta.PutRegistry(ctx, registry); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{}`)
	source := artifact.ArtifactV2{Manifest: artifact.ManifestV2{SchemaVersion: "v2", ArtifactVersion: "v2", Kind: "skill", Name: "review", ContentDigest: artifact.ContentDigest(payload), Provenance: artifact.ProvenanceV2{Generator: artifact.GeneratorV2}, SupportedSchemas: []string{"v2"}}, Payload: payload}
	encoded, err := artifact.EncodeV2(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	rev := store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "skill", Name: "review"}, ContentDigest: store.RevisionDigest(source.Manifest.ContentDigest), ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("a", 64))}
	publishAssessedTestRevision(ctx, t, meta, rev, store.AssessmentDecisionAllow, store.RevisionDigest("sha256:"+strings.Repeat("b", 64)), strings.Repeat("c", 40))
	cfg := config.Config{Instance: config.Instance{ID: "mp"}}
	address := "/api/v1/blobs/sha256/" + strings.TrimPrefix(string(encoded.Reference.BlobDigest), "sha256:")
	request := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		NewServer(cfg, meta, blobs).PublicHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response
	}
	if err := blobs.Put(ctx, store.ArchiveDigest(encoded.Reference.BlobDigest), encoded.Bytes); err != nil {
		t.Fatal(err)
	}
	if response := request(address); response.Code != http.StatusNotFound || blobs.reads != 0 {
		t.Fatalf("orphan read: status=%d reads=%d", response.Code, blobs.reads)
	}
	if err := meta.PutResourceArtifact(ctx, store.ResourceArtifact{Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, Reference: encoded.Reference}); err != nil {
		t.Fatal(err)
	}
	response := request(address)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), encoded.Bytes) || response.Header().Get("Content-Type") != artifact.MediaTypeV2 {
		t.Fatalf("valid route: %d %s", response.Code, response.Body.String())
	}
	detail := request("/api/v1/resources/reg/skill/review/revisions/" + string(rev.ContentDigest))
	var decoded store.AssessedResourceRevision
	if err := json.Unmarshal(detail.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if detail.Code != http.StatusOK || len(decoded.Artifacts) != 1 || decoded.Artifacts[0] != encoded.Reference {
		t.Fatalf("references: %d %s", detail.Code, detail.Body.String())
	}
	blobs.corrupt = true
	if response := request(address); response.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt blob status %d", response.Code)
	}
	blobs.corrupt = false
	assessment := testRevisionAssessment(t, rev, store.AssessmentDecisionReject)
	if err := meta.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	if err := meta.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	reads := blobs.reads
	if response := request(address); response.Code != http.StatusForbidden || blobs.reads != reads {
		t.Fatalf("blocked blob read: status=%d reads=%d", response.Code, blobs.reads)
	}
}
