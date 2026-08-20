package telemetry

import (
	"context"
	"time"
)

type ctxKey int

const telemetryCtxKey ctxKey = 0

type commandContext struct {
	telemetry *Telemetry
	start     time.Time
	path      string
	harnesses string
	managed   bool
}

// NewContext returns a child context that carries telemetry runtime state and
// command metadata. The metadata is used by finishTelemetry to emit a single
// bounded command invocation metric.
func NewContext(ctx context.Context, t *Telemetry, commandPath string, start time.Time, harnesses string, managed bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, telemetryCtxKey, &commandContext{
		telemetry: t,
		start:     start,
		path:      commandPath,
		harnesses: harnesses,
		managed:   managed,
	})
}

// TelemetryFromContext returns the telemetry runtime attached to the context,
// or nil when telemetry is disabled or failed to initialize.
func TelemetryFromContext(ctx context.Context) *Telemetry {
	if v, ok := ctx.Value(telemetryCtxKey).(*commandContext); ok {
		return v.telemetry
	}
	return nil
}

// CommandStartFromContext returns the command start time recorded in the context.
func CommandStartFromContext(ctx context.Context) time.Time {
	if v, ok := ctx.Value(telemetryCtxKey).(*commandContext); ok {
		return v.start
	}
	return time.Time{}
}

// CommandPathFromContext returns the command path recorded in the context.
func CommandPathFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(telemetryCtxKey).(*commandContext); ok {
		return v.path
	}
	return ""
}

// HarnessesFromContext returns the comma-separated harness list recorded in
// the context.
func HarnessesFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(telemetryCtxKey).(*commandContext); ok {
		return v.harnesses
	}
	return ""
}

// ManagedFromContext returns the managed-installation flag recorded in the
// context.
func ManagedFromContext(ctx context.Context) bool {
	if v, ok := ctx.Value(telemetryCtxKey).(*commandContext); ok {
		return v.managed
	}
	return false
}
