package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	metricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestTelemetryEnableAndDisable(t *testing.T) {
	setupTelemetryHome(t)

	if _, err := executeCommand("init", "--harnesses", "factory"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if _, err := executeCommand("telemetry", "enable", "--level", "basic"); err != nil {
		t.Fatalf("enable failed: %v", err)
	}
	cfgPath, _ := config.GlobalConfigPath()
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "telemetry:") || !strings.Contains(string(data), "level: basic") {
		t.Fatalf("expected telemetry enabled, got:\n%s", string(data))
	}

	if _, err := executeCommand("telemetry", "disable"); err != nil {
		t.Fatalf("disable failed: %v", err)
	}
	data, _ = os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "level: off") {
		t.Fatalf("expected telemetry disabled, got:\n%s", string(data))
	}
}

func TestTelemetryEnableEndpoint(t *testing.T) {
	setupTelemetryHome(t)

	if _, err := executeCommand("init", "--harnesses", "factory"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if _, err := executeCommand("telemetry", "enable", "--level", "detailed", "--endpoint", "https://otel.example.com"); err != nil {
		t.Fatalf("enable with endpoint failed: %v", err)
	}
	cfgPath, _ := config.GlobalConfigPath()
	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "endpoint: https://otel.example.com") {
		t.Fatalf("expected endpoint, got:\n%s", string(data))
	}
}

func TestTelemetryEnableRejectsInsecureEndpoint(t *testing.T) {
	setupTelemetryHome(t)

	if _, err := executeCommand("init", "--harnesses", "factory"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	_, err := executeCommand("telemetry", "enable", "--level", "basic", "--endpoint", "http://public.example.com")
	if err == nil {
		t.Fatal("expected error for insecure public http endpoint")
	}
	if !strings.Contains(err.Error(), "insecure") {
		t.Fatalf("expected insecure http error, got: %v", err)
	}
}

func TestTelemetryStatus(t *testing.T) {
	setupTelemetryHome(t)

	if _, err := executeCommand("init", "--harnesses", "factory"); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if _, err := executeCommand("telemetry", "enable", "--level", "basic"); err != nil {
		t.Fatalf("enable failed: %v", err)
	}

	output, err := executeCommand("telemetry", "status")
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "Effective level") {
		t.Fatalf("status missing effective level:\n%s", output)
	}
	if !strings.Contains(output, "basic") {
		t.Fatalf("status missing basic level:\n%s", output)
	}
}

func TestTelemetryResetID(t *testing.T) {
	setupTelemetryHome(t)

	if _, err := executeCommand("init", "--harnesses", "factory"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	output, err := executeCommand("telemetry", "reset-id")
	if err != nil {
		t.Fatalf("reset-id failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Installation ID reset") {
		t.Fatalf("expected reset message, got:\n%s", output)
	}
}

func TestTelemetryPurge(t *testing.T) {
	home := setupTelemetryHome(t)

	if _, err := executeCommand("init", "--harnesses", "factory"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	stateRoot := filepath.Join(home, ".local", "state", "agent-marketplace", "telemetry")
	queueFile := filepath.Join(stateRoot, "queue", "metrics", "queued.json")
	if err := os.MkdirAll(filepath.Dir(queueFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(queueFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	diagFile := filepath.Join(stateRoot, "diagnostic", "diag.json")
	if err := os.MkdirAll(filepath.Dir(diagFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(diagFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("telemetry", "purge")
	if err != nil {
		t.Fatalf("purge failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Purged 1 queued record(s) and 1 diagnostic payload(s)") {
		t.Fatalf("expected purge counts, got:\n%s", output)
	}
	if _, err := os.Stat(queueFile); !os.IsNotExist(err) {
		t.Fatal("queue file should have been removed")
	}
	if _, err := os.Stat(diagFile); !os.IsNotExist(err) {
		t.Fatal("diagnostic file should have been removed")
	}
}

func TestTelemetryPreview(t *testing.T) {
	setupTelemetryHome(t)

	if _, err := executeCommand("init", "--harnesses", "factory"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	output, err := executeCommand("telemetry", "preview", "--level", "detailed")
	if err != nil {
		t.Fatalf("preview failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Preview payload written to") {
		t.Fatalf("expected preview path, got:\n%s", output)
	}

	path := extractPath(output)
	if path == "" {
		t.Fatalf("could not find preview path in output:\n%s", output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read preview: %v", err)
	}
	if !strings.Contains(string(data), "agent_marketplace.command") {
		t.Fatalf("expected detailed spans in preview file, got:\n%s", string(data))
	}
}

func TestTelemetryPreviewDiagnosticRequiresAccept(t *testing.T) {
	setupTelemetryHome(t)

	if _, err := executeCommand("init", "--harnesses", "factory"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	_, err := executeCommand("telemetry", "preview", "--level", "diagnostic")
	if err == nil {
		t.Fatal("expected diagnostic preview to require acknowledgement")
	}

	output, err := executeCommand("telemetry", "preview", "--level", "diagnostic", "--diagnostic-accept-sensitive-data")
	if err != nil {
		t.Fatalf("preview with accept failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Preview payload written to") {
		t.Fatalf("expected preview path, got:\n%s", output)
	}
}

func TestTelemetryEnableHonorsOrgLock(t *testing.T) {
	setupTelemetryHome(t)

	orgDir := t.TempDir()
	orgPath := filepath.Join(orgDir, "org.yaml")
	writeTelemetryTestFile(t, orgPath, "apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\ntelemetry:\n  level: detailed\n  endpoint: https://org.example.com\n  retention_policy: https://org.example.com/retention\n")
	config.SetTestV3OrganizationConfigPath(t, orgPath)

	if _, err := executeCommand("init", "--harnesses", "factory"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	_, err := executeCommand("telemetry", "enable", "--level", "basic")
	if err == nil {
		t.Fatal("expected error when enabling level below organization minimum")
	}
	if !strings.Contains(err.Error(), "organization policy") {
		t.Fatalf("expected organization policy error, got: %v", err)
	}

	if _, err := executeCommand("telemetry", "enable", "--level", "detailed"); err != nil {
		t.Fatalf("enable detailed failed: %v", err)
	}

	_, err = executeCommand("telemetry", "enable", "--level", "detailed", "--endpoint", "https://user.example.com")
	if err == nil {
		t.Fatal("expected error when overriding organization endpoint")
	}
}

func TestTelemetryStatusManaged(t *testing.T) {
	setupTelemetryHome(t)

	orgDir := t.TempDir()
	orgPath := filepath.Join(orgDir, "org.yaml")
	writeTelemetryTestFile(t, orgPath, "apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\ntelemetry:\n  level: basic\n  endpoint: https://org.example.com\n  retention_policy: https://org.example.com/retention\n  community_reporting: true\n")
	config.SetTestV3OrganizationConfigPath(t, orgPath)

	if _, err := executeCommand("init", "--harnesses", "factory"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	output, err := executeCommand("telemetry", "status")
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Managed installation:  true") {
		t.Fatalf("expected managed true, got:\n%s", output)
	}
	if !strings.Contains(output, "org") {
		t.Fatalf("expected organization source, got:\n%s", output)
	}
}

func setupTelemetryHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))

	// Use a non-existent organization config so the developer's real
	// /etc/agent-marketplace/config.yaml does not leak into tests.
	orgPath := filepath.Join(home, "nonexistent-org-config.yaml")
	config.SetTestV3OrganizationConfigPath(t, orgPath)
	return home
}

func extractPath(output string) string {
	prefix := "Preview payload written to "
	idx := strings.Index(output, prefix)
	if idx < 0 {
		return ""
	}
	rest := output[idx+len(prefix):]
	idx = strings.IndexAny(rest, "\n\r")
	if idx < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:idx])
}

func writeTelemetryTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBasicTelemetryEmitsOneCommandMetric(t *testing.T) {
	_ = setupTelemetryHome(t)

	var mu sync.Mutex
	var reqCount int
	var lastReq *metricspb.ExportMetricsServiceRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/v1/metrics" {
			var req metricspb.ExportMetricsServiceRequest
			if err := proto.Unmarshal(body, &req); err == nil {
				mu.Lock()
				reqCount++
				lastReq = &req
				mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfgPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	writeTelemetryTestFile(t, cfgPath, "apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\ntelemetry:\n  level: basic\n  endpoint: "+srv.URL+"\n")

	output, err := executeCommand("--version")
	if err != nil {
		t.Fatalf("version command failed: %v\n%s", err, output)
	}
	if output != defaultVersion+"\n" {
		t.Fatalf("unexpected version output: %q", output)
	}

	mu.Lock()
	count := reqCount
	req := lastReq
	mu.Unlock()

	if count == 0 {
		t.Fatal("expected a metrics export request")
	}
	if count > 1 {
		t.Fatalf("expected exactly one metrics export request, got %d", count)
	}

	attrs := extractFirstMetricAttrs(t, req)
	want := map[string]string{
		string(tmcfg.ResourceAttrCommandName): "agent-marketplace",
		string(tmcfg.ResourceAttrOutcomeCode): "success",
		string(tmcfg.ResourceAttrManaged):     "false",
		string(tmcfg.ResourceAttrHarnesses):   "factory",
	}
	for k, v := range want {
		if attrs[k] != v {
			t.Fatalf("metric attribute %q = %q, want %q", k, attrs[k], v)
		}
	}
	if _, ok := attrs[string(tmcfg.ResourceAttrInstallationID)]; ok {
		t.Fatalf("metric attributes must not contain %q", tmcfg.ResourceAttrInstallationID)
	}
	if _, ok := attrs[string(tmcfg.ResourceAttrSchemaVersion)]; ok {
		t.Fatalf("metric attributes must not contain %q", tmcfg.ResourceAttrSchemaVersion)
	}
}

func TestTelemetryErrorOutcome(t *testing.T) {
	_ = setupTelemetryHome(t)

	var mu sync.Mutex
	var lastReq *metricspb.ExportMetricsServiceRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/v1/metrics" {
			var req metricspb.ExportMetricsServiceRequest
			if err := proto.Unmarshal(body, &req); err == nil {
				mu.Lock()
				lastReq = &req
				mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfgPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	writeTelemetryTestFile(t, cfgPath, "apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\ntelemetry:\n  level: basic\n  endpoint: "+srv.URL+"\n")

	// doctor --yes without --repair returns an error after PersistentPreRunE.
	_, err = executeCommand("doctor", "--yes")
	if err == nil {
		t.Fatal("expected doctor --yes to fail")
	}

	mu.Lock()
	req := lastReq
	mu.Unlock()

	if req == nil {
		t.Fatal("expected a metrics export request")
	}
	attrs := extractFirstMetricAttrs(t, req)
	if attrs[string(tmcfg.ResourceAttrOutcomeCode)] != "error" {
		t.Fatalf("expected outcome.code=error, got %q", attrs[string(tmcfg.ResourceAttrOutcomeCode)])
	}
	if attrs[string(tmcfg.ResourceAttrCommandName)] != "agent-marketplace doctor" {
		t.Fatalf("unexpected command.name: %q", attrs[string(tmcfg.ResourceAttrCommandName)])
	}
}

func TestTelemetryCommandsDoNotSelfReport(t *testing.T) {
	tests := []struct {
		name string
		args []string
		prep func(t *testing.T, home, srvURL, cfgPath string)
	}{
		{
			name: "init",
			args: []string{"init", "--harnesses", "factory"},
			prep: func(t *testing.T, home, srvURL, cfgPath string) {},
		},
		{
			name: "telemetry status",
			args: []string{"telemetry", "status"},
			prep: func(t *testing.T, home, srvURL, cfgPath string) {
				writeTelemetryTestFile(t, cfgPath, "apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\ntelemetry:\n  level: basic\n  endpoint: "+srvURL+"\n")
			},
		},
		{
			name: "telemetry enable",
			args: []string{"telemetry", "enable", "--level", "basic"},
			prep: func(t *testing.T, home, srvURL, cfgPath string) {
				writeTelemetryTestFile(t, cfgPath, "apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\n")
			},
		},
		{
			name: "telemetry disable",
			args: []string{"telemetry", "disable"},
			prep: func(t *testing.T, home, srvURL, cfgPath string) {
				writeTelemetryTestFile(t, cfgPath, "apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\ntelemetry:\n  level: basic\n  endpoint: "+srvURL+"\n")
			},
		},
		{
			name: "telemetry preview",
			args: []string{"telemetry", "preview", "--level", "basic"},
			prep: func(t *testing.T, home, srvURL, cfgPath string) {
				writeTelemetryTestFile(t, cfgPath, "apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\n")
			},
		},
		{
			name: "telemetry reset-id",
			args: []string{"telemetry", "reset-id"},
			prep: func(t *testing.T, home, srvURL, cfgPath string) {
				writeTelemetryTestFile(t, cfgPath, "apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\n")
			},
		},
		{
			name: "telemetry purge",
			args: []string{"telemetry", "purge"},
			prep: func(t *testing.T, home, srvURL, cfgPath string) {
				writeTelemetryTestFile(t, cfgPath, "apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\n")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := setupTelemetryHome(t)

			var mu sync.Mutex
			var reqCount int

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/metrics" {
					mu.Lock()
					reqCount++
					mu.Unlock()
				}
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			cfgPath, err := config.GlobalConfigPath()
			if err != nil {
				t.Fatal(err)
			}
			tc.prep(t, home, srv.URL, cfgPath)

			_, err = executeCommand(tc.args...)
			if err != nil {
				// Some commands may error; that is fine for this test as long as
				// they did not emit telemetry.
				t.Logf("command returned error: %v", err)
			}

			mu.Lock()
			count := reqCount
			mu.Unlock()

			if count != 0 {
				t.Fatalf("expected no telemetry requests for %q, got %d", tc.name, count)
			}
		})
	}
}

func extractFirstMetricAttrs(t *testing.T, req *metricspb.ExportMetricsServiceRequest) map[string]string {
	t.Helper()
	if req == nil {
		t.Fatal("nil metrics request")
	}
	if len(req.ResourceMetrics) == 0 || len(req.ResourceMetrics[0].ScopeMetrics) == 0 {
		t.Fatalf("expected resource metrics, got %+v", req)
	}
	for _, sm := range req.ResourceMetrics[0].ScopeMetrics {
		for _, m := range sm.Metrics {
			dps := m.GetSum().GetDataPoints()
			if len(dps) > 0 {
				return keyValueMap(dps[0].GetAttributes())
			}
			hdps := m.GetHistogram().GetDataPoints()
			if len(hdps) > 0 {
				return keyValueMap(hdps[0].GetAttributes())
			}
		}
	}
	t.Fatal("no metric data points found")
	return nil
}

func keyValueMap(kvs []*commonpb.KeyValue) map[string]string {
	out := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		if kv.Value != nil {
			out[kv.Key] = kv.Value.GetStringValue()
		}
	}
	return out
}

func TestTelemetryReceiverCommandRegistered(t *testing.T) {
	output, err := executeCommand("telemetry", "receiver", "--help")
	if err != nil {
		t.Fatalf("receiver --help failed: %v\n%s", err, output)
	}
	for _, flag := range []string{"--listen", "--storage-dir", "--max-installations", "--max-series", "--max-request-ids"} {
		if !strings.Contains(output, flag) {
			t.Fatalf("receiver help missing %q flag:\n%s", flag, output)
		}
	}
	if !strings.Contains(output, "localhost:4318") {
		t.Fatalf("receiver help missing default listen address:\n%s", output)
	}
}
