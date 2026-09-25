package main

import (
	"context"
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
	intent := config.ReadIntent(ctx, config.LoadEffectiveOptions{RepoRoot: repoRoot, LocalConfigDir: localDir})
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
		if exists && current.APIVersion != lockfile.APIVersionV1 {
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
