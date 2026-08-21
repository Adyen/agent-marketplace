package config

import (
	"context"
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

func TestOpenStoresErrorDoesNotLeakDSNSecret(t *testing.T) {
	const secret = "startup-config-secret"
	_, _, err := OpenStores(context.Background(), Config{Storage: Storage{
		Metadata: StorageMetadata{Driver: "postgres", DSN: "postgres://marketplace:" + secret + "@%zz"},
		Blobs:    StorageBlobs{Driver: "filesystem", Path: t.TempDir()},
	}})
	if err == nil {
		t.Fatal("OpenStores unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("OpenStores error leaked DSN secret: %v", err)
	}
}

func TestStorageMetadataResolveDSNFromEnv(t *testing.T) {
	const secret = "postgres://user:secret@example.invalid/db"
	t.Setenv("MARKETPLACE_TEST_DSN", secret)
	got, err := (StorageMetadata{DSNEnv: "MARKETPLACE_TEST_DSN"}).ResolveDSN()
	if err != nil {
		t.Fatalf("ResolveDSN: %v", err)
	}
	if got != secret {
		t.Fatalf("DSN = %q, want environment value", got)
	}
}

func TestStorageMetadataResolveDSNFromFile(t *testing.T) {
	const secret = "host=db.example.invalid user=marketplace password=secret dbname=marketplace"
	path := filepath.Join(t.TempDir(), "postgres-dsn")
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := (StorageMetadata{DSNFile: path}).ResolveDSN()
	if err != nil {
		t.Fatalf("ResolveDSN: %v", err)
	}
	if got != secret {
		t.Fatalf("DSN = %q, want file value", got)
	}
}

func TestStorageMetadataDriverDSNValidation(t *testing.T) {
	tests := []struct {
		name   string
		driver string
		dsn    string
		valid  bool
	}{
		{name: "sqlite path", driver: "sqlite", dsn: filepath.Join(t.TempDir(), "marketplace.db"), valid: true},
		{name: "sqlite memory", driver: "sqlite", dsn: ":memory:", valid: false},
		{name: "sqlite memory uri", driver: "sqlite", dsn: "file::memory:?mode=memory&cache=shared", valid: false},
		{name: "sqlite shared memory uri", driver: "sqlite", dsn: "file::memory:?cache=shared", valid: false},
		{name: "sqlite read-only uri", driver: "sqlite", dsn: "file:/var/lib/marketplace.db?mode=ro", valid: false},
		{name: "sqlite immutable uri", driver: "sqlite", dsn: "file:/var/lib/marketplace.db?immutable=1", valid: false},
		{name: "postgres url", driver: "postgres", dsn: "postgres://marketplace:secret@db.example.invalid/marketplace", valid: true},
		{name: "postgres key value", driver: "postgres", dsn: "host=db.example.invalid user=marketplace password=secret dbname=marketplace", valid: true},
		{name: "sqlite postgres url", driver: "sqlite", dsn: "postgres://user:secret@db.example.invalid/db", valid: false},
		{name: "postgres sqlite path", driver: "postgres", dsn: ":memory:", valid: false},
		{name: "postgres malformed url", driver: "postgres", dsn: "postgres://%zz", valid: false},
		{name: "postgres malformed key value", driver: "postgres", dsn: "host=db.example.invalid password=\"unterminated", valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := (Storage{Metadata: StorageMetadata{Driver: tt.driver, DSN: tt.dsn}, Blobs: StorageBlobs{Driver: "filesystem", Path: "/tmp/blobs"}}).Validate()
			if (len(errs) == 0) != tt.valid {
				t.Fatalf("Validate() errors = %v, valid = %v", errs, tt.valid)
			}
			for _, err := range errs {
				if strings.Contains(err.Error(), "secret") {
					t.Fatalf("validation error leaked DSN secret: %v", err)
				}
			}
		})
	}
}

func TestStorageMetadataDriverDSNValidationFromEnvAndFile(t *testing.T) {
	t.Setenv("MARKETPLACE_POSTGRES_DSN", "postgres://marketplace:secret@db.example.invalid/marketplace")
	if errs := (Storage{Metadata: StorageMetadata{Driver: "postgres", DSNEnv: "MARKETPLACE_POSTGRES_DSN"}, Blobs: StorageBlobs{Driver: "filesystem", Path: "/tmp/blobs"}}).Validate(); len(errs) != 0 {
		t.Fatalf("valid dsn_env rejected: %v", errs)
	}
	path := filepath.Join(t.TempDir(), "dsn")
	if err := os.WriteFile(path, []byte("postgres://marketplace:secret@db.example.invalid/marketplace\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if errs := (Storage{Metadata: StorageMetadata{Driver: "sqlite", DSNFile: path}, Blobs: StorageBlobs{Driver: "filesystem", Path: "/tmp/blobs"}}).Validate(); len(errs) == 0 {
		t.Fatal("expected sqlite dsn_file containing postgres-incompatible DSN to be rejected")
	}
	if err := os.WriteFile(path, []byte(filepath.Join(t.TempDir(), "marketplace.db")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if errs := (Storage{Metadata: StorageMetadata{Driver: "sqlite", DSNFile: path}, Blobs: StorageBlobs{Driver: "filesystem", Path: "/tmp/blobs"}}).Validate(); len(errs) != 0 {
		t.Fatalf("valid sqlite dsn_file rejected: %v", errs)
	}
	if err := os.WriteFile(path, []byte("file::memory:?mode=memory&cache=shared\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if errs := (Storage{Metadata: StorageMetadata{Driver: "sqlite", DSNFile: path}, Blobs: StorageBlobs{Driver: "filesystem", Path: "/tmp/blobs"}}).Validate(); len(errs) == 0 {
		t.Fatal("expected sqlite memory dsn_file to be rejected for production configuration")
	}
}

func TestStorageMetadataDSNSourceValidationDoesNotLeakSecret(t *testing.T) {
	const secret = "postgres://user:secret@example.invalid/db"
	t.Setenv("MARKETPLACE_TEST_DSN", "")
	_, err := (StorageMetadata{
		DSN:    secret,
		DSNEnv: "MARKETPLACE_TEST_DSN",
	}).ResolveDSN()
	if err == nil {
		t.Fatal("expected multiple DSN source error")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("DSN secret leaked in error: %v", err)
	}
}

func TestLoadConfigRejectsUnsafeCanonicalURL(t *testing.T) {
	for _, raw := range []string{
		"https://user:canonical-secret@example.com",
		"https://marketplace.example/canonical#fragment",
		"https://marketplace.example/canonical?oauth_token=canonical-secret",
		"https://marketplace.example/canonical?X-Amz-Security-Token=canonical-secret",
		"https://marketplace.example/canonical?signature=canonical-secret",
	} {
		t.Run(raw, func(t *testing.T) {
			path := writeConfig(t, strings.Replace(validConfigYAML(), "https://marketplace.test", raw, 1))
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatalf("LoadConfig accepted unsafe canonical URL %q", raw)
			}
			if strings.Contains(err.Error(), "canonical-secret") {
				t.Fatalf("canonical URL secret leaked in validation error: %v", err)
			}
		})
	}
}

func TestLoadConfigRejectsUnsafePublisherURL(t *testing.T) {
	for _, raw := range []string{
		"https://publisher.example#fragment",
		"https://publisher.example?oauth_token=publisher-secret",
		"https://publisher.example?X-Amz-Security-Token=publisher-secret",
		"https://publisher.example?signature=publisher-secret",
	} {
		t.Run(raw, func(t *testing.T) {
			yaml := strings.Replace(validConfigYAML(), "      name: Example", "      name: Example\n      url: "+raw, 1)
			_, err := LoadConfig(writeConfig(t, yaml))
			if err == nil {
				t.Fatalf("LoadConfig accepted unsafe publisher URL %q", raw)
			}
			if strings.Contains(err.Error(), "publisher-secret") {
				t.Fatalf("publisher URL secret leaked in validation error: %v", err)
			}
		})
	}
}

func TestLoadConfigRejectsSourceOriginUserinfo(t *testing.T) {
	path := writeConfig(t, strings.Replace(validConfigYAML(), "https://github.com/example/resources.git", "https://user:source-secret@example.com/resources.git", 1))
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "userinfo") {
		t.Fatalf("expected source origin userinfo rejection, got %v", err)
	}
	if strings.Contains(err.Error(), "source-secret") {
		t.Fatalf("origin secret leaked in validation error: %v", err)
	}
}

func TestLoadConfigRejectsSourceOriginQueryCredentials(t *testing.T) {
	path := writeConfig(t, strings.Replace(validConfigYAML(), "https://github.com/example/resources.git", "https://example.com/resources.git?access_token=source-secret", 1))
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "query parameters") {
		t.Fatalf("expected source origin query credential rejection, got %v", err)
	}
	if strings.Contains(err.Error(), "source-secret") {
		t.Fatalf("origin query secret leaked in validation error: %v", err)
	}
}

func TestLoadConfigRejectsNegativeRefreshConcurrency(t *testing.T) {
	path := writeConfig(t, strings.Replace(validConfigYAML(), "refresh:\n  interval: 30m", "refresh:\n  interval: 30m\nrefresh_concurrency: -1", 1))
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "refresh_concurrency") {
		t.Fatalf("expected negative refresh_concurrency rejection, got %v", err)
	}
}

func TestLoadConfigRejectsMalformedSourceOriginQuery(t *testing.T) {
	path := writeConfig(t, strings.Replace(validConfigYAML(), "https://github.com/example/resources.git", "https://example.com/resources.git?access_token=%zz", 1))
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "valid URL query") {
		t.Fatalf("expected malformed source origin query rejection, got %v", err)
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
