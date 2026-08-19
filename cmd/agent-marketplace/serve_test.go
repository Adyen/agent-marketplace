package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeRequiresConfig(t *testing.T) {
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
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(minimalMarketplaceConfig()), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newServeCmd()
	cmd.SetArgs([]string{"--config", path, "--role", "api"})
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

func TestServeReindexRequiresConfig(t *testing.T) {
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
sources:
  - id: src-test
    slug: example-source
    origin: https://github.com/example/resources.git
    ref: main
    roots:
      - path: .agents
`
}
