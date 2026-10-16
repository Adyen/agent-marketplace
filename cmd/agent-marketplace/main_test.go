package main

import (
	"bytes"
	"context"
	"flag"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/testutil"
)

var testMainHome string

// lookPathOutsideGitExecPath resolves name on PATH, skipping GIT_EXEC_PATH. Git
// hooks prepend that directory to PATH, and its git cannot find its helpers
// through the sandbox symlink.
func lookPathOutsideGitExecPath(name string) (string, error) {
	execPath := os.Getenv("GIT_EXEC_PATH")
	if execPath == "" {
		return exec.LookPath(name)
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || filepath.Clean(dir) == filepath.Clean(execPath) {
			continue
		}
		if path, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return path, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func TestMain(m *testing.M) {
	flag.Parse()
	parallelSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "test.parallel" {
			parallelSet = true
		}
	})
	if !parallelSet {
		parallel := min(max(runtime.GOMAXPROCS(0), 4), 12)
		if err := flag.Set("test.parallel", strconv.Itoa(parallel)); err != nil {
			panic(err)
		}
	}
	commandProcessTempDir = os.TempDir()
	if git, err := lookPathOutsideGitExecPath("git"); err == nil {
		commandProcessGit, _ = filepath.Abs(git)
	}
	if root := os.Getenv(processIsolatedRootEnv); os.Getenv(processIsolatedSelectorEnv) != "" {
		if !filepath.IsAbs(root) {
			os.Exit(2)
		}
		testMainHome = os.Getenv("HOME")
		config.SetTestV3OrganizationConfigPath(nil, filepath.Join(root, "org", "config.yaml"))
		os.Exit(m.Run())
	}
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
	if isolateCommandTest(t) {
		return
	}
	output, err := executeCommand("--version")
	if err != nil {
		t.Fatalf("expected version flag to succeed: %v", err)
	}

	if output != defaultVersion+"\n" {
		t.Fatalf("expected version output %q, got %q", defaultVersion+"\n", output)
	}
}

func TestDebugFlagEnablesStructuredDebugLogs(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())
	withWorkingDir(t, t.TempDir())
	if output, err := executeCommand("list"); err != nil {
		t.Fatalf("baseline list: %v\n%s", err, output)
	}
	if slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("ordinary list unexpectedly enabled debug logging")
	}
	if output, err := executeCommand("--debug", "list"); err != nil {
		t.Fatalf("debug list: %v\n%s", err, output)
	}
	if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("--debug did not enable structured debug logging")
	}
}

func TestSyncFlagsAreDocumentedAndValidated(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
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
	if isolateCommandTest(t) {
		return
	}
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
	if isolateCommandTest(t) {
		return
	}
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
	if isolateCommandTest(t) {
		return
	}
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

func TestCompletionCommandGeneratesEachShellScript(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		shell, marker string
	}{
		{"bash", "__start_agent-marketplace"},
		{"zsh", "#compdef agent-marketplace"},
		{"fish", "complete -c agent-marketplace"},
		{"powershell", "Register-ArgumentCompleter"},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			withDescriptions, err := executeCommand("completion", tc.shell)
			if err != nil || !strings.Contains(withDescriptions, tc.marker) {
				t.Fatalf("completion %s did not generate a script with %q: %v", tc.shell, tc.marker, err)
			}
			withoutDescriptions, err := executeCommand("completion", tc.shell, "--no-descriptions")
			if err != nil || !strings.Contains(withoutDescriptions, tc.marker) || withDescriptions == withoutDescriptions {
				t.Fatalf("completion %s --no-descriptions did not change a valid script: %v", tc.shell, err)
			}
		})
	}
}

func canonicalTempDir(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("canonicalize temporary directory: %v", err)
	}
	return canonical
}

func requireOrganizationLock(t testing.TB, configPath string) {
	t.Helper()
	if err := os.Chmod(configPath, 0o600); err != nil {
		t.Fatalf("secure organization config: %v", err)
	}
	if output, err := executeCommand("lock", "--scope", "organization"); err != nil {
		t.Fatalf("create trusted organization lock: %v\n%s", err, output)
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
