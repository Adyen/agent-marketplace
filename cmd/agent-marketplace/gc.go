package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
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
	loadOptions := config.LoadEffectiveOptions{
		RepoRoot:       config.FindRepoRoot(),
		LocalConfigDir: config.FindLocalConfigDir(),
	}
	effective := pipeline.LoadEffective(ctx, pipeline.ConfigLoadOnline, loadOptions, nil)
	if len(effective.Errors) > 0 {
		return errors.Join(effective.Errors...)
	}
	if len(effective.PresentLayers()) == 0 {
		return errors.New("gc requires at least one effective configuration layer")
	}
	marketplaceCache := marketplace.DefaultCache()
	readReachability := func(readCtx context.Context) (cache.GCReachability, error) {
		intent := config.ReadIntent(readCtx, loadOptions)
		if len(intent.Errors) > 0 {
			return cache.GCReachability{}, errors.Join(intent.Errors...)
		}
		evidence, err := pipeline.ReadDeployedLocks(readCtx, intent)
		if err != nil {
			return cache.GCReachability{}, err
		}
		var locks []lockfile.Document
		for _, doc := range evidence.Locks {
			if doc != nil {
				locks = append(locks, *doc)
			}
		}
		var configs []*config.Config
		for _, source := range []config.Source{config.Organization, config.UserPersonal, config.RepoCommitted, config.RepoPersonal} {
			layer := intent.Layers[source]
			if !layer.Present() || layer.Err() != nil {
				continue
			}
			configs = append(configs, &config.Config{Registries: pipeline.GitRegistryEntries(layer.Sources())})
		}
		if len(configs) == 0 {
			return cache.GCReachability{}, errors.New("gc requires at least one effective configuration layer")
		}
		return cache.GCReachability{Locks: locks, Configs: configs}, nil
	}

	var registryErr, marketplaceErr error
	if dryRun {
		registryResult, registryErr = cache.GarbageCollectWithReachabilityAndContext(ctx, true, readReachability)
		marketplaceResult, marketplaceErr = marketplaceCache.GarbageCollect(true, effective.Merged)
	} else {
		_, registryErr = cache.GarbageCollectWithReachabilityAndContext(ctx, true, readReachability)
		if registryErr != nil {
			registryErr = fmt.Errorf("cache garbage collection preflight: %w", registryErr)
		}
		_, marketplaceErr = marketplaceCache.GarbageCollect(true, effective.Merged)
		if marketplaceErr != nil {
			marketplaceErr = fmt.Errorf("marketplace cache garbage collection preflight: %w", marketplaceErr)
		}
		if registryErr == nil && marketplaceErr == nil {
			marketplaceResult, marketplaceErr = marketplaceCache.GarbageCollect(false, effective.Merged)
			if marketplaceErr == nil {
				registryResult, registryErr = cache.GarbageCollectWithReachabilityAndContext(ctx, false, readReachability)
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
	if _, err := fmt.Fprintf(out, "%s %d marketplace artifact directories\n", action, len(marketplaceResult.Removed)); err != nil {
		return err
	}

	return errors.Join(registryErr, marketplaceErr)
}
