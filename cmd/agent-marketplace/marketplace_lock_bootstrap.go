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
	layers, overlays, err := missingMarketplaceLockLayers(repoRoot, localDir, selected)
	if err != nil || len(layers) == 0 {
		return 0, err
	}
	ec := pipeline.LoadEffective(ctx, pipeline.ConfigLoadOnline, config.LoadEffectiveOptions{RepoRoot: repoRoot, LocalConfigDir: localDir, FileOverlays: overlays}, policy)
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
		data, err := encodeWithGitRegistries(doc, layer.registries)
		if err != nil {
			return 0, err
		}
		if err := os.MkdirAll(filepath.Dir(layer.LockPath), 0o755); err != nil {
			return 0, fmt.Errorf("create Marketplace lock directory: %w", err)
		}
		mode := layer.expectedMode
		if !layer.expected.Exists {
			mode = 0o644
		}
		desired = append(desired, dependencytxn.DesiredFile{Path: layer.LockPath, Expected: layer.expected, ExpectedMode: layer.expectedMode, Bytes: data, Mode: mode})
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
	slog.Info("missing Marketplace locks created", "count", len(desired))
	return len(desired), nil
}

// marketplaceLockBootstrap is a writable layer whose lock is absent or has no
// Marketplace records. An existing lock keeps its Git registries and is
// replaced only if its generation is unchanged at commit.
type marketplaceLockBootstrap struct {
	marketplacelock.Layer
	registries   []lockfile.GitRegistry
	expected     dependencytxn.Generation
	expectedMode os.FileMode
}

func encodeWithGitRegistries(doc lockfile.Document, registries []lockfile.GitRegistry) ([]byte, error) {
	doc.Registries = registries
	normalized, err := lockfile.Normalize(doc)
	if err != nil {
		return nil, err
	}
	return lockfile.Encode(normalized)
}

func missingMarketplaceLockLayers(repoRoot, localDir string, selected map[lockfile.Scope]bool) ([]marketplaceLockBootstrap, map[string][]byte, error) {
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
		current, expected, exists, err := loadLayerLock(lockPath, scope)
		if err != nil {
			return nil, nil, err
		}
		if exists && len(current.Marketplaces) > 0 {
			continue
		}
		var registries []lockfile.GitRegistry
		var expectedMode os.FileMode
		if exists {
			registries = current.Registries
			if _, expectedMode, err = dependencytxn.ReadGenerationAndMode(lockPath); err != nil {
				return nil, nil, err
			}
		}
		doc, err := marketplacelock.Provisional(v4, scope)
		if err != nil {
			return nil, nil, err
		}
		data, err := encodeWithGitRegistries(doc, registries)
		if err != nil {
			return nil, nil, err
		}
		overlays[lockPath] = data
		layers = append(layers, marketplaceLockBootstrap{
			Layer:        marketplacelock.Layer{Source: candidate.source, LockPath: lockPath, V4: v4},
			registries:   registries,
			expected:     expected,
			expectedMode: expectedMode,
		})
	}
	return layers, overlays, nil
}
