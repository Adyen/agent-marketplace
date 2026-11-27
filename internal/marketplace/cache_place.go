package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	cachefs "github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

type cachedPlaceRevision struct {
	store.AssessedResourceRevision
	IndexedRequires json.RawMessage `json:"requires,omitempty"`
}

// ReadLockedPlacePackage verifies a lock-bound place package using cache data only.
func (c *Cache) ReadLockedPlacePackage(endpoint string, instanceID store.MarketplaceID, locked lockfile.MarketplaceResourceLock, selected targetprofile.Identity, profileBytes []byte) (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
	fail := func(err error) (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
		return store.AssessedResourceRevision{}, nil, err
	}
	normalizedEndpoint, err := urlutil.NormalizeEndpointURL(endpoint)
	if err != nil {
		return fail(fmt.Errorf("normalize Marketplace endpoint: %w", err))
	}
	coordinate, profile, err := validateLockedPlacePackage(instanceID, locked, selected, profileBytes)
	if err != nil {
		return fail(err)
	}

	root, err := fsboundary.OpenDir(c.root)
	if err != nil {
		return fail(fmt.Errorf("open Marketplace cache: %w", err))
	}
	defer func() { _ = root.Close() }()
	metadataPath, err := filepath.Rel(c.root, c.placeMetadataPath(normalizedEndpoint, instanceID, locked))
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
	var cached cachedPlaceRevision
	if err := json.Unmarshal(metadata, &cached); err != nil {
		return fail(fmt.Errorf("decode cached place metadata: %w", err))
	}
	rev := cached.AssessedResourceRevision
	rev.Requires = string(cached.IndexedRequires)
	if rev.Assessment.HostPackageIdentity == nil {
		return fail(fmt.Errorf("marketplace.cache.profile-binding-missing: cached package %s has no host_package_identity", locked.Name))
	}
	if err := validateAPIResourceRevisionMatchesFor(rev, coordinate, store.RevisionDigest(locked.Revision), true); err != nil {
		return fail(fmt.Errorf("invalid cached place revision: %w", err))
	}
	if err := matchLockedPlaceEvidence(rev, locked); err != nil {
		return fail(fmt.Errorf("invalid cached place revision: %w", err))
	}
	if rev.Revocation != nil {
		return fail(fmt.Errorf("cached place revision is revoked"))
	}
	if rev.Assessment.Decision == store.AssessmentDecisionReject {
		return fail(fmt.Errorf("cached place revision assessment is rejected"))
	}
	if _, known, err := decodeIndexedPlaceRequirements(rev.Requires); err != nil {
		return fail(fmt.Errorf("marketplace.cache.place-prerequisites-invalid: cached package %s has invalid indexed prerequisites: %w", locked.Name, err))
	} else if !known {
		return fail(fmt.Errorf("marketplace.cache.place-prerequisites-missing: cached package %s has no indexed prerequisites; re-fetch online before using cached package bytes", locked.Name))
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

func validateLockedPlacePackage(instanceID store.MarketplaceID, locked lockfile.MarketplaceResourceLock, selected targetprofile.Identity, profileBytes []byte) (store.ResourceCoordinate, targetprofile.Profile, error) {
	fail := func(err error) (store.ResourceCoordinate, targetprofile.Profile, error) {
		return store.ResourceCoordinate{}, targetprofile.Profile{}, err
	}
	if err := nameutil.ValidateSegment(string(instanceID)); err != nil {
		return fail(fmt.Errorf("locked Marketplace instance ID: %w", err))
	}
	if err := nameutil.ValidateSegment(locked.RegistryID); err != nil {
		return fail(fmt.Errorf("locked registry ID: %w", err))
	}
	targetID, _, scoped := nameutil.SplitPlaceKind(locked.Kind)
	if !scoped || !nameutil.IsPlacePackageName(locked.Name) || targetID != selected.TargetID {
		return fail(fmt.Errorf("locked resource is not a valid scoped place package for the selected profile"))
	}
	if locked.Acquisition != lockfile.MarketplaceAcquisitionMirror || locked.Revision != string(locked.ContentDigest) {
		return fail(fmt.Errorf("locked place package requires mirror acquisition and revision equal to content digest"))
	}
	if locked.AssessmentDigest == "" || locked.HostPackageIdentity == nil {
		return fail(fmt.Errorf("marketplace.cache.profile-binding-missing: package %s has incomplete lock evidence", locked.Name))
	}
	for _, item := range []struct {
		field string
		value string
	}{
		{"content_digest", string(locked.ContentDigest)},
		{"archive_digest", string(locked.ArchiveDigest)},
		{"assessment_digest", string(locked.AssessmentDigest)},
	} {
		if _, err := lockfile.ParseDigest(item.value); err != nil {
			return fail(fmt.Errorf("%s: %w", item.field, err))
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
	return store.ResourceCoordinate{Marketplace: instanceID, Registry: store.RegistryID(locked.RegistryID), Kind: locked.Kind, Name: locked.Name}, profile, nil
}

func matchLockedPlaceEvidence(rev store.AssessedResourceRevision, locked lockfile.MarketplaceResourceLock) error {
	if rev.Assessment.HostPackageIdentity == nil || locked.HostPackageIdentity == nil {
		return fmt.Errorf("marketplace.cache.profile-binding-missing: cached package %s has no host_package_identity", locked.Name)
	}
	if rev.ArchiveDigest != store.ArchiveDigest(locked.ArchiveDigest) ||
		rev.Assessment.AssessmentDigest != store.AssessmentDigest(locked.AssessmentDigest) ||
		*rev.Assessment.HostPackageIdentity != *locked.HostPackageIdentity {
		return fmt.Errorf("place revision does not match lock evidence")
	}
	return nil
}

func (c *Cache) placeMetadataPath(endpoint string, instanceID store.MarketplaceID, locked lockfile.MarketplaceResourceLock) string {
	// Content digests can be shared across assessments; exact lock identities must coexist.
	identity := strings.Join([]string{endpoint, string(instanceID), locked.RegistryID, locked.Kind, locked.Name, locked.Revision, string(locked.ArchiveDigest), string(locked.AssessmentDigest)}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	return filepath.Join(c.root, "place-metadata", hex.EncodeToString(sum[:])+".json")
}

// FetchLockedPlacePackage fills exact lock-keyed metadata and placeholder bytes after verifying them online.
func (c *Cache) FetchLockedPlacePackage(ctx context.Context, client *Client, endpoint string, instanceID store.MarketplaceID, locked lockfile.MarketplaceResourceLock, selected targetprofile.Identity, profileBytes []byte) (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
	fail := func(err error) (store.AssessedResourceRevision, []targetprofile.PackageFile, error) {
		return store.AssessedResourceRevision{}, nil, err
	}
	if client == nil || client.baseURL == nil {
		return fail(fmt.Errorf("missing Marketplace client"))
	}
	normalized, err := urlutil.NormalizeEndpointURL(endpoint)
	if err != nil {
		return fail(fmt.Errorf("normalize Marketplace endpoint: %w", err))
	}
	clientEndpoint, err := urlutil.NormalizeEndpointURL(client.baseURL.String())
	if err != nil || clientEndpoint != normalized {
		return fail(fmt.Errorf("client endpoint does not match requested Marketplace endpoint"))
	}
	coordinate, profile, err := validateLockedPlacePackage(instanceID, locked, selected, profileBytes)
	if err != nil {
		return fail(err)
	}
	rev, err := fetchPlaceRevision(ctx, client, coordinate, store.RevisionDigest(locked.Revision), selected)
	if err != nil {
		return fail(err)
	}
	if err := matchLockedPlaceEvidence(rev, locked); err != nil {
		return fail(err)
	}
	files, raw, err := fetchPlaceArchive(ctx, client, rev, profile)
	if err != nil {
		return fail(err)
	}
	_, err = cachefs.WithCacheMutationLockWithContext(ctx, func() (struct{}, error) {
		if _, err := c.writeArchiveLocked(placeholderForm, string(locked.ArchiveDigest), raw); err != nil {
			return struct{}{}, err
		}
		_, err := c.writeMetadataAt(
			c.placeMetadataPath(normalized, instanceID, locked),
			cachedPlaceRevision{
				AssessedResourceRevision: rev,
				IndexedRequires:          json.RawMessage(rev.Requires),
			},
		)
		return struct{}{}, err
	})
	if err != nil {
		return fail(fmt.Errorf("cache verified place package: %w", err))
	}
	return rev, files, nil
}
