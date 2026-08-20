package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/logging"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func NewModel(repoRoot, homeDir string) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#C47000", Dark: "#FD971F"})

	si := textinput.New()
	si.Prompt = "/ "
	si.PromptStyle = styleSearchPrompt
	si.TextStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#F8F8F2"})
	si.Cursor.Style = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#1A8AAF", Dark: "#66D9EF"})

	m := Model{
		repoRoot:  repoRoot,
		homeDir:   homeDir,
		loading:   true,
		activeTab: tabScope,
		tabStates: newTabStates(),
		spinner:   sp,
		// width and height stay zero (unknown) until the real terminal size
		// arrives via seedTerminalSize or the first WindowSizeMsg: an
		// unknown size renders the bounded below-minimum state instead of a
		// full frame guessed at the wrong dimensions.
		searchInput:    si,
		detailViewport: viewport.New(80, 24),
		helpViewport:   viewport.New(80, 24),
		errorsViewport: viewport.New(80, 24),
		keys:           defaultKeyMap,
		help:           newHelpModel(),
	}
	if !testSkipInitModal {
		showInit, initErr := shouldShowInitModal(repoRoot, homeDir)
		if initErr != nil {
			slog.Error("failed to resolve first-run target configuration", "error", initErr)
		}
		if showInit {
			slog.Info("init modal opened", "home_dir", homeDir)
			initModal, err := newInitModalState(homeDir)
			if err != nil {
				slog.Error("failed to detect installed harnesses", "error", err)
				initModal = initModalState{selected: map[string]bool{}}
			}
			m.initModal = initModal
			m.pane = paneInitModal
		}
	}
	m.beginRequest(requestSnapshot)
	return m
}

// testSkipInitModal is a package-private test hook that, when set, prevents
// NewModel from automatically opening the first-run init modal. Production
// code never touches it; see init_test.go's TestMain for the setter.
var testSkipInitModal bool

// shouldShowInitModal reports whether the current project has no effective
// target set. Project targets take precedence, followed by user-personal and
// organization targets. The home directory must be present so synthetic paths
// do not trigger first-run initialization.
func shouldShowInitModal(repoRoot, homeDir string) (bool, error) {
	if homeDir == "" {
		return false, nil
	}
	if info, err := os.Stat(homeDir); err != nil || !info.IsDir() {
		return false, nil
	}
	ec := config.LoadEffective(context.Background(), config.LoadEffectiveOptions{
		RepoRoot:       repoRoot,
		LocalConfigDir: repoRoot,
		GlobalPath:     filepath.Join(homeDir, ".agents", "config.yaml"),
	})
	if len(ec.Errors) > 0 {
		return false, errors.Join(ec.Errors...)
	}
	return len(ec.TargetHarnesses) == 0, nil
}

// saveTabState captures the current cursor/offset/filter into the active
// tab's state bucket before switching away.
func (m *Model) saveTabState() {
	if m.tabStates == nil {
		return
	}
	st, ok := m.tabStates[m.activeTab]
	if !ok || st == nil {
		return
	}
	st.cursor = m.cursor
	st.listOffset = m.listOffset
	st.filter = m.searchInput.Value()
}

// restoreTabState pulls the incoming tab's state into the model. Callers
// should call saveTabState first for the outgoing tab.
func (m *Model) restoreTabState(t tab) {
	if m.tabStates == nil {
		m.tabStates = newTabStates()
	}
	st, ok := m.tabStates[t]
	if !ok || st == nil {
		st = &tabViewState{foldedSections: make(map[string]bool)}
		m.tabStates[t] = st
	}
	m.activeTab = t
	m.cursor = st.cursor
	m.listOffset = st.listOffset
	m.filter = st.filter
	m.searchInput.SetValue(st.filter)
	m.filterActive = false
	m.searchInput.Blur()
	m.recomputeView()
}

// switchTab saves outgoing state and restores incoming state in one step.
func (m *Model) switchTab(t tab) {
	if t == m.activeTab {
		return
	}
	m.saveTabState()
	m.restoreTabState(t)
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, m.requests.active(requestSnapshot)), m.spinner.Tick)
}

// Update funnels every message through update and then resynchronizes the
// help/diagnostics overlay viewports, so any message that changed the
// terminal size, the active pane, or the diagnostic store leaves the overlay
// content and dimensions current before the next key press or frame.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Sticky-bottom anchors are sampled BEFORE the message is handled and
	// only while the corresponding pane is already active: a viewport that
	// was at the bottom stays anchored to the bottom across content growth
	// and resizes, while a freshly (re)opened pane keeps its persisted
	// offset instead of jumping.
	helpStickyBottom := m.pane == paneHelp && m.helpViewport.AtBottom()
	errorsStickyBottom := m.pane == paneErrors && m.errorsViewport.AtBottom()
	updated, cmd := m.update(msg)
	if updatedModel, ok := updated.(Model); ok {
		updatedModel.syncOverlayViewports(helpStickyBottom, errorsStickyBottom)
		return updatedModel, cmd
	}
	return updated, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		oldWidth, oldHeight := m.width, m.height
		wasBelowMinimum := m.belowMinimumTerminal()
		// Sample the detail bottom anchor against the OLD dimensions and
		// content before the resize mutates them; the re-render scheduled
		// below consumes it on acceptance.
		detailAtBottom := m.pane == paneDetail && m.detailViewport.AtBottom()
		m.width = msg.Width
		m.height = msg.Height
		belowMinimum := m.belowMinimumTerminal()
		if !belowMinimum {
			m.detailViewport.Width = msg.Width
			m.detailViewport.Height = max(1, msg.Height-1)
			m.clampListOffset()
		}
		layout := screenLayoutFor(m.height, m.filterActive)
		listWidth, detailWidth := paneWidths(m.width)
		slog.Debug("tui resized", "old_width", oldWidth, "old_height", oldHeight, "width", m.width, "height", m.height, "list_width", listWidth, "detail_width", detailWidth, "visible_height", layout.mainHeight, "below_minimum", belowMinimum, "recovered", wasBelowMinimum && !belowMinimum)
		// While the detail pane is open, a width change invalidates the
		// rendered wrap: schedule a generation-correlated re-render at the
		// actual new width, keeping the scroll anchor.
		if !belowMinimum && m.pane == paneDetail && msg.Width != m.detailRenderWidth {
			renderedWidth := m.detailRenderWidth
			if rerender := m.rerenderDetailAtWidth(msg.Width); rerender != nil {
				m.detailStickyBottom = detailAtBottom
				slog.Debug("tui detail re-render scheduled for width change", "rendered_width", renderedWidth, "width", msg.Width, "sticky_bottom", detailAtBottom)
				return m, rerender
			}
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case cursor.BlinkMsg:
		// Forward cursor blink ticks to the search input so the cursor
		// blinks while the search input is focused.
		if m.filterActive {
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.MouseMsg:
		// Below the supported minimum, all user input is suspended except
		// the hard-quit keys (handled in handleKey); the mouse is gated too.
		if m.belowMinimumTerminal() {
			return m, nil
		}
		switch m.pane {
		case paneDetail:
			var cmd tea.Cmd
			m.detailViewport, cmd = m.detailViewport.Update(msg)
			return m, cmd
		case paneHelp:
			var cmd tea.Cmd
			m.helpViewport, cmd = m.helpViewport.Update(msg)
			return m, cmd
		case paneErrors:
			var cmd tea.Cmd
			m.errorsViewport, cmd = m.errorsViewport.Update(msg)
			return m, cmd
		}
		return m, nil

	case snapshotLoadedMsg:
		if !m.acceptsRequest(requestSnapshot, msg.generation) {
			return m, nil
		}
		m.loading = false
		// Every accepted snapshot re-derives mutation safety: hard errors
		// make the displayed rows inspection-only until a clean snapshot.
		m.rowsStale = len(diagnosticErrors(msg.diagnostics)) > 0
		if msg.pendingMigration.hasConfigs() {
			m.driftAssessment = msg.driftAssessment
			m.diagnostics.replaceSnapshot(msg.diagnostics)
			m.migrationPrompt = msg.pendingMigration
			m.pane = paneMigrationModal
			m.refreshing = false
			return m, nil
		}
		if msg.mergedCfg == nil && len(diagnosticErrors(msg.diagnostics)) > 0 {
			m.driftAssessment = msg.driftAssessment
			m.diagnostics.replaceSnapshot(msg.diagnostics)
			m.refreshing = false
			return m, nil
		}
		selection := m.selectionKeyForRebuild()
		m.rows = msg.rows
		m.driftReport = msg.driftReport
		m.driftAssessment = msg.driftAssessment
		m.diagnostics.replaceSnapshot(msg.diagnostics)
		m.mergedCfg = msg.mergedCfg
		m.resolved = msg.resolved
		m.localExcludes = msg.localExcludes
		m.globalHarnesses = msg.globalHarnesses
		m.projectHarnesses = msg.projectHarnesses
		m.userHarnesses = msg.userHarnesses
		m.targetHarnesses = msg.targetHarnesses
		m.targetHarnessSource = msg.targetHarnessSource
		m.recomputeView()
		m.restoreSelectionAfterRebuild(selection)
		if msg.refreshAfterLoad {
			m.refreshing = true
			generation := m.beginRequest(requestSnapshot)
			return m, tea.Batch(refreshSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation), m.spinner.Tick)
		}
		m.refreshing = false
		return m, nil

	case refreshLoadedMsg:
		if !m.acceptsRequest(requestSnapshot, msg.generation) {
			return m, nil
		}
		m.refreshing = false
		m.rowsStale = len(diagnosticErrors(msg.diagnostics)) > 0
		if msg.pendingMigration.hasConfigs() {
			m.driftAssessment = msg.driftAssessment
			m.diagnostics.replaceSnapshot(msg.diagnostics)
			m.migrationPrompt = msg.pendingMigration
			m.pane = paneMigrationModal
			return m, nil
		}
		if msg.mergedCfg == nil && len(diagnosticErrors(msg.diagnostics)) > 0 {
			m.driftAssessment = msg.driftAssessment
			m.diagnostics.replaceSnapshot(msg.diagnostics)
			return m, nil
		}
		selection := m.selectionKeyForRebuild()
		m.rows = msg.rows
		m.driftReport = msg.driftReport
		m.driftAssessment = msg.driftAssessment
		m.diagnostics.replaceSnapshot(msg.diagnostics)
		m.mergedCfg = msg.mergedCfg
		m.resolved = msg.resolved
		m.localExcludes = msg.localExcludes
		m.globalHarnesses = msg.globalHarnesses
		m.projectHarnesses = msg.projectHarnesses
		m.userHarnesses = msg.userHarnesses
		m.targetHarnesses = msg.targetHarnesses
		m.targetHarnessSource = msg.targetHarnessSource
		m.recomputeView()
		m.restoreSelectionAfterRebuild(selection)
		return m, nil

	case toastMsg:
		m.toast = msg.text
		m.toastExp = time.Now().Add(msg.duration)
		return m, toastExpiryCmd(msg.duration)

	case toastExpiredMsg:
		// Clear the toast if it has expired. Check the expiry time to
		// avoid clearing a toast that was replaced more recently.
		if !m.toastExp.IsZero() && time.Now().After(m.toastExp) {
			m.toast = ""
			m.toastExp = time.Time{}
		}
		return m, nil

	case configWrittenMsg:
		if !m.acceptsOperation(msg.operation) {
			return m, nil
		}
		m.finishOperation(msg.operation)
		generation := m.beginRequest(requestSnapshot)
		reload := loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation)
		if msg.err != nil {
			// Retain only this operation's diagnostic and schedule the
			// authoritative reload so the rows reflect whatever the failed
			// mutation actually left on disk.
			m.diagnostics.recordOperationAttemptFailure(operationMutation, diagnosticMutation, uint64(msg.operation), msg.err)
			m.toast = fmt.Sprintf("error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("tui config mutation failed", "resource", msg.skillName, "enabled", msg.enabled, "operation_id", msg.operation)
			action := "remove"
			if msg.enabled {
				action = "install"
			}
			recordControlledErrorSpan(m.ctx, action, msg.err)
			return m, tea.Batch(reload, toastExpiryCmd(5*time.Second))
		}
		m.diagnostics.recordOperationAttemptSuccess(operationMutation, uint64(msg.operation))
		action := "Enabled"
		if !msg.enabled {
			action = "Disabled"
		}
		m.toast = fmt.Sprintf("%s %s", action, msg.skillName)
		m.toastExp = time.Now().Add(2 * time.Second)
		m.refreshing = false
		// Re-load snapshot to reflect the config change.
		return m, tea.Batch(reload, toastExpiryCmd(2*time.Second))

	case harnessWrittenMsg:
		if !m.acceptsOperation(msg.operation) {
			return m, nil
		}
		m.finishOperation(msg.operation)
		generation := m.beginRequest(requestSnapshot)
		reload := loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation)
		if msg.err != nil {
			m.diagnostics.recordOperationAttemptFailure(operationTargets, diagnosticMutation, uint64(msg.operation), msg.err)
			m.toast = fmt.Sprintf("error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("tui target configuration mutation failed", "scope", msg.scopeLabel, "operation_id", msg.operation)
			recordControlledErrorSpan(m.ctx, "harness", msg.err)
			return m, tea.Batch(reload, toastExpiryCmd(5*time.Second))
		}
		m.diagnostics.recordOperationAttemptSuccess(operationTargets, uint64(msg.operation))
		m.toast = fmt.Sprintf("Saved %s harnesses", msg.scopeLabel)
		m.toastExp = time.Now().Add(2 * time.Second)
		m.refreshing = false
		return m, tea.Batch(reload, toastExpiryCmd(2*time.Second))

	case syncDoneMsg:
		m.syncing = false
		m.refreshing = false
		if msg.err != nil {
			m.diagnostics.recordOperationFailure(operationSync, diagnosticSync, msg.err)
			m.toast = fmt.Sprintf("sync error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("tui sync result failed")
			recordControlledErrorSpan(m.ctx, "sync", msg.err)
		} else {
			m.diagnostics.recordOperationSuccess(operationSync)
			m.toast = "Sync complete"
			m.toastExp = time.Now().Add(2 * time.Second)
		}
		// If the user confirmed "sync+quit", quit now that the sync has
		// actually completed. This avoids the race where tea.Quit in a
		// tea.Batch kills the program before the sync goroutine finishes.
		if m.quitAfterSync {
			m.quitAfterSync = false
			slog.Debug("sync complete, quitting as requested", "success", msg.err == nil)
			m.syncQuitErr = msg.err
			return m, tea.Quit
		}
		// Re-load to pick up on-disk changes
		generation := m.beginRequest(requestSnapshot)
		return m, tea.Batch(loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation), toastExpiryCmd(5*time.Second))

	case updateDoneMsg:
		m.syncing = false
		accepted := msg.snapshot.generation != 0 && m.acceptsRequest(requestSnapshot, msg.snapshot.generation)
		toastDuration := 2 * time.Second
		if accepted {
			if msg.snapshot.mergedCfg == nil && len(diagnosticErrors(msg.snapshot.diagnostics)) > 0 {
				// Mirror the plain reload path: with no merged config the
				// previous rows are kept for inspection (stale: inspection
				// only), while the new drift assessment and diagnostics land.
				m.driftAssessment = msg.snapshot.driftAssessment
				m.diagnostics.replaceSnapshot(msg.snapshot.diagnostics)
				m.rowsStale = true
			} else {
				selection := m.selectionKeyForRebuild()
				m.rows = msg.snapshot.rows
				m.driftReport = msg.snapshot.driftReport
				m.driftAssessment = msg.snapshot.driftAssessment
				m.diagnostics.replaceSnapshot(msg.snapshot.diagnostics)
				m.rowsStale = len(diagnosticErrors(msg.snapshot.diagnostics)) > 0
				m.mergedCfg = msg.snapshot.mergedCfg
				m.resolved = msg.snapshot.resolved
				m.localExcludes = msg.snapshot.localExcludes
				m.globalHarnesses = msg.snapshot.globalHarnesses
				m.projectHarnesses = msg.snapshot.projectHarnesses
				m.userHarnesses = msg.snapshot.userHarnesses
				m.targetHarnesses = msg.snapshot.targetHarnesses
				m.targetHarnessSource = msg.snapshot.targetHarnessSource
				m.recomputeView()
				m.restoreSelectionAfterRebuild(selection)
			}
			switch {
			case msg.err != nil:
				m.diagnostics.recordOperationFailure(operationUpdate, diagnosticUpdate, msg.err)
				m.toast = msg.err.Error()
				m.toastExp = time.Now().Add(5 * time.Second)
				toastDuration = 5 * time.Second
				slog.Error("tui update failed", "stage", msg.stage)
				recordControlledErrorSpan(m.ctx, "update", msg.err)
			case len(msg.staleCaches) > 0:
				// Partial outcome: the refresh fell back to stale caches.
				// The mutation completed, superseding earlier update
				// failures, but the full-success copy is never emitted.
				m.diagnostics.recordOperationSuccess(operationUpdate)
				m.toast = fmt.Sprintf("Update complete with warnings: %d registry fetch(es) failed, used stale cache", len(msg.staleCaches))
				m.toastExp = time.Now().Add(5 * time.Second)
				toastDuration = 5 * time.Second
			default:
				m.diagnostics.recordOperationSuccess(operationUpdate)
				m.toast = "Update refresh and materialization complete"
				m.toastExp = time.Now().Add(2 * time.Second)
			}
		}
		if m.quitAfterSync {
			// The user confirmed sync+quit while the update was in flight.
			// The confirmed sync could not run concurrently, so it is
			// queued now — and the program quits on the SYNC result, never
			// on the update result.
			m.syncing = true
			slog.Debug("update complete, running confirmed sync before quitting", "update_error", msg.err != nil)
			return m, tea.Batch(runSyncCmd(ctxOrBackground(m.ctx), m.repoRoot), m.spinner.Tick)
		}
		if !accepted {
			// A stale update completion can neither clear errors nor claim
			// success, and its snapshot is not applied. The update may
			// still have mutated on-disk state, so schedule a current
			// snapshot: the current generation captures the truth.
			generation := m.beginRequest(requestSnapshot)
			slog.Debug("stale update completion rejected; scheduled current snapshot",
				"stale_generation", msg.snapshot.generation, "generation", generation)
			return m, loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation)
		}
		return m, toastExpiryCmd(toastDuration)

	case migrationDoneMsg:
		if !m.acceptsOperation(msg.operation) {
			return m, nil
		}
		m.finishOperation(msg.operation)
		generation := m.beginRequest(requestSnapshot)
		reload := loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation)
		if msg.err != nil {
			m.diagnostics.recordOperationAttemptFailure(operationMigration, diagnosticMigration, uint64(msg.operation), msg.err)
			m.toast = fmt.Sprintf("migration error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("tui migration failed", "files_migrated", msg.count, "operation_id", msg.operation)
			recordControlledErrorSpan(m.ctx, "migrate", msg.err)
			return m, tea.Batch(reload, toastExpiryCmd(5*time.Second))
		}
		m.diagnostics.recordOperationAttemptSuccess(operationMigration, uint64(msg.operation))
		m.toast = fmt.Sprintf("Migrated %d config file(s)", msg.count)
		m.toastExp = time.Now().Add(2 * time.Second)
		m.migrationPrompt = migrationPrompt{}
		return m, tea.Batch(reload, toastExpiryCmd(2*time.Second))

	case editConfigPathMsg:
		if !m.acceptsOperation(msg.operation) {
			return m, nil
		}
		m.finishOperation(msg.operation)
		generation := m.beginRequest(requestSnapshot)
		reload := loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation)
		if msg.err != nil {
			m.diagnostics.recordOperationAttemptFailure(operationEditor, diagnosticEditor, uint64(msg.operation), msg.err)
			m.toast = fmt.Sprintf("editor error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("tui config editor failed", "reload", true, "operation_id", msg.operation)
			recordControlledErrorSpan(m.ctx, "edit_config", msg.err)
			return m, tea.Batch(reload, toastExpiryCmd(5*time.Second))
		}
		m.diagnostics.recordOperationAttemptSuccess(operationEditor, uint64(msg.operation))
		slog.Debug("tui config editor completed", "reload", true, "operation_id", msg.operation)
		return m, reload

	case detailContentMsg:
		if m.pane != paneDetail || !m.acceptsRequest(requestDetail, msg.generation) {
			return m, nil
		}
		if msg.width != m.width {
			// The render was requested at a width the terminal no longer
			// has, but the intervening resize began no superseding
			// generation (a below-minimum dip, or the anchored row losing
			// its content path). Applying it would wrap content at a width
			// the viewport does not have and would move detailRenderWidth
			// off the on-screen width, suppressing the next resize's
			// re-render. Reject it and re-render at the current width;
			// when no re-render can be scheduled the previous content stays
			// and the viewport keeps clipping to the frame.
			slog.Debug("tui detail render rejected: width drifted after request",
				"rendered_width", msg.width, "width", m.width, "generation", msg.generation)
			return m, m.rerenderDetailAtWidth(m.width)
		}
		m.detailRenderWidth = msg.width
		if msg.keepScroll {
			// Width re-render of the resource already being read: keep the
			// scroll anchor (same line offset, clamped; sticky bottom when
			// the reader was at the bottom at resize or acceptance time)
			// instead of jumping to the top.
			stickyBottom := m.detailStickyBottom
			m.detailStickyBottom = false
			wasAtBottom := m.detailViewport.AtBottom()
			m.detailViewport.SetContent(msg.content)
			if stickyBottom || wasAtBottom {
				m.detailViewport.GotoBottom()
			}
			return m, nil
		}
		m.detailStickyBottom = false
		m.detailViewport.SetContent(msg.content)
		m.detailViewport.GotoTop()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		// Only keep ticking while there is in-flight work.
		if m.loading || m.syncing || m.refreshing {
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

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Toast auto-expire check
	if !m.toastExp.IsZero() && time.Now().After(m.toastExp) {
		m.toast = ""
	}

	// Ctrl+C always quits immediately regardless of the active pane.
	// This ensures the user can escape the TUI even from overlays or
	// modals. Unlike the 'q' key, Ctrl+C does not prompt for unsaved
	// changes — it is a hard exit that restores the terminal cleanly.
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if m.belowMinimumTerminal() {
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
		return m, nil
	}

	// Overlays take priority
	switch m.pane {
	case paneHelp:
		if key.Matches(msg, m.keys.Help) || key.Matches(msg, m.keys.Clear) {
			m.pane = paneList
			return m, nil
		}
		return m.scrollHelpOverlay(msg)

	case paneDetail:
		return m.handleDetailKey(msg)

	case paneErrors:
		if key.Matches(msg, m.keys.Clear) || key.Matches(msg, m.keys.Errors) {
			// Acknowledgement requires that every operation diagnostic
			// could have been seen: closing a truncated pane (not yet
			// scrolled to the bottom) only closes it. See
			// docs/tui-product-contract.md.
			if m.errorsAcknowledgeable() {
				m.diagnostics.acknowledgeOperations()
			} else {
				slog.Info("tui diagnostics pane closed without acknowledgement: operation diagnostics not fully visible")
			}
			m.pane = paneList
			return m, nil
		}
		return m.scrollErrorsOverlay(msg)

	case paneConfirm:
		return m.handleConfirmKey(msg)

	case paneToggleModal:
		return m.handleToggleModalKey(msg)

	case paneInitModal:
		return m.handleInitModalKey(msg)
	case paneMigrationModal:
		return m.handleMigrationModalKey(msg)
	case paneHarnessModal:
		return m.handleHarnessModalKey(msg)
	case paneEditConfigModal:
		return m.handleEditConfigModalKey(msg)
	}

	// Search mode
	if m.filterActive {
		return m.handleSearchKey(msg)
	}

	// z-prefix: vim-style fold commands (za/zM/zR). Any other key cancels
	// the pending prefix and is processed normally.
	if m.zPending {
		m.zPending = false
		switch {
		case key.Matches(msg, m.keys.FoldToggle):
			m.toggleFoldAtCursor()
			m.rebuildFlatItems()
			m.clampListOffset()
			return m, nil
		case key.Matches(msg, m.keys.FoldAll):
			m.collapseAllSections()
			m.rebuildFlatItems()
			m.cursor = 0
			m.clampListOffset()
			return m, nil
		case key.Matches(msg, m.keys.ExpandAll):
			m.expandAllSections()
			m.rebuildFlatItems()
			m.clampListOffset()
			return m, nil
		default:
			// fall through: treat the key as a fresh input
		}
	}

	// Normal mode
	switch {
	case key.Matches(msg, m.keys.Quit):
		if m.driftReport.PendingCount() > 0 {
			m.pane = paneConfirm
			m.confirmMsg = fmt.Sprintf("%d pending changes not synced. Sync+quit? y/n/esc", m.driftReport.PendingCount())
			return m, nil
		}
		return m, tea.Quit

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

	case key.Matches(msg, m.keys.Top):
		m.cursor = 0
		m.clampListOffset()
		return m, nil

	case key.Matches(msg, m.keys.Bottom):
		m.cursor = max(0, len(m.flatItems)-1)
		m.clampListOffset()
		return m, nil

	case key.Matches(msg, m.keys.Search):
		m.filterActive = true
		m.searchInput.SetValue("")
		cmd := m.searchInput.Focus()
		m.filter = ""
		return m, cmd

	case key.Matches(msg, m.keys.Help):
		m.pane = paneHelp
		return m, nil

	case key.Matches(msg, m.keys.Errors):
		if len(m.diagnostics.snapshot)+len(m.diagnostics.operations) > 0 {
			m.pane = paneErrors
		}
		return m, nil

	case key.Matches(msg, m.keys.Harness):
		m.openHarnessModal()
		return m, nil

	case key.Matches(msg, m.keys.EditConfig):
		m.pane = paneEditConfigModal
		return m, nil

	case key.Matches(msg, m.keys.Enter):
		if _, ok := m.selectedSectionPath(); ok {
			m.toggleFoldAtCursor()
			m.rebuildFlatItems()
			m.clampListOffset()
			return m, nil
		}
		if r, ok := m.selectedRow(); ok {
			if r.isOrphanOutput() {
				return m, nil
			}
			generation := m.beginRequest(requestDetail)
			m.enterDetailPane()
			recordActionSpan(m.ctx, "browse", map[string]string{"tab": tabName(m.activeTab), "view": "detail", "resource.kind": r.Artifact.Kind.String()})
			return m, loadDetailContent(r, generation, m.width, false)
		}
		return m, nil

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
		m.refreshing = false
		generation := m.beginRequest(requestSnapshot)
		return m, loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation)

	case key.Matches(msg, m.keys.Tab1):
		m.switchTab(tabScope)
		recordActionSpan(m.ctx, "browse", map[string]string{"tab": tabName(tabScope)})
		return m, nil

	case key.Matches(msg, m.keys.Tab2):
		m.switchTab(tabRegistry)
		recordActionSpan(m.ctx, "browse", map[string]string{"tab": tabName(tabRegistry)})
		return m, nil

	case key.Matches(msg, m.keys.Tab3):
		m.switchTab(tabStatus)
		recordActionSpan(m.ctx, "browse", map[string]string{"tab": tabName(tabStatus)})
		return m, nil

	case key.Matches(msg, m.keys.NextTab):
		next := nextTab(m.activeTab)
		m.switchTab(next)
		recordActionSpan(m.ctx, "browse", map[string]string{"tab": tabName(next)})
		return m, nil

	case key.Matches(msg, m.keys.PrevTab):
		prev := prevTab(m.activeTab)
		m.switchTab(prev)
		recordActionSpan(m.ctx, "browse", map[string]string{"tab": tabName(prev)})
		return m, nil

	case key.Matches(msg, m.keys.Space):
		if m.syncing {
			return m, nil
		}
		// Space on a section header: toggle fold.
		if _, ok := m.selectedSectionPath(); ok {
			m.toggleFoldAtCursor()
			m.rebuildFlatItems()
			m.clampListOffset()
			return m, nil
		}
		// Space on a skill row: open the toggle modal so the user picks
		// which config file to write.
		r, ok := m.selectedRow()
		if !ok {
			return m, nil
		}
		if r.IsMalformed || r.isOrphanOutput() {
			return m, nil
		}
		if m.rowsStale {
			// The last accepted snapshot carried hard errors: these rows
			// describe state the tool itself flagged as broken. Mutating
			// config from them would act on stale truth.
			return m, func() tea.Msg {
				return toastMsg{text: "resources cannot be changed while errors are unresolved (press e to view)", duration: 3 * time.Second}
			}
		}
		if r.Artifact.Required {
			return m, func() tea.Msg {
				return toastMsg{text: "required items cannot be toggled", duration: 2 * time.Second}
			}
		}
		m.pane = paneToggleModal
		m.modalSkill = r
		return m, nil

	case key.Matches(msg, m.keys.Sync):
		if m.syncing {
			return m, nil
		}
		if m.driftAssessment == driftReady && m.driftReport.InSync() {
			recordActionSpan(m.ctx, "sync", map[string]string{"skipped": "in_sync"})
			return m, func() tea.Msg {
				return toastMsg{text: "already in sync", duration: 2 * time.Second}
			}
		}
		m.refreshing = false
		// No snapshot generation is allocated here: sync schedules no
		// snapshot work, and the syncDoneMsg handler allocates the
		// authoritative post-sync reload generation itself. (A discarded
		// generation would be a phantom: nothing could ever carry it.)
		m.syncing = true
		recordActionSpan(m.ctx, "sync", nil)
		return m, tea.Batch(runSyncCmd(m.ctx, m.repoRoot), m.spinner.Tick)

	case key.Matches(msg, m.keys.Update):
		if m.syncing {
			return m, nil
		}
		m.refreshing = false
		generation := m.beginRequest(requestSnapshot)
		m.syncing = true
		recordActionSpan(m.ctx, "update", nil)
		return m, tea.Batch(runUpdateCmd(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation), m.spinner.Tick)
	}

	return m, nil
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Clear):
		m.filterActive = false
		m.filter = ""
		m.searchInput.SetValue("")
		m.searchInput.Blur()
		m.recomputeView()
		recordActionSpan(m.ctx, "filter", map[string]string{"result_count": strconv.Itoa(len(m.filteredRows))})
		return m, nil

	case key.Matches(msg, m.keys.SearchConfirm):
		m.filterActive = false
		m.searchInput.Blur()
		recordActionSpan(m.ctx, "filter", map[string]string{"result_count": strconv.Itoa(len(m.filteredRows))})
		return m, nil

	default:
		// Delegate to textinput for cursor movement, backspace,
		// paste, and character input.
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		value := sanitizeTerminalData(m.searchInput.Value(), terminalSingleLine)
		if value != m.searchInput.Value() {
			m.searchInput.SetValue(value)
		}
		m.filter = value
		m.recomputeView()
		return m, cmd
	}
}

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.DetailBack):
		m.pane = paneList
		m.detailKey = ""
		return m, nil

	case key.Matches(msg, m.keys.Top):
		m.detailViewport.GotoTop()
		m.detailStickyBottom = false
		return m, nil

	case key.Matches(msg, m.keys.Bottom):
		m.detailViewport.GotoBottom()
		m.detailStickyBottom = false
		return m, nil
	}
	// Delegate all other keys (up/down, j/k, pgup/pgdn, home/end, mouse
	// wheel) to the viewport, which handles scroll offset and clamping.
	// A user scroll supersedes a pending sticky-bottom anchor.
	m.detailStickyBottom = false
	var cmd tea.Cmd
	m.detailViewport, cmd = m.detailViewport.Update(msg)
	return m, cmd
}

// enterDetailPane transitions to the full-screen detail view. The viewport
// is shown immediately with a "Loading…" placeholder; the actual SKILL.md
// read + glamour render happens in a background tea.Cmd so the UI does not
// block on file I/O or markdown rendering.
func (m *Model) enterDetailPane() {
	m.pane = paneDetail

	// Record the stable identity of the resource being opened so snapshot
	// rebuilds and resizes keep the detail pane anchored to it.
	if r, ok := m.selectedRow(); ok {
		m.detailKey = rowKey(r)
	} else {
		m.detailKey = ""
	}

	// Compute viewport dimensions while reserving one line for detail recovery keys.
	m.detailViewport.Width = m.width
	m.detailViewport.Height = max(1, m.height-1)
	m.detailRenderWidth = m.width
	m.detailStickyBottom = false

	// Show a placeholder immediately; the real content arrives via
	// detailContentMsg.
	m.detailViewport.SetContent("Loading…")
	m.detailViewport.GotoTop()
}

// rerenderDetailAtWidth schedules a generation-correlated re-render of the
// open detail resource at a new viewport width, keeping the scroll anchor.
// It returns nil when no detail resource is anchored or the row is gone
// (e.g. after a rebuild removed it): the stale-width content then remains
// until the user reopens the detail, which is safe because the viewport
// still clips to the frame.
func (m *Model) rerenderDetailAtWidth(width int) tea.Cmd {
	if m.detailKey == "" {
		return nil
	}
	for _, r := range m.rows {
		if rowKey(r) == m.detailKey {
			if r.Artifact.Path == "" {
				return nil
			}
			// detailRenderWidth keeps recording the ACCEPTED width; the
			// request is correlated by its generation, and a later resize
			// compares against the width actually on screen.
			generation := m.beginRequest(requestDetail)
			return loadDetailContent(r, generation, width, true)
		}
	}
	return nil
}

func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.ConfirmYes):
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
		slog.Debug("sync+quit confirmed, deferring quit until sync completes")
		return m, tea.Batch(runSyncCmd(m.ctx, m.repoRoot), m.spinner.Tick)
	case key.Matches(msg, m.keys.ConfirmNo):
		m.pane = paneList
		m.quitAfterSync = false
		recordActionSpan(m.ctx, "modal_outcome", map[string]string{"outcome": "cancelled", "modal": "sync+quit"})
		return m, tea.Quit
	case key.Matches(msg, m.keys.ConfirmCancel):
		m.pane = paneList
		m.quitAfterSync = false
		recordActionSpan(m.ctx, "modal_outcome", map[string]string{"outcome": "cancelled", "modal": "sync+quit"})
		return m, nil
	}
	return m, nil
}

func (m Model) View() string {
	if m.belowMinimumTerminal() {
		return renderResizeRequired(m)
	}

	// Full-screen overlays. Both render into their persistent scroll
	// viewports: the content is wrapped at the frame width, the offset is
	// kept across frames, and the footer carries the visible scroll keys.
	switch m.pane {
	case paneHelp:
		m.helpViewport.Width = m.width
		m.helpViewport.Height = max(1, m.height-1)
		m.helpViewport.SetContent(renderHelp(m))
		return fitTerminalWithFooter(m.helpViewport.View(), helpPaneFooter(), m.width, m.height)
	case paneErrors:
		m.errorsViewport.Width = m.width
		m.errorsViewport.Height = max(1, m.height-1)
		m.errorsViewport.SetContent(renderErrors(m))
		return fitTerminalWithFooter(m.errorsViewport.View(), errorsPaneFooter(m), m.width, m.height)
	case paneConfirm:
		return fitTerminalWithFooter(renderConfirm(m), "y sync+quit · n quit · esc cancel", m.width, m.height)
	case paneDetail:
		return fitTerminalWithFooter(m.detailViewport.View(), "esc/q back · ↑/↓ scroll · Ctrl+C quit", m.width, m.height)
	}

	base := m.renderBaseScreen()

	// paneToggleModal renders as an overlay on top of the normal two-pane
	// layout so the user can still see the list behind the modal.
	var modal string
	switch m.pane {
	case paneToggleModal:
		modal = renderToggleModal(m)
	case paneInitModal:
		modal = renderInitModal(m)
	case paneMigrationModal:
		modal = renderMigrationModal(m)
	case paneHarnessModal:
		modal = renderHarnessModal(m)
	case paneEditConfigModal:
		modal = renderEditConfigModal(m)
	}
	if modal == "" {
		return base
	}
	return fitTerminalRows(centerOverlay(base, modal, m.width, m.height), m.width, m.height)
}

// renderBaseScreen renders the standard header + tabs + two-pane layout +
// footer without any overlay. Extracted so overlays can composite on top.
func (m Model) renderBaseScreen() string {
	layout := screenLayoutFor(m.height, m.filterActive)
	if !layout.header {
		return ""
	}

	parts := []string{renderHeader(m)}
	if layout.tabs {
		parts = append(parts, renderTabBar(m.activeTab))
	}
	if layout.search {
		parts = append(parts, m.searchInput.View())
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

type screenLayout struct {
	header     bool
	tabs       bool
	search     bool
	footer     bool
	mainHeight int
}

func screenLayoutFor(height int, filterActive bool) screenLayout {
	if height < 1 {
		return screenLayout{}
	}
	layout := screenLayout{header: true}
	if height == 1 {
		return layout
	}
	layout.footer = true
	remaining := height - 2
	if filterActive && remaining > 0 {
		layout.search = true
		remaining--
	}
	if remaining > 0 {
		layout.tabs = true
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
	if m.filter == "" {
		return m.rows
	}

	filter := strings.ToLower(m.filter)
	filtered := m.filterScratch[:0]
	if cap(filtered) < len(m.rows) {
		filtered = make([]row, 0, len(m.rows))
	}
	for i := range m.rows {
		m.rows[i].ensureSearchable()
		if strings.Contains(m.rows[i].searchableLower, filter) {
			filtered = append(filtered, m.rows[i])
		}
	}
	m.filterScratch = filtered[:0]
	return filtered
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
		m.listHeightPrefix[i+1] = m.listHeightPrefix[i] + flatItemHeight(m.flatItems[i], width)
	}
	m.listHeightWidth = width
	return m.listHeightPrefix
}

// listVisibleHeight computes how many rows fit in the left pane,
// mirroring the same calculation used in View().
func (m Model) listVisibleHeight() int {
	return max(1, screenLayoutFor(m.height, m.filterActive).mainHeight)
}

func Run(ctx context.Context, repoRoot, homeDir string) error {
	// Select terminal styling from the process environment only and seed the
	// lipgloss background cache before anything renders. Active OSC probes can
	// race Bubble Tea's terminal takeover and stall startup.
	applyTerminalBackground()
	initRenderer()

	model := NewModel(repoRoot, homeDir)
	model.ctx = ctxOrBackground(ctx)
	tel := telemetry.TelemetryFromContext(model.ctx)
	if tel != nil {
		model.ctx, model.sessionSpan = tel.StartSpan(model.ctx, "agent_marketplace.tui.session", nil)
	}
	defer model.sessionSpan.End()

	// Seed the real terminal size when it can be determined up front, so the
	// first frame renders at the true size instead of flashing an unknown-size
	// recovery frame until the initial WindowSizeMsg arrives.
	seedTerminalSize(&model, os.Stdout)

	// While the TUI owns the alternate screen, stderr is the live terminal:
	// route logs to the managed file only. The inner terminal guard restores
	// the screen before this deferred logger restoration runs.
	restoreLogs := logging.BeginTerminalSession()
	defer restoreLogs()

	return runTUIModel(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
}

func runTUIModel(model tea.Model, options ...tea.ProgramOption) error {
	return runWithTerminalGuard(defaultTerminalIO(), func() (tea.Model, error) {
		return tea.NewProgram(model, options...).Run()
	})
}

func finishTUIRun(finalModel tea.Model, err error) error {
	if errors.Is(err, tea.ErrInterrupted) {
		slog.Debug("tui exited via interrupt")
		return nil
	}
	if err != nil {
		slog.Error("tui exited with error", "interrupted", false)
		// The caller prints this on the restored terminal (after the
		// guard has left the alternate screen); escape untrusted bytes
		// while keeping the error identity for errors.Is/As.
		return sanitizeReturnedError(err)
	}
	// Surface sync errors from the sync+quit flow so failed syncs don't
	// exit cleanly. The error was already shown as a toast in the TUI;
	// propagating it lets the caller (cobra) print it to stderr. Its text
	// may embed untrusted registry paths/URLs, so it crosses the terminal
	// boundary sanitized.
	if fm, ok := finalModel.(Model); ok && fm.syncQuitErr != nil {
		slog.Error("tui sync+quit reported sync error")
		return sanitizeReturnedError(fm.syncQuitErr)
	}
	return nil
}
