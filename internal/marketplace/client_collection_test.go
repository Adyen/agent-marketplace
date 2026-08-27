package marketplace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestClientGetCollectionManifestVerifiesCanonicalDigestAndLeaves(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	payload, manifestDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion:    hccartifact.CollectionSchemaV1,
		Name:             "bundle",
		Members:          []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "review", SourceID: "src", Revision: digest}},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/manifests/"+manifestDigest {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	manifest, gotPayload, err := client.GetCollectionManifest(context.Background(), manifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "bundle" || string(gotPayload) != string(payload) || manifest.ContentDigest != manifestDigest {
		t.Fatalf("manifest = %#v, payload match = %v", manifest, string(gotPayload) == string(payload))
	}
}

func TestPrepareLocatorRejectsRevokedCollectionBeforeFetchingManifest(t *testing.T) {
	contentDigest := "sha256:" + strings.Repeat("a", 64)
	var manifestRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp")
		case "/api/v1/sources/src":
			writeTestSource(w, store.Source{ID: "src", Slug: "src", Origins: []string{"https://example.com/repo"}, Ref: "main", Roots: []store.SourceRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
		case "/api/v1/resources/src/collection/bundle":
			revision := assessedRevision(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "collection", Name: "bundle"},
				ContentDigest: store.RevisionDigest(contentDigest),
			})
			revision.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: contentDigest, Reason: "test", AppliedAt: time.Now().UTC()}
			_ = json.NewEncoder(w).Encode(revision)
		default:
			if strings.HasPrefix(r.URL.Path, "/api/v1/manifests/") {
				manifestRequests++
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	resolver := &Resolver{HTTPClient: server.Client()}
	_, err := resolver.PrepareLocator(context.Background(), config.MarketplaceEntry{URL: server.URL, InstanceID: "mp"}, MarketplaceLocator{Source: "src", Kind: "collection", Name: "bundle"}, nil)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("PrepareLocator error = %v, want revoked collection error", err)
	}
	if manifestRequests != 0 {
		t.Fatalf("revoked collection caused %d manifest requests", manifestRequests)
	}
}

func TestClientGetCollectionManifestRejectsTamperedManifest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	payload, manifestDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion:    hccartifact.CollectionSchemaV1,
		Name:             "bundle",
		Members:          []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "review", SourceID: "src", Revision: digest}},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload[len(payload)-1] = ' '
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.GetCollectionManifest(context.Background(), manifestDigest); err == nil {
		t.Fatal("tampered collection manifest was accepted")
	}
}
