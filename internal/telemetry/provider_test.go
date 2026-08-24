package telemetry

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	metricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestNewDeliversMetricsAndTraces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	var mu sync.Mutex
	var gotMetrics bool
	var gotTraces bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1/metrics":
			var req metricspb.ExportMetricsServiceRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				t.Errorf("unmarshal metrics: %v", err)
			}
			mu.Lock()
			gotMetrics = true
			mu.Unlock()
		case "/v1/traces":
			var req tracepb.ExportTraceServiceRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				t.Errorf("unmarshal traces: %v", err)
			}
			mu.Lock()
			gotTraces = true
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	user := &amconfig.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelDetailed,
			Endpoint: srv.URL,
		},
	}

	state, err := ResolveTelemetryConfig(nil, user, "", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tel, err := New(ctx, state, logger)
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}

	tel.CommandCounter().Add(ctx, 1, map[string]string{
		string(tmcfg.ResourceAttrCommandName): "test",
	})
	tel.CommandDuration().Record(ctx, 100, map[string]string{
		string(tmcfg.ResourceAttrCommandName): "test",
	})

	spanCtx, span := tel.StartSpan(ctx, "test-span", map[string]string{"key": "value"})
	_ = spanCtx
	span.End()

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := tel.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !gotMetrics {
		t.Error("expected metrics export")
	}
	if !gotTraces {
		t.Error("expected traces export")
	}
}

func TestTelemetryOffDoesNotCreateProviders(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	state, err := ResolveTelemetryConfig(nil, nil, "", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	tel, err := New(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	tel.CommandCounter().Add(context.Background(), 1, nil)
	tel.CommandDuration().Record(context.Background(), 100, nil)
	_, span := tel.StartSpan(context.Background(), "span", nil)
	span.End()

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown off: %v", err)
	}

	if tel.mp != nil || tel.tp != nil {
		t.Fatalf("providers should not be created when level is off")
	}
}

func TestCounterAndHistogramDoNotLeakSDKTypes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer func() {
		if err := tel.Shutdown(context.Background()); err != nil {
			t.Logf("shutdown: %v", err)
		}
	}()

	c := tel.CommandCounter()
	h := tel.CommandDuration()

	c.Add(context.Background(), 1, map[string]string{"command.name": "x"})
	h.Record(context.Background(), 50, map[string]string{"command.name": "x"})

	// The public wrappers expose only methods, not OpenTelemetry types.
	_ = c
	_ = h
}

func TestMetricAttributeAllowlist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	var mu sync.Mutex
	var lastReq *metricspb.ExportMetricsServiceRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/v1/metrics" {
			var req metricspb.ExportMetricsServiceRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				t.Errorf("unmarshal metrics: %v", err)
			}
			mu.Lock()
			lastReq = &req
			mu.Unlock()
		}
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

	attrs := map[string]string{
		string(tmcfg.ResourceAttrCommandName):    "test",
		string(tmcfg.ResourceAttrInstallationID): "must-not-appear-as-instrument-label",
		"disallowed.attr":                        "value",
	}
	tel.CommandCounter().Add(context.Background(), 1, attrs)
	tel.CommandDuration().Record(context.Background(), 100, attrs)

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	mu.Lock()
	req := lastReq
	mu.Unlock()

	if req == nil {
		t.Fatal("no metrics export received")
	}
	if len(req.ResourceMetrics) == 0 || len(req.ResourceMetrics[0].ScopeMetrics) == 0 {
		t.Fatal("expected resource metrics in export request")
	}

	resourceAttrs := make(map[string]bool)
	for _, kv := range req.ResourceMetrics[0].Resource.Attributes {
		resourceAttrs[kv.Key] = true
	}
	if !resourceAttrs[string(tmcfg.ResourceAttrInstallationID)] {
		t.Error("expected installation.id as a resource attribute")
	}

	allowed := make(map[string]struct{})
	for _, a := range tmcfg.MetricSchemaAttributes {
		allowed[string(a)] = struct{}{}
	}

	for _, sm := range req.ResourceMetrics[0].ScopeMetrics {
		for _, m := range sm.Metrics {
			if s := m.GetSum(); s != nil {
				for _, dp := range s.DataPoints {
					assertInstrumentAttributes(t, dp.GetAttributes(), allowed)
				}
			}
			if h := m.GetHistogram(); h != nil {
				for _, dp := range h.DataPoints {
					assertInstrumentAttributes(t, dp.GetAttributes(), allowed)
				}
			}
		}
	}
}

func assertInstrumentAttributes(t *testing.T, attrs []*commonpb.KeyValue, allowed map[string]struct{}) {
	t.Helper()
	for _, kv := range attrs {
		if kv.Key == string(tmcfg.ResourceAttrInstallationID) {
			t.Errorf("instrument attribute must not include %q", kv.Key)
		}
		if _, ok := allowed[kv.Key]; !ok {
			t.Errorf("instrument attribute %q is not in the metric allowlist", kv.Key)
		}
	}
}

func TestInstallationIDPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry", "installation-id")

	id, err := LoadInstallationID(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(id) != 64 {
		t.Fatalf("expected 64 hex chars, got %d: %q", len(id), id)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected file mode 0o600, got 0o%o", info.Mode().Perm())
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("expected dir mode 0o700, got 0o%o", dirInfo.Mode().Perm())
	}

	// Re-loading should return the same ID.
	id2, err := LoadInstallationID(path)
	if err != nil {
		t.Fatalf("load2: %v", err)
	}
	if id2 != id {
		t.Fatalf("id changed: %q -> %q", id, id2)
	}
}

func TestResetInstallationID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry", "installation-id")
	queueRoot := filepath.Join(dir, "telemetry", "queue")

	if _, err := LoadInstallationID(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(queueRoot, "metrics"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(queueRoot, "metrics", "old.json"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ResetInstallationID(path); err != nil {
		t.Fatalf("reset: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("installation id file should be removed")
	}
	if _, err := os.Stat(queueRoot); !os.IsNotExist(err) {
		t.Fatal("queue root should be removed")
	}

	// A new ID is generated on the next load.
	id, err := LoadInstallationID(path)
	if err != nil {
		t.Fatalf("load after reset: %v", err)
	}
	if len(id) != 64 {
		t.Fatalf("expected new id")
	}
}
