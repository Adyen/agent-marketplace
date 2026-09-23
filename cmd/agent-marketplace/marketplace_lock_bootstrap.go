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

type marketplaceLockBootstrap struct {
	marketplacelock.Layer
	current  *lockfile.Document
	expected dependencytxn.Generation
	mode     os.FileMode
}

func bootstrapMissingMarketplaceLocks(ctx context.Context, repoRoot, localDir string, selected map[lockfile.Scope]bool, policy resolver.MarketplaceRevocationPolicy) (int, error) {
	layers, overlays, err := missingMarketplaceLockLayers(ctx, repoRoot, localDir, selected)
	if err != nil || len(layers) == 0 {
		return 0, err
	}
	ec := pipeline.LoadEffectiveForLockPlanning(ctx, config.LoadEffectiveOptions{RepoRoot: repoRoot, LocalConfigDir: localDir, FileOverlays: overlays})
	if len(ec.Errors) > 0 {
		return 0, errors.Join(ec.Errors...)
	}
	resolver := marketplace.NewResolver()
	resolver.AllowedRegistries = ec.Allowed
	desired := make([]dependencytxn.DesiredFile, 0, len(layers))
	for _, layer := range layers {
		doc, err := marketplacelock.Resolve(ctx, resolver, layer.Layer, ec.TargetHarnesses, policy)
		if err != nil {
			return 0, fmt.Errorf("create Marketplace lock %q: %w", layer.LockPath, err)
		}
		if layer.current != nil {
			doc.Registries = layer.current.Registries
		}
		data, err := lockfile.Encode(doc)
		if err != nil {
			return 0, err
		}
		if err := os.MkdirAll(filepath.Dir(layer.LockPath), 0o755); err != nil {
			return 0, fmt.Errorf("create Marketplace lock directory: %w", err)
		}
		mode := layer.mode
		if !layer.expected.Exists {
			mode = 0o644
		}
		desired = append(desired, dependencytxn.DesiredFile{Path: layer.LockPath, Expected: layer.expected, ExpectedMode: layer.mode, Bytes: data, Mode: mode})
	}
	reads, err := effectiveReadAssertions(ec, nil)
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
	slog.Info("Marketplace acquisition locks bootstrapped", "count", len(desired))
	return len(desired), nil
}

func missingMarketplaceLockLayers(ctx context.Context, repoRoot, localDir string, selected map[lockfile.Scope]bool) ([]marketplaceLockBootstrap, map[string][]byte, error) {
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		return nil, nil, err
	}
	paths := []struct {
		source config.Source
		path   string
	}{
		{config.UserPersonal, userPath},
		{config.RepoCommitted, config.ProjectConfigPath(repoRoot)},
		{config.RepoPersonal, config.LocalConfigPath(localDir)},
	}
	var layers []marketplaceLockBootstrap
	overlays := map[string][]byte{}
	for _, candidate := range paths {
		if (candidate.source == config.RepoCommitted && config.IsHomeDir(repoRoot)) || (candidate.source == config.RepoPersonal && config.IsHomeDir(localDir)) {
			continue
		}
		scope := lockfile.Scope(candidate.source.LockScope())
		if selected != nil && !selected[scope] {
			continue
		}
		v4, err := config.LoadV4ConfigIfPresent(candidate.path, candidate.source)
		if err != nil {
			return nil, nil, err
		}
		if v4 == nil || !marketplacelock.HasMarketplaceSource(v4) {
			continue
		}
		paths, err := config.ResolveConfigLockPaths(candidate.path)
		if err != nil {
			return nil, nil, err
		}
		lockPath, err := rejectGeneratedLockSymlink(paths.CanonicalLock)
		if err != nil {
			return nil, nil, err
		}
		current, generation, exists, err := loadLayerLock(lockPath, scope)
		if err != nil {
			return nil, nil, err
		}
		if exists && current.APIVersion == lockfile.APIVersionV2 {
			continue
		}
		freshGeneration, mode, err := dependencytxn.ReadGenerationAndMode(lockPath)
		if err != nil {
			return nil, nil, err
		}
		if freshGeneration != generation {
			return nil, nil, fmt.Errorf("%w: Marketplace lock changed while loading %s", dependencytxn.ErrGenerationConflict, lockPath)
		}
		layers = append(layers, marketplaceLockBootstrap{Layer: marketplacelock.Layer{Source: candidate.source, LockPath: lockPath, V4: v4}, current: current, expected: generation, mode: mode})
	}
	if len(layers) == 0 {
		return layers, overlays, nil
	}
	var instances []config.MarketplaceEntry
	if ec := config.LoadEffective(ctx, config.LoadEffectiveOptions{RepoRoot: repoRoot, LocalConfigDir: localDir}); ec.Merged != nil {
		instances = ec.Merged.Marketplaces
	}
	for _, layer := range layers {
		doc, err := marketplacelock.Provisional(layer.V4, lockfile.Scope(layer.Source.LockScope()), instances...)
		if err != nil {
			return nil, nil, err
		}
		data, err := lockfile.Encode(doc)
		if err != nil {
			return nil, nil, err
		}
		overlays[layer.LockPath] = data
	}
	return layers, overlays, nil
}
