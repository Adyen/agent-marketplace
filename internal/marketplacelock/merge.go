package marketplacelock

import (
	"fmt"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
)

func lockMismatch(path, format string, args ...any) error {
	return &config.V4ValidationError{Code: "v4.marketplace-lock-mismatch", Path: path, Msg: fmt.Sprintf(format, args...)}
}

func cloneMarketplaceLockRecords(records []lockfile.MarketplaceLock) []lockfile.MarketplaceLock {
	cloned := make([]lockfile.MarketplaceLock, len(records))
	for i, record := range records {
		cloned[i] = record
		cloned[i].RegistryBindings = append([]lockfile.MarketplaceRegistryBinding(nil), record.RegistryBindings...)
		cloned[i].Resources = make([]lockfile.MarketplaceResourceLock, len(record.Resources))
		for j, resource := range record.Resources {
			cloned[i].Resources[j] = resource
			cloned[i].Resources[j].SelectedBy = append([]lockfile.MarketplaceSelectionLock(nil), resource.SelectedBy...)
		}
	}
	return cloned
}

type marketplaceResourceEvidence struct {
	RegistryName             string
	RegistryID               string
	Kind                     string
	Name                     string
	Revision                 string
	ContentDigest            lockfile.Digest
	ArchiveDigest            lockfile.Digest
	CollectionManifestDigest lockfile.Digest
	Acquisition              string
	RegistryCommit           string
	RegistryManifestDigest   lockfile.Digest
	SourcePath               string
}

func evidenceOf(record lockfile.MarketplaceResourceLock) marketplaceResourceEvidence {
	return marketplaceResourceEvidence{
		RegistryName:             record.RegistryName,
		RegistryID:               record.RegistryID,
		Kind:                     record.Kind,
		Name:                     record.Name,
		Revision:                 record.Revision,
		ContentDigest:            record.ContentDigest,
		ArchiveDigest:            record.ArchiveDigest,
		CollectionManifestDigest: record.CollectionManifestDigest,
		Acquisition:              record.Acquisition,
		RegistryCommit:           record.RegistryCommit,
		RegistryManifestDigest:   record.RegistryManifestDigest,
		SourcePath:               record.SourcePath,
	}
}

func mergeSelectedBy(existing, added []lockfile.MarketplaceSelectionLock) []lockfile.MarketplaceSelectionLock {
	seen := make(map[string]bool, len(existing)+len(added))
	merged := make([]lockfile.MarketplaceSelectionLock, 0, len(existing)+len(added))
	for _, selections := range [][]lockfile.MarketplaceSelectionLock{existing, added} {
		for _, edge := range selections {
			key := edge.RegistryID + "\x00" + edge.Name + "\x00" + string(edge.Revision)
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, edge)
		}
	}
	return merged
}

func MergePinsToV4(v4 *config.V4Config, current lockfile.Document, entry config.MarketplaceEntry, pins []config.MarketplaceResourcePin, cache *marketplace.Cache) (lockfile.Document, error) {
	if v4 == nil {
		return lockfile.Document{}, lockMismatch("lock", "candidate v4 configuration is missing")
	}
	if current.APIVersion != "" && current.APIVersion != lockfile.APIVersionV1 && current.APIVersion != lockfile.APIVersionV2 {
		return lockfile.Document{}, lockMismatch("lock", "unsupported lock apiVersion %q", current.APIVersion)
	}
	if current.Scope != "" && !current.Scope.Valid() {
		return lockfile.Document{}, lockMismatch("lock", "invalid lock scope %q", current.Scope)
	}
	if wantScope := entry.Source.LockScope(); wantScope != "" && current.Scope != "" && string(current.Scope) != wantScope {
		return lockfile.Document{}, lockMismatch("lock", "lock scope %q does not match source scope %q", current.Scope, wantScope)
	}
	if len(pins) > 0 && cache == nil {
		return lockfile.Document{}, lockMismatch("lock", "verified Marketplace cache is required to merge pins")
	}
	canonical, err := marketplace.NormalizeMarketplaceURL(entry.URL)
	if err != nil {
		return lockfile.Document{}, err
	}
	if entry.InstanceID == "" {
		return lockfile.Document{}, lockMismatch("marketplaces", "Marketplace endpoint %q has no verified instance identity", canonical)
	}
	var sourceNames []string
	for name, source := range v4.Sources {
		if source.Kind == config.V4SourceMarketplace && source.CanonicalURL == canonical {
			sourceNames = append(sourceNames, name)
		}
	}
	if len(sourceNames) == 0 {
		return lockfile.Document{}, lockMismatch("sources", "Marketplace endpoint %q has no v4 source", canonical)
	}
	if len(sourceNames) > 1 {
		sort.Strings(sourceNames)
		return lockfile.Document{}, lockMismatch("sources", "Marketplace endpoint %q matches multiple v4 sources: %v", canonical, sourceNames)
	}
	sourceName := sourceNames[0]

	doc := current
	doc.APIVersion = lockfile.APIVersionV2
	doc.Registries = append([]lockfile.GitRegistry(nil), current.Registries...)
	doc.Marketplaces = cloneMarketplaceLockRecords(current.Marketplaces)

	merged := make([]lockfile.MarketplaceLock, 0, len(doc.Marketplaces)+1)
	var existing *lockfile.MarketplaceLock
	for i := range doc.Marketplaces {
		record := doc.Marketplaces[i]
		recordCanonical, err := marketplace.NormalizeMarketplaceURL(record.URL)
		if err != nil {
			return lockfile.Document{}, lockMismatch("marketplaces", "lock endpoint %q is not a valid URL: %v", record.URL, err)
		}
		if recordCanonical == canonical {
			if existing != nil {
				return lockfile.Document{}, lockMismatch("marketplaces", "lock contains duplicate endpoint records for %q", canonical)
			}
			copy := record
			existing = &copy
			continue
		}
		kept, err := pruneEndpointRecord(v4, record)
		if err != nil {
			return lockfile.Document{}, err
		}
		if kept != nil {
			merged = append(merged, *kept)
		}
	}
	if existing != nil && existing.InstanceID != entry.InstanceID {
		return lockfile.Document{}, lockMismatch("marketplaces", "endpoint %q instance %q conflicts with prepared identity %q", canonical, existing.InstanceID, entry.InstanceID)
	}

	next := lockfile.MarketplaceLock{URL: canonical, InstanceID: entry.InstanceID}
	if existing != nil {
		retained, err := reachableResources(v4, sourceName, existing.Resources)
		if err != nil {
			return lockfile.Document{}, err
		}
		next.Resources = retained
	}
	resources := make(map[string]lockfile.MarketplaceResourceLock, len(next.Resources)+len(pins))
	for _, record := range next.Resources {
		key := record.RegistryID + "\x00" + record.Kind + "\x00" + record.Name
		resources[key] = record
	}
	for _, pin := range pins {
		record, err := cache.VerifiedMarketplaceLockResource(entry, pin)
		if err != nil {
			return lockfile.Document{}, fmt.Errorf("verify %s/%s/%s lock evidence: %w", pin.RegistryID, pin.Kind, pin.Name, err)
		}
		key := record.RegistryID + "\x00" + record.Kind + "\x00" + record.Name
		if previous, ok := resources[key]; ok {
			if evidenceOf(previous) != evidenceOf(record) {
				return lockfile.Document{}, lockMismatch("marketplaces", "resource %s/%s/%s evidence conflicts with existing lock record", pin.RegistryID, pin.Kind, pin.Name)
			}
			record.SelectedBy = mergeSelectedBy(previous.SelectedBy, record.SelectedBy)
		}
		resources[key] = record
	}
	next.Resources = next.Resources[:0]
	for _, record := range resources {
		next.Resources = append(next.Resources, record)
	}
	bindings := make(map[string]string)
	for _, record := range next.Resources {
		if previous, ok := bindings[record.RegistryName]; ok && previous != record.RegistryID {
			return lockfile.Document{}, lockMismatch("marketplaces", "registry binding %q resolves to both %q and %q", record.RegistryName, previous, record.RegistryID)
		}
		bindings[record.RegistryName] = record.RegistryID
	}
	if existing != nil {
		for _, binding := range existing.RegistryBindings {
			if _, referenced := bindings[binding.Name]; !referenced {
				continue
			}
			if bindings[binding.Name] != binding.RegistryID {
				return lockfile.Document{}, lockMismatch("marketplaces", "registry binding %q changes %q to %q", binding.Name, binding.RegistryID, bindings[binding.Name])
			}
		}
	}
	next.RegistryBindings = nil
	for name, id := range bindings {
		next.RegistryBindings = append(next.RegistryBindings, lockfile.MarketplaceRegistryBinding{Name: name, RegistryID: id})
	}
	merged = append(merged, next)
	doc.Marketplaces = merged
	return lockfile.Normalize(doc)
}

func pruneEndpointRecord(v4 *config.V4Config, record lockfile.MarketplaceLock) (*lockfile.MarketplaceLock, error) {
	canonical, err := marketplace.NormalizeMarketplaceURL(record.URL)
	if err != nil {
		return nil, lockMismatch("marketplaces", "lock endpoint %q is not a valid URL: %v", record.URL, err)
	}
	sourceName := ""
	for name, source := range v4.Sources {
		if source.Kind == config.V4SourceMarketplace && source.CanonicalURL == canonical {
			sourceName = name
		}
	}
	if sourceName == "" {
		return nil, nil
	}
	resources, err := reachableResources(v4, sourceName, record.Resources)
	if err != nil {
		return nil, err
	}
	kept := record
	kept.Resources = resources
	referenced := make(map[string]bool, len(resources))
	for _, resource := range resources {
		referenced[resource.RegistryName] = true
	}
	bindings := make([]lockfile.MarketplaceRegistryBinding, 0, len(kept.RegistryBindings))
	for _, binding := range kept.RegistryBindings {
		if referenced[binding.Name] {
			bindings = append(bindings, binding)
		}
	}
	kept.RegistryBindings = bindings
	return &kept, nil
}

func reachableResources(v4 *config.V4Config, sourceName string, records []lockfile.MarketplaceResourceLock) ([]lockfile.MarketplaceResourceLock, error) {
	if _, ok := v4.Sources[sourceName]; !ok {
		return nil, lockMismatch("marketplaces", "lock references missing v4 source %q", sourceName)
	}
	requested := make(map[string]bool)
	for i := range v4.Requests {
		request := &v4.Requests[i]
		if request.SourceName != sourceName {
			continue
		}
		requested[request.Registry+"\x00"+v4MarketplaceLockKind(request.Kind)+"\x00"+request.Name] = true
	}
	retained := make([]lockfile.MarketplaceResourceLock, 0, len(records))
	selected := make(map[string]bool)
	remaining := append([]lockfile.MarketplaceResourceLock(nil), records...)
	for {
		var next []lockfile.MarketplaceResourceLock
		progress := false
		for _, record := range remaining {
			direct := requested[record.RegistryName+"\x00"+record.Kind+"\x00"+record.Name]
			member := false
			for _, edge := range record.SelectedBy {
				if selected[edge.RegistryID+"\x00"+edge.Name+"\x00"+string(edge.Revision)] {
					member = true
					break
				}
			}
			if direct || member {
				retained = append(retained, record)
				selected[record.RegistryID+"\x00"+record.Name+"\x00"+string(record.ContentDigest)] = true
				progress = true
				continue
			}
			next = append(next, record)
		}
		remaining = next
		if !progress {
			break
		}
	}
	return retained, nil
}
