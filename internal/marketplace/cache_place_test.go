package marketplace

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
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
	if _, err := cache.WriteResourceRevision(endpoint, string(coordinate.Registry), coordinate.Kind, coordinate.Name, locked.Revision, metadataWithoutBinding); err != nil {
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
	if _, err := finalOnly.WriteResourceRevision(endpoint, string(coordinate.Registry), coordinate.Kind, coordinate.Name, locked.Revision, revision); err != nil {
		t.Fatalf("write final-only metadata: %v", err)
	}
	if _, err := finalOnly.writeArchive(finalForm, string(revision.ArchiveDigest), placeholder); err != nil {
		t.Fatalf("write final-form archive: %v", err)
	}
	finalRevision, returnedFiles, err := finalOnly.ReadLockedPlacePackage(endpoint, instanceID, locked, selected, profileBytes)
	assertLockedPlaceFailure(t, finalRevision, returnedFiles, err, "")
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
	if _, err := cache.WriteResourceRevision(endpoint, string(revision.Coordinate.Registry), revision.Coordinate.Kind, revision.Coordinate.Name, string(revision.ContentDigest), revision); err != nil {
		t.Fatalf("write cached place revision: %v", err)
	}
	if _, err := cache.writeArchive(placeholderForm, string(revision.ArchiveDigest), placeholder); err != nil {
		t.Fatalf("write cached placeholder archive: %v", err)
	}
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
