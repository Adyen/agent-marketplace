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
	slog.Info("cache garbage collection started", "dry_run", dryRun)
	defer func() {
		attrs := []any{"dry_run", dryRun, "registry_removed", len(registryResult.Removed), "duration_ms", time.Since(started).Milliseconds()}
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
	loadOptions := config.LoadEffectiveOptions{
		RepoRoot:       config.FindRepoRoot(),
		LocalConfigDir: config.FindLocalConfigDir(),
	}
	readReachability := func(readCtx context.Context) (cache.GCReachability, error) {
		effective := config.LoadEffective(readCtx, loadOptions)
		if len(effective.Errors) > 0 {
			return cache.GCReachability{}, errors.Join(effective.Errors...)
		}
		if len(effective.PresentLayers()) == 0 {
			return cache.GCReachability{}, errors.New("gc requires at least one effective configuration layer")
		}
		locks, err := loadEffectiveLayerLocks(readCtx, effective)
		if err != nil {
			return cache.GCReachability{}, err
		}
		var configs []*config.Config
		for _, layer := range effective.PresentLayers() {
			configs = append(configs, &config.Config{Registries: layerRegistries(layer)})
		}
		return cache.GCReachability{Locks: locks, Configs: configs}, nil
	}

	var registryErr error
	if dryRun {
		registryResult, registryErr = cache.GarbageCollectWithReachabilityAndContext(ctx, true, readReachability)
	} else if _, err := cache.GarbageCollectWithReachabilityAndContext(ctx, true, readReachability); err != nil {
		registryErr = fmt.Errorf("cache garbage collection preflight: %w", err)
	} else {
		registryResult, registryErr = cache.GarbageCollectWithReachabilityAndContext(ctx, false, readReachability)
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

	return registryErr
}

func loadEffectiveLayerLocks(ctx context.Context, effective config.EffectiveConfig) ([]lockfile.Document, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var documents []lockfile.Document
	for _, layer := range effective.PresentLayers() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
		if scope == lockfile.ScopeOrganization && !unselectedLayerIsFresh(scope, remoteEntries(layerRegistries(layer)), doc) {
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
		return false
	}
	if layer.Local != nil {
		for _, entry := range layer.Local.Registries {
			if !entry.IsLocal() {
				return true
			}
		}
		return false
	}
	return false
}
