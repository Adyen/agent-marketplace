package marketplace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// TestInspectBucketsCollectionRevisionFailureByAcquisitionFacts pins the
// bucketing contract for expanded collection roots: a revision-check failure
// with no cached acquisition facts is only drift-incomplete — the cache
// cannot prove the collection was ever installed — while the same failure
// with cached facts stays a hard resolve error.
func TestInspectBucketsCollectionRevisionFailureByAcquisitionFacts(t *testing.T) {
	rootDigest := "sha256:" + strings.Repeat("a", 64)
	leafDigest := "sha256:" + strings.Repeat("b", 64)
	leafRevision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "leaf-source", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(leafDigest), ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("c", 64)),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp")
		case "/api/v1/resources/leaf-source/skill/review/revisions/" + leafDigest:
			_ = json.NewEncoder(w).Encode(leafRevision)
		default:
			// The expanded collection revision check fails at the endpoint.
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	newInspection := func(cache *Cache) Inspection {
		entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{
			{RegistryID: "root-source", Kind: "collection", Name: "bundle", Revision: rootDigest, Digest: rootDigest},
			{RegistryID: "leaf-source", Kind: "skill", Name: "review", Revision: leafDigest, Digest: leafDigest,
				SelectedBy: config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{RegistryID: "root-source", Name: "bundle", Revision: rootDigest}})},
		}}
		cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}
		return (&Resolver{HTTPClient: server.Client(), Cache: cache}).Inspect(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	}

	t.Run("no cached acquisition facts stays soft", func(t *testing.T) {
		// A never-acquired or fully wiped collection root leaves no cached
		// facts, so its failed revision proof cannot be hard.
		inspection := newInspection(NewCache(t.TempDir()))
		if len(inspection.ResolveErrors) != 0 {
			t.Fatalf("collection revision failure without facts produced hard errors = %v, want drift-incomplete", inspection.ResolveErrors)
		}
		found := false
		for _, incomplete := range inspection.DriftIncomplete {
			if incomplete.Kind == "collection" && incomplete.Name == "bundle" {
				found = true
				if incomplete.Err == nil || !strings.Contains(incomplete.Err.Error(), "bundle") {
					t.Fatalf("drift-incomplete collection error = %v, want the revision failure", incomplete.Err)
				}
			}
		}
		if !found {
			t.Fatalf("drift-incomplete = %v, want the failed collection root", inspection.DriftIncomplete)
		}
	})

	t.Run("cached acquisition facts stay hard", func(t *testing.T) {
		cache := NewCache(t.TempDir())
		rootRevision := assessedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "root-source", Kind: "collection", Name: "bundle"},
			ContentDigest: store.RevisionDigest(rootDigest),
		})
		if _, err := cache.WriteResourceRevision(server.URL, "root-source", "collection", "bundle", rootDigest, rootRevision); err != nil {
			t.Fatal(err)
		}
		inspection := newInspection(cache)
		if len(inspection.ResolveErrors) == 0 {
			t.Fatalf("collection revision failure with cached facts produced no hard error, drift-incomplete = %v", inspection.DriftIncomplete)
		}
		for _, incomplete := range inspection.DriftIncomplete {
			if incomplete.Kind == "collection" && incomplete.Name == "bundle" {
				t.Fatalf("cached-fact collection failure was softened: %v", incomplete.Err)
			}
		}
	})
}
