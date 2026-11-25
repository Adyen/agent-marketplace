package marketplacelock

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

// A normalized v2 Marketplace resource consumes at least 300 encoded bytes:
// its coordinates, two digest fields, one acquisition-evidence field, and the
// acquisition mode. Anything beyond this count cannot fit MaxFileSize.
const minimumMarketplaceResourceLockBytes = lockfile.MinimumMarketplaceResourceLockBytes
const maxMarketplaceLockResources = lockfile.MaxMarketplaceLockResources

// Resolve acquires and verifies every requested Marketplace resource and
// returns the exact lock evidence for the layer.
func Resolve(ctx context.Context, marketplaceResolver *marketplace.Resolver, layer config.LayerIntent, harnesses []string, policy resolver.MarketplaceRevocationPolicy) (lockfile.Document, error) {
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.Scope(layer.Source.LockScope())}
	targets := lockTargets(harnesses)
	resourceCount := 0
	processedPins := 0
	preparationWork := 0
	for _, source := range layer.MarketplaceSources() {
		entry := config.MarketplaceEntry{URL: source.URL, Alias: source.Name, Source: layer.Source}
		resources := make(map[string]lockfile.MarketplaceResourceLock, len(source.Requests))
		bindings := make(map[string]string, len(source.Requests))
		remainingResources := min(maxMarketplaceLockResources-resourceCount, maxMarketplaceLockResources-processedPins, maxMarketplaceLockResources-preparationWork)
		if len(source.Requests) > 0 && remainingResources < 1 {
			return lockfile.Document{}, fmt.Errorf("marketplace lock resources exceed representable limit %d", maxMarketplaceLockResources)
		}
		type requestResult struct {
			plan   *marketplace.PreparedInstall
			entry  config.MarketplaceEntry
			record *lockfile.MarketplaceResourceLock
			err    error
		}
		budget := marketplace.NewPreparationBudget(remainingResources)
		results := marketplace.ResolveConcurrently(len(source.Requests), func(i int) requestResult {
			request := source.Requests[i]
			locator := marketplace.MarketplaceLocator{
				URL:      entry.URL,
				Registry: request.Registry,
				Kind:     request.MarketplaceKind(),
				Name:     request.Name,
				Revision: request.Options.Revision,
				Digest:   request.Options.Revision,
			}
			if locator.Kind == nameutil.TargetProfileKind {
				acquired, record, err := marketplaceResolver.PrepareTargetProfileLock(ctx, entry, locator, budget)
				return requestResult{entry: acquired, record: &record, err: err}
			}
			if _, _, scoped := nameutil.SplitPlaceKind(locator.Kind); scoped {
				acquired, record, err := marketplaceResolver.PreparePlacePackageLock(ctx, entry, locator, budget)
				return requestResult{entry: acquired, record: &record, err: err}
			}
			plan, err := marketplaceResolver.PrepareLocatorWithBudget(ctx, entry, locator, targets, budget, policy)
			return requestResult{plan: plan, err: err}
		})
		for i, request := range source.Requests {
			remainingResources := min(maxMarketplaceLockResources-resourceCount, maxMarketplaceLockResources-processedPins, maxMarketplaceLockResources-preparationWork)
			if remainingResources < 1 {
				return lockfile.Document{}, fmt.Errorf("marketplace lock resources exceed representable limit %d", maxMarketplaceLockResources)
			}
			result := results[i]
			if result.err != nil {
				return lockfile.Document{}, fmt.Errorf("resolve %s: %w", request.Locator, result.err)
			}
			acquired, work, count := result.entry, 1, 1
			var pins []config.MarketplaceResourcePin
			if result.plan != nil {
				acquired, work = result.plan.Entry, result.plan.PreparationWork
				pins = lockPins(result.plan)
				count = len(pins)
			}
			if i > 0 && acquired.InstanceID != entry.InstanceID {
				return lockfile.Document{}, fmt.Errorf("marketplace instance ID mismatch for %s: got %q, want %q", request.Locator, acquired.InstanceID, entry.InstanceID)
			}
			if work < 1 || work > remainingResources {
				return lockfile.Document{}, fmt.Errorf("marketplace lock preparation work exceeds processing limit %d", maxMarketplaceLockResources)
			}
			preparationWork += work
			entry = acquired
			if count > maxMarketplaceLockResources-processedPins {
				return lockfile.Document{}, fmt.Errorf("marketplace lock records exceed processing limit %d", maxMarketplaceLockResources)
			}
			processedPins += count
			for j := 0; j < count; j++ {
				var record lockfile.MarketplaceResourceLock
				if result.record != nil {
					record = *result.record
				} else {
					pin := pins[j]
					verified, err := marketplaceResolver.Cache.VerifiedMarketplaceLockResource(entry, pin)
					if err != nil {
						return lockfile.Document{}, fmt.Errorf("verify %s/%s/%s lock evidence: %w", pin.RegistryID, pin.Kind, pin.Name, err)
					}
					record = verified
				}
				before := len(resources)
				if err := mergeLockResource(resources, record); err != nil {
					return lockfile.Document{}, err
				}
				if len(resources) > before {
					resourceCount++
				}
				if resourceCount > maxMarketplaceLockResources {
					return lockfile.Document{}, fmt.Errorf("marketplace lock resources exceed representable limit %d", maxMarketplaceLockResources)
				}
				if previous, exists := bindings[record.RegistryName]; exists && previous != record.RegistryID {
					return lockfile.Document{}, fmt.Errorf("registry binding %q resolves to both %q and %q", record.RegistryName, previous, record.RegistryID)
				}
				bindings[record.RegistryName] = record.RegistryID
			}
		}
		if entry.InstanceID == "" {
			instanceID, err := EndpointInstanceID(ctx, entry.URL)
			if err != nil {
				return lockfile.Document{}, err
			}
			entry.InstanceID = instanceID
		}
		locked := lockfile.MarketplaceLock{URL: entry.URL, InstanceID: entry.InstanceID}
		for name, id := range bindings {
			locked.RegistryBindings = append(locked.RegistryBindings, lockfile.MarketplaceRegistryBinding{Name: name, RegistryID: id})
		}
		for _, resource := range resources {
			locked.Resources = append(locked.Resources, resource)
		}
		doc.Marketplaces = append(doc.Marketplaces, locked)
	}
	return lockfile.Normalize(doc)
}

// EndpointInstanceID returns the instance identity that the Marketplace at url
// publishes in its validated well-known document.
func EndpointInstanceID(ctx context.Context, url string) (string, error) {
	client, err := marketplace.NewClient(url)
	if err != nil {
		return "", err
	}
	wellKnown, err := client.WellKnown(ctx)
	if err != nil {
		return "", fmt.Errorf("well-known document: %w", err)
	}
	return wellKnown.InstanceID, nil
}

func lockTargets(names []string) []hcc.Target {
	targets := make([]hcc.Target, len(names))
	for i, name := range names {
		targets[i] = hcc.Target(name)
	}
	return targets
}

func lockPins(plan *marketplace.PreparedInstall) []config.MarketplaceResourcePin {
	pins := make([]config.MarketplaceResourcePin, 1, len(plan.CollectionMembers)+1)
	pins[0] = plan.Pin
	for _, member := range plan.CollectionMembers {
		pins = append(pins, member.Pin)
	}
	return pins
}

func mergeLockResource(resources map[string]lockfile.MarketplaceResourceLock, record lockfile.MarketplaceResourceLock) error {
	key := strings.Join([]string{record.RegistryID, record.Kind, record.Name}, "\x00")
	current, exists := resources[key]
	if !exists {
		resources[key] = record
		return nil
	}
	left, right := current, record
	left.SelectedBy, right.SelectedBy = nil, nil
	if !reflect.DeepEqual(left, right) {
		return fmt.Errorf("marketplace resource %s resolved to conflicting lock records", key)
	}
	seen := map[lockfile.MarketplaceSelectionLock]bool{}
	for _, selection := range current.SelectedBy {
		seen[selection] = true
	}
	for _, selection := range record.SelectedBy {
		if !seen[selection] {
			current.SelectedBy = append(current.SelectedBy, selection)
		}
	}
	resources[key] = current
	return nil
}
