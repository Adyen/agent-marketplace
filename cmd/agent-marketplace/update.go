package main

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
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

	return writeUpdateOutput(out, result, bump)
}

func writeUpdateOutput(out io.Writer, result *pipeline.UpdateResult, bump bool) error {
	if bump {
		if _, err := fmt.Fprintf(out, "Updated %d registries, bumped %d refs\n", result.UpdatedRegistries, len(result.Bumps)); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(out, "Updated %d registries\n", result.UpdatedRegistries); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Marketplace pins: updated %d, skipped %d\n", result.UpdatedMarketplacePins, result.SkippedMarketplacePins); err != nil {
		return err
	}
	for _, pin := range result.MarketplacePins {
		identity := fmt.Sprintf("%s/%s/%s", marketplace.QuoteTerminal(pin.OldPin.SourceID), marketplace.QuoteTerminal(pin.OldPin.Kind), marketplace.QuoteTerminal(pin.OldPin.Name))
		if pin.Updated {
			if _, err := fmt.Fprintf(out, "Marketplace %s %s: updated %s -> %s\n", marketplace.QuoteTerminal(pin.Entry.URL), identity, marketplace.QuoteTerminal(pin.OldPin.Digest), marketplace.QuoteTerminal(pin.NewPin.Digest)); err != nil {
				return err
			}
			continue
		}
		required := ""
		if pin.Required {
			required = " (required)"
		}
		if _, err := fmt.Fprintf(out, "Marketplace %s %s: skipped%s (%s)\n", marketplace.QuoteTerminal(pin.Entry.URL), identity, required, marketplace.QuoteTerminal(pin.Reason)); err != nil {
			return err
		}
	}
	return nil
}
