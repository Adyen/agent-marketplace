package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	marketplaceconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/ingest"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestVerifiedMarketplaceLockResourceIndexOnlyWithoutAdvertisedArchiveDigest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	skillDir := filepath.Join(repo, ".agents", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review code\nlicense: MIT\n---\nReview the code.\n"), 0o644); err != nil {
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
	commit := strings.TrimSpace(string(out))
	origin := "file://" + repo
	registry := store.Registry{ID: "src", Slug: "source", Ref: "main", Origins: []string{origin}, Acquisition: store.AcquisitionIndexOnly, License: "MIT", Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}}}
	built, err := ingest.BuildRegistryRevision(context.Background(), marketplaceconfig.Config{Policy: marketplaceconfig.Policy{HiddenContent: "warn", Limits: marketplaceconfig.Limits{IngestionDuration: marketplaceconfig.Duration(2 * time.Minute)}}}, "mp", registry, repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Resources) != 1 || len(built.Assessments) != 1 || len(built.ResourceOccurrences) != 1 {
		t.Fatalf("expected one ingested resource with assessment and occurrence: %#v", built)
	}
	// Serve actual index-only ingestion metadata. In particular, do not invent
	// an advertised archive_digest to make lock reconstruction succeed.
	revision := store.AssessedResourceRevision{ResourceRevision: built.Resources[0], Assessment: built.Assessments[0], Occurrence: built.ResourceOccurrences[0]}
	if revision.ArchiveDigest != "" {
		t.Fatalf("index-only ingestion advertised an archive digest: %q", revision.ArchiveDigest)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) { writeTestWellKnown(w, "mp") })
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(revision) })
	mux.HandleFunc("/api/v1/manifests/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(built.ManifestPayload) })
	mux.HandleFunc("/api/v1/registries/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/occurrences/") {
			_ = json.NewEncoder(w).Encode(api.ExactRegistryOccurrence{RegistryID: "src", Ref: "main", Roots: registry.Roots, Commit: commit, RegistryRevisionManifestDigest: built.ManifestDigest, Acquisition: store.AcquisitionIndexOnly, AcquisitionOrigin: origin, OccurredAt: revisionTime()})
			return
		}
		_ = json.NewEncoder(w).Encode(api.PublicRegistry{ID: "src", Slug: "source", Origins: []string{origin}, Ref: "main", Roots: registry.Roots, Acquisition: store.AcquisitionIndexOnly, Listed: true})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	cache := NewCache(t.TempDir())
	// This is the same preparation path used by marketplacelock.Resolve for
	// v4 acquisition: exact Git checkout, re-ingestion and trusted cache writes.
	plan, err := (&Resolver{Cache: cache}).PrepareLocatorWithResourceLimit(context.Background(), config.MarketplaceEntry{URL: server.URL, Source: config.RepoCommitted}, MarketplaceLocator{Registry: "source", Kind: "skill", Name: "review"}, []hcc.Target{hcc.TargetFactory}, 1)
	if err != nil {
		t.Fatalf("prepare index-only acquisition: %v", err)
	}
	identity := indexOnlyIdentity(plan.Entry, plan.Pin)
	cachedRevision, found, err := cache.ReadIndexOnlyResourceRevision(identity, "src", "skill", "review", plan.Pin.Digest)
	if err != nil || !found || cachedRevision.ArchiveDigest != "" {
		t.Fatalf("cached index metadata = %#v, found=%t, err=%v", cachedRevision, found, err)
	}
	raw, verified, err := cache.ReadIndexOnlyArtifact(identity, plan.Pin.Digest)
	if err != nil || len(raw) == 0 || !verified {
		t.Fatalf("prepared artifact: bytes=%d, verified=%t, err=%v", len(raw), verified, err)
	}
	expectedArchive := built.ResourceArchiveDigest["skill/review"]
	if expectedArchive == "" {
		t.Fatal("reference ingestion did not retain a canonical archive digest")
	}
	// Evidence reconstruction must remain offline and read-only after the
	// authenticated preparation has populated the cache.
	server.Close()
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	before := snapshotIndexLockEvidenceCache(t, cache.root)
	record, err := cache.VerifiedMarketplaceLockResource(plan.Entry, plan.Pin)
	if err != nil {
		t.Fatalf("verified index-only cache without advertised archive digest: %v", err)
	}
	want := lockfile.MarketplaceResourceLock{
		RegistryName: "source", RegistryID: "src", Kind: "skill", Name: "review",
		Revision: string(revision.ContentDigest), ContentDigest: lockfile.Digest(revision.ContentDigest),
		ArchiveDigest: lockfile.Digest(expectedArchive), Acquisition: config.MarketplaceAcquisitionIndexOnly,
		RegistryCommit: commit, RegistryManifestDigest: lockfile.Digest(built.ManifestDigest), SourcePath: ".agents/skills/review/SKILL.md",
	}
	if !reflect.DeepEqual(record, want) {
		t.Fatalf("index-only lock = %#v, want %#v", record, want)
	}
	if after := snapshotIndexLockEvidenceCache(t, cache.root); !reflect.DeepEqual(before, after) {
		t.Fatal("lock evidence reconstruction changed cached bytes, permissions, timestamps or entries")
	}
}

func TestVerifiedMarketplaceLockResourceIndexOnlyEvidenceBoundaries(t *testing.T) {
	entry := evidenceEntry()
	raw, archiveDigest, contentDigest := evidenceArtifact(t, "skill", "review")
	commit := strings.Repeat("a", 40)
	manifestDigest := "sha256:" + strings.Repeat("b", 64)
	identity := IndexOnlyIdentity{Marketplace: "mp", RegistryID: "src", Commit: commit, ManifestDigest: manifestDigest}
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest, Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: commit, RegistryManifestDigest: manifestDigest}
	amendArtifact := func(amend func(*hccartifact.CanonicalArtifact)) []byte {
		t.Helper()
		canonical, _, err := hccartifact.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		amend(&canonical)
		modified, _, err := hccartifact.Encode(canonical)
		if err != nil {
			t.Fatal(err)
		}
		return modified
	}
	cases := []struct {
		name            string
		revision        func(*store.AssessedResourceRevision)
		artifact        []byte
		missing         bool
		unverified      bool
		mirrorOnly      bool
		otherOccurrence bool
		wantError       string
	}{
		{name: "valid no advertised archive", artifact: raw},
		{name: "missing artifact", missing: true, wantError: "verified artifact is missing from cache"},
		{name: "unverified artifact", artifact: raw, unverified: true, wantError: "verified artifact is missing from cache"},
		{name: "verified mirror is not index evidence", artifact: raw, mirrorOnly: true, wantError: "verified artifact is missing from cache"},
		{name: "other occurrence is not index evidence", artifact: raw, otherOccurrence: true, wantError: "verified artifact is missing from cache"},
		{name: "invalid archive", artifact: []byte("not an archive"), wantError: "decode artifact:"},
		{name: "invalid content", artifact: amendArtifact(func(a *hccartifact.CanonicalArtifact) { a.Manifest.ContentDigest = "sha256:" + strings.Repeat("e", 64) }), wantError: "content digest mismatch:"},
		{name: "manifest name mismatch", artifact: amendArtifact(func(a *hccartifact.CanonicalArtifact) { a.Manifest.Name = "other" }), wantError: "artifact manifest name mismatch:"},
		{name: "manifest kind mismatch", artifact: amendArtifact(func(a *hccartifact.CanonicalArtifact) { a.Manifest.Kind = "command" }), wantError: "artifact manifest kind mismatch:"},
		{name: "manifest registry mismatch", artifact: amendArtifact(func(a *hccartifact.CanonicalArtifact) { a.Manifest.RegistryID = "other" }), wantError: "artifact manifest registry_id mismatch:"},
		{name: "manifest revision mismatch", artifact: amendArtifact(func(a *hccartifact.CanonicalArtifact) { a.Manifest.Revision = "other" }), wantError: "artifact manifest revision mismatch:"},
		{name: "invalid advertised digest", artifact: raw, revision: func(r *store.AssessedResourceRevision) { r.ArchiveDigest = "invalid" }, wantError: "invalid assessed resource revision: archive digest:"},
		{name: "occurrence commit mismatch", artifact: raw, revision: func(r *store.AssessedResourceRevision) { r.Occurrence.Commit = strings.Repeat("f", 40) }, wantError: "cached registry occurrence does not match pinned registry occurrence"},
		{name: "occurrence manifest mismatch", artifact: raw, revision: func(r *store.AssessedResourceRevision) {
			r.Occurrence.ManifestDigest = store.RevisionDigest("sha256:" + strings.Repeat("f", 64))
		}, wantError: "cached registry occurrence does not match pinned registry occurrence"},
		{name: "missing source path", artifact: raw, revision: func(r *store.AssessedResourceRevision) { r.Occurrence.SourcePath = "" }, wantError: "cached registry occurrence has no source path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cache := NewCache(t.TempDir())
			revision := evidenceRevision(coord, contentDigest, "")
			revision.Occurrence = store.ResourceOccurrence{Coordinate: coord, ContentDigest: store.RevisionDigest(contentDigest), Commit: commit, ManifestDigest: store.RevisionDigest(manifestDigest), SourcePath: "skills/review"}
			if tc.revision != nil {
				tc.revision(&revision)
			}
			if _, err := cache.WriteIndexOnlyResourceRevision(identity, "src", "skill", "review", contentDigest, revision); err != nil {
				t.Fatal(err)
			}
			if !tc.missing {
				if tc.mirrorOnly {
					if _, err := cache.WriteArtifact(archiveDigest, tc.artifact); err != nil {
						t.Fatal(err)
					}
					if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
						t.Fatal(err)
					}
				} else {
					artifactIdentity := identity
					if tc.otherOccurrence {
						artifactIdentity.Commit = strings.Repeat("f", 40)
					}
					if _, err := cache.WriteIndexOnlyArtifact(artifactIdentity, contentDigest, tc.artifact); err != nil {
						t.Fatal(err)
					}
					if !tc.unverified {
						if err := cache.MarkIndexOnlyArtifactVerified(artifactIdentity, contentDigest); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			before := snapshotIndexLockEvidenceCache(t, cache.root)
			record, err := cache.VerifiedMarketplaceLockResource(entry, pin)
			if tc.wantError == "" {
				if err != nil || record.ArchiveDigest.String() != archiveDigest {
					t.Fatalf("valid control: record=%#v, err=%v", record, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) || !reflect.DeepEqual(record, lockfile.MarketplaceResourceLock{}) {
				t.Fatalf("record=%#v, err=%v, want empty record and %q", record, err, tc.wantError)
			}
			if after := snapshotIndexLockEvidenceCache(t, cache.root); !reflect.DeepEqual(before, after) {
				t.Fatal("evidence reconstruction changed the cache")
			}
		})
	}
}

func TestVerifiedMarketplaceLockResourceMirrorRequiresAdvertisedArchiveEvidence(t *testing.T) {
	entry := evidenceEntry()
	raw, archiveDigest, contentDigest := evidenceArtifact(t, "skill", "review")
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest, Acquisition: config.MarketplaceAcquisitionMirror}
	cases := []struct {
		name       string
		advertised string
		revoked    bool
		wantError  string
	}{
		{name: "matching digest", advertised: archiveDigest},
		{name: "missing advertised digest", wantError: "revision archive digest is required"},
		{name: "revoked missing advertised digest", revoked: true, wantError: "cached revision metadata has no archive digest"},
		{name: "mismatched advertised digest", advertised: "sha256:" + strings.Repeat("e", 64), wantError: "does not match requested"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cache := NewCache(t.TempDir())
			revision := evidenceRevision(coord, contentDigest, tc.advertised)
			if tc.revoked {
				revision.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: contentDigest, Reason: "test"}
			}
			if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, revision); err != nil {
				t.Fatal(err)
			}
			// Even a marked, canonical archive cannot replace missing advertised
			// mirror evidence or validate bytes cached under the wrong digest.
			key := tc.advertised
			if key == "" {
				key = archiveDigest
			}
			if _, err := cache.WriteArtifact(key, raw); err != nil {
				t.Fatal(err)
			}
			if err := cache.MarkArtifactVerified(key); err != nil {
				t.Fatal(err)
			}
			before := snapshotIndexLockEvidenceCache(t, cache.root)
			record, err := cache.VerifiedMarketplaceLockResource(entry, pin)
			if tc.wantError == "" {
				if err != nil || record.ArchiveDigest.String() != archiveDigest {
					t.Fatalf("valid mirror: record=%#v, err=%v", record, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) || !reflect.DeepEqual(record, lockfile.MarketplaceResourceLock{}) {
				t.Fatalf("record=%#v, err=%v, want empty record and %q", record, err, tc.wantError)
			}
			if after := snapshotIndexLockEvidenceCache(t, cache.root); !reflect.DeepEqual(before, after) {
				t.Fatal("mirror evidence reconstruction changed the cache")
			}
		})
	}
}

type indexLockCacheFile struct {
	Data    string
	Mode    fs.FileMode
	ModTime time.Time
}

func snapshotIndexLockEvidenceCache(t *testing.T, root string) map[string]indexLockCacheFile {
	t.Helper()
	snapshot := make(map[string]indexLockCacheFile)
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var raw []byte
		if !entry.IsDir() {
			raw, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		snapshot[path] = indexLockCacheFile{Data: string(raw), Mode: info.Mode(), ModTime: info.ModTime()}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return snapshot
}
