package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/cursor"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/Adyen/agent-marketplace/internal/logging"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

type DependencyLifecycleFunc func(context.Context, pipeline.Workspace) ([]error, error)
type DependencyMutationFunc func(context.Context, pipeline.Workspace, func(pipeline.ConfigPathMapper) error) error

type DependencyLifecycle struct {
	Sync   DependencyLifecycleFunc
	Update DependencyLifecycleFunc
	Mutate DependencyMutationFunc
	// InstallReconcile prepares acquisition locks for the sync that follows a
	// Marketplace install, so other sources stay at their locked revisions.
	InstallReconcile pipeline.DependencyReconciler
}

type Option func(*Model)

func WithDependencyLifecycle(lifecycle DependencyLifecycle) Option {
	return func(model *Model) { model.dependencyLifecycle = lifecycle }
}

// WithWorkspace supplies the already validated roots shared by every TUI
// pipeline and dependency operation.
func WithWorkspace(workspace pipeline.Workspace) Option {
	return func(model *Model) {
		model.workspace = workspace
		if workspace.RepoRoot() != "" {
			model.repoRoot = workspace.RepoRoot()
		}
	}
}

func searchInputStyles(t theme) textinput.Styles {
	styles := textinput.DefaultStyles(t.dark)
	styles.Focused.Prompt = styleSearchPrompt
	styles.Focused.Text = textStyle(t)
	styles.Cursor.Color = t.accent
	return styles
}

func NewModel(repoRoot, homeDir string, options ...Option) Model {
	// Snapshot the detected background once; tea.BackgroundColorMsg updates
	// the model's own isDark/theme afterwards.
	isDark := compat.HasDarkBackground
	theme := newTheme(isDark)

	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = styleSpinner

	si := textinput.New()
	si.Prompt = "/ "
	si.SetStyles(searchInputStyles(theme))

	workspace := defaultTUIWorkspace(repoRoot)
	if workspace.RepoRoot() != "" {
		repoRoot = workspace.RepoRoot()
	}
	m := Model{
		repoRoot:                   repoRoot,
		workspace:                  workspace,
		homeDir:                    homeDir,
		width:                      80,
		height:                     24,
		loading:                    true,
		foldedSections:             map[viewSettings]map[string]bool{},
		spinner:                    sp,
		searchInput:                si,
		detailViewport:             newTUIViewport(80, 24),
		helpViewport:               newTUIViewport(80, 23),
		errorsViewport:             newTUIViewport(80, 23),
		marketplaceConsentViewport: newTUIViewport(80, 22),
		keys:                       defaultKeyMap,
		help:                       newHelpModel(theme),
		isDark:                     isDark,
		theme:                      theme,
		pendingInventory:           newPendingInventory(),
	}
	for _, option := range options {
		option(&m)
	}
	settings, prefsWarning := loadViewPreferences(homeDir)
	m.viewSettings = settings
	m.viewPrefsWarning = prefsWarning
	if !testSkipInitModal && shouldShowInitModal(homeDir) {
		slog.Info("init modal opened", "home_dir", homeDir)
		initModal, err := newInitModalState(homeDir)
		if err != nil {
			slog.Error("failed to detect installed harnesses", "error", err)
			initModal = initModalState{selected: map[string]bool{}}
		}
		m.initModal = initModal
		m.pane = paneInitModal
	}
	// Bubble Tea schedules the initial snapshot through Init. Reserve its
	// generation here so the resulting command and this model share identity.
	m.beginRequest(requestSnapshot)
	return m
}

func newTUIViewport(width, height int) viewport.Model {
	v := viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
	v.MouseWheelDelta = 1
	return v
}

// testSkipInitModal is a package-private test hook that, when set, prevents
// NewModel from automatically opening the first-run init modal. Production
// code never touches it; see init_test.go's TestMain for the setter.
var testSkipInitModal bool

// shouldShowInitModal returns true when the home directory exists but the
// user personal config file does not. We require the home directory itself to
// be present so test sandboxes with synthetic paths don't accidentally trigger
// the first-run dialog.
func shouldShowInitModal(homeDir string) bool {
	if homeDir == "" {
		return false
	}
	if info, err := os.Stat(homeDir); err != nil || !info.IsDir() {
		return false
	}
	_, err := os.Stat(filepath.Join(homeDir, ".agents", "config.yaml"))
	return os.IsNotExist(err)
}

func (m Model) Init() tea.Cmd {
	var warningCmd tea.Cmd
	if m.viewPrefsWarning != "" {
		text := m.viewPrefsWarning
		warningCmd = func() tea.Msg {
			return toastMsg{text: text, duration: 5 * time.Second}
		}
	}
	return tea.Batch(loadSnapshotProgressive(ctxOrBackground(m.ctx), m.workspace, m.homeDir, m.requests.active(requestSnapshot)), m.spinner.Tick, warningCmd, m.upgradeNotice)
}

func (m *Model) applySnapshotStatus(msg snapshotLoadedMsg) {
	assessment := msg.driftAssessment
	if msg.stage != snapshotStageFinal {
		m.driftAssessment = assessment
		m.replaceSnapshotDiagnostics(msg.diagnostics, msg.errs, msg.warnings)
		m.rowsStale = len(diagnosticValues(m.diagnostics.snapshot, diagnosticError)) > 0
		return
	}
	if assessment == driftUnknown {
		if len(msg.errs) > 0 || len(diagnosticErrors(msg.diagnostics)) > 0 {
			assessment = driftFailed
		} else {
			assessment = driftReady
		}
	}
	m.driftAssessment = assessment
	m.replaceSnapshotDiagnostics(msg.diagnostics, msg.errs, msg.warnings)
	m.rowsStale = assessment == driftFailed || len(diagnosticValues(m.diagnostics.snapshot, diagnosticError)) > 0
	m.configInvalid = false
	for _, d := range m.diagnostics.snapshot {
		if d.severity == diagnosticError && d.category == diagnosticConfig {
			m.configInvalid = true
			break
		}
	}
	m.dismissInvalidConfigMutationPane()
	if m.pendingToggleReload == 0 || msg.generation < m.pendingToggleReload {
		return
	}
	m.finishOperation(m.pendingToggleOperation)
	m.pendingToggleOperation = 0
	m.pendingToggleReload = 0
	slog.Info("tui toggle authoritative reload completed", "generation", msg.generation, "healthy", !m.rowsStale)
	if !m.rowsStale {
		m.recordOperationSuccess(operationMutation)
		return
	}
	msg.driftAssessment = m.driftAssessment
	failure := snapshotAcceptanceError(msg)
	m.recordOperationFailure(operationMutation, diagnosticMutation, failure)
	m.pushToast(fmt.Sprintf("error: %v", failure), 5*time.Second)
}

func (m *Model) dismissInvalidConfigMutationPane() bool {
	if !m.configInvalid {
		return false
	}
	switch m.pane {
	case paneToggleModal, paneHarnessModal, paneInitModal, paneBulkToggle:
		m.pane = paneList
		return true
	case paneMarketplaceConsent:
		m.pane = paneList
		m.clearMarketplaceConsent()
		return true
	case paneConfirm:
		m.pane = paneList
		m.confirmMsg = ""
		m.quitAfterSync = false
		return true
	}
	return false
}

// finishUpdateReload resolves an update operation whose completion arrived
// after a newer snapshot superseded its reload generation. The operation
// stayed serialized and observed through that superseding reload; the
// accepted (or failed) snapshot is what now ends it. The update's own
// error, when it carried one, wins over the reload verdict so a failed
// update is never reported as success.
func (m *Model) finishUpdateReload(generation requestGeneration, reloadErr error) tea.Cmd {
	if m.updateReload == 0 || generation < m.updateReload {
		return nil
	}
	m.updateReload = 0
	pendingErr := m.updatePendingErr
	pendingWarnings := m.updatePendingWarnings
	m.updatePendingErr = nil
	m.updatePendingWarnings = nil
	m.syncing = false
	m.clearOperationProgress()
	// Match the direct updateDoneMsg path: an update never satisfies sync+quit.
	m.quitAfterSync = false
	switch {
	case pendingErr != nil:
		m.recordOperationFailure(operationUpdate, diagnosticUpdate, pendingErr)
		m.pushToast(fmt.Sprintf("update error: %v", pendingErr), 5*time.Second)
		return toastExpiryCmd(5 * time.Second)
	case reloadErr != nil:
		m.recordOperationFailure(operationUpdate, diagnosticUpdate, reloadErr)
		m.pushToast(fmt.Sprintf("update reload failed: %v", reloadErr), 5*time.Second)
		return toastExpiryCmd(5 * time.Second)
	default:
		m.recordOperationSuccess(operationUpdate)
		if len(pendingWarnings) > 0 {
			m.recordOperationWarnings(operationUpdate, diagnosticUpdate, pendingWarnings...)
			m.pushToast(fmt.Sprintf("Update complete with %d warning(s)", len(pendingWarnings)), 2*time.Second)
		} else {
			m.pushToast("Update complete", 2*time.Second)
		}
		return toastExpiryCmd(2 * time.Second)
	}
}

// finishSyncReload resolves a sync operation when its authoritative reload —
// or a newer superseding snapshot — is accepted or fails. The sync's own
// pipeline error, when it carried one, wins over the reload verdict so a
// failed sync is never reported as success.
func (m *Model) finishSyncReload(generation requestGeneration, reloadErr error) tea.Cmd {
	if m.syncReload == 0 || generation < m.syncReload {
		return nil
	}
	m.syncReload = 0
	pendingErr := m.syncPendingErr
	pendingWarnings := m.syncPendingWarnings
	m.syncPendingErr = nil
	m.syncPendingWarnings = nil
	m.syncing = false
	m.clearOperationProgress()
	// A sync+quit confirmed while the reload was already in flight still
	// quits once the authoritative reload lands; the verdict is carried to
	// Run() rather than toasted.
	if m.quitAfterSync {
		m.quitAfterSync = false
		m.syncQuitErr = pendingErr
		if m.syncQuitErr == nil {
			m.syncQuitErr = reloadErr
		}
		slog.Debug("sync reload complete, quitting as requested", "err", m.syncQuitErr)
		return tea.Quit
	}
	switch {
	case pendingErr != nil:
		m.recordOperationFailure(operationSync, diagnosticSync, pendingErr)
		m.pushToast(fmt.Sprintf("sync error: %v", pendingErr), 5*time.Second)
		slog.Error("sync failed", "err", pendingErr)
		recordControlledErrorSpan(m.ctx, "sync", pendingErr)
		return toastExpiryCmd(5 * time.Second)
	case reloadErr != nil:
		m.recordOperationFailure(operationSync, diagnosticSync, reloadErr)
		m.pushToast(fmt.Sprintf("sync reload failed: %v", reloadErr), 5*time.Second)
		return toastExpiryCmd(5 * time.Second)
	default:
		m.recordOperationSuccess(operationSync)
		if len(pendingWarnings) > 0 {
			m.recordOperationWarnings(operationSync, diagnosticSync, pendingWarnings...)
			m.pushToast(fmt.Sprintf("Sync complete with %d warning(s)", len(pendingWarnings)), 2*time.Second)
		} else {
			m.pushToast("Sync complete", 2*time.Second)
		}
		return toastExpiryCmd(2 * time.Second)
	}
}

func snapshotAcceptanceError(msg snapshotLoadedMsg) error {
	hardErrors := append([]error(nil), msg.errs...)
	hardErrors = append(hardErrors, diagnosticErrors(msg.diagnostics)...)
	if len(hardErrors) > 0 {
		return errors.Join(hardErrors...)
	}
	if msg.driftAssessment != driftReady {
		return errResourceStateUnverified
	}
	return nil
}

func (m *Model) applySnapshotData(msg snapshotLoadedMsg) {
	m.previewSel = previewSelection{}
	selection := m.captureRebuildAnchor()
	listOffset := m.listOffset
	selectionAfterDisable := m.selectionAfterDisable
	m.selectionAfterDisable = rebuildAnchor{}
	var previousDetail row
	hadPreviousDetail := false
	if m.pane == paneDetail && m.detailKey != "" {
		for _, r := range m.rows {
			if rowKey(r) == m.detailKey {
				previousDetail, hadPreviousDetail = r, true
				break
			}
		}
	}
	m.authoritativeRows = msg.rows
	m.provisionalRows = nil
	m.rows = msg.rows
	m.driftReport = msg.driftReport
	m.mergedCfg = msg.mergedCfg
	m.resolved = msg.resolved
	m.localExcludes = msg.localExcludes
	m.globalHarnesses = msg.globalHarnesses
	m.projectHarnesses = msg.projectHarnesses
	m.mergeDiscoveryRows()
	// The rebuild may have scrolled to where a toggled row moved (for example
	// a disabled row now in Available); keep the previous viewport and scroll
	// only as far as the final selection needs.
	m.listOffset = listOffset
	if !m.restoreAnchor(selectionAfterDisable) {
		m.restoreSelectionAfterRebuild(selection)
	}
	m.clampListOffset()
	m.rebuildOpenBundle(previousDetail, hadPreviousDetail)
	if msg.stage == snapshotStageFinal {
		m.pendingInventory.set(msg.inventoryResult)
		recordConfigSnapshotSpans(m.ctx, msg)
	}
}

// Update delegates to update and closes the palette whenever the pane
// leaves the list, so async snapshot and consent transitions the
// palette cannot intercept never resurrect the overlay or its input.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	if n, ok := next.(Model); ok && n.pane != paneList && n.palette.open {
		n.closePalette()
		return n, cmd
	}
	return next, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.invalidateStaleSelection()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		oldWidth, oldHeight := m.width, m.height
		detailAtBottom := m.pane == paneDetail && m.detailViewport.AtBottom()
		m.width = msg.Width
		m.height = msg.Height
		m.detailViewport.SetWidth(msg.Width)
		m.detailViewport.SetHeight(max(1, msg.Height-1))
		m.helpViewport.SetWidth(msg.Width)
		m.helpViewport.SetHeight(max(1, msg.Height-1))
		m.errorsViewport.SetWidth(msg.Width)
		m.errorsViewport.SetHeight(max(1, msg.Height-1))
		m.resizeMarketplaceConsentViewport(false)
		m.clampListOffset()
		m.refreshHarnessFormsForSize()
		layout := screenLayoutFor(m.height, m.searchRowVisible())
		listWidth, detailWidth := paneWidths(m.width)
		slog.Debug("tui resized", "old_width", oldWidth, "old_height", oldHeight, "width", m.width, "height", m.height, "list_width", listWidth, "detail_width", detailWidth, "visible_height", layout.mainHeight)
		m.previewSel = previewSelection{}
		if !m.belowMinimumTerminal() && m.pane == paneDetail && previewWidthFor(msg.Width) != m.detailRenderWidth {
			if rerender := m.rerenderDetailAtWidth(msg.Width); rerender != nil {
				m.detailStickyBottom = detailAtBottom
				return m, rerender
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		m.pendingClick = pendingClick{}
		next, cmd := m.handleKey(msg)
		if m.previewSel.active {
			if nm, ok := next.(Model); ok {
				nm.invalidateStaleSelection()
				return nm, cmd
			}
		}
		return next, cmd
	case previewSelectionMouseMsg:
		return m.handlePreviewSelection(msg)

	case tea.PasteMsg:
		return m.handlePaste(msg)

	case tea.BackgroundColorMsg:
		m.isDark = msg.IsDark()
		m.theme = newTheme(m.isDark)
		m.help.Styles = helpStyles(m.theme)
		m.searchInput.SetStyles(searchInputStyles(m.theme))
		return m, nil

	case cursor.BlinkMsg:
		// Forward cursor blink ticks to whichever text input is focused
		// (palette or search) so its cursor blinks.
		if m.palette.open {
			var cmd tea.Cmd
			m.palette.input, cmd = m.palette.input.Update(msg)
			return m, cmd
		}
		if m.filterActive {
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.MouseWheelMsg:
		m.pendingClick = pendingClick{}
		if m.belowMinimumTerminal() ||
			(msg.Button != tea.MouseWheelUp && msg.Button != tea.MouseWheelDown) {
			return m, nil
		}
		m.previewSel = previewSelection{}
		// Bubble's viewport treats Shift+vertical-wheel as horizontal
		// scrolling, but this TUI intentionally supports only vertical scroll.
		msg.Mod &^= tea.ModShift
		direction := 1
		if msg.Button == tea.MouseWheelUp {
			direction = -1
		}
		var cmd tea.Cmd
		switch m.pane {
		case paneList:
			m.moveCursorByRows(direction, 1)
		case paneDetail:
			m.detailStickyBottom = false
			if listWidth, _ := paneWidths(m.width); m.bundle != nil && msg.X < listWidth {
				return m.moveBundleCursor(direction)
			}
			m.detailViewport, cmd = m.detailViewport.Update(msg)
		case paneHelp:
			m.helpViewport.SetContent(renderHelp(m))
			m.helpViewport, cmd = m.helpViewport.Update(msg)
		case paneErrors:
			m.errorsViewport.SetContent(renderErrors(m))
			m.errorsViewport, cmd = m.errorsViewport.Update(msg)
		case paneMarketplaceConsent:
			m.marketplaceConsentViewport, cmd = m.marketplaceConsentViewport.Update(msg)
		}
		return m, cmd

	case snapshotLoadedMsg:
		if msg.stage == snapshotStageConfig {
			if m.requests.active(requestSnapshot) != msg.generation {
				return m, nil
			}
			if msg.provisionalCfg != nil {
				m.setProvisionalRows(msg.provisionalRows)
			}
			if msg.mergedCfg == nil {
				return m, waitSnapshotStages(msg.stages)
			}
			m.mergedCfg = msg.mergedCfg
			return m, tea.Batch(m.beginMarketplaceDiscovery(m.filter, false), waitSnapshotStages(msg.stages))
		}
		if msg.stage == snapshotStageInventory {
			if m.requests.active(requestSnapshot) != msg.generation {
				return m, nil
			}
			m.loading = false
			m.assessmentPending = true
			m.applySnapshotStatus(msg)
			m.applySnapshotData(msg)
			return m, waitSnapshotStages(msg.stages)
		}
		if !m.acceptsRequest(requestSnapshot, msg.generation) {
			return m, nil
		}
		m.loading = false
		m.assessmentPending = false
		m.applySnapshotStatus(msg)
		updateCmd := m.finishUpdateReload(msg.generation, snapshotAcceptanceError(msg))
		syncCmd := m.finishSyncReload(msg.generation, snapshotAcceptanceError(msg))
		if msg.mergedCfg == nil && len(m.errs) > 0 {
			m.provisionalRows = nil
			m.refreshing = false
			m.clearMarketplaceDiscovery(true)
			return m, tea.Batch(m.finishMarketplaceInstallReload(msg.generation, errors.Join(m.errs...)), updateCmd, syncCmd)
		}
		m.applySnapshotData(msg)
		discoveryCmd := m.beginMarketplaceDiscovery(m.filter, false)
		installCmd := m.finishMarketplaceInstallReload(msg.generation, snapshotAcceptanceError(msg))
		if msg.refreshAfterLoad {
			m.refreshing = true
			generation := m.beginRequest(requestSnapshot)
			return m, tea.Batch(refreshSnapshot(ctxOrBackground(m.ctx), m.workspace, m.homeDir, generation), discoveryCmd, installCmd, updateCmd, syncCmd, m.spinner.Tick)
		}
		m.refreshing = false
		return m, tea.Batch(discoveryCmd, installCmd, updateCmd, syncCmd)

	case refreshLoadedMsg:
		if !m.acceptsRequest(requestSnapshot, msg.generation) {
			return m, nil
		}
		m.refreshing = false
		m.assessmentPending = false
		m.applySnapshotStatus(msg.snapshotLoadedMsg)
		updateCmd := m.finishUpdateReload(msg.generation, snapshotAcceptanceError(msg.snapshotLoadedMsg))
		syncCmd := m.finishSyncReload(msg.generation, snapshotAcceptanceError(msg.snapshotLoadedMsg))
		if msg.mergedCfg == nil && len(m.errs) > 0 {
			m.provisionalRows = nil
			m.clearMarketplaceDiscovery(true)
			return m, tea.Batch(m.finishMarketplaceInstallReload(msg.generation, errors.Join(m.errs...)), updateCmd, syncCmd)
		}
		m.applySnapshotData(msg.snapshotLoadedMsg)
		return m, tea.Batch(m.beginMarketplaceDiscovery(m.filter, false), m.finishMarketplaceInstallReload(msg.generation, snapshotAcceptanceError(msg.snapshotLoadedMsg)), updateCmd, syncCmd)

	case reloadMsg:
		snapshot := snapshotLoadedMsg(msg)
		if !m.acceptsRequest(requestSnapshot, snapshot.generation) {
			// Update may have changed disk after a newer snapshot started. Its
			// stale delivery cannot be applied, so schedule a current snapshot
			// rather than leaving either result as authoritative.
			if m.syncReload == 0 {
				m.syncing = false
			}
			generation := m.beginRequest(requestSnapshot)
			return m, loadSnapshot(ctxOrBackground(m.ctx), m.workspace, m.homeDir, generation)
		}
		m.loading = false
		m.assessmentPending = false
		if m.syncReload == 0 {
			m.syncing = false
		}
		m.refreshing = false
		m.applySnapshotStatus(snapshot)
		updateCmd := m.finishUpdateReload(snapshot.generation, snapshotAcceptanceError(snapshot))
		syncCmd := m.finishSyncReload(snapshot.generation, snapshotAcceptanceError(snapshot))
		if snapshot.mergedCfg == nil && len(m.errs) > 0 {
			m.provisionalRows = nil
			m.clearMarketplaceDiscovery(true)
			return m, tea.Batch(m.finishMarketplaceInstallReload(snapshot.generation, errors.Join(m.errs...)), updateCmd, syncCmd)
		}
		m.applySnapshotData(snapshot)
		m.pushToast("Reloaded", 2*time.Second)
		return m, tea.Batch(toastExpiryCmd(2*time.Second), m.beginMarketplaceDiscovery(m.filter, false), m.finishMarketplaceInstallReload(snapshot.generation, snapshotAcceptanceError(snapshot)), updateCmd, syncCmd)

	case discoveryDebounceMsg:
		if m.requests.active(requestDiscovery) != msg.generation {
			return m, nil
		}
		ctx, cancel := context.WithCancel(ctxOrBackground(m.ctx))
		m.discoveryCancel = cancel
		return m, loadMarketplaceDiscovery(ctx, m.mergedCfg, msg.query, msg.generation)

	case discoveryLoadedMsg:
		if msg.done {
			if !m.acceptsRequest(requestDiscovery, msg.generation) {
				return m, nil
			}
			if m.discoveryCancel != nil {
				m.discoveryCancel()
				m.discoveryCancel = nil
			}
			m.discoveryLoading = false
			if msg.err != nil {
				m.discoveryErrs = []error{msg.err}
				return m, nil
			}
			if msg.outcomes != nil {
				m.applyDiscovery(msg.query, msg.outcomes)
			}
			return m, nil
		}
		if m.requests.active(requestDiscovery) != msg.generation || msg.sequence == 0 || msg.sequence <= m.discoverySequence {
			return m, nil
		}
		m.discoverySequence = msg.sequence
		m.applyDiscovery(msg.query, msg.outcomes)
		return m, waitMarketplaceDiscoveryUpdate(msg.generation, msg.sequence+1, msg.query, msg.updates)

	case discoveryInstallMsg:
		if !m.acceptsOperation(msg.operation) {
			return m, nil
		}
		if msg.err == nil && msg.prepared != nil && msg.result == nil {
			m.marketplaceInstallOptions = msg.options
			m.marketplaceInstallOperation = msg.operation
			m.marketplacePreparedInstall = msg.prepared
			if !msg.prepared.RequiresRiskAcceptance() {
				return m, commitDiscoveryInstall(m.observedContext(ctxOrBackground(m.ctx)), msg.operation, msg.options, msg.prepared)
			}
			m.setMarketplaceConsentPreview(msg.preview)
			m.pane = paneMarketplaceConsent
			if m.progress != nil {
				m.operationPhase = operationPhaseConsent
			}
			return m, nil
		}
		var consent *pipeline.MarketplaceConsentError
		if errors.As(msg.err, &consent) {
			m.marketplaceInstallOptions = msg.options
			m.marketplaceInstallOperation = msg.operation
			m.marketplacePreparedInstall = consent.Prepared
			m.setMarketplaceConsentPreview(consent.Preview)
			m.pane = paneMarketplaceConsent
			if m.progress != nil {
				m.operationPhase = operationPhaseConsent
			}
			return m, nil
		}
		m.marketplacePreparedInstall = nil
		m.marketplaceInstallPreview = ""
		if msg.err != nil {
			m.clearOperationProgress()
			m.finishOperation(msg.operation)
			m.marketplaceInstallOperation = 0
			m.recordOperationFailure(operationMutation, diagnosticMutation, msg.err)
			m.pushToast("Marketplace install failed: "+msg.err.Error(), 5*time.Second)
			return m, toastExpiryCmd(5 * time.Second)
		}
		generation := m.beginRequest(requestSnapshot)
		m.marketplaceInstallReload = generation
		if m.progress != nil {
			m.operationPhase = string(pipeline.PhaseReload)
		}
		return m, loadSnapshot(ctxOrBackground(m.ctx), m.workspace, m.homeDir, generation)

	case operationProgressMsg:
		if !m.applyOperationProgress(msg) {
			return m, nil
		}
		return m, m.operationProgressCmd()

	case operationProgressDoneMsg:
		return m, nil

	case operationProgressPollMsg:
		// Idle wake: keep the pump alive only while its own source is the
		// active observation; a stale wake must not spawn a second pump.
		if m.progress == nil || msg.source != m.progress {
			return m, nil
		}
		return m, m.operationProgressCmd()

	case toastMsg:
		return m, m.enqueueToast(msg.text, msg.duration)

	case toastExpiredMsg:
		// Clear the toast if it has expired. Check the expiry time to
		// avoid clearing a toast that was replaced more recently.
		return m, m.expireToast()

	case configWrittenMsg:
		if !m.acceptsOperation(msg.operation) {
			return m, nil
		}
		if msg.err != nil {
			m.finishOperation(msg.operation)
			m.recordOperationFailure(operationMutation, diagnosticMutation, msg.err)
			m.pendingExcludeAction = nil
			m.pushToast(fmt.Sprintf("error: %v", msg.err), 5*time.Second)
			slog.Error("config write failed", "resource", msg.skillName, "enabled", msg.enabled, "err", msg.err)
			action := "remove"
			if msg.enabled {
				action = "install"
			}
			recordControlledErrorSpan(m.ctx, action, msg.err)
			return m, toastExpiryCmd(5 * time.Second)
		}
		action := "Enabled"
		if !msg.enabled {
			action = "Disabled"
		}
		m.pushToast(fmt.Sprintf("%s %s", action, msg.skillName), 2*time.Second)
		m.refreshing = false
		m.driftAssessment = driftUnknown
		if msg.selectNext {
			m.selectionAfterDisable = m.nextRowSelection()
		}
		// If a collision-aware enable was pending, emit the exclude
		// command now that the primary toggle has succeeded.
		if !msg.followUp && msg.enabled && m.pendingExcludeAction != nil {
			act := *m.pendingExcludeAction
			m.pendingExcludeAction = nil
			if followUp := excludeCompetingRegistriesCmd(m.repoRoot, m.rows, act); followUp != nil {
				exclCmd := correlateOperation(followUp, msg.operation, true)
				return m, tea.Batch(exclCmd, toastExpiryCmd(2*time.Second))
			}
		}
		m.pendingExcludeAction = nil
		// Re-load snapshot to reflect the config change.
		generation := m.beginRequest(requestSnapshot)
		m.pendingToggleOperation = msg.operation
		m.pendingToggleReload = generation
		return m, tea.Batch(loadSnapshot(ctxOrBackground(m.ctx), m.workspace, m.homeDir, generation), toastExpiryCmd(2*time.Second))

	case harnessWrittenMsg:
		if !m.acceptsOperation(msg.operation) {
			return m, nil
		}
		m.finishOperation(msg.operation)
		if msg.err != nil {
			m.recordOperationFailure(operationTargets, diagnosticMutation, msg.err)
			m.pushToast(fmt.Sprintf("error: %v", msg.err), 5*time.Second)
			slog.Error("harness configuration write failed", "scope", msg.scopeLabel, "err", msg.err)
			recordControlledErrorSpan(m.ctx, "harness", msg.err)
			return m, toastExpiryCmd(5 * time.Second)
		}
		m.recordOperationSuccess(operationTargets)
		m.pushToast(fmt.Sprintf("Saved %s harnesses", msg.scopeLabel), 2*time.Second)
		m.refreshing = false
		generation := m.beginRequest(requestSnapshot)
		return m, tea.Batch(loadSnapshot(ctxOrBackground(m.ctx), m.workspace, m.homeDir, generation), toastExpiryCmd(2*time.Second))

	case syncDoneMsg:
		m.refreshing = false
		// If the user confirmed "sync+quit", quit now that the sync has
		// actually completed. This avoids the race where tea.Quit in a
		// tea.Batch kills the program before the sync goroutine finishes.
		if m.quitAfterSync {
			m.syncing = false
			m.quitAfterSync = false
			m.clearOperationProgress()
			slog.Debug("sync complete, quitting as requested", "err", msg.err)
			m.syncQuitErr = msg.err
			return m, tea.Quit
		}
		// Completion is not the verdict: the sync stays serialized and
		// observed through the authoritative reload it schedules here; the
		// accepted (or failed) snapshot ends it. The pipeline's own error
		// is retained so a failed sync can never be reported as success.
		m.syncPendingErr = msg.err
		m.syncPendingWarnings = append([]error(nil), msg.warnings...)
		if m.progress != nil {
			m.operationPhase = string(pipeline.PhaseReload)
		}
		generation := m.beginRequest(requestSnapshot)
		m.syncReload = generation
		return m, loadSnapshot(ctxOrBackground(m.ctx), m.workspace, m.homeDir, generation)

	case updateDoneMsg:
		if !m.acceptsRequest(requestSnapshot, msg.generation) {
			// A failed update can still leave partial on-disk effects. Re-read
			// them under a new generation without applying this stale outcome.
			// The operation stays active — serialization and phase observation
			// end only when the superseding reload is accepted or fails.
			m.updatePendingErr = msg.err
			m.updatePendingWarnings = append([]error(nil), msg.warnings...)
			if m.progress != nil {
				m.operationPhase = string(pipeline.PhaseReload)
			}
			generation := m.beginRequest(requestSnapshot)
			m.updateReload = generation
			return m, loadSnapshot(ctxOrBackground(m.ctx), m.workspace, m.homeDir, generation)
		}
		m.syncing = false
		m.clearOperationProgress()
		var installCmd tea.Cmd
		if msg.snapshot.generation != 0 {
			m.applySnapshotStatus(msg.snapshot)
			m.applySnapshotData(msg.snapshot)
			if msg.snapshot.mergedCfg == nil && len(m.errs) > 0 {
				m.clearMarketplaceDiscovery(true)
			}
		}
		// Clear quitAfterSync so a stale flag from a previous sync+quit
		// confirm doesn't cause a later unrelated sync to quit.
		m.quitAfterSync = false
		if msg.err != nil {
			m.recordOperationFailure(operationUpdate, diagnosticUpdate, msg.err)
			m.pushToast(fmt.Sprintf("update error: %v", msg.err), 5*time.Second)
			slog.Error("update failed", "err", msg.err)
			recordControlledErrorSpan(m.ctx, "update", msg.err)
			if msg.snapshot.generation != 0 {
				installCmd = m.finishMarketplaceInstallReload(msg.generation, snapshotAcceptanceError(msg.snapshot))
			}
			return m, tea.Batch(toastExpiryCmd(5*time.Second), installCmd)
		} else {
			m.recordOperationSuccess(operationUpdate)
			if len(msg.warnings) > 0 {
				m.pushToast(fmt.Sprintf("Update complete with %d warning(s)", len(msg.warnings)), 2*time.Second)
			} else {
				m.pushToast("Update complete", 2*time.Second)
			}
		}
		if msg.snapshot.generation != 0 {
			installCmd = m.finishMarketplaceInstallReload(msg.generation, snapshotAcceptanceError(msg.snapshot))
		}
		return m, tea.Batch(toastExpiryCmd(2*time.Second), installCmd)

	case editConfigPathMsg:
		if !m.acceptsOperation(msg.operation) {
			return m, nil
		}
		m.finishOperation(msg.operation)
		if msg.err != nil {
			m.recordOperationFailure(operationEditor, diagnosticEditor, msg.err)
			m.pushToast(fmt.Sprintf("editor error: %v", msg.err), 5*time.Second)
			slog.Error("config editor result handled", "path", msg.path, "reload", false, "err", msg.err)
			recordControlledErrorSpan(m.ctx, "edit_config", msg.err)
			return m, toastExpiryCmd(5 * time.Second)
		}
		m.recordOperationSuccess(operationEditor)
		slog.Debug("config editor result handled", "path", msg.path, "reload", true)
		generation := m.beginRequest(requestSnapshot)
		return m, loadSnapshot(ctxOrBackground(m.ctx), m.workspace, m.homeDir, generation)

	case marketplaceDetailMsg:
		if m.pane != paneDetail || msg.key == "" || msg.key != m.detailKey || !m.acceptsRequest(requestDetail, msg.generation) {
			return m, nil
		}
		m.previewSel = previewSelection{}
		if msg.err != nil {
			m.bundle = nil
			m.detailSelectedPath = ""
			m.detailRenderWidth = m.previewWidth()
			m.detailStickyBottom = false
			m.detailViewport.SetContent(renderMarketplaceInspectionError(msg.err))
			m.detailViewport.GotoTop()
			return m, nil
		}
		m.bundle = msg.bundle
		m.detailLegacyRender = false
		m.detailSelectedPath = msg.bundle.selectedPath()
		m.ensureBundleCursorVisible()
		if msg.width > 0 && msg.width != m.previewWidth() {
			if rerender := m.rerenderDetailAtWidth(m.width); rerender != nil {
				return m, rerender
			}
		}
		renderedWidth := msg.width
		if renderedWidth <= 0 {
			renderedWidth = m.previewWidth()
		}
		m.detailRenderWidth = renderedWidth
		m.detailStickyBottom = false
		m.detailViewport.SetContent(msg.content)
		m.detailViewport.GotoTop()
		return m, nil

	case detailContentMsg:
		if m.pane != paneDetail || (msg.path != "" && msg.path != m.detailSelectedPath) || !m.acceptsRequest(requestDetail, msg.generation) {
			return m, nil
		}
		m.previewSel = previewSelection{}
		if msg.path == "" {
			m.detailLegacyRender = true
		}
		if !m.detailLegacyRender && msg.width > 0 && msg.width != m.previewWidth() {
			if rerender := m.rerenderDetailAtWidth(m.width); rerender != nil {
				return m, rerender
			}
		}
		renderedWidth := msg.width
		if renderedWidth == 0 {
			renderedWidth = m.width
		}
		m.detailRenderWidth = renderedWidth
		if msg.keepScroll {
			offset := m.detailViewport.YOffset()
			stickyBottom := m.detailStickyBottom || m.detailViewport.AtBottom()
			m.detailStickyBottom = false
			m.detailViewport.SetContent(msg.content)
			if stickyBottom {
				m.detailViewport.GotoBottom()
			} else {
				m.detailViewport.SetYOffset(offset)
			}
			return m, nil
		}
		m.detailStickyBottom = false
		m.detailViewport.SetContent(msg.content)
		m.detailViewport.GotoTop()
		return m, nil

	case snapshotStreamClosedMsg:
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		// Only keep ticking while there is in-flight work.
		if m.loading || m.assessmentPending || m.syncing || m.refreshing || m.discoveryLoading {
			return m, cmd
		}
		return m, nil
	}

	// Forward any unhandled messages (e.g. cursor.initialBlinkMsg from
	// the search input's Focus command) to the search input so its
	// cursor keeps blinking while the filter is active.
	if m.filterActive {
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) moveCursorByHalfPage(direction int) {
	m.moveCursorByRows(direction, max(1, m.listVisibleHeight()/2))
}

func (m *Model) moveCursorByPage(direction int) {
	m.moveCursorByRows(direction, m.listVisibleHeight())
}

func (m *Model) moveCursorByRows(direction, step int) {
	if len(m.flatItems) == 0 {
		return
	}
	m.cursor = min(max(0, m.cursor), len(m.flatItems)-1)
	listWidth, _ := paneWidths(m.width)
	heights := m.listHeightOffsets(listWidth)
	target := heights[m.cursor] + direction*max(1, step)
	if target <= 0 {
		m.cursor = 0
	} else if target >= heights[len(m.flatItems)] {
		m.cursor = len(m.flatItems) - 1
	} else {
		candidate := sort.Search(len(m.flatItems), func(i int) bool {
			return heights[i] > target
		}) - 1
		if direction > 0 {
			candidate = max(m.cursor+1, candidate)
		} else {
			candidate = min(m.cursor-1, candidate)
		}
		m.cursor = min(max(0, candidate), len(m.flatItems)-1)
	}
	m.clampListOffset()
}

func (m Model) handleViewportPageKey(msg tea.KeyPressMsg, viewport *viewport.Model) bool {
	if key.Matches(msg, m.keys.PageDown) {
		viewport.PageDown()
		return true
	}
	if key.Matches(msg, m.keys.PageUp) {
		viewport.PageUp()
		return true
	}
	return false
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Toast auto-expire check
	if !m.toastExp.IsZero() && time.Now().After(m.toastExp) {
		m.expireToast()
	}

	// Ctrl+C always quits immediately regardless of the active pane.
	// This ensures the user can escape the TUI even from overlays or
	// modals. Unlike the 'q' key, Ctrl+C does not prompt for unsaved
	// changes — it is a hard exit that restores the terminal cleanly.
	if msg.Code == 'c' && msg.Mod.Contains(tea.ModCtrl) {
		return m, tea.Quit
	}
	if m.previewSel.active && msg.Code == tea.KeyEscape {
		m.previewSel = previewSelection{}
		return m, nil
	}
	if m.belowMinimumTerminal() {
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
		return m, nil
	}

	if m.dismissInvalidConfigMutationPane() {
		return m, invalidConfigToastCmd()
	}

	// Overlays take priority
	switch m.pane {
	case paneHelp:
		if key.Matches(msg, m.keys.Help) || key.Matches(msg, m.keys.Clear) {
			m.pane = paneList
			return m, nil
		}
		m.helpViewport.SetContent(renderHelp(m))
		if key.Matches(msg, m.keys.Top) {
			m.helpViewport.GotoTop()
			return m, nil
		}
		if key.Matches(msg, m.keys.Bottom) {
			m.helpViewport.GotoBottom()
			return m, nil
		}
		if m.handleViewportPageKey(msg, &m.helpViewport) {
			return m, nil
		}
		var cmd tea.Cmd
		m.helpViewport, cmd = m.helpViewport.Update(msg)
		return m, cmd

	case paneDetail:
		return m.handleDetailKey(msg)

	case paneErrors:
		if key.Matches(msg, m.keys.Clear) || key.Matches(msg, m.keys.Errors) {
			m.pane = paneList
			return m, nil
		}
		m.errorsViewport.SetContent(renderErrors(m))
		if key.Matches(msg, m.keys.Top) {
			m.errorsViewport.GotoTop()
			return m, nil
		}
		if key.Matches(msg, m.keys.Bottom) {
			m.errorsViewport.GotoBottom()
			return m, nil
		}
		if m.handleViewportPageKey(msg, &m.errorsViewport) {
			return m, nil
		}
		var cmd tea.Cmd
		m.errorsViewport, cmd = m.errorsViewport.Update(msg)
		return m, cmd

	case paneConfirm:
		return m.handleConfirmKey(msg)

	case paneMarketplaceConsent:
		return m.handleMarketplaceConsentKey(msg)

	case paneToggleModal:
		return m.handleToggleModalKey(msg)

	case paneInitModal:
		return m.handleInitModalKey(msg)
	case paneHarnessModal:
		return m.handleHarnessModalKey(msg)
	case paneEditConfigModal:
		return m.handleEditConfigModalKey(msg)
	case paneViewSettings:
		return m.handleViewSettingsKey(msg)
	case paneBulkToggle:
		return m.handleBulkToggleKey(msg)
	}

	// The command palette owns all keys while open (its input takes
	// typing and paste; esc closes; enter dispatches).
	if m.palette.open {
		return m.handlePaletteKey(msg)
	}

	// Search mode
	if m.filterActive {
		return m.handleSearchKey(msg)
	}

	// z-prefix: vim-style fold commands (za/zM/zR). Any other key cancels
	// the pending prefix and is processed normally — except the palette
	// opener, which a pending prefix swallows rather than layering over.
	if m.zPending {
		m.zPending = false
		switch {
		case key.Matches(msg, m.keys.FoldToggle):
			return m.dispatchAction(actionFoldToggle)
		case key.Matches(msg, m.keys.FoldAll):
			return m.dispatchAction(actionFoldAll)
		case key.Matches(msg, m.keys.ExpandAll):
			return m.dispatchAction(actionExpandAll)
		case key.Matches(msg, m.keys.Palette):
			return m, nil
		default:
			// fall through: treat the key as a fresh input
		}
	}

	// Normal mode
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.dispatchAction(actionQuit)

	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
			m.clampListOffset()
		}
		return m, nil

	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.flatItems)-1 {
			m.cursor++
			m.clampListOffset()
		}
		return m, nil

	case key.Matches(msg, m.keys.HalfPageDown):
		m.moveCursorByHalfPage(1)
		return m, nil

	case key.Matches(msg, m.keys.HalfPageUp):
		m.moveCursorByHalfPage(-1)
		return m, nil

	case key.Matches(msg, m.keys.PageDown):
		m.moveCursorByPage(1)
		return m, nil

	case key.Matches(msg, m.keys.PageUp):
		m.moveCursorByPage(-1)
		return m, nil

	case key.Matches(msg, m.keys.Top):
		m.cursor = 0
		m.clampListOffset()
		return m, nil

	case key.Matches(msg, m.keys.Bottom):
		m.cursor = max(0, len(m.flatItems)-1)
		m.clampListOffset()
		return m, nil

	case key.Matches(msg, m.keys.Search):
		return m.dispatchAction(actionSearch)

	case key.Matches(msg, m.keys.Clear) && m.filter != "":
		return m.dispatchAction(actionClearSearch)

	case key.Matches(msg, m.keys.Palette):
		return m, m.openPalette()

	case key.Matches(msg, m.keys.Help):
		return m.dispatchAction(actionHelp)

	case key.Matches(msg, m.keys.Errors):
		return m.dispatchAction(actionDiagnostics)

	case key.Matches(msg, m.keys.Harness):
		return m.dispatchAction(actionHarness)

	case key.Matches(msg, m.keys.EditConfig):
		return m.dispatchAction(actionEditConfig)

	case key.Matches(msg, m.keys.View):
		return m.dispatchAction(actionViewSettings)

	case key.Matches(msg, m.keys.Enter):
		return m.dispatchAction(actionDetail)

	case key.Matches(msg, m.keys.Collapse):
		m.setFoldAtCursor(true)
		m.rebuildFlatItems()
		m.clampListOffset()
		return m, nil

	case key.Matches(msg, m.keys.Expand):
		m.setFoldAtCursor(false)
		m.rebuildFlatItems()
		m.clampListOffset()
		return m, nil

	case key.Matches(msg, m.keys.FoldPrefix):
		// Start vim-style fold prefix; the next key (a/M/R) completes it.
		m.zPending = true
		return m, nil

	case key.Matches(msg, m.keys.Reload):
		return m.dispatchAction(actionReload)

	case key.Matches(msg, m.keys.Space):
		return m.dispatchAction(actionToggle)

	case key.Matches(msg, m.keys.Sync):
		return m.dispatchAction(actionSync)

	case key.Matches(msg, m.keys.Update):
		return m.dispatchAction(actionUpdate)
	}

	return m, nil
}

// dispatchAction performs one list action by stable identity. It is the
// single guarded path shared by physical key bindings, z-prefix fold
// completions, and command-palette submissions — the palette can therefore
// never reach behavior the keyboard path guards against, and prefix actions
// dispatch the identity directly rather than synthesizing key sequences.
func (m Model) dispatchAction(id paletteActionID) (tea.Model, tea.Cmd) {
	switch id {
	case actionQuit:
		if m.driftReport.PendingCount() > 0 {
			m.pane = paneConfirm
			m.confirmMsg = fmt.Sprintf("%d pending changes not synced. Sync+quit?", m.driftReport.PendingCount())
			return m, nil
		}
		return m, tea.Quit

	case actionSearch:
		// Resume editing an applied filter instead of discarding it.
		m.filterActive = true
		m.searchInput.SetValue(m.filter)
		m.searchInput.CursorEnd()
		return m, m.searchInput.Focus()

	case actionClearSearch:
		return m.clearSearch()

	case actionHelp:
		m.pane = paneHelp
		return m, nil

	case actionDiagnostics:
		if m.hasDiagnostics() {
			m.pane = paneErrors
		}
		return m, nil

	case actionHarness:
		if m.configInvalid {
			return m, invalidConfigToastCmd()
		}
		m.openHarnessModal()
		return m, nil

	case actionEditConfig:
		m.pane = paneEditConfigModal
		return m, nil

	case actionDetail:
		if _, ok := m.selectedSectionPath(); ok {
			m.toggleFoldAtCursor()
			m.rebuildFlatItems()
			m.clampListOffset()
			return m, nil
		}
		if r, ok := m.selectedRow(); ok {
			m.enterDetailPane()
			generation := m.beginRequest(requestDetail)
			recordActionSpan(m.ctx, "browse", map[string]string{"view": "detail", "resource.kind": r.Artifact.Kind.String()})
			if m.bundle != nil && m.detailSelectedPath != "" {
				return m, loadBundleDetailContent(ctxOrBackground(m.ctx), m.currentTheme(), m.bundle, m.detailSelectedPath, generation, m.previewWidth(), false)
			}
			if marketplaceInspectableSkill(r) {
				return m, loadMarketplaceSkillDetail(m.observedContext(ctxOrBackground(m.ctx)), m.currentTheme(), r, generation, m.previewWidth())
			}
			return m, nil
		}
		return m, nil

	case actionToggle:
		if m.syncing {
			return m, nil
		}
		// Space on a section header: open the confirmed bulk toggle modal.
		if path, ok := m.selectedSectionPath(); ok {
			if m.configInvalid {
				return m, invalidConfigToastCmd()
			}
			m.openBulkModal(path)
			return m, nil
		}
		// Space on a skill row: open the toggle modal so the user picks
		// which config file to write.
		r, ok := m.selectedRow()
		if !ok {
			return m, nil
		}
		return m.applyRowSpaceEligibility(r)

	case actionViewSettings:
		m.openViewSettingsModal()
		return m, nil

	case actionToggleSection:
		if m.loading || m.syncing {
			return m, nil
		}
		if m.configInvalid {
			return m, invalidConfigToastCmd()
		}
		if path, ok := m.selectedSectionPath(); ok {
			m.openBulkModal(path)
		}
		return m, nil

	case actionFoldToggle:
		m.toggleFoldAtCursor()
		m.rebuildFlatItems()
		m.clampListOffset()
		return m, nil

	case actionFoldAll:
		m.collapseAllSections()
		m.rebuildFlatItems()
		m.cursor = 0
		m.clampListOffset()
		return m, nil

	case actionExpandAll:
		m.expandAllSections()
		m.rebuildFlatItems()
		m.clampListOffset()
		return m, nil

	case actionReload:
		m.refreshing = false
		generation := m.beginRequest(requestSnapshot)
		return m, loadSnapshot(ctxOrBackground(m.ctx), m.workspace, m.homeDir, generation)

	case actionSync:
		if m.configInvalid {
			return m, invalidConfigToastCmd()
		}
		if m.loading || m.syncing {
			return m, nil
		}
		if m.driftAssessment == driftReady && m.driftReport.InSync() {
			recordActionSpan(m.ctx, "sync", map[string]string{"skipped": "in_sync"})
			return m, func() tea.Msg {
				return toastMsg{text: "already in sync", duration: 2 * time.Second}
			}
		}
		m.refreshing = false
		// Sync does not allocate a snapshot generation until syncDoneMsg
		// actually schedules the authoritative post-sync reload.
		m.syncing = true
		m.beginOperationProgress("sync", m.requests.active(requestSnapshot))
		recordActionSpan(m.ctx, "sync", nil)
		return m, tea.Batch(m.dependencySyncCmd(), m.operationProgressCmd(), m.spinner.Tick)

	case actionUpdate:
		if m.configInvalid {
			return m, invalidConfigToastCmd()
		}
		if m.loading || m.syncing {
			return m, nil
		}
		m.refreshing = false
		generation := m.beginRequest(requestSnapshot)
		m.syncing = true
		m.beginOperationProgress("update", generation)
		recordActionSpan(m.ctx, "update", nil)
		return m, tea.Batch(m.dependencyUpdateCmd(generation), m.operationProgressCmd(), m.spinner.Tick)
	}

	return m, nil
}

func (m *Model) restoreBrowseDiscovery() {
	if m.authoritativeRows != nil {
		m.discoveryRows = slices.Clone(m.browseDiscoveryRows)
		m.mergeDiscoveryRows()
	} else {
		m.recomputeView()
	}
}

func (m Model) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if m.belowMinimumTerminal() || m.pane != paneList {
		return m, nil
	}
	if m.palette.open {
		return m.handlePalettePaste(msg)
	}
	if !m.filterActive {
		return m, nil
	}
	return m.applySearchInput(msg)
}

func (m Model) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Clear):
		return m.clearSearch()

	case key.Matches(msg, m.keys.SearchConfirm):
		m.filterActive = false
		m.searchInput.Blur()
		recordActionSpan(m.ctx, "filter", map[string]string{"result_count": strconv.Itoa(len(m.filteredRows))})
		return m, nil

	default:
		// Delegate to textinput for cursor movement, backspace,
		// and character input.
		return m.applySearchInput(msg)
	}
}

// clearSearch drops the query, whether it is being typed or already
// applied, and restores unfiltered browsing and discovery.
func (m Model) clearSearch() (tea.Model, tea.Cmd) {
	m.filterActive = false
	m.filter = ""
	m.searchInput.SetValue("")
	m.searchInput.Blur()
	m.restoreBrowseDiscovery()
	// Rows hidden by the filter reappear above the selection; scroll as
	// little as possible so they are visible instead of kept off-screen.
	m.listOffset = 0
	m.clampListOffset()
	recordActionSpan(m.ctx, "filter", map[string]string{"result_count": strconv.Itoa(len(m.filteredRows))})
	return m, m.beginMarketplaceDiscovery("", false)
}

func (m Model) applySearchInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	previous := m.filter
	m.searchInput, cmd = m.searchInput.Update(msg)
	m.filter = m.searchInput.Value()
	m.recomputeView()
	if m.filter == previous {
		return m, cmd
	}
	if m.filter == "" {
		m.restoreBrowseDiscovery()
		return m, tea.Batch(cmd, m.beginMarketplaceDiscovery("", false), m.spinner.Tick)
	}
	return m, tea.Batch(cmd, m.beginMarketplaceDiscovery(m.filter, true), m.spinner.Tick)
}

func (m *Model) rebuildOpenBundle(previous row, hadPrevious bool) {
	if m.pane != paneDetail || m.detailKey == "" {
		return
	}
	var current row
	found := false
	for _, r := range m.rows {
		if rowKey(r) == m.detailKey {
			current, found = r, true
			break
		}
	}
	if !found && hadPrevious {
		identity := registryIdentityKey(previous.RegistryURL, previous.RegistryRef, previous.RegistryPath)
		matches := 0
		for _, r := range m.rows {
			if r.UnavailableReason == "" && !previous.PendingVerification {
				continue
			}
			if r.PendingVerification {
				continue
			}
			if r.Artifact.Kind != previous.Artifact.Kind || r.Artifact.Name != previous.Artifact.Name {
				continue
			}
			if registryIdentityKey(r.RegistryURL, r.RegistryRef, r.RegistryPath) != identity {
				continue
			}
			current = r
			matches++
		}
		found = matches == 1
	}
	if !found && hadPrevious {
		current, found = uniqueRowByIdentity(m.rows, rowSelectionIdentity(previous))
	}
	if !found {
		m.detailKey = ""
		m.bundle = nil
		m.detailSelectedPath = ""
		m.cancelRequest(requestDetail)
		m.detailViewport.SetContent("")
		return
	}
	m.detailKey = rowKey(current)
	if current.UnavailableReason != "" {
		m.bundle = nil
		m.detailSelectedPath = ""
		m.cancelRequest(requestDetail)
		m.detailViewport.SetContent(renderDetailFullContent(m.currentTheme(), current, m.previewWidth()))
		return
	}
	if isMarketplaceDetailOnly(current) {
		if m.bundle != nil && m.bundle.inspection != nil {
			m.ensureBundleCursorVisible()
			return
		}
		if marketplaceInspectableSkill(current) && m.requests.active(requestDetail) != 0 {
			return
		}
		m.bundle = nil
		m.detailSelectedPath = ""
		m.cancelRequest(requestDetail)
		m.detailViewport.SetContent(renderMarketplaceDetailContent(m.currentTheme(), current))
		return
	}
	tree, err := bundleTreeForRow(current)
	if err != nil {
		m.bundle = nil
		m.detailSelectedPath = ""
		m.cancelRequest(requestDetail)
		m.detailViewport.SetContent("Error reading resource: " + sanitizeTerminalData(err.Error(), terminalSingleLine))
		return
	}
	selected := m.detailSelectedPath
	if selected != "" && tree.restoreSelection(selected) {
		m.bundle = tree
		m.detailSelectedPath = selected
		m.ensureBundleCursorVisible()
		return
	}
	selected = tree.selectedPath()
	tree.selected = selected
	tree.rebuild()
	m.bundle = tree
	m.detailSelectedPath = selected
	m.ensureBundleCursorVisible()
}

// toggleDenial identifies why the guarded row toggle cannot proceed, in the
// order the keyboard Space dispatch reports it. toggleEligible means the
// toggle modal may open. This is the single authoritative toggle policy:
// the keyboard path maps each denial to its toast/no-op outcome and the
// palette maps any denial to hiding the toggle action.
type toggleDenial int

const (
	toggleEligible toggleDenial = iota
	toggleBusy
	toggleConfigInvalid
	toggleMalformed
	togglePendingVerification
	toggleMarketplaceBlocked
	toggleRequired
	toggleCollectionOnly
)

// rowToggleEligibility evaluates the shared toggle guards against the
// current model and row without dispatching.
func (m Model) rowToggleEligibility(r row) toggleDenial {
	if m.loading || m.syncing {
		return toggleBusy
	}
	if m.configInvalid {
		return toggleConfigInvalid
	}
	return rowToggleDenial(r)
}

// rowToggleDenial evaluates the guards that belong to the row itself, so
// hints can follow what the row supports regardless of transient states.
func rowToggleDenial(r row) toggleDenial {
	switch {
	case r.IsMalformed:
		return toggleMalformed
	case r.PendingVerification:
		return togglePendingVerification
	case r.MarketplaceDiscovery != nil && !marketplaceDiscoveryInstallable(r):
		return toggleMarketplaceBlocked
	case r.Artifact.Required:
		return toggleRequired
	case r.MarketplaceCollectionOnly && !r.canOverrideMarketplace():
		return toggleCollectionOnly
	}
	return toggleEligible
}

func (m Model) applyRowSpaceEligibility(r row) (tea.Model, tea.Cmd) {
	toast := func(text string) tea.Cmd {
		return func() tea.Msg {
			return toastMsg{text: text, duration: 2 * time.Second}
		}
	}
	switch m.rowToggleEligibility(r) {
	case toggleBusy, toggleMalformed:
		return m, nil
	case toggleConfigInvalid:
		return m, invalidConfigToastCmd()
	case togglePendingVerification:
		return m, toast("selection is still being verified")
	case toggleMarketplaceBlocked:
		return m, toast("this Marketplace resource is not installable")
	case toggleRequired:
		return m, toast("required items cannot be toggled")
	case toggleCollectionOnly:
		return m, toast("collection-selected Marketplace items must be toggled through their collection")
	}
	m.pane = paneToggleModal
	m.modalSkill = r
	return m, nil
}

func (m Model) handleDetailKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Space) {
		for _, r := range m.rows {
			if rowKey(r) != m.detailKey {
				continue
			}
			if r.UnavailableReason != "" || marketplaceDiscoveryInstallable(r) {
				return m.applyRowSpaceEligibility(r)
			}
			break
		}
	}
	switch {
	case key.Matches(msg, m.keys.DetailBack):
		m.pane = paneList
		m.detailKey = ""
		m.detailSelectedPath = ""
		m.bundle = nil
		m.cancelRequest(requestDetail)
		return m, nil
	case key.Matches(msg, m.keys.Up):
		if m.bundle == nil {
			var cmd tea.Cmd
			m.detailViewport, cmd = m.detailViewport.Update(msg)
			return m, cmd
		}
		return m.moveBundleCursor(-1)
	case key.Matches(msg, m.keys.Down):
		if m.bundle == nil {
			var cmd tea.Cmd
			m.detailViewport, cmd = m.detailViewport.Update(msg)
			return m, cmd
		}
		return m.moveBundleCursor(1)
	case key.Matches(msg, m.keys.Top):
		if m.bundle != nil {
			m.bundle.top()
			m.ensureBundleCursorVisible()
		} else {
			m.detailViewport.GotoTop()
		}
		return m, nil
	case key.Matches(msg, m.keys.Bottom):
		if m.bundle != nil {
			m.bundle.bottom()
			m.ensureBundleCursorVisible()
		} else {
			m.detailViewport.GotoBottom()
		}
		return m, nil
	case key.Matches(msg, m.keys.Collapse):
		if m.bundle != nil {
			m.bundle.collapse()
			m.ensureBundleCursorVisible()
		}
		return m, nil
	case key.Matches(msg, m.keys.Expand):
		if m.bundle != nil {
			m.bundle.expand()
			m.ensureBundleCursorVisible()
		}
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		if m.bundle != nil {
			before := m.bundle.selectedPath()
			m.bundle.toggle()
			if entry, ok := m.bundle.selectedEntry(); ok && !entry.IsDir {
				m.detailSelectedPath = entry.RelPath
			}
			m.ensureBundleCursorVisible()
			if m.detailSelectedPath != before {
				return m.queueBundlePreview(false)
			}
		}
		return m, nil
	case key.Matches(msg, m.keys.PageDown), key.Matches(msg, m.keys.PageUp):
		m.detailStickyBottom = false
		if m.handleViewportPageKey(msg, &m.detailViewport) {
			return m, nil
		}
	}
	// Preview scrolling remains available through page keys and viewport input.
	m.detailStickyBottom = false
	if m.handleViewportPageKey(msg, &m.detailViewport) {
		return m, nil
	}
	var cmd tea.Cmd
	m.detailViewport, cmd = m.detailViewport.Update(msg)
	return m, cmd
}

func (m Model) moveBundleCursor(delta int) (tea.Model, tea.Cmd) {
	if m.bundle == nil {
		return m, nil
	}
	before := m.bundle.selectedPath()
	m.bundle.move(delta)
	m.ensureBundleCursorVisible()
	if selected := m.bundle.selectedPath(); selected != "" && selected != before {
		m.detailSelectedPath = selected
		return m.queueBundlePreview(false)
	}
	return m, nil
}

func (m Model) queueBundlePreview(keepScroll bool) (tea.Model, tea.Cmd) {
	if m.bundle == nil || m.detailSelectedPath == "" {
		return m, nil
	}
	for _, r := range m.rows {
		if rowKey(r) == m.detailKey {
			generation := m.beginRequest(requestDetail)
			m.detailViewport.SetContent("Loading…")
			return m, loadBundleDetailContent(ctxOrBackground(m.ctx), m.currentTheme(), m.bundle, m.detailSelectedPath, generation, m.previewWidth(), keepScroll)
		}
	}
	return m, nil
}

func (m *Model) ensureBundleCursorVisible() {
	if m.bundle == nil {
		return
	}
	height := max(1, m.height-1)
	if m.bundle.cursor < m.bundleOffset {
		m.bundleOffset = m.bundle.cursor
	}
	if m.bundle.cursor >= m.bundleOffset+height {
		m.bundleOffset = m.bundle.cursor - height + 1
	}
	m.bundleOffset = min(max(0, m.bundleOffset), max(0, len(m.bundle.entries())-height))
}

// enterDetailPane opens the split browser and initializes the filesystem tree.
// Tree inspection is bounded to the root directory; selected-file rendering is
// scheduled separately so file I/O and Glamour never block the event loop.
func (m *Model) enterDetailPane() {
	m.pane = paneDetail
	m.detailLegacyRender = false
	m.detailSelectedPath = ""
	if r, ok := m.selectedRow(); ok {
		m.detailKey = rowKey(r)
		if r.PendingVerification {
			m.bundle = nil
			m.detailViewport.SetContent("Verification pending: " + sanitizeTerminalData(resourceLabel(r), terminalSingleLine) + "\n\nThis selection is configured, but exact-lock and catalog verification have not completed.")
		} else if r.UnavailableReason != "" {
			m.bundle = nil
			m.detailViewport.SetContent(renderDetailFullContent(m.currentTheme(), r, m.previewWidth()))
		} else if isMarketplaceDetailOnly(r) {
			m.bundle = nil
			if marketplaceInspectableSkill(r) {
				m.detailViewport.SetContent("Loading verified Marketplace preview…")
			} else {
				m.detailViewport.SetContent(renderMarketplaceDetailContent(m.currentTheme(), r))
			}
		} else if bundle, err := bundleTreeForRow(r); err != nil {
			m.bundle = nil
			m.detailViewport.SetContent("Error reading resource: " + sanitizeTerminalData(err.Error(), terminalSingleLine))
		} else {
			m.bundle = bundle
			m.detailSelectedPath = bundle.selectedPath()
		}
	} else {
		m.detailKey = ""
		m.bundle = nil
	}
	m.bundleOffset = 0
	m.detailViewport.SetWidth(m.width)
	m.detailViewport.SetHeight(max(1, m.height-1))
	m.detailRenderWidth = m.previewWidth()
	m.detailStickyBottom = false
	if m.bundle != nil && m.detailSelectedPath != "" {
		m.detailViewport.SetContent("Loading…")
	}
	m.detailViewport.GotoTop()
}

func previewWidthFor(width int) int {
	_, preview := paneWidths(width)
	return max(1, preview-1)
}

func (m Model) previewWidth() int { return previewWidthFor(m.width) }

func (m *Model) rerenderDetailAtWidth(width int) tea.Cmd {
	if m.detailKey == "" {
		return nil
	}
	for _, r := range m.rows {
		if rowKey(r) != m.detailKey {
			continue
		}
		if r.Artifact.Path == "" && (m.bundle == nil || m.bundle.inspection == nil) {
			continue
		}
		generation := m.beginRequest(requestDetail)
		if m.detailLegacyRender || m.bundle == nil || m.detailSelectedPath == "" {
			return loadDetailContent(m.currentTheme(), r, generation, width, true)
		}
		return loadBundleDetailContent(ctxOrBackground(m.ctx), m.currentTheme(), m.bundle, m.detailSelectedPath, generation, previewWidthFor(width), true)
	}
	return nil
}

func (m Model) handleConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.ensureSyncQuitForm()
	if key.Matches(msg, m.keys.ConfirmCancel) {
		m.pane = paneList
		m.quitAfterSync = false
		m.syncQuitForm = nil
		m.syncQuitValue = nil
		recordActionSpan(m.ctx, "modal_outcome", map[string]string{"outcome": "cancelled", "modal": "sync+quit"})
		return m, nil
	}
	m.syncQuitForm = updateModalForm(m.syncQuitForm, msg)
	if m.syncQuitForm == nil || m.syncQuitForm.State != huh.StateCompleted {
		return m, nil
	}
	syncQuit := m.syncQuitValue != nil && *m.syncQuitValue
	m.syncQuitForm = nil
	m.syncQuitValue = nil
	if syncQuit {
		return m.confirmSyncQuitYes()
	}
	return m.confirmSyncQuitNo()
}

func (m Model) confirmSyncQuitYes() (tea.Model, tea.Cmd) {
	// A sync may already be running if the user pressed 'q' during an
	// active sync (the Quit key opens the confirm pane without checking
	// m.syncing). Don't start a second sync that would race the first.
	if m.syncing {
		m.pane = paneList
		m.quitAfterSync = true
		return m, nil
	}
	// Sync then quit — start the sync and defer tea.Quit to the
	// syncDoneMsg handler. Including tea.Quit in the batch here
	// would race the sync goroutine and quit before materialization
	// completes.
	m.pane = paneList
	m.syncing = true
	m.quitAfterSync = true
	m.beginOperationProgress("sync", m.requests.active(requestSnapshot))
	slog.Debug("sync+quit confirmed, deferring quit until sync completes")
	return m, tea.Batch(m.dependencySyncCmd(), m.operationProgressCmd(), m.spinner.Tick)
}

func (m Model) confirmSyncQuitNo() (tea.Model, tea.Cmd) {
	m.pane = paneList
	m.quitAfterSync = false
	recordActionSpan(m.ctx, "modal_outcome", map[string]string{"outcome": "cancelled", "modal": "sync+quit"})
	return m, tea.Quit
}

func (m Model) View() tea.View {
	v := tea.NewView(m.viewContent())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) viewContent() string {
	if m.belowMinimumTerminal() {
		return renderResizeRequired(m)
	}

	// Full-screen overlays.
	switch m.pane {
	case paneHelp:
		m.helpViewport.SetWidth(m.width)
		m.helpViewport.SetHeight(max(1, m.height-1))
		m.helpViewport.SetContent(renderHelp(m))
		return fitTerminalWithFooter(m.helpViewport.View(), "↑/↓ or ctrl-d/u scroll · esc/? close · Ctrl+C quit", m.width, m.height)
	case paneErrors:
		m.errorsViewport.SetWidth(m.width)
		m.errorsViewport.SetHeight(max(1, m.height-1))
		m.errorsViewport.SetContent(renderErrors(m))
		return m.applyPreviewSelection(fitTerminalWithFooter(m.errorsViewport.View(), "↑/↓ or ctrl-d/u scroll · esc/e close · Ctrl+C quit", m.width, m.height))
	case paneConfirm:
		return fitTerminal(renderConfirm(m), m.width, m.height)
	case paneMarketplaceConsent:
		return fitTerminalWithFooter(renderMarketplaceConsent(m), "↑/↓/page · y install · n/esc cancel · ^C quit", m.width, m.height)
	case paneDetail:
		listWidth, previewWidth := paneWidths(m.width)
		bodyHeight := max(1, m.height-1)
		preview := m.detailViewport
		preview.SetWidth(max(1, previewWidth-1))
		preview.SetHeight(bodyHeight)
		tree := renderBundleTree(m, listWidth, bodyHeight)
		joined := lipgloss.JoinHorizontal(lipgloss.Top, tree, preview.View())
		footer := "esc/q back · ↑/↓/j/k navigate · h/l fold · enter toggle · pgup/pgdn preview · Ctrl+C quit"
		if compactTerminalLayout(m) {
			footer = "esc/q back · Ctrl+C quit"
		}
		return m.applyPreviewSelection(fitTerminalWithFooter(joined, footer, m.width, m.height))
	}

	base := m.renderBaseScreen()

	// The command palette renders as a Lip Gloss layer over the list
	// without hiding the underlying controls.
	if m.palette.open && m.pane == paneList {
		if layer := renderPalette(m); layer != "" {
			base = compositePalette(base, layer, m.width, m.height, m.paletteTop())
		}
	}

	// paneToggleModal renders as an overlay on top of the normal two-pane
	// layout so the user can still see the list behind the modal.
	var modal string
	switch m.pane {
	case paneToggleModal:
		modal = renderToggleModal(m)
	case paneInitModal:
		modal = renderInitModal(m)
	case paneHarnessModal:
		modal = renderHarnessModal(m)
	case paneEditConfigModal:
		modal = renderEditConfigModal(m)
	case paneViewSettings:
		modal = renderViewSettingsModal(m)
	case paneBulkToggle:
		modal = renderBulkToggleModal(m)
	}
	if modal == "" {
		return m.applyPreviewSelection(compositeToasts(base, m.layeredToasts(), m.width, m.height))
	}
	return m.applyPreviewSelection(fitTerminal(compositeToasts(compositeModal(base, modal, m.width, m.height), m.layeredToasts(), m.width, m.height), m.width, m.height))
}

// renderBaseScreen renders the standard header + two-pane layout + footer
// without any overlay. Extracted so overlays can composite on top.
func (m Model) renderBaseScreen() string {
	layout := screenLayoutFor(m.height, m.searchRowVisible())
	if !layout.header {
		return ""
	}

	parts := []string{renderHeader(m)}
	if layout.search {
		parts = append(parts, renderSearchRow(m))
	}
	if layout.mainHeight > 0 {
		listWidth, detailWidth := paneWidths(m.width)
		listBox := lipgloss.NewStyle().
			Width(listWidth).
			Height(layout.mainHeight).
			MaxHeight(layout.mainHeight).
			Render(renderList(m, layout.mainHeight))

		detailBox := lipgloss.NewStyle().
			Width(detailWidth).
			Height(layout.mainHeight).
			MaxHeight(layout.mainHeight).
			PaddingLeft(1).
			Render(renderDetail(m))

		parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Top, listBox, detailBox))
	}
	if layout.footer {
		parts = append(parts, renderFooter(m))
	}
	return fitTerminal(strings.Join(parts, "\n"), m.width, m.height)
}

// searchRowVisible reports whether the search row occupies a line: while
// the query is being typed and while a confirmed filter is applied.
func (m Model) searchRowVisible() bool {
	return m.filterActive || m.filter != ""
}

// renderSearchRow shows the focused search input, or the applied filter as
// a chip, followed by the local match count and how to change it.
func renderSearchRow(m Model) string {
	count := len(m.filteredRows)
	matches := fmt.Sprintf("%d matches", count)
	if count == 1 {
		matches = "1 match"
	}
	if m.filterActive {
		input := m.searchInput
		if m.discoveryLoading {
			input.Prompt = m.spinner.View() + " "
		}
		if m.filter == "" {
			return ansi.Truncate(input.View(), m.width, "…")
		}
		return ansi.Truncate(input.View()+"  "+styleDim.Render(matches), m.width, "…")
	}
	chip := styleSearchPrompt.Render("/ ") + sanitizeTerminalData(m.filter, terminalSingleLine)
	return ansi.Truncate(chip+"  "+styleDim.Render(matches+" · esc clear · / edit"), m.width, "…")
}

type screenLayout struct {
	header     bool
	search     bool
	footer     bool
	mainHeight int
}

func screenLayoutFor(height int, searchRow bool) screenLayout {
	if height < 1 {
		return screenLayout{}
	}
	layout := screenLayout{header: true}
	if height == 1 {
		return layout
	}
	layout.footer = true
	remaining := height - 2
	if searchRow && remaining > 0 {
		layout.search = true
		remaining--
	}
	layout.mainHeight = remaining
	return layout
}

func fitTerminal(content string, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(content)
}

func paneWidths(totalWidth int) (listWidth, detailWidth int) {
	totalWidth = max(0, totalWidth)
	listWidth = totalWidth * 2 / 5
	if totalWidth >= 40 && listWidth < 20 {
		listWidth = 20
	}
	return listWidth, totalWidth - listWidth
}

func renderHeader(m Model) string {
	repoName := sanitizeTerminalData(filepathBase(m.repoRoot), terminalSingleLine)
	title := styleHeader.Render(fmt.Sprintf(" agent-marketplace — %s ", repoName))
	return title
}

func filepathBase(path string) string {
	if path == "" {
		return "?"
	}
	// Use filepath.Base logic without importing path/filepath
	// (already imported in actions.go, but keeping this self-contained)
	parts := strings.Split(strings.TrimRight(path, "/"), "/")
	return parts[len(parts)-1]
}

func (m *Model) applyFilter() []row {
	for i := range m.rows {
		m.rows[i].ensureLabelMemo()
		if m.filter != "" {
			m.rows[i].ensureSearchable()
		}
	}
	if m.filter == "" {
		return m.rows
	}

	terms := searchTerms(m.filter)
	if len(terms) == 0 {
		return m.rows
	}
	// The name matches fuzzily with the query's separators removed, so a
	// spaced query such as "grill docs" still finds grill-with-docs.
	// Metadata only matches when every query term begins one of its words,
	// so letters scattered through a long description never match.
	nameMatches := fuzzy.FindFromNoSort(strings.Join(terms, ""), searchNameSource(m.rows))

	filtered := m.filterScratch[:0]
	next := 0
	for i := range m.rows {
		r := m.rows[i]
		r.searchRanked = true
		if next < len(nameMatches) && nameMatches[next].Index == i {
			r.searchNameMatch = true
			r.searchScore = nameMatches[next].Score
			r.searchMatchRunes = runeIndexes(r.searchName, nameMatches[next].MatchedIndexes)
			next++
		} else if rowHasAllWordPrefixes(&r, terms) {
			r.searchNameMatch = false
			r.searchScore = 0
			r.searchMatchRunes = nil
		} else {
			continue
		}
		filtered = append(filtered, r)
	}
	m.filterScratch = filtered[:0]
	return filtered
}

func rowHasAllWordPrefixes(r *row, terms []string) bool {
	for _, term := range terms {
		if !r.hasWordWithPrefix(term) {
			return false
		}
	}
	return true
}

// searchRankLess reports whether a ranks strictly above b: name matches
// before metadata-only matches, then by descending fuzzy score.
func searchRankLess(a, b row) bool {
	if a.searchNameMatch != b.searchNameMatch {
		return a.searchNameMatch
	}
	return a.searchScore > b.searchScore
}

type searchNameSource []row

func (s searchNameSource) String(i int) string { return s[i].searchName }
func (s searchNameSource) Len() int            { return len(s) }

func fuzzyMatch(pattern, text string) (fuzzy.Match, bool) {
	for _, match := range fuzzy.FindFromIterNoSort(pattern, func(yield func(string) bool) {
		yield(text)
	}) {
		return match, true
	}
	return fuzzy.Match{}, false
}

func runeIndexes(s string, byteIdx []int) []int {
	if len(byteIdx) == 0 {
		return nil
	}
	out := make([]int, 0, len(byteIdx))
	want := 0
	runeIdx := 0
	for offset := range s {
		if byteIdx[want] == offset {
			out = append(out, runeIdx)
			want++
			if want == len(byteIdx) {
				return out
			}
		}
		runeIdx++
	}
	return out
}

// clampListOffset adjusts listOffset so the cursor stays visible in the
// left pane. Must be called after any change to cursor or filteredRows.
func (m *Model) clampListOffset() {
	if len(m.flatItems) == 0 {
		m.cursor = 0
		m.listOffset = 0
		return
	}
	m.cursor = min(max(0, m.cursor), len(m.flatItems)-1)
	m.listOffset = min(max(0, m.listOffset), len(m.flatItems)-1)

	// Cursor above the visible window
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	}

	listWidth, _ := paneWidths(m.width)
	visibleHeight := m.listVisibleHeight()
	heights := m.listHeightOffsets(listWidth)
	// When every row fits, show the whole list. Keeping a reload anchor's
	// screen line would otherwise hide rows above blank space; overflowing
	// lists keep the anchored line.
	if heights[len(m.flatItems)] <= visibleHeight {
		m.listOffset = 0
		return
	}
	cursorBottom := heights[m.cursor+1]
	if cursorBottom-heights[m.listOffset] <= visibleHeight {
		return
	}
	low, high := m.listOffset, m.cursor
	for low < high {
		mid := low + (high-low)/2
		if cursorBottom-heights[mid] <= visibleHeight {
			high = mid
		} else {
			low = mid + 1
		}
	}
	m.listOffset = low
}

func (m *Model) listHeightOffsets(width int) []int {
	if m.listHeightWidth == width && len(m.listHeightPrefix) == len(m.flatItems)+1 {
		return m.listHeightPrefix
	}
	if cap(m.listHeightPrefix) < len(m.flatItems)+1 {
		m.listHeightPrefix = make([]int, len(m.flatItems)+1)
	} else {
		m.listHeightPrefix = m.listHeightPrefix[:len(m.flatItems)+1]
		m.listHeightPrefix[0] = 0
	}
	for i := range m.flatItems {
		m.listHeightPrefix[i+1] = m.listHeightPrefix[i] + flatItemHeight(m.flatItems[i], width, compactTerminalLayout(*m))
	}
	m.listHeightWidth = width
	return m.listHeightPrefix
}

// listVisibleHeight computes how many rows fit in the left pane,
// mirroring the same calculation used in View().
func (m Model) listVisibleHeight() int {
	return max(1, screenLayoutFor(m.height, m.searchRowVisible()).mainHeight)
}

func Run(ctx context.Context, repoRoot, homeDir string, options ...Option) error {
	return runSession(ctx, repoRoot, homeDir, func(model tea.Model) error {
		return runTUIModel(model, defaultTUIProgramOptions()...)
	}, options...)
}

// runSession owns the session span and final inventory flush. Defers run in
// reverse order, so the flush records before the session span ends.
func runSession(ctx context.Context, repoRoot, homeDir string, runModel func(tea.Model) error, options ...Option) error {
	initRenderer()

	model := NewModel(repoRoot, homeDir, options...)
	model.ctx = pipeline.WithTransientOrganizationMarketplaceLockCache(ctxOrBackground(ctx))
	tel := telemetry.TelemetryFromContext(model.ctx)
	if tel != nil {
		model.ctx, model.sessionSpan = tel.StartSpan(model.ctx, "agent_marketplace.tui.session", nil)
	}
	defer model.sessionSpan.End()
	recordCtx := model.ctx
	model.ctx = pipeline.WithoutInventory(model.ctx)
	defer model.pendingInventory.flush(recordCtx)

	seedTerminalSize(&model, os.Stdout)
	restoreLogs := logging.BeginTerminalSession()
	defer restoreLogs()

	return runModel(model)
}

func defaultTUIProgramOptions() []tea.ProgramOption {
	return nil
}

func newTUIProgram(model tea.Model, options ...tea.ProgramOption) *tea.Program {
	return tea.NewProgram(newFramePacedModel(model), options...)
}

func runTUIModel(model tea.Model, options ...tea.ProgramOption) error {
	return runWithTerminalGuard(defaultTerminalIO(), func() (tea.Model, error) {
		return newTUIProgram(model, options...).Run()
	})
}

func finishTUIRun(finalModel tea.Model, err error) error {
	if errors.Is(err, tea.ErrInterrupted) {
		slog.Debug("tui exited via interrupt")
		return nil
	}
	if err != nil {
		slog.Error("tui exited with error", "interrupted", false)
		return sanitizeReturnedError(err)
	}
	if wrapped, ok := finalModel.(*framePacedModel); ok {
		finalModel = wrapped.inner
	}
	if fm, ok := finalModel.(Model); ok && fm.syncQuitErr != nil {
		slog.Error("tui sync+quit reported sync error")
		return sanitizeReturnedError(fm.syncQuitErr)
	}
	return nil
}
