package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
)

// operationProgressMsg carries one observed pipeline phase for the active
// operation. Acceptance requires the active operation identity, the
// applicable request generation, and a sequence newer than the last accepted
// observation; stale, duplicate, out-of-order and post-finish events are
// dropped without touching phase, diagnostics, success, or serialization.
type operationProgressMsg struct {
	operation  operationID
	generation requestGeneration
	sequence   uint64
	phase      string
}

// operationProgressDoneMsg ends the observation pump when the producer
// channel closes. The source identity binds the event to its pump so a
// late event from a finished operation cannot act on a newer one.
type operationProgressDoneMsg struct {
	source *operationProgressSource
}

// operationProgressPollMsg wakes an idle pump so it can be rescheduled or
// dropped once the operation ended. It keeps the pump from blocking a tea
// runtime (or a test enumerating batch commands) forever while the operation
// reports no phases. The source identity is required: a stale wake arriving
// after the operation was superseded must not spawn a second pump for the
// current operation.
type operationProgressPollMsg struct {
	source *operationProgressSource
}

// waitOperationProgress is the observation pump: it yields one
// operationProgressMsg per real phase and reschedules itself while the
// operation stays active.
func waitOperationProgress(s *operationProgressSource) tea.Cmd {
	return func() tea.Msg {
		select {
		case event, ok := <-s.Events():
			if !ok {
				return operationProgressDoneMsg{source: s}
			}
			return operationProgressMsg{
				operation:  event.Operation,
				generation: event.Generation,
				sequence:   event.Sequence,
				phase:      event.Phase,
			}
		case <-time.After(progressPollInterval):
			return operationProgressPollMsg{source: s}
		}
	}
}

// beginOperationProgress opens phase observation for an operation that does
// not use the mutation serializer (sync/update). The identity is issued from
// the same monotonic space real operations use, so progress can never be
// confused with another operation's events.
func (m *Model) beginOperationProgress(label string, generation requestGeneration) {
	m.beginOperationProgressFor(m.operations.Issue(), label, generation)
}

// beginOperationProgressFor opens phase observation bound to an existing
// serialized operation identity (Marketplace install).
func (m *Model) beginOperationProgressFor(operation operationID, label string, generation requestGeneration) {
	if m.progress != nil {
		m.progress.Close()
	}
	m.progress = session.NewOperationProgressSource(operation, generation)
	m.progressLabel = label
	m.operationPhase = ""
}

// clearOperationProgress ends observation when the owning operation
// finishes, fails, or is cancelled. Late observations are then rejected.
func (m *Model) clearOperationProgress() {
	if m.progress != nil {
		m.progress.Close()
	}
	m.progress = nil
	m.progressLabel = ""
	m.operationPhase = ""
}

// operationProgressCmd returns the observation pump for the active progress
// source, or nil when the operation is unobserved.
func (m Model) operationProgressCmd() tea.Cmd {
	if m.progress == nil {
		return nil
	}
	return waitOperationProgress(m.progress)
}

// observedContext attaches the active operation's phase observer to ctx so
// direct pipeline calls and injected lifecycle callbacks share the same
// phase boundaries. Without an active observation the context is unchanged.
func (m Model) observedContext(ctx context.Context) context.Context {
	if m.progress == nil {
		return ctx
	}
	return pipeline.WithPhaseObserver(ctx, m.progress.Observer())
}

// applyOperationProgress validates one phase observation. Only an event for
// the active operation, its applicable generation, and a sequence newer than
// the last accepted event may advance the visible phase.
func (m *Model) applyOperationProgress(msg operationProgressMsg) bool {
	if m.progress == nil {
		return false
	}
	accepted := m.progress.Accept(session.ProgressEvent{
		Operation:  msg.operation,
		Generation: msg.generation,
		Sequence:   msg.sequence,
	})
	if !accepted {
		return false
	}
	m.operationPhase = msg.phase
	return true
}
