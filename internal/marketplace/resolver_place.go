package marketplace

import (
	"context"
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// ResolvedPlacePackage contains a place package resolved against an exact locked profile.
type ResolvedPlacePackage struct {
	Entry    config.MarketplaceEntry
	Pin      config.MarketplaceResourcePin
	Revision store.AssessedResourceRevision
	Files    []targetprofile.PackageFile
}

type placeResolutionPlan struct {
	entry    config.MarketplaceEntry
	pin      config.MarketplaceResourcePin
	locked   lockfile.MarketplaceResourceLock
	identity targetprofile.Identity
	profile  []byte
}

// ResolvePlacePackages resolves only separately approved place inputs.
// approvedProfiles is caller-authorized input, not proof that approval occurred
// or authorization to execute or materialize a package.
func (r *Resolver) ResolvePlacePackages(ctx context.Context, cfg *config.Config, approvedProfiles map[targetprofile.Identity][]byte, mode resolver.MarketplaceResolveMode) ([]ResolvedPlacePackage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if mode != resolver.MarketplaceResolveModeRefresh && mode != resolver.MarketplaceResolveModeCachedNoFetch {
		return nil, fmt.Errorf("unsupported Marketplace place resolution mode %d", mode)
	}
	if cfg == nil {
		return nil, nil
	}
	selected, err := cfg.SelectedMarketplaceProfiles()
	if err != nil {
		return nil, err
	}
	var allowed []string
	if cfg.AllowedMarketplaces != nil {
		allowed = *cfg.AllowedMarketplaces
	}
	if err := config.ValidateMarketplaceURLs(cfg.Marketplaces, allowed); err != nil {
		return nil, err
	}

	var plans []placeResolutionPlan
	for _, configuredEntry := range cfg.Marketplaces {
		for _, pin := range configuredEntry.Resources {
			if cfg.MarketplacePinExcluded(configuredEntry, pin) {
				continue
			}
			targetID, _, scoped := nameutil.SplitPlaceKind(pin.Kind)
			if !scoped {
				continue
			}
			identity := selected[targetID]
			profileBytes, approved := approvedProfiles[identity]
			if !approved {
				continue
			}
			entry, err := canonicalMarketplaceEntry(configuredEntry)
			if err != nil {
				return nil, &PinError{Entry: configuredEntry, Pin: pin, Err: err}
			}
			binding := pin.LockHostPackageIdentity
			locked := lockfile.MarketplaceResourceLock{
				RegistryName: pin.RegistrySlug, RegistryID: pin.RegistryID, Kind: pin.Kind, Name: pin.Name,
				Revision: pin.Revision, ContentDigest: lockfile.Digest(pin.Digest),
				ArchiveDigest: lockfile.Digest(pin.LockArchiveDigest), AssessmentDigest: lockfile.Digest(pin.LockAssessmentDigest),
				CollectionManifestDigest: lockfile.Digest(pin.LockCollectionManifestDigest), HostPackageIdentity: &binding,
				Acquisition: pin.Acquisition, RegistryCommit: pin.RegistryCommit,
				RegistryManifestDigest: lockfile.Digest(pin.RegistryManifestDigest), SourcePath: pin.LockSourcePath,
			}
			if _, _, err := validateLockedPlacePackage(store.MarketplaceID(entry.InstanceID), locked, identity, profileBytes); err != nil {
				return nil, &PinError{Entry: entry, Pin: pin, Err: err}
			}
			plans = append(plans, placeResolutionPlan{entry: entry, pin: pin, locked: locked, identity: identity, profile: append([]byte(nil), profileBytes...)})
		}
	}
	if len(plans) == 0 {
		return nil, nil
	}
	cache := r.cache()
	var resolved []ResolvedPlacePackage
	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var rev store.AssessedResourceRevision
		var files []targetprofile.PackageFile
		if mode == resolver.MarketplaceResolveModeCachedNoFetch {
			rev, files, err = cache.ReadLockedPlacePackage(plan.entry.URL, store.MarketplaceID(plan.entry.InstanceID), plan.locked, plan.identity, plan.profile)
		} else {
			var client *Client
			client, err = r.clientForEntry(plan.entry)
			if err == nil {
				_, err = acquireMarketplaceIdentity(ctx, client, plan.entry)
			}
			if err == nil {
				rev, files, err = cache.FetchLockedPlacePackage(ctx, client, plan.entry.URL, store.MarketplaceID(plan.entry.InstanceID), plan.locked, plan.identity, plan.profile)
			}
		}
		if err != nil {
			return nil, &PinError{Entry: plan.entry, Pin: plan.pin, Err: err}
		}
		resolved = append(resolved, ResolvedPlacePackage{Entry: plan.entry, Pin: plan.pin, Revision: rev, Files: files})
	}
	return resolved, nil
}
