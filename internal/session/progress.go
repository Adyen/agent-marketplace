package session

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

// ProgressEvent carries one observed pipeline phase for the active
// operation. Acceptance requires the active operation identity, the
// applicable request generation, and a sequence newer than the last accepted
// observation; stale, duplicate, out-of-order and post-finish events are
// dropped without touching phase, diagnostics, success, or serialization.
type ProgressEvent struct {
	Operation  OperationID
	Generation RequestGeneration
	Sequence   uint64
	Phase      string
}

// ProgressPollInterval bounds how long an idle observation pump may block.
const ProgressPollInterval = 200 * time.Millisecond

// OperationPhaseConsent is the surface-entered phase shown while a
// Marketplace install waits on the consent pane. It is set only when the
// pane is actually entered, mirroring the pipeline's "phases only when
// entered" rule.
const OperationPhaseConsent = "awaiting consent"

// OperationProgressSource is the producer side of a phase observation. The
// pipeline emits phases through Observer(); the surface reads them from the
// buffered Events channel. Emissions are dropped rather than blocking the
// pipeline, and Close makes late emissions safe.
type OperationProgressSource struct {
	operation  OperationID
	generation RequestGeneration
	events     chan ProgressEvent
	sequence   atomic.Uint64
	// accepted is the newest sequence the surface accepted; written only
	// from the surface's single-threaded update path.
	accepted uint64

	mu     sync.Mutex
	closed bool
}

// NewOperationProgressSource opens phase observation for one operation and
// request generation.
func NewOperationProgressSource(operation OperationID, generation RequestGeneration) *OperationProgressSource {
	return &OperationProgressSource{
		operation:  operation,
		generation: generation,
		events:     make(chan ProgressEvent, 8),
	}
}

func (s *OperationProgressSource) Observer() pipeline.PhaseObserver {
	return func(phase pipeline.Phase) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed {
			return
		}
		msg := ProgressEvent{
			Operation:  s.operation,
			Generation: s.generation,
			Sequence:   s.sequence.Add(1),
			Phase:      string(phase),
		}
		select {
		case s.events <- msg:
		default:
		}
	}
}

func (s *OperationProgressSource) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.events)
	}
}

// Events is the buffered stream of observed phases; it closes with Close.
func (s *OperationProgressSource) Events() <-chan ProgressEvent { return s.events }

// Operation reports the operation identity this source observes.
func (s *OperationProgressSource) Operation() OperationID { return s.operation }

// Generation reports the request generation this source observes.
func (s *OperationProgressSource) Generation() RequestGeneration { return s.generation }

// Accept validates one phase observation. Only an event for this source's
// operation, its applicable generation, and a sequence newer than the last
// accepted event may advance the visible phase.
func (s *OperationProgressSource) Accept(event ProgressEvent) bool {
	if event.Operation != s.operation {
		return false
	}
	if event.Generation != s.generation || event.Sequence <= s.accepted {
		return false
	}
	s.accepted = event.Sequence
	return true
}
