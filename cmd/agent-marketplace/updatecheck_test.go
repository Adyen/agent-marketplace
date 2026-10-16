package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/updatecheck"
)

const upgradeNoticeTestState = `{"checked_at":"%s","latest":{"tag":"v4.1.4","published_at":"%s","url":"https://github.com/Adyen/agent-marketplace/releases/tag/v4.1.4"}}`

// upgradeNoticeFixture points the release check at a counting server, a private
// state root, and a terminal stderr, with the running version set to v4.1.3.
func upgradeNoticeFixture(t *testing.T) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, `{"tag_name":"v4.1.4","published_at":"2026-09-28T10:00:00Z","html_url":"https://github.com/Adyen/agent-marketplace/releases/tag/v4.1.4"}`)
	}))
	t.Cleanup(srv.Close)

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("CI", "")
	t.Setenv(updatecheck.EnvDisable, "")

	prevEndpoint, prevVersion, prevTerminal := upgradeNoticeEndpoint, version, isTerminal
	upgradeNoticeEndpoint = srv.URL
	version = "v4.1.3"
	isTerminal = func(uintptr) bool { return true }
	t.Cleanup(func() {
		upgradeNoticeEndpoint, version, isTerminal = prevEndpoint, prevVersion, prevTerminal
		upgradeNotice = nil
	})
	return &hits
}

// seedUpgradeNotice writes a cache whose release is already newer, so a run
// that consults the cache alone has a notice to show.
func seedUpgradeNotice(t *testing.T) {
	t.Helper()
	now := time.Now().UTC()
	body := []byte(fmt.Sprintf(upgradeNoticeTestState, now.Add(-time.Hour).Format(time.RFC3339), now.Add(-24*time.Hour).Format(time.RFC3339)))
	path := filepath.Join(os.Getenv("XDG_STATE_HOME"), "agent-marketplace", "update-check.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

// runUpgradeNotice performs the start and finish steps the process runs around cmd.
func runUpgradeNotice(cmd *cobra.Command, rootTUI, showVersion bool) string {
	var stderr bytes.Buffer
	startUpgradeNotice(cmd, rootTUI, showVersion)
	finishUpgradeNotice(&stderr)
	return stderr.String()
}

// spec: upgrade-notice/notice-is-written-once-per-day-on-a-terminal
func TestUpgradeNoticeWrittenOncePerDayOnTerminal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	hits := upgradeNoticeFixture(t)
	seedUpgradeNotice(t)
	cmd := newRootCmd()
	cmd.SetContext(t.Context())

	want := "info: agent-marketplace v4.1.4 is available (you have v4.1.3): https://github.com/Adyen/agent-marketplace/releases/tag/v4.1.4\n"
	if got := runUpgradeNotice(cmd, false, false); got != want {
		t.Fatalf("first command stderr = %q, want %q", got, want)
	}
	if got := runUpgradeNotice(cmd, false, false); got != "" {
		t.Fatalf("second command within 24h stderr = %q, want none", got)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("fresh cache made %d requests, want 0", n)
	}
}

// spec: upgrade-notice/non-terminal-standard-error-receives-no-notice
func TestUpgradeNoticeNonTerminalStderrIsSilent(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	hits := upgradeNoticeFixture(t)
	seedUpgradeNotice(t)
	isTerminal = func(uintptr) bool { return false }
	cmd := newRootCmd()
	cmd.SetContext(t.Context())

	if got := runUpgradeNotice(cmd, false, false); got != "" {
		t.Fatalf("stderr = %q, want none", got)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("non-terminal stderr made %d requests, want 0", n)
	}
}

// spec: upgrade-notice/opt-out-variable-suppresses-the-check
func TestUpgradeNoticeOptOutVariableSuppressesCheck(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	hits := upgradeNoticeFixture(t)
	seedUpgradeNotice(t)
	t.Setenv(updatecheck.EnvDisable, "1")
	cmd := newRootCmd()
	cmd.SetContext(t.Context())

	if got := runUpgradeNotice(cmd, false, false); got != "" {
		t.Fatalf("stderr = %q, want none", got)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("opted-out run made %d requests, want 0", n)
	}
}

// spec: upgrade-notice/continuous-integration-suppresses-the-check
func TestUpgradeNoticeContinuousIntegrationSuppressesCheck(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	hits := upgradeNoticeFixture(t)
	seedUpgradeNotice(t)
	t.Setenv("CI", "true")
	cmd := newRootCmd()
	cmd.SetContext(t.Context())

	if got := runUpgradeNotice(cmd, false, false); got != "" {
		t.Fatalf("stderr = %q, want none", got)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("CI run made %d requests, want 0", n)
	}
}

// spec: upgrade-notice/excluded-commands-suppress-the-check
func TestUpgradeNoticeExcludedCommandsAreSilent(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	hits := upgradeNoticeFixture(t)
	seedUpgradeNotice(t)
	root := newRootCmd()
	serve, _, err := root.Find([]string{"serve"})
	if err != nil {
		t.Fatal(err)
	}
	tuiCmd, _, err := root.Find([]string{"tui"})
	if err != nil {
		t.Fatal(err)
	}
	completion := &cobra.Command{Use: "completion"}
	bash := &cobra.Command{Use: "bash"}
	completion.AddCommand(bash)
	for name, run := range map[string]func() string{
		"serve":            func() string { return runUpgradeNotice(serve, false, false) },
		"tui":              func() string { return runUpgradeNotice(tuiCmd, false, false) },
		"__completeNoDesc": func() string { return runUpgradeNotice(&cobra.Command{Use: "__completeNoDesc"}, false, false) },
		"completion bash":  func() string { return runUpgradeNotice(bash, false, false) },
		"--version":        func() string { return runUpgradeNotice(root, false, true) },
	} {
		if got := run(); got != "" {
			t.Fatalf("%s: stderr = %q, want none", name, got)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("excluded commands made %d requests, want 0", n)
	}
}

// spec: upgrade-notice/user-configuration-disables-the-check
func TestUpgradeNoticeConfigOptOut(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	hits := upgradeNoticeFixture(t)
	seedUpgradeNotice(t)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nupdate_check:\n  enabled: false\n")
	cmd := newRootCmd()
	cmd.SetContext(t.Context())

	if got := runUpgradeNotice(cmd, false, false); got != "" {
		t.Fatalf("stderr = %q, want none", got)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("opted-out run made %d requests, want 0", n)
	}
}
