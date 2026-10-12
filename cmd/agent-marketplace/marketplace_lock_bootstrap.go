package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func bootstrapMissingMarketplaceLocks(ctx context.Context, repoRoot, localDir string, selected map[lockfile.Scope]bool, policy resolver.MarketplaceRevocationPolicy) (int, error) {
	ws, err := pipeline.WorkspaceForRoots(repoRoot, localDir)
	if err != nil {
		return 0, err
	}
	intent := config.ReadIntent(ctx, config.LoadEffectiveOptions{RepoRoot: ws.RepoRoot(), LocalConfigDir: ws.LocalConfigDir()})
	if err := pipeline.MarketplaceAllowlistError(intent); err != nil {
		return 0, err
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
			return 0, err
		}
		if !layer.NeedsMarketplaceLock() {
			continue
		}
		paths, err := config.ResolveConfigLockPaths(layer.Path)
		if err != nil {
			return 0, err
		}
		lockPath, err := rejectGeneratedLockSymlink(paths.CanonicalLock)
		if err != nil {
			return 0, err
		}
		current, expected, exists, err := pipeline.ReadLayerLock(lockPath, scope)
		if err != nil {
			return 0, err
		}
		if exists && current.APIVersion != lockfile.APIVersionV1 && len(current.Marketplaces) > 0 {
			continue
		}
		var registries []lockfile.GitRegistry
		var expectedMode os.FileMode
		if exists {
			registries = current.Registries
			if _, expectedMode, err = dependencytxn.ReadGenerationAndMode(lockPath); err != nil {
				return 0, err
			}
		}
		doc, err := marketplacelock.Resolve(ctx, resolver, layer, targets, policy)
		if err != nil {
			return 0, fmt.Errorf("create Marketplace lock %q: %w", lockPath, err)
		}
		data, err := encodeWithGitRegistries(doc, registries)
		if err != nil {
			return 0, err
		}
		if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
			return 0, fmt.Errorf("create Marketplace lock directory: %w", err)
		}
		mode := expectedMode
		if !expected.Exists {
			mode = 0o644
		}
		desired = append(desired, dependencytxn.DesiredFile{Path: lockPath, Expected: expected, ExpectedMode: expectedMode, Bytes: data, Mode: mode})
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
