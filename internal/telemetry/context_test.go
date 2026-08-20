package telemetry

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

func TestNewContextCarriesRootSpan(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	tel, err := New(context.Background(), state, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	t.Cleanup(func() { _ = tel.Shutdown(context.Background()) })

	_, rootSpan := tel.StartSpan(context.Background(), "test-root", nil)
	ctx := NewContext(context.Background(), nil, "test", time.Now(), "factory", false, rootSpan)

	got := RootSpanFromContext(ctx)
	if got == (Span{}) {
		t.Fatal("expected root span to be stored in context")
	}
}

func TestRootSpanFromContextReturnsEmptyWhenMissing(t *testing.T) {
	got := RootSpanFromContext(context.Background())
	if got != (Span{}) {
		t.Fatalf("expected empty span, got %v", got)
	}
}

func TestRootSpanFromContextHandlesNilContext(t *testing.T) {
	got := RootSpanFromContext(nil) //nolint:staticcheck // explicit nil handling test
	if got != (Span{}) {
		t.Fatalf("expected empty span for nil context, got %v", got)
	}
}
