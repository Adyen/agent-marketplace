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
	enriched, client, coordinate, err := r.preparePinnedPlaceLock(ctx, entry, locator, budget)
	if err != nil {
		return fail(err)
	}
	evidence, err := client.AcquireTargetProfile(ctx, coordinate, store.RevisionDigest(locator.Revision), nil)
	if err != nil {
		return fail(fmt.Errorf("acquire target profile %s/%s: %w", locator.Registry, locator.Name, err))
	}
	if err := r.cache().writeTargetProfile(ctx, enriched.URL, enriched.InstanceID, string(coordinate.Registry), evidence.Identity, evidence.Bytes); err != nil {
		return fail(err)
	}
	identity := evidence.Identity
	locked := lockfile.MarketplaceResourceLock{
		RegistryName:    locator.Registry,
		RegistryID:      string(coordinate.Registry),
		Kind:            locator.Kind,
		Name:            locator.Name,
		Revision:        locator.Revision,
		ContentDigest:   lockfile.Digest(identity.RevisionDigest),
		Acquisition:     lockfile.MarketplaceAcquisitionMirror,
		ProfileIdentity: &identity,
	}
	return enriched, locked, nil
}

// PreparePlacePackageLock records exact scoped-package metadata without
// fetching or caching the package archive.
func (r *Resolver) PreparePlacePackageLock(
	ctx context.Context,
	entry config.MarketplaceEntry,
	locator MarketplaceLocator,
	budget *PreparationBudget,
) (config.MarketplaceEntry, lockfile.MarketplaceResourceLock, error) {
	fail := func(err error) (config.MarketplaceEntry, lockfile.MarketplaceResourceLock, error) {
		return config.MarketplaceEntry{}, lockfile.MarketplaceResourceLock{}, err
	}
	targetID, _, scoped := nameutil.SplitPlaceKind(locator.Kind)
	if !scoped || !nameutil.IsPlacePackageName(locator.Name) || target.IsValid(targetID) {
		return fail(fmt.Errorf("scoped place package locator %q/%q is invalid", locator.Kind, locator.Name))
	}
	enriched, client, coordinate, err := r.preparePinnedPlaceLock(ctx, entry, locator, budget)
	if err != nil {
		return fail(err)
	}
	revision, err := fetchPlaceRevisionMetadata(ctx, client, coordinate, store.RevisionDigest(locator.Revision))
	if err != nil {
		return fail(fmt.Errorf("acquire place package metadata %s/%s: %w", locator.Registry, locator.Name, err))
	}
	locked := placePackageLockRecord(config.MarketplaceResourcePin{
		RegistryID: string(coordinate.Registry), RegistrySlug: locator.Registry,
		Kind: locator.Kind, Name: locator.Name, Revision: locator.Revision,
	}, revision)
	return enriched, locked, nil
}

func placePackageLockRecord(pin config.MarketplaceResourcePin, revision store.AssessedResourceRevision) lockfile.MarketplaceResourceLock {
	binding := *revision.Assessment.HostPackageIdentity
	selectedBy := pin.SelectedBy.List()
	lockedSelections := make([]lockfile.MarketplaceSelectionLock, 0, len(selectedBy))
	for _, selection := range selectedBy {
		lockedSelections = append(lockedSelections, lockfile.MarketplaceSelectionLock{
			RegistryID: selection.RegistryID, Name: selection.Name, Revision: lockfile.Digest(selection.Revision),
		})
	}
	return lockfile.MarketplaceResourceLock{
		RegistryName:        pin.RegistrySlug,
		RegistryID:          pin.RegistryID,
		Kind:                pin.Kind,
		Name:                pin.Name,
		Revision:            pin.Revision,
		ContentDigest:       lockfile.Digest(revision.ContentDigest),
		ArchiveDigest:       lockfile.Digest(revision.ArchiveDigest),
		AssessmentDigest:    lockfile.Digest(revision.Assessment.AssessmentDigest),
		HostPackageIdentity: &binding,
		Acquisition:         lockfile.MarketplaceAcquisitionMirror,
		SelectedBy:          lockedSelections,
	}
}

func (r *Resolver) preparePinnedPlaceLock(
	ctx context.Context,
	entry config.MarketplaceEntry,
	locator MarketplaceLocator,
	budget *PreparationBudget,
) (config.MarketplaceEntry, *Client, store.ResourceCoordinate, error) {
	fail := func(err error) (config.MarketplaceEntry, *Client, store.ResourceCoordinate, error) {
		return config.MarketplaceEntry{}, nil, store.ResourceCoordinate{}, err
	}
	if err := nameutil.ValidateSegment(locator.Registry); err != nil {
		return fail(fmt.Errorf("place locator registry: %w", err))
	}
	if err := validateSHA256Digest(locator.Revision); err != nil {
		return fail(fmt.Errorf("place locator revision: %w", err))
	}
	if locator.Digest != "" && locator.Digest != locator.Revision {
		return fail(fmt.Errorf("place locator digest does not match its pinned revision"))
	}
	if entry.Source != config.UserPersonal && entry.Source != config.Organization {
		return fail(fmt.Errorf("place locks require user or organization scope"))
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
			return fail(fmt.Errorf("place locator endpoint does not match the configured Marketplace"))
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
		return fail(fmt.Errorf("resolve place registry %q: %w", locator.Registry, err))
	}
	if registry.Acquisition != store.AcquisitionMirror {
		return fail(fmt.Errorf("place registry %q does not use mirror acquisition", locator.Registry))
	}
	coordinate := store.ResourceCoordinate{
		Marketplace: store.MarketplaceID(enriched.InstanceID), Registry: registry.ID,
		Kind: locator.Kind, Name: locator.Name,
	}
	return enriched, client, coordinate, nil
}
