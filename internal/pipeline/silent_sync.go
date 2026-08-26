package pipeline

import (
	"context"
	"errors"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

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
	resolveOpts := ResolveOptions{RepoRoot: repoRoot, IgnoreBlockedRegistries: o.IgnoreBlockedRegistries, CatalogMode: o.CatalogMode, CatalogOption: o.CatalogOption, MarketplaceMode: o.MarketplaceMode}
	if o.MaterializeRoot != "" && o.MaterializeRoot != repoRoot {
		resolveOpts.MaterializeRoot = o.MaterializeRoot
	}
	if o.LocalConfigDir != "" && o.LocalConfigDir != repoRoot {
		resolveOpts.LocalConfigDir = o.LocalConfigDir
	}
	syncResult := Sync(ctx, resolveOpts)
	return syncResult.Warnings, errors.Join(syncResult.Errors...)
}

type silentSyncOptions struct {
	MaterializeRoot         string
	LocalConfigDir          string
	IgnoreBlockedRegistries bool
	CatalogMode             catalog.LoadMode
	CatalogOption           []catalog.LoadOption
	MarketplaceMode         *resolver.MarketplaceResolveMode
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

func withCatalogMode(mode catalog.LoadMode) silentSyncOpt {
	return func(o *silentSyncOptions) { o.CatalogMode = mode }
}

func withCatalogOptions(opts []catalog.LoadOption) silentSyncOpt {
	return func(o *silentSyncOptions) { o.CatalogOption = opts }
}

func withMarketplaceMode(mode resolver.MarketplaceResolveMode) silentSyncOpt {
	return func(o *silentSyncOptions) { o.MarketplaceMode = &mode }
}

// WithIgnoreBlockedRegistries causes the sync to treat registry-allowlist
// violations as non-fatal. Used during remove so a previously-allowed
// registry can be de-synced after the policy is tightened.
func WithIgnoreBlockedRegistries() silentSyncOpt {
	return func(o *silentSyncOptions) { o.IgnoreBlockedRegistries = true }
}
