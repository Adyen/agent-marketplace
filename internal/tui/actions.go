package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
)

type reloadMsg snapshotLoadedMsg
type refreshLoadedMsg struct {
	snapshotLoadedMsg
}

type toastMsg struct {
	text     string
	duration time.Duration
}

// toastExpiredMsg is sent by a timer to clear an expired toast.
type toastExpiredMsg struct{}

// toastExpiryCmd returns a tea.Cmd that fires a toastExpiredMsg after d.
func toastExpiryCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return toastExpiredMsg{}
	})
}

// toastStackMax bounds the visible toast stack so transient notices can
// never grow unbounded or bury required controls.
const toastStackMax = 3

type queuedToast struct {
	text string
	exp  time.Time
}

// pushToast adds a toast to the visible stack, keeping the newest
// toastStackMax entries, and mirrors the newest one for the footer segment.
func (m *Model) pushToast(text string, duration time.Duration) {
	exp := time.Now().Add(duration)
	m.toast = text
	m.toastExp = exp
	m.toastQueue = append(m.toastQueue, queuedToast{text: text, exp: exp})
	if len(m.toastQueue) > toastStackMax {
		m.toastQueue = append([]queuedToast(nil), m.toastQueue[len(m.toastQueue)-toastStackMax:]...)
	}
}

// enqueueToast pushes text onto the visible toast stack and schedules its
// expiry tick.
func (m *Model) enqueueToast(text string, duration time.Duration) tea.Cmd {
	m.pushToast(text, duration)
	return toastExpiryCmd(duration)
}

// visibleToasts returns the live toast stack, oldest first. A footer toast
// set outside pushToast (legacy direct write) still renders as a
// single-entry stack.
func (m Model) visibleToasts() []queuedToast {
	now := time.Now()
	var out []queuedToast
	for _, t := range m.toastQueue {
		if now.Before(t.exp) {
			out = append(out, t)
		}
	}
	if len(out) == 0 && m.toast != "" && now.Before(m.toastExp) {
		out = append(out, queuedToast{text: m.toast, exp: m.toastExp})
	}
	return out
}

// layeredToasts returns the toasts rendered in the top-right layer. The
// footer already mirrors the newest toast, so the layer carries only the
// remaining stack and each notification appears exactly once. The error
// footer renders no toast, so there the whole stack goes to the layer.
func (m Model) layeredToasts() []queuedToast {
	toasts := m.visibleToasts()
	if len(m.errs) > 0 {
		return toasts
	}
	if len(toasts) <= 1 {
		return nil
	}
	return toasts[:len(toasts)-1]
}

// expireToast drops every toast whose timer elapsed, mirrors the newest
// survivor for the footer, and re-arms a tick for the next expiry.
func (m *Model) expireToast() tea.Cmd {
	now := time.Now()
	kept := m.toastQueue[:0]
	for _, t := range m.toastQueue {
		if now.Before(t.exp) {
			kept = append(kept, t)
		}
	}
	m.toastQueue = kept
	if !m.toastExp.IsZero() && !now.Before(m.toastExp) {
		m.toast = ""
		m.toastExp = time.Time{}
	}
	if len(m.toastQueue) == 0 {
		return nil
	}
	newest := m.toastQueue[len(m.toastQueue)-1]
	m.toast = newest.text
	m.toastExp = newest.exp
	return toastExpiryCmd(time.Until(m.toastQueue[0].exp))
}

func invalidConfigToastCmd() tea.Cmd {
	return func() tea.Msg {
		return toastMsg{text: "configuration is invalid; press E to edit config", duration: 3 * time.Second}
	}
}

type configWrittenMsg struct {
	operation  operationID
	followUp   bool
	skillName  string
	enabled    bool
	selectNext bool
	err        error
}

type syncDoneMsg struct {
	err      error
	warnings []error
}

type updateDoneMsg struct {
	generation requestGeneration
	snapshot   snapshotLoadedMsg
	stage      string
	err        error
	warnings   []error
}

// detailContentMsg carries the glamour-rendered resource content for the
// full-screen detail view. Produced by loadDetailContent (async tea.Cmd)
// and consumed in Update to populate the viewport.
type detailContentMsg struct {
	generation requestGeneration
	content    string
	width      int
	keepScroll bool
	path       string
}

type snapshotStreamClosedMsg struct{}

// ReadSkillMarkdown reads the SKILL.md content for a given resolved skill path.
func ReadSkillMarkdown(path string) (string, error) {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read resource source: %w", err)
		}
		return string(data), nil
	}
	mdPath := filepath.Join(path, "SKILL.md")
	data, err := os.ReadFile(mdPath)
	if err != nil {
		return "", fmt.Errorf("read SKILL.md: %w", err)
	}
	return string(data), nil
}

// loadDetailContent reads and glamour-renders a skill's SKILL.md in a
// background goroutine so the TUI can transition to the detail pane
// immediately without blocking on file I/O or markdown rendering.
func loadDetailContent(t theme, r row, generation requestGeneration, width int, keepScroll bool) tea.Cmd {
	return func() tea.Msg {
		return detailContentMsg{
			generation: generation,
			content:    renderDetailFullContent(t, r, width),
			width:      width,
			keepScroll: keepScroll,
		}
	}
}

// updateDoneFromCore converts a core update result into the TUI message.
func updateDoneFromCore(result session.UpdateResult) updateDoneMsg {
	return updateDoneMsg{
		generation: result.Generation,
		snapshot:   snapshotMsgFromCore(result.Snapshot, nil),
		stage:      result.Stage,
		err:        result.Err,
		warnings:   result.Warnings,
	}
}

// runSyncCmd runs the authoritative sync pipeline as a tea.Cmd.
// It is a package-level var so tests can override it with a no-op to
// avoid real I/O when inspecting command composition in assertions.
var runSyncCmd = func(ctx context.Context, workspace pipeline.Workspace) tea.Cmd {
	return func() tea.Msg {
		result, err := session.RunSync(ctx, workspace)
		if err != nil {
			return syncDoneMsg{err: err}
		}
		return syncDoneMsg{err: errors.Join(result.Errors...)}
	}
}

func runDependencySyncCmd(ctx context.Context, workspace pipeline.Workspace, sync DependencyLifecycleFunc) tea.Cmd {
	return func() tea.Msg {
		warnings, syncErr := session.RunDependencySync(ctx, workspace, sync)
		return syncDoneMsg{err: syncErr, warnings: warnings}
	}
}

func runDependencyUpdateCmd(ctx context.Context, workspace pipeline.Workspace, homeDir string, generation requestGeneration, update DependencyLifecycleFunc) tea.Cmd {
	return func() tea.Msg {
		return updateDoneFromCore(session.RunDependencyUpdateWithLoader(ctx, workspace, homeDir, generation, update, sessionSnapshotLoader))
	}
}

func (m Model) dependencySyncCmd() tea.Cmd {
	ctx := m.observedContext(ctxOrBackground(m.ctx))
	if m.dependencyLifecycle.Sync != nil {
		return runDependencySyncCmd(ctx, m.workspace, m.dependencyLifecycle.Sync)
	}
	return runSyncCmd(ctx, m.workspace)
}

func (m Model) dependencyUpdateCmd(generation requestGeneration) tea.Cmd {
	ctx := m.observedContext(ctxOrBackground(m.ctx))
	if m.dependencyLifecycle.Update != nil {
		return runDependencyUpdateCmd(ctx, m.workspace, m.homeDir, generation, m.dependencyLifecycle.Update)
	}
	return runUpdateCmdForWorkspace(ctx, m.workspace, m.homeDir, generation)
}

func runUpdateCmdForWorkspace(ctx context.Context, workspace pipeline.Workspace, homeDir string, generation requestGeneration) tea.Cmd {
	return func() tea.Msg {
		return updateDoneFromCore(session.RunUpdateWithLoader(ctx, workspace, homeDir, generation, sessionSnapshotLoader))
	}
}
