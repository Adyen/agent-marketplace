package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigValid(t *testing.T) {
	path := writeConfig(t, validConfigYAML())
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Instance.ID != "test-mp" {
		t.Fatalf("instance id = %q, want test-mp", cfg.Instance.ID)
	}
	if cfg.Listen != "127.0.0.1:8080" {
		t.Fatalf("listen = %q, want 127.0.0.1:8080", cfg.Listen)
	}
	if cfg.Policy.HiddenContent != "reject" {
		t.Fatalf("hidden_content = %q, want reject", cfg.Policy.HiddenContent)
	}
	if len(cfg.Policy.LicenseAllowlist) != 4 {
		t.Fatalf("license_allowlist len = %d, want 4", len(cfg.Policy.LicenseAllowlist))
	}
	if cfg.Policy.Limits.RepositoryBytes != 100*1024*1024 {
		t.Fatalf("repository_bytes = %d, want 104857600", cfg.Policy.Limits.RepositoryBytes)
	}
	if cfg.Sources[0].Acquisition != "mirror" {
		t.Fatalf("source acquisition = %q, want mirror", cfg.Sources[0].Acquisition)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	path := writeConfig(t, minimalConfigYAML())
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Listen != "127.0.0.1:8080" {
		t.Fatalf("listen default = %q", cfg.Listen)
	}
	if cfg.AcquisitionDefault != "mirror" {
		t.Fatalf("acquisition_default default = %q", cfg.AcquisitionDefault)
	}
	if cfg.Policy.HiddenContent != "reject" {
		t.Fatalf("hidden_content default = %q", cfg.Policy.HiddenContent)
	}
	if len(cfg.Policy.LicenseAllowlist) != 4 {
		t.Fatalf("license_allowlist default len = %d", len(cfg.Policy.LicenseAllowlist))
	}
	if cfg.Policy.Limits.RepositoryBytes == 0 {
		t.Fatalf("limits did not default")
	}
	if cfg.Refresh.Interval.Duration() == 0 {
		t.Fatalf("refresh interval did not default")
	}
}

func TestLoadConfigUnknownField(t *testing.T) {
	path := writeConfig(t, validConfigYAML()+"\nunknown_field: true\n")
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatalf("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), "unknown") && !strings.Contains(err.Error(), "field") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigInvalidMetadataDriver(t *testing.T) {
	yaml := strings.ReplaceAll(validConfigYAML(), "driver: sqlite", "driver: mysql")
	path := writeConfig(t, yaml)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatalf("expected error for invalid metadata driver")
	}
}

func TestLoadConfigInlineSSHCredential(t *testing.T) {
	src := sourceYAML(`    credentials:
      ssh_key:
        value: secret-key
`)
	path := writeConfig(t, configYAML(src))
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatalf("expected error for inline ssh credential value")
	}
	if !strings.Contains(err.Error(), "inline secret") {
		t.Fatalf("expected inline secret error, got %v", err)
	}
}

func TestLoadConfigInlineTokenCredential(t *testing.T) {
	src := sourceYAML(`    credentials:
      token:
        value: secret-token
`)
	path := writeConfig(t, configYAML(src))
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatalf("expected error for inline token credential value")
	}
}

func TestLoadConfigLimitBounds(t *testing.T) {
	yaml := strings.ReplaceAll(validConfigYAML(), "repository_bytes: 104857600", "repository_bytes: -1")
	path := writeConfig(t, yaml)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatalf("expected error for negative repository_bytes")
	}
}

func TestLoadConfigDuplicateSourceID(t *testing.T) {
	sources := `  - id: src-test
    slug: example-source
    origin: https://github.com/example/resources.git
    ref: main
    roots:
      - path: .agents
  - id: src-test
    slug: example2
    origin: https://github.com/example/other.git
    ref: main
    roots:
      - path: .agents
`
	path := writeConfig(t, configYAML(sources))
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatalf("expected error for duplicate source id")
	}
}

func TestLoadConfigTraversingRootPath(t *testing.T) {
	yaml := strings.ReplaceAll(validConfigYAML(), "path: .agents", "path: ../escape")
	path := writeConfig(t, yaml)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatalf("expected error for traversing root path")
	}
}

func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func validConfigYAML() string {
	return configYAML(sourceYAML(""))
}

func minimalConfigYAML() string {
	return configYAML(`  - id: src-test
    slug: example-source
    origin: https://github.com/example/resources.git
    ref: main
    roots:
      - path: .agents
`)
}

func configYAML(sourcesBlock string) string {
	return `apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: test-mp
  name: Test Marketplace
  canonical_url: https://marketplace.test
listen: 127.0.0.1:8080
storage:
  metadata:
    driver: sqlite
    dsn: /tmp/marketplace.db
  blobs:
    driver: filesystem
    path: /tmp/blobs
acquisition_default: mirror
refresh:
  interval: 30m
policy:
  hidden_content: reject
  license_allowlist:
    - MIT
    - Apache-2.0
    - BSD-2-Clause
    - BSD-3-Clause
  limits:
    repository_bytes: 104857600
    file_count: 1024
    file_bytes: 2097152
    artifact_bytes: 16777216
    clone_duration: 300s
    ingestion_duration: 120s
sources:
` + sourcesBlock + `revocations:
  source_ids: []
  coordinates: []
  revisions: []
`
}

func sourceYAML(extra string) string {
	return `  - id: src-test
    slug: example-source
    aliases: []
    origin: https://github.com/example/resources.git
    ref: main
    acquisition: mirror
    publisher:
      name: Example
    license: MIT
    roots:
      - path: .agents
        source_harness: factory
` + extra
}
