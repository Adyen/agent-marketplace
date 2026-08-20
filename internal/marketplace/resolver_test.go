package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestCanonicalMarketplaceEntryNormalizesIdentityURL(t *testing.T) {
	entry, err := canonicalMarketplaceEntry(config.MarketplaceEntry{URL: "HTTPS://MARKET.example/"})
	if err != nil {
		t.Fatalf("canonicalMarketplaceEntry: %v", err)
	}
	if entry.URL != "https://market.example" {
		t.Fatalf("canonical URL = %q, want https://market.example", entry.URL)
	}
}

func TestMarketplaceCatalogKeyIncludesInstanceID(t *testing.T) {
	pin := config.MarketplaceResourcePin{SourceID: "source", Digest: "sha256:digest", Revision: "v1"}
	first := marketplaceCatalogKey(config.MarketplaceEntry{URL: "https://market.example", InstanceID: "instance-one"}, pin)
	second := marketplaceCatalogKey(config.MarketplaceEntry{URL: "https://market.example", InstanceID: "instance-two"}, pin)
	if first == second {
		t.Fatalf("catalog keys collided across Marketplace instances: %q", first)
	}
	if !strings.Contains(first, ":instance-one:") {
		t.Fatalf("catalog key %q does not include instance ID", first)
	}
}

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
				URL:        strings.ToUpper(server.URL) + "/",
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
	if art.MarketplaceURL != server.URL || art.RegistryURL != server.URL {
		t.Fatalf("marketplace identity URLs = %q, %q, want %q", art.MarketplaceURL, art.RegistryURL, server.URL)
	}
	if strings.Contains(art.CatalogKey, strings.ToUpper(server.URL)) || !strings.Contains(art.CatalogKey, server.URL) {
		t.Fatalf("catalog key = %q, want canonical endpoint", art.CatalogKey)
	}
	if art.Path != cache.ArtifactPath(archiveDigest) {
		t.Fatalf("artifact path = %q, want archive-digest cache path %q", art.Path, cache.ArtifactPath(archiveDigest))
	}

	// An equivalent lowercase, slashless configuration must reuse the same
	// canonical metadata cache key while still verifying the instance.
	cfg.Marketplaces[0].URL = server.URL
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

func TestResolverInfersRequiredFromMarketplaceOwner(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	rev := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(digest),
		ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("b", 64)),
	}
	mux := http.NewServeMux()
	serveWellKnown(mux, "mp")
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(rev)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	for _, tt := range []struct {
		name     string
		source   config.Source
		required bool
	}{
		{name: "organization", source: config.Organization, required: true},
		{name: "user", source: config.UserPersonal, required: false},
		{name: "repo", source: config.RepoCommitted, required: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
				URL: server.URL, InstanceID: "mp", Source: tt.source,
				Resources: []config.MarketplaceResourcePin{{
					SourceID: "src", Kind: "skill", Name: "review", Revision: "refs/heads/main", Digest: digest,
				}},
			}}}
			resolved, errs := (&Resolver{Cache: NewCache(t.TempDir())}).Resolve(context.Background(), cfg, nil, resolver.MarketplaceResolveModeInspect)
			if len(errs) != 0 {
				t.Fatalf("Resolve errors: %v", errs)
			}
			if len(resolved) != 1 {
				t.Fatalf("resolved %d artifacts, want 1", len(resolved))
			}
			if resolved[0].Required != tt.required {
				t.Fatalf("Required = %t, want %t for source %s", resolved[0].Required, tt.required, tt.source)
			}
			if resolved[0].Source != tt.source {
				t.Fatalf("Source = %s, want %s", resolved[0].Source, tt.source)
			}
		})
	}
}

func TestMergeLayersStampsOrganizationMarketplaceRequired(t *testing.T) {
	const instanceID = "org-marketplace"
	digest := "sha256:" + strings.Repeat("a", 64)
	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: instanceID, Source: "source", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(digest),
		ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("b", 64)),
	}

	mux := http.NewServeMux()
	serveWellKnown(mux, instanceID)
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(revision)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	merged := config.MergeLayers([]config.Layer{
		{
			Source: config.Organization,
			Config: &config.Config{
				Marketplaces: []config.MarketplaceEntry{
					{
						URL: server.URL, InstanceID: instanceID,
						Resources: []config.MarketplaceResourcePin{
							{
								SourceID: "source", Kind: "skill", Name: "review",
								Revision: "refs/heads/main", Digest: digest,
							},
						},
					},
				},
			},
		},
	})
	if len(merged.Marketplaces) != 1 {
		t.Fatalf("merged marketplaces = %d, want one", len(merged.Marketplaces))
	}
	if merged.Marketplaces[0].Source != config.Organization {
		t.Fatalf("merged Marketplace Source = %s, want Organization", merged.Marketplaces[0].Source)
	}

	resolved, errs := (&Resolver{Cache: NewCache(t.TempDir())}).Resolve(
		context.Background(), merged, nil, resolver.MarketplaceResolveModeInspect,
	)
	if len(errs) != 0 {
		t.Fatalf("Resolve errors: %v", errs)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved %d artifacts, want one", len(resolved))
	}
	if !resolved[0].Required {
		t.Fatalf("resolved Marketplace artifact Required = false, want true")
	}
	if resolved[0].Source != config.Organization {
		t.Fatalf("resolved Marketplace Source = %s, want Organization", resolved[0].Source)
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
	pin := config.MarketplaceResourcePin{SourceID: "src", SourceSlug: "old-source-slug", Kind: "skill", Name: "review", Revision: "refs/heads/main", Digest: digest}
	rev := store.ResourceRevision{ArchiveDigest: store.ArchiveDigest(archive)}
	artifact := hccartifact.CanonicalArtifact{}
	artifact.Manifest.Kind = "skill"
	artifact.Manifest.Name = "review"
	artifact.Manifest.SourceID = "src"
	artifact.Manifest.SourceSlug = "renamed-source-slug"
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

func TestResolvePinWithArtifactRetainsVerifiedArtifactWhenCacheIsTampered(t *testing.T) {
	cache := NewCache(t.TempDir())
	digest := "sha256:" + strings.Repeat("a", 64)
	skill := ir.NewSkill("review", "Review code", "Review carefully.", []string{"user"}, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(skill)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	canonical.Manifest.SourceID = "src"
	canonical.Manifest.SourceSlug = "source"
	canonical.Manifest.Revision = digest
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	rev := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(canonical.Manifest.ContentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	}
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"}
	pin := config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review", Revision: digest, Digest: canonical.Manifest.ContentDigest}
	if _, err := cache.WriteResourceRevision(entry.URL, pin.SourceID, pin.Kind, pin.Name, pin.Digest, rev); err != nil {
		t.Fatalf("WriteResourceRevision: %v", err)
	}
	artifactPath, err := cache.WriteArtifact(archiveDigest, raw)
	if err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatalf("MarkArtifactVerified: %v", err)
	}
	client, err := NewClient(entry.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, got, err := (&Resolver{}).resolvePinWithArtifact(context.Background(), client, cache, entry, pin, nil, resolver.MarketplaceResolveModeCached)
	if err != nil {
		t.Fatalf("resolvePinWithArtifact: %v", err)
	}
	if got.Manifest.Name != "review" {
		t.Fatalf("verified artifact name = %q", got.Manifest.Name)
	}
	if err := os.WriteFile(artifactPath, []byte("tampered"), 0o644); err != nil {
		t.Fatalf("tamper artifact: %v", err)
	}
	if got.Manifest.Name != "review" || got.Manifest.ContentDigest != canonical.Manifest.ContentDigest {
		t.Fatal("prepared artifact changed after cache tampering")
	}
}

func TestPrepareLocatorPrefetchesMirrorArtifact(t *testing.T) {
	skill := ir.NewSkill("review", "Review code", "Review carefully.", []string{"user"}, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(skill)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	canonical.Manifest.SourceID = "src"
	canonical.Manifest.SourceSlug = "source"
	canonical.Manifest.Revision = "refs/heads/main"
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	rev := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(canonical.Manifest.ContentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	}
	mux := http.NewServeMux()
	serveWellKnown(mux, "mp")
	mux.HandleFunc("/api/v1/sources/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(store.Source{ID: "src", Slug: "source", Acquisition: store.AcquisitionMirror})
	})
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(rev)
	})
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(raw) })
	server := httptest.NewServer(mux)
	defer server.Close()

	plan, err := (&Resolver{Cache: NewCache(t.TempDir())}).PrepareLocator(context.Background(), config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Alias: "public"}, MarketplaceLocator{URL: server.URL, Source: "source", Kind: "skill", Name: "review"}, nil)
	if err != nil {
		t.Fatalf("PrepareLocator: %v", err)
	}
	if plan.Pin.SourceID != "src" || plan.Pin.Digest != string(rev.ContentDigest) || plan.ArchiveDigest != string(rev.ArchiveDigest) {
		t.Fatalf("prepared pin = %#v", plan.Pin)
	}
	if plan.Artifact.Manifest.Name != "review" {
		t.Fatalf("prepared artifact manifest = %#v", plan.Artifact.Manifest)
	}
}
