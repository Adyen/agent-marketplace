package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestInitCreatesV2UserPersonalConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	output, err := executeCommand("init", "--harnesses", "factory,claude")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Initialized user personal config") {
		t.Fatalf("unexpected output: %s", output)
	}

	cfgPath, err := config.UserPersonalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "apiVersion: agent-marketplace/v2") {
		t.Errorf("expected v2 apiVersion, got:\n%s", content)
	}
	if !strings.Contains(content, "harnesses:") {
		t.Errorf("expected harnesses list, got:\n%s", content)
	}
	if strings.Contains(content, "registries:") || strings.Contains(content, "logging:") {
		t.Errorf("config contains implicit registry or logging:\n%s", content)
	}
}

func TestInitRequiresHarnessesInNonInteractiveMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := executeCommand("init")
	if err == nil {
		t.Fatal("expected non-interactive init without --harnesses to fail")
	}
	if !strings.Contains(err.Error(), "--harnesses") {
		t.Fatalf("expected --harnesses guidance, got: %v", err)
	}
}

func TestInitIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := executeCommand("init", "--harnesses", "factory")
	if err != nil {
		t.Fatalf("first init failed: %v", err)
	}

	cfgPath, err := config.UserPersonalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("init", "--harnesses", "claude")
	if err != nil {
		t.Fatalf("second init failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "already initialized") {
		t.Fatalf("expected idempotent message, got: %s", output)
	}

	second, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("config was modified on second init:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestInitValidatesHarnesses(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := executeCommand("init", "--harnesses", "skynet")
	if err == nil {
		t.Fatal("expected init with unknown harness to fail")
	}
	if !strings.Contains(err.Error(), "skynet") {
		t.Fatalf("expected unknown harness error, got: %v", err)
	}
}

func TestInitDoesNotCreateConfigWhenValidationFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := executeCommand("init", "--harnesses", "skynet")
	if err == nil {
		t.Fatal("expected init with unknown harness to fail")
	}

	cfgPath, err := config.UserPersonalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(cfgPath); !os.IsNotExist(statErr) {
		t.Fatalf("config should not have been created after validation failure")
	}
}

func TestInitHelp(t *testing.T) {
	output, err := executeCommand("init", "--help")
	if err != nil {
		t.Fatalf("init --help failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "--harnesses") {
		t.Fatalf("help missing --harnesses flag:\n%s", output)
	}
}

func TestInitInteractiveUsesDetectedHarnesses(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")

	if err := os.MkdirAll(filepath.Join(home, ".factory"), 0o755); err != nil {
		t.Fatalf("mkdir factory root: %v", err)
	}

	prev := isTerminal
	isTerminal = func(fd uintptr) bool { return true }
	defer func() { isTerminal = prev }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	go func() {
		_, _ = w.WriteString("y\ny\n")
		_ = w.Close()
	}()

	var out bytes.Buffer
	if err := runInit(&out, r, nil); err != nil {
		t.Fatalf("runInit failed: %v\noutput: %s", err, out.String())
	}

	if !strings.Contains(out.String(), "Detected harnesses: factory") {
		t.Fatalf("expected detection prompt, got: %s", out.String())
	}

	data, err := os.ReadFile(filepath.Join(home, ".agents", "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "apiVersion: agent-marketplace/v2") || !strings.Contains(content, "factory") {
		t.Fatalf("expected v2 config with factory, got:\n%s", content)
	}
	if !strings.Contains(content, "telemetry:") || !strings.Contains(content, "level: basic") {
		t.Fatalf("expected telemetry consent to be written, got:\n%s", content)
	}
}

func TestInitDeduplicatesHarnesses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	output, err := executeCommand("init", "--harnesses", "factory,factory,claude")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, output)
	}

	cfgPath, err := config.UserPersonalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	countFactory := strings.Count(content, "- factory")
	countClaude := strings.Count(content, "- claude")
	if countFactory != 1 || countClaude != 1 {
		t.Fatalf("expected one factory and one claude entry, got factory=%d claude=%d:\n%s", countFactory, countClaude, content)
	}
}

func TestInitInteractiveTelemetryConsentNo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".factory"), 0o755); err != nil {
		t.Fatalf("mkdir factory root: %v", err)
	}

	prev := isTerminal
	isTerminal = func(fd uintptr) bool { return true }
	defer func() { isTerminal = prev }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	go func() {
		_, _ = w.WriteString("y\nn\n")
		_ = w.Close()
	}()

	var out bytes.Buffer
	if err := runInit(&out, r, nil); err != nil {
		t.Fatalf("runInit failed: %v\noutput: %s", err, out.String())
	}

	data, err := os.ReadFile(filepath.Join(home, ".agents", "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "telemetry:") {
		t.Fatalf("expected no telemetry block when declined, got:\n%s", content)
	}
}

func TestPromptTelemetryConsentEOFDeclines(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath, err := config.InitializeUserPersonal([]string{"factory"})
	if err != nil {
		t.Fatalf("initialize config: %v", err)
	}

	var out bytes.Buffer
	if err := promptTelemetryConsent(&out, bufio.NewReader(strings.NewReader("")), configPath); err != nil {
		t.Fatalf("EOF should decline consent without failing: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), "telemetry:") {
		t.Fatalf("expected EOF to leave telemetry disabled, got:\n%s", data)
	}
}

func TestPromptTelemetryConsentEOFWithReplyEnables(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath, err := config.InitializeUserPersonal([]string{"factory"})
	if err != nil {
		t.Fatalf("initialize config: %v", err)
	}

	var out bytes.Buffer
	if err := promptTelemetryConsent(&out, bufio.NewReader(strings.NewReader("y")), configPath); err != nil {
		t.Fatalf("non-empty EOF reply should be processed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "telemetry:\n  level: basic") {
		t.Fatalf("expected non-empty EOF reply to enable telemetry, got:\n%s", data)
	}
}

func TestInitInteractiveTelemetryManagedSkipsPrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	orgDir := t.TempDir()
	orgPath := filepath.Join(orgDir, "org.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v3\nharnesses:\n  - factory\ntelemetry:\n  level: basic\n  endpoint: https://org.example.com\n  retention_policy: https://org.example.com/retention\n")
	config.SetTestV3OrganizationConfigPath(t, orgPath)

	if err := os.MkdirAll(filepath.Join(home, ".factory"), 0o755); err != nil {
		t.Fatalf("mkdir factory root: %v", err)
	}

	prev := isTerminal
	isTerminal = func(fd uintptr) bool { return true }
	defer func() { isTerminal = prev }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	go func() {
		_, _ = w.WriteString("y\n")
		_ = w.Close()
	}()

	var out bytes.Buffer
	if err := runInit(&out, r, nil); err != nil {
		t.Fatalf("runInit failed: %v\noutput: %s", err, out.String())
	}

	if !strings.Contains(out.String(), "managed by your organization") {
		t.Fatalf("expected managed message, got: %s", out.String())
	}

	data, err := os.ReadFile(filepath.Join(home, ".agents", "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), "telemetry:") {
		t.Fatalf("expected no user telemetry block when managed, got:\n%s", string(data))
	}
}

func TestInitNonInteractiveLeavesTelemetryUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	output, err := executeCommand("init", "--harnesses", "factory")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, output)
	}

	cfgPath, _ := config.UserPersonalConfigPath()
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "telemetry:") {
		t.Fatalf("expected no telemetry block for non-interactive init, got:\n%s", string(data))
	}
}
