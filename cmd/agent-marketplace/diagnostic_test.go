package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

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

	output, err := executeCommand("--diagnostic", "--diagnostic-accept-sensitive-data", "--version")
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
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

	if !strings.Contains(output, "requires --diagnostic-accept-sensitive-data") {
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

	output, err := executeCommand("--diagnostic", "--diagnostic-accept-sensitive-data", "--version")
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
