package marketplace

import (
	"context"
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

// PrepareTargetProfileLock acquires one explicitly pinned target-profile
// revision and returns its enriched Marketplace entry and virtual lock record.
func (r *Resolver) PrepareTargetProfileLock(
	ctx context.Context,
	entry config.MarketplaceEntry,
	locator MarketplaceLocator,
	budget *PreparationBudget,
) (config.MarketplaceEntry, lockfile.MarketplaceResourceLock, error) {
	fail := func(err error) (config.MarketplaceEntry, lockfile.MarketplaceResourceLock, error) {
		return config.MarketplaceEntry{}, lockfile.MarketplaceResourceLock{}, err
	}
	if locator.Kind != nameutil.TargetProfileKind {
		return fail(fmt.Errorf("target-profile locator kind %q is invalid", locator.Kind))
	}
	if !nameutil.IsTargetIdentifier(locator.Name) || target.IsValid(locator.Name) {
		return fail(fmt.Errorf("target-profile locator name %q is invalid", locator.Name))
	}
	if err := nameutil.ValidateSegment(locator.Registry); err != nil {
		return fail(fmt.Errorf("target-profile locator registry: %w", err))
	}
	if err := validateSHA256Digest(locator.Revision); err != nil {
		return fail(fmt.Errorf("target-profile locator revision: %w", err))
	}
	if locator.Digest != "" && locator.Digest != locator.Revision {
		return fail(fmt.Errorf("target-profile locator digest does not match its pinned revision"))
	}
	if entry.Source != config.UserPersonal && entry.Source != config.Organization {
		return fail(fmt.Errorf("target-profile locks require user or organization scope"))
	}
	if err := budget.reserve(1); err != nil {
		return fail(err)
	}
	canonical, err := canonicalMarketplaceEntry(entry)
	if err != nil {
		return fail(err)
	}
	if locator.URL != "" {
		locatorURL, err := urlutil.NormalizeEndpointURL(locator.URL)
		if err != nil {
			return fail(fmt.Errorf("normalize locator Marketplace endpoint: %w", err))
		}
		if locatorURL != canonical.URL {
			return fail(fmt.Errorf("target-profile locator endpoint does not match the configured Marketplace"))
		}
	}
	client, err := r.clientForEntry(canonical)
	if err != nil {
		return fail(err)
	}
	enriched, err := acquireMarketplaceIdentity(ctx, client, canonical)
	if err != nil {
		return fail(err)
	}
	registry, err := client.GetRegistryBySlug(ctx, locator.Registry)
	if err != nil {
		return fail(fmt.Errorf("resolve target-profile registry %q: %w", locator.Registry, err))
	}
	if registry.Acquisition != store.AcquisitionMirror {
		return fail(fmt.Errorf("target-profile registry %q does not use mirror acquisition", locator.Registry))
	}
	coordinate := store.ResourceCoordinate{
		Marketplace: store.MarketplaceID(enriched.InstanceID), Registry: registry.ID,
		Kind: locator.Kind, Name: locator.Name,
	}
	evidence, err := client.AcquireTargetProfile(ctx, coordinate, store.RevisionDigest(locator.Revision), nil)
	if err != nil {
		return fail(fmt.Errorf("acquire target profile %s/%s: %w", locator.Registry, locator.Name, err))
	}
	if err := r.cache().writeTargetProfile(ctx, enriched.URL, enriched.InstanceID, string(registry.ID), evidence.Identity, evidence.Bytes); err != nil {
		return fail(err)
	}
	identity := evidence.Identity
	locked := lockfile.MarketplaceResourceLock{
		RegistryName:    locator.Registry,
		RegistryID:      string(registry.ID),
		Kind:            locator.Kind,
		Name:            locator.Name,
		Revision:        locator.Revision,
		ContentDigest:   lockfile.Digest(identity.RevisionDigest),
		Acquisition:     lockfile.MarketplaceAcquisitionMirror,
		ProfileIdentity: &identity,
	}
	return enriched, locked, nil
}
