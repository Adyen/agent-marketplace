package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
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
