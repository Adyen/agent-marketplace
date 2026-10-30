package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type nestedCollectionFixture struct {
	payloads map[string][]byte
	names    map[string]string
	requests map[string]*atomic.Int32
}

func (f *nestedCollectionFixture) add(t *testing.T, manifest hccartifact.CollectionManifest) string {
	t.Helper()
	manifest.SchemaVersion = hccartifact.CollectionSchemaV1
	manifest.SupportedSchemas = []string{hccartifact.CollectionSchemaV1}
	payload, digest, err := hccartifact.EncodeCollectionManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	f.payloads[digest], f.names[digest], f.requests[digest] = payload, manifest.Name, &atomic.Int32{}
	return digest
}

func (f *nestedCollectionFixture) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp")
		case r.URL.Path == "/api/v1/registries/src" || r.URL.Path == "/api/v1/registries/nested":
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/registries/")
			writeTestRegistry(w, store.Registry{ID: store.RegistryID(id), Slug: id, Origins: []string{"https://example.com/" + id}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror})
		case r.URL.Path == "/api/v1/resources/src/collection/bundle":
			for digest, name := range f.names {
				if name == "bundle" {
					_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "collection", Name: "bundle"}, ContentDigest: store.RevisionDigest(digest)}))
				}
			}
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/nested/collection/"):
			parts := strings.Split(r.URL.Path, "/")
			digest := parts[len(parts)-1]
			f.requests[digest].Add(1)
			_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "nested", Kind: "collection", Name: f.names[digest]}, ContentDigest: store.RevisionDigest(digest)}))
		case strings.HasPrefix(r.URL.Path, "/api/v1/manifests/"):
			digest := strings.TrimPrefix(r.URL.Path, "/api/v1/manifests/")
			if counter := f.requests[digest]; counter != nil && f.names[digest] != "bundle" {
				counter.Add(1)
			}
			_, _ = w.Write(f.payloads[digest])
		default:
			http.NotFound(w, r)
		}
	}))
}

func newNestedCollectionFixture() *nestedCollectionFixture {
	return &nestedCollectionFixture{payloads: map[string][]byte{}, names: map[string]string{}, requests: map[string]*atomic.Int32{}}
}

func nestedMember(name, digest string) hccartifact.CollectionMemberEntry {
	return hccartifact.CollectionMemberEntry{Kind: "collection", Name: name, RegistryID: "nested", Revision: digest}
}

// spec: marketplace-delivery-security/collection-preparation-fetches-each-nested-collection-once
func TestVerifyNestedCollectionsFetchesSharedCollectionOnce(t *testing.T) {
	f := newNestedCollectionFixture()
	inner := f.add(t, hccartifact.CollectionManifest{Name: "inner", Members: []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "nested", Revision: "sha256:" + strings.Repeat("a", 64)}}})
	left := f.add(t, hccartifact.CollectionManifest{Name: "left", Members: []hccartifact.CollectionMemberEntry{nestedMember("inner", inner)}})
	right := f.add(t, hccartifact.CollectionManifest{Name: "right", Members: []hccartifact.CollectionMemberEntry{nestedMember("inner", inner)}})
	root := hccartifact.CollectionManifest{SchemaVersion: hccartifact.CollectionSchemaV1, SupportedSchemas: []string{hccartifact.CollectionSchemaV1}, Name: "root", Members: []hccartifact.CollectionMemberEntry{nestedMember("left", left), nestedMember("right", right)}}
	server := f.server()
	defer server.Close()
	client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	cache := NewCache(t.TempDir())
	resolver := &Resolver{HTTPClient: server.Client(), Cache: cache}
	nested := map[string]hccartifact.CollectionManifest{}
	if err := resolver.verifyNestedCollectionManifests(context.Background(), client, cache, config.MarketplaceEntry{URL: server.URL, InstanceID: "mp"}, root, map[string]bool{"root": true}, nested, &collectionPreparationBudget{}, nil); err != nil {
		t.Fatalf("verify nested collections: %v", err)
	}
	if got := f.requests[inner].Load(); got != 2 {
		t.Fatalf("shared nested collection requests = %d, want 2 (one revision, one manifest)", got)
	}
	if _, ok := nested[inner]; !ok || len(nested) != 3 {
		t.Fatalf("verified nested manifests = %d, want left, right and inner", len(nested))
	}
}

// spec: marketplace-delivery-security/collection-preparation-stays-within-the-lock-bootstrap-limit
func TestPrepareLocatorAppliesLockBootstrapWorkLimit(t *testing.T) {
	// 120 collections each list the same 120 nested collections: only 240
	// unique collections, but 14,520 traversal edges exceed the limit.
	const width = 120
	if width+width*width <= lockfile.MaxMarketplaceLockResources {
		t.Fatalf("fixture of %d edges does not exceed the %d limit", width+width*width, lockfile.MaxMarketplaceLockResources)
	}
	f := newNestedCollectionFixture()
	leaves := make([]hccartifact.CollectionMemberEntry, width)
	for i := range leaves {
		name := fmt.Sprintf("leaf-%03d", i)
		leaves[i] = nestedMember(name, f.add(t, hccartifact.CollectionManifest{Name: name, Members: []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "nested", Revision: "sha256:" + strings.Repeat("a", 64)}}}))
	}
	middles := make([]hccartifact.CollectionMemberEntry, width)
	for i := range middles {
		name := fmt.Sprintf("middle-%03d", i)
		middles[i] = nestedMember(name, f.add(t, hccartifact.CollectionManifest{Name: name, Members: leaves}))
	}
	f.add(t, hccartifact.CollectionManifest{Name: "bundle", Members: middles})
	server := f.server()
	defer server.Close()
	resolver := &Resolver{HTTPClient: server.Client(), Cache: NewCache(t.TempDir())}
	_, err := resolver.PrepareLocator(context.Background(), config.MarketplaceEntry{URL: server.URL, InstanceID: "mp"}, MarketplaceLocator{Registry: "src", Kind: "collection", Name: "bundle"}, nil)
	if err == nil || !strings.Contains(err.Error(), "resource limit") {
		t.Fatalf("PrepareLocator err = %v, want the lock-bootstrap resource limit", err)
	}
}
