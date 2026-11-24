package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestReadLockedPlacePackageVerifiesAllEvidence(t *testing.T) {
	endpoint := "https://market.example"
	instanceID := store.MarketplaceID("mp")
	profileBytes := []byte(placeAcquireProfileYAML)
	selected := placeAcquireProfileIdentity(profileBytes)
	profile, err := targetprofile.Parse(profileBytes, selected)
	if err != nil {
		t.Fatalf("parse profile fixture: %v", err)
	}
	coordinate := store.ResourceCoordinate{Marketplace: instanceID, Registry: "reg", Kind: "invented-app/mod", Name: "sample"}
	files := []targetprofile.PackageFile{{
		SourcePath: "payload/instructions.md", DestinationPath: "mods/sample/instructions.md",
		Mode: 0o644, Bytes: []byte("locked place bytes\n"),
	}}
	artifact, err := hccartifact.NewPlaceArtifact(coordinate.Kind, coordinate.Name, selected, files)
	if err != nil {
		t.Fatalf("build place artifact: %v", err)
	}
	_, placeholder, archiveDigest, err := hccartifact.EncodeArchives(artifact)
	if err != nil {
		t.Fatalf("encode place artifact: %v", err)
	}
	revision := placeAcquireRevision(t, coordinate, artifact, archiveDigest)
	locked := lockedPlaceResource(revision)
	root := t.TempDir()
	cache := NewCache(root)
	writeLockedPlaceCache(t, cache, endpoint, revision, placeholder)
	before := snapshotPlaceCache(t, root)
	assertFailure := func(read func() (store.AssessedResourceRevision, []targetprofile.PackageFile, error), want string) {
		t.Helper()
		rev, files, err := read()
		assertLockedPlaceFailure(t, rev, files, err, want)
	}

	got, gotFiles, err := cache.ReadLockedPlacePackage(endpoint, instanceID, locked, selected, profileBytes)
	if err != nil {
		t.Fatalf("ReadLockedPlacePackage: %v", err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal read revision: %v", err)
	}
	wantJSON, err := json.Marshal(revision)
	if err != nil {
		t.Fatalf("marshal expected revision: %v", err)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("read revision = %s, want %s", gotJSON, wantJSON)
	}
	if len(gotFiles) != 1 || gotFiles[0].SourcePath != files[0].SourcePath ||
		gotFiles[0].DestinationPath != files[0].DestinationPath || gotFiles[0].Mode != files[0].Mode ||
		!bytes.Equal(gotFiles[0].Bytes, files[0].Bytes) {
		t.Fatalf("read files = %+v, want exact mapped payload", gotFiles)
	}
	if err := targetprofile.ValidatePackageDestinations(profile, coordinate.Kind, gotFiles); err != nil {
		t.Fatalf("read destination validation: %v", err)
	}
	assertPlaceCacheUnchanged(t, root, before)

	missingBinding := locked
	missingBinding.HostPackageIdentity = nil
	assertFailure(func() (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
		return cache.ReadLockedPlacePackage(endpoint, instanceID, missingBinding, selected, profileBytes)
	}, "marketplace.cache.profile-binding-missing")
	assertPlaceCacheUnchanged(t, root, before)

	changedProfile := []byte(strings.Replace(placeAcquireProfileYAML, "evidence: fixture", "evidence: changed fixture", 1))
	changedSelected := placeAcquireProfileIdentity(changedProfile)
	missingRoot := NewCache(filepath.Join(t.TempDir(), "not-created"))
	assertFailure(func() (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
		return missingRoot.ReadLockedPlacePackage(endpoint, instanceID, locked, changedSelected, changedProfile)
	}, "marketplace.profile.revision-mismatch")

	changedAssessment := locked
	changedAssessment.AssessmentDigest = lockfile.Digest("sha256:" + strings.Repeat("c", 64))
	assertFailure(func() (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
		return cache.ReadLockedPlacePackage(endpoint, instanceID, changedAssessment, selected, profileBytes)
	}, "")
	assertPlaceCacheUnchanged(t, root, before)

	changedMapping := locked
	mappingBinding := *locked.HostPackageIdentity
	mappingBinding.DeliveryMappingDigest = "sha256:" + strings.Repeat("c", 64)
	changedMapping.HostPackageIdentity = &mappingBinding
	assertFailure(func() (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
		return cache.ReadLockedPlacePackage(endpoint, instanceID, changedMapping, selected, profileBytes)
	}, "")
	assertPlaceCacheUnchanged(t, root, before)

	metadataWithoutBinding := revision
	metadataWithoutBinding.Assessment.HostPackageIdentity = nil
	if err := seedPlaceResourceRevision(cache, endpoint, instanceID, locked, metadataWithoutBinding); err != nil {
		t.Fatalf("write metadata without binding: %v", err)
	}
	metadataMissingSnapshot := snapshotPlaceCache(t, root)
	assertFailure(func() (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
		return cache.ReadLockedPlacePackage(endpoint, instanceID, locked, selected, profileBytes)
	}, "marketplace.cache.profile-binding-missing")
	assertPlaceCacheUnchanged(t, root, metadataMissingSnapshot)

	writeLockedPlaceCache(t, cache, endpoint, revision, placeholder)
	archivePath := cache.PlaceholderArchivePath(string(revision.ArchiveDigest))
	if err := os.WriteFile(archivePath, append(bytes.Clone(placeholder), 'x'), 0o644); err != nil {
		t.Fatalf("tamper placeholder: %v", err)
	}
	tamperedSnapshot := snapshotPlaceCache(t, root)
	assertFailure(func() (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
		return cache.ReadLockedPlacePackage(endpoint, instanceID, locked, selected, profileBytes)
	}, "")
	assertPlaceCacheUnchanged(t, root, tamperedSnapshot)

	finalOnly := NewCache(t.TempDir())
	if err := seedPlaceResourceRevision(finalOnly, endpoint, instanceID, locked, revision); err != nil {
		t.Fatalf("write final-only metadata: %v", err)
	}
	if _, err := finalOnly.writeArchive(finalForm, string(revision.ArchiveDigest), placeholder); err != nil {
		t.Fatalf("write final-form archive: %v", err)
	}
	finalRevision, returnedFiles, err := finalOnly.ReadLockedPlacePackage(endpoint, instanceID, locked, selected, profileBytes)
	assertLockedPlaceFailure(t, finalRevision, returnedFiles, err, "")
}

func TestFetchLockedPlacePackageCachesExactEvidence(t *testing.T) {
	profileBytes := []byte(placeAcquireProfileYAML)
	selected := placeAcquireProfileIdentity(profileBytes)
	coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "invented-app/mod", Name: "sample"}
	files := []targetprofile.PackageFile{{SourcePath: "payload/instructions.md", DestinationPath: "mods/sample/instructions.md", Mode: 0o644, Bytes: []byte("online place\n")}}
	artifact, err := hccartifact.NewPlaceArtifact(coordinate.Kind, coordinate.Name, selected, files)
	if err != nil {
		t.Fatal(err)
	}
	_, placeholder, archiveDigest, err := hccartifact.EncodeArchives(artifact)
	if err != nil {
		t.Fatal(err)
	}
	rev1 := placeAcquireRevision(t, coordinate, artifact, archiveDigest)
	rev2 := rev1
	assessment := rev2.Assessment
	assessment.AnalyzerVersion = "test/v2"
	assessment.AssessmentDigest = ""
	rev2.Assessment, err = store.FinalizeResourceAssessment(assessment)
	if err != nil {
		t.Fatal(err)
	}
	lock1, lock2 := lockedPlaceResource(rev1), lockedPlaceResource(rev2)
	revisionPath := "/api/v1/resources/reg/invented-app%2Fmod/sample/revisions/" + string(rev1.ContentDigest)
	archivePath := "/api/v1/archives/sha256/" + strings.TrimPrefix(string(rev1.ArchiveDigest), "sha256:")
	var response atomic.Pointer[store.AssessedResourceRevision]
	response.Store(&rev1)
	var revisionRequests, archiveRequests atomic.Int32
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case revisionPath:
			revisionRequests.Add(1)
			if values := r.Header.Values("X-Agent-Marketplace-Features"); len(values) != 1 || values[0] != mpapi.TargetProfilePlaceFeature {
				t.Errorf("feature header = %q", values)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				store.AssessedResourceRevision
				Availability struct {
					Availability string `json:"availability"`
				} `json:"current_availability"`
			}{AssessedResourceRevision: *response.Load(), Availability: struct {
				Availability string `json:"availability"`
			}{Availability: "available"}})
		case archivePath:
			archiveRequests.Add(1)
			_, _ = w.Write(placeholder)
		default:
			t.Errorf("unexpected path %q", r.URL.EscapedPath())
			http.NotFound(w, r)
		}
	}))
	closed := false
	defer func() {
		if !closed {
			peer.Close()
		}
	}()
	client, err := NewClient(peer.URL)
	if err != nil {
		t.Fatal(err)
	}
	cache := NewCache(t.TempDir())
	ctx := context.Background()
	got1, _, err := cache.FetchLockedPlacePackage(ctx, client, peer.URL, coordinate.Marketplace, lock1, selected, profileBytes)
	if err != nil {
		t.Fatalf("fetch first evidence: %v", err)
	}
	response.Store(&rev2)
	got2, _, err := cache.FetchLockedPlacePackage(ctx, client, peer.URL, coordinate.Marketplace, lock2, selected, profileBytes)
	if err != nil {
		t.Fatalf("fetch second evidence: %v", err)
	}
	if got1.Assessment.AssessmentDigest != rev1.Assessment.AssessmentDigest ||
		got2.Assessment.AssessmentDigest != rev2.Assessment.AssessmentDigest ||
		rev1.ContentDigest != rev2.ContentDigest || rev1.ArchiveDigest != rev2.ArchiveDigest {
		t.Fatalf("online evidence not isolated: first=%+v second=%+v", got1.Assessment, got2.Assessment)
	}
	before := snapshotPlaceCache(t, cache.root)
	assertFailure := func(read func() (store.AssessedResourceRevision, []targetprofile.PackageFile, error), want string) {
		rev, files, err := read()
		assertLockedPlaceFailure(t, rev, files, err, want)
	}
	badLock := lock2
	badLock.AssessmentDigest = lockfile.Digest("sha256:" + strings.Repeat("c", 64))
	revisionsBefore, archivesBefore := revisionRequests.Load(), archiveRequests.Load()
	assertFailure(func() (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
		return cache.FetchLockedPlacePackage(ctx, client, peer.URL, coordinate.Marketplace, badLock, selected, profileBytes)
	}, "does not match lock evidence")
	if revisionRequests.Load() != revisionsBefore+1 || archiveRequests.Load() != archivesBefore {
		t.Fatalf("assessment mismatch requests: revisions=%d archives=%d", revisionRequests.Load()-revisionsBefore, archiveRequests.Load()-archivesBefore)
	}
	assertPlaceCacheUnchanged(t, cache.root, before)
	wrongClient, err := NewClient(peer.URL + "/other")
	if err != nil {
		t.Fatal(err)
	}
	revisionsBefore, archivesBefore = revisionRequests.Load(), archiveRequests.Load()
	assertFailure(func() (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
		return cache.FetchLockedPlacePackage(ctx, wrongClient, peer.URL, coordinate.Marketplace, lock2, selected, profileBytes)
	}, "")
	if revisionRequests.Load() != revisionsBefore || archiveRequests.Load() != archivesBefore {
		t.Fatal("endpoint mismatch issued an HTTP request")
	}
	revisionsBefore, archivesBefore = revisionRequests.Load(), archiveRequests.Load()
	assertFailure(func() (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
		return cache.FetchLockedPlacePackage(ctx, client, peer.URL, coordinate.Marketplace, lock2, targetprofile.Identity{}, profileBytes)
	}, "")
	if revisionRequests.Load() != revisionsBefore || archiveRequests.Load() != archivesBefore {
		t.Fatal("missing selected profile issued an HTTP request")
	}
	changedProfile := []byte(strings.Replace(placeAcquireProfileYAML, "evidence: fixture", "evidence: changed fixture", 1))
	changedIdentity := placeAcquireProfileIdentity(changedProfile)
	revisionsBefore = revisionRequests.Load()
	archivesBefore = archiveRequests.Load()
	assertFailure(func() (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
		return cache.FetchLockedPlacePackage(ctx, client, peer.URL, coordinate.Marketplace, lock2, changedIdentity, changedProfile)
	}, "marketplace.profile.revision-mismatch")
	if revisionRequests.Load() != revisionsBefore || archiveRequests.Load() != archivesBefore {
		t.Fatal("profile mismatch issued an HTTP request")
	}
	assertPlaceCacheUnchanged(t, cache.root, before)
	peer.Close()
	closed = true
	offline1, _, err := cache.ReadLockedPlacePackage(peer.URL, coordinate.Marketplace, lock1, selected, profileBytes)
	if err != nil {
		t.Fatalf("read first evidence offline: %v", err)
	}
	offline2, _, err := cache.ReadLockedPlacePackage(peer.URL, coordinate.Marketplace, lock2, selected, profileBytes)
	if err != nil {
		t.Fatalf("read second evidence offline: %v", err)
	}
	if offline1.Assessment.AssessmentDigest != rev1.Assessment.AssessmentDigest ||
		offline2.Assessment.AssessmentDigest != rev2.Assessment.AssessmentDigest {
		t.Fatalf("offline records did not coexist: first=%+v second=%+v", offline1.Assessment, offline2.Assessment)
	}
}

func lockedPlaceResource(revision store.AssessedResourceRevision) lockfile.MarketplaceResourceLock {
	return lockfile.MarketplaceResourceLock{
		RegistryName: "tools", RegistryID: string(revision.Coordinate.Registry),
		Kind: revision.Coordinate.Kind, Name: revision.Coordinate.Name,
		Revision: string(revision.ContentDigest), ContentDigest: lockfile.Digest(revision.ContentDigest),
		ArchiveDigest:       lockfile.Digest(revision.ArchiveDigest),
		AssessmentDigest:    lockfile.Digest(revision.Assessment.AssessmentDigest),
		Acquisition:         lockfile.MarketplaceAcquisitionMirror,
		HostPackageIdentity: revision.Assessment.HostPackageIdentity,
	}
}

func writeLockedPlaceCache(t *testing.T, cache *Cache, endpoint string, revision store.AssessedResourceRevision, placeholder []byte) {
	t.Helper()
	locked := lockedPlaceResource(revision)
	if err := seedPlaceResourceRevision(cache, endpoint, revision.Coordinate.Marketplace, locked, revision); err != nil {
		t.Fatalf("write cached place revision: %v", err)
	}
	if _, err := cache.writeArchive(placeholderForm, string(revision.ArchiveDigest), placeholder); err != nil {
		t.Fatalf("write cached placeholder archive: %v", err)
	}
}

func seedPlaceResourceRevision(cache *Cache, endpoint string, instanceID store.MarketplaceID, locked lockfile.MarketplaceResourceLock, rev store.AssessedResourceRevision) error {
	_, err := cache.writeResourceRevisionAt(cache.placeMetadataPath(endpoint, instanceID, locked), rev)
	return err
}

type placeCacheFileSnapshot struct {
	Bytes   []byte
	ModTime time.Time
}

func snapshotPlaceCache(t *testing.T, root string) map[string]placeCacheFileSnapshot {
	t.Helper()
	snapshot := make(map[string]placeCacheFileSnapshot)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		snapshot[relative] = placeCacheFileSnapshot{Bytes: content, ModTime: info.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot cache: %v", err)
	}
	return snapshot
}

func assertPlaceCacheUnchanged(t *testing.T, root string, before map[string]placeCacheFileSnapshot) {
	t.Helper()
	after := snapshotPlaceCache(t, root)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("cache changed during read:\nbefore=%+v\nafter=%+v", before, after)
	}
}

func assertLockedPlaceFailure(t *testing.T, rev store.AssessedResourceRevision, files []targetprofile.PackageFile, err error, want string) {
	t.Helper()
	if err == nil || !reflect.DeepEqual(rev, store.AssessedResourceRevision{}) || files != nil {
		t.Fatalf("failure result = (%+v, %+v, %v), want zero revision and nil files", rev, files, err)
	}
	if want != "" && !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want %q", err, want)
	}
}
