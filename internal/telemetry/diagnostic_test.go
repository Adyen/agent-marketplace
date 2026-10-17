package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

func TestDiagnosticContextFlag(t *testing.T) {
	_ = setupDiagnosticTest(t)

	user := amconfig.Settings{
		Harnesses: []string{"factory"},
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelDetailed,
			Endpoint: "http://127.0.0.1:1",
		},
	}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		t.Fatalf("resolve telemetry config: %v", err)
	}

	tel, err := New(context.Background(), state, nil, WithDiagnostic(true))
	if err != nil {
		t.Fatalf("new diagnostic telemetry: %v", err)
	}
	defer func() { _ = tel.Shutdown(context.Background()) }()

	if !tel.DiagnosticActive() {
		t.Fatal("expected diagnostic telemetry to be active")
	}

	ctx := NewContext(context.Background(), tel, "test", time.Now(), "factory", false, Span{}, true)
	if !DiagnosticFromContext(ctx) {
		t.Fatal("expected diagnostic flag in context")
	}
}

// spec: telemetry/diagnostics-require-detailed-telemetry
func TestDiagnosticRequiresDetailedLevel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	user := amconfig.Settings{
		Harnesses: []string{"factory"},
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: "http://127.0.0.1:1",
		},
	}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		t.Fatalf("resolve telemetry config: %v", err)
	}

	tel, err := New(context.Background(), state, nil, WithDiagnostic(true))
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	defer func() { _ = tel.Shutdown(context.Background()) }()

	if tel.DiagnosticActive() {
		t.Fatal("diagnostic capture should require detailed level")
	}
}

// spec: telemetry/diagnostic-failure-retains-redacted-review-evidence
func TestDiagnosticPayloadCreatedAndRedacted(t *testing.T) {
	_ = setupDiagnosticTest(t)

	var gotMetrics, gotTraces bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/metrics":
			gotMetrics = true
		case "/v1/traces":
			gotTraces = true
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	tel, state := newDiagnosticTelemetry(t, srv.URL)
	defer func() { _ = tel.Shutdown(context.Background()) }()

	_, rootSpan := tel.StartSpan(context.Background(), "agent_marketplace.command", map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-test",
		"registry.url":                        "https://user:token@example.com/repo.git",
	})
	rootSpan.End()

	tel.CommandCounter().Add(context.Background(), 1, map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-test",
	})
	tel.CommandDuration().Record(context.Background(), 100, map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-test",
	})

	var out bytes.Buffer
	err := tel.FinishDiagnostic(context.Background(), &out, strings.NewReader(""), false, true)
	if err == nil {
		t.Fatal("expected diagnostic export to fail")
	}

	path := filepath.Join(state.StateRoot(), "diagnostic", "payload-"+rootSpan.TraceID()+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected retained payload file: %v", err)
	}

	var payload DiagnosticPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal diagnostic payload: %v", err)
	}

	if payload.TraceID != rootSpan.TraceID() {
		t.Fatalf("trace id mismatch: %q vs %q", payload.TraceID, rootSpan.TraceID())
	}
	if payload.InstallationID != tel.InstallationID() {
		t.Fatalf("installation id mismatch")
	}

	content := string(data)
	if strings.Contains(content, "token@") {
		t.Fatalf("payload should not contain URL credentials: %s", content)
	}
	if !strings.Contains(content, "https://***@example.com/repo.git") {
		t.Fatalf("payload should contain redacted URL: %s", content)
	}
	if !gotMetrics {
		t.Fatal("expected metrics export attempt")
	}
	if !gotTraces {
		t.Fatal("expected traces export attempt")
	}
}

// spec: telemetry/successful-diagnostic-export-removes-review-evidence
func TestDiagnosticExportSuccessDeletesPayload(t *testing.T) {
	_ = setupDiagnosticTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tel, state := newDiagnosticTelemetry(t, srv.URL)
	defer func() { _ = tel.Shutdown(context.Background()) }()

	_, rootSpan := tel.StartSpan(context.Background(), "agent_marketplace.command", map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-test",
	})
	rootSpan.End()

	tel.CommandCounter().Add(context.Background(), 1, map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-test",
	})
	tel.CommandDuration().Record(context.Background(), 100, map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-test",
	})

	var out bytes.Buffer
	if err := tel.FinishDiagnostic(context.Background(), &out, strings.NewReader(""), false, true); err != nil {
		t.Fatalf("finish diagnostic: %v\n%s", err, out.String())
	}

	path := filepath.Join(state.StateRoot(), "diagnostic", "payload-"+rootSpan.TraceID()+".json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("payload file should have been deleted: %s", path)
	}

	output := out.String()
	if !strings.Contains(output, "Diagnostic export succeeded") {
		t.Fatalf("expected success message, got:\n%s", output)
	}
	if !strings.Contains(output, "trace id:") {
		t.Fatalf("expected trace id in output, got:\n%s", output)
	}
	if !strings.Contains(output, "installation id:") {
		t.Fatalf("expected installation id in output, got:\n%s", output)
	}
}

// spec: telemetry/noninteractive-diagnostics-require-acknowledgement
func TestDiagnosticNonInteractiveRequiresAck(t *testing.T) {
	_ = setupDiagnosticTest(t)

	tel, state := newDiagnosticTelemetry(t, "http://127.0.0.1:1")
	defer func() { _ = tel.Shutdown(context.Background()) }()

	_, rootSpan := tel.StartSpan(context.Background(), "agent_marketplace.command", map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-test",
	})
	rootSpan.End()

	tel.CommandCounter().Add(context.Background(), 1, map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-test",
	})
	tel.CommandDuration().Record(context.Background(), 100, map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-test",
	})

	var out bytes.Buffer
	if err := tel.FinishDiagnostic(context.Background(), &out, strings.NewReader(""), false, false); err != nil {
		t.Fatalf("finish diagnostic should not error when declined: %v", err)
	}

	path := filepath.Join(state.StateRoot(), "diagnostic", "payload-"+rootSpan.TraceID()+".json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("payload file should be retained when acknowledgement is missing: %s", path)
	}

	output := out.String()
	if !strings.Contains(output, "requires --diagnostic-accept-sensitive-data") {
		t.Fatalf("expected non-interactive acknowledgement message, got:\n%s", output)
	}
}

// spec: telemetry/failed-diagnostic-export-is-never-queued
func TestDiagnosticPayloadNotQueued(t *testing.T) {
	dir := setupDiagnosticTest(t)

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	tel, state := newDiagnosticTelemetry(t, srv.URL)
	defer func() { _ = tel.Shutdown(context.Background()) }()

	_, rootSpan := tel.StartSpan(context.Background(), "agent_marketplace.command", map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-test",
	})
	rootSpan.End()

	tel.CommandCounter().Add(context.Background(), 1, map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-test",
	})
	tel.CommandDuration().Record(context.Background(), 100, map[string]string{
		string(tmcfg.ResourceAttrCommandName): "diagnostic-test",
	})

	var out bytes.Buffer
	if err := tel.FinishDiagnostic(context.Background(), &out, strings.NewReader(""), false, true); err == nil {
		t.Fatal("expected diagnostic export failure")
	}
	if requests.Load() == 0 {
		t.Fatal("diagnostic export was not attempted")
	}
	if _, err := os.Stat(filepath.Join(state.StateRoot(), "diagnostic", "payload-"+rootSpan.TraceID()+".json")); err != nil {
		t.Fatalf("failed export must retain review evidence: %v", err)
	}

	queueRoot := filepath.Join(dir, "agent-marketplace", "telemetry", "queue")
	entries, err := os.ReadDir(queueRoot)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 0 {
		t.Fatalf("diagnostic payload should not be queued, found %d entries", len(entries))
	}
}

func setupDiagnosticTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	orgPath := filepath.Join(dir, "nonexistent-org-config.yaml")
	amconfig.SetTestV3OrganizationConfigPath(t, orgPath)
	return dir
}

func newDiagnosticTelemetry(t *testing.T, endpoint string) (*Telemetry, TelemetryState) {
	t.Helper()

	user := amconfig.Settings{
		Harnesses: []string{"factory"},
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelDetailed,
			Endpoint: endpoint,
		},
	}

	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		t.Fatalf("resolve telemetry config: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tel, err := New(context.Background(), state, logger, WithDiagnostic(true))
	if err != nil {
		t.Fatalf("new diagnostic telemetry: %v", err)
	}
	return tel, state
}
