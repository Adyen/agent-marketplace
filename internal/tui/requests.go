package tui

import (
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) beginRequest(kind requestKind) requestGeneration {
	generation := m.requests.Begin(kind)
	if kind == requestDiscovery {
		m.discoverySequence = 0
	}
	slog.Debug("tui request created", "kind", kind, "generation", generation)
	return generation
}

func (m *Model) cancelRequest(kind requestKind) {
	if generation := m.requests.Cancel(kind); generation != 0 {
		slog.Debug("tui request cancelled", "kind", kind, "generation", generation)
	}
}

func (m *Model) acceptsRequest(kind requestKind, generation requestGeneration) bool {
	active := m.requests.Active(kind)
	accepted := generation != 0 && generation == active
	if accepted {
		m.requests.Cancel(kind)
		slog.Debug("tui request completion accepted", "kind", kind, "generation", generation)
	} else {
		slog.Debug("tui request completion rejected", "kind", kind, "generation", generation, "active_generation", active)
	}
	return accepted
}

func (m *Model) beginOperation(kind operationKind) (operationID, bool) {
	var external operationID
	if m.operationGate != nil {
		reserved, ok := m.operationGate.Begin(kind)
		if !ok {
			slog.Debug("tui operation blocked by shared gate", "operation", kind)
			return 0, false
		}
		external = reserved
	}
	id, ok := m.operations.Begin(kind)
	if !ok {
		if m.operationGate != nil {
			m.operationGate.Finish(external)
		}
		return 0, false
	}
	m.externalOperation = external
	slog.Debug("tui operation started", "operation", kind, "operation_id", id)
	return id, true
}

func (m Model) acceptsOperation(id operationID) bool {
	accepted := m.operations.Accepts(id)
	if !accepted {
		var active operationID
		if m.operations.Active != nil {
			active = m.operations.Active.ID
		}
		slog.Debug("tui operation completion rejected", "operation_id", id, "active_operation_id", active)
	}
	return accepted
}

func (m *Model) finishOperation(id operationID) {
	if m.operationGate == nil {
		m.operations.Finish(id)
		return
	}
	if !m.operations.Accepts(id) {
		return
	}
	m.operations.Finish(id)
	m.operationGate.Finish(m.externalOperation)
	m.externalOperation = 0
}

func (m *Model) beginDependencyOperation(kind operationKind) bool {
	if m.operationGate == nil {
		return true
	}
	id, ok := m.operationGate.Begin(kind)
	if !ok {
		slog.Debug("tui dependency operation blocked by shared gate", "operation", kind)
		return false
	}
	m.dependencyOperation = id
	return true
}

func (m *Model) finishDependencyOperation() {
	if m.operationGate == nil {
		return
	}
	m.operationGate.Finish(m.dependencyOperation)
	m.dependencyOperation = 0
}

func operationBlockedCmd() tea.Cmd {
	return func() tea.Msg {
		return toastMsg{text: "another operation is in progress", duration: 2 * time.Second}
	}
}

func correlateOperation(cmd tea.Cmd, id operationID, followUp bool) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		switch msg := cmd().(type) {
		case configWrittenMsg:
			msg.operation = id
			msg.followUp = followUp
			return msg
		case harnessWrittenMsg:
			msg.operation = id
			return msg
		default:
			return msg
		}
	}
}
