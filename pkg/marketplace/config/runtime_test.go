package config

import (
	"strings"
	"testing"
)

// spec: marketplace-runtime-configuration/the-indexing-generation-excludes-secret-values
func TestParseConfigAndIndexingGenerationCanonicalProjection(t *testing.T) {
	original, err := ParseConfig([]byte(validConfigYAML()))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}

	reformatted := strings.Replace(validConfigYAML(), "apiVersion:", "# operator note\napiVersion:", 1)
	registry := `  - id: src-test
    slug: example-source
    aliases: []
    origin: https://github.com/example/resources.git
    ref: main
    acquisition: mirror
    publisher:
      name: Example
    license: MIT
    roots:
      - path: .agents
        source_harness: factory
`
	reorderedRegistry := `  - origin: https://github.com/example/resources.git
    ref: main
    slug: example-source
    aliases: []
    id: src-test
    publisher:
      name: Example
    license: MIT
    roots:
      - source_harness: factory
        path: .agents
`
	if !strings.Contains(reformatted, registry) {
		t.Fatal("valid config fixture no longer contains the registry block")
	}
	reformatted = strings.Replace(reformatted, registry, reorderedRegistry, 1)
	alternate, err := ParseConfig([]byte(reformatted))
	if err != nil {
		t.Fatalf("ParseConfig reformatted candidate: %v", err)
	}
	want := "semantic-v1:567c212d449a2656f9bd5f3c1840d3b42cb4c18696833078c18422051c8b6537"
	got, err := IndexingGeneration(original, original.Registries[0])
	if err != nil {
		t.Fatalf("IndexingGeneration: %v", err)
	}
	if got != want {
		t.Fatalf("IndexingGeneration = %q, want independently authored %q", got, want)
	}
	alternateGeneration, err := IndexingGeneration(alternate, alternate.Registries[0])
	if err != nil {
		t.Fatalf("IndexingGeneration alternate: %v", err)
	}
	if alternateGeneration != got {
		t.Fatalf("reformatted/default-equivalent generation = %q, want %q", alternateGeneration, got)
	}

	withSecretsA := parseRuntimeConfig(t)
	withSecretsA.Registries[0].Credentials = Credentials{
		SSHKey: &SSHKeyCredential{File: "/keys/deploy", Value: "ssh-secret-one"},
		Token:  &TokenCredential{Env: "MARKETPLACE_TOKEN", Value: "token-secret-one"},
	}
	withSecretsB := parseRuntimeConfig(t)
	withSecretsB.Registries[0].Credentials = Credentials{
		SSHKey: &SSHKeyCredential{File: "/keys/deploy", Value: "ssh-secret-two"},
		Token:  &TokenCredential{Env: "MARKETPLACE_TOKEN", Value: "token-secret-two"},
	}
	secretGenerationA, err := IndexingGeneration(withSecretsA, withSecretsA.Registries[0])
	if err != nil {
		t.Fatalf("IndexingGeneration with first secrets: %v", err)
	}
	secretGenerationB, err := IndexingGeneration(withSecretsB, withSecretsB.Registries[0])
	if err != nil {
		t.Fatalf("IndexingGeneration with second secrets: %v", err)
	}
	if secretGenerationA != secretGenerationB {
		t.Fatalf("secret-value-only change altered generation: %q != %q", secretGenerationA, secretGenerationB)
	}
}

func TestIndexingGenerationChangesForEveryProjectedInput(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{name: "origin", change: func(cfg *Config) { cfg.Registries[0].Origin += "/fork" }},
		{name: "ref", change: func(cfg *Config) { cfg.Registries[0].Ref = "release" }},
		{name: "root path", change: func(cfg *Config) { cfg.Registries[0].Roots[0].Path = "skills" }},
		{name: "source harness", change: func(cfg *Config) { cfg.Registries[0].Roots[0].SourceHarness = "claude" }},
		{name: "acquisition", change: func(cfg *Config) { cfg.Registries[0].Acquisition = "index-only" }},
		{name: "license", change: func(cfg *Config) { cfg.Registries[0].License = "Apache-2.0" }},
		{name: "CA reference", change: func(cfg *Config) { cfg.Registries[0].CAFile = "/etc/marketplace/ca.pem" }},
		{name: "SSH key reference", change: func(cfg *Config) { cfg.Registries[0].Credentials.SSHKey = &SSHKeyCredential{File: "/keys/next"} }},
		{name: "token file reference", change: func(cfg *Config) { cfg.Registries[0].Credentials.Token = &TokenCredential{File: "/run/token"} }},
		{name: "token environment reference", change: func(cfg *Config) { cfg.Registries[0].Credentials.Token = &TokenCredential{Env: "NEXT_TOKEN"} }},
		{name: "inline secret presence", change: func(cfg *Config) { cfg.Registries[0].Credentials.Token = &TokenCredential{Value: "present"} }},
		{name: "policy override", change: func(cfg *Config) { cfg.Registries[0].PolicyOverrides = map[string]any{"hidden_content": "reject"} }},
		{name: "global policy", change: func(cfg *Config) { cfg.Policy.HiddenContent = "warn" }},
		{name: "global limit", change: func(cfg *Config) { cfg.Policy.Limits.FileCount++ }},
	}
	base := parseRuntimeConfig(t)
	baseGeneration := generationForTest(t, base)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			candidate := parseRuntimeConfig(t)
			tc.change(&candidate)
			if got := generationForTest(t, candidate); got == baseGeneration {
				t.Fatalf("projected change left generation unchanged: %q", got)
			}
		})
	}

	ordered := parseRuntimeConfig(t)
	ordered.Registries[0].Roots = append(ordered.Registries[0].Roots, RegistryRoot{Path: "prompts", SourceHarness: "devin"})
	orderedGeneration := generationForTest(t, ordered)
	reversed := ordered
	reversed.Registries = append([]Registry(nil), ordered.Registries...)
	reversed.Registries[0].Roots = append([]RegistryRoot(nil), ordered.Registries[0].Roots...)
	reversed.Registries[0].Roots[0], reversed.Registries[0].Roots[1] = reversed.Registries[0].Roots[1], reversed.Registries[0].Roots[0]
	if generationForTest(t, reversed) == orderedGeneration {
		t.Fatal("changing authored registry-root order left generation unchanged")
	}
}

// spec: marketplace-runtime-configuration/a-worker-safe-change-applies-while-restart-required-changes-are-reported
// spec: marketplace-runtime-configuration/restoring-startup-values-clears-the-restart-required-report
// spec: marketplace-runtime-configuration/a-refresh-interval-change-requests-no-refresh
func TestClassifyRuntimeConfigurationChanges(t *testing.T) {
	startup := parseRuntimeConfig(t)

	t.Run("registry additions", func(t *testing.T) {
		candidate := parseRuntimeConfig(t)
		added := candidate.Registries[0]
		added.ID = "another-source"
		added.Slug = "another-source"
		candidate.Registries = append(candidate.Registries, added)
		got, err := Classify(startup, startup, candidate)
		if err != nil {
			t.Fatalf("Classify: %v", err)
		}
		if !equalStrings(got.Added, []string{"another-source"}) || len(got.Removed) != 0 {
			t.Fatalf("added/removal classification = %+v", got)
		}
	})

	t.Run("registry removal", func(t *testing.T) {
		candidate := parseRuntimeConfig(t)
		candidate.Registries = nil
		got, err := Classify(startup, startup, candidate)
		if err != nil {
			t.Fatalf("Classify: %v", err)
		}
		if !equalStrings(got.Removed, []string{"src-test"}) || len(got.Added) != 0 {
			t.Fatalf("added/removal classification = %+v", got)
		}
	})

	t.Run("indexing change", func(t *testing.T) {
		candidate := parseRuntimeConfig(t)
		candidate.Registries[0].PolicyOverrides = map[string]any{"hidden_content": "reject"}
		got, err := Classify(startup, startup, candidate)
		if err != nil {
			t.Fatalf("Classify: %v", err)
		}
		if !equalStrings(got.IndexingChanged, []string{"src-test"}) || len(got.MetadataChanged) != 0 {
			t.Fatalf("indexing/metadata classification = %+v", got)
		}
	})

	t.Run("presentation-only change", func(t *testing.T) {
		candidate := parseRuntimeConfig(t)
		candidate.Registries[0].Slug = "new-slug"
		candidate.Registries[0].Publisher.Name = "New Publisher"
		got, err := Classify(startup, startup, candidate)
		if err != nil {
			t.Fatalf("Classify: %v", err)
		}
		if !equalStrings(got.MetadataChanged, []string{"src-test"}) || len(got.IndexingChanged) != 0 {
			t.Fatalf("indexing/metadata classification = %+v", got)
		}
	})

	t.Run("refresh interval only", func(t *testing.T) {
		candidate := parseRuntimeConfig(t)
		candidate.Refresh.Interval = Duration(45 * 60 * 1e9)
		got, err := Classify(startup, startup, candidate)
		if err != nil {
			t.Fatalf("Classify: %v", err)
		}
		if !got.RefreshIntervalChanged || len(got.Added)+len(got.Removed)+len(got.IndexingChanged)+len(got.MetadataChanged) != 0 {
			t.Fatalf("refresh interval classification = %+v", got)
		}
	})

	t.Run("startup-owned field order and restore", func(t *testing.T) {
		type fieldChange struct {
			name   string
			change func(*Config)
		}
		fields := []fieldChange{
			{"listen", func(cfg *Config) { cfg.Listen = "127.0.0.1:8181" }},
			{"operations_listen", func(cfg *Config) { cfg.OperationsListen = "127.0.0.1:9191" }},
			{"storage", func(cfg *Config) { cfg.Storage.Metadata.DSN = "/tmp/other.db" }},
			{"instance", func(cfg *Config) { cfg.Instance.Name = "Other Marketplace" }},
			{"revocations", func(cfg *Config) { cfg.Revocations.RegistryIDs = []string{"src-test"} }},
			{"management", func(cfg *Config) { cfg.Management.Mode = "database" }},
			{"admin", func(cfg *Config) { cfg.Admin.Enabled = true }},
			{"catalog_projection", func(cfg *Config) { cfg.CatalogProjection.Enabled = true }},
			{"installation_metrics", func(cfg *Config) { cfg.InstallationMetrics.URL = "https://metrics.example" }},
			{"refresh_concurrency", func(cfg *Config) { cfg.RefreshConcurrency = 2 }},
		}
		combined := parseRuntimeConfig(t)
		want := make([]string, 0, len(fields))
		for _, field := range fields {
			candidate := parseRuntimeConfig(t)
			field.change(&candidate)
			got, err := Classify(startup, startup, candidate)
			if err != nil {
				t.Fatalf("Classify %s: %v", field.name, err)
			}
			if !equalStrings(got.RestartRequired, []string{field.name}) {
				t.Errorf("%s restart-required fields = %v, want [%s]", field.name, got.RestartRequired, field.name)
			}
			field.change(&combined)
			want = append(want, field.name)
		}
		got, err := Classify(startup, startup, combined)
		if err != nil {
			t.Fatalf("Classify combined restart-required fields: %v", err)
		}
		if !equalStrings(got.RestartRequired, want) {
			t.Fatalf("restart-required field order = %v, want %v", got.RestartRequired, want)
		}

		changed := parseRuntimeConfig(t)
		changed.Listen = "127.0.0.1:8181"
		first, err := Classify(startup, startup, changed)
		if err != nil || !equalStrings(first.RestartRequired, []string{"listen"}) {
			t.Fatalf("Classify changed startup field = %+v, %v", first, err)
		}
		restored, err := Classify(startup, changed, startup)
		if err != nil || len(restored.RestartRequired) != 0 {
			t.Fatalf("Classify restored startup field = %+v, %v", restored, err)
		}
	})
}

func parseRuntimeConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := ParseConfig([]byte(validConfigYAML()))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	return cfg
}

func generationForTest(t *testing.T, cfg Config) string {
	t.Helper()
	generation, err := IndexingGeneration(cfg, cfg.Registries[0])
	if err != nil {
		t.Fatalf("IndexingGeneration: %v", err)
	}
	return generation
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
