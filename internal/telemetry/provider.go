package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	otelattribute "go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	otelresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	maxShutdownFlush      = 500 * time.Millisecond
	preExportDrainTimeout = 250 * time.Millisecond
)

// Telemetry is the runtime telemetry handle. It exposes only small wrapper
// types so callers do not depend directly on OpenTelemetry SDK types.
type Telemetry struct {
	state              TelemetryState
	id                 string
	queue              *Queue
	mp                 *sdkmetric.MeterProvider
	tp                 *sdktrace.TracerProvider
	counter            otelmetric.Int64Counter
	hist               otelmetric.Int64Histogram
	tracer             oteltrace.Tracer
	sendClients        map[string]*http.Client
	logger             *slog.Logger
	allowedMetricAttrs map[string]struct{}
	failurePath        string
	failureMu          sync.Mutex

	// lastFailure holds a *error so the concrete type stored in the Value is
	// always consistent regardless of the dynamic error type.
	lastFailure atomic.Value
}

// Counter is a thin wrapper around an OpenTelemetry int64 counter.
type Counter struct {
	c       otelmetric.Int64Counter
	allowed map[string]struct{}
}

// Add records n increments with the supplied string attributes.
func (c Counter) Add(ctx context.Context, n int64, attrs map[string]string) {
	if c.c == nil {
		return
	}
	filtered := filteredAttrsToKeyValues(attrs, c.allowed)
	if len(filtered) == 0 {
		c.c.Add(ctx, n)
		return
	}
	c.c.Add(ctx, n, otelmetric.WithAttributes(filtered...))
}

// Histogram is a thin wrapper around an OpenTelemetry int64 histogram.
type Histogram struct {
	h       otelmetric.Int64Histogram
	allowed map[string]struct{}
}

// Record records value milliseconds with the supplied string attributes.
func (h Histogram) Record(ctx context.Context, ms int64, attrs map[string]string) {
	if h.h == nil {
		return
	}
	filtered := filteredAttrsToKeyValues(attrs, h.allowed)
	if len(filtered) == 0 {
		h.h.Record(ctx, ms)
		return
	}
	h.h.Record(ctx, ms, otelmetric.WithAttributes(filtered...))
}

// Span is a thin wrapper around an OpenTelemetry span.
type Span struct {
	s oteltrace.Span
}

// End completes the span.
func (s Span) End() {
	if s.s != nil {
		s.s.End()
	}
}

// SetAttributes sets the supplied string attributes on the span.
func (s Span) SetAttributes(attrs map[string]string) {
	if s.s != nil {
		s.s.SetAttributes(attrsToKeyValues(attrs)...)
	}
}

// SetError records err as a span error and marks the span status as Error.
func (s Span) SetError(err error) {
	if s.s == nil || err == nil {
		return
	}
	s.s.RecordError(err)
	s.s.SetStatus(otelcodes.Error, err.Error())
}

// New builds the telemetry runtime for the resolved TelemetryState.
func New(ctx context.Context, state TelemetryState, logger *slog.Logger) (*Telemetry, error) {
	if logger == nil {
		logger = slog.Default()
	}

	t := &Telemetry{
		state:              state,
		logger:             logger,
		sendClients:        make(map[string]*http.Client),
		allowedMetricAttrs: make(map[string]struct{}, len(tmcfg.MetricSchemaAttributes)),
		failurePath:        filepath.Join(state.StateRoot(), "last-delivery-failure"),
	}
	for _, a := range tmcfg.MetricSchemaAttributes {
		t.allowedMetricAttrs[string(a)] = struct{}{}
	}

	logger.Info("building telemetry runtime", "effective_level", state.EffectiveLevel)

	if state.EffectiveLevel == tmcfg.LevelOff {
		logger.Info("telemetry disabled; skipping installation id, queue, and provider setup")
		return t, nil
	}

	queueRoot := state.QueueRoot()
	t.queue = NewQueue(queueRoot)

	id, err := LoadInstallationID(state.InstallationIDPath)
	if err != nil {
		logger.Error("failed to load installation id", "path", state.InstallationIDPath, "error", err)
		return nil, fmt.Errorf("load installation id: %w", err)
	}
	t.id = id
	logger.Debug("loaded installation id", "path", state.InstallationIDPath)

	res, err := buildResource(ctx, id, state.SchemaVersion)
	if err != nil {
		logger.Warn("partial resource built for telemetry", "error", err)
	}

	cache := newExporterCache()

	var primaryMetricExp sdkmetric.Exporter
	var primaryTraceExp sdktrace.SpanExporter
	var communityMetricExp sdkmetric.Exporter

	if state.PrimaryEndpoint != "" {
		primaryHeaders, err := resolveHeaders(state.PrimaryHeaders)
		if err != nil {
			logger.Error("failed to resolve primary telemetry headers", "error", err)
			return nil, err
		}

		if state.EffectiveLevel == tmcfg.LevelBasic || state.EffectiveLevel == tmcfg.LevelDetailed {
			mKey := cacheKeyFor(state.PrimaryEndpoint, primaryHeaders, state.PrimaryCAFile, state.PrimaryAllowInsecure, queueSubdirMetrics)
			mExp, sender, err := cache.metricExporter(ctx, mKey, func() (sdkmetric.Exporter, *http.Client, error) {
				live, send, err := buildRouteClients(state.PrimaryEndpoint, primaryHeaders, state.PrimaryCAFile, state.PrimaryAllowInsecure, "primary", queueSubdirMetrics, t.recordFailure, t.queue)
				if err != nil {
					return nil, nil, err
				}
				e, err := buildMetricExporter(ctx, live, state.PrimaryEndpoint, primaryHeaders)
				return e, send, err
			})
			if err != nil {
				logger.Error("failed to build primary metric exporter", "error", err)
				return nil, fmt.Errorf("build primary metric exporter: %w", err)
			}
			primaryMetricExp = mExp
			t.sendClients["primary"] = sender
		}

		if state.EffectiveLevel == tmcfg.LevelDetailed {
			tKey := cacheKeyFor(state.PrimaryEndpoint, primaryHeaders, state.PrimaryCAFile, state.PrimaryAllowInsecure, queueSubdirTraces)
			tExp, _, err := cache.traceExporter(ctx, tKey, func() (sdktrace.SpanExporter, *http.Client, error) {
				live, send, err := buildRouteClients(state.PrimaryEndpoint, primaryHeaders, state.PrimaryCAFile, state.PrimaryAllowInsecure, "primary", queueSubdirTraces, t.recordFailure, t.queue)
				if err != nil {
					return nil, nil, err
				}
				e, err := buildTraceExporter(ctx, live, state.PrimaryEndpoint, primaryHeaders)
				return e, send, err
			})
			if err != nil {
				logger.Error("failed to build primary trace exporter", "error", err)
				return nil, fmt.Errorf("build primary trace exporter: %w", err)
			}
			primaryTraceExp = tExp
		}
	}

	if state.CommunityReportingEnabled && !state.CommunityReportingFoldedIntoPrimary && state.CommunityEndpoint != "" {
		if err := tmcfg.ValidateEndpoint(state.CommunityEndpoint, false); err != nil {
			logger.Error("invalid community endpoint", "endpoint", state.CommunityEndpoint, "error", err)
			return nil, fmt.Errorf("validate community endpoint: %w", err)
		}

		cKey := cacheKeyFor(state.CommunityEndpoint, nil, "", false, queueSubdirMetrics)
		cExp, sender, err := cache.metricExporter(ctx, cKey, func() (sdkmetric.Exporter, *http.Client, error) {
			live, send, err := buildRouteClients(state.CommunityEndpoint, nil, "", false, "community", queueSubdirMetrics, t.recordFailure, t.queue)
			if err != nil {
				return nil, nil, err
			}
			e, err := buildMetricExporter(ctx, live, state.CommunityEndpoint, nil)
			return e, send, err
		})
		if err != nil {
			logger.Error("failed to build community metric exporter", "error", err)
			return nil, fmt.Errorf("build community metric exporter: %w", err)
		}
		communityMetricExp = cExp
		t.sendClients["community"] = sender
	}

	if len(t.sendClients) > 0 && t.queue != nil {
		preCtx, cancel := context.WithTimeout(ctx, preExportDrainTimeout)
		if err := t.drain(preCtx); err != nil {
			logger.Warn("pre-export drain failed, continuing", "error", err)
		}
		cancel()
	}

	if primaryMetricExp != nil || communityMetricExp != nil {
		opts := []sdkmetric.Option{sdkmetric.WithResource(res)}
		if primaryMetricExp != nil {
			opts = append(opts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(primaryMetricExp,
				sdkmetric.WithInterval(24*time.Hour),
				sdkmetric.WithTimeout(30*time.Second),
			)))
		}
		if communityMetricExp != nil {
			opts = append(opts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(communityMetricExp,
				sdkmetric.WithInterval(24*time.Hour),
				sdkmetric.WithTimeout(30*time.Second),
			)))
		}
		t.mp = sdkmetric.NewMeterProvider(opts...)

		meter := t.mp.Meter("agent_marketplace")
		t.counter, err = meter.Int64Counter(
			"agent_marketplace.command.invocations",
			otelmetric.WithUnit("{invocation}"),
			otelmetric.WithDescription("Command invocations"),
		)
		if err != nil {
			logger.Error("failed to create command counter", "error", err)
			return nil, fmt.Errorf("create command counter: %w", err)
		}

		t.hist, err = meter.Int64Histogram(
			"agent_marketplace.command.duration_ms",
			otelmetric.WithUnit("ms"),
			otelmetric.WithDescription("Command duration in milliseconds"),
			otelmetric.WithExplicitBucketBoundaries(10, 50, 100, 250, 500, 1000, 2500, 5000, 10000),
		)
		if err != nil {
			logger.Error("failed to create command duration histogram", "error", err)
			return nil, fmt.Errorf("create command duration histogram: %w", err)
		}
	}

	if primaryTraceExp != nil {
		tpOpts := []sdktrace.TracerProviderOption{
			sdktrace.WithResource(res),
			sdktrace.WithBatcher(primaryTraceExp,
				sdktrace.WithBatchTimeout(24*time.Hour),
				sdktrace.WithExportTimeout(30*time.Second),
			),
		}
		t.tp = sdktrace.NewTracerProvider(tpOpts...)
		t.tracer = t.tp.Tracer("agent_marketplace")
	}

	logger.Info("telemetry runtime built", "effective_level", state.EffectiveLevel, "primary_endpoint", state.PrimaryEndpoint != "", "community_enabled", state.CommunityReportingEnabled)

	return t, nil
}

// Shutdown drains any queued payloads, then flushes trace and metric providers
// with the supplied context. Export failures are queued if possible and
// recorded as last delivery failures; they do not change command exit status.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}

	t.logger.Debug("shutting down telemetry")
	flushCtx, cancel := context.WithTimeout(ctx, maxShutdownFlush)
	defer cancel()

	if t.queue != nil {
		if err := t.drain(flushCtx); err != nil {
			slog.Error("telemetry drain failed during shutdown", "error", err)
			t.recordFailure(err)
		}
	}

	if t.tp != nil {
		if err := t.tp.Shutdown(flushCtx); err != nil {
			slog.Error("telemetry trace provider shutdown failed", "error", err)
			t.recordFailure(err)
		}
	}

	if t.mp != nil {
		if err := t.mp.Shutdown(flushCtx); err != nil {
			slog.Error("telemetry metric provider shutdown failed", "error", err)
			t.recordFailure(err)
		}
	}

	t.logger.Debug("telemetry shutdown complete")
	return nil
}

// CommandCounter returns the command invocation counter.
func (t *Telemetry) CommandCounter() Counter {
	t.logger.Debug("returning command counter")
	return Counter{c: t.counter, allowed: t.allowedMetricAttrs}
}

// CommandDuration returns the command duration histogram.
func (t *Telemetry) CommandDuration() Histogram {
	t.logger.Debug("returning command duration histogram")
	return Histogram{h: t.hist, allowed: t.allowedMetricAttrs}
}

// StartSpan starts a new lifecycle span. Attributes are supplied as a plain
// string map to keep SDK types internal to this package.
func (t *Telemetry) StartSpan(ctx context.Context, name string, attrs map[string]string) (context.Context, Span) {
	if t.tracer == nil {
		return ctx, Span{}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	t.logger.Debug("starting span", "name", name)

	var opts []oteltrace.SpanStartOption
	if len(attrs) > 0 {
		opts = append(opts, oteltrace.WithAttributes(attrsToKeyValues(attrs)...))
	}

	ctx, sp := t.tracer.Start(ctx, name, opts...)
	return ctx, Span{s: sp}
}

// InstallationID returns the durable installation ID for this runtime.
func (t *Telemetry) InstallationID() string {
	return t.id
}

// LastDeliveryFailure returns the most recent delivery failure, if any.
func (t *Telemetry) LastDeliveryFailure() error {
	v := t.lastFailure.Load()
	if v != nil {
		if p, ok := v.(*error); ok {
			return *p
		}
	}

	if t.failurePath == "" {
		return nil
	}
	df, rerr := ReadLastDeliveryFailure(t.failurePath)
	if rerr != nil || df.Error == "" {
		return nil
	}
	return fmt.Errorf("%s: %s", df.Time.Format(time.RFC3339), df.Error)
}

func (t *Telemetry) recordFailure(err error) {
	if err == nil {
		return
	}

	p := err
	t.lastFailure.Store(&p)

	if t.failurePath == "" {
		return
	}

	t.failureMu.Lock()
	defer t.failureMu.Unlock()

	df := DeliveryFailure{Time: time.Now().UTC(), Error: err.Error()}
	data, merr := json.Marshal(df)
	if merr != nil {
		slog.Error("failed to marshal last delivery failure", "path", t.failurePath, "error", merr)
		return
	}
	if werr := os.WriteFile(t.failurePath, data, 0o600); werr != nil {
		slog.Error("failed to write last delivery failure", "path", t.failurePath, "error", werr)
	}
}

// DeliveryFailure is the persisted record of a delivery failure.
type DeliveryFailure struct {
	Time  time.Time `json:"time"`
	Error string    `json:"error"`
}

// ReadLastDeliveryFailure reads a persisted delivery failure record. It returns
// an empty DeliveryFailure and no error when the file does not exist.
func ReadLastDeliveryFailure(path string) (DeliveryFailure, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DeliveryFailure{}, nil
		}
		return DeliveryFailure{}, err
	}
	var df DeliveryFailure
	if err := json.Unmarshal(data, &df); err != nil {
		return DeliveryFailure{}, err
	}
	return df, nil
}

func buildResource(ctx context.Context, installationID, schemaVersion string) (*otelresource.Resource, error) {
	slog.Debug("building telemetry resource", "schema_version", schemaVersion)

	attrs := []otelattribute.KeyValue{
		otelattribute.String(string(tmcfg.ResourceAttrInstallationID), installationID),
		otelattribute.String(string(tmcfg.ResourceAttrSchemaVersion), schemaVersion),
		otelattribute.String(string(tmcfg.ResourceAttrCLIVersion), cliVersion()),
		otelattribute.String(string(tmcfg.ResourceAttrOSName), runtime.GOOS),
		otelattribute.String(string(tmcfg.ResourceAttrArch), runtime.GOARCH),
	}

	res, err := otelresource.New(ctx, otelresource.WithAttributes(attrs...))
	if err != nil {
		slog.Error("failed to build telemetry resource", "error", err)
	}
	return res, err
}

func cliVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	for _, d := range info.Deps {
		if d.Path == "github.com/Adyen/agent-marketplace" {
			return d.Version
		}
	}
	return "dev"
}

func attrsToKeyValues(attrs map[string]string) []otelattribute.KeyValue {
	out := make([]otelattribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		if k == "" {
			continue
		}
		out = append(out, otelattribute.String(k, v))
	}
	return out
}

func filteredAttrsToKeyValues(attrs map[string]string, allowed map[string]struct{}) []otelattribute.KeyValue {
	if len(attrs) == 0 || len(allowed) == 0 {
		return nil
	}
	out := make([]otelattribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		if k == "" {
			continue
		}
		if _, ok := allowed[k]; !ok {
			continue
		}
		out = append(out, otelattribute.String(k, v))
	}
	return out
}
