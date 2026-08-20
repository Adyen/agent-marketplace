package telemetry

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

var (
	benchSrvOnce sync.Once
	benchSrv     *httptest.Server
)

func benchServer() *httptest.Server {
	benchSrvOnce.Do(func() {
		benchSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/x-protobuf")
			w.WriteHeader(http.StatusOK)
		}))
		// Prime the server so subsequent benchmark loops do not pay the
		// listener-startup race.
		if _, err := http.Get(benchSrv.URL); err != nil {
			panic(err)
		}
	})
	return benchSrv
}

func BenchmarkNewShutdown(b *testing.B) {
	dir := b.TempDir()
	b.Setenv("XDG_STATE_HOME", dir)

	srv := benchServer()

	user := &amconfig.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: srv.URL,
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	oldLog := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(oldLog)

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state, err := ResolveTelemetryConfig(nil, user, "", "")
		if err != nil {
			b.Fatal(err)
		}
		tel, err := New(ctx, state, logger)
		if err != nil {
			b.Fatal(err)
		}
		tel.CommandCounter().Add(ctx, 1, nil)
		if err := tel.Shutdown(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCounterAdd(b *testing.B) {
	dir := b.TempDir()
	b.Setenv("XDG_STATE_HOME", dir)

	srv := benchServer()

	user := &amconfig.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: srv.URL,
		},
	}

	state, err := ResolveTelemetryConfig(nil, user, "", "")
	if err != nil {
		b.Fatal(err)
	}
	tel, err := New(context.Background(), state, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		b.StopTimer()
		_ = tel.Shutdown(context.Background())
	}()

	ctx := context.Background()
	attrs := map[string]string{
		string(tmcfg.ResourceAttrCommandName): "bench",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tel.CommandCounter().Add(ctx, 1, attrs)
	}
}

func BenchmarkHistogramRecord(b *testing.B) {
	dir := b.TempDir()
	b.Setenv("XDG_STATE_HOME", dir)

	srv := benchServer()

	user := &amconfig.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: srv.URL,
		},
	}

	state, err := ResolveTelemetryConfig(nil, user, "", "")
	if err != nil {
		b.Fatal(err)
	}
	tel, err := New(context.Background(), state, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		b.StopTimer()
		_ = tel.Shutdown(context.Background())
	}()

	ctx := context.Background()
	attrs := map[string]string{
		string(tmcfg.ResourceAttrCommandName): "bench",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tel.CommandDuration().Record(ctx, 50, attrs)
	}
}

func BenchmarkStartSpanCommandPath(b *testing.B) {
	dir := b.TempDir()
	b.Setenv("XDG_STATE_HOME", dir)

	srv := benchServer()

	user := &amconfig.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelDetailed,
			Endpoint: srv.URL,
		},
	}

	state, err := ResolveTelemetryConfig(nil, user, "", "")
	if err != nil {
		b.Fatal(err)
	}
	tel, err := New(context.Background(), state, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		b.StopTimer()
		_ = tel.Shutdown(context.Background())
	}()

	ctx := NewContext(context.Background(), tel, "sync", time.Now(), "factory", false, Span{}, false)

	attrs := map[string]string{
		string(tmcfg.ResourceAttrCommandName): "sync",
		"command.harnesses":                   "factory",
		"command.managed":                     "false",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, span := tel.StartSpan(ctx, "agent_marketplace.command", attrs)
		span.End()
	}
}

func BenchmarkStartSpan(b *testing.B) {
	dir := b.TempDir()
	b.Setenv("XDG_STATE_HOME", dir)

	srv := benchServer()

	user := &amconfig.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelDetailed,
			Endpoint: srv.URL,
		},
	}

	state, err := ResolveTelemetryConfig(nil, user, "", "")
	if err != nil {
		b.Fatal(err)
	}
	tel, err := New(context.Background(), state, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		b.StopTimer()
		_ = tel.Shutdown(context.Background())
	}()

	ctx := context.Background()
	attrs := map[string]string{
		"span.attr": "bench",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, span := tel.StartSpan(ctx, "bench-span", attrs)
		span.End()
	}
}
