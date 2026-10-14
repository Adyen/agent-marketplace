package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
)

// telemetryReceiver counts OTLP requests by path. Requests to /v1/logs get
// logsStatus; every other signal is accepted.
type telemetryReceiver struct {
	mu     sync.Mutex
	counts map[string]int
}

func newTelemetryReceiver(t *testing.T, logsStatus int) (*httptest.Server, *telemetryReceiver) {
	t.Helper()
	recv := &telemetryReceiver{counts: make(map[string]int)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		recv.mu.Lock()
		recv.counts[r.URL.Path]++
		recv.mu.Unlock()
		if r.URL.Path == "/v1/logs" {
			w.WriteHeader(logsStatus)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, recv
}

func (r *telemetryReceiver) count(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[path]
}

func (r *telemetryReceiver) total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	sum := 0
	for _, n := range r.counts {
		sum += n
	}
	return sum
}

func writeTelemetryLevelConfig(t *testing.T, level, endpoint string) {
	t.Helper()
	cfgPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	writeTelemetryTestFile(t, cfgPath, "apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\ntelemetry:\n  level: "+level+"\n  endpoint: "+endpoint+"\n")
}

// retainedDiagnosticPayloads returns the payloads kept for review under the
// test's state root.
func retainedDiagnosticPayloads(t *testing.T, home string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(home, ".local", "state", "agent-marketplace", "telemetry", "diagnostic", "payload-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestDiagnosticCapturesDebugRecordsAtInfoLevel(t *testing.T) {
	// spec: diagnostic-capture/debug-records-are-captured-when-the-log-level-is-info
	if isolateCommandTest(t) {
		return
	}
	home := setupTelemetryHome(t)
	logFile := filepath.Join(home, "agent-marketplace.log")
	t.Setenv("AGENT_MARKETPLACE_LOG_ENABLED", "true")
	t.Setenv("AGENT_MARKETPLACE_LOG_LEVEL", "info")
	t.Setenv("AGENT_MARKETPLACE_LOG_FILE", logFile)

	srv, _ := newTelemetryReceiver(t, http.StatusServiceUnavailable)
	writeTelemetryLevelConfig(t, "detailed", srv.URL)

	output, err := executeCommand("--diagnostic", "--diagnostic-accept-sensitive-data", "--version")
	if err != nil {
		t.Fatalf("diagnostic version command failed: %v\n%s", err, output)
	}

	payloads := retainedDiagnosticPayloads(t, home)
	if len(payloads) != 1 {
		t.Fatalf("expected one retained payload, got %v", payloads)
	}
	data, err := os.ReadFile(payloads[0])
	if err != nil {
		t.Fatal(err)
	}
	var payload telemetry.DiagnosticPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	var sawDebug, sawInfo bool
	for _, entry := range payload.Logs {
		switch {
		case entry.Severity == "DEBUG" && entry.Message == "loaded installation id":
			sawDebug = true
		case entry.Severity == "INFO" && entry.Message == "command start":
			sawInfo = true
		}
	}
	if !sawInfo {
		t.Fatal("control: expected the info record in the payload")
	}
	if !sawDebug {
		t.Fatalf("expected the debug record in the payload, got %d records", len(payload.Logs))
	}

	logContent, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if strings.Contains(string(logContent), `"level":"DEBUG"`) {
		t.Fatalf("log file should not contain debug records at info level:\n%s", logContent)
	}
}

func TestDiagnosticRunsWithoutCaptureSendNoLogs(t *testing.T) {
	// spec: diagnostic-capture/runs-without-diagnostic-capture-send-no-logs
	if isolateCommandTest(t) {
		return
	}
	cases := []struct {
		name  string
		level string
		args  []string
	}{
		{name: "detailed without diagnostic", level: "detailed", args: []string{"--version"}},
		{name: "diagnostic at basic", level: "basic", args: []string{"--diagnostic", "--diagnostic-accept-sensitive-data", "--version"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupTelemetryHome(t)
			srv, recv := newTelemetryReceiver(t, http.StatusOK)
			writeTelemetryLevelConfig(t, tc.level, srv.URL)

			output, err := executeCommand(tc.args...)
			if err != nil {
				t.Fatalf("command failed: %v\n%s", err, output)
			}
			if recv.total() == 0 {
				t.Fatal("control: expected ordinary telemetry traffic")
			}
			if n := recv.count("/v1/logs"); n != 0 {
				t.Fatalf("expected no /v1/logs requests, got %d", n)
			}
		})
	}
}

func TestDiagnosticRejectedLogsRequestLeavesCommandSuccessful(t *testing.T) {
	// spec: diagnostic-capture/a-rejected-logs-request-fails-the-export
	if isolateCommandTest(t) {
		return
	}
	home := setupTelemetryHome(t)

	srv, recv := newTelemetryReceiver(t, http.StatusNotFound)
	writeTelemetryLevelConfig(t, "detailed", srv.URL)

	output, err := executeCommand("--diagnostic", "--diagnostic-accept-sensitive-data", "--version")
	if err != nil {
		t.Fatalf("a rejected logs request should not change the exit status: %v\n%s", err, output)
	}
	if recv.count("/v1/logs") == 0 {
		t.Fatal("expected a /v1/logs request")
	}
	if !strings.Contains(output, "Diagnostic export failed") {
		t.Fatalf("expected diagnostic export failure, got:\n%s", output)
	}
	if len(retainedDiagnosticPayloads(t, home)) != 1 {
		t.Fatal("expected the payload to be retained after the failed export")
	}
}
