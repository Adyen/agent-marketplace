package tui

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	return tea.Batch(loadSnapshot(m.repoRoot, m.homeDir), m.spinner.Tick)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.detailViewport.Width = msg.Width
		m.detailViewport.Height = msg.Height
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
		if m.pane == paneDetail {
			var cmd tea.Cmd
			m.detailViewport, cmd = m.detailViewport.Update(msg)
			return m, cmd
		}
		return m, nil

	case snapshotLoadedMsg:
		m.loading = false
		if msg.pendingMigration.hasConfigs() {
			m.errs = msg.errs
			m.warnings = msg.warnings
			m.migrationPrompt = msg.pendingMigration
			m.pane = paneMigrationModal
			m.refreshing = false
			m.refreshToken = 0
			return m, nil
		}
		if msg.mergedCfg == nil && len(msg.errs) > 0 {
			m.errs = append(m.errs, msg.errs...)
			m.warnings = msg.warnings
			m.refreshing = false
			m.refreshToken = 0
			return m, nil
		}
		m.rows = msg.rows
		m.driftReport = msg.driftReport
		m.errs = msg.errs
		m.warnings = msg.warnings
		m.mergedCfg = msg.mergedCfg
		m.resolved = msg.resolved
		m.localExcludes = msg.localExcludes
		m.globalHarnesses = msg.globalHarnesses
		m.projectHarnesses = msg.projectHarnesses
		m.recomputeView()
		if msg.refreshAfterLoad {
			m.refreshing = true
			m.nextRefreshToken++
			m.refreshToken = m.nextRefreshToken
			return m, tea.Batch(refreshSnapshot(m.repoRoot, m.homeDir, m.refreshToken), m.spinner.Tick)
		}
		m.refreshing = false
		m.refreshToken = 0
		return m, nil

	case refreshLoadedMsg:
		if msg.refreshToken != m.refreshToken {
			return m, nil
		}
		m.refreshing = false
		m.refreshToken = 0
		if msg.pendingMigration.hasConfigs() {
			m.errs = msg.errs
			m.migrationPrompt = msg.pendingMigration
			m.pane = paneMigrationModal
			return m, nil
		}
		if msg.mergedCfg == nil && len(msg.errs) > 0 {
			m.errs = append(m.errs, msg.errs...)
			return m, nil
		}
		m.rows = msg.rows
		m.driftReport = msg.driftReport
		m.errs = msg.errs
		m.mergedCfg = msg.mergedCfg
		m.resolved = msg.resolved
		m.localExcludes = msg.localExcludes
		m.globalHarnesses = msg.globalHarnesses
		m.projectHarnesses = msg.projectHarnesses
		m.recomputeView()
		return m, nil

	case reloadMsg:
		m.loading = false
		m.syncing = false
		m.refreshing = false
		m.refreshToken = 0
		m.rows = msg.rows
		m.driftReport = msg.driftReport
		m.errs = msg.errs
		m.warnings = msg.warnings
		m.mergedCfg = msg.mergedCfg
		m.resolved = msg.resolved
		m.localExcludes = msg.localExcludes
		m.globalHarnesses = msg.globalHarnesses
		m.projectHarnesses = msg.projectHarnesses
		m.recomputeView()
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
		if msg.err != nil {
			m.errs = append(m.errs, msg.err)
			m.pendingExcludeAction = nil
			m.toast = fmt.Sprintf("error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("config write failed", "resource", msg.skillName, "enabled", msg.enabled, "err", msg.err)
			return m, toastExpiryCmd(5 * time.Second)
		}
		action := "Enabled"
		if !msg.enabled {
			action = "Disabled"
		}
		m.toast = fmt.Sprintf("%s %s", action, msg.skillName)
		m.toastExp = time.Now().Add(2 * time.Second)
		m.refreshing = false
		m.refreshToken = 0
		// If a collision-aware enable was pending, emit the exclude
		// command now that the primary toggle has succeeded.
		if msg.enabled && m.pendingExcludeAction != nil {
			act := *m.pendingExcludeAction
			m.pendingExcludeAction = nil
			exclCmd := excludeCompetingRegistriesCmd(m.repoRoot, m.rows, act)
			// Re-load snapshot to reflect the config change, then
			// the exclude cmd runs concurrently.
			return m, tea.Batch(loadSnapshot(m.repoRoot, m.homeDir), exclCmd, toastExpiryCmd(2*time.Second))
		}
		m.pendingExcludeAction = nil
		// Re-load snapshot to reflect the config change
		return m, tea.Batch(loadSnapshot(m.repoRoot, m.homeDir), toastExpiryCmd(2*time.Second))

	case harnessWrittenMsg:
		if msg.err != nil {
			m.errs = append(m.errs, msg.err)
			m.toast = fmt.Sprintf("error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("harness configuration write failed", "scope", msg.scopeLabel, "err", msg.err)
			return m, toastExpiryCmd(5 * time.Second)
		}
		m.toast = fmt.Sprintf("Saved %s harnesses", msg.scopeLabel)
		m.toastExp = time.Now().Add(2 * time.Second)
		m.refreshing = false
		m.refreshToken = 0
		return m, tea.Batch(loadSnapshot(m.repoRoot, m.homeDir), toastExpiryCmd(2*time.Second))

	case syncDoneMsg:
		m.syncing = false
		m.refreshing = false
		m.refreshToken = 0
		if msg.err != nil {
			m.errs = append(m.errs, msg.err)
			m.toast = fmt.Sprintf("sync error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("sync failed", "err", msg.err)
		} else {
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
		// Re-load to pick up on-disk changes
		return m, tea.Batch(loadSnapshot(m.repoRoot, m.homeDir), toastExpiryCmd(5*time.Second))

	case updateDoneMsg:
		m.syncing = false
		// Clear quitAfterSync so a stale flag from a previous sync+quit
		// confirm doesn't cause a later unrelated sync to quit.
		m.quitAfterSync = false
		if msg.err != nil {
			m.errs = append(m.errs, msg.err)
			m.toast = fmt.Sprintf("update error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("update failed", "err", msg.err)
			return m, toastExpiryCmd(5 * time.Second)
		} else {
			m.toast = "Update complete"
			m.toastExp = time.Now().Add(2 * time.Second)
		}
		return m, toastExpiryCmd(2 * time.Second)

	case migrationDoneMsg:
		if msg.err != nil {
			m.errs = append(m.errs, msg.err)
			m.toast = fmt.Sprintf("migration error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("migration failed", "count", msg.count, "err", msg.err)
			return m, toastExpiryCmd(5 * time.Second)
		}
		m.toast = fmt.Sprintf("Migrated %d config file(s)", msg.count)
		m.toastExp = time.Now().Add(2 * time.Second)
		m.migrationPrompt = migrationPrompt{}
		return m, tea.Batch(loadSnapshot(m.repoRoot, m.homeDir), toastExpiryCmd(2*time.Second))

	case editConfigPathMsg:
		if msg.err != nil {
			m.errs = append(m.errs, msg.err)
			m.toast = fmt.Sprintf("editor error: %v", msg.err)
			m.toastExp = time.Now().Add(5 * time.Second)
			slog.Error("config editor result handled", "path", msg.path, "reload", false, "err", msg.err)
			return m, toastExpiryCmd(5 * time.Second)
		}
		slog.Debug("config editor result handled", "path", msg.path, "reload", true)
		return m, loadSnapshot(m.repoRoot, m.homeDir)

	case detailContentMsg:
		// The background glamour render completed; fill the viewport.
		if m.pane == paneDetail {
			m.detailViewport.SetContent(msg.content)
			m.detailViewport.GotoTop()
		}
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

	// Overlays take priority
	switch m.pane {
	case paneHelp:
		if key.Matches(msg, m.keys.Help) || key.Matches(msg, m.keys.Clear) {
			m.pane = paneList
			return m, nil
		}
		return m, nil

	case paneDetail:
		return m.handleDetailKey(msg)

	case paneErrors:
		if key.Matches(msg, m.keys.Clear) || key.Matches(msg, m.keys.Errors) {
			m.pane = paneList
			return m, nil
		}
		return m, nil

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
			return m, loadDetailContent(r)
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
		m.refreshToken = 0
		return m, loadSnapshot(m.repoRoot, m.homeDir)

	case key.Matches(msg, m.keys.Tab1):
		m.switchTab(tabScope)
		return m, nil

	case key.Matches(msg, m.keys.Tab2):
		m.switchTab(tabRegistry)
		return m, nil

	case key.Matches(msg, m.keys.Tab3):
		m.switchTab(tabStatus)
		return m, nil

	case key.Matches(msg, m.keys.NextTab):
		m.switchTab(nextTab(m.activeTab))
		return m, nil

	case key.Matches(msg, m.keys.PrevTab):
		m.switchTab(prevTab(m.activeTab))
		return m, nil

	case key.Matches(msg, m.keys.Space):
		if m.syncing {
			return m, nil
		}
		m.refreshing = false
		m.refreshToken = 0
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
		if m.driftReport.InSync() {
			return m, func() tea.Msg {
				return toastMsg{text: "already in sync", duration: 2 * time.Second}
			}
		}
		m.refreshing = false
		m.refreshToken = 0
		m.syncing = true
		return m, tea.Batch(runSyncCmd(m.repoRoot), m.spinner.Tick)

	case key.Matches(msg, m.keys.Update):
		if m.syncing {
			return m, nil
		}
		m.refreshing = false
		m.refreshToken = 0
		m.syncing = true
		return m, tea.Batch(runUpdateCmd(m.repoRoot, m.homeDir), m.spinner.Tick)
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
		return m, nil

	case key.Matches(msg, m.keys.SearchConfirm):
		m.filterActive = false
		m.searchInput.Blur()
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

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.DetailBack):
		m.pane = paneList
		return m, nil

	case key.Matches(msg, m.keys.Top):
		m.detailViewport.GotoTop()
		return m, nil

	case key.Matches(msg, m.keys.Bottom):
		m.detailViewport.GotoBottom()
		return m, nil
	}
	// Delegate all other keys (up/down, j/k, pgup/pgdn, home/end, mouse
	// wheel) to the viewport, which handles scroll offset and clamping.
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

	// Compute full-screen viewport dimensions (no header/footer in detail
	// overlay — the viewport fills the terminal).
	m.detailViewport.Width = m.width
	m.detailViewport.Height = m.height

	// Show a placeholder immediately; the real content arrives via
	// detailContentMsg.
	m.detailViewport.SetContent("Loading…")
	m.detailViewport.GotoTop()
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
		return m, tea.Batch(runSyncCmd(m.repoRoot), m.spinner.Tick)
	case key.Matches(msg, m.keys.ConfirmNo):
		m.pane = paneList
		m.quitAfterSync = false
		return m, tea.Quit
	case key.Matches(msg, m.keys.ConfirmCancel):
		m.pane = paneList
		m.quitAfterSync = false
		return m, nil
	}
	return m, nil
}

func (m Model) View() string {
	// Full-screen overlays.
	switch m.pane {
	case paneHelp:
		return renderHelp(m)
	case paneErrors:
		return renderErrors(m)
	case paneConfirm:
		return renderConfirm(m)
	case paneDetail:
		return m.detailViewport.View()
	}

	base := m.renderBaseScreen()

	// paneToggleModal renders as an overlay on top of the normal two-pane
	// layout so the user can still see the list behind the modal.
	if m.pane == paneToggleModal {
		modal := renderToggleModal(m)
		return centerOverlay(base, modal, m.width, m.height)
	}

	if m.pane == paneInitModal {
		modal := renderInitModal(m)
		return centerOverlay(base, modal, m.width, m.height)
	}

	if m.pane == paneMigrationModal {
		modal := renderMigrationModal(m)
		return centerOverlay(base, modal, m.width, m.height)
	}

	if m.pane == paneHarnessModal {
		modal := renderHarnessModal(m)
		return centerOverlay(base, modal, m.width, m.height)
	}

	if m.pane == paneEditConfigModal {
		modal := renderEditConfigModal(m)
		return centerOverlay(base, modal, m.width, m.height)
	}

	return base
}

// renderBaseScreen renders the standard header + tabs + two-pane layout +
// footer without any overlay. Extracted so overlays can composite on top.
func (m Model) renderBaseScreen() string {
	header := renderHeader(m)
	tabBar := renderTabBar(m.activeTab)
	footer := renderFooter(m)

	mainHeight := m.height - lipgloss.Height(header) - lipgloss.Height(tabBar) - lipgloss.Height(footer)
	if mainHeight < 1 {
		mainHeight = 1
	}

	listWidth := m.width * 2 / 5
	if listWidth < 20 {
		listWidth = 20
	}
	detailWidth := m.width - listWidth
	if detailWidth < 20 {
		detailWidth = 20
	}

	listContent := renderList(m, mainHeight)
	detailContent := renderDetail(m)

	listBox := lipgloss.NewStyle().
		Width(listWidth).
		Height(mainHeight).
		Render(listContent)

	detailBox := lipgloss.NewStyle().
		Width(detailWidth).
		Height(mainHeight).
		PaddingLeft(1).
		Render(detailContent)

	panes := lipgloss.JoinHorizontal(lipgloss.Top, listBox, detailBox)

	searchLine := ""
	if m.filterActive {
		searchLine = m.searchInput.View() + "\n"
	}

	return header + "\n" + tabBar + "\n" + searchLine + panes + "\n" + footer
}

func renderHeader(m Model) string {
	repoName := filepathBase(m.repoRoot)
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
	vh := m.listVisibleHeight()
	if vh < 1 {
		vh = 1
	}

	// Cursor above the visible window
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	}
	// Cursor below the visible window
	if m.cursor >= m.listOffset+vh {
		m.listOffset = m.cursor - vh + 1
	}

	// Clamp to valid range
	maxOffset := max(0, len(m.flatItems)-vh)
	if m.listOffset > maxOffset {
		m.listOffset = maxOffset
	}
	if m.listOffset < 0 {
		m.listOffset = 0
	}
}

// listVisibleHeight computes how many rows fit in the left pane,
// mirroring the same calculation used in View().
func (m Model) listVisibleHeight() int {
	header := renderHeader(m)
	tabBar := renderTabBar(m.activeTab)
	footer := renderFooter(m)
	h := m.height - lipgloss.Height(header) - lipgloss.Height(tabBar) - lipgloss.Height(footer)
	if m.filterActive {
		h-- // search line
	}
	if h < 1 {
		h = 1
	}
	return h
}

func Run(repoRoot, homeDir string) error {
	// Initialise the glamour renderer while we still own the terminal
	// (before tea.WithAltScreen swaps to the alt buffer).  WithAutoStyle
	// sends an OSC 11 query to detect dark/light background; inside
	// alt-screen the response is swallowed, causing a 5 s timeout.
	initRenderer()

	m := NewModel(repoRoot, homeDir)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	finalModel, err := p.Run()
	// An interrupt (e.g. external SIGINT) is a legitimate exit path,
	// not an error worth reporting to the user.
	if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, tea.ErrProgramKilled) {
		slog.Debug("tui exited via interrupt", "err", err)
		return nil
	}
	if err != nil {
		slog.Error("tui exited with error", "err", err)
		return err
	}
	// Surface sync errors from the sync+quit flow so failed syncs don't
	// exit cleanly. The error was already shown as a toast in the TUI;
	// propagating it lets the caller (cobra) print it to stderr.
	if fm, ok := finalModel.(Model); ok && fm.syncQuitErr != nil {
		slog.Error("tui sync+quit reported sync error", "err", fm.syncQuitErr)
		return fm.syncQuitErr
	}
	return nil
}
