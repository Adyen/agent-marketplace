package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
)

type stubRoundTripper func(*http.Request) (*http.Response, error)

func (f stubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// spec: configuration-settings/telemetry-endpoint-credentials-are-not-logged
// spec: configuration-settings/unparseable-telemetry-urls-fail-closed
func TestTelemetryEndpointCredentialsAreRedacted(t *testing.T) {
	setStateRoot(t)
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	var logged []string
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			logged = append(logged, attr.Key+"="+attr.Value.String())
			return attr
		},
	})))
	ctx := context.Background()
	q := NewQueue(filepath.Join(t.TempDir(), "queue"))

	endpoint := "https://am-user:am-secret@127.0.0.1:1/otlp?token=am-query-secret am-space-secret"
	live, _, err := buildRouteClients(endpoint, nil, "", false, "primary", queueSubdirMetrics, nil, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildMetricExporter(ctx, live, endpoint, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := buildTraceExporter(ctx, live, endpoint, nil); err != nil {
		t.Fatal(err)
	}

	var errs []error
	_, _, err = buildRouteClients("http://am-user:am-secret@203.0.113.10/otlp?token=am-query-secret am-space-secret", nil, "", false, "primary", queueSubdirMetrics, nil, q)
	errs = append(errs, err)

	override := "https://am-user:am-secret@env.example/otlp?token=am-query-secret am-space-secret"
	t.Setenv(EnvEndpoint, override)
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, amconfig.Settings{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if state.PrimaryEndpoint != override {
		t.Fatalf("primary endpoint = %q, want configured value retained for delivery", state.PrimaryEndpoint)
	}

	offline := errors.New("offline")
	queued := func(rawURL string) []byte {
		t.Helper()
		envelope, err := json.Marshal(Envelope{Route: "primary", Signal: queueSubdirMetrics, URL: rawURL})
		if err != nil {
			t.Fatal(err)
		}
		return envelope
	}
	envelope := queued("https://am-user:am-secret@queue.example/v1/metrics?token=am-query-secret am-space-secret")
	for _, respond := range []stubRoundTripper{
		func(*http.Request) (*http.Response, error) { return nil, offline },
		func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Status: "503 Service Unavailable", Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		},
	} {
		tel := &Telemetry{sendClients: map[string]*http.Client{"primary": {Transport: respond}}}
		errs = append(errs, tel.send(ctx, envelope))
	}
	malformed := &Telemetry{sendClients: map[string]*http.Client{"primary": http.DefaultClient}}
	errs = append(errs, malformed.send(ctx, queued("https://am-user:am-secret@queue.example/v1/%zz?token=am-query-secret am-space-secret")))

	values := slices.Clone(logged)
	for i, err := range errs {
		if err == nil {
			t.Fatalf("error %d = nil", i)
		}
		values = append(values, err.Error())
	}
	if !errors.Is(errs[1], offline) {
		t.Fatalf("queued delivery error lost its cause: %v", errs[1])
	}
	if replay := errs[len(errs)-1].Error(); !strings.Contains(replay, "[redacted URL]") {
		t.Fatalf("malformed queued URL did not fail closed: %q", replay)
	}
	for _, value := range values {
		if strings.Contains(value, "am-user") || strings.Contains(value, "am-secret") || strings.Contains(value, "am-query-secret") || strings.Contains(value, "am-space-secret") {
			t.Fatalf("telemetry URL credentials leaked: %q", value)
		}
	}
	for _, want := range []string{"127.0.0.1:1/otlp", "203.0.113.10/otlp", "env.example/otlp", "queue.example/v1/metrics"} {
		if !slices.ContainsFunc(values, func(value string) bool { return strings.Contains(value, want) }) {
			t.Fatalf("logs and errors no longer identify %s: %q", want, values)
		}
	}
}
