package tui

import "log/slog"

type requestKind string

type requestGeneration uint64

const (
	requestDetail   requestKind = "detail"
	requestSnapshot requestKind = "snapshot"
)

type requestState struct {
	next     requestGeneration
	detail   requestGeneration
	snapshot requestGeneration
}

func (r *requestState) begin(kind requestKind) requestGeneration {
	switch kind {
	case requestDetail, requestSnapshot:
	default:
		return 0
	}

	r.next++
	switch kind {
	case requestDetail:
		r.detail = r.next
	case requestSnapshot:
		r.snapshot = r.next
	}
	return r.next
}

func (r requestState) active(kind requestKind) requestGeneration {
	switch kind {
	case requestDetail:
		return r.detail
	case requestSnapshot:
		return r.snapshot
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
