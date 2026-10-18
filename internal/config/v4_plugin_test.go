package config

import (
	"errors"
	"strings"
	"testing"
)

func v4Code(t *testing.T, err error, code string) {
	t.Helper()
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
}

func v4PluginConfig(body string) []byte {
	return []byte(`apiVersion: agent-marketplace/v4
sources:
  team-plugins:
    directory: ./plugins
    format: claude-marketplace
resources:
` + body)
}

// spec: plugin-packages/format-on-a-marketplace-source-is-refused
func TestV4FormatOnMarketplaceSourceRefused(t *testing.T) {
	_, err := ParseV4Config([]byte(`apiVersion: agent-marketplace/v4
sources:
  cloud:
    marketplace: https://market.example/api
    format: claude-marketplace
`), "/repo/.agents/config.yaml", true)
	v4Code(t, err, "v4.option-combination")
}

// spec: plugin-packages/format-combined-with-source-harness-is-refused
func TestV4FormatWithSourceHarnessRefused(t *testing.T) {
	_, err := ParseV4Config([]byte(`apiVersion: agent-marketplace/v4
sources:
  team:
    git: https://git.example/team.git
    ref: main
    source_harness: claude
    format: claude-marketplace
`), "/repo/.agents/config.yaml", true)
	v4Code(t, err, "v4.option-combination")
}

// spec: plugin-packages/unknown-format-is-refused
func TestV4UnknownFormatRefused(t *testing.T) {
	_, err := ParseV4Config([]byte(`apiVersion: agent-marketplace/v4
sources:
  team:
    directory: ./plugins
    format: made-up-format
`), "/repo/.agents/config.yaml", true)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "plugin.format.unknown" {
		t.Fatalf("error = %v, want plugin.format.unknown", err)
	}
	if !strings.Contains(validation.Path, "sources.team.format") {
		t.Fatalf("error path = %q, want sources.team.format", validation.Path)
	}
}

func TestV4FormatAcceptedOnGitAndDirectorySources(t *testing.T) {
	for _, format := range []string{"agent-plugin", "claude-plugin", "factory-plugin", "codex-plugin", "claude-marketplace", "factory-marketplace", "codex-marketplace"} {
		_, err := ParseV4Config([]byte(`apiVersion: agent-marketplace/v4
sources:
  team:
    git: https://git.example/team.git
    ref: main
    format: `+format+`
`), "/repo/.agents/config.yaml", true)
		if err != nil {
			t.Fatalf("format %q refused: %v", format, err)
		}
	}
	for _, format := range []string{"agent-plugin", "claude-marketplace"} {
		cfg, err := ParseV4Config([]byte(`apiVersion: agent-marketplace/v4
sources:
  local:
    directory: ./plugins
    format: `+format+`
`), "/repo/.agents/config.yaml", true)
		if err != nil {
			t.Fatalf("directory format %q refused: %v", format, err)
		}
		if cfg.Sources["local"].Format != format {
			t.Fatalf("source format = %q, want %q", cfg.Sources["local"].Format, format)
		}
	}
}

// spec: plugin-packages/plugin-options-are-refused-on-other-kinds
func TestV4PluginOptionsRefusedOnOtherKinds(t *testing.T) {
	for _, body := range []string{
		`  team-plugins: [skill/review: {delivery: decompose}]`,
		`  team-plugins: [skill/review: {exclude: [command/legacy]}]`,
		`  team-plugins: [collection/bundle: {delivery: native}]`,
		`  team-plugins: [collection/bundle: {exclude: [skill/x]}]`,
	} {
		_, err := ParseV4Config(v4PluginConfig(body), "/repo/.agents/config.yaml", true)
		v4Code(t, err, "v4.option-combination")
	}
}

// spec: plugin-packages/installed-name-alias-on-a-plugin-is-refused
func TestV4InstalledNameAliasOnPluginRefused(t *testing.T) {
	_, err := ParseV4Config(v4PluginConfig(`  team-plugins: [plugin/review-kit: {as: other-name}]`), "/repo/.agents/config.yaml", true)
	v4Code(t, err, "v4.option-combination")
}

func TestV4PluginLocatorAndOptionsParse(t *testing.T) {
	cfg, err := ParseV4Config(v4PluginConfig(`  team-plugins:
    - plugin/review-kit
    - plugin/lint-kit: {delivery: decompose, exclude: [command/legacy-review]}
`), "/repo/.agents/config.yaml", true)
	if err != nil {
		t.Fatalf("ParseV4Config() error = %v", err)
	}
	if len(cfg.Requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(cfg.Requests))
	}
	var lint V4ResourceRequest
	for _, r := range cfg.Requests {
		if r.Name == "lint-kit" {
			lint = r
		}
	}
	if lint.Kind != "plugin" || lint.Options.Delivery != "decompose" || len(lint.Options.Exclude) != 1 || lint.Options.Exclude[0] != "command/legacy-review" {
		t.Fatalf("plugin request options = %#v", lint.Options)
	}
	if lint.Options.HasAs || lint.Options.As != "" {
		t.Fatalf("plugin request must not gain an installed name: %#v", lint.Options)
	}
}

func TestV4PluginStructuredEntryParses(t *testing.T) {
	cfg, err := ParseV4Config(v4PluginConfig(`  team-plugins:
    - {kind: plugin, name: "1password.tools"}
    - {kind: plugin, name: "audit.kit", delivery: native, exclude: [skill/old]}
`), "/repo/.agents/config.yaml", true)
	if err != nil {
		t.Fatalf("ParseV4Config() error = %v", err)
	}
	if len(cfg.Requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(cfg.Requests))
	}
	found := map[string]V4ResourceRequest{}
	for _, r := range cfg.Requests {
		found[r.Name] = r
	}
	entry, ok := found["1password.tools"]
	if !ok || entry.Kind != "plugin" || !entry.Structured {
		t.Fatalf("structured plugin request missing: %#v", cfg.Requests)
	}
	audit := found["audit.kit"]
	if audit.Options.Delivery != "native" || len(audit.Options.Exclude) != 1 || audit.Options.Exclude[0] != "skill/old" {
		t.Fatalf("structured options = %#v", audit.Options)
	}
}

func TestV4PluginStructuredEntryNameRules(t *testing.T) {
	for _, name := range []string{"", "has/slash", "with space", "..", "with*star"} {
		_, err := ParseV4Config(v4PluginConfig(`  team-plugins:
    - {kind: plugin, name: "`+name+`"}
`), "/repo/.agents/config.yaml", true)
		if err == nil {
			t.Fatalf("structured plugin name %q was accepted", name)
		}
	}
}

func TestV4PluginRequestOnPlainSourceRefused(t *testing.T) {
	_, err := ParseV4Config([]byte(`apiVersion: agent-marketplace/v4
sources:
  plain:
    directory: ./resources
resources:
  plain: [plugin/review-kit]
`), "/repo/.agents/config.yaml", true)
	var validation *V4ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want V4ValidationError", err)
	}
}

func TestV4PluginShortLocatorNameGrammar(t *testing.T) {
	_, err := ParseV4Config(v4PluginConfig(`  team-plugins: [plugin/1password.tools]`), "/repo/.agents/config.yaml", true)
	var validation *V4ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("plugin/1password.tools should be refused (use the structured entry); got %v", err)
	}
}

func TestV4PluginDeliveryValues(t *testing.T) {
	for _, delivery := range []string{"auto", "decompose", "native"} {
		_, err := ParseV4Config(v4PluginConfig(`  team-plugins: [plugin/kit: {delivery: `+delivery+`}]`), "/repo/.agents/config.yaml", true)
		if err != nil {
			t.Fatalf("delivery %q refused: %v", delivery, err)
		}
	}
	_, err := ParseV4Config(v4PluginConfig(`  team-plugins: [plugin/kit: {delivery: sideways}]`), "/repo/.agents/config.yaml", true)
	if err == nil {
		t.Fatal("an unknown delivery value was accepted")
	}
}

func TestV4PluginExcludeLocators(t *testing.T) {
	_, err := ParseV4Config(v4PluginConfig(`  team-plugins: [plugin/kit: {exclude: [notalocator]}]`), "/repo/.agents/config.yaml", true)
	if err == nil {
		t.Fatal("a member exclusion that is not kind/name was accepted")
	}
	_, err = ParseV4Config(v4PluginConfig(`  team-plugins: [plugin/kit: {exclude: [plugin/other]}]`), "/repo/.agents/config.yaml", true)
	if err == nil {
		t.Fatal("a member exclusion naming a plugin was accepted")
	}
}

func TestV4StructuredEntryKindMustBePlugin(t *testing.T) {
	_, err := ParseV4Config(v4PluginConfig(`  team-plugins:
    - {kind: skill, name: review}
`), "/repo/.agents/config.yaml", true)
	if err == nil {
		t.Fatal("a structured entry with a non-plugin kind was accepted")
	}
}
