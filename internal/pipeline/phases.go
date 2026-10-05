package pipeline

import "context"

// Phase identifies a real pipeline stage boundary reported to an optional
// PhaseObserver. Phases are only emitted when the pipeline actually enters
// the stage; callers must never derive progress from results or error kinds.
type Phase string

const (
	// PhaseConfigure marks the start of effective configuration loading.
	PhaseConfigure Phase = "loading configuration"
	// PhaseResolve marks the start of catalog load and resource resolution.
	PhaseResolve Phase = "resolving resources"
	// PhaseMaterialize marks the start of harness materialization.
	PhaseMaterialize Phase = "materializing"
	// PhasePrepare marks the start of a Marketplace install preparation.
	PhasePrepare Phase = "preparing install"
	// PhaseReload marks the start of an authoritative post-operation state
	// reload performed by the caller.
	PhaseReload Phase = "reloading state"
)

// PhaseObserver receives phase transitions in emission order. It is invoked
// synchronously at phase boundaries, so it must be cheap and non-blocking.
// Observers are advisory only: they cannot complete an operation, clear
// diagnostics, or announce success.
type PhaseObserver func(Phase)

type phaseObserverContextKey struct{}

// WithPhaseObserver attaches an optional phase observer to ctx. Any pipeline
// entry point (Sync, Resolve, Update, Marketplace install prepare/commit)
// that crosses a real phase boundary reports it through the observer.
// A nil observer leaves the context unchanged; callers that attach nothing
// observe no phases and show generic running.
func WithPhaseObserver(ctx context.Context, observer PhaseObserver) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, phaseObserverContextKey{}, observer)
}

// ObservePhase reports a phase boundary to the observer attached via
// WithPhaseObserver. It is a no-op for unobserved callers. Callers that own
// a post-operation authoritative reload may emit PhaseReload for the same
// observation channel the pipeline uses for its own boundaries.
func ObservePhase(ctx context.Context, phase Phase) {
	observer, ok := ctx.Value(phaseObserverContextKey{}).(PhaseObserver)
	if !ok || observer == nil {
		return
	}
	observer(phase)
}
