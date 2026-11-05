package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// executeCommandAtTerminal runs args with stdin reading input from a pipe that
// reports itself as a terminal, so interactive confirmations are exercised.
func executeCommandAtTerminal(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	prev := isTerminal
	isTerminal = func(uintptr) bool { return true }
	t.Cleanup(func() { isTerminal = prev })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	go func() {
		_, _ = w.WriteString(input)
		_ = w.Close()
	}()
	return executeCommandWithInput(context.Background(), r, args...)
}

func TestDiagnosticFlagExportsPayload(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := setupTelemetryHome(t)

	var mu sync.Mutex
	var metricCount, traceCount int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v1/metrics":
			metricCount++
		case "/v1/traces":
			traceCount++
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfgPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	writeTelemetryTestFile(t, cfgPath, "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\ntelemetry:\n  level: detailed\n  endpoint: "+srv.URL+"\n")

	output, err := executeCommandAtTerminal(t, "y\n", "--diagnostic", "--version")
	if err != nil {
		t.Fatalf("diagnostic version command failed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "dev") {
		t.Fatalf("expected version output, got:\n%s", output)
	}
	if !strings.Contains(output, "Diagnostic payload written to") {
		t.Fatalf("expected diagnostic payload message, got:\n%s", output)
	}
	if !strings.Contains(output, "Diagnostic export succeeded") {
		t.Fatalf("expected diagnostic export success, got:\n%s", output)
	}
	if !strings.Contains(output, "trace id:") {
		t.Fatalf("expected trace id in output, got:\n%s", output)
	}
	if !strings.Contains(output, "installation id:") {
		t.Fatalf("expected installation id in output, got:\n%s", output)
	}

	mu.Lock()
	mc, tc := metricCount, traceCount
	mu.Unlock()
	if mc == 0 {
		t.Fatal("expected metrics export request")
	}
	if tc == 0 {
		t.Fatal("expected traces export request")
	}

	stateRoot := filepath.Join(home, ".local", "state", "agent-marketplace", "telemetry")
	matches, _ := filepath.Glob(filepath.Join(stateRoot, "diagnostic", "payload-*.json"))
	if len(matches) > 0 {
		t.Fatalf("diagnostic payload file should have been deleted: %v", matches)
	}
}

func TestDiagnosticFlagRequiresAcknowledgement(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := setupTelemetryHome(t)

	var mu sync.Mutex
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		requests++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfgPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	writeTelemetryTestFile(t, cfgPath, "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\ntelemetry:\n  level: detailed\n  endpoint: "+srv.URL+"\n")

	output, err := executeCommand("--diagnostic", "--version")
	if err != nil {
		t.Fatalf("diagnostic version command failed: %v\n%s", err, output)
	}
	t.Logf("output:\n%s", output)

	if !strings.Contains(output, "requires interactive acknowledgement at a terminal") {
		t.Fatalf("expected non-interactive acknowledgement message, got:\n%s", output)
	}
	if strings.Contains(output, "Diagnostic export succeeded") {
		t.Fatal("expected diagnostic export to be refused")
	}

	stateRoot := filepath.Join(home, ".local", "state", "agent-marketplace", "telemetry", "diagnostic")
	matches, _ := filepath.Glob(filepath.Join(stateRoot, "payload-*.json"))
	if len(matches) == 0 {
		t.Fatalf("expected retained diagnostic payload file")
	}
}

// spec: telemetry/sensitive-data-acknowledgement-flag-is-not-accepted
func TestDiagnosticSensitiveDataFlagIsRejected(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := setupTelemetryHome(t)

	cfgPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	writeTelemetryTestFile(t, cfgPath, "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\ntelemetry:\n  level: detailed\n  endpoint: http://127.0.0.1:1\n")

	output, err := executeCommandAtTerminal(t, "y\n", "--diagnostic", "--diagnostic-accept-sensitive-data", "--version")
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --diagnostic-accept-sensitive-data") {
		t.Fatalf("expected unknown flag refusal, got err=%v\n%s", err, output)
	}
	if strings.Contains(output, "Diagnostic payload written to") {
		t.Fatalf("rejected flag must not capture a diagnostic payload, got:\n%s", output)
	}

	stateRoot := filepath.Join(home, ".local", "state", "agent-marketplace", "telemetry", "diagnostic")
	matches, _ := filepath.Glob(filepath.Join(stateRoot, "payload-*.json"))
	if len(matches) > 0 {
		t.Fatalf("rejected flag must not write a diagnostic payload: %v", matches)
	}
}

func TestDiagnosticFlagIgnoredWhenTelemetryBasic(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupTelemetryHome(t)

	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfgPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	writeTelemetryTestFile(t, cfgPath, "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\ntelemetry:\n  level: basic\n  endpoint: "+srv.URL+"\n")

	output, err := executeCommand("--diagnostic", "--version")
	if err != nil {
		t.Fatalf("diagnostic version command failed: %v\n%s", err, output)
	}

	if strings.Contains(output, "Diagnostic payload written to") {
		t.Fatalf("diagnostic payload should not be created for basic telemetry, got:\n%s", output)
	}
	if !strings.Contains(output, "dev") {
		t.Fatalf("expected version output, got:\n%s", output)
	}
	if requestCount == 0 {
		t.Fatal("expected basic telemetry metrics to be exported")
	}
}
