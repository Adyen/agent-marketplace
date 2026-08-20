package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
)

func newGCCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Remove orphaned registry cache directories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGC(cmd.OutOrStdout(), dryRun)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show cache entries that would be removed without deleting")

	return cmd
}

func runGC(out io.Writer, dryRun bool) error {
	mergedCtx, err := loadMergedConfigContext()
	if err != nil {
		return err
	}
	if mergedCtx.OrgCfg == nil && mergedCtx.GlobalCfg == nil && mergedCtx.RepoCfg == nil && mergedCtx.LocalCfg == nil {
		return errors.New("gc requires at least one effective configuration layer")
	}

	var registryResult cache.GCResult
	var registryErr, marketplaceErr error
	var marketplaceResult marketplace.GCResult
	marketplaceCache := marketplace.DefaultCache()

	if dryRun {
		registryResult, registryErr = cache.GarbageCollect(true, mergedCtx.MergedCfg)
		marketplaceResult, marketplaceErr = marketplaceCache.GarbageCollect(true, mergedCtx.MergedCfg)
	} else {
		if _, err := marketplaceCache.GarbageCollect(true, mergedCtx.MergedCfg); err != nil {
			marketplaceErr = fmt.Errorf("marketplace cache garbage collection preflight: %w", err)
		} else {
			marketplaceResult, marketplaceErr = marketplaceCache.GarbageCollect(false, mergedCtx.MergedCfg)
			if marketplaceErr == nil {
				registryResult, registryErr = cache.GarbageCollect(false, mergedCtx.MergedCfg)
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
