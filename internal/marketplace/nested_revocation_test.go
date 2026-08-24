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
	marketresolver "github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestPrepareLocatorRejectsRevokedNestedCollectionBeforeManifestFetch(t *testing.T) {
	nestedDigest := "sha256:" + strings.Repeat("b", 64)
	rootPayload, rootDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion: hccartifact.CollectionSchemaV1,
		Name:          "bundle",
		Members: []hccartifact.CollectionMemberEntry{{
			Kind:     "collection",
			Name:     "inner",
			SourceID: "nested",
			Revision: nestedDigest,
		}},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	var nestedManifestRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp")
		case "/api/v1/sources/src":
			writeTestSource(w, store.Source{ID: "src", Slug: "src", Origins: []string{"https://example.com/root"}, Ref: "main", Roots: []store.SourceRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
		case "/api/v1/sources/nested":
			writeTestSource(w, store.Source{ID: "nested", Slug: "nested", Origins: []string{"https://example.com/nested"}, Ref: "main", Roots: []store.SourceRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
		case "/api/v1/resources/src/collection/bundle":
			_ = json.NewEncoder(w).Encode(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "collection", Name: "bundle"},
				ContentDigest: store.RevisionDigest(rootDigest),
			})
		case "/api/v1/resources/nested/collection/inner/revisions/" + nestedDigest:
			_ = json.NewEncoder(w).Encode(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "nested", Kind: "collection", Name: "inner"},
				ContentDigest: store.RevisionDigest(nestedDigest),
				Revocation:    &store.Revocation{Kind: store.RevocationRevision, TargetID: "mp/nested/collection/inner@" + nestedDigest, AppliedAt: time.Now().UTC()},
			})
		default:
			if strings.HasPrefix(r.URL.Path, "/api/v1/manifests/"+nestedDigest) {
				nestedManifestRequests++
			}
			switch {
			case strings.HasPrefix(r.URL.Path, "/api/v1/manifests/"+rootDigest):
				_, _ = w.Write(rootPayload)
			default:
				http.NotFound(w, r)
			}
		}
	}))
	defer server.Close()

	resolver := &Resolver{HTTPClient: server.Client()}
	_, err = resolver.PrepareLocator(context.Background(), config.MarketplaceEntry{URL: server.URL, InstanceID: "mp"}, MarketplaceLocator{Source: "src", Kind: "collection", Name: "bundle"}, nil)
	if err == nil || !strings.Contains(err.Error(), "nested collection") || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("PrepareLocator error = %v, want revoked nested collection error", err)
	}
	if nestedManifestRequests != 0 {
		t.Fatalf("revoked nested collection caused %d manifest requests", nestedManifestRequests)
	}
}

func TestResolveChecksExpandedCollectionRootBeforeSkipping(t *testing.T) {
	rootDigest := "sha256:" + strings.Repeat("a", 64)
	leafDigest := "sha256:" + strings.Repeat("b", 64)
	archiveDigest := "sha256:" + strings.Repeat("c", 64)
	rootTarget := "mp/src/collection/bundle@" + rootDigest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp")
		case "/api/v1/resources/src/collection/bundle/revisions/" + rootDigest:
			_ = json.NewEncoder(w).Encode(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "collection", Name: "bundle"},
				ContentDigest: store.RevisionDigest(rootDigest),
				Revocation:    &store.Revocation{Kind: store.RevocationRevision, TargetID: rootTarget, Reason: "test", AppliedAt: time.Now().UTC()},
			})
		case "/api/v1/resources/src/skill/review/revisions/" + leafDigest:
			_ = json.NewEncoder(w).Encode(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
				ContentDigest: store.RevisionDigest(leafDigest),
				ArchiveDigest: store.ArchiveDigest(archiveDigest),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{
		{SourceID: "src", Kind: "collection", Name: "bundle", Revision: rootDigest, Digest: rootDigest},
		{SourceID: "src", Kind: "skill", Name: "review", Revision: leafDigest, Digest: leafDigest,
			SelectedBy: config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{SourceID: "src", Name: "bundle", Revision: rootDigest}})},
	}}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}

	r := &Resolver{HTTPClient: server.Client(), Cache: NewCache(t.TempDir())}
	resolved, errs := r.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, marketresolver.MarketplaceResolveModeInspect)
	if len(resolved) != 1 || len(errs) != 1 || !strings.Contains(errs[0].Error(), "revision is revoked") {
		t.Fatalf("without override resolved/errors = %d/%v, want leaf plus root revocation error", len(resolved), errs)
	}

	override, err := NewRevocationOverridePolicy([]string{rootTarget})
	if err != nil {
		t.Fatal(err)
	}
	resolved, errs = (&Resolver{HTTPClient: server.Client(), Cache: NewCache(t.TempDir())}).ResolveWithPolicy(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, marketresolver.MarketplaceResolveModeInspect, override)
	if len(errs) != 0 || len(resolved) != 1 || resolved[0].Name != "review" {
		t.Fatalf("with exact root override resolved/errors = %#v/%v, want leaf only", resolved, errs)
	}
}
