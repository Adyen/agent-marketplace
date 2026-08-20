package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func BenchmarkTelemetryCommandPath(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfgDir := filepath.Join(home, ".agents")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		b.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: agent-marketplace/v3
harnesses:
  - factory
telemetry:
  level: basic
  endpoint: %s
`, srv.URL)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		b.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	oldLog := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(oldLog)

	cmd := &cobra.Command{Use: "agent-marketplace bench"}
	cmd.SetContext(context.Background())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		initTelemetry(cmd, start)
		finishTelemetry(cmd, nil)
	}
}
