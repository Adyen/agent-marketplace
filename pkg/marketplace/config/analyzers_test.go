package config

import (
	"strings"
	"testing"
)

func TestLoadConfigAnalyzersNilAndEmpty(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, validConfigYAML()))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Policy.Analyzers != nil {
		t.Fatalf("expected omitted analyzers to be nil, got %v", cfg.Policy.Analyzers)
	}

	empty := strings.Replace(validConfigYAML(), "  limits:\n", "  analyzers: []\n  limits:\n", 1)
	cfg, err = LoadConfig(writeConfig(t, empty))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Policy.Analyzers == nil {
		t.Fatal("expected explicit empty analyzers to be non-nil")
	}
	if len(cfg.Policy.Analyzers) != 0 {
		t.Fatalf("expected 0 analyzers, got %d", len(cfg.Policy.Analyzers))
	}
}

func TestLoadConfigAnalyzersBuiltInRoleAndDisable(t *testing.T) {
	analyzers := `    - id: marketplace.hidden-content
      role: advisory
    - id: marketplace.capabilities
      enabled: false`
	cfg, err := LoadConfig(writeConfig(t, withAnalyzers(analyzers)))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Policy.Analyzers) != 2 {
		t.Fatalf("got %d analyzers", len(cfg.Policy.Analyzers))
	}
	if cfg.Policy.Analyzers[0].ID != "marketplace.hidden-content" || cfg.Policy.Analyzers[0].Role != "advisory" {
		t.Fatalf("unexpected first analyzer: %+v", cfg.Policy.Analyzers[0])
	}
	if cfg.Policy.Analyzers[1].Enabled == nil || *cfg.Policy.Analyzers[1].Enabled {
		t.Fatalf("expected capabilities to be disabled: %+v", cfg.Policy.Analyzers[1].Enabled)
	}
}

func TestLoadConfigAnalyzersValidRunnerAndRulepack(t *testing.T) {
	analyzers := `    - id: marketplace.custom
      role: gating
      version: 1.0.0
      run:
        command: /usr/bin/analyzer
        timeout: 30s
        implementation_digest: ` + goodDigest() + `
    - id: marketplace.rulepack
      rulepack:
        path: /etc/rules.yaml`
	cfg, err := LoadConfig(writeConfig(t, withAnalyzers(analyzers)))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Policy.Analyzers) != 2 {
		t.Fatalf("got %d analyzers", len(cfg.Policy.Analyzers))
	}
	if cfg.Policy.Analyzers[0].Run == nil || cfg.Policy.Analyzers[0].Run.Command != "/usr/bin/analyzer" {
		t.Fatalf("unexpected runner: %+v", cfg.Policy.Analyzers[0].Run)
	}
	if cfg.Policy.Analyzers[1].Rulepack == nil || cfg.Policy.Analyzers[1].Rulepack.Path != "/etc/rules.yaml" {
		t.Fatalf("unexpected rulepack: %+v", cfg.Policy.Analyzers[1].Rulepack)
	}
}

func TestLoadConfigAnalyzersValidation(t *testing.T) {
	tests := []struct {
		name      string
		analyzers string
		wantErr   string
	}{
		{
			name:      "duplicate id",
			analyzers: `    - id: marketplace.hidden-content\n    - id: marketplace.hidden-content`,
			wantErr:   "duplicated",
		},
		{
			name:      "unsafe id uppercase",
			analyzers: `    - id: Marketplace.custom\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "not a safe dotted lower-case identifier",
		},
		{
			name:      "unknown id missing run",
			analyzers: `    - id: marketplace.unknown\n      version: 1.0.0\n      role: gating`,
			wantErr:   "requires a run block",
		},
		{
			name:      "invalid role",
			analyzers: `    - id: marketplace.custom\n      role: bad\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "must be",
		},
		{
			name:      "custom missing version",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "version is required",
		},
		{
			name:      "built-in run block",
			analyzers: `    - id: marketplace.hidden-content\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "must not declare a run block",
		},
		{
			name:      "built-in version mismatch",
			analyzers: `    - id: marketplace.hidden-content\n      version: 9.9.9`,
			wantErr:   "does not match built-in version",
		},
		{
			name:      "rulepack with run",
			analyzers: `    - id: marketplace.rulepack\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest() + `\n      rulepack:\n        path: /etc/rules.yaml`,
			wantErr:   "must not declare a run block",
		},
		{
			name:      "rulepack missing path",
			analyzers: `    - id: marketplace.rulepack\n      rulepack: {}`,
			wantErr:   "must be an absolute non-empty path",
		},
		{
			name:      "rulepack relative path",
			analyzers: `    - id: marketplace.rulepack\n      rulepack:\n        path: rules.yaml`,
			wantErr:   "must be an absolute non-empty path",
		},
		{
			name:      "runner relative command",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: analyzer\n        timeout: 1s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "must be an absolute path",
		},
		{
			name:      "runner zero timeout",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 0s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "timeout must be a positive duration",
		},
		{
			name:      "runner malformed digest",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: sha256:short`,
			wantErr:   "64 hex",
		},
		{
			name:      "runner uppercase digest",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: sha256:` + strings.Repeat("A", 64),
			wantErr:   "non-hex or uppercase",
		},
		{
			name:      "runner plain digest",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: deadbeef`,
			wantErr:   "sha256:",
		},
		{
			name:      "runner invalid env name",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        env_passthrough:\n          - 1BAD\n        implementation_digest: ` + goodDigest(),
			wantErr:   "valid environment variable name",
		},
		{
			name:      "runner duplicate env name",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        env_passthrough:\n          - FOO\n          - FOO\n        implementation_digest: ` + goodDigest(),
			wantErr:   "duplicated",
		},
		{
			name:      "inline secret literal",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        args:\n          - --token=ghp_secret\n        timeout: 1s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "inline secret literal",
		},
		{
			name:      "inline api-key secret literal",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        args:\n          - --api-key=secret\n        timeout: 1s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "inline secret literal",
		},
		{
			name:      "next-argument secret literal",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        args:\n          - --token\n          - ghp_secret\n        timeout: 1s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "followed by a secret literal",
		},
		{
			name:      "file reference positive control",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        args:\n          - --token-file=/run/secrets/token\n          - --api-key-env=API_KEY\n        timeout: 1s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "",
		},
		{
			name:      "config secret literal",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest() + `\n      config:\n        api_key: secret`,
			wantErr:   "contains a disallowed secret value",
		},
		{
			name:      "config secret file",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest() + `\n      config:\n        api_key:\n          file: /run/secrets/key`,
			wantErr:   "",
		},
		{
			name:      "config secret env",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest() + `\n      config:\n        token:\n          env: TOKEN`,
			wantErr:   "",
		},
		{
			name:      "nested config secret",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest() + `\n      config:\n        auth:\n          password: secret`,
			wantErr:   "contains a disallowed secret value",
		},
		{
			name:      "nested config secret file",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest() + `\n      config:\n        auth:\n          api_key:\n            file: /run/secrets/key`,
			wantErr:   "",
		},
		{
			name:      "disabled entry still validates",
			analyzers: `    - id: marketplace.unknown\n      enabled: false\n      version: 1.0.0`,
			wantErr:   "requires a run block",
		},
		{
			name:      "unknown yaml field",
			analyzers: `    - id: marketplace.custom\n      unknown_field: true`,
			wantErr:   "unknown",
		},
		{
			name:      "config secret file whitespace only",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest() + `\n      config:\n        api_key:\n          file: "   "`,
			wantErr:   "must be a non-empty string",
		},
		{
			name:      "config secret env whitespace only",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest() + `\n      config:\n        token:\n          env: "   "`,
			wantErr:   "must be a non-empty string",
		},
		{
			name:      "config api-key literal with punctuation",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest() + `\n      config:\n        api-key: secret`,
			wantErr:   "contains a disallowed secret value",
		},
		{
			name:      "config api-key file reference with punctuation",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest() + `\n      config:\n        api-key:\n          file: /run/secrets/key`,
			wantErr:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analyzers := strings.ReplaceAll(tt.analyzers, `\n`, "\n")
			_, err := LoadConfig(writeConfig(t, withAnalyzers(analyzers)))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func goodDigest() string {
	return "sha256:" + strings.Repeat("a", 64)
}

func TestLoadConfigAnalyzersRemediation(t *testing.T) {
	tests := []struct {
		name      string
		analyzers string
		wantErr   string
	}{
		{
			name:      "custom runner missing role",
			analyzers: `    - id: marketplace.custom\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "role must be",
		},
		{
			name:      "padded analyzer id",
			analyzers: `    - id: " marketplace.custom"\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "id must not have leading or trailing whitespace",
		},
		{
			name:      "padded analyzer role",
			analyzers: `    - id: marketplace.custom\n      role: " gating"\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "role must not have leading or trailing whitespace",
		},
		{
			name:      "padded analyzer version",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: " 1.0.0"\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "version must not have leading or trailing whitespace",
		},
		{
			name:      "padded env passthrough",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        env_passthrough:\n          - " FOO"\n        implementation_digest: ` + goodDigest(),
			wantErr:   "must not have leading or trailing whitespace",
		},
		{
			name:      "padded run command",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: " /bin/x"\n        timeout: 1s\n        implementation_digest: ` + goodDigest(),
			wantErr:   "must not have leading or trailing whitespace",
		},
		{
			name:      "padded rulepack path",
			analyzers: `    - id: marketplace.rulepack\n      rulepack:\n        path: " /etc/rules.yaml"`,
			wantErr:   "must not have leading or trailing whitespace",
		},
		{
			name:      "rulepack rejects configured version",
			analyzers: `    - id: marketplace.rulepack\n      version: 1.0.0\n      rulepack:\n        path: /etc/rules.yaml`,
			wantErr:   "is not configurable for the built-in rulepack analyzer",
		},
		{
			name:      "config list secret literal",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest() + `\n      config:\n        items:\n          - password: literal`,
			wantErr:   "contains a disallowed secret value",
		},
		{
			name:      "config list secret env reference",
			analyzers: `    - id: marketplace.custom\n      role: gating\n      version: 1.0.0\n      run:\n        command: /bin/x\n        timeout: 1s\n        implementation_digest: ` + goodDigest() + `\n      config:\n        items:\n          - name: allowed\n            token:\n              env: TOKEN`,
			wantErr:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := strings.ReplaceAll(tt.analyzers, `\n`, "\n")
			_, err := LoadConfig(writeConfig(t, withAnalyzers(block)))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestWalkAnalyzerMapDeterministic(t *testing.T) {
	m := map[string]any{
		"zed_secret": map[string]any{"file": " "},
		"api_secret": map[string]any{"env": "\t"},
		"mtd_secret": map[string]any{"file": "  "},
	}
	var first []string
	for i := 0; i < 50; i++ {
		errs := walkAnalyzerMap("policy.analyzers[0].config", m)
		if len(errs) != 3 {
			t.Fatalf("expected 3 errors, got %d: %v", len(errs), errs)
		}
		var got []string
		for _, err := range errs {
			got = append(got, err.Error())
		}
		if i == 0 {
			first = got
			continue
		}
		if len(got) != len(first) {
			t.Fatalf("iteration %d: error count changed", i)
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("iteration %d: error %d differs from first; got %q, want %q", i, j, got[j], first[j])
			}
		}
	}
}

func withAnalyzers(block string) string {
	return strings.Replace(validConfigYAML(), "  limits:\n", "  analyzers:\n"+block+"\n  limits:\n", 1)
}
