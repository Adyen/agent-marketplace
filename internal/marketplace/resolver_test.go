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
	contentDigest := artifact.Manifest.ContentDigest
	sourceRef := "refs/heads/main"
	artifact.Manifest.SourceID = "src"
	artifact.Manifest.Revision = sourceRef
	raw, archiveDigest, err := hccartifact.Encode(artifact)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

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
						Revision: sourceRef,
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
	if art.Ref != sourceRef {
		t.Fatalf("ref = %q, want %q", art.Ref, sourceRef)
	}
	if art.Resource == nil {
		t.Fatalf("expected Resource to be populated")
	}
	if art.MarketplaceURL != server.URL {
		t.Fatalf("marketplace URL = %q, want %q", art.MarketplaceURL, server.URL)
	}
	if art.Path != cache.ArtifactPath(archiveDigest) {
		t.Fatalf("artifact path = %q, want archive-digest cache path %q", art.Path, cache.ArtifactPath(archiveDigest))
	}

	// Resolve again in cached mode should only verify endpoint identity.
	before := requestCount
	resolved2, errs := r.Resolve(ctx, cfg, []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeCached)
	if len(errs) > 0 {
		t.Fatalf("Cached Resolve errors: %v", errs)
	}
	if len(resolved2) != 1 {
		t.Fatalf("expected 1 resolved artifact in cached mode, got %d", len(resolved2))
	}
	if requestCount != before+1 {
		t.Fatalf("cached resolve made %d network requests, expected only well-known verification", requestCount-before)
	}

	before = requestCount
	if _, errs := r.Resolve(ctx, cfg, []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeRefresh); len(errs) > 0 {
		t.Fatalf("Refresh Resolve errors: %v", errs)
	}
	if requestCount <= before {
		t.Fatalf("refresh resolve did not refresh metadata")
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
	serveWellKnown(mux, "mp")
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
	serveWellKnown(mux, "mp")
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
	mux := http.NewServeMux()
	serveWellKnown(mux, "mp")
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

func TestResolverValidatesRevisionIdentity(t *testing.T) {
	digest := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	archive := store.ArchiveDigest("sha256:" + strings.Repeat("b", 64))
	entry := config.MarketplaceEntry{InstanceID: "mp"}
	pin := config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review", Revision: "refs/heads/main", Digest: string(digest)}
	if err := validatePin(entry, pin); err != nil {
		t.Fatalf("opaque pin revision rejected: %v", err)
	}
	valid := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
		ContentDigest: digest,
		ArchiveDigest: archive,
	}
	if err := validateResourceRevision(entry, pin, valid); err != nil {
		t.Fatalf("valid revision rejected: %v", err)
	}

	invalid := valid
	invalid.Coordinate.Marketplace = "other"
	if err := validateResourceRevision(entry, pin, invalid); err == nil || !strings.Contains(err.Error(), "coordinate mismatch") {
		t.Fatalf("invalid coordinate error = %v", err)
	}

	invalid = valid
	invalid.ContentDigest = store.RevisionDigest("sha256:" + strings.Repeat("c", 64))
	if err := validateResourceRevision(entry, pin, invalid); err == nil || !strings.Contains(err.Error(), "content digest mismatch") {
		t.Fatalf("invalid content digest error = %v", err)
	}
}

func TestResolverValidatesArtifactManifestIdentity(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	archive := "sha256:" + strings.Repeat("b", 64)
	entry := config.MarketplaceEntry{InstanceID: "mp"}
	pin := config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review", Revision: "refs/heads/main", Digest: digest}
	rev := store.ResourceRevision{ArchiveDigest: store.ArchiveDigest(archive)}
	artifact := hccartifact.CanonicalArtifact{}
	artifact.Manifest.Kind = "skill"
	artifact.Manifest.Name = "review"
	artifact.Manifest.SourceID = "src"
	artifact.Manifest.Revision = pin.Revision
	artifact.Manifest.ContentDigest = digest
	artifact.Manifest.ArchiveDigest = archive
	if err := validateArtifactManifest(entry, pin, rev, artifact); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}

	artifact.Manifest.Name = "other"
	if err := validateArtifactManifest(entry, pin, rev, artifact); err == nil || !strings.Contains(err.Error(), "name mismatch") {
		t.Fatalf("invalid manifest name error = %v", err)
	}
}

func TestResolverRejectsMarketplaceInstanceMismatchBeforePins(t *testing.T) {
	var resourceRequests int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		resourceRequests++
	})
	serveWellKnown(mux, "actual")
	server := httptest.NewServer(mux)
	defer server.Close()

	digest := "sha256:" + strings.Repeat("a", 64)
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL:        server.URL,
		InstanceID: "configured",
		Resources: []config.MarketplaceResourcePin{{
			SourceID: "src",
			Kind:     "skill",
			Name:     "review",
			Revision: "refs/heads/main",
			Digest:   digest,
		}},
	}}}

	r := &Resolver{Cache: NewCache(t.TempDir())}
	_, errs := r.Resolve(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "instance ID mismatch") {
		t.Fatalf("Resolve errors = %v, want instance mismatch", errs)
	}
	if resourceRequests != 0 {
		t.Fatalf("resource endpoint received %d requests after identity mismatch", resourceRequests)
	}
}

func serveWellKnown(mux *http.ServeMux, instanceID string) {
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"instance_id": instanceID})
	})
}

func TestResolverImplementsInterface(t *testing.T) {
	var _ resolver.MarketplaceResolver = &Resolver{}
}
