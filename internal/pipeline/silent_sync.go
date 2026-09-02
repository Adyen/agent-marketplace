package pipeline

import (
	"context"
	"errors"
)

func silentSync(ctx context.Context, repoRoot string, opts ...silentSyncOpt) error {
	_, err := silentSyncCollect(ctx, repoRoot, opts...)
	return err
}

func silentSyncCollect(ctx context.Context, repoRoot string, opts ...silentSyncOpt) ([]error, error) {
	var o silentSyncOptions
	for _, opt := range opts {
		opt(&o)
	}
	resolveOpts := ResolveOptions{RepoRoot: repoRoot, IgnoreBlockedRegistries: o.IgnoreBlockedRegistries}
	if o.MaterializeRoot != "" && o.MaterializeRoot != repoRoot {
		resolveOpts.MaterializeRoot = o.MaterializeRoot
	}
	if o.LocalConfigDir != "" && o.LocalConfigDir != repoRoot {
		resolveOpts.LocalConfigDir = o.LocalConfigDir
	}
	resolveOpts.CatalogAlreadyRefreshed = o.CatalogAlreadyRefreshed
	syncResult := Sync(ctx, resolveOpts)
	return syncResult.Warnings, errors.Join(syncResult.Errors...)
}

type silentSyncOptions struct {
	MaterializeRoot         string
	LocalConfigDir          string
	IgnoreBlockedRegistries bool
	CatalogAlreadyRefreshed bool
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

// WithIgnoreBlockedRegistries causes the sync to treat registry-allowlist
// violations as non-fatal. Used during remove so a previously-allowed
// registry can be de-synced after the policy is tightened.
func WithIgnoreBlockedRegistries() silentSyncOpt {
	return func(o *silentSyncOptions) { o.IgnoreBlockedRegistries = true }
}

func withAlreadyRefreshedCatalog() silentSyncOpt {
	return func(o *silentSyncOptions) { o.CatalogAlreadyRefreshed = true }
}
