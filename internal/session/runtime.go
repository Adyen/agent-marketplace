package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

var (
	// PrepareTUILocks prepares the exact locks an interactive dependency
	// lifecycle needs. It is a package-level seam so tests can substitute a
	// no-op; production always calls lifecycle.PrepareRuntimeLocks.
	PrepareTUILocks = func(ctx context.Context, repoRoot, localDir string, mode lifecycle.LockMode) (lifecycle.ExactLockLeaseFunc, error) {
		_, _, lease, err := lifecycle.PrepareRuntimeLocks(cache.WithFreshExactGitVerificationScope(ctx), repoRoot, localDir, mode)
		return lease, err
	}
	// RunTUIExactSync runs the authoritative sync for the interactive
	// lifecycle. It is a package-level seam so tests can substitute fixtures.
	RunTUIExactSync = pipeline.Sync
)

// RuntimeDependencyLifecycle runs the interactive dependency lifecycle:
// prepare exact locks (upgrading when requested), sync read-only against the
// lease, persist refreshed Marketplace lock evidence, and surface lock
// warnings ahead of sync warnings.
func RuntimeDependencyLifecycle(ctx context.Context, workspace pipeline.Workspace, upgrade bool) (pipeline.SyncResult, error) {
	ctx = pipeline.WithTransientOrganizationMarketplaceLockCache(ctx)
	ctx = cache.WithFreshExactGitVerificationScope(ctx)
	ws, err := pipeline.NewWorkspace(workspace.RepoRoot(), workspace.LocalConfigDir())
	if err != nil {
		return pipeline.SyncResult{}, err
	}
	var lockWarnings []error
	mode := lifecycle.LockMode{Upgrade: upgrade, WarningSink: &lockWarnings}
	if upgrade {
		selected, _, err := lifecycle.ParseLockSelection("all", false)
		if err != nil {
			return pipeline.SyncResult{}, err
		}
		mode.Selects = selected
		mode.SelectionGuard = true
	}
	exactLease, err := PrepareTUILocks(ctx, ws.RepoRoot(), ws.LocalConfigDir(), mode)
	if err != nil {
		return pipeline.SyncResult{}, err
	}
	options := pipeline.ResolveOptions{Workspace: ws, ReadOnlyExactLocks: true, ExactLockLease: exactLease, MarketplaceReportArchiveEvidenceDrift: true}
	result := RunTUIExactSync(ctx, options)
	errs := result.Errors
	var persistedRefreshes map[lifecycle.ArchiveRefreshKey]bool
	if len(errs) == 0 {
		var refreshErr error
		persistedRefreshes, refreshErr = lifecycle.RefreshDriftedMarketplaceLocks(ctx, ws.RepoRoot(), ws.LocalConfigDir(), result.MergedCfg, result.Warnings)
		if refreshErr != nil {
			errs = append(errs, fmt.Errorf("refresh Marketplace lock evidence: %w", refreshErr))
		}
	}
	result.Warnings = lifecycle.StripUnpersistedRefreshWarnings(result.Warnings, persistedRefreshes)
	result.Warnings = append(lockWarnings, result.Warnings...)
	return result, errors.Join(errs...)
}

// RuntimeConfigMutation runs a guarded config mutation through runtime lock
// reconciliation against a fresh workspace.
func RuntimeConfigMutation(ctx context.Context, workspace pipeline.Workspace, mutation func(pipeline.ConfigPathMapper) error) error {
	ws, err := pipeline.NewWorkspace(workspace.RepoRoot(), workspace.LocalConfigDir())
	if err != nil {
		return err
	}
	return pipeline.MutateConfigWithDependencies(ctx, ws.RepoRoot(), ws.LocalConfigDir(), mutation, lifecycle.ReconcileCommandDependencies)
}

// RuntimeLifecycle exposes the production interactive dependency lifecycle.
func RuntimeLifecycle() DependencyLifecycle {
	return DependencyLifecycle{
		Sync: func(ctx context.Context, workspace pipeline.Workspace) (pipeline.SyncResult, error) {
			return RuntimeDependencyLifecycle(ctx, workspace, false)
		},
		Update: func(ctx context.Context, workspace pipeline.Workspace) (pipeline.SyncResult, error) {
			return RuntimeDependencyLifecycle(ctx, workspace, true)
		},
		Mutate:           RuntimeConfigMutation,
		InstallReconcile: lifecycle.ReconcileCommandDependencies,
		InstallPrepare:   lifecycle.PrepareInstallDependencies,
	}
}
