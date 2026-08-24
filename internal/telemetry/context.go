package telemetry

import (
	"context"
	"time"
)

type ctxKey int

const telemetryCtxKey ctxKey = 0

type commandContext struct {
	telemetry  *Telemetry
	start      time.Time
	path       string
	harnesses  string
	managed    bool
	rootSpan   Span
	diagnostic bool
}

// NewContext returns a child context that carries telemetry runtime state and
// command metadata. The metadata is used by finishTelemetry to emit a single
// bounded command invocation metric. rootSpan is stored so finishTelemetry can
// end the root command span before shutting down telemetry.
func NewContext(ctx context.Context, t *Telemetry, commandPath string, start time.Time, harnesses string, managed bool, rootSpan Span, diagnostic bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, telemetryCtxKey, &commandContext{
		telemetry:  t,
		start:      start,
		path:       commandPath,
		harnesses:  harnesses,
		managed:    managed,
		rootSpan:   rootSpan,
		diagnostic: diagnostic,
	})
}

// TelemetryFromContext returns the telemetry runtime attached to the context,
// or nil when telemetry is disabled or failed to initialize.
func TelemetryFromContext(ctx context.Context) *Telemetry {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(telemetryCtxKey).(*commandContext); ok {
		return v.telemetry
	}
	return nil
}

// CommandStartFromContext returns the command start time recorded in the context.
func CommandStartFromContext(ctx context.Context) time.Time {
	if ctx == nil {
		return time.Time{}
	}
	if v, ok := ctx.Value(telemetryCtxKey).(*commandContext); ok {
		return v.start
	}
	return time.Time{}
}

// CommandPathFromContext returns the command path recorded in the context.
func CommandPathFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(telemetryCtxKey).(*commandContext); ok {
		return v.path
	}
	return ""
}

// HarnessesFromContext returns the comma-separated harness list recorded in
// the context.
func HarnessesFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(telemetryCtxKey).(*commandContext); ok {
		return v.harnesses
	}
	return ""
}

// ManagedFromContext returns the managed-installation flag recorded in the
// context.
func ManagedFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if v, ok := ctx.Value(telemetryCtxKey).(*commandContext); ok {
		return v.managed
	}
	return false
}

// RootSpanFromContext returns the root command span recorded in the context.
func RootSpanFromContext(ctx context.Context) Span {
	if ctx == nil {
		return Span{}
	}
	if v, ok := ctx.Value(telemetryCtxKey).(*commandContext); ok {
		return v.rootSpan
	}
	return Span{}
}

// DiagnosticFromContext returns the diagnostic capture flag recorded in the
// context.
func DiagnosticFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if v, ok := ctx.Value(telemetryCtxKey).(*commandContext); ok {
		return v.diagnostic
	}
	return false
}
