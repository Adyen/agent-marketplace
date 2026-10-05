package tui

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
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

// progressPollInterval bounds how long an idle observation pump may block.
const progressPollInterval = 200 * time.Millisecond

// operationPhaseConsent is the model-entered phase shown while a Marketplace
// install waits on the consent pane. It is set only when the pane is
// actually entered, mirroring the pipeline's "phases only when entered" rule.
const operationPhaseConsent = "awaiting consent"

// operationProgressSource is the producer side of a phase observation. The
// pipeline emits phases through observer(); the model reads them from the
// buffered channel via waitOperationProgress. Emissions are dropped rather
// than blocking the pipeline, and close() makes late emissions safe.
type operationProgressSource struct {
	operation  operationID
	generation requestGeneration
	updates    chan operationProgressMsg
	sequence   atomic.Uint64
	// accepted is the newest sequence the model accepted; written only
	// from Update.
	accepted uint64

	mu     sync.Mutex
	closed bool
}

func (s *operationProgressSource) observer() pipeline.PhaseObserver {
	return func(phase pipeline.Phase) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed {
			return
		}
		msg := operationProgressMsg{
			operation:  s.operation,
			generation: s.generation,
			sequence:   s.sequence.Add(1),
			phase:      string(phase),
		}
		select {
		case s.updates <- msg:
		default:
		}
	}
}

func (s *operationProgressSource) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.updates)
	}
}

// waitOperationProgress is the observation pump: it yields one
// operationProgressMsg per real phase and reschedules itself while the
// operation stays active.
func waitOperationProgress(s *operationProgressSource) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg, ok := <-s.updates:
			if !ok {
				return operationProgressDoneMsg{source: s}
			}
			return msg
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
	m.beginOperationProgressFor(m.operations.issue(), label, generation)
}

// beginOperationProgressFor opens phase observation bound to an existing
// serialized operation identity (Marketplace install).
func (m *Model) beginOperationProgressFor(operation operationID, label string, generation requestGeneration) {
	if m.progress != nil {
		m.progress.close()
	}
	m.progress = &operationProgressSource{
		operation:  operation,
		generation: generation,
		updates:    make(chan operationProgressMsg, 8),
	}
	m.progressLabel = label
	m.operationPhase = ""
}

// clearOperationProgress ends observation when the owning operation
// finishes, fails, or is cancelled. Late observations are then rejected.
func (m *Model) clearOperationProgress() {
	if m.progress != nil {
		m.progress.close()
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
	return pipeline.WithPhaseObserver(ctx, m.progress.observer())
}

// applyOperationProgress validates one phase observation. Only an event for
// the active operation, its applicable generation, and a sequence newer than
// the last accepted event may advance the visible phase.
func (m *Model) applyOperationProgress(msg operationProgressMsg) bool {
	if m.progress == nil || msg.operation != m.progress.operation {
		return false
	}
	if msg.generation != m.progress.generation || msg.sequence <= m.progress.accepted {
		return false
	}
	m.progress.accepted = msg.sequence
	m.operationPhase = msg.phase
	return true
}
