package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

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

func silentSync(ctx context.Context, ws Workspace, opts ...silentSyncOpt) error {
	_, err := silentSyncCollect(ctx, ws, opts...)
	return err
}

func silentSyncCollect(ctx context.Context, ws Workspace, opts ...silentSyncOpt) ([]error, error) {
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
		// The reconciler keeps the pre-Workspace contract: an empty local dir
		// means "no nested local root", so only a genuinely nested root is
		// passed through.
		localDir := ""
		if ws.LocalConfigDir() != ws.RepoRoot() {
			localDir = ws.LocalConfigDir()
		}
		view, err := o.DependencyReconciler(ctx, ws.RepoRoot(), localDir, o.IgnoreBlockedRegistries, o.StagedConfigEdits)
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
	resolveOpts := ResolveOptions{Workspace: ws, IgnoreBlockedRegistries: o.IgnoreBlockedRegistries, CatalogMode: o.CatalogMode, CatalogOption: o.CatalogOption, ExactLockLease: o.ExactLockLease, MarketplaceMode: o.MarketplaceMode, marketplaceInstall: o.marketplaceInstall, marketplaceInstallSource: o.marketplaceInstallSource}
	syncResult := Sync(ctx, resolveOpts)
	if o.resolved != nil {
		*o.resolved = syncResult.Resolved
	}
	err := errors.Join(syncResult.Errors...)
	if err != nil && o.DependencyCommitted {
		err = &DependencyCommittedSyncError{Err: err}
	}
	warnings := append(dependencyWarnings, syncResult.Warnings...)
	return warnings, err
}

type silentSyncOptions struct {
	IgnoreBlockedRegistries  bool
	CatalogMode              catalog.LoadMode
	CatalogOption            []catalog.LoadOption
	ExactLockLease           func(context.Context) (func(), error)
	DependencyReconciler     DependencyReconciler
	StagedConfigEdits        []StagedConfigEdit
	ConfigStaging            *configStagingManager
	DependencyCommitted      bool
	MarketplaceMode          *resolver.MarketplaceResolveMode
	marketplaceInstall       *marketplace.PreparedInstall
	marketplaceInstallSource config.Source
	resolved                 *[]resolver.ResolvedArtifact
}

type silentSyncOpt func(*silentSyncOptions)

// withResolvedCapture stores the sync's final resolution in dst.
func withResolvedCapture(dst *[]resolver.ResolvedArtifact) silentSyncOpt {
	return func(o *silentSyncOptions) { o.resolved = dst }
}

func withMarketplaceInstall(plan *marketplace.PreparedInstall, source config.Source) silentSyncOpt {
	return func(o *silentSyncOptions) {
		o.marketplaceInstall = plan
		o.marketplaceInstallSource = source
	}
}

func withDependencyReconciler(reconciler DependencyReconciler) silentSyncOpt {
	return func(o *silentSyncOptions) { o.DependencyReconciler = reconciler }
}

func withConfigStaging(staging *configStagingManager) silentSyncOpt {
	return func(o *silentSyncOptions) { o.ConfigStaging = staging }
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
