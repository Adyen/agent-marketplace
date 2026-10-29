package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func bootstrapMissingMarketplaceLocks(ctx context.Context, repoRoot, localDir string, selected map[lockfile.Scope]bool, policy resolver.MarketplaceRevocationPolicy) (int, error) {
	intent, desired, err := prepareMissingMarketplaceLockFiles(ctx, repoRoot, localDir, selected, policy)
	if err != nil {
		return 0, err
	}
	if len(desired) == 0 {
		return 0, nil
	}
	reads, err := pipeline.IntentReadAssertions(intent)
	if err != nil {
		return 0, err
	}
	stateDir, err := dependencyStateDir()
	if err != nil {
		return 0, err
	}
	if _, err := commitDependencyTransaction(ctx, stateDir, desired, dependencytxn.WithReadAssertions(reads)); err != nil {
		return 0, fmt.Errorf("commit missing Marketplace locks: %w", err)
	}
	slog.Info("missing Marketplace locks created", "count", len(desired))
	return len(desired), nil
}

// prepareMissingMarketplaceLockEdits computes the Marketplace lock records an
// update would bootstrap for writable layers, without committing them, so a
// preview can stage the same initial acquisition evidence in memory. Planned
// pin replacements merge on top of these bytes exactly as they would merge on
// top of the committed bootstrap during an applying update.
func prepareMissingMarketplaceLockEdits(ctx context.Context, repoRoot, localDir string, selected map[lockfile.Scope]bool, policy resolver.MarketplaceRevocationPolicy) ([]pipeline.StagedLockEdit, error) {
	_, desired, err := prepareMissingMarketplaceLockFiles(ctx, repoRoot, localDir, selected, policy)
	if err != nil {
		return nil, err
	}
	edits := make([]pipeline.StagedLockEdit, 0, len(desired))
	for _, file := range desired {
		edits = append(edits, pipeline.StagedLockEdit{Path: file.Path, Expected: file.Expected, ExpectedMode: file.ExpectedMode, Bytes: file.Bytes, Mode: file.Mode})
	}
	return edits, nil
}

// prepareMissingMarketplaceLockFiles computes the desired missing Marketplace
// lock documents without touching the filesystem: it resolves the records an
// update's bootstrap would create and returns them as dependency desired
// files together with the read intent that observed the pre-publication state.
func prepareMissingMarketplaceLockFiles(ctx context.Context, repoRoot, localDir string, selected map[lockfile.Scope]bool, policy resolver.MarketplaceRevocationPolicy) (config.Intent, []dependencytxn.DesiredFile, error) {
	ws, err := pipeline.WorkspaceForRoots(repoRoot, localDir)
	if err != nil {
		return config.Intent{}, nil, err
	}
	intent := config.ReadIntent(ctx, config.LoadEffectiveOptions{RepoRoot: ws.RepoRoot(), LocalConfigDir: ws.LocalConfigDir()})
	if err := pipeline.MarketplaceAllowlistError(intent); err != nil {
		return config.Intent{}, nil, err
	}
	resolver := marketplace.NewResolver()
	resolver.AllowedRegistries = intent.AllowedRegistries()
	targets, _ := intent.TargetHarnesses()
	var desired []dependencytxn.DesiredFile
	for _, source := range []config.Source{config.UserPersonal, config.RepoCommitted, config.RepoPersonal} {
		layer := intent.Layers[source]
		scope := lockfile.Scope(source.LockScope())
		if selected != nil && !selected[scope] {
			continue
		}
		if !layer.Present() {
			continue
		}
		if err := layer.Err(); err != nil {
			return config.Intent{}, nil, err
		}
		if !layer.NeedsMarketplaceLock() {
			continue
		}
		paths, err := config.ResolveConfigLockPaths(layer.Path)
		if err != nil {
			return config.Intent{}, nil, err
		}
		lockPath, err := rejectGeneratedLockSymlink(paths.CanonicalLock)
		if err != nil {
			return config.Intent{}, nil, err
		}
		current, expected, exists, err := pipeline.ReadLayerLock(lockPath, scope)
		if err != nil {
			return config.Intent{}, nil, err
		}
		// An existing v2 lock keeps its recorded pins and gains only selected
		// resource and registration records it does not already contain.
		var unrecorded []string
		existingMarketplaceLock := exists && current.APIVersion != lockfile.APIVersionV1 && len(current.Marketplaces) > 0
		missingResources := false
		if existingMarketplaceLock {
			unrecorded = unrecordedRegistrations(layer, current)
			missingResources = selectedMarketplaceResourcesMissing(layer, current)
			if len(unrecorded) == 0 && !missingResources {
				continue
			}
		}
		var registries []lockfile.GitRegistry
		var expectedMode os.FileMode
		if exists {
			registries = current.Registries
			if _, expectedMode, err = dependencytxn.ReadGenerationAndMode(lockPath); err != nil {
				return config.Intent{}, nil, err
			}
		}
		var doc lockfile.Document
		if existingMarketplaceLock {
			if missingResources {
				resolved, resolveErr := marketplacelock.Resolve(ctx, resolver, layer, targets, policy)
				if resolveErr != nil {
					return config.Intent{}, nil, fmt.Errorf("record selected Marketplace resources in lock %q: %w", lockPath, resolveErr)
				}
				doc, err = mergeMissingMarketplaceRecords(layer, *current, resolved)
				if err != nil {
					return config.Intent{}, nil, fmt.Errorf("record selected Marketplace resources in lock %q: %w", lockPath, err)
				}
			} else {
				if doc, err = appendRegistrationIdentities(ctx, *current, unrecorded); err != nil {
					return config.Intent{}, nil, fmt.Errorf("record Marketplace registrations in lock %q: %w", lockPath, err)
				}
			}
			if missingResources {
				slog.Info("selected Marketplace resource records reconciled", "scope", scope)
			} else {
				slog.Info("marketplace registration identities recorded", "scope", scope, "count", len(unrecorded))
			}
		} else if doc, err = marketplacelock.Resolve(ctx, resolver, layer, targets, policy); err != nil {
			return config.Intent{}, nil, fmt.Errorf("create Marketplace lock %q: %w", lockPath, err)
		}
		data, err := encodeWithGitRegistries(doc, registries)
		if err != nil {
			return config.Intent{}, nil, err
		}
		mode := expectedMode
		if !expected.Exists {
			mode = 0o644
		}
		desired = append(desired, dependencytxn.DesiredFile{Path: lockPath, Expected: expected, ExpectedMode: expectedMode, Bytes: data, Mode: mode})
	}
	return intent, desired, nil
}

func selectedMarketplaceResourcesMissing(layer config.LayerIntent, current *lockfile.Document) bool {
	if current == nil {
		return false
	}
	for _, source := range layer.MarketplaceSources() {
		var existing *lockfile.MarketplaceLock
		for i := range current.Marketplaces {
			if current.Marketplaces[i].URL == source.URL {
				existing = &current.Marketplaces[i]
				break
			}
		}
		if existing == nil {
			if len(source.Requests) > 0 {
				return true
			}
			continue
		}
		registryIDs := make(map[string]string, len(existing.RegistryBindings))
		for _, binding := range existing.RegistryBindings {
			registryIDs[binding.Name] = binding.RegistryID
		}
		for _, request := range source.Requests {
			registryID := registryIDs[request.Registry]
			if registryID == "" {
				return true
			}
			found := false
			for _, resource := range existing.Resources {
				if resource.RegistryID == registryID && resource.Kind == request.MarketplaceKind() && resource.Name == request.Name {
					if request.Options.Revision != "" && resource.Revision != request.Options.Revision {
						return true
					}
					found = true
					break
				}
			}
			if !found {
				return true
			}
		}
	}
	return false
}

func mergeMissingMarketplaceRecords(layer config.LayerIntent, current, resolved lockfile.Document) (lockfile.Document, error) {
	doc := current
	doc.Marketplaces = slices.Clone(current.Marketplaces)
	changedResources := make(map[string]map[string]bool)
	changedCollections := make(map[string]bool)
	for _, fresh := range resolved.Marketplaces {
		index := -1
		for i := range doc.Marketplaces {
			if doc.Marketplaces[i].URL == fresh.URL {
				index = i
				break
			}
		}
		if index < 0 {
			fresh.Resources = slices.Clone(fresh.Resources)
			for i := range fresh.Resources {
				resource := fresh.Resources[i]
				resource.SelectedBy = marketplacelock.MergeSelectedBy(nil, resource.SelectedBy)
				fresh.Resources[i] = resource
				markMarketplaceResourceChanged(changedResources, fresh.URL, resource)
				if resource.Kind == "collection" {
					changedCollections[fresh.URL] = true
				}
			}
			doc.Marketplaces = append(doc.Marketplaces, fresh)
			continue
		}
		existing := doc.Marketplaces[index]
		if existing.InstanceID != fresh.InstanceID {
			return lockfile.Document{}, fmt.Errorf("marketplace endpoint %q changed instance identity during lock bootstrap", fresh.URL)
		}
		existing.RegistryBindings = slices.Clone(existing.RegistryBindings)
		for _, binding := range fresh.RegistryBindings {
			found := false
			for _, old := range existing.RegistryBindings {
				if old.Name != binding.Name {
					continue
				}
				if old.RegistryID != binding.RegistryID {
					return lockfile.Document{}, fmt.Errorf("marketplace registry %q changed identity during lock bootstrap", binding.Name)
				}
				found = true
				break
			}
			if !found {
				existing.RegistryBindings = append(existing.RegistryBindings, binding)
			}
		}
		existing.Resources = slices.Clone(existing.Resources)
		for _, resource := range fresh.Resources {
			resourceIndex := -1
			for i, old := range existing.Resources {
				if marketplaceResourceKey(old) == marketplaceResourceKey(resource) {
					resourceIndex = i
					break
				}
			}
			if resourceIndex < 0 {
				resource.SelectedBy = marketplacelock.MergeSelectedBy(nil, resource.SelectedBy)
				existing.Resources = append(existing.Resources, resource)
				markMarketplaceResourceChanged(changedResources, fresh.URL, resource)
				if resource.Kind == "collection" {
					changedCollections[fresh.URL] = true
				}
			} else if marketplaceResourceRevisionExplicit(layer, fresh.URL, resource) &&
				existing.Resources[resourceIndex].Revision != resource.Revision {
				resource.SelectedBy = marketplacelock.MergeSelectedBy(existing.Resources[resourceIndex].SelectedBy, resource.SelectedBy)
				existing.Resources[resourceIndex] = resource
				markMarketplaceResourceChanged(changedResources, fresh.URL, resource)
				if resource.Kind == "collection" {
					changedCollections[fresh.URL] = true
				}
			}
		}
		doc.Marketplaces[index] = existing
	}
	for _, fresh := range resolved.Marketplaces {
		if !changedCollections[fresh.URL] {
			continue
		}
		for i := range doc.Marketplaces {
			if doc.Marketplaces[i].URL != fresh.URL {
				continue
			}
			record := &doc.Marketplaces[i]
			for _, resolvedResource := range fresh.Resources {
				for j := range record.Resources {
					resource := &record.Resources[j]
					if marketplaceResourceKey(*resource) != marketplaceResourceKey(resolvedResource) {
						continue
					}
					resource.SelectedBy = marketplacelock.MergeSelectedBy(resource.SelectedBy, resolvedResource.SelectedBy)
					markMarketplaceResourceChanged(changedResources, fresh.URL, *resource)
					break
				}
			}
			for _, resource := range record.Resources {
				markMarketplaceResourceChanged(changedResources, fresh.URL, resource)
			}
			break
		}
	}
	for i := range doc.Marketplaces {
		retainConsistentMarketplaceResources(layer, &doc.Marketplaces[i], changedResources[doc.Marketplaces[i].URL])
	}
	return lockfile.Normalize(doc)
}

func markMarketplaceResourceChanged(changed map[string]map[string]bool, endpoint string, resource lockfile.MarketplaceResourceLock) {
	if changed[endpoint] == nil {
		changed[endpoint] = make(map[string]bool)
	}
	changed[endpoint][marketplaceResourceKey(resource)] = true
}

func retainConsistentMarketplaceResources(layer config.LayerIntent, record *lockfile.MarketplaceLock, changed map[string]bool) {
	if len(changed) == 0 {
		return
	}
	for {
		collections := make(map[string]lockfile.Digest)
		for _, resource := range record.Resources {
			if resource.Kind == "collection" {
				collections[resource.RegistryID+"\x00"+resource.Name] = resource.ContentDigest
			}
		}
		removed := false
		resources := record.Resources[:0]
		for i := range record.Resources {
			resource := &record.Resources[i]
			if changed[marketplaceResourceKey(*resource)] {
				hadSelectionEdges := len(resource.SelectedBy) > 0
				kept := resource.SelectedBy[:0]
				for _, edge := range resource.SelectedBy {
					if digest, ok := collections[edge.RegistryID+"\x00"+edge.Name]; ok && digest == edge.Revision {
						kept = append(kept, edge)
					}
				}
				resource.SelectedBy = kept
				if hadSelectionEdges && len(kept) == 0 && !marketplaceResourceDirectlySelected(layer, record.URL, *resource) {
					removed = true
					continue
				}
			}
			resources = append(resources, *resource)
		}
		record.Resources = resources
		if !removed {
			return
		}
	}
}

func marketplaceResourceDirectlySelected(layer config.LayerIntent, endpoint string, resource lockfile.MarketplaceResourceLock) bool {
	for _, source := range layer.MarketplaceSources() {
		if source.URL != endpoint {
			continue
		}
		for _, request := range source.Requests {
			if (request.Registry == resource.RegistryName || request.Registry == resource.RegistryID) &&
				request.MarketplaceKind() == resource.Kind && request.Name == resource.Name {
				return true
			}
		}
	}
	return false
}

func marketplaceResourceKey(resource lockfile.MarketplaceResourceLock) string {
	return resource.RegistryID + "\x00" + resource.Kind + "\x00" + resource.Name
}

func marketplaceResourceRevisionExplicit(layer config.LayerIntent, endpoint string, resource lockfile.MarketplaceResourceLock) bool {
	for _, source := range layer.MarketplaceSources() {
		if source.URL != endpoint {
			continue
		}
		for _, request := range source.Requests {
			if request.Options.Revision != "" &&
				(request.Registry == resource.RegistryName || request.Registry == resource.RegistryID) &&
				request.MarketplaceKind() == resource.Kind &&
				request.Name == resource.Name {
				return true
			}
		}
	}
	return false
}

// unrecordedRegistrations returns the canonical endpoints of the layer's
// resource-free registrations that an existing lock does not identify yet.
// A lock that does not bind to the layer gains nothing here, so the reload
// that follows reports its binding error unchanged.
func unrecordedRegistrations(layer config.LayerIntent, current *lockfile.Document) []string {
	if err := layer.ValidateDeployedMarketplaceLock(current); err != nil {
		slog.Debug("existing Marketplace lock does not bind; registration identities not recorded", "scope", current.Scope, "error", err)
		return nil
	}
	recorded := make(map[string]bool, len(current.Marketplaces))
	for _, locked := range current.Marketplaces {
		recorded[locked.URL] = true
	}
	var endpoints []string
	for _, source := range layer.MarketplaceSources() {
		if len(source.Requests) == 0 && !recorded[source.URL] {
			endpoints = append(endpoints, source.URL)
		}
	}
	return endpoints
}

// appendRegistrationIdentities adds one identity-only record per endpoint,
// leaving every existing record of current untouched.
func appendRegistrationIdentities(ctx context.Context, current lockfile.Document, endpoints []string) (lockfile.Document, error) {
	doc := current
	doc.Marketplaces = slices.Clone(current.Marketplaces)
	for _, endpoint := range endpoints {
		instanceID, err := marketplacelock.EndpointInstanceID(ctx, endpoint)
		if err != nil {
			return lockfile.Document{}, fmt.Errorf("identify Marketplace endpoint %s: %w", endpoint, err)
		}
		doc.Marketplaces = append(doc.Marketplaces, lockfile.MarketplaceLock{URL: endpoint, InstanceID: instanceID})
	}
	return doc, nil
}

func encodeWithGitRegistries(doc lockfile.Document, registries []lockfile.GitRegistry) ([]byte, error) {
	doc.Registries = registries
	normalized, err := lockfile.Normalize(doc)
	if err != nil {
		return nil, err
	}
	return lockfile.Encode(normalized)
}

type archiveRefreshKey struct{ endpoint, instance, registryID, kind, name string }

func archiveRefreshWarningKey(w *resolver.ArchiveEvidenceRefreshedWarning) archiveRefreshKey {
	return archiveRefreshKey{w.MarketplaceURL, w.InstanceID, w.RegistryID, w.Kind, w.Name}
}

func archiveRefreshPinKey(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) archiveRefreshKey {
	return archiveRefreshKey{entry.URL, entry.InstanceID, pin.RegistryID, pin.Kind, pin.Name}
}

func stripUnpersistedRefreshWarnings(warnings []error, persisted map[archiveRefreshKey]bool) []error {
	kept := make([]error, 0, len(warnings))
	for _, w := range warnings {
		var refresh *resolver.ArchiveEvidenceRefreshedWarning
		if errors.As(w, &refresh) && !persisted[archiveRefreshWarningKey(refresh)] {
			continue
		}
		kept = append(kept, w)
	}
	return kept
}

func refreshDriftedMarketplaceLocks(ctx context.Context, repoRoot, localDir string, mergedCfg *config.Config, warnings []error) (map[archiveRefreshKey]bool, error) {
	if mergedCfg == nil {
		return nil, nil
	}
	refreshed := make(map[archiveRefreshKey]bool)
	for _, w := range warnings {
		var refresh *resolver.ArchiveEvidenceRefreshedWarning
		if errors.As(w, &refresh) {
			refreshed[archiveRefreshWarningKey(refresh)] = true
		}
	}
	if len(refreshed) == 0 {
		return nil, nil
	}
	intent := config.ReadIntent(ctx, config.LoadEffectiveOptions{RepoRoot: repoRoot, LocalConfigDir: localDir})
	cache := marketplace.DefaultCache()
	type pendingMerge struct {
		entry config.MarketplaceEntry
		pins  []config.MarketplaceResourcePin
	}
	type layerWork struct {
		path         string
		v4           *config.V4Config
		current      *lockfile.Document
		expected     dependencytxn.Generation
		expectedMode os.FileMode
		merges       []pendingMerge
	}
	var order []config.Source
	layers := make(map[config.Source]*layerWork)
	for _, entry := range mergedCfg.Marketplaces {
		var pins []config.MarketplaceResourcePin
		for _, pin := range entry.Resources {
			if refreshed[archiveRefreshPinKey(entry, pin)] {
				pins = append(pins, pin)
			}
		}
		if len(pins) == 0 || !entry.Source.IsWritable() {
			continue
		}
		work, ok := layers[entry.Source]
		if !ok {
			layer := intent.Layers[entry.Source]
			if !layer.Present() {
				continue
			}
			if err := layer.Err(); err != nil {
				return nil, err
			}
			v4Cfg, err := config.LoadV4ConfigIfPresent(layer.Path, entry.Source)
			if err != nil {
				return nil, err
			}
			if v4Cfg == nil {
				continue
			}
			paths, err := config.ResolveConfigLockPaths(layer.Path)
			if err != nil {
				return nil, err
			}
			lockPath, err := rejectGeneratedLockSymlink(paths.CanonicalLock)
			if err != nil {
				return nil, err
			}
			current, expected, exists, err := pipeline.ReadLayerLock(lockPath, lockfile.Scope(entry.Source.LockScope()))
			if err != nil {
				return nil, err
			}
			if !exists || current == nil || len(current.Marketplaces) == 0 {
				continue
			}
			_, expectedMode, err := dependencytxn.ReadGenerationAndMode(lockPath)
			if err != nil {
				return nil, err
			}
			work = &layerWork{path: lockPath, v4: v4Cfg, current: current, expected: expected, expectedMode: expectedMode}
			layers[entry.Source] = work
			order = append(order, entry.Source)
		}
		work.merges = append(work.merges, pendingMerge{entry: entry, pins: pins})
	}
	var desired []dependencytxn.DesiredFile
	stagedKeysByPath := make(map[string][]archiveRefreshKey)
	for _, source := range order {
		work := layers[source]
		doc := *work.current
		for _, merge := range work.merges {
			merged, err := marketplacelock.MergePinsToV4(work.v4, doc, merge.entry, merge.pins, cache)
			if err != nil {
				return nil, fmt.Errorf("refresh Marketplace lock %q evidence: %w", work.path, err)
			}
			doc = merged
		}
		data, err := lockfile.Encode(doc)
		if err != nil {
			return nil, err
		}
		normalizedCurrent, err := lockfile.Normalize(*work.current)
		if err != nil {
			return nil, err
		}
		currentData, err := lockfile.Encode(normalizedCurrent)
		if err != nil {
			return nil, err
		}
		if string(data) == string(currentData) {
			continue
		}
		desired = append(desired, dependencytxn.DesiredFile{Path: work.path, Expected: work.expected, ExpectedMode: work.expectedMode, Bytes: data, Mode: work.expectedMode})
		for _, merge := range work.merges {
			for _, pin := range merge.pins {
				key := archiveRefreshPinKey(merge.entry, pin)
				stagedKeysByPath[work.path] = append(stagedKeysByPath[work.path], key)
			}
		}
	}
	if len(desired) == 0 {
		return nil, nil
	}
	reads, err := pipeline.IntentReadAssertions(intent)
	if err != nil {
		return nil, err
	}
	stateDir, err := dependencyStateDir()
	if err != nil {
		return nil, err
	}
	committed, err := commitDependencyTransaction(ctx, stateDir, desired, dependencytxn.WithReadAssertions(reads))
	if err != nil {
		return nil, fmt.Errorf("commit refreshed Marketplace lock evidence: %w", err)
	}
	slog.Info("marketplace lock evidence refreshed", "count", len(committed.Changed))
	persisted := make(map[archiveRefreshKey]bool)
	for _, path := range committed.Changed {
		for _, key := range stagedKeysByPath[path] {
			persisted[key] = true
		}
	}
	return persisted, nil
}
