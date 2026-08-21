package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/catalog"
)

// silentSyncOptsForPaths carries the non-default roots used by monorepo
// commands into the shared sync path.
func silentSyncOptsForPaths(repoRoot, materializeRoot, localConfigDir string) []silentSyncOpt {
	var opts []silentSyncOpt
	if materializeRoot != "" && materializeRoot != repoRoot {
		opts = append(opts, withMaterializeRoot(materializeRoot))
	}
	if localConfigDir != "" && localConfigDir != repoRoot {
		opts = append(opts, withLocalConfigDir(localConfigDir))
	}
	return opts
}

type DependencyLockView struct {
	CatalogOption  catalog.LoadOption
	ExactLockLease func(context.Context) (func(), error)
	Warnings       []error
}

type DependencyReconciler func(context.Context, string, string, bool, []StagedConfigEdit) (DependencyLockView, error)

type DependencyCommittedSyncError struct{ Err error }

func (e *DependencyCommittedSyncError) Error() string {
	return "dependency-committed sync failure: " + e.Err.Error()
}
func (e *DependencyCommittedSyncError) Unwrap() error { return e.Err }

func silentSync(ctx context.Context, repoRoot string, opts ...silentSyncOpt) error {
	_, err := silentSyncCollect(ctx, repoRoot, opts...)
	return err
}

// silentSyncOptsForPaths carries the non-default roots used by monorepo
// commands into the shared sync path.
func silentSyncOptsForPaths(repoRoot, materializeRoot, localConfigDir string) []silentSyncOpt {
	var opts []silentSyncOpt
	if materializeRoot != "" && materializeRoot != repoRoot {
		opts = append(opts, withMaterializeRoot(materializeRoot))
	}
	if localConfigDir != "" && localConfigDir != repoRoot {
		opts = append(opts, withLocalConfigDir(localConfigDir))
	}
	return opts
}

func silentSyncCollect(ctx context.Context, repoRoot string, opts ...silentSyncOpt) ([]error, error) {
	var o silentSyncOptions
	for _, opt := range opts {
		opt(&o)
	}
	var dependencyWarnings []error
	if o.DependencyReconciler != nil {
		if o.ConfigStaging != nil {
			var err error
			o.StagedConfigEdits, err = o.ConfigStaging.Edits()
			if err != nil {
				return nil, fmt.Errorf("collect staged config edits: %w", err)
			}
		}
		view, err := o.DependencyReconciler(ctx, repoRoot, o.LocalConfigDir, o.IgnoreBlockedRegistries, o.StagedConfigEdits)
		if err != nil {
			return nil, fmt.Errorf("reconcile dependency locks: %w", err)
		}
		if view.CatalogOption != nil {
			o.CatalogOption = append(o.CatalogOption, view.CatalogOption)
		}
		o.ExactLockLease = view.ExactLockLease
		dependencyWarnings = view.Warnings
		o.DependencyCommitted = true
	}
	resolveOpts := ResolveOptions{RepoRoot: repoRoot, IgnoreBlockedRegistries: o.IgnoreBlockedRegistries, CatalogMode: o.CatalogMode, CatalogOption: o.CatalogOption, ExactLockLease: o.ExactLockLease}
	if o.MaterializeRoot != "" && o.MaterializeRoot != repoRoot {
		resolveOpts.MaterializeRoot = o.MaterializeRoot
	}
	if o.LocalConfigDir != "" && o.LocalConfigDir != repoRoot {
		resolveOpts.LocalConfigDir = o.LocalConfigDir
	}
	syncResult := Sync(ctx, resolveOpts)
	err := errors.Join(syncResult.Errors...)
	if err != nil && o.DependencyCommitted {
		err = &DependencyCommittedSyncError{Err: err}
	}
	warnings := append(dependencyWarnings, syncResult.Warnings...)
	return warnings, err
}

type silentSyncOptions struct {
	MaterializeRoot         string
	LocalConfigDir          string
	IgnoreBlockedRegistries bool
	CatalogMode             catalog.LoadMode
	CatalogOption           []catalog.LoadOption
	ExactLockLease          func(context.Context) (func(), error)
	DependencyReconciler    DependencyReconciler
	StagedConfigEdits       []StagedConfigEdit
	ConfigStaging           *configStagingManager
	DependencyCommitted     bool
}

type silentSyncOpt func(*silentSyncOptions)

// withMaterializeRoot sets a different root for materialization (where
// harness files are written), while keeping RepoRoot for config loading
// and catalog path resolution of repo-layer entries.
func withMaterializeRoot(dir string) silentSyncOpt {
	return func(o *silentSyncOptions) { o.MaterializeRoot = dir }
}

// withLocalConfigDir sets the base directory for resolving relative paths
// in local-config-layer registry entries.
func withLocalConfigDir(dir string) silentSyncOpt {
	return func(o *silentSyncOptions) { o.LocalConfigDir = dir }
}

func withDependencyReconciler(reconciler DependencyReconciler) silentSyncOpt {
	return func(o *silentSyncOptions) { o.DependencyReconciler = reconciler }
}

func withConfigStaging(staging *configStagingManager) silentSyncOpt {
	return func(o *silentSyncOptions) { o.ConfigStaging = staging }
}

// WithIgnoreBlockedRegistries causes the sync to treat registry-allowlist
// violations as non-fatal. Used during remove so a previously-allowed
// registry can be de-synced after the policy is tightened.
func WithIgnoreBlockedRegistries() silentSyncOpt {
	return func(o *silentSyncOptions) { o.IgnoreBlockedRegistries = true }
}
