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

func (m *Model) beginRequest(kind requestKind) requestGeneration {
	generation := m.requests.begin(kind)
	slog.Debug("tui request started", "kind", kind, "generation", generation)
	return generation
}

func (m Model) acceptsRequest(kind requestKind, generation requestGeneration) bool {
	accepted := generation != 0 && generation == m.requests.active(kind)
	if accepted {
		slog.Debug("tui request completion accepted", "kind", kind, "generation", generation)
	} else {
		slog.Debug("tui stale request completion rejected", "kind", kind, "generation", generation, "active_generation", m.requests.active(kind))
	}
	return accepted
}

// operationID identifies one mutation-class operation (resource toggle,
// harness save, migration, editor). It is allocated when the operation
// starts and carried by its result message, so a late or duplicated
// completion is correlated with — or rejected against — the operation that
// is actually active.
type operationID uint64

// competingRegistry is the registry identity a collision follow-up must
// exclude, captured when the operation starts so a reload landing mid-flight
// cannot change what the operation acts on.
type competingRegistry struct {
	url  string
	ref  string
	path string
}

// collisionFollowUp is operation-owned collision state. It replaces the
// former Model-level pendingExcludeAction singleton: the competing-registry
// excludes are captured at operation start and written inside the same
// operation, so the primary include and the competing excludes complete as
// one result instead of a second, uncorrelated write.
type collisionFollowUp struct {
	competing []competingRegistry
}

// activeOperation is the currently in-flight mutation-class operation.
type activeOperation struct {
	id        operationID
	kind      operationKind
	collision *collisionFollowUp
}

// operationState tracks at most one in-flight mutation-class operation.
// While one is active, starting a second is blocked so sequential user
// actions can never interleave config writes or cross-apply results.
type operationState struct {
	next   operationID
	active *activeOperation
}

func (s *operationState) begin(kind operationKind, collision *collisionFollowUp) (operationID, bool) {
	if s.active != nil {
		return 0, false
	}
	s.next++
	s.active = &activeOperation{id: s.next, kind: kind, collision: collision}
	return s.next, true
}

func (s operationState) accepts(id operationID) bool {
	return id != 0 && s.active != nil && s.active.id == id
}

// finish clears the active operation when id matches it. The operation-owned
// collision state is dropped with the operation.
func (s *operationState) finish(id operationID) {
	if s.active != nil && s.active.id == id {
		s.active = nil
	}
}

// beginOperation allocates an operation ID and marks the operation active.
// ok=false — start-blocked — when another mutation-class operation is still
// active; the caller must not schedule work in that case.
func (m *Model) beginOperation(kind operationKind, collision *collisionFollowUp) (operationID, bool) {
	id, ok := m.operations.begin(kind, collision)
	if !ok {
		slog.Debug("tui operation start blocked", "operation", kind, "active_operation", m.operations.active.kind, "active_operation_id", m.operations.active.id)
		return 0, false
	}
	slog.Debug("tui operation started", "operation", kind, "operation_id", id, "collision_follow_up", collision != nil)
	return id, true
}

// acceptsOperation reports whether a result carrying id belongs to the
// currently active operation. Rejected (stale or never-started) results must
// leave the model untouched.
func (m Model) acceptsOperation(id operationID) bool {
	accepted := m.operations.accepts(id)
	if accepted {
		slog.Debug("tui operation completion accepted", "operation_id", id)
	} else {
		var activeID operationID
		if m.operations.active != nil {
			activeID = m.operations.active.id
		}
		slog.Debug("tui stale operation completion rejected", "operation_id", id, "active_operation_id", activeID)
	}
	return accepted
}

func (m *Model) finishOperation(id operationID) {
	m.operations.finish(id)
}
