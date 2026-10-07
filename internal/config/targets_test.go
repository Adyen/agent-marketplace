package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestValidateRejectsUnknownHarnessTargetsAtEveryConfigSurface(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV4,
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
		APIVersion: APIVersionV4,
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
	data := []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory, devni]\n")
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
		APIVersion: APIVersionV4,
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
		APIVersion: APIVersionV4,
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

func TestLoadConfigNormalizesSourceHarnessAndRetainsTargets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	mustMkdirAll(t, filepath.Dir(path))
	mustWriteFile(t, path, []byte(`apiVersion: agent-marketplace/v4
harnesses: [cursor]
sources:
  registry:
    git: git@example.com:registry.git
    ref: main
    source_harness: " claude "
resources:
  registry: {harnesses: [factory]}
`))
	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if !equalStrings(cfg.Harnesses, []string{"cursor"}) || len(cfg.Registries) != 1 {
		t.Fatalf("runtime settings = %#v", cfg)
	}
	if got := cfg.Registries[0]; !equalStrings(got.Harnesses, []string{"factory"}) || got.SourceHarness != "claude" {
		t.Fatalf("source/group targets = %#v", got)
	}
}

func TestLoadLocalConfigNormalizesSourceHarnessAndRetainsTargets(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.local.yaml")
	mustWriteFile(t, path, []byte(`apiVersion: agent-marketplace/v4
sources:
  registry:
    directory: ./registry
    source_harness: " claude "
resources:
  registry: {harnesses: [factory]}
`))
	cfg, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatalf("LoadLocalConfigFromPath: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("local registries = %#v", cfg.Registries)
	}
	if got := cfg.Registries[0]; !equalStrings(got.Harnesses, []string{"factory"}) || got.SourceHarness != "claude" || got.URL != filepath.Join(dir, "registry") {
		t.Fatalf("local directory/source/group targets = %#v", got)
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
		APIVersion: APIVersionV4,
		Harnesses:  targetmeta.CompilerNames(),
	}).Validate(); err != nil {
		t.Fatalf("declared target registry should produce valid config: %v", err)
	}
}

func TestV4LoadersRejectPaddedTargetListsWithCanonicalControls(t *testing.T) {
	for _, tc := range []struct {
		name, file, rootTargets, groupTargets, code string
	}{
		{"root targets", "config.yaml", "[\" cursor \"]", "[factory]", "v4.harness"},
		{"group targets", "config.yaml", "[cursor]", "[\" factory \"]", "v4.harness"},
		{"local group targets", "config.local.yaml", "", "[\" factory \"]", "v4.harness"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.file)
			input := "apiVersion: agent-marketplace/v4\n"
			if tc.rootTargets != "" {
				input += "harnesses: " + tc.rootTargets + "\n"
			}
			input += "sources:\n  registry: {git: \"git@example.com:registry.git\", ref: main, source_harness: \" claude \"}\nresources:\n  registry:\n    include: [skill/example]\n    harnesses: " + tc.groupTargets + "\n"
			mustWriteFile(t, path, []byte(input))
			_, err := LoadConfigFromPath(path)
			var validation *V4ValidationError
			if !errors.As(err, &validation) || validation.Code != tc.code {
				t.Fatalf("padded targets error = %v, want %s", err, tc.code)
			}
			canonical := strings.ReplaceAll(strings.ReplaceAll(input, "\" cursor \"", "cursor"), "\" factory \"", "factory")
			mustWriteFile(t, path, []byte(canonical))
			cfg, err := LoadConfigFromPath(path)
			if err != nil || cfg == nil || len(cfg.Registries) != 1 {
				t.Fatalf("canonical control: cfg=%#v, err=%v", cfg, err)
			}
			entry := cfg.Registries[0]
			if !equalStrings(entry.Harnesses, []string{"factory"}) || entry.SourceHarness != "claude" || !equalStrings(entry.Skills.Include, []string{"example"}) {
				t.Fatalf("canonical control lost target/selection fields: %#v", entry)
			}
		})
	}
}
