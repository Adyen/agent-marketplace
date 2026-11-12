package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// spec: claude-enterprise-delivery/inline-api-key-is-refused-at-validation
func TestClaudeEnterpriseInlineAPIKeyIsRefusedWithFieldCode(t *testing.T) {
	raw := validConfigYAML() + `
claude_enterprise:
  enabled: true
  api_key:
    value: do-not-render-this-key
`
	cfg, err := ParseConfig([]byte(raw))
	if err == nil {
		t.Fatal("inline Claude Admin API key was accepted")
	}
	var coded interface {
		ErrorCode() string
		FieldName() string
	}
	if !errors.As(err, &coded) {
		t.Fatalf("error = %T %v, want coded Claude Enterprise validation error", err, err)
	}
	if coded.ErrorCode() != "claude_enterprise.api-key-inline" ||
		coded.FieldName() != "claude_enterprise.api_key" {
		t.Fatalf("coded error = %q for %q", coded.ErrorCode(), coded.FieldName())
	}
	if strings.Contains(err.Error(), "do-not-render-this-key") {
		t.Fatalf("validation error disclosed the inline key: %v", err)
	}
	yamlDump, dumpErr := yaml.Marshal(cfg)
	if dumpErr != nil {
		t.Fatalf("marshal invalid candidate: %v", dumpErr)
	}
	for _, rendered := range []string{fmt.Sprintf("%v %#v %+v", cfg, cfg, cfg), string(yamlDump)} {
		if strings.Contains(rendered, "do-not-render-this-key") {
			t.Fatalf("configuration rendering disclosed the inline key: %q", rendered)
		}
	}

	_, err = ParseConfig([]byte(validConfigYAML() + `
claude_enterprise:
  enabled: true
  api_key: scalar-secret-sentinel
`))
	if err == nil {
		t.Fatal("scalar Claude Admin API key was accepted")
	}
	if !errors.As(err, &coded) {
		t.Fatalf("scalar error = %T %v, want coded Claude Enterprise validation error", err, err)
	}
	if coded.ErrorCode() != "claude_enterprise.api-key-inline" ||
		coded.FieldName() != "claude_enterprise.api_key" {
		t.Fatalf("scalar coded error = %q for %q", coded.ErrorCode(), coded.FieldName())
	}
	if strings.Contains(err.Error(), "scalar-secret-sentinel") {
		t.Fatalf("validation error disclosed scalar key: %v", err)
	}
}

func TestClaudeEnterpriseDefaultsAndReferences(t *testing.T) {
	cfg, err := ParseConfig([]byte(validConfigYAML() + `
claude_enterprise:
  enabled: true
  api_key_id: apikey_AbC123
  api_key:
    env: CLAUDE_ENTERPRISE_ADMIN_KEY
`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.ClaudeEnterprise == nil {
		t.Fatal("ClaudeEnterprise configuration was not parsed")
	}
	if got := cfg.ClaudeEnterprise.PluginNamePrefix; got == nil || *got != "am" {
		t.Errorf("plugin_name_prefix default = %v, want am", got)
	}
	if got := cfg.ClaudeEnterprise.DefaultInstallationPreference; got == nil || *got != "available" {
		t.Errorf("default_installation_preference default = %v, want available", got)
	}
	if got := cfg.ClaudeEnterprise.Interval; got == nil || got.Duration() != 24*time.Hour {
		t.Errorf("default interval = %v, want 24h", got)
	}
	if got := cfg.ClaudeEnterprise.APIKey.Env; got != "CLAUDE_ENTERPRISE_ADMIN_KEY" {
		t.Fatalf("API key environment reference = %q", got)
	}
	if got := cfg.ClaudeEnterprise.APIKeyID; got != "apikey_AbC123" {
		t.Fatalf("API key identifier = %q", got)
	}
}

func TestClaudeEnterpriseAPIKeyIdentifierValidation(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		wantErr bool
	}{
		{name: "missing when enabled", field: ""},
		{name: "valid identifier", field: "apikey_AbC123"},
		{name: "invalid identifier", field: "apikey_bad-id", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			field := ""
			if test.field != "" {
				field = "  api_key_id: " + test.field + "\n"
			}
			_, err := ParseConfig([]byte(validConfigYAML() + `
claude_enterprise:
  enabled: true
` + field + `  api_key:
    env: CLAUDE_ENTERPRISE_ADMIN_KEY
`))
			if test.wantErr || test.field == "" {
				var coded interface {
					ErrorCode() string
					FieldName() string
				}
				if !errors.As(err, &coded) || coded.ErrorCode() != "claude_enterprise.api-key-id" ||
					coded.FieldName() != "claude_enterprise.api_key_id" {
					t.Fatalf("identifier validation error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseConfig: %v", err)
			}
		})
	}
}

func TestClaudeEnterpriseAcceptsFileReferencedAPIKey(t *testing.T) {
	cfg, err := ParseConfig([]byte(validConfigYAML() + `
claude_enterprise:
  enabled: true
  api_key_id: apikey_AbC123
  api_key:
    file: /run/secrets/claude-admin-key
`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.ClaudeEnterprise == nil || cfg.ClaudeEnterprise.APIKey.File != "/run/secrets/claude-admin-key" {
		t.Fatalf("file credential reference = %#v", cfg.ClaudeEnterprise)
	}
}

func TestClaudeEnterprisePreferenceAndPrefixValidation(t *testing.T) {
	tests := []struct {
		name  string
		block string
		field string
		code  string
	}{
		{
			name:  "unknown installation preference",
			block: "  default_installation_preference: someday\n",
			field: "claude_enterprise.default_installation_preference",
			code:  "claude_enterprise.installation-preference",
		},
		{
			name:  "uppercase prefix",
			block: "  plugin_name_prefix: AM\n",
			field: "claude_enterprise.plugin_name_prefix",
			code:  "claude_enterprise.plugin-name-prefix",
		},
		{
			name:  "underscore prefix",
			block: "  plugin_name_prefix: agent_marketplace\n",
			field: "claude_enterprise.plugin_name_prefix",
			code:  "claude_enterprise.plugin-name-prefix",
		},
		{
			name:  "empty explicit prefix",
			block: `  plugin_name_prefix: ""` + "\n",
			field: "claude_enterprise.plugin_name_prefix",
			code:  "claude_enterprise.plugin-name-prefix",
		},
		{
			name:  "prefix longer than 16 characters",
			block: "  plugin_name_prefix: agent-marketplace\n",
			field: "claude_enterprise.plugin_name_prefix",
			code:  "claude_enterprise.plugin-name-prefix",
		},
		{
			name:  "doubled hyphen",
			block: "  plugin_name_prefix: agent--market\n",
			field: "claude_enterprise.plugin_name_prefix",
			code:  "claude_enterprise.plugin-name-prefix",
		},
		{
			name:  "leading hyphen",
			block: "  plugin_name_prefix: -agent\n",
			field: "claude_enterprise.plugin_name_prefix",
			code:  "claude_enterprise.plugin-name-prefix",
		},
		{
			name:  "trailing hyphen",
			block: "  plugin_name_prefix: agent-\n",
			field: "claude_enterprise.plugin_name_prefix",
			code:  "claude_enterprise.plugin-name-prefix",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(validConfigYAML() + "\nclaude_enterprise:\n  enabled: false\n" + test.block))
			if err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("ParseConfig error = %v, want validation failure naming %s", err, test.field)
			}
			var coded interface {
				ErrorCode() string
				FieldName() string
			}
			if !errors.As(err, &coded) || coded.ErrorCode() != test.code || coded.FieldName() != test.field {
				t.Fatalf("coded configuration error = %v", err)
			}
		})
	}
}

func TestClaudeEnterpriseRuntimeChangesAreWorkerSafe(t *testing.T) {
	tests := []struct {
		name    string
		running string
		next    string
	}{
		{
			name:    "enable",
			running: validConfigYAML(),
			next: validConfigYAML() + `
claude_enterprise:
  enabled: true
  api_key_id: apikey_AbC123
  api_key:
    env: ORIGINAL_ADMIN_KEY
`,
		},
		{
			name: "disable",
			running: validConfigYAML() + `
claude_enterprise:
  enabled: true
  api_key_id: apikey_AbC123
  api_key:
    env: ORIGINAL_ADMIN_KEY
`,
			next: validConfigYAML() + `
claude_enterprise:
  enabled: false
  api_key:
    env: ORIGINAL_ADMIN_KEY
`,
		},
		{
			name: "credential reference",
			running: validConfigYAML() + `
claude_enterprise:
  enabled: true
  api_key_id: apikey_AbC123
  api_key:
    env: ORIGINAL_ADMIN_KEY
`,
			next: validConfigYAML() + `
claude_enterprise:
  enabled: true
  api_key_id: apikey_AbC123
  api_key:
    env: ROTATED_ADMIN_KEY
`,
		},
		{
			name: "API key identifier",
			running: validConfigYAML() + `
claude_enterprise:
  enabled: true
  api_key_id: apikey_AbC123
  api_key:
    env: ORIGINAL_ADMIN_KEY
`,
			next: validConfigYAML() + `
claude_enterprise:
  enabled: true
  api_key_id: apikey_XYZ789
  api_key:
    env: ORIGINAL_ADMIN_KEY
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			running := mustParseClaudeConfig(t, test.running)
			candidate := mustParseClaudeConfig(t, test.next)
			classification, err := Classify(running, running, candidate)
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if !classification.ClaudeEnterpriseChanged {
				t.Fatalf("runtime classification does not identify Claude Enterprise change: %+v", classification)
			}
			if len(classification.RestartRequired) != 0 {
				t.Fatalf("Claude Enterprise change requires restart: %+v", classification.RestartRequired)
			}
		})
	}
}

func TestClaudeEnterpriseRuntimeReplacementAppliesAndInvalidCandidateKeepsLastGood(t *testing.T) {
	logs := captureRuntimeLogs(t)
	path := writeConfig(t, validConfigYAML())
	source, err := LoadRuntimeSource(path)
	if err != nil {
		t.Fatalf("LoadRuntimeSource: %v", err)
	}
	applied := make(chan Classification, 2)
	stop := startRuntimeSource(t, source, func(_ context.Context, _ Snapshot, _ Config, classification Classification) error {
		applied <- classification
		return nil
	})
	t.Cleanup(stop)

	valid := validConfigYAML() + `
claude_enterprise:
  enabled: true
  api_key_id: apikey_AbC123
  api_key:
    env: CLAUDE_ADMIN_KEY
`
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatalf("write valid candidate: %v", err)
	}
	select {
	case got := <-applied:
		if !got.ClaudeEnterpriseChanged {
			t.Fatalf("applied classification = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker-safe Claude Enterprise configuration was not applied")
	}
	awaitRuntimeCondition(t, func() bool { return source.Current().Version == 1 })

	invalid := strings.Replace(valid, "  enabled: true", "  enabled: true\n  plugin_name_prefix: invalid_prefix", 1)
	candidatePath := path + ".candidate"
	if err := os.WriteFile(candidatePath, []byte(invalid), 0o600); err != nil {
		t.Fatalf("write invalid candidate: %v", err)
	}
	if err := os.Rename(candidatePath, path); err != nil {
		t.Fatalf("replace config with invalid candidate: %v", err)
	}
	awaitRuntimeCondition(t, func() bool {
		logOutput := logs.String()
		return strings.Contains(logOutput, "config candidate rejected") &&
			strings.Contains(logOutput, "claude_enterprise.plugin-name-prefix")
	})
	if source.Current().Version != 1 {
		t.Fatalf("invalid candidate replaced last-good runtime configuration: version=%d", source.Current().Version)
	}
	target := source.Current().Config.ClaudeEnterprise
	if target == nil || !target.Enabled || target.APIKey.Env != "CLAUDE_ADMIN_KEY" {
		t.Fatalf("last-good Claude Enterprise configuration changed: %#v", target)
	}
}

func mustParseClaudeConfig(t *testing.T, raw string) Config {
	t.Helper()
	cfg, err := ParseConfig([]byte(raw))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	return cfg
}
