package config

import (
	"errors"
	"testing"
)

func TestV4LayerFieldsAndUnknownInput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  layerKind
		field string
		code  string
	}{
		{"organization unknown", organizationLayer, "unknown_field: value", "v4.unknown-field"},
		{"user identity", userLayer, "identity: {provider: ldap}", "v4.unknown-field"},
		{"repo backup", repoLayer, "backup: {enabled: true}", "v4.layer-field"},
		{"repo logging", repoLayer, "logging: {level: debug}", "v4.layer-field"},
		{"local allowlist", localLayer, "allowed_registries: []", "v4.layer-field"},
		{"local harnesses", localLayer, "harnesses: [factory]", "v4.layer-field"},
		{"source unknown", userLayer, "sources: {agents: {git: https://example.test/agents.git, ref: main, forbidden_entry_field: value}}", "v4.unknown-field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readRootLayerIntent("authored.yaml", []byte("apiVersion: agent-marketplace/v4\n"+tc.field+"\n"), tc.kind)
			var validation *V4ValidationError
			if !errors.As(err, &validation) || validation.Code != tc.code {
				t.Fatalf("error = %v, want %s", err, tc.code)
			}
		})
	}
	for _, kind := range []layerKind{organizationLayer, userLayer, repoLayer, localLayer} {
		for _, field := range []string{"identity", "team_profiles"} {
			_, err := readRootLayerIntent("authored.yaml", []byte("apiVersion: agent-marketplace/v4\n"+field+": {}\n"), kind)
			var validation *V4ValidationError
			if !errors.As(err, &validation) || validation.Code != "v4.unknown-field" || validation.Path != field {
				t.Fatalf("%s %s error = %v", kind.name(), field, err)
			}
		}
	}
}

func TestV4LayerPolicyControls(t *testing.T) {
	org, err := readRootLayerIntent("org.yaml", []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory, windsurf]\n"), organizationLayer)
	if err != nil || !equalStrings(org.Settings().Harnesses, []string{"factory", "windsurf"}) {
		t.Fatalf("organization fallback harnesses = %v, %v", org.Settings().Harnesses, err)
	}
	repo, err := readRootLayerIntent("repo.yaml", []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nallowed_registries: []\n"), repoLayer)
	if err != nil || repo.Settings().AllowedRegistries == nil || len(*repo.Settings().AllowedRegistries) != 0 {
		t.Fatalf("repository policy = %#v, %v", repo.Settings(), err)
	}
	local, err := readRootLayerIntent("local.yaml", []byte(`apiVersion: agent-marketplace/v4
sources: {agents: {git: https://example.test/agents.git, ref: main}}
resources: {agents: {include: [skill/local-skill], required: true, all: true}}
`), localLayer)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := local.bind(nil)
	if err != nil || len(cfg.Registries) != 1 || cfg.Registries[0].URL != "https://example.test/agents.git" || !cfg.Registries[0].Required || !cfg.Registries[0].V4All || !equalStrings(cfg.Registries[0].Skills.Include, []string{"local-skill"}) {
		t.Fatalf("local selection/policy = %#v, %v", cfg, err)
	}
}

func TestV4YAMLAliasRefused(t *testing.T) {
	_, err := readRootLayerIntent("local.yaml", []byte(`apiVersion: agent-marketplace/v4
sources:
  first: &source {git: https://example.test/agents.git, ref: main}
  second: *source
resources: {first: {all: true, required: true}}
`), localLayer)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.alias-forbidden" {
		t.Fatalf("error = %v, want alias refusal", err)
	}
}
