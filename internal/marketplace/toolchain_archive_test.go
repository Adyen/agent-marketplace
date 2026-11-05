package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var toolchainFixtureVersions = []string{"go1.26", "go1.27"}

// readToolchainFixture returns the final and placeholder archives recorded by one
// toolchain, with the manifest of the final archive.
func readToolchainFixture(t *testing.T, goMajorMinor string) ([]byte, []byte, hccartifact.CanonicalManifest) {
	t.Helper()
	dir := filepath.Join("..", "..", "pkg", "harness-compilation", "artifact", "testdata", "toolchain", goMajorMinor)
	raw, err := os.ReadFile(filepath.Join(dir, "mcpserver.tar.gz"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	placeholder, err := os.ReadFile(filepath.Join(dir, "mcpserver.placeholder.tar.gz"))
	if err != nil {
		t.Fatalf("read placeholder fixture: %v", err)
	}
	a, _, err := hccartifact.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode fixture %s: %v", goMajorMinor, err)
	}
	return raw, placeholder, a.Manifest
}

// currentToolchain returns the go major.minor version of the test binary.
func currentToolchain() string {
	return strings.Join(strings.SplitN(runtime.Version(), ".", 3)[:2], ".")
}

// servePlaceholderRoute answers the placeholder route for archiveDigest only.
func servePlaceholderRoute(mux *http.ServeMux, placeholder []byte, archiveDigest string) {
	mux.HandleFunc("/api/v1/archives/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/"+strings.TrimPrefix(archiveDigest, "sha256:")) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
		_, _ = w.Write(placeholder)
	})
}

// serveToolchainFixtureResource serves one mcpserver revision whose archive is the
// final fixture. withPlaceholder controls whether the placeholder route exists;
// without it the server behaves like one that predates the placeholder route.
func serveToolchainFixtureResource(t *testing.T, raw, placeholder []byte, manifest hccartifact.CanonicalManifest, withPlaceholder bool) *httptest.Server {
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
	if withPlaceholder {
		servePlaceholderRoute(mux, placeholder, manifest.ArchiveDigest)
	}
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

func toolchainFixtureConfig(url string, manifest hccartifact.CanonicalManifest) *config.Config {
	return &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: url, InstanceID: "mp", Source: config.UserPersonal,
		Resources: []config.MarketplaceResourcePin{toolchainFixturePin(manifest)},
	}}}
}

// spec: marketplace-api/client-verifies-placeholder-archives-by-hash
// spec: marketplace-api/locks-written-under-any-toolchain-stay-valid
func TestResolverVerifiesArtifactRecordedByOtherGoToolchain(t *testing.T) {
	for _, goMajorMinor := range toolchainFixtureVersions {
		t.Run("recorded-by-"+goMajorMinor, func(t *testing.T) {
			raw, placeholder, manifest := readToolchainFixture(t, goMajorMinor)
			server := serveToolchainFixtureResource(t, raw, placeholder, manifest, true)
			cfg := toolchainFixtureConfig(server.URL, manifest)

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

// spec: marketplace-api/client-verifies-placeholder-archives-by-hash
func TestResolverRefreshesArchiveEvidenceForArtifactRecordedByOtherGoToolchain(t *testing.T) {
	for _, goMajorMinor := range toolchainFixtureVersions {
		t.Run("recorded-by-"+goMajorMinor, func(t *testing.T) {
			raw, placeholder, manifest := readToolchainFixture(t, goMajorMinor)
			server := serveToolchainFixtureResource(t, raw, placeholder, manifest, true)
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

// spec: marketplace-api/client-verifies-placeholder-archives-by-hash
func TestResolverVerifiesCollectionMemberRecordedByOtherGoToolchain(t *testing.T) {
	for _, goMajorMinor := range toolchainFixtureVersions {
		t.Run("recorded-by-"+goMajorMinor, func(t *testing.T) {
			raw, placeholder, manifest := readToolchainFixture(t, goMajorMinor)
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
			servePlaceholderRoute(mux, placeholder, manifest.ArchiveDigest)
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

// spec: marketplace-api/fallback-to-the-artifact-route-only-on-not-found
// spec: marketplace-api/unreproducible-legacy-archive-reports-an-encoding-mismatch
func TestResolverOldServerWithoutPlaceholderRoute(t *testing.T) {
	for _, goMajorMinor := range toolchainFixtureVersions {
		t.Run("recorded-by-"+goMajorMinor, func(t *testing.T) {
			raw, placeholder, manifest := readToolchainFixture(t, goMajorMinor)
			server := serveToolchainFixtureResource(t, raw, placeholder, manifest, false)
			cfg := toolchainFixtureConfig(server.URL, manifest)

			r := &Resolver{Cache: NewCache(t.TempDir())}
			resolved, errs := r.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeRefresh)
			if goMajorMinor == currentToolchain() {
				if len(errs) != 0 || len(resolved) != 1 {
					t.Fatalf("native archive from old server: artifacts=%d errors=%v", len(resolved), errs)
				}
				return
			}
			if len(resolved) != 0 || len(errs) != 1 {
				t.Fatalf("foreign archive from old server: artifacts=%d errors=%v", len(resolved), errs)
			}
			var diagnostic *ArchiveEncodingMismatchError
			if !errors.As(errs[0], &diagnostic) || diagnostic.Cached {
				t.Fatalf("foreign archive error = %v, want a non-cached *ArchiveEncodingMismatchError", errs[0])
			}
			for _, want := range []string{"does not serve the placeholder archive", runtime.Version()} {
				if !strings.Contains(errs[0].Error(), want) {
					t.Fatalf("diagnostic %q does not contain %q", errs[0], want)
				}
			}
		})
	}
}

// spec: marketplace-api/cached-unreproducible-archive-is-refetched-online
func TestResolverMigratesStaleCachedFinalArchive(t *testing.T) {
	for _, goMajorMinor := range toolchainFixtureVersions {
		if goMajorMinor == currentToolchain() {
			continue
		}
		t.Run("recorded-by-"+goMajorMinor, func(t *testing.T) {
			raw, placeholder, manifest := readToolchainFixture(t, goMajorMinor)
			server := serveToolchainFixtureResource(t, raw, placeholder, manifest, true)
			cfg := toolchainFixtureConfig(server.URL, manifest)

			cache := NewCache(t.TempDir())
			if _, err := cache.WriteArtifact(manifest.ArchiveDigest, raw); err != nil {
				t.Fatal(err)
			}
			if err := cache.MarkArtifactVerified(manifest.ArchiveDigest); err != nil {
				t.Fatal(err)
			}

			resolved, errs := (&Resolver{Cache: cache}).Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeRefresh)
			if len(errs) != 0 || len(resolved) != 1 {
				t.Fatalf("refresh over stale cache: artifacts=%d errors=%v", len(resolved), errs)
			}
			cached, verified, err := cache.readArchive(placeholderForm, manifest.ArchiveDigest)
			if err != nil || !verified || !bytes.Equal(cached, placeholder) {
				t.Fatalf("placeholder cache after refresh: verified=%v err=%v, want the placeholder bytes", verified, err)
			}
			if legacy, legacyVerified, err := cache.ReadArtifact(manifest.ArchiveDigest); err != nil || legacyVerified {
				t.Fatalf("legacy artifact cache after refresh: verified=%v err=%v, want the stale copy not verified", legacyVerified, err)
			} else if legacy != nil && bytes.Equal(legacy, placeholder) {
				t.Fatal("placeholder bytes were written to the legacy artifact path")
			}

			// Seed the revision metadata online, then replace the artifact with the stale
			// final archive, as a cache written by another encoder would hold it.
			offline := NewCache(t.TempDir())
			if _, errs := (&Resolver{Cache: offline}).Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeRefresh); len(errs) != 0 {
				t.Fatalf("seed offline cache: %v", errs)
			}
			if err := os.RemoveAll(filepath.Dir(offline.archivePath(placeholderForm, manifest.ArchiveDigest))); err != nil {
				t.Fatal(err)
			}
			if _, err := offline.WriteArtifact(manifest.ArchiveDigest, raw); err != nil {
				t.Fatal(err)
			}
			if err := offline.MarkArtifactVerified(manifest.ArchiveDigest); err != nil {
				t.Fatal(err)
			}
			_, errs = (&Resolver{Cache: offline}).Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeCachedNoFetch)
			var diagnostic *ArchiveEncodingMismatchError
			if len(errs) != 1 || !errors.As(errs[0], &diagnostic) || !diagnostic.Cached {
				t.Fatalf("cached-no-fetch over stale cache errors = %v, want a cached *ArchiveEncodingMismatchError", errs)
			}
			if !strings.Contains(errs[0].Error(), "cached copy") || !strings.Contains(errs[0].Error(), "agent-marketplace sync") {
				t.Fatalf("cached diagnostic %q lacks the cached-copy remediation", errs[0])
			}
		})
	}
}

// spec: marketplace-api/fallback-to-the-artifact-route-only-on-not-found
func TestResolverDoesNotFallBackFromGonePlaceholderRoute(t *testing.T) {
	raw, _, manifest := readToolchainFixture(t, toolchainFixtureVersions[0])
	artifactFetches := 0
	mux := http.NewServeMux()
	serveWellKnown(mux, "mp")
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "mcpserver", Name: "genai-docs"},
			ContentDigest: store.RevisionDigest(manifest.ContentDigest),
			ArchiveDigest: store.ArchiveDigest(manifest.ArchiveDigest),
		}))
	})
	mux.HandleFunc("/api/v1/archives/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "revoked", http.StatusGone)
	})
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		artifactFetches++
		_, _ = w.Write(raw)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	_, errs := (&Resolver{Cache: NewCache(t.TempDir())}).Resolve(context.Background(), toolchainFixtureConfig(server.URL, manifest), []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "410") {
		t.Fatalf("gone placeholder route errors = %v, want a 410 failure", errs)
	}
	if artifactFetches != 0 {
		t.Fatalf("410 from the placeholder route fell back to the artifact route %d times", artifactFetches)
	}
}
