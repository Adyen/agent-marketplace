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
		_, err = fmt.Fprintf(out, "Updated %d registries, bumped %d refs\n", result.UpdatedRegistries, len(result.Bumps))
		return err
	}

	_, err = fmt.Fprintf(out, "Updated %d registries\n", result.UpdatedRegistries)
	return err
}
