package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var toolchainFixtureVersions = []string{"go1.26", "go1.27"}

func readToolchainFixture(t *testing.T, goMajorMinor string) ([]byte, hccartifact.CanonicalManifest) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "pkg", "harness-compilation", "artifact", "testdata", "toolchain", goMajorMinor, "mcpserver.tar.gz"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	a, _, err := hccartifact.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode fixture %s: %v", goMajorMinor, err)
	}
	return raw, a.Manifest
}

func serveToolchainFixtureResource(t *testing.T, raw []byte, manifest hccartifact.CanonicalManifest) *httptest.Server {
	t.Helper()
	rev := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "mcpserver", Name: "genai-docs"},
		ContentDigest: store.RevisionDigest(manifest.ContentDigest),
		ArchiveDigest: store.ArchiveDigest(manifest.ArchiveDigest),
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rev)
	})
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
		_, _ = w.Write(raw)
	})
	serveWellKnown(mux, "mp")
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func toolchainFixturePin(manifest hccartifact.CanonicalManifest) config.MarketplaceResourcePin {
	return config.MarketplaceResourcePin{
		RegistryID: "src", Kind: "mcpserver", Name: "genai-docs",
		Revision: manifest.ContentDigest, Digest: manifest.ContentDigest,
		LockArchiveDigest: manifest.ArchiveDigest,
	}
}

func TestResolverVerifiesArtifactRecordedByOtherGoToolchain(t *testing.T) {
	for _, goMajorMinor := range toolchainFixtureVersions {
		t.Run("recorded-by-"+goMajorMinor, func(t *testing.T) {
			raw, manifest := readToolchainFixture(t, goMajorMinor)
			server := serveToolchainFixtureResource(t, raw, manifest)
			cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
				URL: server.URL, InstanceID: "mp", Source: config.UserPersonal,
				Resources: []config.MarketplaceResourcePin{toolchainFixturePin(manifest)},
			}}}

			r := &Resolver{Cache: NewCache(t.TempDir())}
			resolved, errs := r.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeRefresh)
			if len(errs) != 0 || len(resolved) != 1 {
				t.Fatalf("resolve artifacts=%d errors=%v", len(resolved), errs)
			}
			if resolved[0].MarketplaceArchiveDigest != manifest.ArchiveDigest {
				t.Fatalf("resolved archive digest %q, want locked %q", resolved[0].MarketplaceArchiveDigest, manifest.ArchiveDigest)
			}
		})
	}
}

func TestResolverRefreshesArchiveEvidenceForArtifactRecordedByOtherGoToolchain(t *testing.T) {
	for _, goMajorMinor := range toolchainFixtureVersions {
		t.Run("recorded-by-"+goMajorMinor, func(t *testing.T) {
			raw, manifest := readToolchainFixture(t, goMajorMinor)
			server := serveToolchainFixtureResource(t, raw, manifest)
			entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Source: config.UserPersonal}

			archiveDigest, err := (&Resolver{Cache: NewCache(t.TempDir())}).RefreshArchiveEvidence(context.Background(), entry, toolchainFixturePin(manifest))
			if err != nil {
				t.Fatalf("RefreshArchiveEvidence: %v", err)
			}
			if archiveDigest != manifest.ArchiveDigest {
				t.Fatalf("refreshed archive digest %q, want %q", archiveDigest, manifest.ArchiveDigest)
			}
		})
	}
}

func TestResolverVerifiesCollectionMemberRecordedByOtherGoToolchain(t *testing.T) {
	for _, goMajorMinor := range toolchainFixtureVersions {
		t.Run("recorded-by-"+goMajorMinor, func(t *testing.T) {
			raw, manifest := readToolchainFixture(t, goMajorMinor)
			payload, manifestDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
				SchemaVersion: hccartifact.CollectionSchemaV1,
				Name:          "bundle",
				Members: []hccartifact.CollectionMemberEntry{{
					Kind: "mcpserver", Name: "genai-docs", RegistryID: "src", Revision: manifest.ContentDigest,
				}},
				SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
			})
			if err != nil {
				t.Fatal(err)
			}
			collectionRev := assessedRevision(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "collection", Name: "bundle"},
				ContentDigest: store.RevisionDigest(manifestDigest),
			})
			leafRev := assessedRevision(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "mcpserver", Name: "genai-docs"},
				ContentDigest: store.RevisionDigest(manifest.ContentDigest),
				ArchiveDigest: store.ArchiveDigest(manifest.ArchiveDigest),
			})
			mux := http.NewServeMux()
			serveWellKnown(mux, "mp")
			mux.HandleFunc("/api/v1/registries/src", func(w http.ResponseWriter, r *http.Request) {
				writeTestRegistry(w, store.Registry{ID: "src", Slug: "src", Origins: []string{"https://example.com/repo"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
			})
			mux.HandleFunc("/api/v1/resources/src/collection/bundle", func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(collectionRev)
			})
			mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(leafRev)
			})
			mux.HandleFunc("/api/v1/manifests/"+manifestDigest, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(payload)
			})
			mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
				_, _ = w.Write(raw)
			})
			server := httptest.NewServer(mux)
			defer server.Close()

			entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp"}
			r := &Resolver{Cache: NewCache(t.TempDir())}
			prepared, err := r.PrepareLocator(context.Background(), entry, MarketplaceLocator{Registry: "src", Kind: "collection", Name: "bundle"}, []hcc.Target{hcc.TargetClaude})
			if err != nil {
				t.Fatalf("PrepareLocator: %v", err)
			}
			if len(prepared.CollectionMembers) != 1 {
				t.Fatalf("collection members = %d, want 1", len(prepared.CollectionMembers))
			}
			if got := prepared.CollectionMembers[0].ArchiveDigest; got != manifest.ArchiveDigest {
				t.Fatalf("member archive digest %q, want %q", got, manifest.ArchiveDigest)
			}
		})
	}
}
