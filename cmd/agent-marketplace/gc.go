package main

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

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

func runGC(ctx context.Context, out io.Writer, dryRun bool) error {
	report, err := pipeline.GarbageCollect(ctx, dryRun)

	action := "Removed"
	if dryRun {
		action = "Would remove"
	}

	print := func(removed []string, summary string) error {
		for _, path := range removed {
			if _, writeErr := fmt.Fprintf(out, "%s %s\n", action, path); writeErr != nil {
				return writeErr
			}
		}
		_, writeErr := fmt.Fprintf(out, "%s %d %s\n", action, len(removed), summary)
		return writeErr
	}
	if printErr := print(report.Registry.Removed, "cache entries"); printErr != nil {
		return printErr
	}
	if printErr := print(report.Marketplace.Removed, "marketplace artifact directories"); printErr != nil {
		return printErr
	}

	return err
}
