package tui

// This file adapts the surface-neutral session core (internal/session) to
// the TUI: type aliases and var wrappers keep the package-local call sites
// short, and the converters translate the core snapshot stream into tea
// messages.

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/session"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

type (
	row                     = session.Row
	marketplaceSelection    = session.MarketplaceSelection
	marketplaceDiscovery    = session.MarketplaceDiscovery
	driftAssessment         = session.DriftAssessment
	requestKind             = session.RequestKind
	requestGeneration       = session.RequestGeneration
	requestState            = session.RequestState
	operationID             = session.OperationID
	operationKind           = session.OperationKind
	operationState          = session.OperationState
	operationProgressSource = session.OperationProgressSource
	diagnosticStore         = session.DiagnosticStore
	tuiDiagnostic           = session.Diagnostic
	diagnosticSeverity      = session.DiagnosticSeverity
	diagnosticCategory      = session.DiagnosticCategory
	snapshotStage           = session.SnapshotStage
	catalogLoadMode         = session.CatalogLoadMode
	previewTargetRead       = session.PreviewTargetRead

	DependencyLifecycleFunc = session.DependencyLifecycleFunc
	DependencyMutationFunc  = session.DependencyMutationFunc
	DependencyLifecycle     = session.DependencyLifecycle
)

const (
	driftUnknown = session.DriftUnknown
	driftReady   = session.DriftReady
	driftFailed  = session.DriftFailed

	requestDetail    = session.RequestDetail
	requestSnapshot  = session.RequestSnapshot
	requestDiscovery = session.RequestDiscovery

	operationMutation = session.OperationMutation
	operationTargets  = session.OperationTargets
	operationSync     = session.OperationSync
	operationUpdate   = session.OperationUpdate
	operationEditor   = session.OperationEditor

	diagnosticError   = session.DiagnosticError
	diagnosticWarning = session.DiagnosticWarning

	diagnosticConfig          = session.DiagnosticConfig
	diagnosticCatalog         = session.DiagnosticCatalog
	diagnosticResolution      = session.DiagnosticResolution
	diagnosticMaterialization = session.DiagnosticMaterialization
	diagnosticMutation        = session.DiagnosticMutation
	diagnosticSync            = session.DiagnosticSync
	diagnosticUpdate          = session.DiagnosticUpdate
	diagnosticEditor          = session.DiagnosticEditor

	snapshotStageFinal     = session.SnapshotStageFinal
	snapshotStageConfig    = session.SnapshotStageConfig
	snapshotStageInventory = session.SnapshotStageInventory

	catalogLoadCached  = session.CatalogLoadCached
	catalogLoadRefresh = session.CatalogLoadRefresh

	terminalSingleLine   = session.TerminalSingleLine
	terminalMarkdownBody = session.TerminalMarkdownBody

	maxDetailSourceBytes   = session.MaxDetailSourceBytes
	detailTruncationMarker = session.DetailTruncationMarker

	progressPollInterval  = session.ProgressPollInterval
	operationPhaseConsent = session.OperationPhaseConsent
)

// Pure-function aliases under the package-local names.
var (
	sanitizeTerminalData            = session.SanitizeTerminalData
	sanitizeMarkdownForTerminal     = session.SanitizeMarkdownForTerminal
	sanitizeReturnedError           = session.SanitizeReturnedError
	searchTerms                     = session.SearchTerms
	registryDisplayName             = session.RegistryDisplayName
	registryIdentityKey             = session.RegistryIdentityKey
	marketplaceEndpointDisplay      = session.MarketplaceEndpointDisplay
	marketplaceCatalogKey           = session.MarketplaceCatalogKey
	marketplaceDiscoveryInstallable = session.MarketplaceDiscoveryInstallable
	rowsHaveUnavailable             = session.RowsHaveUnavailable
	buildRows                       = session.BuildRows
	appendUnavailableSelectionRows  = session.AppendUnavailableSelectionRows
	loadCatalogsForTUI              = session.LoadCatalogs
	validatedTUIWorkspace           = session.ValidatedWorkspace
	defaultTUIWorkspace             = session.DefaultWorkspace
	promoteReportErrors             = session.PromoteReportErrors
	shouldShowInitModal             = session.NeedsFirstConfiguration
	discoveryInstallOptions         = discoveryInstallOptionsForScope
	snapshotDiagnostic              = session.SnapshotDiagnostic
	diagnosticErrors                = session.DiagnosticErrors
	diagnosticsFromLegacy           = session.DiagnosticsFromLegacy
	diagnosticValues                = session.DiagnosticValues
	stableDiagnosticKey             = pipeline.IssueDedupKey
	readDetailSourceLimit           = session.ReadDetailSourceLimit
	readPreviewSource               = session.ReadPreviewSource
	sourceLabel                     = session.SourceLabel
)

// discoveryInstallOptionsForScope translates the tui scope to a pipeline
// scope before consulting the core.
func discoveryInstallOptionsForScope(repoRoot string, r row, s scope) (pipeline.InstallOptions, error) {
	return session.DiscoveryInstallOptions(repoRoot, r, pipelineScope(s))
}

// snapshotLoadedMsg is the TUI message carrying the core snapshot fields.
// stages carries the remainder of the session stage stream this message was
// delivered on, already bridged to TUI message values.
type snapshotLoadedMsg struct {
	generation           requestGeneration
	stage                snapshotStage
	stages               <-chan snapshotLoadedMsg
	rows                 []row
	driftReport          materialize.DriftReport
	driftAssessment      driftAssessment
	catalogs             map[string][]catalog.Entry
	resolved             []resolver.ResolvedArtifact
	mergedCfg            *config.Config
	provisionalCfg       *config.Config
	provisionalRows      []row
	configuredRegistries []config.RegistryEntry
	inventoryResult      *pipeline.ResolveResult
	materializationModes config.MaterializationModes
	localExcludes        []string
	globalHarnesses      []string
	projectHarnesses     []string
	targetHarnesses      []hcc.Target
	targetHarnessSource  config.Source
	errs                 []error
	warnings             []error
	diagnostics          []tuiDiagnostic
	refreshAfterLoad     bool
}

// snapshotMsgFromCore converts one core snapshot delivery into the TUI
// message. stages is the TUI-side stage channel that replaces the core
// channel on the message.
func snapshotMsgFromCore(s session.Snapshot, stages <-chan snapshotLoadedMsg) snapshotLoadedMsg {
	return snapshotLoadedMsg{
		generation:           s.Generation,
		stage:                s.Stage,
		stages:               stages,
		rows:                 s.Rows,
		driftReport:          s.DriftReport,
		driftAssessment:      s.DriftAssessment,
		catalogs:             s.Catalogs,
		resolved:             s.Resolved,
		mergedCfg:            s.MergedCfg,
		provisionalCfg:       s.ProvisionalCfg,
		provisionalRows:      s.ProvisionalRows,
		configuredRegistries: s.ConfiguredRegistries,
		inventoryResult:      s.InventoryResult,
		materializationModes: s.MaterializationModes,
		localExcludes:        s.LocalExcludes,
		globalHarnesses:      s.GlobalHarnesses,
		projectHarnesses:     s.ProjectHarnesses,
		targetHarnesses:      s.TargetHarnesses,
		targetHarnessSource:  s.TargetHarnessSource,
		errs:                 s.Errs,
		warnings:             s.Warnings,
		diagnostics:          s.Diagnostics,
		refreshAfterLoad:     s.RefreshAfterLoad,
	}
}

// bridgeSnapshotStages adapts a core stage channel into the TUI message
// channel, preserving the buffered non-blocking producer semantics: the
// core writer never blocks on this buffer, and the bridge drains it into an
// equally buffered channel.
func bridgeSnapshotStages(in <-chan session.Snapshot) <-chan snapshotLoadedMsg {
	if in == nil {
		return nil
	}
	out := make(chan snapshotLoadedMsg, cap(in)+1)
	go func() {
		defer close(out)
		for s := range in {
			out <- snapshotMsgFromCore(s, out)
		}
	}()
	return out
}

// loadSnapshot reads one non-progressive snapshot as a tea.Cmd.
func loadSnapshot(ctx context.Context, workspace pipeline.Workspace, homeDir string, generation requestGeneration) tea.Cmd {
	return func() tea.Msg {
		snapshot := session.ReadSnapshotForWorkspace(ctx, workspace, homeDir, catalogLoadCached)
		snapshot.Generation = generation
		return snapshotMsgFromCore(snapshot, nil)
	}
}

// loadSnapshotProgressive schedules the staged startup snapshot: a validated
// config stage (which starts cached Marketplace discovery), a verified
// inventory stage, and the final assessed snapshot, all under one generation.
func loadSnapshotProgressive(ctx context.Context, workspace pipeline.Workspace, homeDir string, generation requestGeneration) tea.Cmd {
	return waitSnapshotStages(bridgeSnapshotStages(session.ReadSnapshotStagesForWorkspace(ctx, workspace, homeDir, catalogLoadCached, generation)))
}

func waitSnapshotStages(stages <-chan snapshotLoadedMsg) tea.Cmd {
	if stages == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-stages
		if !ok {
			return snapshotStreamClosedMsg{}
		}
		return msg
	}
}

func refreshSnapshot(ctx context.Context, workspace pipeline.Workspace, homeDir string, generation requestGeneration) tea.Cmd {
	return func() tea.Msg {
		snapshot := session.ReadSnapshotForWorkspace(ctx, workspace, homeDir, catalogLoadRefresh)
		snapshot.Generation = generation
		return refreshLoadedMsg{snapshotLoadedMsg: snapshotMsgFromCore(snapshot, nil)}
	}
}

// readSnapshotStagesForWorkspace bridges the core staged reader into the
// TUI message channel.
func readSnapshotStagesForWorkspace(ctx context.Context, ws pipeline.Workspace, homeDir string, mode catalogLoadMode, generation requestGeneration) <-chan snapshotLoadedMsg {
	return bridgeSnapshotStages(session.ReadSnapshotStagesForWorkspace(ctx, ws, homeDir, mode, generation))
}

func readSnapshotStages(ctx context.Context, repoRoot, homeDir string, mode catalogLoadMode, generation requestGeneration) <-chan snapshotLoadedMsg {
	return readSnapshotStagesForWorkspace(ctx, defaultTUIWorkspace(repoRoot), homeDir, mode, generation)
}

func readSnapshotForWorkspace(ctx context.Context, ws pipeline.Workspace, homeDir string, mode catalogLoadMode) snapshotLoadedMsg {
	return snapshotMsgFromCore(session.ReadSnapshotForWorkspace(ctx, ws, homeDir, mode), nil)
}

// readSnapshotForUpdate is the package-level reload seam dependency update
// and plain update use; tests substitute it with fixtures.
var readSnapshotForUpdate = readSnapshotForWorkspace

// snapshotCoreFromMsg converts a TUI snapshot message back into the core
// snapshot value, so test fixtures substituting readSnapshotForUpdate feed
// the core update pipeline unchanged.
func snapshotCoreFromMsg(m snapshotLoadedMsg) session.Snapshot {
	return session.Snapshot{
		Generation:           m.generation,
		Stage:                m.stage,
		Rows:                 m.rows,
		DriftReport:          m.driftReport,
		DriftAssessment:      m.driftAssessment,
		Catalogs:             m.catalogs,
		Resolved:             m.resolved,
		MergedCfg:            m.mergedCfg,
		ProvisionalCfg:       m.provisionalCfg,
		ProvisionalRows:      m.provisionalRows,
		ConfiguredRegistries: m.configuredRegistries,
		InventoryResult:      m.inventoryResult,
		MaterializationModes: m.materializationModes,
		LocalExcludes:        m.localExcludes,
		GlobalHarnesses:      m.globalHarnesses,
		ProjectHarnesses:     m.projectHarnesses,
		TargetHarnesses:      m.targetHarnesses,
		TargetHarnessSource:  m.targetHarnessSource,
		Errs:                 m.errs,
		Warnings:             m.warnings,
		Diagnostics:          m.diagnostics,
		RefreshAfterLoad:     m.refreshAfterLoad,
	}
}

// sessionSnapshotLoader adapts readSnapshotForUpdate to the core loader
// seam.
func sessionSnapshotLoader(ctx context.Context, ws pipeline.Workspace, homeDir string, mode session.CatalogLoadMode) session.Snapshot {
	return snapshotCoreFromMsg(readSnapshotForUpdate(ctx, ws, homeDir, mode))
}
