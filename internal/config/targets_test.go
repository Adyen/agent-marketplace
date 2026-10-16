package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestValidateRejectsUnknownHarnessTargetsAtEveryConfigSurface(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV2,
		Harnesses:  []string{"factory", "devni"},
		Registries: []RegistryEntry{{
			URL:           "git@example.com:registry.git",
			Ref:           "main",
			Harnesses:     []string{"cursor", "curser"},
			SourceHarness: "claud",
		}},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected unknown harness values to fail validation")
	}
	for _, want := range []string{"registries[0].harnesses[1]", "registries[0].source_harness", "curser", "claud"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("validation error = %q, want it to mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "devni") {
		t.Errorf("readable validation must not reject authored top-level identifiers, got %q", err)
	}
}

func TestValidateAcceptsCodexAtEveryConfigSurface(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV2,
		Harnesses:  []string{"codex"},
		Registries: []RegistryEntry{{
			URL:           "git@example.com:registry.git",
			Ref:           "main",
			Harnesses:     []string{"codex"},
			SourceHarness: "codex",
		}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Codex config rejected: %v", err)
	}
}

// spec: config-layering/existing-unsupported-identifier-is-retained-without-materialization-authority
func TestLoadConfigRetainsUnknownHarnessTarget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("GlobalConfigPath: %v", err)
	}
	data := []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory, devni]\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatalf("LoadConfigFromPath rejected an authored identifier: %v", err)
	}
	if !equalStrings(cfg.Harnesses, []string{"factory", "devni"}) {
		t.Fatalf("harnesses = %v, want [factory devni]", cfg.Harnesses)
	}
}

func TestValidateWithoutHarnessesRejectsUnknownRegistryHarnessTargets(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV2,
		Registries: []RegistryEntry{{
			URL:           "git@example.com:registry.git",
			Ref:           "main",
			Harnesses:     []string{"windsurf", "windurf"},
			SourceHarness: "gemin",
		}},
	}
	if err := cfg.ValidateWithoutHarnesses(); err == nil {
		t.Fatal("expected project config validation to reject unknown registry targets")
	}
}

func TestValidateRejectsWhitespaceAroundKnownHarnessTargets(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV2,
		Harnesses:  []string{"cursor"},
		Registries: []RegistryEntry{{
			URL:           "git@example.com:registry.git",
			Ref:           "main",
			Harnesses:     []string{" factory "},
			SourceHarness: " claude ",
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("direct validation should reject non-canonical target values")
	}
}

func TestLoadConfigCanonicalizesHarnessTargets(t *testing.T) {
	// The user-personal (global) layer, because top-level harnesses is the
	// subject here and only that layer may declare it.
	t.Setenv("HOME", t.TempDir())
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("GlobalConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data := []byte(`apiVersion: agent-marketplace/v2
harnesses: [" cursor "]
registries:
  - url: git@example.com:registry.git
    ref: main
    harnesses: [" factory "]
    source_harness: " claude "
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if got := cfg.Harnesses; len(got) != 1 || got[0] != "cursor" {
		t.Fatalf("config harnesses = %#v, want [cursor]", got)
	}
	if got := cfg.Registries[0]; got.Harnesses[0] != "factory" || got.SourceHarness != "claude" {
		t.Fatalf("registry targets were not canonicalized: %#v", got)
	}
}

func TestLoadLocalConfigCanonicalizesHarnessTargets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.local.yaml")
	data := []byte(`registries:
  - url: ./registry
    harnesses: [" factory "]
    source_harness: " claude "
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write local config: %v", err)
	}

	cfg, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatalf("LoadLocalConfigFromPath: %v", err)
	}
	if got := cfg.Registries[0]; got.Harnesses[0] != "factory" || got.SourceHarness != "claude" {
		t.Fatalf("local registry targets were not canonicalized: %#v", got)
	}
}

func TestLocalConfigValidationRejectsUnknownSourceHarness(t *testing.T) {
	cfg := LocalConfig{Registries: []RegistryEntry{{
		URL:           "./registry",
		SourceHarness: "claud",
	}}}
	if err := cfg.ValidateBasic(); err == nil {
		t.Fatal("expected local config validation to reject unknown source_harness")
	}
}

func TestValidateAcceptsEveryDeclaredTarget(t *testing.T) {
	if err := (Config{
		APIVersion: APIVersionV2,
		Harnesses:  targetmeta.CompilerNames(),
	}).Validate(); err != nil {
		t.Fatalf("declared target registry should produce valid config: %v", err)
	}
}

// TestV3LoadersCanonicalizeBeforeValidating guards the ordering bug found when
// main's harness-target validation met the v3 loading boundary: the v3 paths
// decoded and validated without ever calling the normalizer, so a target
// written with stray whitespace was reported as an unknown harness instead of
// being trimmed. Validation must never see a non-canonical target.
func TestV3LoadersCanonicalizeBeforeValidating(t *testing.T) {
	v3 := []byte(`apiVersion: agent-marketplace/v3
harnesses:
  - " cursor "
registries:
  - url: ./registry
    source_harness: " claude "
    harnesses:
      - " factory "
`)
	cfg, errs := loadV3Config(v3, userLayer, true)
	if len(errs) > 0 {
		t.Fatalf("loadV3Config rejected padded targets instead of trimming them: %v", errs)
	}
	if got := cfg.Harnesses[0]; got != "cursor" {
		t.Errorf("harnesses[0] = %q, want %q", got, "cursor")
	}
	if got := cfg.Registries[0].Harnesses[0]; got != "factory" {
		t.Errorf("registries[0].harnesses[0] = %q, want %q", got, "factory")
	}
	if got := cfg.Registries[0].SourceHarness; got != "claude" {
		t.Errorf("registries[0].source_harness = %q, want %q", got, "claude")
	}

	local := []byte(`apiVersion: agent-marketplace/v3
registries:
  - url: ./registry
    source_harness: " claude "
    harnesses:
      - " factory "
`)
	lc, lerrs := loadV3LocalConfig(local)
	if len(lerrs) > 0 {
		t.Fatalf("loadV3LocalConfig rejected padded targets instead of trimming them: %v", lerrs)
	}
	if got := lc.Registries[0].Harnesses[0]; got != "factory" {
		t.Errorf("local registries[0].harnesses[0] = %q, want %q", got, "factory")
	}
	if got := lc.Registries[0].SourceHarness; got != "claude" {
		t.Errorf("local registries[0].source_harness = %q, want %q", got, "claude")
	}
}
