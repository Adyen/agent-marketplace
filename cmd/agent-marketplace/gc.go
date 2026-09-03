package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
)

func newGCCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Remove orphaned registry cache directories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGC(cmd.Context(), cmd.OutOrStdout(), dryRun)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show cache entries that would be removed without deleting")

	return cmd
}

func runGC(ctx context.Context, out io.Writer, dryRun bool) (resultErr error) {
	started := time.Now()
	var registryResult cache.GCResult
	var marketplaceResult marketplace.GCResult
	slog.Info("cache garbage collection started", "dry_run", dryRun)
	defer func() {
		attrs := []any{"dry_run", dryRun, "registry_removed", len(registryResult.Removed), "marketplace_removed", len(marketplaceResult.Removed), "duration_ms", time.Since(started).Milliseconds()}
		if resultErr != nil {
			attrs = append(attrs, "error", resultErr)
			slog.Error("cache garbage collection failed", attrs...)
			return
		}
		slog.Info("cache garbage collection completed", attrs...)
	}()

	if err := recoverDependencyTransactions(ctx); err != nil {
		return err
	}
	effective := config.LoadEffective(ctx, config.LoadEffectiveOptions{})
	if len(effective.Errors) > 0 {
		return errors.Join(effective.Errors...)
	}
	if len(effective.PresentLayers()) == 0 {
		return errors.New("gc requires at least one effective configuration layer")
	}
	locks, err := loadEffectiveLayerLocks(effective)
	if err != nil {
		return err
	}

	var registryErr, marketplaceErr error
	marketplaceCache := marketplace.DefaultCache()

	if dryRun {
		registryResult, registryErr = cache.GarbageCollectWithLocks(true, locks, effective.Merged)
		marketplaceResult, marketplaceErr = marketplaceCache.GarbageCollectWithLocks(true, locks)
	} else {
		if _, err := cache.GarbageCollectWithLocks(true, locks, effective.Merged); err != nil {
			registryErr = fmt.Errorf("cache garbage collection preflight: %w", err)
		} else if _, err := marketplaceCache.GarbageCollectWithLocks(true, locks); err != nil {
			marketplaceErr = fmt.Errorf("marketplace cache garbage collection preflight: %w", err)
		} else {
			marketplaceResult, marketplaceErr = marketplaceCache.GarbageCollectWithLocks(false, locks)
			if marketplaceErr == nil {
				registryResult, registryErr = cache.GarbageCollectWithLocks(false, locks, effective.Merged)
			}
		}
	}

	action := "Removed"
	if dryRun {
		action = "Would remove"
	}

	for _, removed := range registryResult.Removed {
		if _, err := fmt.Fprintf(out, "%s %s\n", action, removed); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "%s %d cache entries\n", action, len(registryResult.Removed)); err != nil {
		return err
	}

	for _, removed := range marketplaceResult.Removed {
		if _, err := fmt.Fprintf(out, "%s %s\n", action, removed); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "%s %d marketplace cache directories\n", action, len(marketplaceResult.Removed)); err != nil {
		return err
	}

	return errors.Join(registryErr, marketplaceErr)
}

func loadEffectiveLayerLocks(effective config.EffectiveConfig) ([]lockfile.Document, error) {
	var documents []lockfile.Document
	for _, layer := range effective.PresentLayers() {
		lockPath := layer.Source.LockPath(layer.Path)
		scope, err := lockfile.ParseScope(layer.Source.LockScope())
		if err != nil {
			return nil, err
		}
		// The organization config is an externally managed trust unit. A
		// consumer may use its requirements without an adjacent lock, but a
		// lock that is present must pass the same trust checks as sync before
		// it can influence cache reachability.
		if scope == lockfile.ScopeOrganization {
			if err := config.ValidateOrganizationTrustUnit(layer.Path, true, true); err != nil {
				return nil, fmt.Errorf("validate organization config and lock %q: %w", layer.Path, err)
			}
		}
		required := layerRequiresLock(layer) && scope != lockfile.ScopeOrganization
		raw, exists, err := readTrustedLayerLock(lockPath)
		if !exists && !required {
			continue
		}
		if !exists {
			return nil, fmt.Errorf("required %s layer lock %q is missing: %w", layer.Source, lockPath, os.ErrNotExist)
		}
		if err != nil {
			return nil, fmt.Errorf("read %s layer lock %q: %w", layer.Source, lockPath, err)
		}
		doc, err := lockfile.Decode(raw)
		if err != nil {
			return nil, fmt.Errorf("decode %s layer lock %q: %w", layer.Source, lockPath, err)
		}
		if err := lockfile.ValidateScope(doc, scope); err != nil {
			return nil, fmt.Errorf("validate %s layer lock %q: %w", layer.Source, lockPath, err)
		}
		if scope == lockfile.ScopeOrganization && !unselectedLayerIsFresh(scope, remoteEntries(layerRegistries(layer)), layerMarketplaces(layer), doc) {
			return nil, &lockLifecycleError{Code: "lock.stale", Scope: scope, Path: lockPath, Err: errors.New("lock does not match configuration")}
		}
		documents = append(documents, doc)
	}
	return documents, nil
}

func readTrustedLayerLock(path string) ([]byte, bool, error) {
	raw, err := lockfile.ReadBytes(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return raw, true, err
}

func layerRequiresLock(layer config.LayerResult) bool {
	if layer.Config != nil {
		for _, entry := range layer.Config.Registries {
			if !entry.IsLocal() {
				return true
			}
		}
		return len(layer.Config.Marketplaces) > 0
	}
	if layer.Local != nil {
		for _, entry := range layer.Local.Registries {
			if !entry.IsLocal() {
				return true
			}
		}
		return len(layer.Local.Marketplaces) > 0
	}
	return false
}
