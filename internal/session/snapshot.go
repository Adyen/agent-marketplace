package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"maps"
	"slices"
	"strings"
)

// SnapshotStage identifies which delivery of the staged startup snapshot a
// Snapshot carries.
type SnapshotStage uint8

const (
	SnapshotStageFinal SnapshotStage = iota
	SnapshotStageConfig
	SnapshotStageInventory
)

// Snapshot is one staged delivery of the interactive session's authoritative
// state. Stages is the stream the snapshot arrived on; later stages reuse it.
type Snapshot struct {
	Generation           RequestGeneration
	Stage                SnapshotStage
	Stages               <-chan Snapshot
	Rows                 []Row
	DriftReport          materialize.DriftReport
	DriftAssessment      DriftAssessment
	Catalogs             map[string][]catalog.Entry
	Resolved             []resolver.ResolvedArtifact
	MergedCfg            *config.Config
	ProvisionalCfg       *config.Config
	ProvisionalRows      []Row
	ConfiguredRegistries []config.RegistryEntry
	InventoryResult      *pipeline.ResolveResult
	MaterializationModes config.MaterializationModes
	LocalExcludes        []string
	GlobalHarnesses      []string
	ProjectHarnesses     []string
	TargetHarnesses      []hcc.Target
	TargetHarnessSource  config.Source
	Errs                 []error
	Warnings             []error
	Diagnostics          []Diagnostic
	RefreshAfterLoad     bool
}

// Injectable seams: tests substitute fakes for real config/catalog/pipeline
// I/O through these variables.
var (
	LoadOrgConfig    = config.LoadOrganizationConfig
	LoadGlobalConfig = config.LoadGlobalConfig
	LoadRepoConfig   = config.LoadRepoConfig
	LoadLocalConfig  = LoadLocalConfigAt
	CloneOrPull      = cache.CloneOrPull
	DiscoverCatalog  = catalog.Discover
	RunMaterialize   = materialize.Run
	RunPipelineSync  = pipeline.Sync
)

func LoadLocalConfigAt(localDir string) (*config.LocalConfig, error) {
	if config.IsHomeDir(localDir) {
		return nil, nil
	}
	return config.LoadLocalConfigFromPath(config.LocalConfigPath(localDir))
}

func ValidatedWorkspace(ws pipeline.Workspace) (pipeline.Workspace, error) {
	return pipeline.NewWorkspace(ws.RepoRoot(), ws.LocalConfigDir())
}

func DefaultWorkspace(repoRoot string) pipeline.Workspace {
	ws, err := pipeline.NewWorkspace(repoRoot, "")
	if err != nil {
		return pipeline.Workspace{}
	}
	return ws
}

func invalidWorkspaceSnapshot(err error) Snapshot {
	diagnostics := appendSnapshotDiagnostics(nil, DiagnosticError, DiagnosticResolution, err)
	return Snapshot{
		DriftAssessment: DriftFailed,
		Diagnostics:     diagnostics,
		Errs:            DiagnosticValues(diagnostics, DiagnosticError),
	}
}

// ReadSnapshotStagesForWorkspace runs one snapshot read and delivers its
// progressive stages on the returned channel: validated config, verified
// inventory, and the assessed result. Every delivery shares the caller's
// generation so stale streams are rejected by the normal request acceptance
// path. The channel is buffered past the maximum number of stages, so a
// reader that stops consuming after a superseded generation cannot block
// the writer.
func ReadSnapshotStagesForWorkspace(ctx context.Context, ws pipeline.Workspace, homeDir string, mode CatalogLoadMode, generation RequestGeneration) <-chan Snapshot {
	stages := make(chan Snapshot, 5)
	go func() {
		defer close(stages)
		emit := func(msg Snapshot) {
			msg.Generation = generation
			msg.Stages = stages
			stages <- msg
		}
		readSnapshotStaged(ctx, ws, homeDir, mode, true, false, emit)
	}()
	return stages
}

func ReadSnapshotForWorkspace(ctx context.Context, ws pipeline.Workspace, homeDir string, mode CatalogLoadMode) Snapshot {
	var snapshot Snapshot
	readSnapshotStaged(ctx, ws, homeDir, mode, false, false, func(msg Snapshot) { snapshot = msg })
	return snapshot
}

func userWorkspace(homeDir string) (pipeline.Workspace, error) {
	if !config.IsHomeDir(homeDir) {
		return pipeline.Workspace{}, fmt.Errorf("session: %s is not the user home directory", homeDir)
	}
	return pipeline.NewWorkspace(homeDir, homeDir)
}

func ReadUserSnapshotStages(ctx context.Context, homeDir string, mode CatalogLoadMode, generation RequestGeneration) <-chan Snapshot {
	stages := make(chan Snapshot, 5)
	go func() {
		defer close(stages)
		emit := func(msg Snapshot) {
			msg.Generation = generation
			msg.Stages = stages
			stages <- msg
		}
		ws, err := userWorkspace(homeDir)
		if err != nil {
			emit(invalidWorkspaceSnapshot(err))
			return
		}
		readSnapshotStaged(ctx, ws, homeDir, mode, true, true, emit)
	}()
	return stages
}

func ReadUserSnapshot(ctx context.Context, homeDir string, mode CatalogLoadMode) Snapshot {
	ws, err := userWorkspace(homeDir)
	if err != nil {
		return invalidWorkspaceSnapshot(err)
	}
	var snapshot Snapshot
	readSnapshotStaged(ctx, ws, homeDir, mode, false, true, func(msg Snapshot) { snapshot = msg })
	return snapshot
}

func readSnapshotStaged(ctx context.Context, ws pipeline.Workspace, homeDir string, mode CatalogLoadMode, progressive bool, userOnly bool, emit func(Snapshot)) {
	ctx = cache.WithFreshExactGitVerificationScope(ctx)
	ws, err := ValidatedWorkspace(ws)
	if err != nil {
		emit(invalidWorkspaceSnapshot(err))
		return
	}
	repoRoot := ws.RepoRoot()
	makeBase := func(inspection pipeline.InspectionResult) Snapshot {
		resolvedResult := inspection.ResolveResult
		base := Snapshot{
			Catalogs:             EntriesFromCatalogs(resolvedResult.CatalogLoad.Catalogs),
			Resolved:             resolvedResult.Resolved,
			MergedCfg:            resolvedResult.LoadedCfg,
			InventoryResult:      &resolvedResult,
			MaterializationModes: resolvedResult.MaterializationModes,
			LocalExcludes:        LocalExcludes(resolvedResult.LocalCfg),
			GlobalHarnesses:      configHarnesses(resolvedResult.GlobalCfg),
			ProjectHarnesses:     configHarnesses(resolvedResult.RepoCfg),
			TargetHarnesses:      resolvedResult.TargetHarnesses,
			TargetHarnessSource:  resolvedResult.TargetHarnessSource,
			DriftAssessment:      DriftFailed,
			RefreshAfterLoad:     resolvedResult.CatalogLoad.RefreshAfterLoad,
		}
		if resolvedResult.MergedCfg != nil {
			base.ConfiguredRegistries = append([]config.RegistryEntry(nil), resolvedResult.MergedCfg.Registries...)
		}
		for _, issue := range inspection.Issues {
			severity := DiagnosticWarning
			if issue.Severity == pipeline.IssueError {
				severity = DiagnosticError
			}
			err := issue.Err
			if issue.Category == pipeline.IssueCatalog && errors.Is(err, cache.ErrExactGitSnapshotUnavailable) {
				severity = DiagnosticWarning
				err = fmt.Errorf("locked exact snapshot is not cached; press s to sync or run agent-marketplace sync --locked: %w", err)
				slog.Debug("TUI exact snapshot requires synchronization", "diagnostic", "lock.snapshot-unavailable")
			}
			base.Diagnostics = appendSnapshotDiagnostics(base.Diagnostics, severity, diagnosticCategoryFor(issue.Category), err)
		}
		base.Diagnostics = deduplicateDiagnostics(base.Diagnostics)
		base.Errs = DiagnosticValues(base.Diagnostics, DiagnosticError)
		base.Warnings = DiagnosticValues(base.Diagnostics, DiagnosticWarning)
		return base
	}
	loaders := pipeline.Loaders{
		Org:    LoadOrgConfig,
		Global: LoadGlobalConfig,
		Repo:   LoadRepoConfig,
		Local: func() (*config.LocalConfig, error) {
			return LoadLocalConfig(ws.LocalConfigDir())
		},
	}
	if userOnly {
		loaders = pipeline.Loaders{}
	}
	var inventoryRows []Row
	var unavailable bool
	inspectOptions := pipeline.InspectOptions{
		Resolve: pipeline.ResolveOptions{
			Workspace:     ws,
			CatalogMode:   sessionCatalogLoadMode(mode),
			SkipInventory: true,
			EffectiveOpts: config.LoadEffectiveOptions{
				OrganizationPath: config.OrganizationConfigPath(),
				GlobalPath:       filepath.Join(homeDir, ".agents", "config.yaml"),
				RepoPath:         config.ProjectConfigPath(repoRoot),
				LocalPath:        config.LocalConfigPath(ws.LocalConfigDir()),
			},
			Loaders: loaders,
			CatalogOption: []catalog.LoadOption{
				catalog.WithCloneOrPull(CloneOrPull),
				catalog.WithDiscoverFunc(DiscoverCatalog),
			},
			ReadOnlyExactLocks:        true,
			AllowMissingReadOnlyLocks: true,
		},
		Resolved: func(result pipeline.InspectionResult) {
			resolved := result.ResolveResult
			recoveryCfg := resolved.LoadedCfg
			for _, err := range resolved.CatalogErrors {
				if catalog.IsOverlapWarning(err) {
					continue
				}
				if resolved.MergedCfg != nil {
					recoveryCfg = resolved.MergedCfg
				}
				break
			}
			// The verified inventory is published before detail capture and the
			// materialization dry run: rows are navigable and config-only toggles are
			// eligible while drift stays explicitly pending.
			inventoryRows = BuildRows(resolved.LoadedCfg, resolved.CatalogLoad.Catalogs, resolved.DisplayArtifacts(), resolved.LocalCfg, materialize.DriftReport{}, harnessDefaultsBySource(resolved.GlobalCfg, resolved.RepoCfg))
			inventoryRows = AppendUnavailableSelectionRows(inventoryRows, recoveryCfg, resolved.LocalCfg, resolved.ResolveErrors, resolved.CatalogLoad.Catalogs)
			unavailable = RowsHaveUnavailable(inventoryRows)
			inventory := makeBase(result)
			inventory.Stage = SnapshotStageInventory
			inventory.Rows = inventoryRows
			inventory.DriftAssessment = DriftUnknown
			inventory.InventoryResult = nil // the assessed stage owns inventory telemetry
			emit(inventory)
		},
		DryRun: RunMaterialize,
	}
	if progressive {
		inspectOptions.Resolve.BrowsingConfigReady = func(merged *config.Config) {
			emit(Snapshot{Stage: SnapshotStageConfig, MergedCfg: merged})
		}
		inspectOptions.Resolve.ProvisionalConfigReady = func(merged *config.Config, local *config.LocalConfig) {
			var defaults map[config.Source][]string
			if userOnly {
				globalCfg, _ := LoadGlobalConfig()
				defaults = harnessDefaultsBySource(globalCfg, nil)
			} else {
				defaults = provisionalHarnessDefaults()
			}
			emit(Snapshot{Stage: SnapshotStageConfig, ProvisionalCfg: merged, ProvisionalRows: BuildProvisionalRows(WithCachedCollectionLeaves(merged, marketplace.DefaultCache()), local, provisionalLocalCatalogs(merged, ws), defaults)})
		}
		inspectOptions.Resolve.ConfigReady = func(merged *config.Config, targets []hcc.Target) {
			emit(Snapshot{Stage: SnapshotStageConfig, MergedCfg: merged, TargetHarnesses: targets})
		}
	}
	inspection, releaseExactLease := pipeline.Inspect(ctx, inspectOptions)
	defer releaseExactLease()
	resolvedResult := inspection.ResolveResult

	base := makeBase(inspection)

	if len(resolvedResult.ConfigErrors) > 0 {
		emit(base)
		return
	}

	var hardCatalogFailure bool
	for _, err := range resolvedResult.CatalogErrors {
		if !catalog.IsOverlapWarning(err) {
			hardCatalogFailure = true
			break
		}
	}

	// Any generic resolve failure — a zero Workspace, context cancellation,
	// an exact-lock acquisition error — must surface diagnostics and return
	// here, before detail capture or dry-run materialization can act on an
	// invalid root and mask the original error.
	if len(resolvedResult.Errors) > 0 && len(resolvedResult.CatalogErrors) == 0 && len(resolvedResult.ResolveErrors) == 0 {
		emit(base)
		return
	}

	var resolvedNames []string
	// Copy detail bytes before any downstream work can yield long enough for a
	// waiting GC to run immediately after the resolution lease is released.
	detailSources := CaptureSnapshotDetailSources(resolvedResult.Resolved, resolvedResult.CatalogLoad.Catalogs)
	for _, resolved := range resolvedResult.Resolved {
		resolvedNames = append(resolvedNames, skill.ResourceKey(resolved.Kind, resolved.Name))
	}
	dryRunReport := inspection.Report
	driftReport := materialize.DriftReportFromReport(dryRunReport, resolvedNames)
	driftState := DriftReady
	if hardCatalogFailure || unavailable || dryRunReport == nil || !inspection.Healthy() {
		driftState = DriftFailed
	}
	base.Rows = ApplyDriftItems(inventoryRows, driftReport)
	attachDetailSources(base.Rows, detailSources)
	base.DriftReport = driftReport
	base.DriftAssessment = driftState
	emit(base)
}

// DriftItemsByResource keys drift items by resource. A resource can have
// several, for example missing from a new target harness and orphaned in the
// one it left, so the most actionable wins: broken, then missing, then an
// orphaned copy.
func DriftItemsByResource(driftReport materialize.DriftReport) map[string]materialize.DriftItem {
	byName := make(map[string]materialize.DriftItem, len(driftReport.Missing)+len(driftReport.Broken)+len(driftReport.Orphaned))
	for _, items := range [][]materialize.DriftItem{driftReport.Orphaned, driftReport.Missing, driftReport.Broken} {
		for _, item := range items {
			byName[item.Name] = item
		}
	}
	return byName
}

func ApplyDriftItems(rows []Row, driftReport materialize.DriftReport) []Row {
	out := slices.Clone(rows)
	if len(driftReport.Missing)+len(driftReport.Broken)+len(driftReport.Orphaned) == 0 {
		return out
	}
	driftByName := DriftItemsByResource(driftReport)
	for i := range out {
		if out[i].IsAvailable || out[i].DriftItem != nil {
			continue
		}
		item, hasDrift := driftByName[skill.ResourceKey(out[i].Artifact.Kind, out[i].Artifact.Name)]
		if hasDrift {
			out[i].DriftItem = &item
		}
	}
	return out
}

func CaptureSnapshotDetailSources(resolved []resolver.ResolvedArtifact, catalogs map[string]CatalogInfo) map[string]*DetailSourceSnapshot {
	detailSources := make(map[string]*DetailSourceSnapshot, len(resolved))
	var detailBudget DetailCaptureBudget
	capturePath := func(key, sourcePath string) {
		if key == "" {
			return
		}
		if _, ok := detailSources[key]; !ok {
			detailSources[key] = detailBudget.Capture(sourcePath)
		}
	}
	captureEntry := func(entry catalog.Entry) {
		capturePath(entry.Path, catalog.SourcePath(entry))
	}
	for _, artifact := range resolved {
		captureEntry(artifact.Entry)
	}
	for _, key := range slices.Sorted(maps.Keys(catalogs)) {
		loaded := catalogs[key]
		entries := slices.Clone(loaded.Entries)
		slices.SortFunc(entries, func(a, b catalog.Entry) int {
			if order := strings.Compare(a.Kind.String(), b.Kind.String()); order != 0 {
				return order
			}
			if order := strings.Compare(a.Name, b.Name); order != 0 {
				return order
			}
			if order := strings.Compare(a.Path, b.Path); order != 0 {
				return order
			}
			return strings.Compare(catalog.SourcePath(a), catalog.SourcePath(b))
		})
		for _, entry := range entries {
			captureEntry(entry)
		}
		malformedEntries := slices.Clone(loaded.Malformed)
		slices.SortFunc(malformedEntries, func(a, b skill.MalformedSkill) int {
			if order := strings.Compare(a.DirName, b.DirName); order != 0 {
				return order
			}
			return strings.Compare(a.Path, b.Path)
		})
		for _, malformed := range malformedEntries {
			capturePath(malformed.Path, malformed.Path)
		}
	}
	return detailSources
}
