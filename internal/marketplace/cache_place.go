package marketplace

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

// ReadLockedPlacePackage verifies a lock-bound place package using cache data only.
func (c *Cache) ReadLockedPlacePackage(endpoint string, instanceID store.MarketplaceID, locked lockfile.MarketplaceResourceLock, selected targetprofile.Identity, profileBytes []byte) (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
	fail := func(err error) (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
		return store.AssessedResourceRevision{}, nil, err
	}
	normalizedEndpoint, err := urlutil.NormalizeEndpointURL(endpoint)
	if err != nil {
		return fail(fmt.Errorf("normalize Marketplace endpoint: %w", err))
	}
	instance := string(instanceID)
	if instance == "" || instance != strings.TrimSpace(instance) {
		return fail(fmt.Errorf("missing or untrimmed Marketplace instance ID"))
	}
	if err := nameutil.ValidateSegment(locked.RegistryID); err != nil {
		return fail(fmt.Errorf("locked registry ID: %w", err))
	}
	targetID, _, scoped := nameutil.SplitPlaceKind(locked.Kind)
	if !scoped || !nameutil.IsPlacePackageName(locked.Name) {
		return fail(fmt.Errorf("locked resource is not a valid scoped place package"))
	}
	if targetID != selected.TargetID {
		return fail(fmt.Errorf("scoped kind target %q does not match selected profile target %q", targetID, selected.TargetID))
	}
	if locked.Acquisition != lockfile.MarketplaceAcquisitionMirror {
		return fail(fmt.Errorf("locked place package requires mirror acquisition"))
	}
	if locked.Revision != string(locked.ContentDigest) {
		return fail(fmt.Errorf("locked place revision must equal content digest"))
	}
	if locked.AssessmentDigest == "" || locked.HostPackageIdentity == nil {
		return fail(fmt.Errorf("marketplace.cache.profile-binding-missing: package %s has incomplete lock evidence", locked.Name))
	}
	for _, item := range []struct {
		name   string
		digest string
	}{
		{name: "content_digest", digest: string(locked.ContentDigest)},
		{name: "archive_digest", digest: string(locked.ArchiveDigest)},
		{name: "assessment_digest", digest: string(locked.AssessmentDigest)},
	} {
		if _, err := lockfile.ParseDigest(item.digest); err != nil {
			return fail(fmt.Errorf("%s: %w", item.name, err))
		}
	}
	if err := locked.HostPackageIdentity.ValidateFor(locked.Kind, string(locked.ContentDigest)); err != nil {
		return fail(fmt.Errorf("locked host_package_identity: %w", err))
	}
	profile, err := targetprofile.Parse(profileBytes, selected)
	if err != nil {
		return fail(fmt.Errorf("parse selected profile: %w", err))
	}
	if err := locked.HostPackageIdentity.MatchProfile(selected); err != nil {
		return fail(fmt.Errorf("marketplace.profile.revision-mismatch: package %s profile %s: %w", locked.Name, selected.TargetID, err))
	}

	root, err := fsboundary.OpenDir(c.root)
	if err != nil {
		return fail(fmt.Errorf("open Marketplace cache: %w", err))
	}
	defer func() { _ = root.Close() }()
	coordinate := store.ResourceCoordinate{
		Marketplace: instanceID, Registry: store.RegistryID(locked.RegistryID),
		Kind: locked.Kind, Name: locked.Name,
	}
	metadataPath, err := filepath.Rel(c.root, c.MetadataPath(normalizedEndpoint, locked.RegistryID, locked.Kind, locked.Name, locked.Revision))
	if err != nil {
		return fail(fmt.Errorf("resolve cached place metadata path: %w", err))
	}
	metadata, _, err := root.ReadFile(filepath.ToSlash(metadataPath), maxArtifactBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fail(fmt.Errorf("marketplace.cache.place-metadata-missing: %w", err))
		}
		return fail(fmt.Errorf("read cached place metadata: %w", err))
	}
	var rev store.AssessedResourceRevision
	if err := json.Unmarshal(metadata, &rev); err != nil {
		return fail(fmt.Errorf("decode cached place metadata: %w", err))
	}
	if rev.Assessment.HostPackageIdentity == nil {
		return fail(fmt.Errorf("marketplace.cache.profile-binding-missing: cached package %s has no host_package_identity", locked.Name))
	}
	if err := validateAPIResourceRevisionMatchesFor(rev, coordinate, store.RevisionDigest(locked.Revision), true); err != nil {
		return fail(fmt.Errorf("invalid cached place revision: %w", err))
	}
	if rev.ArchiveDigest != store.ArchiveDigest(locked.ArchiveDigest) ||
		rev.Assessment.AssessmentDigest != store.AssessmentDigest(locked.AssessmentDigest) ||
		*rev.Assessment.HostPackageIdentity != *locked.HostPackageIdentity {
		return fail(fmt.Errorf("cached place revision does not match lock evidence"))
	}
	if rev.Revocation != nil {
		return fail(fmt.Errorf("cached place revision is revoked"))
	}
	if rev.Assessment.Decision == store.AssessmentDecisionReject {
		return fail(fmt.Errorf("cached place revision assessment is rejected"))
	}
	archivePath, err := filepath.Rel(c.root, c.PlaceholderArchivePath(string(locked.ArchiveDigest)))
	if err != nil {
		return fail(fmt.Errorf("resolve cached placeholder path: %w", err))
	}
	raw, _, err := root.ReadFile(filepath.ToSlash(archivePath), maxArtifactBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fail(fmt.Errorf("marketplace.cache.place-archive-missing: %w", err))
		}
		return fail(fmt.Errorf("read cached placeholder archive: %w", err))
	}
	files, err := verifyPlacePackageArchive(rev, profile, raw)
	if err != nil {
		return fail(err)
	}
	return rev, files, nil
}
