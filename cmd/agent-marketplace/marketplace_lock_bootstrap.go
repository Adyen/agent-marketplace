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
	ec := pipeline.LoadEffectiveOnline(ctx, config.LoadEffectiveOptions{RepoRoot: repoRoot, LocalConfigDir: localDir, FileOverlays: overlays}, policy)
	if len(ec.Errors) > 0 {
		return 0, errors.Join(ec.Errors...)
	}
	resolver := marketplace.NewResolver()
	resolver.AllowedRegistries = ec.Allowed
	desired := make([]dependencytxn.DesiredFile, 0, len(layers))
	for _, layer := range layers {
		doc, err := marketplacelock.Resolve(ctx, resolver, layer, ec.TargetHarnesses, policy)
		if err != nil {
			return 0, fmt.Errorf("create Marketplace lock %q: %w", layer.LockPath, err)
		}
		data, err := lockfile.Encode(doc)
		if err != nil {
			return 0, err
		}
		if err := os.MkdirAll(filepath.Dir(layer.LockPath), 0o755); err != nil {
			return 0, fmt.Errorf("create Marketplace lock directory: %w", err)
		}
		desired = append(desired, dependencytxn.DesiredFile{Path: layer.LockPath, Expected: dependencytxn.Generation{}, Bytes: data, Mode: 0o644})
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

func missingMarketplaceLockLayers(repoRoot, localDir string, selected map[lockfile.Scope]bool) ([]marketplacelock.Layer, map[string][]byte, error) {
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
	var layers []marketplacelock.Layer
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
		if _, err := os.Lstat(lockPath); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
		doc, err := marketplacelock.Provisional(v4, scope)
		if err != nil {
			return nil, nil, err
		}
		data, err := lockfile.Encode(doc)
		if err != nil {
			return nil, nil, err
		}
		overlays[lockPath] = data
		layers = append(layers, marketplacelock.Layer{Source: candidate.source, LockPath: lockPath, V4: v4})
	}
	return layers, overlays, nil
}
