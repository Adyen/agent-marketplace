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

func TestSearchAppliesSourceAndCoordinateRevocations(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name string
		cfg  config.Revocations
	}{
		{name: "source", cfg: config.Revocations{SourceIDs: []string{"src"}}},
		{name: "coordinate", cfg: config.Revocations{Coordinates: []string{"mp/src/skill/review"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			meta := sqlite.New(":memory:")
			if err := meta.Open(ctx); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = meta.Close() })
			if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Marketplace"}); err != nil {
				t.Fatal(err)
			}
			if err := meta.PutSource(ctx, store.Source{ID: "src", Slug: "src", Listed: true, Acquisition: store.AcquisitionMirror}); err != nil {
				t.Fatal(err)
			}
			revision := store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
				ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
			}
			publishAssessedTestRevision(ctx, t, meta, revision, store.AssessmentDecisionAllow, store.RevisionDigest("sha256:"+strings.Repeat("c", 64)), strings.Repeat("e", 40))
			server := httptest.NewServer(NewServer(config.Config{Instance: config.Instance{ID: "mp"}, Revocations: test.cfg}, meta, fsblob.New(t.TempDir())).PublicHandler())
			defer server.Close()
			resp, err := http.Get(server.URL + "/api/v1/search?q=review")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			var result struct {
				Items []ResourceListItem `json:"items"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK || len(result.Items) != 0 {
				t.Fatalf("search = %d/%+v, want no effective result", resp.StatusCode, result.Items)
			}
		})
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
	publishAssessedTestRevision(ctx, t, meta, older, store.AssessmentDecisionAllow, store.RevisionDigest("sha256:"+strings.Repeat("c", 64)), strings.Repeat("e", 40))
	publishAssessedTestRevision(ctx, t, meta, newer, store.AssessmentDecisionAllow, store.RevisionDigest("sha256:"+strings.Repeat("c", 64)), strings.Repeat("e", 40))
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
