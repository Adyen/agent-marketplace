package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const listFrameInterval = time.Second / 60

type framePacedModel struct {
	inner tea.Model
	clock func() time.Time

	view      tea.View
	hasView   bool
	lastFrame time.Time

	framePending    bool
	frameGeneration uint64
}

type framePacedTickMsg struct {
	generation uint64
}

func newFramePacedModel(inner tea.Model) *framePacedModel {
	return &framePacedModel{inner: inner, clock: time.Now}
}

func (m *framePacedModel) Init() tea.Cmd {
	return m.inner.Init()
}

func (m *framePacedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tick, ok := msg.(framePacedTickMsg); ok {
		if !m.framePending || tick.generation != m.frameGeneration {
			return m, nil
		}
		m.framePending = false
		m.refresh(m.clock())
		return m, nil
	}

	before := m.inner
	inner, cmd := m.inner.Update(msg)
	m.inner = inner

	if !pacesResourceListWheel(msg, before, inner) {
		m.cancelPendingFrame()
		m.refresh(m.clock())
		return m, cmd
	}

	if !m.hasView {
		return m, cmd
	}
	now := m.clock()
	if elapsed := now.Sub(m.lastFrame); elapsed >= listFrameInterval {
		m.cancelPendingFrame()
		m.refresh(now)
		return m, cmd
	}
	if !m.framePending {
		m.frameGeneration++
		generation := m.frameGeneration
		m.framePending = true
		remaining := listFrameInterval - now.Sub(m.lastFrame)
		return m, tea.Batch(cmd, tea.Tick(remaining, func(time.Time) tea.Msg {
			return framePacedTickMsg{generation: generation}
		}))
	}
	return m, cmd
}

func (m *framePacedModel) View() tea.View {
	if !m.hasView {
		m.refresh(m.clock())
	}
	return m.view
}

func (m *framePacedModel) refresh(now time.Time) {
	m.view = m.inner.View()
	m.hasView = true
	m.lastFrame = now
}

func (m *framePacedModel) cancelPendingFrame() {
	if m.framePending {
		m.framePending = false
		m.frameGeneration++
	}
}

func pacesResourceListWheel(msg tea.Msg, before, after tea.Model) bool {
	wheel, ok := msg.(tea.MouseWheelMsg)
	if !ok || (wheel.Button != tea.MouseWheelUp && wheel.Button != tea.MouseWheelDown) {
		return false
	}
	for _, candidate := range []tea.Model{before, after} {
		inner, ok := candidate.(Model)
		if !ok || inner.pane != paneList || inner.belowMinimumTerminal() || inner.palette.open {
			return false
		}
	}
	return true
}
