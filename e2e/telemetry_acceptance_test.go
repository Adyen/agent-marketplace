package e2e_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// capturedRequest records a single OTLP/HTTP request seen by the test server.
type capturedRequest struct {
	method string
	path   string
	body   []byte
}

// telemetryServer is a test HTTP server that records OTLP/HTTP requests and
// can be configured to fail the first N requests.
type telemetryServer struct {
	*httptest.Server

	mu         sync.Mutex
	requests   []capturedRequest
	count      atomic.Int32
	failFirstN atomic.Int32
}

func newTelemetryServer(t *testing.T) *telemetryServer {
	ts := &telemetryServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		ts.mu.Lock()
		ts.requests = append(ts.requests, capturedRequest{
			method: r.Method,
			path:   r.URL.Path,
			body:   body,
		})
		ts.mu.Unlock()

		idx := ts.count.Add(1) - 1
		if idx < ts.failFirstN.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("server error"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (ts *telemetryServer) requestsByPath(path string) []capturedRequest {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	var out []capturedRequest
	for _, req := range ts.requests {
		if req.path == path {
			out = append(out, req)
		}
	}
	return out
}

func (ts *telemetryServer) setFailFirstN(n int32) {
	ts.failFirstN.Store(n)
}

// telemetryEnv creates a sandbox with a minimal global config so non-catalog
// commands like list run without interactive prompts.
func telemetryEnv(t *testing.T, srv *telemetryServer) *Env {
	env := newEnv(t)
	env.WriteGlobalConfig(`
harnesses:
  - factory
`)
	env.InitGitRepo()
	return env
}

// TestTelemetryDisabledNoExport verifies that when telemetry is explicitly off
// no network request is made.
func TestTelemetryDisabledNoExport(t *testing.T) {
	t.Parallel()
	srv := newTelemetryServer(t)
	env := telemetryEnv(t, srv)

	r := env.RunAMWithEnv(map[string]string{
		"AGENT_MARKETPLACE_TELEMETRY_LEVEL":    "off",
		"AGENT_MARKETPLACE_TELEMETRY_ENDPOINT": "",
	}, "list")
	if r.ExitCode != 0 {
		t.Fatalf("list should exit 0, got %d:\n%s", r.ExitCode, r.Combined)
	}

	if len(srv.requestsByPath("/v1/metrics")) != 0 {
		t.Fatalf("expected no metrics export when telemetry is off, got %d /v1/metrics requests", len(srv.requestsByPath("/v1/metrics")))
	}
	if len(srv.requestsByPath("/v1/traces")) != 0 {
		t.Fatalf("expected no traces export when telemetry is off, got %d /v1/traces requests", len(srv.requestsByPath("/v1/traces")))
	}
}

// TestTelemetryBasicExport verifies that basic telemetry emits only metrics to
// the primary endpoint.
func TestTelemetryBasicExport(t *testing.T) {
	t.Parallel()
	srv := newTelemetryServer(t)
	env := telemetryEnv(t, srv)

	r := env.RunAMWithEnv(map[string]string{
		"AGENT_MARKETPLACE_TELEMETRY_LEVEL":    "basic",
		"AGENT_MARKETPLACE_TELEMETRY_ENDPOINT": srv.URL,
	}, "list")
	if r.ExitCode != 0 {
		t.Fatalf("list should exit 0, got %d:\n%s", r.ExitCode, r.Combined)
	}

	metrics := srv.requestsByPath("/v1/metrics")
	if len(metrics) == 0 {
		t.Fatalf("expected at least one /v1/metrics request, got none\n%s", r.Combined)
	}
	if len(srv.requestsByPath("/v1/traces")) != 0 {
		t.Fatalf("basic telemetry should not export traces, got %d /v1/traces requests", len(srv.requestsByPath("/v1/traces")))
	}
}

// TestTelemetryDetailedExport verifies that detailed telemetry emits both
// metrics and traces.
func TestTelemetryDetailedExport(t *testing.T) {
	t.Parallel()
	srv := newTelemetryServer(t)
	env := telemetryEnv(t, srv)

	r := env.RunAMWithEnv(map[string]string{
		"AGENT_MARKETPLACE_TELEMETRY_LEVEL":    "detailed",
		"AGENT_MARKETPLACE_TELEMETRY_ENDPOINT": srv.URL,
	}, "list")
	if r.ExitCode != 0 {
		t.Fatalf("list should exit 0, got %d:\n%s", r.ExitCode, r.Combined)
	}

	if len(srv.requestsByPath("/v1/metrics")) == 0 {
		t.Fatalf("expected /v1/metrics request, got none\n%s", r.Combined)
	}
	if len(srv.requestsByPath("/v1/traces")) == 0 {
		t.Fatalf("expected /v1/traces request, got none\n%s", r.Combined)
	}
}

// TestTelemetryDiagnosticExport verifies that --diagnostic captures and exports a
// reviewed payload, including both metrics and traces, and prints a trace ID.
func TestTelemetryDiagnosticExport(t *testing.T) {
	t.Parallel()
	srv := newTelemetryServer(t)
	env := telemetryEnv(t, srv)

	r := env.RunAMWithEnv(map[string]string{
		"AGENT_MARKETPLACE_TELEMETRY_LEVEL":    "detailed",
		"AGENT_MARKETPLACE_TELEMETRY_ENDPOINT": srv.URL,
	}, "--diagnostic", "--diagnostic-accept-sensitive-data", "list")
	if r.ExitCode != 0 {
		t.Fatalf("diagnostic list should exit 0, got %d:\n%s", r.ExitCode, r.Combined)
	}

	if !strings.Contains(r.Combined, "Diagnostic export succeeded.") {
		t.Fatalf("expected diagnostic export success message, got:\n%s", r.Combined)
	}
	if !strings.Contains(r.Combined, "trace id:") {
		t.Fatalf("expected trace id in output, got:\n%s", r.Combined)
	}
	if len(srv.requestsByPath("/v1/metrics")) == 0 {
		t.Fatalf("expected /v1/metrics request, got none\n%s", r.Combined)
	}
	if len(srv.requestsByPath("/v1/traces")) == 0 {
		t.Fatalf("expected /v1/traces request, got none\n%s", r.Combined)
	}
}

// TestTelemetryOfflineQueue verifies that failed exports are queued and drained
// on the next successful command.
func TestTelemetryOfflineQueue(t *testing.T) {
	t.Parallel()
	srv := newTelemetryServer(t)
	env := telemetryEnv(t, srv)

	// First invocation: server fails the export. Telemetry failure must not
	// change the command exit status.
	srv.setFailFirstN(1)
	r := env.RunAMWithEnv(map[string]string{
		"AGENT_MARKETPLACE_TELEMETRY_LEVEL":    "basic",
		"AGENT_MARKETPLACE_TELEMETRY_ENDPOINT": srv.URL,
	}, "list")
	if r.ExitCode != 0 {
		t.Fatalf("list should exit 0 despite export failure, got %d:\n%s", r.ExitCode, r.Combined)
	}

	status := env.RunAM("telemetry", "status")
	if status.ExitCode != 0 {
		t.Fatalf("telemetry status should exit 0, got %d:\n%s", status.ExitCode, status.Combined)
	}
	if !strings.Contains(status.Combined, "Pending records:       1") {
		t.Fatalf("expected one pending queued record after failed export, got:\n%s", status.Combined)
	}

	// Second invocation: server succeeds. The queue is drained before the
	// current invocation's metrics are exported.
	srv.setFailFirstN(0)
	r = env.RunAMWithEnv(map[string]string{
		"AGENT_MARKETPLACE_TELEMETRY_LEVEL":    "basic",
		"AGENT_MARKETPLACE_TELEMETRY_ENDPOINT": srv.URL,
	}, "list")
	if r.ExitCode != 0 {
		t.Fatalf("list should exit 0, got %d:\n%s", r.ExitCode, r.Combined)
	}

	status = env.RunAM("telemetry", "status")
	if status.ExitCode != 0 {
		t.Fatalf("telemetry status should exit 0, got %d:\n%s", status.ExitCode, status.Combined)
	}
	if !strings.Contains(status.Combined, "Pending records:       0") {
		t.Fatalf("expected zero pending queued records after successful drain, got:\n%s", status.Combined)
	}

	// The final successful invocation plus the drained queue should produce at
	// least two successful /v1/metrics requests.
	metrics := srv.requestsByPath("/v1/metrics")
	if len(metrics) < 2 {
		t.Fatalf("expected at least 2 /v1/metrics requests, got %d", len(metrics))
	}
}

// TestTelemetryPurge verifies that telemetry purge removes queued records and
// retained diagnostic payloads.
func TestTelemetryPurge(t *testing.T) {
	t.Parallel()
	srv := newTelemetryServer(t)
	env := telemetryEnv(t, srv)

	srv.setFailFirstN(1)
	r := env.RunAMWithEnv(map[string]string{
		"AGENT_MARKETPLACE_TELEMETRY_LEVEL":    "basic",
		"AGENT_MARKETPLACE_TELEMETRY_ENDPOINT": srv.URL,
	}, "list")
	if r.ExitCode != 0 {
		t.Fatalf("list should exit 0, got %d:\n%s", r.ExitCode, r.Combined)
	}

	status := env.RunAM("telemetry", "status")
	if !strings.Contains(status.Combined, "Pending records:       1") {
		t.Fatalf("expected one pending queued record, got:\n%s", status.Combined)
	}

	purge := env.RunAM("telemetry", "purge")
	if purge.ExitCode != 0 {
		t.Fatalf("telemetry purge should exit 0, got %d:\n%s", purge.ExitCode, purge.Combined)
	}
	if !strings.Contains(purge.Combined, "Purged 1 queued record") {
		t.Fatalf("expected purge to remove 1 queued record, got:\n%s", purge.Combined)
	}

	status = env.RunAM("telemetry", "status")
	if !strings.Contains(status.Combined, "Pending records:       0") {
		t.Fatalf("expected zero pending queued records after purge, got:\n%s", status.Combined)
	}
}

// TestTelemetryManagedOrganizationLevel verifies that an organization policy sets
// the effective telemetry level even when the user has not enabled it.
func TestTelemetryManagedOrganizationLevel(t *testing.T) {
	t.Parallel()
	srv := newTelemetryServer(t)
	env := telemetryEnv(t, srv)

	env.WriteOrgConfig(`
apiVersion: agent-marketplace/v3
harnesses:
  - factory
telemetry:
  level: basic
  endpoint: ` + srv.URL + `
`)

	status := env.RunAM("telemetry", "status")
	if status.ExitCode != 0 {
		t.Fatalf("telemetry status should exit 0, got %d:\n%s", status.ExitCode, status.Combined)
	}
	if !strings.Contains(status.Combined, "Managed installation:  true") {
		t.Fatalf("expected managed installation, got:\n%s", status.Combined)
	}
	if !strings.Contains(status.Combined, "Effective level:       basic (org)") {
		t.Fatalf("expected organization-enforced basic level, got:\n%s", status.Combined)
	}

	r := env.RunAM("list")
	if r.ExitCode != 0 {
		t.Fatalf("list should exit 0, got %d:\n%s", r.ExitCode, r.Combined)
	}
	if len(srv.requestsByPath("/v1/metrics")) == 0 {
		t.Fatalf("expected managed basic telemetry to export metrics, got none\n%s", r.Combined)
	}
}
