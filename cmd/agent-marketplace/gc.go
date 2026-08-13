package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/cache"
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

	result, gcErr := cache.GarbageCollect(dryRun, mergedCtx.MergedCfg)

	action := "Removed"
	if dryRun {
		action = "Would remove"
	}

	for _, removed := range result.Removed {
		if _, err := fmt.Fprintf(out, "%s %s\n", action, removed); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(out, "%s %d cache entries\n", action, len(result.Removed)); err != nil {
		return err
	}

	return gcErr
}
