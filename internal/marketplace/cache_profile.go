package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

// ReadLockedTargetProfile returns cached bytes for the exact target-profile
// identity recorded by a mirror-acquired lock.
func (c *Cache) ReadLockedTargetProfile(endpoint, instanceID string, lock lockfile.MarketplaceResourceLock) ([]byte, error) {
	fail := func(err error) ([]byte, error) {
		return nil, err
	}
	normalizedEndpoint, err := urlutil.NormalizeEndpointURL(endpoint)
	if err != nil {
		return fail(fmt.Errorf("normalize Marketplace endpoint: %w", err))
	}
	if err := nameutil.ValidateSegment(instanceID); err != nil {
		return fail(fmt.Errorf("locked Marketplace instance ID: %w", err))
	}
	if err := nameutil.ValidateSegment(lock.RegistryName); err != nil {
		return fail(fmt.Errorf("locked registry name: %w", err))
	}
	if err := nameutil.ValidateSegment(lock.RegistryID); err != nil {
		return fail(fmt.Errorf("locked registry ID: %w", err))
	}
	if lock.Kind != nameutil.TargetProfileKind || lock.Acquisition != lockfile.MarketplaceAcquisitionMirror {
		return fail(fmt.Errorf("locked resource is not a mirror-acquired target profile"))
	}
	if lock.ProfileIdentity == nil {
		return fail(fmt.Errorf("locked target profile has no profile_identity"))
	}
	identity := *lock.ProfileIdentity
	if err := identity.Validate(); err != nil {
		return fail(fmt.Errorf("locked target profile identity: %w", err))
	}
	if lock.Name != identity.TargetID ||
		lock.Revision != identity.RevisionDigest ||
		string(lock.ContentDigest) != identity.RevisionDigest {
		return fail(fmt.Errorf("locked target profile identity does not match its exact revision and content digest"))
	}
	if lock.ArchiveDigest != "" {
		return fail(fmt.Errorf("target-profile lock must not include an archive digest"))
	}

	cachePath := c.targetProfileCachePath(normalizedEndpoint, instanceID, lock.RegistryID, identity)
	relativePath, err := filepath.Rel(c.root, cachePath)
	if err != nil {
		return fail(fmt.Errorf("resolve cached target profile path: %w", err))
	}
	root, err := fsboundary.OpenDir(c.root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fail(fmt.Errorf("marketplace.cache.profile-missing: %w", err))
		}
		return fail(fmt.Errorf("open Marketplace cache: %w", err))
	}
	defer func() { _ = root.Close() }()
	raw, _, err := root.ReadFile(filepath.ToSlash(relativePath), targetprofile.MaxProfileBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fail(fmt.Errorf("marketplace.cache.profile-missing: %w", err))
		}
		return fail(fmt.Errorf("read cached target profile: %w", err))
	}
	if _, err := targetprofile.Parse(raw, identity); err != nil {
		return fail(fmt.Errorf("invalid cached target profile: %w", err))
	}
	return raw, nil
}

func (c *Cache) targetProfileCachePath(endpoint, instanceID, registryID string, identity targetprofile.Identity) string {
	key := strings.Join([]string{endpoint, instanceID, registryID, identity.TargetID, identity.SourceID, identity.RevisionDigest}, "\x00")
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.root, "target-profiles", hex.EncodeToString(sum[:])+".yaml")
}

func (c *Cache) writeTargetProfile(ctx context.Context, endpoint, instanceID, registryID string, identity targetprofile.Identity, raw []byte) error {
	path := c.targetProfileCachePath(endpoint, instanceID, registryID, identity)
	_, err := cachefs.WithCacheMutationLockWithContext(ctx, func() (struct{}, error) {
		if err := ensureCacheDirectory(c.root, filepath.Dir(path)); err != nil {
			return struct{}{}, fmt.Errorf("prepare target profile cache: %w", err)
		}
		if err := writeAtomic(c.root, path, raw, 0o644, ""); err != nil {
			return struct{}{}, fmt.Errorf("write target profile cache: %w", err)
		}
		return struct{}{}, nil
	})
	return err
}
