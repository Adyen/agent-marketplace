package telemetry

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
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

// benchDiscardLogs points the package-wide slog default at io.Discard for the
// duration of a benchmark and returns a function that restores the previous
// logger. The telemetry functions being benchmarked log through slog.Default()
// during setup, which otherwise interleaves with testing's benchmark output.
func benchDiscardLogs() func() {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	old := slog.Default()
	slog.SetDefault(discard)
	return func() { slog.SetDefault(old) }
}

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

	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: srv.URL,
		},
	}

	defer benchDiscardLogs()()

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
		if err != nil {
			b.Fatal(err)
		}
		tel, err := New(ctx, state, nil)
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
	defer benchDiscardLogs()()

	dir := b.TempDir()
	b.Setenv("XDG_STATE_HOME", dir)

	srv := benchServer()

	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: srv.URL,
		},
	}

	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
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
	defer benchDiscardLogs()()

	dir := b.TempDir()
	b.Setenv("XDG_STATE_HOME", dir)

	srv := benchServer()

	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: srv.URL,
		},
	}

	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
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

func BenchmarkRecordInventory(b *testing.B) {
	defer benchDiscardLogs()()

	dir := b.TempDir()
	b.Setenv("XDG_STATE_HOME", dir)
	srv := benchServer()
	user := amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelDetailed, Endpoint: srv.URL}}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
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

	snapshot := InventorySnapshot{
		Fingerprint:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Harnesses:    "claude,codex,devin,factory",
		HarnessScope: "repo",
		Registries:   make([]InventoryRegistry, 10),
		Resources:    make([]InventoryResource, 100),
	}
	for i := range snapshot.Registries {
		snapshot.Registries[i] = InventoryRegistry{
			ID: fmt.Sprintf("github.com/example/registry-%d", i), Location: fmt.Sprintf("https://github.com/example/registry-%d", i),
			Ref: "main", ConfigScope: "user", Default: true,
		}
	}
	for i := range snapshot.Resources {
		registry := snapshot.Registries[i%len(snapshot.Registries)]
		snapshot.Resources[i] = InventoryResource{
			RegistryID: registry.ID, RegistryLocation: registry.Location, RegistryRef: "main",
			Kind: "skill", Name: fmt.Sprintf("resource-%d", i), ConfigScope: "repo",
		}
	}

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Keep this an accepted-observation benchmark rather than measuring the
		// bounded queue-full fallback after the producer outruns local OTLP.
		for len(tel.inventoryQueue) >= inventoryQueueCapacity/2 {
			runtime.Gosched()
		}
		tel.RecordInventory(ctx, snapshot)
	}
}

func BenchmarkStartSpanCommandPath(b *testing.B) {
	defer benchDiscardLogs()()

	dir := b.TempDir()
	b.Setenv("XDG_STATE_HOME", dir)

	srv := benchServer()

	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelDetailed,
			Endpoint: srv.URL,
		},
	}

	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
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
	defer benchDiscardLogs()()

	dir := b.TempDir()
	b.Setenv("XDG_STATE_HOME", dir)

	srv := benchServer()

	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelDetailed,
			Endpoint: srv.URL,
		},
	}

	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
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
