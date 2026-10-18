package main

import (
	"context"
	"errors"
	"fmt"
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
	workspace, err := pipeline.NewWorkspace(repoRoot, config.FindLocalConfigDir())
	if err != nil {
		return err
	}
	if err := pipeline.RecoverDependencyTransactions(ctx); err != nil {
		return err
	}
	options := []tui.Option{tui.WithWorkspace(workspace), tui.WithDependencyLifecycle(tui.DependencyLifecycle{
		Sync: func(ctx context.Context, workspace pipeline.Workspace) ([]error, error) {
			return runTUIDependencyLifecycle(ctx, workspace, false)
		},
		Update: func(ctx context.Context, workspace pipeline.Workspace) ([]error, error) {
			return runTUIDependencyLifecycle(ctx, workspace, true)
		},
		Mutate: runTUIConfigMutation,
	})}
	if checker := newUpgradeChecker(); checker != nil {
		options = append(options, tui.WithUpgradeCheck(checker))
	}
	return tui.Run(ctx, workspace.RepoRoot(), homeDir, options...)
}

var (
	prepareTUILocks = func(ctx context.Context, repoRoot, localDir string, mode lockMode) (exactLockLeaseFunc, error) {
		_, _, lease, err := prepareRuntimeLocks(ctx, repoRoot, localDir, mode)
		return lease, err
	}
	runTUIExactSync = pipeline.Sync
)

func runTUIConfigMutation(ctx context.Context, workspace pipeline.Workspace, mutation func(pipeline.ConfigPathMapper) error) error {
	ws, err := pipeline.NewWorkspace(workspace.RepoRoot(), workspace.LocalConfigDir())
	if err != nil {
		return err
	}
	return pipeline.MutateConfigWithDependencies(ctx, ws.RepoRoot(), ws.LocalConfigDir(), mutation, reconcileCommandDependencies)
}

func runTUIDependencyLifecycle(ctx context.Context, workspace pipeline.Workspace, upgrade bool) ([]error, error) {
	ctx = pipeline.WithTransientOrganizationMarketplaceLockCache(ctx)
	ws, err := pipeline.NewWorkspace(workspace.RepoRoot(), workspace.LocalConfigDir())
	if err != nil {
		return nil, err
	}
	var lockWarnings []error
	mode := lockMode{upgrade: upgrade, warningSink: &lockWarnings}
	if upgrade {
		selected, _, err := parseLockSelection("all", false)
		if err != nil {
			return nil, err
		}
		mode.selects = selected
		mode.selectionGuard = true
	}
	exactLease, err := prepareTUILocks(ctx, ws.RepoRoot(), ws.LocalConfigDir(), mode)
	if err != nil {
		return nil, err
	}
	options := pipeline.ResolveOptions{Workspace: ws, ReadOnlyExactLocks: true, ExactLockLease: exactLease, MarketplaceReportArchiveEvidenceDrift: true}
	result := runTUIExactSync(ctx, options)
	errs := result.Errors
	var persistedRefreshes map[archiveRefreshKey]bool
	if len(errs) == 0 {
		var refreshErr error
		persistedRefreshes, refreshErr = refreshDriftedMarketplaceLocks(ctx, ws.RepoRoot(), ws.LocalConfigDir(), result.MergedCfg, result.Warnings)
		if refreshErr != nil {
			errs = append(errs, fmt.Errorf("refresh Marketplace lock evidence: %w", refreshErr))
		}
	}
	result.Warnings = stripUnpersistedRefreshWarnings(result.Warnings, persistedRefreshes)
	return append(lockWarnings, result.Warnings...), errors.Join(errs...)
}
