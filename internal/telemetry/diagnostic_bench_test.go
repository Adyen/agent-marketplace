package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/redact"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	otelattribute "go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	otelresource "go.opentelemetry.io/otel/sdk/resource"
)

func BenchmarkDiagnosticRedaction(b *testing.B) {
	defer benchDiscardLogs()()

	dir := b.TempDir()
	b.Setenv("XDG_STATE_HOME", dir)

	user := amconfig.Settings{
		Harnesses: []string{"factory"},
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelDetailed,
			Endpoint: "http://127.0.0.1:4318",
		},
	}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		b.Fatal(err)
	}

	tel, err := New(context.Background(), state, slog.New(slog.NewTextHandler(io.Discard, nil)), WithDiagnostic(true))
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = tel.Shutdown(context.Background()) }()

	ctx, rootSpan := tel.StartSpan(context.Background(), "agent_marketplace.command", map[string]string{
		"sensitive.url": "https://user:token@example.com/repo.git",
		"registry.url":  "https://deploy:secret@gitlab.example.com/group/repo.git",
	})
	_, childSpan := tel.StartSpan(ctx, "agent_marketplace.config.load", map[string]string{
		"config.url": "https://api:key@example.com/config.yaml",
	})
	childSpan.End()
	rootSpan.End()

	tel.CommandCounter().Add(context.Background(), 1, map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-bench",
	})
	tel.CommandDuration().Record(context.Background(), 100, map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-bench",
	})

	var rm metricdata.ResourceMetrics
	if err := tel.diagnosticManualReader.Collect(context.Background(), &rm); err != nil {
		b.Fatal(err)
	}
	spans := tel.diagnosticSpanExporter.Spans()

	res := rm.Resource
	if res == nil && len(spans) > 0 {
		res = spans[0].Resource()
	}
	redactedRes := redactResource(res)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = redactResourceMetrics(context.Background(), &rm)
		_ = redactSpans(context.Background(), spans, redactedRes)
	}
}

func BenchmarkDiagnosticPayloadSerialization(b *testing.B) {
	defer benchDiscardLogs()()

	dir := b.TempDir()
	b.Setenv("XDG_STATE_HOME", dir)

	user := amconfig.Settings{
		Harnesses: []string{"factory"},
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelDetailed,
			Endpoint: "http://127.0.0.1:4318",
		},
	}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		b.Fatal(err)
	}

	tel, err := New(context.Background(), state, slog.New(slog.NewTextHandler(io.Discard, nil)), WithDiagnostic(true))
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = tel.Shutdown(context.Background()) }()

	_, rootSpan := tel.StartSpan(context.Background(), "agent_marketplace.command", map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-bench",
	})
	rootSpan.End()

	tel.CommandCounter().Add(context.Background(), 1, map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-bench",
	})
	tel.CommandDuration().Record(context.Background(), 100, map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-bench",
	})

	var rm metricdata.ResourceMetrics
	if err := tel.diagnosticManualReader.Collect(context.Background(), &rm); err != nil {
		b.Fatal(err)
	}
	spans := tel.diagnosticSpanExporter.Spans()

	res := rm.Resource
	if res == nil && len(spans) > 0 {
		res = spans[0].Resource()
	}
	redactedRes := redactResource(res)
	rmRedacted := redactResourceMetrics(context.Background(), &rm)
	rmRedacted.Resource = redactedRes
	redactedSpans := redactSpans(context.Background(), spans, redactedRes)
	traceID := rootSpan.TraceID()

	payload, err := buildDiagnosticPayload(state, tel.InstallationID(), traceID, rmRedacted, redactedSpans, nil)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(payload)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDiagnosticURLRedaction(b *testing.B) {
	samples := []string{
		"https://user:token@example.com/repo.git",
		"https://deploy:secret@gitlab.example.com/group/repo.git",
		"https://api:key@example.com/config.yaml",
		"https://oauth2:ghp_xxx@github.com/org/repo.git",
		"https://example.com/path",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, s := range samples {
			_ = redact.URL(s)
		}
	}
}

func init() {
	// Warm up the attribute set used by benchmarks so allocation measurements
	// focus on the redaction/serialization paths.
	_, _ = otelresource.New(context.Background(), otelresource.WithAttributes(
		otelattribute.String("test", "value"),
	))
}
