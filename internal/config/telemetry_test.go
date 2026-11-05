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

func TestV4OrganizationAcceptsTelemetry(t *testing.T) {
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caFile, []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, root, fmt.Sprintf(`apiVersion: agent-marketplace/v4
harnesses: [factory]
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
`, caFile))
	SetTestOrganizationConfigPath(t, root)
	cfg, err := LoadOrganizationConfig()
	if err != nil {
		t.Fatalf("expected org telemetry to be accepted: %v", err)
	}
	if cfg == nil || cfg.Telemetry == nil {
		t.Fatal("expected telemetry config to be parsed")
	}
	if cfg.Telemetry.Level != telemetryconfig.LevelDetailed || cfg.Telemetry.Endpoint != "https://otel.example.com" || cfg.Telemetry.CAFile != caFile || cfg.Telemetry.RetentionPolicy != "https://example.com/retention" || cfg.Telemetry.CommunityReporting == nil || !*cfg.Telemetry.CommunityReporting {
		t.Fatalf("telemetry policy lost: %#v", cfg.Telemetry)
	}
	if cfg.Telemetry.Headers["Authorization"].Env != "OTEL_AUTH" {
		t.Fatal("header source mismatch")
	}
}

func TestV4UserAcceptsTelemetry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeIncludeConfig(t, filepath.Join(home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
telemetry:
  level: basic
  endpoint: https://user-otel.example.com
  community_reporting: true
`)
	cfg, err := LoadGlobalConfig()
	if err != nil || cfg == nil || cfg.Telemetry == nil || cfg.Telemetry.Level != "basic" || cfg.Telemetry.Endpoint != "https://user-otel.example.com" {
		t.Fatalf("expected user telemetry policy: cfg=%#v err=%v", cfg, err)
	}
}

func TestV4RepoAndLocalRejectTelemetry(t *testing.T) {
	for _, filename := range []string{"config.yaml", "config.local.yaml"} {
		t.Run(filename, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), filename)
			writeIncludeConfig(t, path, "apiVersion: agent-marketplace/v4\ntelemetry: {level: basic}\n")
			_, err := LoadConfigFromPath(path)
			var validation *V4ValidationError
			if !errors.As(err, &validation) || validation.Code != "v4.layer-field" || validation.Path != "auxiliary" {
				t.Fatalf("expected layer-specific telemetry refusal: %v", err)
			}
		})
	}
}

func TestTelemetryReadersRefuseLegacyVersions(t *testing.T) {
	for _, version := range []string{"agent-marketplace/v2", "agent-marketplace/v3"} {
		for _, layer := range []string{"user", "repo", "local", "organization"} {
			t.Run(version+"/"+layer, func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("HOME", dir)
				path := filepath.Join(dir, ".agents", "config.yaml")
				if layer == "local" {
					path = filepath.Join(dir, ".agents", "config.local.yaml")
				}
				writeIncludeConfig(t, path, "apiVersion: "+version+"\ntelemetry: {level: basic}\n")
				var err error
				switch layer {
				case "user":
					_, err = LoadUserSettings()
				case "organization":
					SetTestOrganizationConfigPath(t, path)
					_, err = LoadOrganizationSettings()
				case "repo":
					_, err = loadSettings(path, repoLayer)
				case "local":
					_, err = LoadLocalConfigFromPath(path)
				}
				var validation *V4ValidationError
				if !errors.As(err, &validation) || validation.Code != "v4.api-version" || validation.Path != path || validation.Line != 1 {
					t.Fatalf("settings format refusal = %v", err)
				}
			})
		}
	}
}

func TestConfigValidateRejectsDiagnosticLevel(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory"},
		Telemetry:  &telemetryconfig.TelemetryConfig{Level: telemetryconfig.LevelDiagnostic},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "diagnostic") {
		t.Fatalf("expected diagnostic level to be rejected, got %v", err)
	}
}

func TestSourceCanConfigureTelemetry(t *testing.T) {
	for _, tc := range []struct {
		source Source
		want   bool
	}{
		{SourceUnset, false}, {Organization, true}, {UserPersonal, true}, {RepoCommitted, false}, {RepoPersonal, false},
	} {
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
	if settings.Telemetry == nil || settings.Telemetry.Level != telemetryconfig.LevelBasic || len(settings.Harnesses) != 1 || settings.Harnesses[0] != "factory" {
		t.Fatalf("settings policy lost with v1 lock: %#v", settings)
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
