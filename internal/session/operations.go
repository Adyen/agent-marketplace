package session

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/skill"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

// DependencyLifecycleFunc runs a dependency lifecycle step against a
// validated workspace and returns its warnings alongside the outcome.
type DependencyLifecycleFunc func(context.Context, pipeline.Workspace) (pipeline.SyncResult, error)

// DependencyMutationFunc applies a guarded mutation through the provided
// config path mapper.
type DependencyMutationFunc func(context.Context, pipeline.Workspace, func(pipeline.ConfigPathMapper) error) error

// DependencyLifecycle carries the dependency lifecycle hooks a host injects
// so guarded actions run against the host's runtime reconciliation.
type DependencyLifecycle struct {
	Sync   DependencyLifecycleFunc
	Update DependencyLifecycleFunc
	Mutate DependencyMutationFunc
	// InstallReconcile prepares acquisition locks for the sync that follows a
	// Marketplace install, so other sources stay at their locked revisions.
	InstallReconcile pipeline.DependencyReconciler
	InstallPrepare   pipeline.InstallDependencyPreparer
}

// Update stage labels shared by the sync/update verdicts.
const (
	UpdateStageRefresh         = "refresh"
	UpdateStageMaterialization = "materialization"
	UpdateStageReload          = "state reload"
)

// UpdateResult is the outcome of a dependency update run: the reloaded
// snapshot, the stage that failed (or completed), and the verdict.
type UpdateResult struct {
	Generation RequestGeneration
	Snapshot   Snapshot
	Stage      string
	Err        error
	Warnings   []error
	Report     *materialize.Report
}

// RunSync runs the authoritative sync pipeline.
func RunSync(ctx context.Context, workspace pipeline.Workspace) (pipeline.SyncResult, error) {
	ws, err := ValidatedWorkspace(workspace)
	if err != nil {
		return pipeline.SyncResult{}, err
	}
	return RunPipelineSync(ctx, PipelineOptionsForWorkspace(ws)), nil
}

// RunDependencySync runs the injected lifecycle sync against a validated
// workspace.
func RunDependencySync(ctx context.Context, workspace pipeline.Workspace, sync DependencyLifecycleFunc) (pipeline.SyncResult, error) {
	ws, err := ValidatedWorkspace(workspace)
	if err != nil {
		return pipeline.SyncResult{}, err
	}
	return sync(ctx, ws)
}

// SnapshotLoaderFunc reloads the snapshot after a dependency operation.
type SnapshotLoaderFunc func(ctx context.Context, ws pipeline.Workspace, homeDir string, mode CatalogLoadMode) Snapshot

// RunDependencyUpdateWithLoader is RunDependencyUpdate with an explicit
// snapshot reload seam.
func RunDependencyUpdateWithLoader(ctx context.Context, workspace pipeline.Workspace, homeDir string, generation RequestGeneration, update DependencyLifecycleFunc, load SnapshotLoaderFunc) UpdateResult {
	ws, err := ValidatedWorkspace(workspace)
	if err != nil {
		return UpdateResult{Generation: generation, Stage: UpdateStageRefresh, Err: err}
	}
	synced, lifecycleErr := update(ctx, ws)
	warnings := synced.Warnings
	pipeline.ObservePhase(ctx, pipeline.PhaseReload)
	current := load(ctx, ws, homeDir, CatalogLoadCached)
	current.Generation = generation
	for _, warning := range warnings {
		if !pipeline.IsPromotedDiagnostic(warning) {
			current.Diagnostics = appendSnapshotDiagnostics(current.Diagnostics, DiagnosticWarning, DiagnosticUpdate, warning)
		}
	}
	current.Diagnostics = deduplicateDiagnostics(current.Diagnostics)
	current.Errs = DiagnosticValues(current.Diagnostics, DiagnosticError)
	current.Warnings = DiagnosticValues(current.Diagnostics, DiagnosticWarning)
	result := UpdateResult{Generation: generation, Snapshot: current, Stage: UpdateStageRefresh, Warnings: warnings, Report: synced.Report}
	if lifecycleErr != nil {
		result.Err = fmt.Errorf("update %s failed: %w", result.Stage, lifecycleErr)
	} else if reloadErr := errors.Join(DiagnosticErrors(current.Diagnostics)...); reloadErr != nil {
		result.Stage = UpdateStageReload
		result.Err = fmt.Errorf("update %s failed: %w", result.Stage, reloadErr)
	}
	return result
}

// RunUpdateWithLoader is RunUpdate with an explicit snapshot reload seam.
func RunUpdateWithLoader(ctx context.Context, workspace pipeline.Workspace, homeDir string, generation RequestGeneration, load SnapshotLoaderFunc) UpdateResult {
	ws, err := ValidatedWorkspace(workspace)
	if err != nil {
		return UpdateResult{Generation: generation, Stage: UpdateStageMaterialization, Err: err}
	}
	syncResult := RunPipelineSync(ctx, PipelineOptionsForWorkspace(ws))
	pipeline.ObservePhase(ctx, pipeline.PhaseReload)
	current := load(ctx, ws, homeDir, CatalogLoadCached)
	current.Generation = generation
	for _, warning := range syncResult.Warnings {
		if !pipeline.IsPromotedDiagnostic(warning) {
			current.Diagnostics = appendSnapshotDiagnostics(current.Diagnostics, DiagnosticWarning, DiagnosticUpdate, warning)
		}
	}
	current.Diagnostics = deduplicateDiagnostics(current.Diagnostics)
	current.Errs = DiagnosticValues(current.Diagnostics, DiagnosticError)
	current.Warnings = DiagnosticValues(current.Diagnostics, DiagnosticWarning)

	result := UpdateResult{
		Generation: generation,
		Snapshot:   current,
		Stage:      UpdateStageMaterialization,
		Warnings:   syncResult.Warnings,
		Report:     syncResult.Report,
	}
	switch {
	case len(syncResult.CatalogTrustErrors) > 0:
		result.Stage = UpdateStageRefresh
		result.Err = fmt.Errorf("update %s failed: %w", result.Stage, errors.Join(syncResult.CatalogTrustErrors...))
	case len(syncResult.MaterializeErrors) > 0:
		result.Err = fmt.Errorf("update %s failed: %w", result.Stage, errors.Join(syncResult.MaterializeErrors...))
	case len(DiagnosticErrors(current.Diagnostics)) > 0:
		result.Stage = UpdateStageReload
		result.Err = fmt.Errorf("update %s failed: %w", result.Stage, errors.Join(DiagnosticErrors(current.Diagnostics)...))
	}
	return result
}

// PipelineOptionsForWorkspace builds the resolve options shared by every
// session pipeline and dependency operation.
func PipelineOptionsForWorkspace(ws pipeline.Workspace) pipeline.ResolveOptions {
	return pipeline.ResolveOptions{
		Workspace: ws,
		Loaders: pipeline.Loaders{
			Org:    LoadOrgConfig,
			Global: LoadGlobalConfig,
			Repo:   LoadRepoConfig,
			Local: func() (*config.LocalConfig, error) {
				return LoadLocalConfig(ws.LocalConfigDir())
			},
		},
		CatalogOption: []catalog.LoadOption{
			catalog.WithCloneOrPull(CloneOrPull),
			catalog.WithDiscoverFunc(DiscoverCatalog),
		},
	}
}

// PromoteReportErrors turns a materialization report's ERROR diagnostics into
// error values, through the same promotion `sync` uses, and collapses repeats
// the same way.
//
// Two bugs used to live in the four lines this replaces (bead
// agent-marketplace-oc2r, adversarial review):
//
//   - It flattened the diagnostic with fmt.Errorf("%s: %s", d.Code, d.Message),
//     which is exactly the anonymous, resource-less, hint-less line the bead
//     removed from `sync`. `update` still showed three byte-identical lines for
//     three different failing skills, and deduplicated nothing.
//   - It selected diagnostics with `d.Severity >= diag.SeverityError`.
//     diag.Severity is an iota with SeverityError = 0, so that predicate is
//     `>= 0`: every warning and every info was promoted to an error and failed
//     the update. A `rule.devin.unsupported` warning — which AGENTS.md says
//     must never fail a run — failed the TUI's update.
func PromoteReportErrors(report *materialize.Report) []error {
	if report == nil {
		return nil
	}
	var errs []error
	for _, d := range report.Diags {
		if !d.IsError() {
			continue
		}
		errs = append(errs, pipeline.PromoteDiagnostic(d))
	}
	return pipeline.DedupIssues(errs)
}

// MarketplaceDiscoveryInstallable reports whether a discovery row can be
// installed.
func MarketplaceDiscoveryInstallable(r Row) bool {
	d := r.MarketplaceDiscovery
	return d != nil && (d.Item.Availability == mpapi.AvailabilityAvailable || d.Item.Availability == mpapi.AvailabilityOperatorExposed)
}

// DiscoveryInstallOptions derives the exact-revision install options for a
// discovered Marketplace row.
func DiscoveryInstallOptions(repoRoot string, r Row, s pipeline.Scope) (pipeline.InstallOptions, error) {
	d := r.MarketplaceDiscovery
	if !MarketplaceDiscoveryInstallable(r) {
		return pipeline.InstallOptions{}, errors.New("marketplace resource is not installable")
	}
	if d.Item.Revision == "" || d.Item.ContentDigest == "" {
		return pipeline.InstallOptions{}, errors.New("marketplace discovery did not provide an exact revision and digest")
	}
	registry := d.Item.RegistryID
	if registry == "" {
		registry = d.Item.RegistrySlug
	}
	locator := d.Endpoint.Alias + ":" + registry + "/" + d.Item.Kind + "/" + d.Item.Name + "@" + d.Item.ContentDigest
	selector := d.Endpoint.Alias
	if d.Endpoint.Alias == "" {
		locator = strings.TrimSuffix(d.Endpoint.URL, "/") + "/r/" + registry + "/" + d.Item.Kind + "/" + d.Item.Name + "/revisions/" + d.Item.ContentDigest
		selector = d.Endpoint.URL
	}
	kind, ok := skill.ParseKind(d.Item.Kind)
	if !ok {
		return pipeline.InstallOptions{}, fmt.Errorf("unsupported Marketplace resource kind %q", d.Item.Kind)
	}
	exact := &marketplace.MarketplaceLocator{Marketplace: selector, Alias: d.Endpoint.Alias, URL: d.Endpoint.URL, Registry: registry, Kind: d.Item.Kind, Name: d.Item.Name, Revision: d.Item.Revision, Digest: d.Item.ContentDigest}
	return pipeline.InstallOptions{RepoRoot: repoRoot, Args: []string{locator}, Kind: kind, HasKind: true, Scope: s, ScopeSpecified: true, Marketplace: selector, ExactMarketplaceLocator: exact}, nil
}

var (
	PrepareDiscoveryMarketplaceInstall = pipeline.PrepareMarketplaceInstall
	CommitPreparedMarketplaceInstall   = pipeline.CommitPreparedMarketplaceInstall
)

// DiscoveryInstallResult is the outcome of preparing or committing a
// Marketplace discovery install.
type DiscoveryInstallResult struct {
	Options  pipeline.InstallOptions
	Prepared *pipeline.PreparedMarketplaceInstall
	Preview  string
	Result   *pipeline.InstallResult
	Err      error
}

// PrepareDiscoveryInstall verifies and prepares an exact Marketplace
// install without writing configuration.
func PrepareDiscoveryInstall(ctx context.Context, options pipeline.InstallOptions) DiscoveryInstallResult {
	prepared, err := PrepareDiscoveryMarketplaceInstall(ctx, options)
	preview := ""
	if prepared != nil {
		preview = prepared.Preview()
	}
	return DiscoveryInstallResult{Options: options, Prepared: prepared, Preview: preview, Err: err}
}

// CommitDiscoveryInstall commits a prepared Marketplace install.
func CommitDiscoveryInstall(ctx context.Context, options pipeline.InstallOptions, prepared *pipeline.PreparedMarketplaceInstall) DiscoveryInstallResult {
	result, err := CommitPreparedMarketplaceInstall(ctx, prepared)
	return DiscoveryInstallResult{Options: options, Result: result, Err: err}
}
