package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
)

// spec: marketplace-api/api-lifecycle-owns-refresh-work
func TestNewServerStartsNoRefreshWorkAndReadyzIgnoresTelemetry(t *testing.T) {
	public, fixture, server, _ := adoptionSearchSetup(t, adoptionSearchSamples(), false)

	result, _ := getSearch(t, public.URL, "?sort=installations")
	if len(result.Items) == 0 {
		t.Fatal("search returned no items")
	}
	for _, item := range result.Items {
		if item.InstallationCount != nil {
			t.Fatalf("unseeded snapshot served a count: %+v", item)
		}
	}
	if hits := fixture.hits.Load(); hits != 0 {
		t.Fatalf("handler access or search issued telemetry: hits = %d", hits)
	}

	recorder := httptest.NewRecorder()
	server.OpsHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("readyz gated on telemetry: %d %s", recorder.Code, recorder.Body.String())
	}
	if hits := fixture.hits.Load(); hits != 0 {
		t.Fatalf("readyz issued telemetry: hits = %d", hits)
	}
}

// spec: marketplace-api/api-lifecycle-owns-refresh-work
func TestDisabledInstallationMetricsCreatesNoWorker(t *testing.T) {
	_, _, _, meta := adoptionSearchSetup(t, adoptionSearchSamples(), false)
	server := NewServer(config.Config{
		Instance: config.Instance{ID: "mp", Name: "Marketplace", CanonicalURL: "https://example.com"},
	}, meta, fsblob.New(t.TempDir()))
	if server.installations != nil {
		t.Fatal("NewServer created an installation reader without a metrics URL")
	}
	public := httptest.NewServer(server.PublicHandler())
	t.Cleanup(public.Close)
	result, body := getSearch(t, public.URL, "?sort=installations")
	if len(result.Items) == 0 || strings.Contains(body, "installation_count") {
		t.Fatalf("disabled metrics changed search: %s", body)
	}
}

// spec: marketplace-api/api-lifecycle-owns-refresh-work
func TestStartOwnsWorkerStartupAndShutdown(t *testing.T) {
	_, fixture, server, _ := adoptionSearchSetup(t, adoptionSearchSamples())
	server.installations.timing.Timeout = time.Minute
	server.installations.client.Timeout = time.Minute
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	fetchStarted, backendCancelled := make(chan struct{}), make(chan struct{})
	fixture.handler.Store(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-fetchStarted:
		default:
			close(fetchStarted)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			close(backendCancelled)
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	})

	ctx, cancel := context.WithCancel(context.Background())
	startErr := make(chan error, 1)
	go func() { startErr <- server.Start(ctx) }()
	finished := false
	t.Cleanup(func() {
		cancel()
		releaseOnce.Do(func() { close(release) })
		if !finished {
			select {
			case <-startErr:
			case <-time.After(5 * time.Second):
				t.Error("Start did not return after cancellation")
			}
		}
	})

	select {
	case <-fetchStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not attempt a refresh after Start")
	}
	select {
	case err := <-startErr:
		finished = true
		t.Fatalf("Start returned while telemetry was blocked: %v", err)
	default:
	}

	recorder := httptest.NewRecorder()
	server.OpsHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("readiness gated on blocked telemetry: %d", recorder.Code)
	}

	cancel()
	select {
	case <-backendCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel the in-flight backend request")
	}
	select {
	case err := <-startErr:
		finished = true
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Start() error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not join the worker")
	}
	if hits := fixture.hits.Load(); hits != 1 {
		t.Fatalf("hits = %d, want exactly one blocked attempt", hits)
	}
}
