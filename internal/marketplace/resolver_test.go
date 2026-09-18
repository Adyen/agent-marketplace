package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

func TestResolverRejectsCachedBroadRevocationWithoutOverride(t *testing.T) {
	err := enforceMarketplaceRevocation(revokedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
	}, "src"), nil)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("broad cached revocation error = %v, want revoked error", err)
	}
}

func TestPrepareLocatorRejectsAssessmentBeforeMirrorDownload(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	archive := "sha256:" + strings.Repeat("b", 64)
	artifactRequests := 0
	mux := http.NewServeMux()
	serveWellKnown(mux, "mp")
	mux.HandleFunc("/api/v1/registries/source", func(w http.ResponseWriter, _ *http.Request) {
		writeTestRegistry(w, store.Registry{ID: "src", Slug: "source", Origins: []string{"https://example.com/repo"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
	})
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(rejectedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
			ContentDigest: store.RevisionDigest(digest), ArchiveDigest: store.ArchiveDigest(archive),
		}))
	})
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, _ *http.Request) {
		artifactRequests++
		http.Error(w, "must not download", http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	_, err := (&Resolver{Cache: NewCache(t.TempDir())}).PrepareLocator(context.Background(), config.MarketplaceEntry{URL: server.URL, InstanceID: "mp"}, MarketplaceLocator{Registry: "source", Kind: "skill", Name: "review"}, nil)
	if err == nil || !strings.Contains(err.Error(), "assessment rejected") {
		t.Fatalf("PrepareLocator error = %v, want assessment rejection", err)
	}
	if artifactRequests != 0 {
		t.Fatalf("rejected assessment caused %d artifact downloads", artifactRequests)
	}
}

func TestResolverRejectsCachedAssessmentBeforeArtifactRead(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	archive := "sha256:" + strings.Repeat("b", 64)
	cache := NewCache(t.TempDir())
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"}
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: digest, Digest: digest}
	if _, err := cache.WriteResourceRevision(entry.URL, pin.RegistryID, pin.Kind, pin.Name, pin.Digest, rejectedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(digest), ArchiveDigest: store.ArchiveDigest(archive),
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteArtifact(archive, []byte("invalid but must remain unread")); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkArtifactVerified(archive); err != nil {
		t.Fatal(err)
	}
	_, errs := (&Resolver{Cache: cache}).Resolve(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: entry.URL, InstanceID: entry.InstanceID, Resources: []config.MarketplaceResourcePin{pin}}}}, nil, resolver.MarketplaceResolveModeCachedNoFetch)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "assessment rejected") {
		t.Fatalf("cached rejected assessment errors = %v", errs)
	}
}

func TestIndexOnlyResolverRejectsAssessmentBeforeRegistryAcquisition(t *testing.T) {
	commit := strings.Repeat("a", 40)
	manifest := "sha256:" + strings.Repeat("b", 64)
	digest := "sha256:" + strings.Repeat("c", 64)
	registryRequests := 0
	mux := http.NewServeMux()
	serveWellKnown(mux, "mp")
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(rejectAssessment(assessedRevisionOccurrence(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
			ContentDigest: store.RevisionDigest(digest),
		}, commit, manifest)))
	})
	mux.HandleFunc("/api/v1/registries/", func(w http.ResponseWriter, _ *http.Request) {
		registryRequests++
		http.Error(w, "must not acquire registry", http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: digest, Digest: digest, Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: commit, RegistryManifestDigest: manifest}
	_, errs := (&Resolver{Cache: NewCache(t.TempDir())}).Resolve(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}, nil, resolver.MarketplaceResolveModeRefresh)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "assessment rejected") {
		t.Fatalf("index-only rejected assessment errors = %v", errs)
	}
	if registryRequests != 0 {
		t.Fatalf("rejected assessment caused %d registry acquisition requests", registryRequests)
	}
}

func TestPreparedInstallPreviewUsesAssessmentFindings(t *testing.T) {
	digest := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	assessment := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: digest,
	})
	assessment.Assessment.AssessmentDigest = ""
	assessment.Assessment.AnalyzerVersion = "analyzer-v2"
	assessment.Assessment.Findings = []store.Finding{{Code: "policy.example", Severity: "warning", Policy: "warn", Message: "review this", Location: &store.FindingLocation{Path: "SKILL.md", Line: 4, Column: 2}}}
	finalized, err := store.FinalizeResourceAssessment(assessment.Assessment)
	if err != nil {
		t.Fatal(err)
	}
	assessment.Assessment = finalized
	preview := (&PreparedInstall{
		Pin:      config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Digest: string(digest)},
		Revision: assessment,
	}).Preview()
	if len(preview.Findings) != 1 {
		t.Fatalf("preview findings = %#v, want current assessment finding", preview.Findings)
	}
	finding := preview.Findings[0]
	if finding.Code != "policy.example" || finding.AnalyzerVersion != "analyzer-v2" || finding.File != "SKILL.md" || finding.Line != 4 || finding.Column != 2 {
		t.Fatalf("preview assessment finding = %#v", finding)
	}
}

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
	pin := config.MarketplaceResourcePin{RegistryID: "source", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Revision: "v1"}
	first := marketplaceCatalogKey(config.MarketplaceEntry{URL: "https://market.example", InstanceID: "instance-one"}, pin)
	second := marketplaceCatalogKey(config.MarketplaceEntry{URL: "https://market.example", InstanceID: "instance-two"}, pin)
	if first == second {
		t.Fatalf("catalog keys collided across Marketplace instances: %q", first)
	}
	if !strings.Contains(first, "\x00instance-one\x00") {
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
	artifact.Manifest.RegistryID = "src"
	artifact.Manifest.Revision = sourceRef
	raw, archiveDigest, err := hccartifact.Encode(artifact)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	rev := assessedRevision(store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID("mp"),
			Registry:    store.RegistryID("src"),
			Kind:        "skill",
			Name:        "review",
		},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})

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
		writeTestWellKnown(w, "mp")
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
						RegistryID: "src",
						Kind:       "skill",
						Name:       "review",
						Revision:   sourceRef,
						Digest:     contentDigest,
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

	before = requestCount
	resolved3, errs := r.Resolve(ctx, cfg, []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeCachedNoFetch)
	if len(errs) > 0 {
		t.Fatalf("CachedNoFetch Resolve errors: %v", errs)
	}
	if len(resolved3) != 1 || resolved3[0].Resource == nil {
		t.Fatalf("CachedNoFetch resolved artifacts = %#v, want one materializable artifact", resolved3)
	}
	if requestCount != before {
		t.Fatalf("CachedNoFetch made %d network requests, want none", requestCount-before)
	}

}

func TestResolverDoesNotMarkArtifactVerifiedBeforeResourceDecode(t *testing.T) {
	cache := NewCache(t.TempDir())
	payload := []byte(`{"name":"review"`)
	contentDigest := hccartifact.ContentDigest(payload)
	canonical := hccartifact.CanonicalArtifact{
		Manifest: hccartifact.CanonicalManifest{
			SchemaVersion: hccartifact.CanonicalArtifactSchemaV1, ArtifactVersion: hccartifact.CanonicalArtifactSchemaV1,
			Kind: "skill", Name: "review", RegistryID: "src", Revision: contentDigest, ContentDigest: contentDigest,
			Provenance: hccartifact.CanonicalProvenance{RegistryURL: "https://market.example", Ref: "main", Generator: "test"},
		},
		Payload: payload,
	}
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	rev := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) { writeTestWellKnown(w, "mp") })
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(rev) })
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(raw) })
	server := httptest.NewServer(mux)
	defer server.Close()

	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		RegistryID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest,
	}}}}}
	if resolved, errs := (&Resolver{Cache: cache}).Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh); len(resolved) != 0 || len(errs) != 1 || !strings.Contains(errs[0].Error(), "decode resource") {
		t.Fatalf("malformed resource resolve = %d artifacts, errors %v", len(resolved), errs)
	}
	if _, verified, err := cache.ReadArtifact(archiveDigest); err != nil {
		t.Fatalf("ReadArtifact: %v", err)
	} else if verified {
		t.Fatal("artifact was marked verified before resource decode completed")
	}
}

func TestResolverCachedNoFetchFailsClosedBeforeHTTP(t *testing.T) {
	cache := NewCache(t.TempDir())
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()
	digest := "sha256:" + strings.Repeat("a", 64)
	archiveDigest := "sha256:" + strings.Repeat("b", 64)
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		RegistryID: "src", Kind: "skill", Name: "review", Revision: digest, Digest: digest,
	}}}
	r := &Resolver{Cache: cache}
	_, errs := r.Resolve(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil, resolver.MarketplaceResolveModeCachedNoFetch)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "metadata is missing") {
		t.Fatalf("missing metadata errors = %v, want precise cache miss", errs)
	}
	if requests != 0 {
		t.Fatalf("missing metadata made %d HTTP requests", requests)
	}
	if _, err := cache.WriteResourceRevision(server.URL, "src", "skill", "review", digest, assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(digest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})); err != nil {
		t.Fatal(err)
	}
	_, errs = r.Resolve(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil, resolver.MarketplaceResolveModeCachedNoFetch)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "verified artifact is missing") {
		t.Fatalf("missing artifact errors = %v, want precise cache miss", errs)
	}
	if requests != 0 {
		t.Fatalf("missing artifact made %d HTTP requests", requests)
	}
	if _, err := cache.WriteArtifact(archiveDigest, []byte("unverified")); err != nil {
		t.Fatal(err)
	}
	_, errs = r.Resolve(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil, resolver.MarketplaceResolveModeCachedNoFetch)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "not verified") {
		t.Fatalf("unverified artifact errors = %v, want trust failure", errs)
	}
	if requests != 0 {
		t.Fatalf("unverified artifact made %d HTTP requests", requests)
	}
}

func TestResolverContextCancellationCachedNoFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, errs := (&Resolver{Cache: NewCache(t.TempDir())}).Resolve(ctx, &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "https://market.example", InstanceID: "mp",
	}}}, nil, resolver.MarketplaceResolveModeCachedNoFetch)
	if len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
		t.Fatalf("canceled resolve errors = %v, want context.Canceled", errs)
	}
}

func TestResolverInspect(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cache := NewCache(tmp)

	sum := sha256.Sum256([]byte("payload"))
	contentDigest := "sha256:" + hex.EncodeToString(sum[:])
	rev := assessedRevision(store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID("mp"),
			Registry:    store.RegistryID("src"),
			Kind:        "skill",
			Name:        "review",
		},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest("sha256:0000000000000000000000000000000000000000000000000000000000000000"),
	})

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
						RegistryID: "src",
						Kind:       "skill",
						Name:       "review",
						Revision:   contentDigest,
						Digest:     contentDigest,
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
	rev := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(digest),
		ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("b", 64)),
	})
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
					RegistryID: "src", Kind: "skill", Name: "review", Revision: "refs/heads/main", Digest: digest,
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
	revision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: instanceID, Registry: "source", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(digest),
		ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("b", 64)),
	})

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
								RegistryID: "source", Kind: "skill", Name: "review",
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

	resource := ir.NewSkill("review", "Review code", "Review the code carefully.", nil, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(resource)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	contentDigest := canonical.Manifest.ContentDigest
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.Revision = contentDigest
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatalf("MarkArtifactVerified: %v", err)
	}

	rev := revokedRevision(store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID("mp"),
			Registry:    store.RegistryID("src"),
			Kind:        "skill",
			Name:        "review",
		},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	}, contentDigest)

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
						RegistryID: "src",
						Kind:       "skill",
						Name:       "review",
						Revision:   contentDigest,
						Digest:     contentDigest,
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

	if resolved, errs := r.Resolve(ctx, cfg, nil, resolver.MarketplaceResolveModeCachedNoFetch); len(resolved) != 0 || len(errs) == 0 || !strings.Contains(errs[0].Error(), "revoked") {
		t.Fatalf("cached revoked result = %d artifacts, errors %v, want blocked", len(resolved), errs)
	}

	policy, err := NewRevocationOverridePolicy([]string{"mp/src/skill/review@" + contentDigest})
	if err != nil {
		t.Fatalf("NewRevocationOverridePolicy: %v", err)
	}
	resolved, errs := r.ResolveWithPolicy(ctx, cfg, nil, resolver.MarketplaceResolveModeCachedNoFetch, policy)
	if len(errs) != 0 || len(resolved) != 1 || !resolved[0].Revoked {
		t.Fatalf("exact revoked override result = %d artifacts, errors %v, want one revoked artifact", len(resolved), errs)
	}

	wrongPolicy, err := NewRevocationOverridePolicy([]string{"mp/src/skill/review@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatalf("NewRevocationOverridePolicy wrong digest: %v", err)
	}
	if resolved, errs := r.ResolveWithPolicy(ctx, cfg, nil, resolver.MarketplaceResolveModeCachedNoFetch, wrongPolicy); len(resolved) != 0 || len(errs) == 0 {
		t.Fatalf("wrong digest override result = %d artifacts, errors %v, want blocked", len(resolved), errs)
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
						RegistryID: "src",
						Kind:       "collection",
						Name:       "bundle",
						Revision:   "sha256:0000000000000000000000000000000000000000000000000000000000000000",
						Digest:     "sha256:0000000000000000000000000000000000000000000000000000000000000000",
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
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: "refs/heads/main", Digest: string(digest)}
	if err := validatePin(entry, pin); err != nil {
		t.Fatalf("opaque pin revision rejected: %v", err)
	}
	valid := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: digest,
		ArchiveDigest: archive,
	})
	if err := validateResourceRevision(entry, pin, valid); err != nil {
		t.Fatalf("valid revision rejected: %v", err)
	}

	invalidCoordinate := valid.Coordinate
	invalidCoordinate.Marketplace = "other"
	invalid := assessedRevision(store.ResourceRevision{Coordinate: invalidCoordinate, ContentDigest: valid.ContentDigest, ArchiveDigest: valid.ArchiveDigest})
	if err := validateResourceRevision(entry, pin, invalid); err == nil || !strings.Contains(err.Error(), "coordinate mismatch") {
		t.Fatalf("invalid coordinate error = %v", err)
	}

	invalidDigest := store.RevisionDigest("sha256:" + strings.Repeat("c", 64))
	invalid = assessedRevision(store.ResourceRevision{Coordinate: valid.Coordinate, ContentDigest: invalidDigest, ArchiveDigest: valid.ArchiveDigest})
	if err := validateResourceRevision(entry, pin, invalid); err == nil || !strings.Contains(err.Error(), "content digest mismatch") {
		t.Fatalf("invalid content digest error = %v", err)
	}
}

func TestResolverValidatesArtifactManifestIdentity(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	archive := "sha256:" + strings.Repeat("b", 64)
	entry := config.MarketplaceEntry{InstanceID: "mp"}
	pin := config.MarketplaceResourcePin{RegistryID: "src", RegistrySlug: "old-source-slug", Kind: "skill", Name: "review", Revision: "refs/heads/main", Digest: digest}
	rev := store.AssessedResourceRevision{ResourceRevision: store.ResourceRevision{ArchiveDigest: store.ArchiveDigest(archive)}}
	artifact := hccartifact.CanonicalArtifact{}
	artifact.Manifest.Kind = "skill"
	artifact.Manifest.Name = "review"
	artifact.Manifest.RegistryID = "src"
	artifact.Manifest.RegistrySlug = "renamed-source-slug"
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
			RegistryID: "src",
			Kind:       "skill",
			Name:       "review",
			Revision:   "refs/heads/main",
			Digest:     digest,
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
		writeTestWellKnown(w, instanceID)
	})
}

func TestResolvePinWithArtifactRetainsVerifiedArtifactWhenCacheIsTampered(t *testing.T) {
	cache := NewCache(t.TempDir())
	digest := "sha256:" + strings.Repeat("a", 64)
	skill := ir.NewSkill("review", "Review code", "Review carefully.", []string{"user"}, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(skill)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.RegistrySlug = "source"
	canonical.Manifest.Revision = digest
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	rev := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(canonical.Manifest.ContentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"}
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: digest, Digest: canonical.Manifest.ContentDigest}
	if _, err := cache.WriteResourceRevision(entry.URL, pin.RegistryID, pin.Kind, pin.Name, pin.Digest, rev); err != nil {
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

func TestLatestRevisionUsesRegistrySlugRoute(t *testing.T) {
	contentDigest := "sha256:" + strings.Repeat("a", 64)
	archiveDigest := "sha256:" + strings.Repeat("b", 64)
	revision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	mux := http.NewServeMux()
	serveWellKnown(mux, "mp")
	mux.HandleFunc("/api/v1/registries/source-slug", func(w http.ResponseWriter, r *http.Request) {
		writeTestRegistry(w, store.Registry{ID: "src", Slug: "resolved-source-slug", Origins: []string{"https://git.example/repo.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
	})
	mux.HandleFunc("/api/v1/resources/resolved-source-slug/skill/review", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(revision)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	source, latest, err := (&Resolver{}).LatestRevision(context.Background(), config.MarketplaceEntry{URL: server.URL, InstanceID: "mp"}, config.MarketplaceResourcePin{
		RegistryID: "src", RegistrySlug: "source-slug", Kind: "skill", Name: "review", Revision: "old", Digest: contentDigest,
	})
	if err != nil {
		t.Fatalf("LatestRevision: %v", err)
	}
	if source.ID != "src" || latest.ContentDigest != revision.ContentDigest {
		t.Fatalf("LatestRevision result = %#v/%#v", source, latest)
	}
}

func TestPrepareLocatorRejectsExactRevisionDigestMismatch(t *testing.T) {
	requestedDigest := "sha256:" + strings.Repeat("a", 64)
	responseDigest := "sha256:" + strings.Repeat("b", 64)
	revision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(responseDigest),
		ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("c", 64)),
	})
	mux := http.NewServeMux()
	serveWellKnown(mux, "mp")
	mux.HandleFunc("/api/v1/registries/source-slug", func(w http.ResponseWriter, r *http.Request) {
		writeTestRegistry(w, store.Registry{ID: "src", Slug: "source-slug", Origins: []string{"https://git.example/repo.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
	})
	mux.HandleFunc("/api/v1/resources/source-slug/skill/review/revisions/"+requestedDigest, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(revision)
	})
	artifactRequested := false
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		artifactRequested = true
		http.Error(w, "artifact must not be fetched", http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	_, err := (&Resolver{Cache: NewCache(t.TempDir())}).PrepareLocator(context.Background(), config.MarketplaceEntry{URL: server.URL, InstanceID: "mp"}, MarketplaceLocator{
		Registry: "source-slug", Kind: "skill", Name: "review", Revision: requestedDigest, Digest: requestedDigest,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "exact revision content digest mismatch") {
		t.Fatalf("PrepareLocator error = %v, want exact digest mismatch", err)
	}
	if artifactRequested {
		t.Fatal("PrepareLocator fetched an artifact after exact digest mismatch")
	}
}

func TestPrepareLocatorPrefetchesMirrorArtifact(t *testing.T) {
	skill := ir.NewSkill("review", "Review code", "Review carefully.", []string{"user"}, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(skill)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.RegistrySlug = "source"
	canonical.Manifest.Revision = "refs/heads/main"
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	rev := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(canonical.Manifest.ContentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	mux := http.NewServeMux()
	serveWellKnown(mux, "mp")
	mux.HandleFunc("/api/v1/registries/", func(w http.ResponseWriter, r *http.Request) {
		writeTestRegistry(w, store.Registry{ID: "src", Slug: "source", Origins: []string{"https://git.example/repo.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
	})
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(rev)
	})
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(raw) })
	server := httptest.NewServer(mux)
	defer server.Close()

	plan, err := (&Resolver{Cache: NewCache(t.TempDir())}).PrepareLocator(context.Background(), config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Alias: "public"}, MarketplaceLocator{URL: server.URL, Registry: "source", Kind: "skill", Name: "review"}, nil)
	if err != nil {
		t.Fatalf("PrepareLocator: %v", err)
	}
	if plan.Pin.RegistryID != "src" || plan.Pin.Digest != string(rev.ContentDigest) || plan.ArchiveDigest != string(rev.ArchiveDigest) {
		t.Fatalf("prepared pin = %#v", plan.Pin)
	}
	if plan.Artifact.Manifest.Name != "review" {
		t.Fatalf("prepared artifact manifest = %#v", plan.Artifact.Manifest)
	}
}
