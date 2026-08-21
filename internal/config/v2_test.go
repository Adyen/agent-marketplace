package config

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// --- v2 schema tests ---

func TestV2ConfigUnmarshalWithApiVersion(t *testing.T) {
	const input = `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
  - cursor
registries:
  - url: git@gitlab.example.com:example-org/example-infra-skills.git
    ref: main
    source_harness: claude
    skills:
      - vm-lifecycle
    commands:
      - onboard-vm
    subagents: []
    hooks: []
    mcp: []
    tags:
      - k8s
    harnesses:
      - factory
skills:
  include:
    - puppet-module-conventions
  exclude:
    - deprecated-skill
tags:
  - network
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("expected v2 config to unmarshal: %v", err)
	}

	if cfg.APIVersion != "agent-marketplace/v2" {
		t.Fatalf("expected apiVersion %q, got %q", "agent-marketplace/v2", cfg.APIVersion)
	}

	if !equalStrings(cfg.Harnesses, []string{"factory", "cursor"}) {
		t.Fatalf("expected harnesses [factory, cursor], got %v", cfg.Harnesses)
	}

	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
	}

	e := cfg.Registries[0]
	if e.SourceHarness != "claude" {
		t.Fatalf("expected source_harness %q, got %q", "claude", e.SourceHarness)
	}
	if !equalStrings(e.Skills.Include, []string{"vm-lifecycle"}) {
		t.Fatalf("expected skills [vm-lifecycle], got %v", e.Skills.Include)
	}
	if !equalStrings(e.Commands.Include, []string{"onboard-vm"}) {
		t.Fatalf("expected commands [onboard-vm], got %v", e.Commands.Include)
	}
	if !equalStrings(e.Harnesses, []string{"factory"}) {
		t.Fatalf("expected entry harnesses [factory], got %v", e.Harnesses)
	}
}

func TestV2ConfigValidateRejectsMissingApiVersion(t *testing.T) {
	cfg := Config{
		Harnesses: []string{"factory"},
		Registries: []RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error for missing apiVersion")
	}
	if !strings.Contains(err.Error(), "apiVersion") {
		t.Fatalf("expected error to mention apiVersion, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "agent-marketplace/v2") {
		t.Fatalf("expected error to suggest v2 apiVersion, got %q", err.Error())
	}
}

func TestV2ConfigValidateRejectsWrongApiVersion(t *testing.T) {
	cfg := Config{
		APIVersion: "agent-marketplace/v1",
		Harnesses:  []string{"factory"},
		Registries: []RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error for wrong apiVersion")
	}
	if !strings.Contains(err.Error(), "apiVersion") {
		t.Fatalf("expected error to mention apiVersion, got %q", err.Error())
	}
}

func TestV2ConfigValidateRejectsMissingHarnesses(t *testing.T) {
	cfg := Config{
		APIVersion: "agent-marketplace/v2",
		Registries: []RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error for missing harnesses")
	}
	if !strings.Contains(err.Error(), "harnesses") {
		t.Fatalf("expected error to mention harnesses, got %q", err.Error())
	}
}

func TestV2ConfigValidateRejectsEmptyHarnesses(t *testing.T) {
	cfg := Config{
		APIVersion: "agent-marketplace/v2",
		Harnesses:  []string{},
		Registries: []RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error for empty harnesses")
	}
}

func TestV2ConfigValidateAcceptsValidV2(t *testing.T) {
	cfg := Config{
		APIVersion: "agent-marketplace/v2",
		Harnesses:  []string{"factory"},
		Registries: []RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main", Skills: KindSelector{Include: []string{"vm-lifecycle"}, Exclude: []string{"deprecated-skill"}}},
		},
		Tags: []string{"network"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid v2 config to pass validation: %v", err)
	}
}

func TestV2RegistryEntrySourceHarnessDefaultsEmpty(t *testing.T) {
	const input = `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra.git
    ref: main
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("expected v2 config to unmarshal: %v", err)
	}
	if cfg.Registries[0].SourceHarness != "" {
		t.Fatalf("expected empty source_harness, got %q", cfg.Registries[0].SourceHarness)
	}
}

func TestV2RegistryEntryHarnessesOverride(t *testing.T) {
	const input = `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
  - cursor
registries:
  - url: git@example.com:infra.git
    ref: main
    harnesses:
      - factory
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("expected v2 config to unmarshal: %v", err)
	}
	if !equalStrings(cfg.Registries[0].Harnesses, []string{"factory"}) {
		t.Fatalf("expected per-entry harnesses [factory], got %v", cfg.Registries[0].Harnesses)
	}
}

func TestV2RegistryEntryReservedKindsAreParsed(t *testing.T) {
	const input = `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra.git
    ref: main
    skills:
      - vm-lifecycle
    commands:
      - deploy
    subagents:
      - reviewer
    hooks:
      - pre-commit
    mcp:
      - k8s-api
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("expected v2 config with reserved kinds to unmarshal: %v", err)
	}
	e := cfg.Registries[0]
	if !equalStrings(e.Skills.Include, []string{"vm-lifecycle"}) {
		t.Fatalf("expected skills [vm-lifecycle], got %v", e.Skills.Include)
	}
	if !equalStrings(e.Commands.Include, []string{"deploy"}) {
		t.Fatalf("expected commands [deploy], got %v", e.Commands.Include)
	}
	if !equalStrings(e.Subagents.Include, []string{"reviewer"}) {
		t.Fatalf("expected subagents [reviewer], got %v", e.Subagents.Include)
	}
	if !equalStrings(e.Hooks.Include, []string{"pre-commit"}) {
		t.Fatalf("expected hooks [pre-commit], got %v", e.Hooks.Include)
	}
	if !equalStrings(e.MCP.Include, []string{"k8s-api"}) {
		t.Fatalf("expected mcp [k8s-api], got %v", e.MCP.Include)
	}
}

func TestV2RoundTripPreservesNewFields(t *testing.T) {
	original := Config{
		APIVersion: "agent-marketplace/v2",
		Harnesses:  []string{"factory", "cursor"},
		Registries: []RegistryEntry{
			{
				URL:           "git@gitlab.example.com:example-org/example-infra-skills.git",
				Ref:           "main",
				SourceHarness: "claude",
				Skills:        KindSelector{Include: []string{"vm-lifecycle"}, Exclude: []string{"firewall-rule-lifecycle"}},
				Commands:      KindSelector{Include: []string{"onboard-vm"}},
				Subagents:     KindSelector{Include: []string{"reviewer"}},
				Hooks:         KindSelector{Include: []string{"pre-commit"}},
				MCP:           KindSelector{Include: []string{"k8s-api"}},
				Harnesses:     []string{"factory"},
				Tags:          []string{"k8s"},
			},
		},
		Tags: []string{"network"},
	}

	data, err := yaml.Marshal(original)
	if err != nil {
		t.Fatalf("expected v2 config to marshal: %v", err)
	}

	var roundTripped Config
	if err := yaml.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("expected marshaled v2 config to unmarshal: %v", err)
	}

	if roundTripped.APIVersion != original.APIVersion {
		t.Fatalf("expected apiVersion %q, got %q", original.APIVersion, roundTripped.APIVersion)
	}
	if !equalStrings(roundTripped.Harnesses, original.Harnesses) {
		t.Fatalf("expected harnesses %v, got %v", original.Harnesses, roundTripped.Harnesses)
	}
	if roundTripped.Registries[0].SourceHarness != original.Registries[0].SourceHarness {
		t.Fatalf("expected source_harness %q, got %q", original.Registries[0].SourceHarness, roundTripped.Registries[0].SourceHarness)
	}
	if !equalStrings(roundTripped.Registries[0].Commands.Include, original.Registries[0].Commands.Include) {
		t.Fatalf("expected commands %v, got %v", original.Registries[0].Commands.Include, roundTripped.Registries[0].Commands.Include)
	}
	if !equalStrings(roundTripped.Registries[0].Harnesses, original.Registries[0].Harnesses) {
		t.Fatalf("expected per-entry harnesses %v, got %v", original.Registries[0].Harnesses, roundTripped.Registries[0].Harnesses)
	}
}

// --- Loader v2 gating tests ---

func TestLoadConfigRejectsV1Config(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + "/.agents/config.yaml"
	mustMkdirAll(t, dir+"/.agents")
	mustWriteFile(t, configPath, []byte(`
registries:
  - url: git@example.com:infra.git
    ref: main
skills:
  - vm-lifecycle
`))

	_, err := loadConfig(configPath, true)
	if err == nil {
		t.Fatalf("expected v1 config to be rejected by loader")
	}
	if !strings.Contains(err.Error(), "apiVersion") {
		t.Fatalf("expected error to mention apiVersion, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "agent-marketplace/v2") {
		t.Fatalf("expected error to suggest v2 apiVersion, got %q", err.Error())
	}
}

func TestLoadConfigAcceptsV2Config(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + "/.agents/config.yaml"
	mustMkdirAll(t, dir+"/.agents")
	mustWriteFile(t, configPath, []byte(`
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra.git
    ref: main
skills:
  - vm-lifecycle
`))

	cfg, err := loadConfig(configPath, true)
	if err != nil {
		t.Fatalf("expected v2 config to load: %v", err)
	}
	if cfg.APIVersion != "agent-marketplace/v2" {
		t.Fatalf("expected apiVersion %q, got %q", "agent-marketplace/v2", cfg.APIVersion)
	}
}

func TestLoadV2NonOrganizationRejectsRequiredRegistry(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:repo.git
    ref: main
    required: true
`)

	for _, kind := range []layerKind{userLayer, repoLayer} {
		t.Run(kind.name(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			mustWriteFile(t, path, data)
			_, err := loadV3OrV2Config(path, kind, false)
			if err == nil {
				t.Fatalf("expected required registry to be rejected in %s config", kind.name())
			}
			if !strings.Contains(err.Error(), "only allowed in organization configuration") {
				t.Fatalf("expected authority error, got %v", err)
			}
		})
	}
}

func TestLoadLocalConfigRejectsRequiredRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	mustWriteFile(t, path, []byte(`registries:
  - url: git@example.com:repo.git
    ref: main
    required: true
`))

	_, err := loadLocalConfig(path)
	if err == nil {
		t.Fatal("expected required registry to be rejected in repository personal config")
	}
	if !strings.Contains(err.Error(), "only allowed in organization configuration") {
		t.Fatalf("expected authority error, got %v", err)
	}
}

func TestLoadLocalConfigDoesNotRequireApiVersion(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + "/config.local.yaml"
	mustWriteFile(t, configPath, []byte(`
skills:
  include:
    - local-skill
  exclude:
    - unwanted
`))

	cfg, err := loadLocalConfig(configPath)
	if err != nil {
		t.Fatalf("expected local config to load without apiVersion: %v", err)
	}
	if !equalStrings(cfg.Skills.Include, []string{"local-skill"}) {
		t.Fatalf("expected skills [local-skill], got %v", cfg.Skills.Include)
	}
	if !equalStrings(cfg.Skills.Exclude, []string{"unwanted"}) {
		t.Fatalf("expected skills exclude [unwanted], got %v", cfg.Skills.Exclude)
	}
}

// --- Merge with new fields ---

func TestMergeResolvesHarnessesInheritance(t *testing.T) {
	global := &Config{
		APIVersion: "agent-marketplace/v2",
		Harnesses:  []string{"factory", "cursor"},
		Registries: []RegistryEntry{
			{URL: "git@example.com:global.git", Ref: "main"},
			{URL: "git@example.com:override.git", Ref: "main", Harnesses: []string{"factory"}},
		},
	}

	repo := &Config{
		APIVersion: "agent-marketplace/v2",
		Harnesses:  []string{"factory"},
		Registries: []RegistryEntry{
			{URL: "git@example.com:repo.git", Ref: "stable"},
		},
	}

	got := Merge(global, repo)

	// Global entry inherits from global's Harnesses.
	if !equalStrings(got.Registries[0].Harnesses, []string{"factory", "cursor"}) {
		t.Fatalf("expected global entry to inherit top-level harnesses, got %v", got.Registries[0].Harnesses)
	}
	// Global entry with override uses its own.
	if !equalStrings(got.Registries[1].Harnesses, []string{"factory"}) {
		t.Fatalf("expected global entry with override to use per-entry harnesses, got %v", got.Registries[1].Harnesses)
	}
	// Repo entry inherits from repo's Harnesses.
	if !equalStrings(got.Registries[2].Harnesses, []string{"factory"}) {
		t.Fatalf("expected repo entry to inherit repo's top-level harnesses, got %v", got.Registries[2].Harnesses)
	}
}

func TestMergeResolvesPerKindInheritance(t *testing.T) {
	global := &Config{
		APIVersion: "agent-marketplace/v2",
		Harnesses:  []string{"factory"},
		Skills:     KindSelector{Include: []string{"global-skill"}, Exclude: []string{"legacy-skill"}},
		Commands:   KindSelector{Include: []string{"global-cmd"}},
		Registries: []RegistryEntry{
			{URL: "git@example.com:inherit.git", Ref: "main"},
			{URL: "git@example.com:override.git", Ref: "main", Skills: KindSelector{Include: []string{"entry-skill"}}, Commands: KindSelector{Include: []string{"entry-cmd"}}},
			{URL: "git@example.com:explicit-empty.git", Ref: "main", Skills: KindSelector{Include: []string{}}},
		},
	}

	got := Merge(global, nil)

	if !equalStrings(got.Registries[0].Skills.Include, []string{"global-skill"}) {
		t.Fatalf("expected inherited skills, got %v", got.Registries[0].Skills.Include)
	}
	if !equalStrings(got.Registries[0].Skills.Exclude, []string{"legacy-skill"}) {
		t.Fatalf("expected inherited excludes, got %v", got.Registries[0].Skills.Exclude)
	}
	if !equalStrings(got.Registries[0].Commands.Include, []string{"global-cmd"}) {
		t.Fatalf("expected inherited commands, got %v", got.Registries[0].Commands.Include)
	}
	if !equalStrings(got.Registries[1].Skills.Include, []string{"entry-skill"}) {
		t.Fatalf("expected overridden skills, got %v", got.Registries[1].Skills.Include)
	}
	if !equalStrings(got.Registries[1].Skills.Exclude, []string{"legacy-skill"}) {
		t.Fatalf("expected override entry to inherit top-level excludes, got %v", got.Registries[1].Skills.Exclude)
	}
	if !equalStrings(got.Registries[1].Commands.Include, []string{"entry-cmd"}) {
		t.Fatalf("expected overridden commands, got %v", got.Registries[1].Commands.Include)
	}
	if got.Registries[2].Skills.Include == nil || len(got.Registries[2].Skills.Include) != 0 {
		t.Fatalf("expected explicit empty include to override inherited defaults, got %#v", got.Registries[2].Skills.Include)
	}
}

// --- helper ---

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- harnesses-optional for project/local configs ---

func TestValidateWithoutHarnessesAcceptsMissingHarnesses(t *testing.T) {
	cfg := Config{
		APIVersion: "agent-marketplace/v2",
		Registries: []RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main"},
		},
	}
	if err := cfg.ValidateWithoutHarnesses(); err != nil {
		t.Fatalf("expected v2 config without harnesses to pass ValidateWithoutHarnesses: %v", err)
	}
}

func TestValidateWithoutHarnessesAcceptsEmptyHarnesses(t *testing.T) {
	cfg := Config{
		APIVersion: "agent-marketplace/v2",
		Harnesses:  []string{},
		Registries: []RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main"},
		},
	}
	if err := cfg.ValidateWithoutHarnesses(); err != nil {
		t.Fatalf("expected v2 config with empty harnesses to pass ValidateWithoutHarnesses: %v", err)
	}
}

func TestValidateWithoutHarnessesStillRequiresApiVersion(t *testing.T) {
	cfg := Config{
		Harnesses: []string{"factory"},
		Registries: []RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main"},
		},
	}
	err := cfg.ValidateWithoutHarnesses()
	if err == nil {
		t.Fatalf("expected validation error for missing apiVersion even without harnesses requirement")
	}
	if !strings.Contains(err.Error(), "apiVersion") {
		t.Fatalf("expected error to mention apiVersion, got %q", err.Error())
	}
}

func TestValidateStillRequiresHarnessesForGlobal(t *testing.T) {
	cfg := Config{
		APIVersion: "agent-marketplace/v2",
		Registries: []RegistryEntry{
			{URL: "git@example.com:infra.git", Ref: "main"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected Validate (global) to reject missing harnesses")
	}
	if !strings.Contains(err.Error(), "harnesses") {
		t.Fatalf("expected error to mention harnesses, got %q", err.Error())
	}
}

func TestLoadConfigRepoLayerAcceptsMissingHarnesses(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + "/.agents/config.yaml"
	mustMkdirAll(t, dir+"/.agents")
	mustWriteFile(t, configPath, []byte(`
apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
`))

	cfg, err := loadConfig(configPath, false)
	if err != nil {
		t.Fatalf("expected repo-level config without harnesses to load: %v", err)
	}
	if cfg.APIVersion != "agent-marketplace/v2" {
		t.Fatalf("expected apiVersion %q, got %q", "agent-marketplace/v2", cfg.APIVersion)
	}
	if len(cfg.Harnesses) != 0 {
		t.Fatalf("expected empty harnesses, got %v", cfg.Harnesses)
	}
}

func TestLoadConfigGlobalLayerRejectsMissingHarnesses(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + "/config.yaml"
	mustWriteFile(t, configPath, []byte(`
apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
`))

	_, err := loadConfig(configPath, true)
	if err == nil {
		t.Fatalf("expected global-level config without harnesses to be rejected")
	}
	if !strings.Contains(err.Error(), "harnesses") {
		t.Fatalf("expected error to mention harnesses, got %q", err.Error())
	}
}

// --- LocalConfig Registries YAML parsing ---

func TestLoadLocalConfigWithRegistriesParsesRegistries(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + "/config.local.yaml"
	mustWriteFile(t, configPath, []byte(`
registries:
  - url: git@example.com:local-skills.git
    ref: main
    tags:
      - security
  - url: /local/path
    skills:
      include:
        - local-skill
`))

	cfg, err := LoadLocalConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("expected local config with registries to load: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Registries) != 2 {
		t.Fatalf("expected 2 registries, got %d", len(cfg.Registries))
	}
	if cfg.Registries[0].URL != "git@example.com:local-skills.git" {
		t.Fatalf("expected registry URL %q, got %q", "git@example.com:local-skills.git", cfg.Registries[0].URL)
	}
	if cfg.Registries[0].Ref != "main" {
		t.Fatalf("expected ref %q, got %q", "main", cfg.Registries[0].Ref)
	}
	if !equalStrings(cfg.Registries[0].Tags, []string{"security"}) {
		t.Fatalf("expected tags [security], got %v", cfg.Registries[0].Tags)
	}
	if cfg.Registries[1].URL != "/local/path" {
		t.Fatalf("expected second registry URL %q, got %q", "/local/path", cfg.Registries[1].URL)
	}
	if !equalStrings(cfg.Registries[1].Skills.Include, []string{"local-skill"}) {
		t.Fatalf("expected skills [local-skill], got %v", cfg.Registries[1].Skills.Include)
	}
}

func TestLoadLocalConfigWithoutRegistriesStillWorks(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + "/config.local.yaml"
	mustWriteFile(t, configPath, []byte(`
skills:
  include:
    - local-skill
  exclude:
    - opt-out
commands:
  include:
    - local-cmd
`))

	cfg, err := LoadLocalConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("expected local config without registries to load: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Registries) != 0 {
		t.Fatalf("expected 0 registries, got %d", len(cfg.Registries))
	}
	if !equalStrings(cfg.Skills.Include, []string{"local-skill"}) {
		t.Fatalf("expected skills include [local-skill], got %v", cfg.Skills.Include)
	}
	if !equalStrings(cfg.Skills.Exclude, []string{"opt-out"}) {
		t.Fatalf("expected skills exclude [opt-out], got %v", cfg.Skills.Exclude)
	}
	if !equalStrings(cfg.Commands.Include, []string{"local-cmd"}) {
		t.Fatalf("expected commands include [local-cmd], got %v", cfg.Commands.Include)
	}
}

func TestLoadLocalConfigWithPerKindSelectorsInRegistryEntry(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + "/config.local.yaml"
	mustWriteFile(t, configPath, []byte(`
registries:
  - url: git@example.com:local-skills.git
    ref: main
    skills:
      exclude:
        - unwanted-skill
      include:
        - needed-skill
    commands:
      include:
        - deploy-cmd
    subagents:
      exclude:
        - restricted-subagent
    hooks:
      include:
        - local-hook
    mcp:
      exclude:
        - blocked-mcp
    collections:
      include:
        - local-collection
`))

	cfg, err := LoadLocalConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("expected local config with per-kind selectors to load: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
	}

	e := cfg.Registries[0]
	if !equalStrings(e.Skills.Exclude, []string{"unwanted-skill"}) {
		t.Fatalf("expected Skills.Exclude [unwanted-skill], got %v", e.Skills.Exclude)
	}
	if !equalStrings(e.Skills.Include, []string{"needed-skill"}) {
		t.Fatalf("expected Skills.Include [needed-skill], got %v", e.Skills.Include)
	}
	if !equalStrings(e.Commands.Include, []string{"deploy-cmd"}) {
		t.Fatalf("expected Commands.Include [deploy-cmd], got %v", e.Commands.Include)
	}
	if !equalStrings(e.Subagents.Exclude, []string{"restricted-subagent"}) {
		t.Fatalf("expected Subagents.Exclude [restricted-subagent], got %v", e.Subagents.Exclude)
	}
	if !equalStrings(e.Hooks.Include, []string{"local-hook"}) {
		t.Fatalf("expected Hooks.Include [local-hook], got %v", e.Hooks.Include)
	}
	if !equalStrings(e.MCP.Exclude, []string{"blocked-mcp"}) {
		t.Fatalf("expected MCP.Exclude [blocked-mcp], got %v", e.MCP.Exclude)
	}
	if !equalStrings(e.Collections.Include, []string{"local-collection"}) {
		t.Fatalf("expected Collections.Include [local-collection], got %v", e.Collections.Include)
	}
}

func TestLoadLocalConfigFromPathStrictParsingRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + "/config.local.yaml"
	mustWriteFile(t, configPath, []byte(`
skills:
  exclude: [opt-out]
harnesses: [factory]
`))

	_, err := LoadLocalConfigFromPath(configPath)
	if err == nil {
		t.Fatalf("expected strict parse error for unknown key 'harnesses'")
	}
	if !strings.Contains(err.Error(), "harnesses") {
		t.Fatalf("expected error to mention unknown key 'harnesses', got %v", err)
	}
}

func TestYAMLUnmarshal_LocalConfigWithRegistries(t *testing.T) {
	input := `
registries:
  - url: git@example.com:strict.git
    ref: main
    default: true
    tags:
      - k8s
`
	var cfg LocalConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("expected YAML to unmarshal: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
	}
	if cfg.Registries[0].URL != "git@example.com:strict.git" {
		t.Fatalf("expected URL %q, got %q", "git@example.com:strict.git", cfg.Registries[0].URL)
	}
	if !cfg.Registries[0].Default {
		t.Fatalf("expected Default to be true")
	}
	if !equalStrings(cfg.Registries[0].Tags, []string{"k8s"}) {
		t.Fatalf("expected tags [k8s], got %v", cfg.Registries[0].Tags)
	}
}

func TestYAMLUnmarshal_LocalConfigWithLegacyListKindSelector(t *testing.T) {
	input := `
registries:
  - url: git@example.com:list-selector.git
    ref: main
    skills:
      - vm-lifecycle
      - k8s-agent
`
	var cfg LocalConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("expected legacy list form to unmarshal: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
	}
	if !equalStrings(cfg.Registries[0].Skills.Include, []string{"vm-lifecycle", "k8s-agent"}) {
		t.Fatalf("expected skills [vm-lifecycle, k8s-agent], got %v", cfg.Registries[0].Skills.Include)
	}
}
