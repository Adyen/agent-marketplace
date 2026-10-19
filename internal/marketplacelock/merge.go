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

func sameEvidenceModuloArchiveDigest(a, b marketplaceResourceEvidence) bool {
	a.ArchiveDigest = ""
	b.ArchiveDigest = ""
	return a == b
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
	if len(pins) > 0 && cache == nil {
		return lockfile.Document{}, lockMismatch("lock", "verified Marketplace cache is required to merge pins")
	}
	doc, canonical, sourceName, merged, existing, err := beginV4MarketplaceMerge(v4, current, entry)
	if err != nil {
		return lockfile.Document{}, err
	}
	return finishV4MarketplaceMerge(v4, doc, canonical, sourceName, entry, merged, existing, pins, cache)
}

func ReplacePinsToV4(v4 *config.V4Config, current lockfile.Document, entry config.MarketplaceEntry, expectedRoot config.MarketplaceResourcePin, replacements []config.MarketplaceResourcePin, cache *marketplace.Cache) (lockfile.Document, error) {
	if len(replacements) == 0 {
		return lockfile.Document{}, lockMismatch("lock", "Marketplace update requires a verified replacement")
	}
	if cache == nil {
		return lockfile.Document{}, lockMismatch("lock", "verified Marketplace cache is required to replace pins")
	}
	if err := config.ValidateMarketplaceResourcePin(expectedRoot); err != nil {
		return lockfile.Document{}, err
	}
	doc, canonical, sourceName, merged, existing, err := beginV4MarketplaceMerge(v4, current, entry)
	if err != nil {
		return lockfile.Document{}, err
	}
	if existing == nil {
		return lockfile.Document{}, lockMismatch("marketplaces", "endpoint %q has no lock record to update", canonical)
	}
	registryName := expectedRoot.RegistrySlug
	if registryName == "" {
		registryName = expectedRoot.RegistryID
	}
	registryNames := map[string]bool{registryName: true}
	for _, binding := range existing.RegistryBindings {
		if binding.RegistryID == expectedRoot.RegistryID {
			registryNames[binding.Name] = true
		}
	}
	for i := range v4.Requests {
		request := &v4.Requests[i]
		if request.SourceName != sourceName {
			continue
		}
		if registryNames[request.Registry] && request.MarketplaceKind() == expectedRoot.Kind && request.Name == expectedRoot.Name && request.Options.Revision != "" {
			return lockfile.Document{}, lockMismatch("marketplaces", "request %q pins an explicit revision and cannot be updated", request.Locator)
		}
	}
	root := -1
	for i := range existing.Resources {
		record := &existing.Resources[i]
		if record.RegistryID == expectedRoot.RegistryID && record.Kind == expectedRoot.Kind && record.Name == expectedRoot.Name {
			root = i
			break
		}
	}
	if root < 0 {
		return lockfile.Document{}, lockMismatch("marketplaces", "resource %s/%s/%s has no lock record to update", expectedRoot.RegistryID, expectedRoot.Kind, expectedRoot.Name)
	}
	verified, err := cache.VerifiedMarketplaceLockResource(entry, expectedRoot)
	if err != nil {
		return lockfile.Document{}, fmt.Errorf("verify expected %s/%s/%s lock evidence: %w", expectedRoot.RegistryID, expectedRoot.Kind, expectedRoot.Name, err)
	}
	if !sameEvidenceModuloArchiveDigest(evidenceOf(existing.Resources[root]), evidenceOf(verified)) {
		return lockfile.Document{}, lockMismatch("marketplaces", "resource %s/%s/%s lock evidence no longer matches the expected pin", expectedRoot.RegistryID, expectedRoot.Kind, expectedRoot.Name)
	}
	existing.Resources = append(existing.Resources[:root], existing.Resources[root+1:]...)
	if expectedRoot.Kind == "collection" {
		for i := range existing.Resources {
			record := &existing.Resources[i]
			kept := record.SelectedBy[:0]
			for _, edge := range record.SelectedBy {
				if edge.RegistryID == expectedRoot.RegistryID && edge.Name == expectedRoot.Name && string(edge.Revision) == expectedRoot.Revision {
					continue
				}
				kept = append(kept, edge)
			}
			record.SelectedBy = kept
		}
	}
	return finishV4MarketplaceMerge(v4, doc, canonical, sourceName, entry, merged, existing, replacements, cache)
}

func beginV4MarketplaceMerge(v4 *config.V4Config, current lockfile.Document, entry config.MarketplaceEntry) (lockfile.Document, string, string, []lockfile.MarketplaceLock, *lockfile.MarketplaceLock, error) {
	if v4 == nil {
		return lockfile.Document{}, "", "", nil, nil, lockMismatch("lock", "candidate v4 configuration is missing")
	}
	if current.APIVersion != "" && current.APIVersion != lockfile.APIVersionV1 && current.APIVersion != lockfile.APIVersionV2 {
		return lockfile.Document{}, "", "", nil, nil, lockMismatch("lock", "unsupported lock apiVersion %q", current.APIVersion)
	}
	if current.Scope != "" && !current.Scope.Valid() {
		return lockfile.Document{}, "", "", nil, nil, lockMismatch("lock", "invalid lock scope %q", current.Scope)
	}
	if wantScope := entry.Source.LockScope(); wantScope != "" && current.Scope != "" && string(current.Scope) != wantScope {
		return lockfile.Document{}, "", "", nil, nil, lockMismatch("lock", "lock scope %q does not match source scope %q", current.Scope, wantScope)
	}
	canonical, err := marketplace.NormalizeMarketplaceURL(entry.URL)
	if err != nil {
		return lockfile.Document{}, "", "", nil, nil, err
	}
	if entry.InstanceID == "" {
		return lockfile.Document{}, "", "", nil, nil, lockMismatch("marketplaces", "Marketplace endpoint %q has no verified instance identity", canonical)
	}
	var sourceNames []string
	for name, source := range v4.Sources {
		if source.Kind == config.V4SourceMarketplace && source.CanonicalURL == canonical {
			sourceNames = append(sourceNames, name)
		}
	}
	if len(sourceNames) == 0 {
		return lockfile.Document{}, "", "", nil, nil, lockMismatch("sources", "Marketplace endpoint %q has no v4 source", canonical)
	}
	if len(sourceNames) > 1 {
		sort.Strings(sourceNames)
		return lockfile.Document{}, "", "", nil, nil, lockMismatch("sources", "Marketplace endpoint %q matches multiple v4 sources: %v", canonical, sourceNames)
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
			return lockfile.Document{}, "", "", nil, nil, lockMismatch("marketplaces", "lock endpoint %q is not a valid URL: %v", record.URL, err)
		}
		if recordCanonical == canonical {
			if existing != nil {
				return lockfile.Document{}, "", "", nil, nil, lockMismatch("marketplaces", "lock contains duplicate endpoint records for %q", canonical)
			}
			copy := record
			existing = &copy
			continue
		}
		kept, err := pruneEndpointRecord(v4, record)
		if err != nil {
			return lockfile.Document{}, "", "", nil, nil, err
		}
		if kept != nil {
			merged = append(merged, *kept)
		}
	}
	if existing != nil {
		if existing.InstanceID != entry.InstanceID {
			return lockfile.Document{}, "", "", nil, nil, lockMismatch("marketplaces", "endpoint %q instance %q conflicts with prepared identity %q", canonical, existing.InstanceID, entry.InstanceID)
		}
		// Check namespace ownership before replacement removes the old root.
		if _, err := marketplaceRegistryIDs(existing.RegistryBindings, existing.Resources); err != nil {
			return lockfile.Document{}, "", "", nil, nil, err
		}
	}
	return doc, canonical, sourceName, merged, existing, nil
}

func finishV4MarketplaceMerge(v4 *config.V4Config, doc lockfile.Document, canonical, sourceName string, entry config.MarketplaceEntry, merged []lockfile.MarketplaceLock, existing *lockfile.MarketplaceLock, pins []config.MarketplaceResourcePin, cache *marketplace.Cache) (lockfile.Document, error) {
	next := lockfile.MarketplaceLock{URL: canonical, InstanceID: entry.InstanceID}
	if existing != nil {
		retained, err := reachableResources(v4, sourceName, existing.Resources, existing.RegistryBindings...)
		if err != nil {
			return lockfile.Document{}, err
		}
		next.Resources = retained
	}
	resources := make(map[string]lockfile.MarketplaceResourceLock, int64(len(next.Resources))+int64(len(pins)))
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
			if !sameEvidenceModuloArchiveDigest(evidenceOf(previous), evidenceOf(record)) {
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
	var previousBindings []lockfile.MarketplaceRegistryBinding
	if existing != nil {
		previousBindings = existing.RegistryBindings
	}
	var err error
	next.RegistryBindings, err = retainedRegistryBindings(v4, sourceName, next.Resources, previousBindings)
	if err != nil {
		return lockfile.Document{}, err
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
	resources, err := reachableResources(v4, sourceName, record.Resources, record.RegistryBindings...)
	if err != nil {
		return nil, err
	}
	kept := record
	kept.Resources = resources
	kept.RegistryBindings, err = retainedRegistryBindings(v4, sourceName, resources, record.RegistryBindings)
	if err != nil {
		return nil, err
	}
	return &kept, nil
}

// marketplaceRegistryIDs checks the endpoint's namespace before pruning can
// discard evidence of a conflicting binding.
func marketplaceRegistryIDs(bindings []lockfile.MarketplaceRegistryBinding, records []lockfile.MarketplaceResourceLock) (map[string]string, error) {
	if len(bindings) == 0 {
		return nil, nil
	}
	ids := make(map[string]string, len(bindings))
	for _, binding := range bindings {
		if previous, ok := ids[binding.Name]; ok && previous != binding.RegistryID {
			return nil, lockMismatch("marketplaces", "registry binding %q resolves to both %q and %q", binding.Name, previous, binding.RegistryID)
		}
		ids[binding.Name] = binding.RegistryID
	}
	for _, record := range records {
		if id, ok := ids[record.RegistryName]; ok && id != record.RegistryID {
			return nil, lockMismatch("marketplaces", "registry binding %q changes %q to %q", record.RegistryName, id, record.RegistryID)
		}
	}
	return ids, nil
}

func retainedRegistryBindings(v4 *config.V4Config, sourceName string, records []lockfile.MarketplaceResourceLock, previous []lockfile.MarketplaceRegistryBinding) ([]lockfile.MarketplaceRegistryBinding, error) {
	oldIDs, err := marketplaceRegistryIDs(previous, records)
	if err != nil {
		return nil, err
	}
	bindings := make(map[string]string)
	resources := make(map[string]bool, len(records))
	for _, record := range records {
		if id, ok := bindings[record.RegistryName]; ok && id != record.RegistryID {
			return nil, lockMismatch("marketplaces", "registry binding %q resolves to both %q and %q", record.RegistryName, id, record.RegistryID)
		}
		bindings[record.RegistryName] = record.RegistryID
		resources[record.RegistryID+"\x00"+record.Kind+"\x00"+record.Name] = true
	}
	// A current slug and an authored alias may name the same immutable registry.
	// Preserve only aliases still selecting a resource of that exact registry;
	// unrelated old bindings must not grant a replacement a different identity.
	for _, request := range v4.Requests {
		if request.SourceName != sourceName {
			continue
		}
		id, ok := oldIDs[request.Registry]
		if ok && resources[id+"\x00"+request.MarketplaceKind()+"\x00"+request.Name] {
			bindings[request.Registry] = id
		}
	}
	var retained []lockfile.MarketplaceRegistryBinding
	for name, id := range bindings {
		retained = append(retained, lockfile.MarketplaceRegistryBinding{Name: name, RegistryID: id})
	}
	return retained, nil
}

func reachableResources(v4 *config.V4Config, sourceName string, records []lockfile.MarketplaceResourceLock, registryBindings ...lockfile.MarketplaceRegistryBinding) ([]lockfile.MarketplaceResourceLock, error) {
	if _, ok := v4.Sources[sourceName]; !ok {
		return nil, lockMismatch("marketplaces", "lock references missing v4 source %q", sourceName)
	}
	registryIDs, err := marketplaceRegistryIDs(registryBindings, records)
	if err != nil {
		return nil, err
	}
	requested := make(map[string]bool)
	for i := range v4.Requests {
		request := &v4.Requests[i]
		if request.SourceName != sourceName {
			continue
		}
		registry := request.Registry
		if len(registryBindings) > 0 {
			var ok bool
			registry, ok = registryIDs[request.Registry]
			if !ok {
				continue
			}
		}
		requested[registry+"\x00"+request.MarketplaceKind()+"\x00"+request.Name] = true
	}
	retained := make([]lockfile.MarketplaceResourceLock, 0, len(records))
	selected := make(map[string]bool)
	remaining := append([]lockfile.MarketplaceResourceLock(nil), records...)
	for {
		var next []lockfile.MarketplaceResourceLock
		progress := false
		for _, record := range remaining {
			registry := record.RegistryName
			if len(registryBindings) > 0 {
				registry = record.RegistryID
			}
			direct := requested[registry+"\x00"+record.Kind+"\x00"+record.Name]
			member := false
			for _, edge := range record.SelectedBy {
				if selected[edge.RegistryID+"\x00"+edge.Name+"\x00"+string(edge.Revision)] {
					member = true
					break
				}
			}
			if direct || member {
				retained = append(retained, record)
				if record.Kind == "collection" {
					selected[record.RegistryID+"\x00"+record.Name+"\x00"+string(record.ContentDigest)] = true
				}
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
	for i := range retained {
		var edges []lockfile.MarketplaceSelectionLock
		for index, edge := range retained[i].SelectedBy {
			if selected[edge.RegistryID+"\x00"+edge.Name+"\x00"+string(edge.Revision)] {
				if edges != nil {
					edges = append(edges, edge)
				}
			} else if edges == nil {
				edges = make([]lockfile.MarketplaceSelectionLock, 0, len(retained[i].SelectedBy)-1)
				edges = append(edges, retained[i].SelectedBy[:index]...)
			}
		}
		if edges != nil {
			retained[i].SelectedBy = edges
		}
	}
	return retained, nil
}
