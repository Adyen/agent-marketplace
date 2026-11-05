package marketplace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	marketplaceartifact "github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	marketplaceconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/ingest"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// commitReviewSkillRepo creates a Git repository with one factory skill and
// returns its path and HEAD commit.
func commitReviewSkillRepo(t *testing.T, body string) (string, string) {
	t.Helper()
	repo := t.TempDir()
	skillDir := filepath.Join(repo, ".agents", "skills", "review")
	if err := os.MkdirAll(filepath.Join(skillDir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review code\nlicense: MIT\n---\n"+body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "docs", "support.txt"), []byte("support material\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "--initial-branch=main"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"add", "."}, {"commit", "-q", "-m", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return repo, strings.TrimSpace(string(out))
}

// serveIndexOnlyOccurrence serves one index-only occurrence whose registry revision
// manifest is payload, recorded under manifestDigest.
func serveIndexOnlyOccurrence(t *testing.T, registry store.Registry, origin, commit string, revision store.AssessedResourceRevision, manifestDigest string, payload []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) { writeTestWellKnown(w, "mp") })
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(revision) })
	mux.HandleFunc("/api/v1/manifests/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(payload) })
	mux.HandleFunc("/api/v1/registries/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/occurrences/") {
			_ = json.NewEncoder(w).Encode(api.ExactRegistryOccurrence{RegistryID: "src", Ref: "main", Roots: registry.Roots, Commit: commit, RegistryRevisionManifestDigest: store.RevisionDigest(manifestDigest), Acquisition: store.AcquisitionIndexOnly, AcquisitionOrigin: origin, OccurredAt: time.Now().UTC()})
			return
		}
		_ = json.NewEncoder(w).Encode(api.PublicRegistry{ID: "src", Slug: "source", Origins: []string{origin}, Ref: "main", Roots: registry.Roots, Acquisition: store.AcquisitionIndexOnly, Listed: true})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// spec: marketplace-api/index-only-resolution-verifies-the-registry-revision-manifest-by-hash
func TestIndexOnlyResolutionBindsRegistryRevisionManifest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo, commit := commitReviewSkillRepo(t, "Review the code.")
	origin := "file://" + repo
	registry := store.Registry{ID: "src", Slug: "source", Ref: "main", Origins: []string{origin}, Acquisition: store.AcquisitionIndexOnly, License: "MIT", Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}}}
	built, err := ingest.BuildRegistryRevision(context.Background(), marketplaceconfig.Config{Policy: marketplaceconfig.Policy{HiddenContent: "warn", Limits: marketplaceconfig.Limits{IngestionDuration: marketplaceconfig.Duration(2 * time.Minute)}}}, "mp", registry, repo, commit)
	if err != nil {
		t.Fatalf("build revision: %v", err)
	}
	var revision store.AssessedResourceRevision
	for _, candidate := range built.Resources {
		if candidate.Coordinate.Kind == "skill" && candidate.Coordinate.Name == "review" {
			revision = assessedRevisionOccurrence(candidate, commit, string(built.ManifestDigest))
		}
	}
	if revision.ContentDigest == "" {
		t.Fatal("reference revision is missing")
	}
	targets := []hcc.Target{hcc.TargetFactory}
	pinFor := func(manifestDigest string) config.MarketplaceResourcePin {
		return config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: string(revision.ContentDigest), Digest: string(revision.ContentDigest), Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: commit, RegistryManifestDigest: manifestDigest}
	}
	configFor := func(url, manifestDigest string) *config.Config {
		return &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: url, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pinFor(manifestDigest)}}}}
	}

	server := serveIndexOnlyOccurrence(t, registry, origin, commit, revision, string(built.ManifestDigest), built.ManifestPayload)
	cache := NewCache(t.TempDir())
	if resolved, errs := (&Resolver{Cache: cache}).Resolve(context.Background(), configFor(server.URL, string(built.ManifestDigest)), targets, resolver.MarketplaceResolveModeRefresh); len(errs) != 0 || len(resolved) != 1 {
		t.Fatalf("attested index-only resolve = %d artifacts, errors %v", len(resolved), errs)
	}

	var attested marketplaceartifact.RegistryRevisionManifest
	if err := json.Unmarshal(built.ManifestPayload, &attested); err != nil {
		t.Fatal(err)
	}
	attested.Resources[0].ContentDigest = "sha256:" + strings.Repeat("c", 64)
	forgedPayload, forgedDigest, err := marketplaceartifact.CanonicalRegistryRevisionPayload(attested)
	if err != nil {
		t.Fatal(err)
	}
	forgedRevision := revision
	forgedRevision.Occurrence.ManifestDigest = store.RevisionDigest(forgedDigest)
	forged := serveIndexOnlyOccurrence(t, registry, origin, commit, forgedRevision, forgedDigest, forgedPayload)
	if _, errs := (&Resolver{Cache: NewCache(t.TempDir())}).Resolve(context.Background(), configFor(forged.URL, forgedDigest), targets, resolver.MarketplaceResolveModeRefresh); len(errs) != 1 || !strings.Contains(errs[0].Error(), "attested registry revision manifest") {
		t.Fatalf("manifest recording a different content digest errors = %v", errs)
	}

	wrongBytes := serveIndexOnlyOccurrence(t, registry, origin, commit, revision, string(built.ManifestDigest), forgedPayload)
	if _, errs := (&Resolver{Cache: NewCache(t.TempDir())}).Resolve(context.Background(), configFor(wrongBytes.URL, string(built.ManifestDigest)), targets, resolver.MarketplaceResolveModeRefresh); len(errs) != 1 || !strings.Contains(errs[0].Error(), "digest mismatch") {
		t.Fatalf("manifest bytes that do not hash to the occurrence digest errors = %v", errs)
	}

	// A cached artifact whose own archive digest field came from another encoder is
	// verified by content: resolution must not re-encode it.
	identity := IndexOnlyIdentity{Marketplace: "mp", RegistryID: "src", Commit: commit, ManifestDigest: string(built.ManifestDigest)}
	cached, verified, err := cache.ReadIndexOnlyArtifact(identity, string(revision.ContentDigest))
	if err != nil || !verified {
		t.Fatalf("cached index-only artifact: verified=%v err=%v", verified, err)
	}
	foreign := tamperCanonicalArchiveDigest(t, cached)
	if _, err := cache.WriteIndexOnlyArtifact(identity, string(revision.ContentDigest), foreign); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkIndexOnlyArtifactVerified(identity, string(revision.ContentDigest)); err != nil {
		t.Fatal(err)
	}
	if resolved, errs := (&Resolver{Cache: cache}).Resolve(context.Background(), configFor(server.URL, string(built.ManifestDigest)), targets, resolver.MarketplaceResolveModeCachedNoFetch); len(errs) != 0 || len(resolved) != 1 {
		t.Fatalf("cached index-only artifact with a foreign archive digest = %d artifacts, errors %v", len(resolved), errs)
	}
}
