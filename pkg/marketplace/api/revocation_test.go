package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func TestManifestRevocationUsesLiveDeclarationWithoutRevisionMetadata(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	blobs := fsblob.New(t.TempDir())

	payload, digest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion:    hccartifact.CollectionSchemaV1,
		Name:             "bundle",
		Members:          []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "review", SourceID: "src", Revision: "sha256:" + strings.Repeat("b", 64)}},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := blobs.Put(ctx, store.ArchiveDigest(digest), payload); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Instance:    config.Instance{ID: "mp"},
		Revocations: config.Revocations{Revisions: []string{"mp/src/collection/bundle@" + digest}},
	}
	server := httptest.NewServer(NewServer(cfg, meta, blobs).PublicHandler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/manifests/" + digest)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if resp.StatusCode != http.StatusGone || !strings.Contains(string(body), "revoked") {
		t.Fatalf("manifest status/body = %d/%s, want HTTP 410", resp.StatusCode, body)
	}
}

func TestSearchSelectsLatestEffectiveNonRevokedRevision(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	blobs := fsblob.New(t.TempDir())
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Marketplace"}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutSource(ctx, store.Source{ID: "src", Slug: "src", Listed: true, Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatal(err)
	}
	older := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: KindSkill, Name: "review"},
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
	}
	newer := older
	newer.ContentDigest = store.RevisionDigest("sha256:" + strings.Repeat("b", 64))
	if err := meta.PutResourceRevision(ctx, older); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceRevision(ctx, newer); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Instance:    config.Instance{ID: "mp"},
		Revocations: config.Revocations{Revisions: []string{"mp/src/skill/review@" + string(newer.ContentDigest)}},
	}
	server := httptest.NewServer(NewServer(cfg, meta, blobs).PublicHandler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/search?q=review")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Items []ResourceListItem `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || len(result.Items) != 1 || result.Items[0].ContentDigest != string(older.ContentDigest) {
		t.Fatalf("search status/items = %d/%+v, want older effective revision", resp.StatusCode, result.Items)
	}
}
