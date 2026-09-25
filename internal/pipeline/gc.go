package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
)

// GCReport carries the per-collector GC results for command output.
type GCReport struct {
	Registry    cache.GCResult
	Marketplace marketplace.GCResult
}

// RecoverDependencyTransactions recovers interrupted dependency lock
// transactions under the shared exact-cache namespace lease, in the same order
// as normal publication, so exclusive GC cannot inventory a half-recovered
// transaction and delete its newly reachable snapshots.
func RecoverDependencyTransactions(ctx context.Context) error {
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(stateDir); errors.Is(err, os.ErrNotExist) {
		slog.Debug("lock transaction recovery skipped", "reason", "state absent")
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect dependency transaction state: %w", err)
	}

	releaseExactNamespace, err := cache.AcquireExactGitNamespaceLeaseWithContext(ctx)
	if err != nil {
		return fmt.Errorf("acquire exact cache recovery lease: %w", err)
	}
	defer releaseExactNamespace()

	started := time.Now()
	if err := dependencytxn.Recover(ctx, stateDir); err != nil {
		slog.Error("lock transaction recovery failed", "duration_ms", time.Since(started).Milliseconds(), "error", err)
		return fmt.Errorf("recover dependency lock transaction: %w", err)
	}
	slog.Debug("lock transaction recovery completed", "duration_ms", time.Since(started).Milliseconds())
	return nil
}

// GarbageCollect collects unreachable registry cache directories and
// Marketplace artifacts from local reachability alone under the exclusive
// exact-cache namespace lease, held across both collectors' preflights and
// collections; both preflights complete before either collection mutates
// data, and no network acquisition or organization config/lock writes occur.
func GarbageCollect(ctx context.Context, dryRun bool) (report GCReport, resultErr error) {
	started := time.Now()
	slog.Info("cache garbage collection started", "dry_run", dryRun)
	defer func() {
		attrs := []any{"dry_run", dryRun, "registry_removed", len(report.Registry.Removed), "marketplace_removed", len(report.Marketplace.Removed), "duration_ms", time.Since(started).Milliseconds()}
		if resultErr != nil {
			attrs = append(attrs, "error", resultErr)
			slog.Error("cache garbage collection failed", attrs...)
			return
		}
		slog.Info("cache garbage collection completed", attrs...)
	}()

	if err := RecoverDependencyTransactions(ctx); err != nil {
		return report, err
	}
	err := cache.WithExactGitNamespaceExclusiveLease(ctx, func(leaseCtx context.Context) error {
		var runErr error
		report, runErr = garbageCollectUnderLease(leaseCtx, dryRun, report)
		return runErr
	})
	return report, err
}

// garbageCollectUnderLease derives both collectors' reachability from one
// intent/lock snapshot and runs the preflight/collect phases while the caller
// holds the exclusive exact-git namespace lease.
func garbageCollectUnderLease(ctx context.Context, dryRun bool, report GCReport) (GCReport, error) {
	intent := config.ReadIntent(ctx, config.LoadEffectiveOptions{
		RepoRoot:       config.FindRepoRoot(),
		LocalConfigDir: config.FindLocalConfigDir(),
	})
	if len(intent.Errors) > 0 {
		return report, errors.Join(intent.Errors...)
	}
	present := make(map[config.Source]config.LayerIntent)
	for _, source := range deployedLockOrder {
		if layer := intent.Layers[source]; layer.Present() && layer.Err() == nil {
			present[source] = layer
		}
	}
	if len(present) == 0 {
		return report, errors.New("gc requires at least one effective configuration layer")
	}
	evidence, err := ReadDeployedLocks(ctx, intent)
	if err != nil {
		return report, err
	}

	reachability := cache.GCReachability{}
	roots := marketplace.GCRoots{}
	for _, source := range deployedLockOrder {
		layer, ok := present[source]
		if !ok {
			continue
		}
		for _, decl := range layer.Sources() {
			if decl.Kind == config.V4SourceGit {
				reachability.Sources = append(reachability.Sources, decl.URL)
			}
		}
		roots.ExactPins = append(roots.ExactPins, layer.LegacyMarketplaces()...)
		if layer.NeedsMarketplaceLock() && evidence.Locks[source] == nil {
			roots.UnlockedEndpoints = append(roots.UnlockedEndpoints, layer.MarketplaceEndpoints()...)
		}
	}
	for _, doc := range evidence.Locks {
		if doc != nil {
			reachability.Locks = append(reachability.Locks, *doc)
			roots.LockedResources = append(roots.LockedResources, doc.Marketplaces...)
		}
	}
	readReachability := func(context.Context) (cache.GCReachability, error) { return reachability, nil }
	var registryErr, marketplaceErr error
	if dryRun {
		report.Registry, registryErr = cache.GarbageCollect(ctx, true, readReachability)
		report.Marketplace, marketplaceErr = marketplace.DefaultCache().GarbageCollect(true, roots)
		return report, errors.Join(registryErr, marketplaceErr)
	}
	if _, registryErr = cache.GarbageCollect(ctx, true, readReachability); registryErr != nil {
		registryErr = fmt.Errorf("cache garbage collection preflight: %w", registryErr)
	}
	if _, marketplaceErr = marketplace.DefaultCache().GarbageCollect(true, roots); marketplaceErr != nil {
		marketplaceErr = fmt.Errorf("marketplace cache garbage collection preflight: %w", marketplaceErr)
	}
	if registryErr == nil && marketplaceErr == nil {
		if report.Marketplace, marketplaceErr = marketplace.DefaultCache().GarbageCollect(false, roots); marketplaceErr == nil {
			report.Registry, registryErr = cache.GarbageCollect(ctx, false, readReachability)
		}
	}
	return report, errors.Join(registryErr, marketplaceErr)
}
