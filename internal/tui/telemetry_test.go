package tui

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	tracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracev1pb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type testSpanExporter struct {
	mu     sync.Mutex
	spans  []*tracev1pb.Span
	server *httptest.Server
}

func newTestSpanExporter(t *testing.T) *testSpanExporter {
	t.Helper()
	e := &testSpanExporter{}
	e.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/metrics" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/v1/traces" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			w.WriteHeader(http.StatusOK)
			return
		}
		var req tracepb.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshal traces: %v", err)
			w.WriteHeader(http.StatusOK)
			return
		}
		e.mu.Lock()
		for _, rs := range req.ResourceSpans {
			for _, ils := range rs.ScopeSpans {
				e.spans = append(e.spans, ils.Spans...)
			}
		}
		e.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(e.server.Close)
	return e
}

func (e *testSpanExporter) findSpan(name string) *tracev1pb.Span {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.spans {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func attrValue(attrs []*commonpb.KeyValue, key string) string {
	for _, a := range attrs {
		if a.Key == key && a.Value != nil {
			return a.Value.GetStringValue()
		}
	}
	return ""
}

func setupTuiTelemetry(t *testing.T, e *testSpanExporter) (context.Context, *telemetry.Telemetry) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	user := &amconfig.Config{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelDetailed,
			Endpoint: e.server.URL,
		},
	}

	state, err := telemetry.ResolveTelemetryConfig(nil, user, "", "")
	if err != nil {
		t.Fatalf("resolve telemetry config: %v", err)
	}

	tel, err := telemetry.New(context.Background(), state, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}

	ctx := telemetry.NewContext(context.Background(), tel, "tui", time.Now(), "factory", false, telemetry.Span{})
	return ctx, tel
}

func TestRecordActionSpanEmits(t *testing.T) {
	e := newTestSpanExporter(t)
	ctx, tel := setupTuiTelemetry(t, e)
	defer func() { _ = tel.Shutdown(context.Background()) }()

	recordActionSpan(ctx, "tui.action.test", map[string]string{"resource.kind": "skill"})

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	s := e.findSpan("agent_marketplace.tui.action")
	if s == nil {
		t.Fatal("expected agent_marketplace.tui.action span")
	}
	if got := attrValue(s.Attributes, "action"); got != "tui.action.test" {
		t.Fatalf("expected action attribute %q, got %q", "tui.action.test", got)
	}
	if got := attrValue(s.Attributes, "resource.kind"); got != "skill" {
		t.Fatalf("expected resource.kind attribute %q, got %q", "skill", got)
	}
}

func TestRecordActionSpanNoTelemetry(t *testing.T) {
	ctx := context.Background()
	recordActionSpan(ctx, "tui.action.test", nil)
}

func TestRecordControlledErrorSpanEmits(t *testing.T) {
	e := newTestSpanExporter(t)
	ctx, tel := setupTuiTelemetry(t, e)
	defer func() { _ = tel.Shutdown(context.Background()) }()

	recordControlledErrorSpan(ctx, "sync", errors.New("boom"))

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	s := e.findSpan("agent_marketplace.tui.controlled_error")
	if s == nil {
		t.Fatal("expected agent_marketplace.tui.controlled_error span")
	}
	if got := attrValue(s.Attributes, "action"); got != "sync" {
		t.Fatalf("expected action attribute %q, got %q", "sync", got)
	}
}
