package pipeline

import (
	"context"
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/gitidentity"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

func deployedPlaceProfileReader(intent config.Intent, evidence config.Evidence, cfg *config.Config, cache *marketplace.Cache) PlaceProfileReader {
	readGit := DeployedGitPlaceProfileReader(intent, evidence)
	return func(ctx context.Context, identity targetprofile.Identity, mode resolver.MarketplaceResolveMode) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if mode != resolver.MarketplaceResolveModeRefresh && mode != resolver.MarketplaceResolveModeCachedNoFetch {
			return nil, fmt.Errorf("unsupported Marketplace place profile read mode %d", mode)
		}
		if err := identity.Validate(); err != nil {
			return nil, err
		}
		source, err := targetprofile.ParseSourceID(identity.SourceID)
		if err != nil {
			return nil, err
		}
		if source.Kind == "git" && hasDeclaredGitPlaceProfileSource(intent, source) {
			return readGit(ctx, identity, mode)
		}
		if cfg != nil {
			for _, entry := range cfg.Marketplaces {
				for _, pin := range entry.Resources {
					if pin.Kind != nameutil.TargetProfileKind || !pin.Direct ||
						pin.Name != identity.TargetID ||
						pin.Revision != identity.RevisionDigest ||
						pin.Digest != identity.RevisionDigest ||
						pin.Acquisition != config.MarketplaceAcquisitionMirror ||
						pin.LockProfileIdentity != identity ||
						cfg.MarketplacePinExcluded(entry, pin) {
						continue
					}
					record, ok := deployedHostedProfileLock(evidence, entry, pin)
					if !ok {
						continue
					}
					raw, err := cache.ReadLockedTargetProfile(entry.URL, entry.InstanceID, record)
					if err != nil {
						return nil, err
					}
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					return raw, nil
				}
			}
		}
		return nil, fmt.Errorf("marketplace.profile.lock-missing: target %s has no matching deployed profile lock", identity.TargetID)
	}
}

func hasDeclaredGitPlaceProfileSource(intent config.Intent, source targetprofile.Source) bool {
	for _, layerSource := range []config.Source{config.Organization, config.UserPersonal} {
		layer := intent.Layers[layerSource]
		if layer.Source != layerSource || !layer.Present() || layer.Err() != nil {
			continue
		}
		for _, declaration := range layer.Sources() {
			if declaration.Kind != config.V4SourceGit || declaration.Ref != source.Ref {
				continue
			}
			origin, err := gitidentity.CanonicalV1(declaration.URL)
			if err == nil && origin == source.Origin {
				return true
			}
		}
	}
	return false
}

func deployedHostedProfileLock(
	evidence config.Evidence,
	entry config.MarketplaceEntry,
	pin config.MarketplaceResourcePin,
) (lockfile.MarketplaceResourceLock, bool) {
	if entry.Source != config.Organization && entry.Source != config.UserPersonal {
		return lockfile.MarketplaceResourceLock{}, false
	}
	doc := evidence.Locks[entry.Source]
	if doc == nil || doc.Scope != lockfile.Scope(entry.Source.LockScope()) {
		return lockfile.MarketplaceResourceLock{}, false
	}
	entryURL, err := marketplace.NormalizeMarketplaceURL(entry.URL)
	if err != nil {
		return lockfile.MarketplaceResourceLock{}, false
	}
	var match *lockfile.MarketplaceResourceLock
	for _, endpoint := range doc.Marketplaces {
		endpointURL, err := marketplace.NormalizeMarketplaceURL(endpoint.URL)
		if err != nil || endpointURL != entryURL || endpoint.InstanceID != entry.InstanceID {
			continue
		}
		for i := range endpoint.Resources {
			record := endpoint.Resources[i]
			if record.RegistryID != pin.RegistryID || record.RegistryName != pin.RegistrySlug ||
				record.Kind != pin.Kind || record.Name != pin.Name ||
				record.Revision != pin.Revision || string(record.ContentDigest) != pin.Digest ||
				record.Acquisition != lockfile.MarketplaceAcquisitionMirror || record.ArchiveDigest != "" ||
				record.ProfileIdentity == nil || *record.ProfileIdentity != pin.LockProfileIdentity {
				continue
			}
			if match != nil {
				return lockfile.MarketplaceResourceLock{}, false
			}
			match = &record
		}
	}
	if match == nil {
		return lockfile.MarketplaceResourceLock{}, false
	}
	return *match, true
}
