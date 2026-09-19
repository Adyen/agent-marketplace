package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetUserTelemetryLevel(t *testing.T) {
	path := setupUserConfig(t)

	if err := SetUserTelemetryLevel(path, "basic"); err != nil {
		t.Fatalf("SetUserTelemetryLevel: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "telemetry:") || !strings.Contains(content, "level: basic") {
		t.Fatalf("expected telemetry.level basic, got:\n%s", content)
	}

	if err := SetUserTelemetryLevel(path, "detailed"); err != nil {
		t.Fatalf("SetUserTelemetryLevel detailed: %v", err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "level: detailed") {
		t.Fatalf("expected level detailed, got:\n%s", string(data))
	}
}

func TestSetUserTelemetryLevelUpdatesV4UserConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"))

	if err := SetUserTelemetryLevel(path, "basic"); err != nil {
		t.Fatalf("SetUserTelemetryLevel on v4: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "apiVersion: agent-marketplace/v4") || !strings.Contains(string(data), "level: basic") {
		t.Fatalf("expected v4 telemetry level, got:\n%s", data)
	}
}

func TestSetUserTelemetryLevelRejectsInvalid(t *testing.T) {
	path := setupUserConfig(t)

	if err := SetUserTelemetryLevel(path, "bogus"); err == nil {
		t.Fatal("expected error for invalid level")
	}
	if err := SetUserTelemetryLevel(path, "diagnostic"); err == nil {
		t.Fatal("expected error for diagnostic as persisted level")
	}
}

func TestSetUserTelemetryEndpoint(t *testing.T) {
	path := setupUserConfig(t)

	if err := SetUserTelemetryEndpoint(path, "https://example.com/v1/metrics"); err != nil {
		t.Fatalf("SetUserTelemetryEndpoint: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "endpoint: https://example.com/v1/metrics") {
		t.Fatalf("expected endpoint, got:\n%s", string(data))
	}

	if err := SetUserTelemetryEndpoint(path, "http://public.example.com"); err == nil {
		t.Fatal("expected error for insecure public http endpoint")
	}
	if err := SetUserTelemetryEndpoint(path, "http://localhost:4318"); err != nil {
		t.Fatalf("loopback http endpoint should be allowed: %v", err)
	}

	if err := SetUserTelemetryEndpoint(path, ""); err != nil {
		t.Fatalf("removing endpoint: %v", err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "endpoint:") {
		t.Fatalf("expected endpoint removed, got:\n%s", string(data))
	}
}

func TestDisableTelemetry(t *testing.T) {
	path := setupUserConfig(t)

	if err := SetUserTelemetryLevel(path, "detailed"); err != nil {
		t.Fatal(err)
	}
	if err := DisableTelemetry(path); err != nil {
		t.Fatalf("DisableTelemetry: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "level: off") {
		t.Fatalf("expected level off, got:\n%s", string(data))
	}
}

func TestDisableTelemetryMissingConfigIsNoOp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := DisableTelemetry(path); err != nil {
		t.Fatalf("DisableTelemetry on missing config should be no-op: %v", err)
	}
}

func TestSetUserTelemetryRejectsNonGlobal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	nonGlobal := filepath.Join(dir, "repo", ".agents", "config.yaml")

	if err := os.MkdirAll(filepath.Dir(nonGlobal), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, nonGlobal, []byte("apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\n"))

	if err := SetUserTelemetryLevel(nonGlobal, "basic"); err == nil {
		t.Fatal("expected error for non-global config")
	}
}

func TestSetUserTelemetryRequiresExistingConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	global, err := GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := SetUserTelemetryLevel(global, "basic"); err == nil {
		t.Fatal("expected error when user config does not exist")
	}
}

func setupUserConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, []byte("apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\n"))
	return path
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
