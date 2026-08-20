package pipeline

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
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	tracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracev1pb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type pipelineSpanExporter struct {
	mu     sync.Mutex
	spans  []*tracev1pb.Span
	server *httptest.Server
}

func newPipelineSpanExporter(t *testing.T) *pipelineSpanExporter {
	t.Helper()
	e := &pipelineSpanExporter{}
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

func (e *pipelineSpanExporter) hasSpan(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.spans {
		if s.Name == name {
			return true
		}
	}
	return false
}

func setupPipelineTelemetry(t *testing.T, e *pipelineSpanExporter) (context.Context, *telemetry.Telemetry) {
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

	ctx := telemetry.NewContext(context.Background(), tel, "sync", time.Now(), "factory", false, telemetry.Span{}, false)
	return ctx, tel
}

func TestResolveEmitsLifecycleSpans(t *testing.T) {
	e := newPipelineSpanExporter(t)
	ctx, tel := setupPipelineTelemetry(t, e)
	defer func() { _ = tel.Shutdown(context.Background()) }()

	repoRoot := t.TempDir()
	_ = Resolve(ctx, ResolveOptions{
		RepoRoot: repoRoot,
		Loaders: Loaders{
			Org:    func() (*amconfig.Config, error) { return nil, nil },
			Global: func() (*amconfig.Config, error) { return nil, nil },
			Repo:   func() (*amconfig.Config, error) { return nil, nil },
			Local:  func() (*amconfig.LocalConfig, error) { return nil, nil },
		},
	})

	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	for _, name := range []string{
		"agent_marketplace.pipeline.resolve",
		"agent_marketplace.pipeline.config_load",
		"agent_marketplace.pipeline.selection",
	} {
		if !e.hasSpan(name) {
			t.Errorf("expected span %q to be emitted", name)
		}
	}
}
