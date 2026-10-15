package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// spec: config-layering/v4-root-rejects-legacy-installation-keys
func TestV4RootRejectsLegacyInstallationKeys(t *testing.T) {
	_, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nregistries: []\n"))
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.legacy-key" {
		t.Fatalf("error = %v, want legacy-key diagnostic", err)
	}
}

func TestV4SelectorsAndPoliciesRemainEffective(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeIncludeConfig(t, path, `apiVersion: agent-marketplace/v4
harnesses: [claude, factory]
allowed_registries: ["^https://"]
sources:
  agents: {git: https://example.test/agents.git, ref: main, path: skills, source_harness: claude}
resources:
  agents:
    required: true
    harnesses: [claude]
    tags: [platform]
    include: [skill/review]
    exclude: [skill/old, command/*]
`)
	cfg, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	source, group := cfg.Sources["agents"], cfg.Groups["agents"]
	if source.Git != "https://example.test/agents.git" || source.Ref != "main" || source.Path != "skills" || source.SourceHarness != "claude" {
		t.Fatalf("source = %#v", source)
	}
	if group.Required == nil || !*group.Required || len(group.Include) != 1 || group.Include[0].Locator != "skill/review" || !slices.Equal(group.Exclude, []string{"skill/old", "command/*"}) || !slices.Equal(group.Tags, []string{"platform"}) || !slices.Equal(group.Harnesses, []string{"claude"}) {
		t.Fatalf("group policy = %#v", group)
	}
	loaded, err := LoadConfigFromPath(path)
	if err != nil || loaded.APIVersion != APIVersionV4 || len(loaded.Registries) != 1 || !slices.Equal(loaded.Registries[0].Skills.Include, []string{"review"}) || !loaded.Registries[0].Commands.ExcludeAll {
		t.Fatalf("runtime policy = %#v %v", loaded, err)
	}
}

func TestV4EveryResourceKindRemainsSelected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeIncludeConfig(t, path, `apiVersion: agent-marketplace/v4
sources: {agents: {git: https://example.test/agents.git, ref: main}}
resources:
  agents:
    include: [skill/skill-new, command/command-new, subagent/subagent-new, hook/hook-new, mcp/mcp-new, collection/collection-new]
    exclude: [skill/skill-old, command/command-old, subagent/subagent-old, hook/hook-old, mcp/mcp-old, collection/collection-old]
`)
	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, kind := range AllKinds {
		want := []string{"skill", "command", "subagent", "hook", "mcp", "collection"}[i]
		selector := cfg.Registries[0].SelectorForKind(kind)
		if !slices.Equal(selector.Include, []string{want + "-new"}) || !slices.Equal(selector.Exclude, []string{want + "-old"}) {
			t.Fatalf("%s selector = %#v", kind, selector)
		}
	}
}

func TestV4HundredSourcesRetainAuthoredOrderAndPolicies(t *testing.T) {
	var input strings.Builder
	input.WriteString("apiVersion: agent-marketplace/v4\nsources:\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&input, "  source-%03d: {git: https://example.test/source-%03d.git, ref: main}\n", i, i)
	}
	input.WriteString("resources:\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&input, "  source-%03d: {include: [skill/skill-%03d], required: %t, all: %t}\n", i, i, i%2 == 0, i%3 == 0)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeIncludeConfig(t, path, input.String())
	cfg, err := LoadConfigFromPath(path)
	if err != nil || len(cfg.Registries) != 100 {
		t.Fatalf("hundred-source config = %#v %v", cfg, err)
	}
	for i, entry := range cfg.Registries {
		if entry.URL != fmt.Sprintf("https://example.test/source-%03d.git", i) || entry.Required != (i%2 == 0) || entry.V4All != (i%3 == 0) || !slices.Equal(entry.Skills.Include, []string{fmt.Sprintf("skill-%03d", i)}) {
			t.Fatalf("source[%d] = %#v", i, entry)
		}
	}
}

func TestV4HarnessDefaultsStayAtRootAndTelemetryIsOptIn(t *testing.T) {
	for _, telemetry := range []string{"", "telemetry: {level: basic}\n"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		writeIncludeConfig(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"+telemetry+`sources:
  agents: {git: https://example.test/agents.git, ref: main}
  security: {git: https://example.test/security.git, ref: main}
resources:
  agents: [skill/review]
  security: {include: [skill/audit], harnesses: [claude]}
`)
		cfg, err := LoadV4ConfigFromPath(path)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(cfg.Harnesses, []string{"factory"}) || cfg.Groups["agents"].HasHarnesses || cfg.Groups["agents"].Harnesses != nil || !cfg.Groups["security"].HasHarnesses || !slices.Equal(cfg.Groups["security"].Harnesses, []string{"claude"}) {
			t.Fatalf("harness defaults = %#v", cfg)
		}
		if telemetry == "" {
			if cfg.Telemetry != nil {
				t.Fatal("unconfigured telemetry was fabricated")
			}
		} else if cfg.Telemetry == nil || cfg.Telemetry.Level != "basic" {
			t.Fatalf("telemetry lost: %#v", cfg.Telemetry)
		}
	}
}
