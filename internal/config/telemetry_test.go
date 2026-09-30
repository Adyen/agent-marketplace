package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	telemetryconfig "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

func TestV3OrganizationAcceptsTelemetry(t *testing.T) {
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caFile, []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte(fmt.Sprintf(`apiVersion: agent-marketplace/v3
harnesses:
  - factory
telemetry:
  level: detailed
  endpoint: https://otel.example.com
  retention_policy: https://example.com/retention
  community_reporting: true
  ca_file: %s
  allow_insecure: false
  headers:
    Authorization:
      env: OTEL_AUTH
registries: []
`, caFile))
	cfg, errs := loadV3Config(data, organizationLayer, false)
	if len(errs) > 0 {
		t.Fatalf("expected org telemetry to be accepted: %v", errs)
	}
	if cfg == nil || cfg.Telemetry == nil {
		t.Fatal("expected telemetry config to be parsed")
	}
	if cfg.Telemetry.Level != telemetryconfig.LevelDetailed {
		t.Fatalf("level = %q", cfg.Telemetry.Level)
	}
	if cfg.Telemetry.Endpoint != "https://otel.example.com" {
		t.Fatalf("endpoint = %q", cfg.Telemetry.Endpoint)
	}
	if cfg.Telemetry.Headers["Authorization"].Env != "OTEL_AUTH" {
		t.Fatalf("header source mismatch")
	}
}

func TestV3UserAcceptsTelemetry(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v3
harnesses:
  - factory
telemetry:
  level: basic
  endpoint: https://user-otel.example.com
  community_reporting: true
registries: []
`)
	cfg, errs := loadV3Config(data, userLayer, true)
	if len(errs) > 0 {
		t.Fatalf("expected user telemetry to be accepted: %v", errs)
	}
	if cfg == nil || cfg.Telemetry == nil {
		t.Fatal("expected telemetry config to be parsed")
	}
}

func TestV3RepoRejectsTelemetry(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v3
registries: []
telemetry:
  level: basic
`)
	_, errs := loadV3Config(data, repoLayer, false)
	if len(errs) == 0 {
		t.Fatal("expected repo config to reject telemetry")
	}
	combined := errorsJoin(errs)
	if !strings.Contains(combined, "telemetry") {
		t.Fatalf("expected error to mention telemetry, got: %v", combined)
	}
}

func TestV3LocalRejectsTelemetry(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v3
registries: []
telemetry:
  level: basic
`)
	_, errs := loadV3LocalConfig(data)
	if len(errs) == 0 {
		t.Fatal("expected local config to reject telemetry")
	}
	combined := errorsJoin(errs)
	if !strings.Contains(combined, "telemetry") {
		t.Fatalf("expected error to mention telemetry, got: %v", combined)
	}
}

func TestLoadConfigGatedRepoRejectsTelemetryV2(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte(`apiVersion: agent-marketplace/v2
telemetry:
  level: basic
registries: []
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadConfigGated(path, false, true, repoLayer.name())
	if err == nil {
		t.Fatal("expected repo v2 config to reject telemetry")
	}
	if !strings.Contains(err.Error(), "telemetry") {
		t.Fatalf("expected error to mention telemetry, got: %v", err)
	}
}

func TestLoadConfigGatedUserAcceptsTelemetryV2(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte(`apiVersion: agent-marketplace/v2
harnesses:
  - factory
telemetry:
  level: basic
registries: []
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfigGated(path, true, true, userLayer.name())
	if err != nil {
		t.Fatalf("expected user v2 config to accept telemetry: %v", err)
	}
	if cfg == nil || cfg.Telemetry == nil {
		t.Fatal("expected telemetry config to be parsed")
	}
}

func TestConfigValidateRejectsDiagnosticLevel(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV2,
		Harnesses:  []string{"factory"},
		Telemetry: &telemetryconfig.TelemetryConfig{
			Level: telemetryconfig.LevelDiagnostic,
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected diagnostic level to be rejected")
	}
	if !strings.Contains(err.Error(), "diagnostic") {
		t.Fatalf("expected error to mention diagnostic, got: %v", err)
	}
}

func TestSourceCanConfigureTelemetry(t *testing.T) {
	tests := []struct {
		source Source
		want   bool
	}{
		{SourceUnset, false},
		{Organization, true},
		{UserPersonal, true},
		{RepoCommitted, false},
		{RepoPersonal, false},
	}
	for _, tc := range tests {
		t.Run(tc.source.String(), func(t *testing.T) {
			if got := tc.source.CanConfigureTelemetry(); got != tc.want {
				t.Fatalf("CanConfigureTelemetry() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOrganizationSettingsIgnoreMarketplaceLock(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(root, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\ntelemetry: {level: basic}\nsources:\n  hosted: {marketplace: https://market.example}\n  shared: {git: https://example.com/shared.git, ref: main}\nresources:\n  hosted: {include: [tools/skill/review]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	SetTestOrganizationConfigPath(t, root)

	if _, err := LoadOrganizationConfig(); err == nil || !strings.Contains(err.Error(), "v4.marketplace-lock-required") {
		t.Fatalf("source-bearing view error = %v, want lock requirement", err)
	}
	settings, err := LoadOrganizationSettings()
	if err != nil {
		t.Fatalf("absent organization lock settings: %v", err)
	}
	if settings.Telemetry == nil || settings.Telemetry.Level != telemetryconfig.LevelBasic {
		t.Fatalf("settings telemetry = %#v, want basic policy", settings.Telemetry)
	}
	if len(settings.Harnesses) != 1 || settings.Harnesses[0] != "factory" {
		t.Fatalf("settings harnesses = %v, want [factory]", settings.Harnesses)
	}

	if err := os.WriteFile(filepath.Join(dir, "lock.yaml"), []byte("apiVersion: agent-marketplace-lock/v1\nscope: organization\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings, err = LoadOrganizationSettings()
	if err != nil {
		t.Fatalf("present v1 organization lock settings: %v", err)
	}
	if settings.Telemetry == nil || settings.Telemetry.Level != telemetryconfig.LevelBasic {
		t.Fatalf("settings telemetry = %#v, want basic policy", settings.Telemetry)
	}
	if len(settings.Harnesses) != 1 || settings.Harnesses[0] != "factory" {
		t.Fatalf("settings harnesses = %v, want [factory]", settings.Harnesses)
	}
}

func TestOrganizationSettingsRejectLayerInvalidV4(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(root, []byte("apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\ntelemetry: {level: basic}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	SetTestOrganizationConfigPath(t, root)

	_, err := LoadOrganizationSettings()
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.layer-field" {
		t.Fatalf("error = %v, want v4.layer-field", err)
	}
}
