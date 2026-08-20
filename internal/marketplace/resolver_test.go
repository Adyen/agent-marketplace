package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestResolverMirrorRoundTrip(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cache := NewCache(tmp)

	skill := ir.NewSkill("review", "Review code", "Review the code carefully.", []string{"user"}, nil, ir.SourceRef{})
	artifact, err := hccartifact.FromIR(skill)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	raw, archiveDigest, err := hccartifact.Encode(artifact)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	contentDigest := artifact.Manifest.ContentDigest

	rev := store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID("mp"),
			Source:      store.SourceID("src"),
			Kind:        "skill",
			Name:        "review",
		},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	}

	var requestCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if !strings.HasSuffix(r.URL.Path, "/revisions/"+contentDigest) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rev)
	})
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		expected := "sha256/" + strings.TrimPrefix(archiveDigest, "sha256:")
		if !strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"+expected) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
		_, _ = w.Write(raw)
	})
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"instance_id":   "mp",
			"canonical_url": "",
			"api_base":      "",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	cfg := &config.Config{
		Marketplaces: []config.MarketplaceEntry{
			{
				URL:        server.URL,
				InstanceID: "mp",
				Alias:      "test",
				Resources: []config.MarketplaceResourcePin{
					{
						SourceID: "src",
						Kind:     "skill",
						Name:     "review",
						Revision: contentDigest,
						Digest:   contentDigest,
					},
				},
			},
		},
	}

	r := &Resolver{Cache: cache}
	resolved, errs := r.Resolve(ctx, cfg, []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) > 0 {
		t.Fatalf("Resolve errors: %v", errs)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved artifact, got %d", len(resolved))
	}
	art := resolved[0]
	if !art.IsMarketplace {
		t.Fatalf("expected IsMarketplace true")
	}
	if art.Name != "review" {
		t.Fatalf("name = %q, want review", art.Name)
	}
	if art.Resource == nil {
		t.Fatalf("expected Resource to be populated")
	}
	if art.MarketplaceURL != server.URL {
		t.Fatalf("marketplace URL = %q, want %q", art.MarketplaceURL, server.URL)
	}

	// Resolve again in cached mode should not hit the network.
	before := requestCount
	resolved2, errs := r.Resolve(ctx, cfg, []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeCached)
	if len(errs) > 0 {
		t.Fatalf("Cached Resolve errors: %v", errs)
	}
	if len(resolved2) != 1 {
		t.Fatalf("expected 1 resolved artifact in cached mode, got %d", len(resolved2))
	}
	if requestCount != before {
		t.Fatalf("cached resolve made %d network requests, expected 0", requestCount-before)
	}

}

func TestResolverInspect(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cache := NewCache(tmp)

	sum := sha256.Sum256([]byte("payload"))
	contentDigest := "sha256:" + hex.EncodeToString(sum[:])
	rev := store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID("mp"),
			Source:      store.SourceID("src"),
			Kind:        "skill",
			Name:        "review",
		},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest("sha256:0000000000000000000000000000000000000000000000000000000000000000"),
	}

	var artifactRequested bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rev)
	})
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		artifactRequested = true
		http.Error(w, "artifact fetch should not happen in inspect mode", http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	cfg := &config.Config{
		Marketplaces: []config.MarketplaceEntry{
			{
				URL:        server.URL,
				InstanceID: "mp",
				Resources: []config.MarketplaceResourcePin{
					{
						SourceID: "src",
						Kind:     "skill",
						Name:     "review",
						Revision: contentDigest,
						Digest:   contentDigest,
					},
				},
			},
		},
	}

	r := &Resolver{Cache: cache}
	resolved, errs := r.Resolve(ctx, cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(errs) > 0 {
		t.Fatalf("Resolve errors: %v", errs)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved artifact, got %d", len(resolved))
	}
	if !resolved[0].Unresolved {
		t.Fatalf("expected Unresolved in inspect mode")
	}
	if resolved[0].Resource != nil {
		t.Fatalf("expected nil Resource in inspect mode")
	}
	if artifactRequested {
		t.Fatalf("artifact endpoint should not be requested in inspect mode")
	}
}

func TestResolverRevoked(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cache := NewCache(tmp)

	sum := sha256.Sum256([]byte("payload"))
	contentDigest := "sha256:" + hex.EncodeToString(sum[:])
	rev := store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID("mp"),
			Source:      store.SourceID("src"),
			Kind:        "skill",
			Name:        "review",
		},
		ContentDigest: store.RevisionDigest(contentDigest),
		Revocation: &store.Revocation{
			Kind:     store.RevocationRevision,
			TargetID: contentDigest,
			Reason:   "compromised",
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rev)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	cfg := &config.Config{
		Marketplaces: []config.MarketplaceEntry{
			{
				URL:        server.URL,
				InstanceID: "mp",
				Resources: []config.MarketplaceResourcePin{
					{
						SourceID: "src",
						Kind:     "skill",
						Name:     "review",
						Revision: contentDigest,
						Digest:   contentDigest,
					},
				},
			},
		},
	}

	r := &Resolver{Cache: cache}
	_, errs := r.Resolve(ctx, cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(errs) == 0 {
		t.Fatalf("expected revocation error")
	}
	if !strings.Contains(errs[0].Error(), "revoked") && !strings.Contains(errs[0].Error(), "revocation") {
		t.Fatalf("expected revoked error, got %v", errs[0])
	}
}

func TestResolverCollectionUnsupported(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cache := NewCache(tmp)

	cfg := &config.Config{
		Marketplaces: []config.MarketplaceEntry{
			{
				URL:        "https://example.com",
				InstanceID: "mp",
				Resources: []config.MarketplaceResourcePin{
					{
						SourceID: "src",
						Kind:     "collection",
						Name:     "bundle",
						Revision: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
						Digest:   "sha256:0000000000000000000000000000000000000000000000000000000000000000",
					},
				},
			},
		},
	}

	r := &Resolver{Cache: cache}
	_, errs := r.Resolve(ctx, cfg, nil, resolver.MarketplaceResolveModeRefresh)
	if len(errs) == 0 {
		t.Fatalf("expected collection unsupported error")
	}
}

func TestResolverImplementsInterface(t *testing.T) {
	var _ resolver.MarketplaceResolver = &Resolver{}
}
