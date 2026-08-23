package config

import (
	"strings"
	"testing"
)

func TestV3OrganizationRejectsUnknownFields(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v3
harnesses:
  - factory
unknown_field: value
registries: []
`)
	_, errs := loadV3Config(data, organizationLayer, false)
	if len(errs) == 0 {
		t.Fatalf("expected unknown field to be rejected")
	}
	if !strings.Contains(errs[0].Error(), "unknown top-level field \"unknown_field\"") {
		t.Fatalf("expected error to mention unknown_field, got: %v", errs[0])
	}
}

func TestV3MarketplaceRejectsDevelopmentAliasesEvenWithOtherConfigErrors(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v2
harnesses:
  - factory
unknown_field: value
marketplaces:
  - url: https://market.example
    instance_id: market
    resources:
      - source_id: source
        kind: skill
        name: review
        revision: revision
        digest: sha256:0000000000000000000000000000000000000000000000000000000000000000
        acquisition: index
        revision_commit: legacy-commit
`)
	_, errs := loadV3Config(data, userLayer, true)
	if len(errs) == 0 {
		t.Fatal("expected invalid Marketplace aliases to be rejected")
	}
	combined := errorsJoin(errs)
	for _, want := range []string{"revision_commit", "acquisition"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("expected error to mention %q, got: %v", want, combined)
		}
	}
}

func TestV3UserRejectsForbiddenFields(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v3
harnesses:
  - factory
identity:
  provider: ldap
registries: []
`)
	_, errs := loadV3Config(data, userLayer, true)
	if len(errs) == 0 {
		t.Fatalf("expected forbidden identity field to be rejected")
	}
	if !strings.Contains(errs[0].Error(), "identity") {
		t.Fatalf("expected error to mention identity, got: %v", errs[0])
	}
}

func TestV3RepoAllowsHarnessesAndRejectsBackup(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v3
harnesses:
  - factory
backup:
  enabled: true
registries: []
`)
	_, errs := loadV3Config(data, repoLayer, false)
	if len(errs) != 1 {
		t.Fatalf("expected one forbidden field error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errorsJoin(errs), "backup") {
		t.Fatalf("expected error to mention backup, got: %v", errs)
	}
}

func TestV3LocalAllowsRequiredAndDefault(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v3
registries:
  - url: git@example.com:repo.git
    ref: main
    required: true
    default: true
`)
	_, errs := loadV3LocalConfig(data)
	if len(errs) != 0 {
		t.Fatalf("expected local config to accept required and default, got errors: %v", errs)
	}
}

func TestV3LocalRejectsAllowedRegistries(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v3
allowed_registries: []
registries:
  - url: git@example.com:repo.git
    ref: main
`)
	_, errs := loadV3LocalConfig(data)
	if len(errs) != 1 {
		t.Fatalf("expected 1 forbidden field error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "allowed_registries") {
		t.Fatalf("expected error to mention allowed_registries, got: %v", errs[0])
	}
}

func TestV3AnyLayerRejectsIdentityAndTeamProfiles(t *testing.T) {
	for _, kind := range []layerKind{organizationLayer, userLayer, repoLayer, localLayer} {
		data := []byte(`apiVersion: agent-marketplace/v3
team_profiles:
  platform:
    registries: []
identity:
  provider: ldap
`)
		if kind == localLayer {
			_, errs := loadV3LocalConfig(data)
			if len(errs) != 2 {
				t.Fatalf("kind %v: expected 2 forbidden field errors, got %d: %v", kind, len(errs), errs)
			}
			continue
		}
		_, errs := loadV3Config(data, kind, kind != repoLayer)
		if len(errs) != 2 {
			t.Fatalf("kind %v: expected 2 forbidden field errors, got %d: %v", kind, len(errs), errs)
		}
	}
}

func TestV3RegistryEntryRejectsForbiddenLocalRequired(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v3
registries:
  - url: git@example.com:repo.git
    ref: main
    forbidden_entry_field: value
`)
	_, errs := loadV3Config(data, userLayer, true)
	if len(errs) == 0 {
		t.Fatalf("expected unknown registry entry field to be rejected")
	}
	if !strings.Contains(errs[0].Error(), "forbidden_entry_field") {
		t.Fatalf("expected error to mention forbidden_entry_field, got: %v", errs[0])
	}
}

func TestV3OrganizationAllowsFallbackHarnesses(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v3
harnesses:
  - factory
  - windsurf
registries: []
`)
	cfg, errs := loadV3Config(data, organizationLayer, false)
	if len(errs) > 0 {
		t.Fatalf("expected valid organization config, got errors: %v", errs)
	}
	if cfg == nil {
		t.Fatalf("expected parsed config")
	}
	if len(cfg.Harnesses) != 2 {
		t.Fatalf("expected 2 harnesses, got %v", cfg.Harnesses)
	}
}

func TestV3UserRequiresHarnesses(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v3
registries: []
`)
	_, errs := loadV3Config(data, userLayer, true)
	if len(errs) == 0 {
		t.Fatalf("expected user config to require harnesses")
	}
	if !strings.Contains(errs[0].Error(), "harnesses is required") {
		t.Fatalf("expected harnesses required error, got: %v", errs[0])
	}
}

func TestV3RepoAllowedRegistriesClaimsPolicy(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v3
allowed_registries: []
registries:
  - url: git@example.com:repo.git
    ref: main
`)
	cfg, errs := loadV3Config(data, repoLayer, false)
	if len(errs) > 0 {
		t.Fatalf("expected valid repo config, got errors: %v", errs)
	}
	if cfg == nil || cfg.AllowedRegistries == nil || len(*cfg.AllowedRegistries) != 0 {
		t.Fatalf("expected explicit empty allowed_registries to be preserved")
	}
}

func TestV3LocalLoadsValidConfig(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v3
registries:
  - url: git@example.com:repo.git
    ref: main
skills:
  include:
    - local-skill
`)
	cfg, errs := loadV3LocalConfig(data)
	if len(errs) > 0 {
		t.Fatalf("expected valid repository personal config, got: %v", errorsJoin(errs))
	}
	if cfg == nil {
		t.Fatal("expected parsed config")
	}
	if len(cfg.Registries) != 1 || cfg.Registries[0].URL != "git@example.com:repo.git" {
		t.Fatalf("expected one registry, got %v", cfg.Registries)
	}
	if len(cfg.Skills.Include) != 1 || cfg.Skills.Include[0] != "local-skill" {
		t.Fatalf("expected local skill include, got %v", cfg.Skills.Include)
	}
}

func TestV3LocalAllowsAliasedRequiredAndDefault(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v3
registries:
  - &entry
    url: git@example.com:repo.git
    ref: main
    required: true
    default: true
  - *entry
`)
	_, errs := loadV3LocalConfig(data)
	if len(errs) != 0 {
		t.Fatalf("expected local config to accept aliased required and default, got errors: %v", errs)
	}
}

func errorsJoin(errs []error) string {
	var parts []string
	for _, e := range errs {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "; ")
}
