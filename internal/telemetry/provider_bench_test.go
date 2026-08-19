package telemetry

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

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
