package marketplacelock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// A normalized v2 Marketplace resource consumes at least 300 encoded bytes:
// its coordinates, two digest fields, one acquisition-evidence field, and the
// acquisition mode. Anything beyond this count cannot fit MaxFileSize.
const minimumMarketplaceResourceLockBytes = 300
const maxMarketplaceLockResources = int(lockfile.MaxFileSize) / minimumMarketplaceResourceLockBytes

// Layer describes one v4 configuration layer whose Marketplace acquisition
// lock is being resolved.
type Layer struct {
	Source   config.Source
	LockPath string
	V4       *config.V4Config
}

// HasRequiredError reports whether errs contain a v4 Marketplace lock
// requirement.
func HasRequiredError(errs []error) bool {
	for _, err := range errs {
		var validation *config.V4ValidationError
		if errors.As(err, &validation) && validation.Code == "v4.marketplace-lock-required" {
			return true
		}
	}
	return false
}

// MissingOrganizationLayer returns the trusted organization v4 layer only
// when it declares Marketplace sources and its deployed lock is absent.
// Existing locks, including malformed ones, are never replaced by a transient
// acquisition.
func MissingOrganizationLayer(configPath string) (Layer, bool, error) {
	if err := config.ValidateOrganizationConfigAuthority(configPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Layer{}, false, nil
		}
		return Layer{}, false, err
	}
	v4, err := config.LoadV4ConfigIfPresent(configPath, config.Organization)
	if err != nil {
		return Layer{}, false, err
	}
	if v4 == nil || !HasMarketplaceSource(v4) {
		return Layer{}, false, nil
	}
	paths, err := config.ResolveConfigLockPaths(configPath)
	if err != nil {
		return Layer{}, false, err
	}
	if _, err := os.Lstat(paths.CanonicalLock); err == nil {
		return Layer{}, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Layer{}, false, err
	}
	return Layer{Source: config.Organization, LockPath: paths.CanonicalLock, V4: v4}, true, nil
}

// OrganizationProvisional returns the missing organization layer and its
// encoded placeholder lock for policy-only loading.
func OrganizationProvisional(configPath string, instances ...config.MarketplaceEntry) (Layer, []byte, bool, error) {
	layer, missing, err := MissingOrganizationLayer(configPath)
	if err != nil || !missing {
		return Layer{}, nil, missing, err
	}
	doc, err := Provisional(layer.V4, lockfile.ScopeOrganization, instances...)
	if err != nil {
		return Layer{}, nil, false, err
	}
	data, err := lockfile.Encode(doc)
	if err != nil {
		return Layer{}, nil, false, err
	}
	return layer, data, true, nil
}

// HasMarketplaceSource reports whether a v4 tree needs Marketplace lock
// hydration.
func HasMarketplaceSource(v4 *config.V4Config) bool {
	for _, source := range v4.Sources {
		if source.Kind == config.V4SourceMarketplace {
			return true
		}
	}
	return false
}

// Provisional returns a structurally valid placeholder lock. It is used only
// to load and validate the complete effective policy before any acquisition.
func Provisional(v4 *config.V4Config, scope lockfile.Scope, instances ...config.MarketplaceEntry) (lockfile.Document, error) {
	const digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: scope}
	for _, group := range marketplaceSourceRequests(v4) {
		sourceName := group.name
		source := v4.Sources[sourceName]
		entry := lockfile.MarketplaceLock{URL: source.CanonicalURL, InstanceID: "bootstrap"}
		for _, instance := range instances {
			if url, err := marketplace.NormalizeMarketplaceURL(instance.URL); err == nil && url == entry.URL && instance.InstanceID != "" {
				entry.InstanceID = instance.InstanceID
				break
			}
		}
		requests := group.requests
		bindings := make(map[string]string, len(requests))
		entry.Resources = make([]lockfile.MarketplaceResourceLock, 0, len(requests))
		for _, request := range requests {
			bindings[request.Registry] = request.Registry
			revision := request.Options.Revision
			if revision == "" {
				revision = digest
			}
			resource := lockfile.MarketplaceResourceLock{
				RegistryName:  request.Registry,
				RegistryID:    request.Registry,
				Kind:          request.Kind,
				Name:          request.Name,
				Revision:      revision,
				ContentDigest: lockfile.Digest(revision),
				Acquisition:   lockfile.MarketplaceAcquisitionMirror,
			}
			if request.Kind == "collection" {
				resource.CollectionManifestDigest = lockfile.Digest(revision)
			} else {
				resource.ArchiveDigest = lockfile.Digest(digest)
			}
			entry.Resources = append(entry.Resources, resource)
		}
		for name, id := range bindings {
			entry.RegistryBindings = append(entry.RegistryBindings, lockfile.MarketplaceRegistryBinding{Name: name, RegistryID: id})
		}
		doc.Marketplaces = append(doc.Marketplaces, entry)
	}
	return lockfile.Normalize(doc)
}

// Resolve acquires and verifies every requested Marketplace resource and
// returns the exact lock evidence for the layer.
func Resolve(ctx context.Context, marketplaceResolver *marketplace.Resolver, layer Layer, harnesses []string, policy resolver.MarketplaceRevocationPolicy) (lockfile.Document, error) {
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.Scope(layer.Source.LockScope())}
	targets := lockTargets(harnesses)
	resourceCount := 0
	processedPins := 0
	preparationWork := 0
	for _, group := range marketplaceSourceRequests(layer.V4) {
		sourceName := group.name
		source := layer.V4.Sources[sourceName]
		entry := config.MarketplaceEntry{URL: source.CanonicalURL, Alias: sourceName, Source: layer.Source}
		requests := group.requests
		resources := make(map[string]lockfile.MarketplaceResourceLock, len(requests))
		bindings := make(map[string]string, len(requests))
		for _, request := range requests {
			remainingResources := min(maxMarketplaceLockResources-resourceCount, maxMarketplaceLockResources-processedPins, maxMarketplaceLockResources-preparationWork)
			if remainingResources < 1 {
				return lockfile.Document{}, fmt.Errorf("marketplace lock resources exceed representable limit %d", maxMarketplaceLockResources)
			}
			plan, err := marketplaceResolver.PrepareLocatorWithResourceLimit(ctx, entry, marketplace.MarketplaceLocator{
				URL:      entry.URL,
				Registry: request.Registry,
				Kind:     request.Kind,
				Name:     request.Name,
				Revision: request.Options.Revision,
				Digest:   request.Options.Revision,
			}, targets, remainingResources, policy)
			if err != nil {
				return lockfile.Document{}, fmt.Errorf("resolve %s: %w", request.Locator, err)
			}
			if plan.PreparationWork < 1 || plan.PreparationWork > remainingResources {
				return lockfile.Document{}, fmt.Errorf("marketplace lock preparation work exceeds processing limit %d", maxMarketplaceLockResources)
			}
			preparationWork += plan.PreparationWork
			entry = plan.Entry
			pins := lockPins(plan)
			if len(pins) > maxMarketplaceLockResources-processedPins {
				return lockfile.Document{}, fmt.Errorf("marketplace lock records exceed processing limit %d", maxMarketplaceLockResources)
			}
			processedPins += len(pins)
			for _, pin := range pins {
				record, err := marketplaceResolver.Cache.VerifiedMarketplaceLockResource(entry, pin)
				if err != nil {
					return lockfile.Document{}, fmt.Errorf("verify %s/%s/%s lock evidence: %w", pin.RegistryID, pin.Kind, pin.Name, err)
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
			client, err := marketplace.NewClient(entry.URL)
			if err != nil {
				return lockfile.Document{}, err
			}
			wellKnown, err := client.WellKnown(ctx)
			if err != nil {
				return lockfile.Document{}, fmt.Errorf("well-known document: %w", err)
			}
			entry.InstanceID = wellKnown.InstanceID
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

func PruneToV4(v4 *config.V4Config, current lockfile.Document) (lockfile.Document, error) {
	if v4 == nil || current.APIVersion != lockfile.APIVersionV2 {
		return lockfile.Document{}, fmt.Errorf("v4 Marketplace removal requires a normalized v2 acquisition lock")
	}
	byURL := make(map[string]lockfile.MarketplaceLock, len(current.Marketplaces))
	for _, entry := range current.Marketplaces {
		byURL[entry.URL] = entry
	}
	desired := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: current.Scope, Registries: current.Registries}
	for _, group := range marketplaceSourceRequests(v4) {
		endpoint := v4.Sources[group.name].CanonicalURL
		entry, ok := byURL[endpoint]
		if !ok {
			return lockfile.Document{}, fmt.Errorf("marketplace source %q has no acquisition lock", group.name)
		}
		bindings := make(map[string]string, len(entry.RegistryBindings))
		for _, binding := range entry.RegistryBindings {
			bindings[binding.Name] = binding.RegistryID
		}
		resources := make(map[string]int, len(entry.Resources))
		collections := make(map[string]int)
		for i, resource := range entry.Resources {
			resources[resource.RegistryID+"\x00"+resource.Kind+"\x00"+resource.Name] = i
			if resource.Kind == "collection" {
				collections[resource.RegistryID+"\x00"+resource.Name] = i
			}
		}
		reachable := make([]bool, len(entry.Resources))
		for _, request := range group.requests {
			registryID, ok := bindings[request.Registry]
			if !ok {
				return lockfile.Document{}, fmt.Errorf("marketplace registry %q has no acquisition binding", request.Registry)
			}
			kind := request.Kind
			if kind == "mcp" {
				kind = "mcpserver"
			}
			index, ok := resources[registryID+"\x00"+kind+"\x00"+request.Name]
			if !ok || request.Options.Revision != "" && request.Options.Revision != entry.Resources[index].Revision {
				return lockfile.Document{}, fmt.Errorf("marketplace request %q has no matching acquisition record", request.Locator)
			}
			reachable[index] = true
		}
		for changed := true; changed; {
			changed = false
			for i, resource := range entry.Resources {
				if reachable[i] {
					continue
				}
				for _, edge := range resource.SelectedBy {
					parent, ok := collections[edge.RegistryID+"\x00"+edge.Name]
					if ok && reachable[parent] && entry.Resources[parent].ContentDigest == edge.Revision {
						reachable[i] = true
						changed = true
						break
					}
				}
			}
		}
		retained := lockfile.MarketplaceLock{URL: entry.URL, InstanceID: entry.InstanceID}
		usedBindings := map[string]bool{}
		for i, resource := range entry.Resources {
			if !reachable[i] {
				continue
			}
			copy := resource
			copy.SelectedBy = nil
			for _, edge := range resource.SelectedBy {
				parent, ok := collections[edge.RegistryID+"\x00"+edge.Name]
				if ok && reachable[parent] && entry.Resources[parent].ContentDigest == edge.Revision {
					copy.SelectedBy = append(copy.SelectedBy, edge)
				}
			}
			retained.Resources = append(retained.Resources, copy)
			usedBindings[resource.RegistryName] = true
		}
		for _, binding := range entry.RegistryBindings {
			if usedBindings[binding.Name] {
				retained.RegistryBindings = append(retained.RegistryBindings, binding)
			}
		}
		desired.Marketplaces = append(desired.Marketplaces, retained)
	}
	return lockfile.Normalize(desired)
}

func sortedMarketplaceSources(v4 *config.V4Config) []string {
	names := make([]string, 0, len(v4.Sources))
	for name, source := range v4.Sources {
		if source.Kind == config.V4SourceMarketplace {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

type marketplaceSourceRequestGroup struct {
	name     string
	requests []*config.V4ResourceRequest
}

func marketplaceSourceRequests(v4 *config.V4Config) []marketplaceSourceRequestGroup {
	names := sortedMarketplaceSources(v4)
	requests := make([]*config.V4ResourceRequest, len(v4.Requests))
	for i := range v4.Requests {
		requests[i] = &v4.Requests[i]
	}
	sort.SliceStable(requests, func(i, j int) bool {
		return requests[i].SourceName < requests[j].SourceName
	})
	groups := make([]marketplaceSourceRequestGroup, 0, len(names))
	requestIndex := 0
	for _, name := range names {
		for requestIndex < len(requests) && requests[requestIndex].SourceName < name {
			requestIndex++
		}
		start := requestIndex
		for requestIndex < len(requests) && requests[requestIndex].SourceName == name {
			requestIndex++
		}
		groups = append(groups, marketplaceSourceRequestGroup{name: name, requests: requests[start:requestIndex]})
	}
	return groups
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
