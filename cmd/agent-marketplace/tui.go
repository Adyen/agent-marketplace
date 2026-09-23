package main

import (
	"context"
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/tui"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive terminal UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot := config.FindRepoRoot()
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			return runProductionTUI(cmd.Context(), repoRoot, homeDir)
		},
	}
}

func runProductionTUI(ctx context.Context, repoRoot, homeDir string) error {
	if err := recoverDependencyTransactions(ctx); err != nil {
		return err
	}
	return tui.Run(ctx, repoRoot, homeDir, tui.WithDependencyLifecycle(tui.DependencyLifecycle{
		Sync: func(ctx context.Context, repoRoot string) error {
			return runTUIDependencyLifecycle(ctx, repoRoot, false)
		},
		Update: func(ctx context.Context, repoRoot string) error {
			return runTUIDependencyLifecycle(ctx, repoRoot, true)
		},
		Mutate: runTUIConfigMutation,
	}))
}

var (
	prepareTUILocks = func(ctx context.Context, repoRoot, localDir string, mode lockMode) (exactLockLeaseFunc, error) {
		_, _, lease, err := prepareRuntimeLocks(ctx, repoRoot, localDir, mode)
		return lease, err
	}
	runTUIExactSync = pipeline.Sync
)

func runTUIConfigMutation(ctx context.Context, repoRoot string, mutation func(pipeline.ConfigPathMapper) error) error {
	return pipeline.MutateConfigWithDependencies(ctx, repoRoot, config.FindLocalConfigDir(), mutation, reconcileCommandDependencies)
}

func runTUIDependencyLifecycle(ctx context.Context, repoRoot string, upgrade bool) error {
	ctx = pipeline.WithTransientOrganizationMarketplaceLockCache(ctx)
	localDir := config.FindLocalConfigDir()
	mode := lockMode{upgrade: upgrade}
	if upgrade {
		selected, _, err := parseLockSelection("all", false)
		if err != nil {
			return err
		}
		mode.selects = selected
	}
	exactLease, err := prepareTUILocks(ctx, repoRoot, localDir, mode)
	if err != nil {
		return err
	}
	options := pipeline.ResolveOptions{RepoRoot: repoRoot, ReadOnlyExactLocks: true, ExactLockLease: exactLease}
	if localDir != "" && localDir != repoRoot {
		options.LocalConfigDir = localDir
		options.MaterializeRoot = localDir
	}
	result := runTUIExactSync(ctx, options)
	return errors.Join(result.Errors...)
}
