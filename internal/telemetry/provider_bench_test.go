package telemetry

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

func BenchmarkNewShutdown(b *testing.B) {
	dir := b.TempDir()
	b.Setenv("XDG_STATE_HOME", dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Wait for the httptest server to be ready before the benchmark loop.
	if _, err := http.Get(srv.URL); err != nil {
		b.Fatalf("server not ready: %v", err)
	}

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
		_, span := tel.StartSpan(ctx, "bench-span", nil)
		span.End()
		if err := tel.Shutdown(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
