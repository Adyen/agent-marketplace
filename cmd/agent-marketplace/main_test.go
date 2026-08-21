package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/testutil"
)

var testMainHome string

func TestMain(m *testing.M) {
	// Use a visible stderr logger for TestMain diagnostics. Command tests
	// re-configure the default logger, so it is reset before cleanup.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// The runtime allowlist is now derived from effective configuration
	// layers. Tests that exercise allowlist behavior declare allowed_registries
	// in their fixture configs; all other tests use temp-directory git repos
	// and example.com URLs with no allowlist configured (permitting all).

	home, cleanup, err := testutil.IsolatedHome()
	if err != nil {
		slog.Error("failed to create isolated test home", "err", err)
		os.Exit(1)
	}
	testMainHome = home

	// Neutralize managed org config so the developer's real
	// /etc/agent-marketplace/config.yaml does not leak into test execution.
	// Use a private temp directory so the path cannot collide with another run.
	// Individual tests that need an org config set the test override explicitly
	// with config.SetTestOrganizationConfigPath.
	orgDir, err := os.MkdirTemp("", "agent-marketplace-test-org-config-*")
	if err != nil {
		slog.Error("failed to create isolated org config directory", "err", err)
		if cleanupErr := cleanup(); cleanupErr != nil {
			slog.Error("failed to remove isolated test home", "err", cleanupErr)
		}
		os.Exit(1)
	}
	orgConfigPath := filepath.Join(orgDir, "nonexistent-v3-org-config-for-tests.yaml")

	fail := func(msg string, err error) {
		slog.Error(msg, "err", err)
		_ = os.RemoveAll(orgDir)
		if cleanupErr := cleanup(); cleanupErr != nil {
			slog.Error("failed to remove isolated test home", "err", cleanupErr)
		}
		os.Exit(1)
	}

	config.SetTestOrganizationConfigPath(nil, orgConfigPath)

	if err := os.Setenv("AGENT_MARKETPLACE_LOG_ENABLED", "false"); err != nil {
		fail("failed to disable logging in cmd tests", err)
	}
	if err := os.Setenv("CODEX_HOME", ""); err != nil {
		fail("failed to isolate CODEX_HOME in cmd tests", err)
	}

	code := m.Run()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	_ = os.RemoveAll(orgDir)
	if cleanupErr := cleanup(); cleanupErr != nil {
		slog.Error("failed to remove isolated test home", "err", cleanupErr)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func TestMainIsolatesHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if home != testMainHome {
		t.Fatalf("UserHomeDir() = %q, want isolated test home %q", home, testMainHome)
	}
}

func TestStartupTargetRegistrationsAreValid(t *testing.T) {
	if err := validateStartupTargetRegistrations(); err != nil {
		t.Fatalf("startup target registration validation failed: %v", err)
	}
}

func TestVersionFlagPrintsVersion(t *testing.T) {
	output, err := executeCommand("--version")
	if err != nil {
		t.Fatalf("expected version flag to succeed: %v", err)
	}

	if output != defaultVersion+"\n" {
		t.Fatalf("expected version output %q, got %q", defaultVersion+"\n", output)
	}
}

func TestSyncFlagsAreDocumentedAndValidated(t *testing.T) {
	output, err := executeCommand("sync", "--help")
	if err != nil {
		t.Fatalf("expected sync help to succeed: %v", err)
	}
	for _, flag := range []string{"--offline", "--allow-revoked"} {
		if !strings.Contains(output, flag) {
			t.Fatalf("sync help missing %q: %q", flag, output)
		}
	}
	output, err = executeCommand("sync", "--offline", "--allow-revoked", "not-a-coordinate")
	if err == nil {
		t.Fatalf("invalid --allow-revoked unexpectedly succeeded with output %q", output)
	}
	if !strings.Contains(err.Error(), "invalid --allow-revoked value") {
		t.Fatalf("invalid --allow-revoked error = %v, output %q", err, output)
	}
}

func TestSyncOfflineRejectsPruneStale(t *testing.T) {
	output, err := executeCommand("sync", "--offline", "--prune-stale")
	if err == nil {
		t.Fatalf("sync --offline --prune-stale unexpectedly succeeded with output %q", output)
	}
	if !strings.Contains(err.Error(), "--prune-stale cannot be used with --offline") {
		t.Fatalf("unexpected offline prune error = %v, output %q", err, output)
	}
	if !strings.Contains(err.Error(), "cached snapshots must never drive destructive stale-selection pruning") {
		t.Fatalf("offline prune error lacks safety explanation: %v", err)
	}
}

func TestSyncCheckRejectsRevocationOverride(t *testing.T) {
	validOverride := "marketplace/source/skill/review@sha256:" + strings.Repeat("0", 64)
	output, err := executeCommand("sync", "--offline", "--check", "--allow-revoked", validOverride)
	if err == nil {
		t.Fatalf("sync --check unexpectedly accepted --allow-revoked, output %q", output)
	}
	if !strings.Contains(err.Error(), "--allow-revoked cannot be used with --check") {
		t.Fatalf("unexpected combination error = %v, output %q", err, output)
	}
	if !strings.Contains(err.Error(), "execution-only") || !strings.Contains(err.Error(), "checks must remain unhealthy") {
		t.Fatalf("combination error lacks the execution-only/check-health explanation: %v", err)
	}
	if strings.Contains(err.Error(), "invalid --allow-revoked value") {
		t.Fatalf("valid override was reported as invalid: %v", err)
	}
}

func TestHelpListsPlaceholderSubcommands(t *testing.T) {
	output, err := executeCommand("--help")
	if err != nil {
		t.Fatalf("expected help flag to succeed: %v", err)
	}

	for _, subcommand := range []string{"sync", "list", "install", "remove", "update", "status", "telemetry"} {
		if !strings.Contains(output, subcommand) {
			t.Fatalf("expected help output to contain %q, got %q", subcommand, output)
		}
	}
}

func executeCommand(args ...string) (string, error) {
	return executeCommandContext(context.Background(), args...)
}

func executeCommandContext(ctx context.Context, args ...string) (string, error) {
	cmd := newRootCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetIn(&bytes.Buffer{})
	cmd.SetArgs(args)
	cmd.SetContext(ctx)

	execCmd, err := cmd.ExecuteC()
	finishTelemetry(execCmd, err)
	finishRootLogging(err)
	return output.String(), err
}
