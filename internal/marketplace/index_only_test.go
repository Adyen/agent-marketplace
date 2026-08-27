package marketplace

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	marketplaceconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/ingest"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestIndexOnlyResolverChecksExactCommitAndReingestedDigests(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	skillDir := filepath.Join(repo, ".agents", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review code\nlicense: MIT\n---\nReview the code.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(skillDir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "docs", "support.txt"), []byte("support material\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"add", "."}, {"commit", "-q", "-m", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.TrimSpace(string(out))
	origin := "file://" + repo
	source := store.Source{ID: "src", Slug: "source", Ref: "main", Origins: []string{origin}, Acquisition: store.AcquisitionIndexOnly, License: "MIT", Roots: []store.SourceRoot{{Path: ".agents", SourceHarness: "factory"}}}
	built, err := ingest.BuildSourceRevision(context.Background(), marketplaceconfig.Config{Policy: marketplaceconfig.Policy{HiddenContent: "warn", Limits: marketplaceconfig.Limits{IngestionDuration: marketplaceconfig.Duration(2 * time.Minute)}}}, "mp", source, repo, commit)
	if err != nil {
		t.Fatalf("build reference revision: %v", err)
	}
	var revision store.AssessedResourceRevision
	for _, candidate := range built.Resources {
		if candidate.Coordinate.Kind == "skill" && candidate.Coordinate.Name == "review" {
			revision = assessedRevisionOccurrence(candidate, commit, string(built.ManifestDigest))
			break
		}
	}
	if revision.ContentDigest == "" || built.ManifestDigest == "" {
		t.Fatalf("reference revision is incomplete: %#v / %q", revision, built.ManifestDigest)
	}
	revision.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: string(revision.ContentDigest), Reason: "test revocation"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) { writeTestWellKnown(w, "mp") })
	mux.HandleFunc("/api/v1/sources/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/occurrences/"):
			_ = json.NewEncoder(w).Encode(api.ExactSourceOccurrence{SourceID: "src", Ref: "main", Roots: source.Roots, Commit: commit, SourceRevisionManifestDigest: built.ManifestDigest, Acquisition: store.AcquisitionIndexOnly, AcquisitionOrigin: origin, OccurredAt: revisionTime()})
		default:
			_ = json.NewEncoder(w).Encode(api.PublicSource{ID: "src", Slug: "source", Origins: []string{origin}, Ref: "main", Roots: []store.SourceRoot{{Path: ".wrong", SourceHarness: "factory"}}, Acquisition: store.AcquisitionIndexOnly, Listed: true})
		}
	})
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(revision) })
	server := httptest.NewServer(mux)
	defer server.Close()
	pin := config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review", Revision: string(revision.ContentDigest), Digest: string(revision.ContentDigest), Acquisition: config.MarketplaceAcquisitionIndexOnly, SourceCommit: commit, SourceManifestDigest: string(built.ManifestDigest)}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}
	r := &Resolver{Cache: NewCache(t.TempDir())}
	if resolved, errs := r.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh); len(resolved) != 0 || len(errs) != 1 || !strings.Contains(errs[0].Error(), "revoked") {
		t.Fatalf("index-only revoked result without override = %d artifacts, errors %v", len(resolved), errs)
	}
	policy, err := NewRevocationOverridePolicy([]string{"mp/src/skill/review@" + string(revision.ContentDigest)})
	if err != nil {
		t.Fatalf("NewRevocationOverridePolicy: %v", err)
	}
	resolved, errs := r.ResolveWithPolicy(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh, policy)
	if len(errs) != 0 || len(resolved) != 1 || !resolved[0].Revoked {
		t.Fatalf("index-only exact revoked override result = %d artifacts, errors %v, want one revoked artifact", len(resolved), errs)
	}
	if resolved[0].Resource == nil || resolved[0].Path == "" || strings.Contains(resolved[0].Path, "artifact.tar.gz") || strings.Contains(resolved[0].Path, "verified.marker") {
		t.Fatalf("index-only resource must use a reconstructed source path, not cache internals: %#v", resolved[0])
	}
	if _, err := materialize.Run(context.Background(), repo, resolved); err != nil {
		t.Fatalf("materialize reconstructed index-only skill: %v", err)
	}
	materializedSkill := filepath.Join(repo, ".factory", "skills", "review")
	if _, err := os.Stat(filepath.Join(materializedSkill, "docs", "support.txt")); err != nil {
		t.Fatalf("reconstructed support file was not materialized: %v", err)
	}
	var leaked []string
	_ = filepath.WalkDir(materializedSkill, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr == nil && entry.Name() != "review" && (entry.Name() == "artifact.tar.gz" || entry.Name() == "verified.marker") {
			leaked = append(leaked, path)
		}
		return nil
	})
	if len(leaked) != 0 {
		t.Fatalf("cache internals leaked into materialized skill: %v", leaked)
	}

	// Cached-no-fetch must use only the occurrence-scoped verified state. The
	// resolver must not construct or contact the still-running Marketplace endpoint.
	offlineResolved, offlineErrs := r.ResolveWithPolicy(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeCachedNoFetch, policy)
	if len(offlineErrs) != 0 || len(offlineResolved) != 1 || offlineResolved[0].Resource == nil {
		t.Fatalf("offline index-only resolve = %d artifacts, errors %v", len(offlineResolved), offlineErrs)
	}
	if _, err := os.Stat(filepath.Join(offlineResolved[0].Path, "docs", "support.txt")); err != nil {
		t.Fatalf("offline reconstructed support file missing: %v", err)
	}

	badDigest := "sha256:" + strings.Repeat("f", 64)
	badPin := pin
	badPin.Digest, badPin.Revision = badDigest, badDigest
	badCfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{badPin}}}}
	if _, errs := r.ResolveWithPolicy(context.Background(), badCfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh, policy); len(errs) == 0 || !strings.Contains(errs[0].Error(), "digest") {
		t.Fatalf("content digest mismatch errors = %v", errs)
	}
	badManifestPin := pin
	badManifestPin.SourceManifestDigest = "sha256:" + strings.Repeat("f", 64)
	badManifestCfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{badManifestPin}}}}
	if _, errs := r.ResolveWithPolicy(context.Background(), badManifestCfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh, policy); len(errs) == 0 || !strings.Contains(errs[0].Error(), "manifest") {
		t.Fatalf("source manifest mismatch errors = %v", errs)
	}
}

func TestIndexOnlyResolverRejectsPostReingestionManifestMismatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	skillDir := filepath.Join(repo, ".agents", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review code\nlicense: MIT\n---\n"+body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill("same body")
	if err := os.MkdirAll(filepath.Join(skillDir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "docs", "support.txt"), []byte("first support\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"add", "."}, {"commit", "-q", "-m", "first"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	commitOutput, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	firstCommit := strings.TrimSpace(string(commitOutput))
	source := store.Source{ID: "src", Slug: "source", Ref: "main", Origins: []string{"file://" + repo}, Acquisition: store.AcquisitionIndexOnly, License: "MIT", Roots: []store.SourceRoot{{Path: ".agents", SourceHarness: "factory"}}}
	old, err := ingest.BuildSourceRevision(context.Background(), marketplaceconfig.Config{Policy: marketplaceconfig.Policy{HiddenContent: "warn", Limits: marketplaceconfig.Limits{IngestionDuration: marketplaceconfig.Duration(2 * time.Minute)}}}, "mp", source, repo, firstCommit)
	if err != nil {
		t.Fatalf("build first revision: %v", err)
	}
	var oldResource store.AssessedResourceRevision
	for _, candidate := range old.Resources {
		if candidate.Coordinate.Kind == "skill" && candidate.Coordinate.Name == "review" {
			oldResource = assessedRevision(candidate)
			break
		}
	}
	writeSkill("same body")
	if err := os.WriteFile(filepath.Join(skillDir, "docs", "support.txt"), []byte("second support\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add second: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-q", "-m", "second")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit second: %v\n%s", err, out)
	}
	commitOutput, err = exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	secondCommit := strings.TrimSpace(string(commitOutput))

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) { writeTestWellKnown(w, "mp") })
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(assessedRevisionOccurrence(oldResource.ResourceRevision, secondCommit, string(old.ManifestDigest)))
	})
	mux.HandleFunc("/api/v1/sources/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/occurrences/") {
			_ = json.NewEncoder(w).Encode(api.ExactSourceOccurrence{SourceID: "src", Ref: "main", Roots: source.Roots, Commit: secondCommit, SourceRevisionManifestDigest: old.ManifestDigest, Acquisition: store.AcquisitionIndexOnly, AcquisitionOrigin: "file://" + repo, OccurredAt: time.Now().UTC()})
			return
		}
		_ = json.NewEncoder(w).Encode(api.PublicSource{ID: "src", Slug: "source", Origins: []string{"file://" + repo}, Ref: "main", Roots: source.Roots, Acquisition: store.AcquisitionIndexOnly, Listed: true})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	pin := config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review", Revision: string(oldResource.ContentDigest), Digest: string(oldResource.ContentDigest), Acquisition: config.MarketplaceAcquisitionIndexOnly, SourceCommit: secondCommit, SourceManifestDigest: string(old.ManifestDigest)}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}
	_, errs := (&Resolver{Cache: NewCache(t.TempDir())}).Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "after re-ingestion") {
		t.Fatalf("post-reingestion mismatch errors = %v", errs)
	}
}

func TestIndexOnlyResolverRejectsDisallowedOriginBeforeGit(t *testing.T) {
	commit := strings.Repeat("a", 40)
	manifest := "sha256:" + strings.Repeat("b", 64)
	content := "sha256:" + strings.Repeat("c", 64)
	revision := assessedRevisionOccurrence(store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"}, ContentDigest: store.RevisionDigest(content)}, commit, manifest)
	var sourceRequests int
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) { writeTestWellKnown(w, "mp") })
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(revision) })
	mux.HandleFunc("/api/v1/sources/", func(w http.ResponseWriter, r *http.Request) {
		sourceRequests++
		_ = json.NewEncoder(w).Encode(api.ExactSourceOccurrence{SourceID: "src", Ref: "main", Roots: []store.SourceRoot{{Path: ".agents", SourceHarness: "factory"}}, Commit: commit, SourceRevisionManifestDigest: store.RevisionDigest(manifest), Acquisition: store.AcquisitionIndexOnly, AcquisitionOrigin: "https://evil.example/repo.git", OccurredAt: time.Now().UTC()})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	allowed := []string{`trusted\\.example/`}
	pin := config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review", Revision: content, Digest: content, Acquisition: config.MarketplaceAcquisitionIndexOnly, SourceCommit: commit, SourceManifestDigest: manifest}
	cfg := &config.Config{AllowedRegistries: &allowed, Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}
	cache := NewCache(t.TempDir())
	identity := IndexOnlyIdentity{Marketplace: "mp", SourceID: "src", Commit: commit, ManifestDigest: manifest}
	if err := cache.MarkIndexOnlyArtifactVerified(identity, pin.Digest); err != nil {
		t.Fatal(err)
	}
	_, errs := (&Resolver{Cache: cache}).Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "allowed_registries") {
		t.Fatalf("disallowed origin errors = %v", errs)
	}
	if sourceRequests != 1 {
		t.Fatalf("source occurrence requests = %d, want 1 and no Git access", sourceRequests)
	}
	markerPath := filepath.Join(filepath.Dir(cache.IndexOnlyArtifactPath(identity, pin.Digest)), "verified.marker")
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("refresh failure left occurrence marker present: %v", err)
	}
}

func TestIndexOnlyResolverRejectsBareLocalOriginBeforeGit(t *testing.T) {
	commit := strings.Repeat("a", 40)
	manifest := "sha256:" + strings.Repeat("b", 64)
	content := "sha256:" + strings.Repeat("c", 64)
	origin := filepath.Join(t.TempDir(), "not-a-git-origin")
	marker := filepath.Join(t.TempDir(), "git-ran")
	bin := t.TempDir()
	git := filepath.Join(bin, "git")
	if err := os.WriteFile(git, []byte("#!/bin/sh\nprintf x > "+marker+"\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	revision := assessedRevisionOccurrence(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(content),
	}, commit, manifest)
	occurrenceRequests, sourceRequests := 0, 0
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) { writeTestWellKnown(w, "mp") })
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(revision) })
	mux.HandleFunc("/api/v1/sources/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/occurrences/") {
			occurrenceRequests++
			_ = json.NewEncoder(w).Encode(api.ExactSourceOccurrence{SourceID: "src", Ref: "main", Roots: []store.SourceRoot{{Path: ".agents", SourceHarness: "factory"}}, Commit: commit, SourceRevisionManifestDigest: store.RevisionDigest(manifest), Acquisition: store.AcquisitionIndexOnly, AcquisitionOrigin: origin, OccurredAt: time.Now().UTC()})
			return
		}
		sourceRequests++
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	pin := config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review", Revision: content, Digest: content, Acquisition: config.MarketplaceAcquisitionIndexOnly, SourceCommit: commit, SourceManifestDigest: manifest}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}
	_, errs := (&Resolver{Cache: NewCache(t.TempDir())}).Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "bare relative or absolute paths") {
		t.Fatalf("unsafe local origin errors = %v", errs)
	}
	if occurrenceRequests != 1 || sourceRequests != 0 {
		t.Fatalf("source requests = occurrence %d, metadata %d; origin should fail before source acquisition", occurrenceRequests, sourceRequests)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Git was invoked for unsafe local origin; marker stat = %v", err)
	}
}

func TestReconstructedArtifactIdentityIsAnalyzerIndependent(t *testing.T) {
	pin := config.MarketplaceResourcePin{SourceID: "requested", Kind: "skill", Name: "review", Digest: "sha256:" + strings.Repeat("a", 64)}
	artifact := hccartifact.CanonicalArtifact{Manifest: hccartifact.CanonicalManifest{SourceID: "other", Kind: "skill", Name: "review", ContentDigest: pin.Digest}}
	if err := validateReconstructedArtifactIdentity(pin, artifact); err != nil {
		t.Fatalf("analyzer-independent identity validation failed: %v", err)
	}
}

func TestStageReconstructedResourceRejectsSymlinkedSupportParent(t *testing.T) {
	checkout := t.TempDir()
	skillDir := filepath.Join(checkout, ".agents", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(skillDir, "docs")); err != nil {
		t.Fatal(err)
	}

	source := store.Source{Roots: []store.SourceRoot{{Path: ".agents", SourceHarness: "factory"}}}
	_, err := stageReconstructedResource(NewCache(t.TempDir()), IndexOnlyIdentity{Marketplace: "mp", SourceID: "src", Commit: strings.Repeat("a", 40), ManifestDigest: "sha256:" + strings.Repeat("b", 64)}, checkout, source, "skill", "review", []string{"docs/secret.txt"})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("stage symlinked support parent error = %v, want symlink rejection", err)
	}
}

func TestCachedIndexOnlyFailuresInvalidateOccurrenceMarker(t *testing.T) {
	resource := ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(resource)
	if err != nil {
		t.Fatal(err)
	}
	contentDigest := canonical.Manifest.ContentDigest
	canonical.Manifest.SourceID = "src"
	canonical.Manifest.Revision = contentDigest
	validRaw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("a", 40)
	manifest := "sha256:" + strings.Repeat("b", 64)
	identity := IndexOnlyIdentity{Marketplace: "mp", SourceID: "src", Commit: commit, ManifestDigest: manifest}
	pin := config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest, Acquisition: config.MarketplaceAcquisitionIndexOnly, SourceCommit: commit, SourceManifestDigest: manifest}

	cases := []struct {
		name       string
		artifact   func() []byte
		stageFault bool
	}{
		{name: "decode", artifact: func() []byte { return []byte("not an archive") }},
		{name: "content", artifact: func() []byte {
			bad := canonical
			bad.Manifest.ContentDigest = "sha256:" + strings.Repeat("c", 64)
			raw, _, _ := hccartifact.Encode(bad)
			return raw
		}},
		{name: "archive", artifact: func() []byte {
			return tamperCanonicalArchiveDigest(t, validRaw)
		}},
		{name: "staging", artifact: func() []byte { return validRaw }, stageFault: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cache := NewCache(t.TempDir())
			if _, err := cache.WriteIndexOnlyResourceRevision(identity, pin.SourceID, pin.Kind, pin.Name, pin.Digest, assessedRevisionOccurrence(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
				ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
			}, commit, manifest)); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.WriteIndexOnlyArtifact(identity, pin.Digest, tc.artifact()); err != nil {
				t.Fatal(err)
			}
			if err := cache.MarkIndexOnlyArtifactVerified(identity, pin.Digest); err != nil {
				t.Fatal(err)
			}
			stagedDir := cachedReconstructedResourcePath(cache, identity, pin.Kind)
			if tc.stageFault {
				outside := t.TempDir()
				if err := os.Symlink(outside, filepath.Join(cache.root, "compiled")); err != nil {
					t.Fatal(err)
				}
			} else if err := os.MkdirAll(stagedDir, 0o755); err != nil {
				t.Fatal(err)
			} else if err := os.WriteFile(filepath.Join(stagedDir, "SKILL.md"), []byte("cached"), 0o644); err != nil {
				t.Fatal(err)
			}

			shared := NewCache(cache.root)
			if _, err := shared.WriteArtifact(archiveDigest, validRaw); err != nil {
				t.Fatal(err)
			}
			if err := shared.MarkArtifactVerified(archiveDigest); err != nil {
				t.Fatal(err)
			}
			if _, errs := (&Resolver{Cache: cache}).Resolve(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://market.example", InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeCachedNoFetch); len(errs) == 0 {
				t.Fatal("cached invalid index-only artifact unexpectedly resolved")
			}
			marker := filepath.Join(filepath.Dir(cache.IndexOnlyArtifactPath(identity, pin.Digest)), "verified.marker")
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("occurrence marker still present after %s failure: %v", tc.name, err)
			}
			if _, verified, err := cache.ReadArtifact(archiveDigest); err != nil || !verified {
				t.Fatalf("shared mirror marker changed after %s failure: verified=%t err=%v", tc.name, verified, err)
			}
		})
	}
}

func tamperCanonicalArchiveDigest(t *testing.T, raw []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	var output bytes.Buffer
	writer := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(writer)
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == hccartifact.CanonicalManifestPath {
			var manifest map[string]any
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest["archive_digest"] = "sha256:" + strings.Repeat("d", 64)
			data, err = json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			header.Size = int64(len(data))
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestStageReconstructedResourceRejectsSymlinkedCacheAncestor(t *testing.T) {
	checkout := t.TempDir()
	skillDir := filepath.Join(checkout, ".agents", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cacheRoot := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(cacheRoot, "compiled")); err != nil {
		t.Fatal(err)
	}
	source := store.Source{Roots: []store.SourceRoot{{Path: ".agents", SourceHarness: "factory"}}}
	_, err := stageReconstructedResource(NewCache(cacheRoot), IndexOnlyIdentity{Marketplace: "mp", SourceID: "src", Commit: strings.Repeat("a", 40), ManifestDigest: "sha256:" + strings.Repeat("b", 64)}, checkout, source, "skill", "review", nil)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("stage symlinked cache ancestor error = %v, want symlink rejection", err)
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("staging wrote outside cache root: %v", entries)
	}
}

func revisionTime() time.Time { return time.Now().UTC() }
