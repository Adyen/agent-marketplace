package telemetry

import (
	"context"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestAuthHeaderResolution(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", dir)

	tenant := filepath.Join(dir, "tenant")
	if err := os.WriteFile(tenant, []byte("tenant-42"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OTEL_AUTH_TOKEN", "token-99")

	var gotHeaders http.Header
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { gotHeaders = r.Header.Clone() })
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	user := &amconfig.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: srv.URL,
			Headers: map[string]tmcfg.HeaderSource{
				"Authorization": {Env: "OTEL_AUTH_TOKEN"},
				"X-Tenant":      {File: tenant},
			},
		},
	}

	state, err := ResolveTelemetryConfig(nil, user, "", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	tel.CommandCounter().Add(context.Background(), 1, nil)

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if gotHeaders.Get("Authorization") != "token-99" {
		t.Fatalf("authorization header = %q, want token-99", gotHeaders.Get("Authorization"))
	}
	if gotHeaders.Get("X-Tenant") != "tenant-42" {
		t.Fatalf("x-tenant header = %q, want tenant-42", gotHeaders.Get("X-Tenant"))
	}
}

func TestExporterDeduplication(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	q := NewQueue(filepath.Join(dir, "queue"))
	cache := newExporterCache()

	live, send, err := buildRouteClients("http://127.0.0.1:12345", nil, "", false, "primary", queueSubdirMetrics, nil, q)
	if err != nil {
		t.Fatalf("build clients: %v", err)
	}
	_ = send

	ctx := context.Background()
	key := cacheKeyFor("http://127.0.0.1:12345", nil, "", false, queueSubdirMetrics)

	exp1, _, err := cache.metricExporter(ctx, key, func() (sdkmetric.Exporter, *http.Client, error) {
		e, err := buildMetricExporter(ctx, live, "http://127.0.0.1:12345", nil)
		return e, send, err
	})
	if err != nil {
		t.Fatalf("metric exporter 1: %v", err)
	}

	exp2, _, err := cache.metricExporter(ctx, key, func() (sdkmetric.Exporter, *http.Client, error) {
		e, err := buildMetricExporter(ctx, live, "http://127.0.0.1:12345", nil)
		return e, send, err
	})
	if err != nil {
		t.Fatalf("metric exporter 2: %v", err)
	}

	if exp1 != exp2 {
		t.Fatal("metric exporter cache did not deduplicate")
	}

	diffKey := cacheKeyFor("http://127.0.0.1:99999", nil, "", false, queueSubdirMetrics)
	exp3, _, err := cache.metricExporter(ctx, diffKey, func() (sdkmetric.Exporter, *http.Client, error) {
		live2, send2, _ := buildRouteClients("http://127.0.0.1:99999", nil, "", false, "primary", queueSubdirMetrics, nil, q)
		e, err := buildMetricExporter(ctx, live2, "http://127.0.0.1:99999", nil)
		return e, send2, err
	})
	if err != nil {
		t.Fatalf("different endpoint exporter: %v", err)
	}
	if exp1 == exp3 {
		t.Fatal("different endpoints should not share an exporter")
	}
}

func TestRouteFolding(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	var metricsCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/metrics" {
			metricsCount.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	user := &amconfig.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:              tmcfg.LevelBasic,
			Endpoint:           srv.URL,
			CommunityReporting: boolPtr(true),
		},
	}

	state, err := ResolveTelemetryConfig(nil, user, srv.URL, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !state.CommunityReportingEnabled {
		t.Fatal("community reporting should be enabled")
	}
	if !state.CommunityReportingFoldedIntoPrimary {
		t.Fatal("community should be folded into primary when endpoints match")
	}

	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	tel.CommandCounter().Add(context.Background(), 1, nil)

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if metricsCount.Load() != 1 {
		t.Fatalf("expected one metric export, got %d", metricsCount.Load())
	}
}

func TestCommunityNotFolded(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	var primaryMetrics, communityMetrics atomic.Int32
	primarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/metrics" {
			primaryMetrics.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer primarySrv.Close()

	communitySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/metrics" {
			communityMetrics.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer communitySrv.Close()

	user := &amconfig.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:              tmcfg.LevelBasic,
			Endpoint:           primarySrv.URL,
			CommunityReporting: boolPtr(true),
		},
	}

	state, err := ResolveTelemetryConfig(nil, user, communitySrv.URL, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !state.CommunityReportingEnabled || state.CommunityReportingFoldedIntoPrimary {
		t.Fatal("community should be enabled and not folded")
	}

	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	tel.CommandCounter().Add(context.Background(), 1, nil)

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if primaryMetrics.Load() != 1 {
		t.Fatalf("expected one primary metric export, got %d", primaryMetrics.Load())
	}
	if communityMetrics.Load() != 1 {
		t.Fatalf("expected one community metric export, got %d", communityMetrics.Load())
	}
}

func TestTLSWithCAFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", dir)

	var gotRequest atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	caFile := writeServerCert(t, dir, srv)

	user := &amconfig.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: srv.URL,
			CAFile:   caFile,
		},
	}

	state, err := ResolveTelemetryConfig(nil, user, "", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	tel.CommandCounter().Add(context.Background(), 1, nil)

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if !gotRequest.Load() {
		t.Fatal("expected TLS export to succeed with ca_file")
	}
}

func TestInsecureHTTPForLoopback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	var gotRequest atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	user := &amconfig.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: srv.URL,
		},
	}

	state, err := ResolveTelemetryConfig(nil, user, "", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	tel.CommandCounter().Add(context.Background(), 1, nil)

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if !gotRequest.Load() {
		t.Fatal("expected plain HTTP export to loopback")
	}
}

func TestOtelEndpointEnvDoesNotOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:99999")

	var gotRequest atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	user := &amconfig.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: srv.URL,
		},
	}

	state, err := ResolveTelemetryConfig(nil, user, "", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	tel.CommandCounter().Add(context.Background(), 1, nil)

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if !gotRequest.Load() {
		t.Fatal("expected configured endpoint to be used despite OTEL endpoint env var")
	}
}

func TestQueueOnExportFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	user := &amconfig.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: srv.URL,
		},
	}

	state, err := ResolveTelemetryConfig(nil, user, "", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	tel, err := New(context.Background(), state, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	tel.CommandCounter().Add(context.Background(), 1, nil)

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown should not return an error, got: %v", err)
	}

	if tel.LastDeliveryFailure() == nil {
		t.Fatal("expected last delivery failure to be recorded")
	}

	qroot := queueRootFromInstallationIDPath(state.InstallationIDPath)
	q := NewQueue(qroot)
	if q.Count() < 1 {
		t.Fatalf("expected queued payloads, got %d", q.Count())
	}
}

func writeServerCert(t *testing.T, dir string, srv *httptest.Server) string {
	t.Helper()
	cert := srv.Certificate()
	if cert == nil {
		t.Fatal("server certificate not available")
	}

	caFile := filepath.Join(dir, "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	if err := os.WriteFile(caFile, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return caFile
}
