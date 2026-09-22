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
		Members:          []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "src", Revision: digest}},
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
		case "/api/v1/registries/src":
			writeTestRegistry(w, store.Registry{ID: "src", Slug: "src", Origins: []string{"https://example.com/repo"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
		case "/api/v1/resources/src/collection/bundle":
			revision := assessedRevision(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "collection", Name: "bundle"},
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
	_, err := resolver.PrepareLocator(context.Background(), config.MarketplaceEntry{URL: server.URL, InstanceID: "mp"}, MarketplaceLocator{Registry: "src", Kind: "collection", Name: "bundle"}, nil)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("PrepareLocator error = %v, want revoked collection error", err)
	}
	if manifestRequests != 0 {
		t.Fatalf("revoked collection caused %d manifest requests", manifestRequests)
	}
}

func TestPrepareLocatorResourceLimitRejectsCollectionBeforeFetchingLeaves(t *testing.T) {
	revision := "sha256:" + strings.Repeat("a", 64)
	payload, manifestDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion: hccartifact.CollectionSchemaV1,
		Name:          "bundle",
		Members: []hccartifact.CollectionMemberEntry{
			{Kind: "skill", Name: "first", RegistryID: "src", Revision: revision},
			{Kind: "skill", Name: "second", RegistryID: "src", Revision: revision},
		},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	var leafRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp")
		case "/api/v1/registries/src":
			writeTestRegistry(w, store.Registry{ID: "src", Slug: "src", Origins: []string{"https://example.com/repo"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
		case "/api/v1/resources/src/collection/bundle":
			_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "collection", Name: "bundle"},
				ContentDigest: store.RevisionDigest(manifestDigest),
			}))
		case "/api/v1/manifests/" + manifestDigest:
			_, _ = w.Write(payload)
		default:
			if strings.Contains(r.URL.Path, "/skill/") {
				leafRequests++
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	resolver := &Resolver{HTTPClient: server.Client(), Cache: NewCache(t.TempDir())}
	_, err = resolver.PrepareLocatorWithResourceLimit(
		context.Background(),
		config.MarketplaceEntry{URL: server.URL, InstanceID: "mp"},
		MarketplaceLocator{Registry: "src", Kind: "collection", Name: "bundle"},
		nil,
		2,
	)
	if err == nil || !strings.Contains(err.Error(), "resource limit") {
		t.Fatalf("PrepareLocatorWithResourceLimit error = %v, want resource limit", err)
	}
	if leafRequests != 0 {
		t.Fatalf("oversized collection caused %d leaf requests", leafRequests)
	}
}

func TestPrepareLocatorBoundsNestedWorkAndCachesNestedRevision(t *testing.T) {
	leafDigest := "sha256:" + strings.Repeat("a", 64)
	nestedPayload, nestedDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion: hccartifact.CollectionSchemaV1,
		Name:          "inner",
		Members: []hccartifact.CollectionMemberEntry{{
			Kind:       "skill",
			Name:       "review",
			RegistryID: "nested",
			Revision:   leafDigest,
		}},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	rootManifest := hccartifact.CollectionManifest{
		SchemaVersion: hccartifact.CollectionSchemaV1,
		Name:          "bundle",
		Members: []hccartifact.CollectionMemberEntry{{
			Kind:       "collection",
			Name:       "inner",
			RegistryID: "nested",
			Revision:   nestedDigest,
		}},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	}
	rootPayload, rootDigest, err := hccartifact.EncodeCollectionManifest(rootManifest)
	if err != nil {
		t.Fatal(err)
	}
	var nestedRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp")
		case "/api/v1/registries/src":
			writeTestRegistry(w, store.Registry{ID: "src", Slug: "src", Origins: []string{"https://example.com/src"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror})
		case "/api/v1/registries/nested":
			nestedRequests++
			writeTestRegistry(w, store.Registry{ID: "nested", Slug: "nested", Origins: []string{"https://example.com/nested"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror})
		case "/api/v1/resources/src/collection/bundle":
			_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "collection", Name: "bundle"},
				ContentDigest: store.RevisionDigest(rootDigest),
			}))
		case "/api/v1/resources/nested/collection/inner/revisions/" + nestedDigest:
			nestedRequests++
			_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "nested", Kind: "collection", Name: "inner"},
				ContentDigest: store.RevisionDigest(nestedDigest),
			}))
		case "/api/v1/manifests/" + rootDigest:
			_, _ = w.Write(rootPayload)
		case "/api/v1/manifests/" + nestedDigest:
			nestedRequests++
			_, _ = w.Write(nestedPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp"}
	locator := MarketplaceLocator{Registry: "src", Kind: "collection", Name: "bundle"}
	limited := &Resolver{HTTPClient: server.Client(), Cache: NewCache(t.TempDir())}
	if _, err := limited.PrepareLocatorWithResourceLimit(context.Background(), entry, locator, nil, 1); err == nil || !strings.Contains(err.Error(), "resource limit") {
		t.Fatalf("nested work limit error = %v, want resource limit", err)
	}
	if nestedRequests != 0 {
		t.Fatalf("nested work limit allowed %d nested requests", nestedRequests)
	}

	cache := NewCache(t.TempDir())
	resolver := &Resolver{HTTPClient: server.Client(), Cache: cache}
	client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	budget := &collectionPreparationBudget{limit: 3}
	if err := resolver.verifyNestedCollectionManifests(context.Background(), client, cache, entry, rootManifest, map[string]bool{rootDigest: true}, map[string]hccartifact.CollectionManifest{}, budget, nil); err != nil {
		t.Fatalf("verify nested collection: %v", err)
	}
	if budget.used != 1 {
		t.Fatalf("nested collection preparation work = %d, want 1", budget.used)
	}
	revision, found, err := cache.ReadResourceRevision(server.URL, "nested", "collection", "inner", nestedDigest)
	if err != nil {
		t.Fatal(err)
	}
	if !found || revision.ContentDigest != store.RevisionDigest(nestedDigest) {
		t.Fatalf("nested revision cache = found %v, revision %#v", found, revision)
	}
}

func TestClientGetCollectionManifestRejectsTamperedManifest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	payload, manifestDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion:    hccartifact.CollectionSchemaV1,
		Name:             "bundle",
		Members:          []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "src", Revision: digest}},
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
