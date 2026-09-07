package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/logging"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
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
		repoRoot:       repoRoot,
		homeDir:        homeDir,
		width:          80,
		height:         24,
		loading:        true,
		activeTab:      tabScope,
		tabStates:      newTabStates(),
		spinner:        sp,
		searchInput:    si,
		detailViewport: viewport.New(80, 24),
		helpViewport:   viewport.New(80, 23),
		errorsViewport: viewport.New(80, 23),
		keys:           defaultKeyMap,
		help:           newHelpModel(),
	}
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

func (m *Model) applySnapshotStatus(msg snapshotLoadedMsg) {
	assessment := msg.driftAssessment
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
}

func (m *Model) applySnapshotData(msg snapshotLoadedMsg) {
	selection := m.selectionKeyForRebuild()
	selectionAfterDisable := m.selectionAfterDisable
	m.selectionAfterDisable = ""
	m.rows = msg.rows
	m.driftReport = msg.driftReport
	m.mergedCfg = msg.mergedCfg
	m.resolved = msg.resolved
	m.localExcludes = msg.localExcludes
	m.globalHarnesses = msg.globalHarnesses
	m.projectHarnesses = msg.projectHarnesses
	m.recomputeView()
	if !m.restoreSelectionKey(selectionAfterDisable) {
		m.restoreSelectionAfterRebuild(selection)
	}
	m.rebuildOpenBundle()
	if msg.inventoryResult != nil {
		pipeline.RecordResolvedInventory(m.ctx, *msg.inventoryResult)
	}
	recordConfigSnapshotSpans(m.ctx, msg)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		oldWidth, oldHeight := m.width, m.height
		detailAtBottom := m.pane == paneDetail && m.detailViewport.AtBottom()
		m.width = msg.Width
		m.height = msg.Height
		m.detailViewport.Width = msg.Width
		m.detailViewport.Height = max(1, msg.Height-1)
		m.helpViewport.Width = msg.Width
		m.helpViewport.Height = max(1, msg.Height-1)
		m.errorsViewport.Width = msg.Width
		m.errorsViewport.Height = max(1, msg.Height-1)
		m.clampListOffset()
		layout := screenLayoutFor(m.height, m.filterActive)
		listWidth, detailWidth := paneWidths(m.width)
		slog.Debug("tui resized", "old_width", oldWidth, "old_height", oldHeight, "width", m.width, "height", m.height, "list_width", listWidth, "detail_width", detailWidth, "visible_height", layout.mainHeight)
		if !m.belowMinimumTerminal() && m.pane == paneDetail && previewWidthFor(msg.Width) != m.detailRenderWidth {
			if rerender := m.rerenderDetailAtWidth(msg.Width); rerender != nil {
				m.detailStickyBottom = detailAtBottom
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
		if m.belowMinimumTerminal() {
			return m, nil
		}
		var cmd tea.Cmd
		switch m.pane {
		case paneDetail:
			m.detailViewport, cmd = m.detailViewport.Update(msg)
		case paneHelp:
			m.helpViewport.SetContent(renderHelp(m))
			m.helpViewport, cmd = m.helpViewport.Update(msg)
		case paneErrors:
			m.errorsViewport.SetContent(renderErrors(m))
			m.errorsViewport, cmd = m.errorsViewport.Update(msg)
		}
		return m, cmd

	case snapshotLoadedMsg:
		if !m.acceptsRequest(requestSnapshot, msg.generation) {
			return m, nil
		}
		m.loading = false
		m.applySnapshotStatus(msg)
		if msg.pendingMigration.hasConfigs() {
			m.migrationPrompt = msg.pendingMigration
			m.pane = paneMigrationModal
			m.refreshing = false
			return m, nil
		}
		if msg.mergedCfg == nil && len(m.errs) > 0 {
			m.refreshing = false
			return m, nil
		}
		m.applySnapshotData(msg)
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
		m.applySnapshotStatus(msg.snapshotLoadedMsg)
		if msg.pendingMigration.hasConfigs() {
			m.migrationPrompt = msg.pendingMigration
			m.pane = paneMigrationModal
			return m, nil
		}
		if msg.mergedCfg == nil && len(m.errs) > 0 {
			return m, nil
		}
		m.applySnapshotData(msg.snapshotLoadedMsg)
		return m, nil

	case reloadMsg:
		snapshot := snapshotLoadedMsg(msg)
		if !m.acceptsRequest(requestSnapshot, snapshot.generation) {
			// Update may have changed disk after a newer snapshot started. Its
			// stale delivery cannot be applied, so schedule a current snapshot
			// rather than leaving either result as authoritative.
			m.syncing = false
			generation := m.beginRequest(requestSnapshot)
			return m, loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation)
		}
		m.loading = false
		m.syncing = false
		m.refreshing = false
		m.applySnapshotStatus(snapshot)
		m.applySnapshotData(snapshot)
		m.toast = "Reloaded"
		m.toastExp = time.Now().Add(2 * time.Second)
		return m, toastExpiryCmd(2 * time.Second)

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
		if msg.err != nil {
			m.finishOperation(msg.operation)
			m.recordOperationFailure(operationMutation, diagnosticMutation, msg.err)
			m.pendingExcludeAction = nil
			m.toast = fmt.Sprintf("error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
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
		m.toast = fmt.Sprintf("%s %s", action, msg.skillName)
		m.toastExp = time.Now().Add(2 * time.Second)
		m.refreshing = false
		if msg.selectNext {
			m.selectionAfterDisable = m.nextRowSelectionKey()
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
		m.finishOperation(msg.operation)
		m.recordOperationSuccess(operationMutation)
		// Re-load snapshot to reflect the config change.
		generation := m.beginRequest(requestSnapshot)
		return m, tea.Batch(loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation), toastExpiryCmd(2*time.Second))

	case harnessWrittenMsg:
		if !m.acceptsOperation(msg.operation) {
			return m, nil
		}
		m.finishOperation(msg.operation)
		if msg.err != nil {
			m.recordOperationFailure(operationTargets, diagnosticMutation, msg.err)
			m.toast = fmt.Sprintf("error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("harness configuration write failed", "scope", msg.scopeLabel, "err", msg.err)
			recordControlledErrorSpan(m.ctx, "harness", msg.err)
			return m, toastExpiryCmd(5 * time.Second)
		}
		m.recordOperationSuccess(operationTargets)
		m.toast = fmt.Sprintf("Saved %s harnesses", msg.scopeLabel)
		m.toastExp = time.Now().Add(2 * time.Second)
		m.refreshing = false
		generation := m.beginRequest(requestSnapshot)
		return m, tea.Batch(loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation), toastExpiryCmd(2*time.Second))

	case syncDoneMsg:
		m.syncing = false
		m.refreshing = false
		if msg.err != nil {
			m.recordOperationFailure(operationSync, diagnosticSync, msg.err)
			m.toast = fmt.Sprintf("sync error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("sync failed", "err", msg.err)
			recordControlledErrorSpan(m.ctx, "sync", msg.err)
		} else {
			m.recordOperationSuccess(operationSync)
			m.toast = "Sync complete"
			m.toastExp = time.Now().Add(2 * time.Second)
		}
		// If the user confirmed "sync+quit", quit now that the sync has
		// actually completed. This avoids the race where tea.Quit in a
		// tea.Batch kills the program before the sync goroutine finishes.
		if m.quitAfterSync {
			m.quitAfterSync = false
			slog.Debug("sync complete, quitting as requested", "err", msg.err)
			m.syncQuitErr = msg.err
			return m, tea.Quit
		}
		// Re-load to pick up on-disk changes.
		generation := m.beginRequest(requestSnapshot)
		return m, tea.Batch(loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation), toastExpiryCmd(5*time.Second))

	case updateDoneMsg:
		if !m.acceptsRequest(requestSnapshot, msg.generation) {
			// A failed update can still leave partial on-disk effects. Re-read
			// them under a new generation without applying this stale outcome.
			m.syncing = false
			generation := m.beginRequest(requestSnapshot)
			return m, loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation)
		}
		m.syncing = false
		if msg.snapshot.generation != 0 {
			m.applySnapshotStatus(msg.snapshot)
			m.applySnapshotData(msg.snapshot)
		}
		// Clear quitAfterSync so a stale flag from a previous sync+quit
		// confirm doesn't cause a later unrelated sync to quit.
		m.quitAfterSync = false
		if msg.err != nil {
			m.recordOperationFailure(operationUpdate, diagnosticUpdate, msg.err)
			m.toast = fmt.Sprintf("update error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("update failed", "err", msg.err)
			recordControlledErrorSpan(m.ctx, "update", msg.err)
			return m, toastExpiryCmd(5 * time.Second)
		} else {
			m.recordOperationSuccess(operationUpdate)
			m.toast = "Update complete"
			m.toastExp = time.Now().Add(2 * time.Second)
		}
		return m, toastExpiryCmd(2 * time.Second)

	case migrationDoneMsg:
		if msg.err != nil {
			m.recordOperationFailure(operationMigration, diagnosticMigration, msg.err)
			m.toast = fmt.Sprintf("migration error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("migration failed", "count", msg.count, "err", msg.err)
			recordControlledErrorSpan(m.ctx, "migrate", msg.err)
			return m, toastExpiryCmd(5 * time.Second)
		}
		m.recordOperationSuccess(operationMigration)
		m.toast = fmt.Sprintf("Migrated %d config file(s)", msg.count)
		m.toastExp = time.Now().Add(2 * time.Second)
		m.migrationPrompt = migrationPrompt{}
		generation := m.beginRequest(requestSnapshot)
		return m, tea.Batch(loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation), toastExpiryCmd(2*time.Second))

	case editConfigPathMsg:
		if !m.acceptsOperation(msg.operation) {
			return m, nil
		}
		m.finishOperation(msg.operation)
		if msg.err != nil {
			m.recordOperationFailure(operationEditor, diagnosticEditor, msg.err)
			m.toast = fmt.Sprintf("editor error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("config editor result handled", "path", msg.path, "reload", false, "err", msg.err)
			recordControlledErrorSpan(m.ctx, "edit_config", msg.err)
			return m, toastExpiryCmd(5 * time.Second)
		}
		m.recordOperationSuccess(operationEditor)
		slog.Debug("config editor result handled", "path", msg.path, "reload", true)
		generation := m.beginRequest(requestSnapshot)
		return m, loadSnapshot(ctxOrBackground(m.ctx), m.repoRoot, m.homeDir, generation)

	case detailContentMsg:
		if m.pane != paneDetail || (msg.path != "" && msg.path != m.detailSelectedPath) || !m.acceptsRequest(requestDetail, msg.generation) {
			return m, nil
		}
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
			offset := m.detailViewport.YOffset
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

func (m Model) handleViewportPageKey(msg tea.KeyMsg, viewport *viewport.Model) bool {
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
		m.filterActive = true
		m.searchInput.SetValue("")
		cmd := m.searchInput.Focus()
		m.filter = ""
		return m, cmd

	case key.Matches(msg, m.keys.Help):
		m.pane = paneHelp
		return m, nil

	case key.Matches(msg, m.keys.Errors):
		if len(m.errs) > 0 || len(m.warnings) > 0 {
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
			m.enterDetailPane()
			generation := m.beginRequest(requestDetail)
			recordActionSpan(m.ctx, "browse", map[string]string{"tab": tabName(m.activeTab), "view": "detail", "resource.kind": r.Artifact.Kind.String()})
			if m.bundle == nil || m.detailSelectedPath == "" {
				return m, nil
			}
			return m, loadBundleDetailContent(r, m.bundle.root, m.detailSelectedPath, generation, m.previewWidth(), false)
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
		if m.rowsStale {
			return m, func() tea.Msg {
				return toastMsg{text: "reload before changing broken state", duration: 2 * time.Second}
			}
		}
		r, ok := m.selectedRow()
		if !ok {
			return m, nil
		}
		if r.IsMalformed {
			return m, nil
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
		// Sync does not allocate a snapshot generation until syncDoneMsg
		// actually schedules the authoritative post-sync reload.
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
		m.filter = m.searchInput.Value()
		m.recomputeView()
		return m, cmd
	}
}

func (m *Model) rebuildOpenBundle() {
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
	if !found {
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

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			return m, loadBundleDetailContent(r, m.bundle.root, m.detailSelectedPath, generation, m.previewWidth(), keepScroll)
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
		bundle, err := bundleTreeForRow(r)
		if err != nil {
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
	m.detailViewport.Width = m.width
	m.detailViewport.Height = max(1, m.height-1)
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
		if rowKey(r) != m.detailKey || r.Artifact.Path == "" {
			continue
		}
		generation := m.beginRequest(requestDetail)
		if m.detailLegacyRender || m.bundle == nil || m.detailSelectedPath == "" {
			return loadDetailContent(r, generation, width, true)
		}
		return loadBundleDetailContent(r, m.bundle.root, m.detailSelectedPath, generation, previewWidthFor(width), true)
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

	// Full-screen overlays.
	switch m.pane {
	case paneHelp:
		m.helpViewport.Width = m.width
		m.helpViewport.Height = max(1, m.height-1)
		m.helpViewport.SetContent(renderHelp(m))
		return fitTerminalWithFooter(m.helpViewport.View(), "↑/↓ or ctrl-d/u scroll · esc/? close · Ctrl+C quit", m.width, m.height)
	case paneErrors:
		m.errorsViewport.Width = m.width
		m.errorsViewport.Height = max(1, m.height-1)
		m.errorsViewport.SetContent(renderErrors(m))
		return fitTerminalWithFooter(m.errorsViewport.View(), "↑/↓ or ctrl-d/u scroll · esc/e close · Ctrl+C quit", m.width, m.height)
	case paneConfirm:
		return fitTerminalWithFooter(renderConfirm(m), "y sync+quit · n quit · esc cancel", m.width, m.height)
	case paneDetail:
		listWidth, previewWidth := paneWidths(m.width)
		bodyHeight := max(1, m.height-1)
		preview := m.detailViewport
		preview.Width = max(1, previewWidth-1)
		preview.Height = bodyHeight
		tree := renderBundleTree(m, listWidth, bodyHeight)
		joined := lipgloss.JoinHorizontal(lipgloss.Top, tree, preview.View())
		footer := "esc/q back · ↑/↓/j/k navigate · h/l fold · enter toggle · pgup/pgdn preview · Ctrl+C quit"
		if compactTerminalLayout(m) {
			footer = "esc/q back · Ctrl+C quit"
		}
		return fitTerminalWithFooter(joined, footer, m.width, m.height)
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
	return fitTerminal(centerOverlay(base, modal, m.width, m.height), m.width, m.height)
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
	applyTerminalBackground()
	initRenderer()

	model := NewModel(repoRoot, homeDir)
	model.ctx = ctxOrBackground(ctx)
	tel := telemetry.TelemetryFromContext(model.ctx)
	if tel != nil {
		model.ctx, model.sessionSpan = tel.StartSpan(model.ctx, "agent_marketplace.tui.session", nil)
	}
	defer model.sessionSpan.End()

	seedTerminalSize(&model, os.Stdout)
	restoreLogs := logging.BeginTerminalSession()
	defer restoreLogs()

	return runTUIModel(model, defaultTUIProgramOptions()...)
}

func defaultTUIProgramOptions() []tea.ProgramOption {
	return []tea.ProgramOption{tea.WithAltScreen()}
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
		return sanitizeReturnedError(err)
	}
	if fm, ok := finalModel.(Model); ok && fm.syncQuitErr != nil {
		slog.Error("tui sync+quit reported sync error")
		return sanitizeReturnedError(fm.syncQuitErr)
	}
	return nil
}
