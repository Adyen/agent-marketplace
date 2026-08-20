package main

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func newUpdateCmd() *cobra.Command {
	var bump bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Refresh pinned registries and sync enabled skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd.Context(), cmd.OutOrStdout(), bump)
		},
	}

	cmd.Flags().BoolVar(&bump, "bump", false, "Bump repo registry refs to newer matching tags")

	return cmd
}

func runUpdate(ctx context.Context, out io.Writer, bump bool) error {
	repoRoot := config.FindRepoRoot()
	result, err := pipeline.Update(ctx, pipeline.UpdateOptions{
		RepoRoot: repoRoot,
		Bump:     bump,
	})
	if err != nil {
		return err
	}

	if bump {
		if _, err = fmt.Fprintf(out, "Updated %d registries, bumped %d refs\n", result.UpdatedRegistries, len(result.Bumps)); err != nil {
			return err
		}
	} else if _, err = fmt.Fprintf(out, "Updated %d registries\n", result.UpdatedRegistries); err != nil {
		return err
	}

	// A refresh that fell back to a stale cache is a partial outcome: the run
	// succeeded, but the materialized content may be older than the
	// configured ref, so the fallback must be printed rather than left in the
	// log while the summary claims a clean refresh.
	if err := writeGroupedWarnings(out, result.StaleCaches, false); err != nil {
		return err
	}
	return nil
}
