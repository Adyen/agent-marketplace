package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
