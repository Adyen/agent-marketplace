package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
)

func TestServeRequiresConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	cmd := newServeCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for missing --config")
	}
	if !strings.Contains(err.Error(), "config") {
		t.Fatalf("expected config error, got %v", err)
	}
}

func TestServeRejectsUnsupportedRole(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(minimalMarketplaceConfig()), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newServeCmd()
	cmd.SetArgs([]string{"--config", path, "--role", "invalid"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for unsupported role")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported role error, got %v", err)
	}
}

func TestServeRejectsDigestOnlyRevisionRevocation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	contents := minimalMarketplaceConfig() + `
revocations:
  revisions:
    - sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := mpconfig.LoadConfig(path); err == nil || !strings.Contains(err.Error(), "revocations.revisions") {
		t.Fatalf("digest-only revocation validation error = %v", err)
	}
	cmd := newServeCmd()
	cmd.SetArgs([]string{"--config", path, "--role", "invalid"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("serve unexpectedly accepted invalid role")
	}
}

// spec: marketplace-runtime-configuration/a-replaced-configuration-file-is-applied
func TestServeWorkerPollsAtomicRuntimeConfigReplacement(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	contents := fmt.Sprintf(`apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: test-mp
  name: Test Marketplace
  canonical_url: https://marketplace.test
storage:
  metadata:
    driver: sqlite
    dsn: %s
  blobs:
    driver: filesystem
    path: %s
refresh:
  interval: 30m
registries: []
`, filepath.Join(dir, "marketplace.db"), filepath.Join(dir, "blobs"))
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	logs := &serveTestLogBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	defer slog.SetDefault(previous)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve worker: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("serve worker did not stop after cancellation")
		}
		slog.SetDefault(previous)
	}()
	cmd := newServeCmd()
	cmd.SetArgs([]string{"--config", path, "--role", "worker"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	go func() { done <- cmd.ExecuteContext(ctx) }()
	waitForServeLog(t, logs, "serve running")

	replacement := strings.Replace(contents, "interval: 30m", "interval: 45m", 1)
	replacementPath := path + ".replacement"
	if err := os.WriteFile(replacementPath, []byte(replacement), 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.Rename(replacementPath, path); err != nil {
		t.Fatalf("replace config: %v", err)
	}
	waitForServeLog(t, logs, "config candidate applied")
	if !strings.Contains(logs.String(), "version=1") {
		t.Fatalf("runtime config version was not advanced: %q", logs.String())
	}
}

func TestServeReindexRequiresConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	cmd := newServeReindexCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for missing --config")
	}
	if !strings.Contains(err.Error(), "config") {
		t.Fatalf("expected config error, got %v", err)
	}
}

func waitForServeLog(t *testing.T, logs *serveTestLogBuffer, message string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if strings.Contains(logs.String(), message) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("serve did not log %q: %s", message, logs.String())
		case <-ticker.C:
		}
	}
}

type serveTestLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *serveTestLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *serveTestLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func minimalMarketplaceConfig() string {
	return `apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: test-mp
  name: Test Marketplace
  canonical_url: https://marketplace.test
storage:
  metadata:
    driver: sqlite
    dsn: /tmp/marketplace-test.db
  blobs:
    driver: filesystem
    path: /tmp/blobs-test
registries:
  - id: src-test
    slug: example-registry
    origin: https://github.com/example/resources.git
    ref: main
    roots:
      - path: .agents
`
}
