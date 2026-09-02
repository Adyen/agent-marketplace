package tui

import (
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type requestKind string

type requestGeneration uint64

const (
	requestDetail    requestKind = "detail"
	requestSnapshot  requestKind = "snapshot"
	requestDiscovery requestKind = "marketplace-discovery"
)

type requestState struct {
	next      requestGeneration
	detail    requestGeneration
	snapshot  requestGeneration
	discovery requestGeneration
}

func (r *requestState) begin(kind requestKind) requestGeneration {
	switch kind {
	case requestDetail, requestSnapshot, requestDiscovery:
	default:
		return 0
	}

	r.next++
	switch kind {
	case requestDetail:
		r.detail = r.next
	case requestSnapshot:
		r.snapshot = r.next
	case requestDiscovery:
		r.discovery = r.next
	}
	return r.next
}

func (r requestState) active(kind requestKind) requestGeneration {
	switch kind {
	case requestDetail:
		return r.detail
	case requestSnapshot:
		return r.snapshot
	case requestDiscovery:
		return r.discovery
	default:
		return 0
	}
}

func (r *requestState) cancel(kind requestKind) requestGeneration {
	generation := r.active(kind)
	switch kind {
	case requestDetail:
		r.detail = 0
	case requestSnapshot:
		r.snapshot = 0
	case requestDiscovery:
		r.discovery = 0
	}
	return generation
}

func (m *Model) beginRequest(kind requestKind) requestGeneration {
	generation := m.requests.begin(kind)
	slog.Debug("tui request created", "kind", kind, "generation", generation)
	return generation
}

func (m *Model) cancelRequest(kind requestKind) {
	if generation := m.requests.cancel(kind); generation != 0 {
		slog.Debug("tui request cancelled", "kind", kind, "generation", generation)
	}
}

func (m *Model) acceptsRequest(kind requestKind, generation requestGeneration) bool {
	active := m.requests.active(kind)
	accepted := generation != 0 && generation == active
	if accepted {
		m.requests.cancel(kind)
		slog.Debug("tui request completion accepted", "kind", kind, "generation", generation)
	} else {
		slog.Debug("tui request completion rejected", "kind", kind, "generation", generation, "active_generation", active)
	}
	return accepted
}

type operationID uint64

type activeOperation struct {
	id   operationID
	kind operationKind
}

type operationState struct {
	next   operationID
	active *activeOperation
}

func (s *operationState) begin(kind operationKind) (operationID, bool) {
	if s.active != nil {
		return 0, false
	}
	s.next++
	s.active = &activeOperation{id: s.next, kind: kind}
	return s.next, true
}

func (s operationState) accepts(id operationID) bool {
	return id != 0 && s.active != nil && s.active.id == id
}

func (s *operationState) finish(id operationID) {
	if s.accepts(id) {
		s.active = nil
	}
}

func (m *Model) beginOperation(kind operationKind) (operationID, bool) {
	id, ok := m.operations.begin(kind)
	if ok {
		slog.Debug("tui operation started", "operation", kind, "operation_id", id)
	}
	return id, ok
}

func (m Model) acceptsOperation(id operationID) bool {
	accepted := m.operations.accepts(id)
	if !accepted {
		var active operationID
		if m.operations.active != nil {
			active = m.operations.active.id
		}
		slog.Debug("tui operation completion rejected", "operation_id", id, "active_operation_id", active)
	}
	return accepted
}

func (m *Model) finishOperation(id operationID) {
	m.operations.finish(id)
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
		case editConfigPathMsg:
			msg.operation = id
			return msg
		default:
			return msg
		}
	}
}
