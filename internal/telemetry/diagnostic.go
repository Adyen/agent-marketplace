package telemetry

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/redact"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	otelattribute "go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	otlpmetrichttp "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otlptracehttp "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	otelresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	diagnosticFilePerm      = 0o600
	diagnosticDirPerm       = 0o700
	diagnosticExportTimeout = 5 * time.Second
)

// writeDiagnosticInfo writes a formatted diagnostic message to w. It logs a
// warning instead of failing, since a writer error should not abort export.
func writeDiagnosticInfo(w io.Writer, format string, args ...any) {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		slog.Warn("failed to write diagnostic output", "error", err)
	}
}

// writeDiagnosticLine writes a single diagnostic line to w. It logs a
// warning instead of failing, since a writer error should not abort export.
func writeDiagnosticLine(w io.Writer, args ...any) {
	if _, err := fmt.Fprintln(w, args...); err != nil {
		slog.Warn("failed to write diagnostic output", "error", err)
	}
}

// DiagnosticEvent is a narrow diagnostic log/event entry captured during an
// invocation. The message is redacted before being written to the payload or
// exported.
type DiagnosticEvent struct {
	Name      string    `json:"name"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// DiagnosticPayload is the user-only review file format. It is a JSON summary
// of the exact metrics, traces, and events that will be exported, with URL
// credentials removed.
type DiagnosticPayload struct {
	SchemaVersion      string             `json:"schema_version"`
	Level              string             `json:"level"`
	TraceID            string             `json:"trace_id"`
	InstallationID     string             `json:"installation_id"`
	EndpointHostname   string             `json:"endpoint_hostname"`
	RetentionPolicyURL string             `json:"retention_policy_url,omitempty"`
	Timestamp          time.Time          `json:"timestamp"`
	SensitiveWarning   string             `json:"sensitive_data_warning,omitempty"`
	ResourceAttributes map[string]string  `json:"resource_attributes,omitempty"`
	Metrics            []DiagnosticMetric `json:"metrics,omitempty"`
	Spans              []DiagnosticSpan   `json:"spans,omitempty"`
	DiagnosticEvents   []DiagnosticEvent  `json:"diagnostic_events,omitempty"`
}

// DiagnosticMetric is a JSON-friendly representation of a single metric data
// point. For histograms, Value is a histValue object.
type DiagnosticMetric struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Unit        string            `json:"unit,omitempty"`
	Type        string            `json:"type"`
	Attributes  map[string]string `json:"attributes,omitempty"`
	Value       any               `json:"value"`
}

// DiagnosticSpan is a JSON-friendly representation of a captured span.
type DiagnosticSpan struct {
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	StartTime    time.Time         `json:"start_time"`
	EndTime      time.Time         `json:"end_time"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	Events       []DiagnosticEvent `json:"events,omitempty"`
	StatusCode   string            `json:"status_code,omitempty"`
}

// diagnosticSpanExporter is a trace SpanExporter that simply stores completed
// spans in memory. It is used by the diagnostic TracerProvider so the payload
// can be reviewed before any export.
type diagnosticSpanExporter struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

func (e *diagnosticSpanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spans = append(e.spans, spans...)
	return nil
}

func (e *diagnosticSpanExporter) Shutdown(context.Context) error { return nil }

func (e *diagnosticSpanExporter) Spans() []sdktrace.ReadOnlySpan {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]sdktrace.ReadOnlySpan, len(e.spans))
	copy(out, e.spans)
	return out
}

// setupDiagnostic builds the OpenTelemetry providers used for diagnostic
// capture: a ManualReader for metrics and a TracerProvider with an in-memory
// span exporter. No exporters that hit the network are attached.
func (t *Telemetry) setupDiagnostic(ctx context.Context, res *otelresource.Resource) error {
	manualReader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(manualReader),
	)

	meter := mp.Meter("agent_marketplace")
	counter, err := meter.Int64Counter(
		string(tmcfg.CommandCounterName),
		otelmetric.WithUnit("{invocation}"),
		otelmetric.WithDescription("Command invocations"),
	)
	if err != nil {
		return fmt.Errorf("create diagnostic counter: %w", err)
	}

	hist, err := meter.Int64Histogram(
		string(tmcfg.CommandDurationName),
		otelmetric.WithUnit("ms"),
		otelmetric.WithDescription("Command duration in milliseconds"),
		otelmetric.WithExplicitBucketBoundaries(10, 50, 100, 250, 500, 1000, 2500, 5000, 10000),
	)
	if err != nil {
		return fmt.Errorf("create diagnostic histogram: %w", err)
	}
	inventorySnapshot, inventoryRegistry, inventoryResource, inventoryOmissions, err := newInventoryInstruments(mp)
	if err != nil {
		return fmt.Errorf("create diagnostic inventory instruments: %w", err)
	}

	spanExp := &diagnosticSpanExporter{}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(spanExp)),
	)

	t.mp = mp
	t.tp = tp
	t.counter = counter
	t.hist = hist
	t.inventorySnapshotGauge = inventorySnapshot
	t.inventoryRegistryGauge = inventoryRegistry
	t.inventoryResourceGauge = inventoryResource
	t.inventoryOmissionCounter = inventoryOmissions
	t.tracer = tp.Tracer("agent_marketplace")
	t.diagnosticManualReader = manualReader
	t.diagnosticSpanExporter = spanExp
	t.diagnostic = true

	t.logger.Info("diagnostic telemetry runtime built")
	return nil
}

// FinishDiagnostic collects the buffered metrics and spans, writes the reviewed
// payload to a user-only file, prompts for confirmation, and exports the
// redacted payload to the primary endpoint on approval. It never queues the
// diagnostic payload for normal delivery.
func (t *Telemetry) FinishDiagnostic(ctx context.Context, out io.Writer, in io.Reader, isTerminal, accepted bool) error {
	if t == nil || !t.diagnostic {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var rm metricdata.ResourceMetrics
	if t.diagnosticManualReader != nil {
		if err := t.diagnosticManualReader.Collect(ctx, &rm); err != nil {
			t.logger.Warn("diagnostic metric collection failed", "error", err)
		}
	}

	var spans []sdktrace.ReadOnlySpan
	if t.diagnosticSpanExporter != nil {
		spans = t.diagnosticSpanExporter.Spans()
	}

	// Resolve the resource from whichever signal has it, then redact and use
	// it consistently for both metrics and traces.
	res := rm.Resource
	if res == nil && len(spans) > 0 {
		res = spans[0].Resource()
	}
	redactedRes := redactResource(res)

	rmRedacted := redactResourceMetrics(ctx, &rm)
	rmRedacted.Resource = redactedRes
	redactedSpans := redactSpans(ctx, spans, redactedRes)

	traceID := ""
	if len(redactedSpans) > 0 {
		traceID = redactedSpans[0].SpanContext().TraceID().String()
	}
	if traceID == "" {
		traceID = generateDiagnosticTraceID()
	}

	diagnosticEvents := t.DiagnosticEvents()
	for i := range diagnosticEvents {
		diagnosticEvents[i].Message = redact.URL(diagnosticEvents[i].Message)
	}

	payload, err := buildDiagnosticPayload(t.state, t.id, traceID, rmRedacted, redactedSpans, diagnosticEvents)
	if err != nil {
		return fmt.Errorf("build diagnostic payload: %w", err)
	}

	path, err := writeDiagnosticPayload(t.state.StateRoot(), traceID, payload)
	if err != nil {
		return fmt.Errorf("write diagnostic payload: %w", err)
	}

	t.logger.Info("diagnostic payload written for review", "path", path, "trace_id", traceID)

	writeDiagnosticInfo(out, "\nDiagnostic payload written to %s\n", path)
	writeDiagnosticInfo(out, "  destination hostname: %s\n", emptyOr(t.state.PrimaryEndpointHostname, "none"))
	writeDiagnosticInfo(out, "  retention policy:     %s\n", emptyOr(t.state.RetentionPolicyURL, "none"))
	writeDiagnosticInfo(out, "  installation id:      %s\n", t.id)
	writeDiagnosticInfo(out, "  trace id:             %s\n", traceID)
	writeDiagnosticLine(out, "  warning: Diagnostic payloads may contain sensitive information. Review the file before sharing.")

	if !isTerminal && !accepted {
		writeDiagnosticLine(out, "\nDiagnostic export requires --diagnostic-accept-sensitive-data in non-interactive mode.")
		writeDiagnosticInfo(out, "The payload has been retained at %s\n", path)
		return nil
	}

	if isTerminal && !accepted {
		writeDiagnosticInfo(out, "\nProceed with export? [y/N]: ")
		reader := bufio.NewReader(in)
		reply, err := reader.ReadString('\n')
		if err != nil {
			writeDiagnosticInfo(out, "\nFailed to read confirmation. The payload has been retained at %s\n", path)
			return fmt.Errorf("read confirmation: %w", err)
		}
		reply = strings.TrimSpace(strings.ToLower(reply))
		if !strings.HasPrefix(reply, "y") {
			writeDiagnosticInfo(out, "Diagnostic export cancelled. The payload has been retained at %s\n", path)
			return nil
		}
	}

	slog.Info("exporting diagnostic payload", "trace_id", traceID, "endpoint_hostname", t.state.PrimaryEndpointHostname)
	if err := exportDiagnosticPayload(ctx, t.state, rmRedacted, redactedSpans); err != nil {
		writeDiagnosticInfo(out, "\nDiagnostic export failed: %v\n", err)
		writeDiagnosticInfo(out, "  trace id:        %s\n", traceID)
		writeDiagnosticInfo(out, "  installation id: %s\n", t.id)
		writeDiagnosticInfo(out, "  payload retained: %s\n", path)
		return err
	}

	if err := os.Remove(path); err != nil {
		slog.Warn("diagnostic payload cleanup failed", "path", path, "error", err)
		writeDiagnosticInfo(out, "\nDiagnostic export completed, but local payload cleanup failed: %v\n", err)
		writeDiagnosticInfo(out, "  payload retained: %s\n", path)
		return fmt.Errorf("remove exported diagnostic payload: %w", err)
	}
	writeDiagnosticLine(out, "\nDiagnostic export succeeded.")
	writeDiagnosticInfo(out, "  trace id:        %s\n", traceID)
	writeDiagnosticInfo(out, "  installation id: %s\n", t.id)
	slog.Info("diagnostic payload exported", "trace_id", traceID, "path", path)
	return nil
}

func emptyOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func generateDiagnosticTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// buildDiagnosticPayload converts redacted metrics and spans into the JSON
// review format.
func buildDiagnosticPayload(state TelemetryState, installationID, traceID string, rm *metricdata.ResourceMetrics, spans []sdktrace.ReadOnlySpan, diagnosticEvents []DiagnosticEvent) (*DiagnosticPayload, error) {
	p := &DiagnosticPayload{
		SchemaVersion:      tmcfg.SchemaVersion,
		Level:              tmcfg.LevelDiagnostic,
		TraceID:            traceID,
		InstallationID:     installationID,
		EndpointHostname:   state.PrimaryEndpointHostname,
		RetentionPolicyURL: state.RetentionPolicyURL,
		Timestamp:          time.Now().UTC(),
		SensitiveWarning:   "Diagnostic payloads may contain URLs (with credentials removed), filesystem paths, and error messages. Review before sharing.",
		DiagnosticEvents:   diagnosticEvents,
	}

	if rm != nil && rm.Resource != nil {
		p.ResourceAttributes = attributeSetToMap(otelattribute.NewSet(rm.Resource.Attributes()...))
	}

	if rm != nil {
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				p.Metrics = append(p.Metrics, metricToDiagnosticMetrics(m)...)
			}
		}
	}

	for _, span := range spans {
		p.Spans = append(p.Spans, readOnlySpanToDiagnosticSpan(span))
	}

	return p, nil
}

func writeDiagnosticPayload(stateRoot, traceID string, payload *DiagnosticPayload) (string, error) {
	dir := filepath.Join(stateRoot, "diagnostic")
	if err := os.MkdirAll(dir, diagnosticDirPerm); err != nil {
		return "", err
	}
	_ = os.Chmod(dir, diagnosticDirPerm)

	name := fmt.Sprintf("payload-%s.json", traceID)
	path := filepath.Join(dir, name)

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(path, data, diagnosticFilePerm); err != nil {
		return "", err
	}
	return path, nil
}

func metricToDiagnosticMetrics(m metricdata.Metrics) []DiagnosticMetric {
	var out []DiagnosticMetric
	switch d := m.Data.(type) {
	case metricdata.Sum[int64]:
		for _, dp := range d.DataPoints {
			out = append(out, DiagnosticMetric{
				Name:        m.Name,
				Description: m.Description,
				Unit:        m.Unit,
				Type:        "sum",
				Attributes:  attributeSetToMap(dp.Attributes),
				Value:       dp.Value,
			})
		}
	case metricdata.Histogram[int64]:
		for _, dp := range d.DataPoints {
			out = append(out, DiagnosticMetric{
				Name:        m.Name,
				Description: m.Description,
				Unit:        m.Unit,
				Type:        "histogram",
				Attributes:  attributeSetToMap(dp.Attributes),
				Value: histValue{
					Count:  dp.Count,
					Sum:    dp.Sum,
					Bucket: histogramBucketLabel(dp.Bounds, float64(dp.Sum)),
				},
			})
		}
	case metricdata.Sum[float64]:
		for _, dp := range d.DataPoints {
			out = append(out, DiagnosticMetric{
				Name:        m.Name,
				Description: m.Description,
				Unit:        m.Unit,
				Type:        "sum",
				Attributes:  attributeSetToMap(dp.Attributes),
				Value:       dp.Value,
			})
		}
	case metricdata.Histogram[float64]:
		for _, dp := range d.DataPoints {
			out = append(out, DiagnosticMetric{
				Name:        m.Name,
				Description: m.Description,
				Unit:        m.Unit,
				Type:        "histogram",
				Attributes:  attributeSetToMap(dp.Attributes),
				Value: histValue{
					Count:  dp.Count,
					Sum:    dp.Sum,
					Bucket: histogramBucketLabel(dp.Bounds, dp.Sum),
				},
			})
		}
	}
	return out
}

type histValue struct {
	Count  uint64 `json:"count"`
	Sum    any    `json:"sum"`
	Bucket string `json:"bucket"`
}

func histogramBucketLabel(bounds []float64, value float64) string {
	for _, b := range bounds {
		if value <= b {
			return fmt.Sprintf("<=%s", strconv.FormatFloat(b, 'f', -1, 64))
		}
	}
	if len(bounds) > 0 {
		return fmt.Sprintf(">%s", strconv.FormatFloat(bounds[len(bounds)-1], 'f', -1, 64))
	}
	return "unknown"
}

func readOnlySpanToDiagnosticSpan(span sdktrace.ReadOnlySpan) DiagnosticSpan {
	parentID := ""
	if span.Parent().IsValid() {
		parentID = span.Parent().SpanID().String()
	}

	events := make([]DiagnosticEvent, 0, len(span.Events()))
	for _, ev := range span.Events() {
		msg := ""
		for _, attr := range ev.Attributes {
			if string(attr.Key) == "message" && attr.Value.Type() == otelattribute.STRING {
				msg = attr.Value.AsString()
				break
			}
		}
		events = append(events, DiagnosticEvent{
			Name:      ev.Name,
			Message:   msg,
			Timestamp: ev.Time,
		})
	}

	status := ""
	if span.Status().Code != otelcodes.Unset {
		status = span.Status().Code.String()
	}

	return DiagnosticSpan{
		TraceID:      span.SpanContext().TraceID().String(),
		SpanID:       span.SpanContext().SpanID().String(),
		ParentSpanID: parentID,
		Name:         span.Name(),
		Kind:         span.SpanKind().String(),
		StartTime:    span.StartTime(),
		EndTime:      span.EndTime(),
		Attributes:   attributeSetToMap(otelattribute.NewSet(span.Attributes()...)),
		Events:       events,
		StatusCode:   status,
	}
}

func attributeSetToMap(s otelattribute.Set) map[string]string {
	attrs := make(map[string]string, s.Len())
	iter := s.Iter()
	for iter.Next() {
		kv := iter.Attribute()
		attrs[string(kv.Key)] = kv.Value.String()
	}
	return attrs
}

// redactResource builds a new Resource with URL credentials removed from string
// attribute values.
func redactResource(res *otelresource.Resource) *otelresource.Resource {
	if res == nil {
		return nil
	}
	return otelresource.NewSchemaless(redactKeyValues(res.Attributes())...)
}

// redactResourceMetrics returns a deep copy of rm with URL credentials removed
// from all string attribute values.
func redactResourceMetrics(ctx context.Context, rm *metricdata.ResourceMetrics) *metricdata.ResourceMetrics {
	if rm == nil {
		return nil
	}
	out := &metricdata.ResourceMetrics{
		Resource:     redactResource(rm.Resource),
		ScopeMetrics: make([]metricdata.ScopeMetrics, 0, len(rm.ScopeMetrics)),
	}
	for _, sm := range rm.ScopeMetrics {
		out.ScopeMetrics = append(out.ScopeMetrics, redactScopeMetrics(ctx, sm))
	}
	return out
}

func redactScopeMetrics(ctx context.Context, sm metricdata.ScopeMetrics) metricdata.ScopeMetrics {
	out := metricdata.ScopeMetrics{Scope: sm.Scope, Metrics: make([]metricdata.Metrics, 0, len(sm.Metrics))}
	for _, m := range sm.Metrics {
		out.Metrics = append(out.Metrics, redactMetric(ctx, m))
	}
	return out
}

func redactMetric(ctx context.Context, m metricdata.Metrics) metricdata.Metrics {
	out := metricdata.Metrics{
		Name:        m.Name,
		Description: m.Description,
		Unit:        m.Unit,
		Data:        m.Data,
	}
	switch d := m.Data.(type) {
	case metricdata.Sum[int64]:
		d.DataPoints = redactSumDataPoints(d.DataPoints)
		out.Data = d
	case metricdata.Sum[float64]:
		d.DataPoints = redactSumDataPointsFloat(d.DataPoints)
		out.Data = d
	case metricdata.Histogram[int64]:
		d.DataPoints = redactHistogramDataPoints(d.DataPoints)
		out.Data = d
	case metricdata.Histogram[float64]:
		d.DataPoints = redactHistogramDataPointsFloat(d.DataPoints)
		out.Data = d
	}
	return out
}

func redactSumDataPoints(dps []metricdata.DataPoint[int64]) []metricdata.DataPoint[int64] {
	out := make([]metricdata.DataPoint[int64], len(dps))
	for i, dp := range dps {
		out[i] = dp
		out[i].Attributes = redactAttributeSet(dp.Attributes)
	}
	return out
}

func redactSumDataPointsFloat(dps []metricdata.DataPoint[float64]) []metricdata.DataPoint[float64] {
	out := make([]metricdata.DataPoint[float64], len(dps))
	for i, dp := range dps {
		out[i] = dp
		out[i].Attributes = redactAttributeSet(dp.Attributes)
	}
	return out
}

func redactHistogramDataPoints(dps []metricdata.HistogramDataPoint[int64]) []metricdata.HistogramDataPoint[int64] {
	out := make([]metricdata.HistogramDataPoint[int64], len(dps))
	for i, dp := range dps {
		out[i] = dp
		out[i].Attributes = redactAttributeSet(dp.Attributes)
	}
	return out
}

func redactHistogramDataPointsFloat(dps []metricdata.HistogramDataPoint[float64]) []metricdata.HistogramDataPoint[float64] {
	out := make([]metricdata.HistogramDataPoint[float64], len(dps))
	for i, dp := range dps {
		out[i] = dp
		out[i].Attributes = redactAttributeSet(dp.Attributes)
	}
	return out
}

func redactAttributeSet(s otelattribute.Set) otelattribute.Set {
	return otelattribute.NewSet(redactKeyValues(s.ToSlice())...)
}

func redactKeyValues(kvs []otelattribute.KeyValue) []otelattribute.KeyValue {
	out := make([]otelattribute.KeyValue, 0, len(kvs))
	for _, kv := range kvs {
		switch kv.Value.Type() {
		case otelattribute.STRING:
			out = append(out, otelattribute.String(string(kv.Key), redact.URL(kv.Value.AsString())))
		case otelattribute.STRINGSLICE:
			out = append(out, otelattribute.StringSlice(string(kv.Key), redactStrings(kv.Value.AsStringSlice())))
		default:
			out = append(out, kv)
		}
	}
	return out
}

func redactStrings(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = redact.URL(v)
	}
	return out
}

// redactedReadOnlySpan wraps a ReadOnlySpan and overrides attribute/event
// accessors to return values with URL credentials removed.
type redactedReadOnlySpan struct {
	sdktrace.ReadOnlySpan
	resource *otelresource.Resource
	attrs    []otelattribute.KeyValue
	events   []sdktrace.Event
}

func (s *redactedReadOnlySpan) Resource() *otelresource.Resource     { return s.resource }
func (s *redactedReadOnlySpan) Attributes() []otelattribute.KeyValue { return s.attrs }
func (s *redactedReadOnlySpan) Events() []sdktrace.Event             { return s.events }

func redactSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan, redactedRes *otelresource.Resource) []sdktrace.ReadOnlySpan {
	out := make([]sdktrace.ReadOnlySpan, 0, len(spans))
	for _, span := range spans {
		out = append(out, redactReadOnlySpan(ctx, span, redactedRes))
	}
	return out
}

func redactReadOnlySpan(ctx context.Context, span sdktrace.ReadOnlySpan, redactedRes *otelresource.Resource) sdktrace.ReadOnlySpan {
	return &redactedReadOnlySpan{
		ReadOnlySpan: span,
		resource:     redactedRes,
		attrs:        redactKeyValues(span.Attributes()),
		events:       redactEvents(span.Events()),
	}
}

func redactEvents(events []sdktrace.Event) []sdktrace.Event {
	out := make([]sdktrace.Event, len(events))
	for i, ev := range events {
		out[i] = ev
		out[i].Attributes = redactKeyValues(ev.Attributes)
	}
	return out
}

// exportDiagnosticPayload serializes the redacted metrics and spans using the
// official OTLP/HTTP exporters and a capture transport, then replays the
// captured requests to the primary endpoint with a 5-second timeout.
func exportDiagnosticPayload(ctx context.Context, state TelemetryState, rm *metricdata.ResourceMetrics, spans []sdktrace.ReadOnlySpan) error {
	if state.PrimaryEndpoint == "" {
		return fmt.Errorf("no primary telemetry endpoint configured")
	}

	headers, err := resolveHeaders(state.PrimaryHeaders)
	if err != nil {
		return fmt.Errorf("resolve primary headers: %w", err)
	}

	capture := &diagnosticCaptureTransport{}
	captureClient := &http.Client{Transport: capture}

	metricExp, err := buildCaptureMetricExporter(ctx, captureClient, state.PrimaryEndpoint, headers)
	if err != nil {
		return fmt.Errorf("build diagnostic metric exporter: %w", err)
	}

	traceExp, err := buildCaptureTraceExporter(ctx, captureClient, state.PrimaryEndpoint, headers)
	if err != nil {
		return fmt.Errorf("build diagnostic trace exporter: %w", err)
	}

	if rm != nil {
		if err := metricExp.Export(ctx, rm); err != nil {
			return fmt.Errorf("serialize diagnostic metrics: %w", err)
		}
	}
	if len(spans) > 0 {
		if err := traceExp.ExportSpans(ctx, spans); err != nil {
			return fmt.Errorf("serialize diagnostic traces: %w", err)
		}
	}

	_, send, err := buildRouteClients(state.PrimaryEndpoint, headers, state.PrimaryCAFile, state.PrimaryAllowInsecure, "primary", queueSubdirMetrics, nil, nil)
	if err != nil {
		return fmt.Errorf("build diagnostic send client: %w", err)
	}
	if send == nil {
		send = &http.Client{Timeout: diagnosticExportTimeout}
	}

	exportCtx, cancel := context.WithTimeout(ctx, diagnosticExportTimeout)
	defer cancel()

	capture.mu.Lock()
	metric, trace := capture.metric, capture.trace
	capture.mu.Unlock()

	var exportErrs []error
	if metric != nil {
		if err := sendCapturedRequest(exportCtx, send, metric); err != nil {
			exportErrs = append(exportErrs, err)
		}
	}
	if trace != nil {
		if err := sendCapturedRequest(exportCtx, send, trace); err != nil {
			exportErrs = append(exportErrs, err)
		}
	}
	if len(exportErrs) > 0 {
		return fmt.Errorf("send diagnostic payload: %w", errors.Join(exportErrs...))
	}
	return nil
}

func buildCaptureMetricExporter(ctx context.Context, client *http.Client, endpoint string, headers map[string]string) (sdkmetric.Exporter, error) {
	ep, err := buildSignalEndpoint(endpoint, queueSubdirMetrics)
	if err != nil {
		return nil, err
	}
	return otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithHTTPClient(client),
		otlpmetrichttp.WithEndpointURL(ep),
		otlpmetrichttp.WithHeaders(headers),
		otlpmetrichttp.WithCompression(otlpmetrichttp.NoCompression),
		otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}),
		otlpmetrichttp.WithTimeout(30*time.Second),
		otlpmetrichttp.WithTemporalitySelector(sdkmetric.DefaultTemporalitySelector),
		otlpmetrichttp.WithAggregationSelector(sdkmetric.DefaultAggregationSelector),
	)
}

func buildCaptureTraceExporter(ctx context.Context, client *http.Client, endpoint string, headers map[string]string) (sdktrace.SpanExporter, error) {
	ep, err := buildSignalEndpoint(endpoint, queueSubdirTraces)
	if err != nil {
		return nil, err
	}
	return otlptracehttp.New(ctx,
		otlptracehttp.WithHTTPClient(client),
		otlptracehttp.WithEndpointURL(ep),
		otlptracehttp.WithHeaders(headers),
		otlptracehttp.WithCompression(otlptracehttp.NoCompression),
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}),
		otlptracehttp.WithTimeout(30*time.Second),
	)
}

// diagnosticCaptureTransport intercepts OTLP/HTTP requests from the SDK
// exporters, stores the serialized request, and returns a synthetic success
// response so the SDK believes the export succeeded.
type diagnosticCaptureTransport struct {
	mu     sync.Mutex
	metric *capturedRequest
	trace  *capturedRequest
}

type capturedRequest struct {
	URL     string
	Headers http.Header
	Body    []byte
}

func (ct *diagnosticCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()

	cr := &capturedRequest{
		URL:     req.URL.String(),
		Headers: req.Header.Clone(),
		Body:    body,
	}

	ct.mu.Lock()
	switch {
	case strings.HasSuffix(req.URL.Path, "/v1/metrics"):
		ct.metric = cr
	case strings.HasSuffix(req.URL.Path, "/v1/traces"):
		ct.trace = cr
	}
	ct.mu.Unlock()

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/x-protobuf"}},
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

func sendCapturedRequest(ctx context.Context, client *http.Client, cr *capturedRequest) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cr.URL, bytes.NewReader(cr.Body))
	if err != nil {
		return err
	}

	for k, vv := range cr.Headers {
		if k == "Content-Length" || k == "Host" || k == "Transfer-Encoding" {
			continue
		}
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	req.ContentLength = int64(len(cr.Body))

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("server returned %s", resp.Status)
	}
	return nil
}

// sanitizePreviewPayload applies narrow URL credential redaction to every
// string value in a preview payload so it is consistent with the diagnostic
// review file.
func sanitizePreviewPayload(p *PreviewPayload) {
	for k, v := range p.ResourceAttributes {
		if s, ok := v.(string); ok {
			p.ResourceAttributes[k] = redact.URL(s)
		}
	}
	for i := range p.Metrics {
		for k, v := range p.Metrics[i].Attributes {
			p.Metrics[i].Attributes[k] = redact.URL(v)
		}
		if s, ok := p.Metrics[i].Value.(string); ok {
			p.Metrics[i].Value = redact.URL(s)
		}
	}
	for i := range p.Spans {
		for k, v := range p.Spans[i].Attributes {
			p.Spans[i].Attributes[k] = redact.URL(v)
		}
	}
	for i := range p.DiagnosticEvents {
		p.DiagnosticEvents[i].Message = redact.URL(p.DiagnosticEvents[i].Message)
	}
	p.SensitiveWarning = redact.URL(p.SensitiveWarning)
}

var (
	_ sdktrace.SpanExporter = (*diagnosticSpanExporter)(nil)
)
