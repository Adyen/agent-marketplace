package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	"charm.land/huh/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
)

type pane int

const (
	paneList pane = iota
	paneDetail
	paneHelp
	paneErrors
	paneConfirm
	paneToggleModal
	paneInitModal
	paneMigrationModal
	paneHarnessModal
	paneEditConfigModal
	paneMarketplaceConsent
	paneViewSettings
	paneBulkToggle
)

// rowLabelMemo is TUI view state cached on a row's opaque LabelMemo slot.
// The session core owns the slot but never interprets it.
type rowLabelMemo struct {
	depth   int
	width   int
	compact bool
	color   bool
	height  int
	valid   bool
}

// rowLabelMemoFor returns the row's TUI label memo, creating it on first
// use. The memo pointer is shared by every copy of the row, exactly as
// before the slot moved into the session row type.
func rowLabelMemoFor(r *row) *rowLabelMemo {
	memo, _ := r.LabelMemo.(*rowLabelMemo)
	if memo == nil {
		memo = &rowLabelMemo{}
		r.LabelMemo = memo
	}
	return memo
}

func ensureLabelMemo(r *row) {
	rowLabelMemoFor(r)
}

type Model struct {
	repoRoot            string
	workspace           pipeline.Workspace
	homeDir             string
	rows                []row
	authoritativeRows   []row
	provisionalRows     []row
	discoveryRows       []row
	browseDiscoveryRows []row
	discoveryOutcomes   []marketplace.DiscoveryOutcome
	discoveryCancel     context.CancelFunc
	discoveryLoading    bool
	discoverySequence   uint64
	discoveryErrs       []error
	filteredRows        []row
	filterScratch       []row
	cursor              int
	filter              string
	filterActive        bool
	searchInput         textinput.Model
	loading             bool
	assessmentPending   bool

	pane                pane
	driftReport         materialize.DriftReport
	driftAssessment     driftAssessment
	diagnostics         diagnosticStore
	errs                []error
	warnings            []error
	syncing             bool
	refreshing          bool
	requests            requestState
	configInvalid       bool
	rowsStale           bool
	dependencyLifecycle DependencyLifecycle

	// Cached config state for toggle/sync
	mergedCfg        *config.Config
	resolved         []resolver.ResolvedArtifact
	localExcludes    []string
	globalHarnesses  []string
	projectHarnesses []string

	listOffset       int // first visible flatItems index in the left pane
	listHeightPrefix []int
	listHeightWidth  int
	width            int
	height           int

	// viewSettings is the confirmed (or live-previewed) grouping selection.
	viewSettings viewSettings

	// foldedSections keeps fold state per grouping combination for the
	// session, keyed by the normalized viewSettings; each inner map is keyed
	// by section path ("status:enabled", "src:<id>:kind:skills", ...).
	foldedSections map[viewSettings]map[string]bool

	// viewPrefsWarning holds the startup preference-load warning; it is
	// surfaced once as a footer toast without touching authored config.
	viewPrefsWarning string

	// viewModal is the View settings modal state: the focused field and the
	// original settings captured at open so Esc can restore them.
	viewModal viewModalState

	// bulkModal is the section bulk-toggle modal state; see bulk.go.
	bulkModal bulkModalState

	queryFoldSections map[viewSettings]map[string]bool
	queryFoldQuery    string

	// flatItems is the render-ready list computed from filteredRows +
	// foldState. Cursor indexes into this slice. Call rebuildFlatItems
	// whenever rows, filter, or fold state change.
	flatItems []flatItem

	// zPending is true after the user presses 'z' and awaits a follow-up
	// key (a/M/R) to complete a vim-style fold command.
	zPending bool

	// palette is the command-palette overlay state. It opens only in the
	// list pane with no active search input, modal, overlay, or pending
	// fold prefix; see palette.go.
	palette paletteState

	// modalSkill is the row whose state is displayed in paneToggleModal.
	// Captured at modal-open time so rapid reloads can't change what the
	// user is acting on mid-interaction.
	modalSkill                  row
	marketplaceInstallOptions   pipeline.InstallOptions
	marketplacePreparedInstall  *pipeline.PreparedMarketplaceInstall
	marketplaceInstallOperation operationID
	marketplaceInstallReload    requestGeneration
	marketplaceInstallPreview   string
	harnessModal                harnessModalState
	initModal                   initModalState
	migrationPrompt             migrationPrompt
	migrationDismissed          bool
	operations                  operationState
	pendingToggleOperation      operationID
	pendingToggleReload         requestGeneration
	// updateReload is the superseding snapshot generation that must be
	// accepted (or fail) before a stale updateDoneMsg's operation ends;
	// updatePendingErr retains that update's own error for the verdict.
	updateReload          requestGeneration
	updatePendingErr      error
	updatePendingWarnings []error
	// syncReload is the snapshot generation scheduled by syncDoneMsg that
	// must be accepted (or fail) before the sync operation ends;
	// syncPendingErr retains the pipeline's own error for the verdict.
	syncReload          requestGeneration
	syncPendingErr      error
	syncPendingWarnings []error

	// pendingExcludeAction holds a toggleAction that needs to emit
	// excludeCompetingRegistriesCmd after the primary toggle succeeds.
	// Set when enabling a colliding resource; cleared on configWrittenMsg.
	pendingExcludeAction  *toggleAction
	selectionAfterDisable string

	// spinner provides an animated indicator in the footer while
	// loading, refreshing, or syncing.
	spinner spinner.Model

	detailViewport             viewport.Model
	helpViewport               viewport.Model
	errorsViewport             viewport.Model
	marketplaceConsentViewport viewport.Model

	// detailKey is the stable row identity of the resource whose full detail
	// pane is open. It keeps an accepted snapshot rebuild anchored to the same
	// resource even when rows are inserted or reordered.
	detailKey          string
	detailRenderWidth  int
	detailStickyBottom bool
	bundle             *bundleTree
	bundleOffset       int
	detailSelectedPath string
	detailLegacyRender bool

	previewSel   previewSelection
	pendingClick pendingClick

	// toast/toastExp mirror the newest visible toast for the single-line
	// footer segment; toastQueue holds the whole visible stack (newest
	// last, bounded by toastStackMax) rendered as a top-right layer.
	toast      string
	toastExp   time.Time
	toastQueue []queuedToast

	// progress is the active operation's phase observation source;
	// operationPhase is the newest accepted phase label (empty = generic
	// running), and progressLabel names the operation for the status bar.
	progress       *operationProgressSource
	progressLabel  string
	operationPhase string

	confirmMsg string

	// Huh forms owned by the interactive modals (AC4). Each form carries
	// its option state; letter accelerators are routed before delegation.
	toggleForm       *huh.Form
	toggleFormFor    string
	toggleChoice     *string
	migrationForm    *huh.Form
	migrationValue   *bool
	editConfigForm   *huh.Form
	editConfigChoice *string
	syncQuitForm     *huh.Form
	syncQuitValue    *bool

	// quitAfterSync is set when the user confirms "sync+quit" in the
	// confirm dialog. The syncDoneMsg handler checks this flag and emits
	// tea.Quit only after the sync has actually completed, avoiding the
	// race where tea.Quit in a tea.Batch kills the program before the
	// sync goroutine finishes.
	quitAfterSync bool

	// syncQuitErr holds the sync error (if any) from the sync that
	// triggered a quit. Run() checks this after the program exits so
	// failed sync+quit flows surface the error to the caller instead of
	// exiting cleanly.
	syncQuitErr error

	// ctx carries the command/root span context through the TUI lifecycle.
	// It is set by Run and used by action and session span instrumentation.
	ctx context.Context

	// sessionSpan is the TUI session span; it is ended when Run returns.
	sessionSpan telemetry.Span

	// keys is the centralized keymap; the single source of truth for all
	// key bindings and their help text. Both the footer hint line and the
	// ? help overlay are driven from this keymap via the help model.
	keys keyMap
	help help.Model

	// isDark records the terminal background reported by the most recent
	// tea.BackgroundColorMsg; theme carries the semantic colors built from
	// it so every model renders against its own reported background.
	isDark bool
	theme  theme
}
