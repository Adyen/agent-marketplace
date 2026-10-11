package config

import (
	"strings"
	"testing"
)

// spec: marketplace-api/metrics-configuration-and-requests-preserve-the-server-boundary
func TestLoadConfigInstallationMetrics(t *testing.T) {
	block := func(inner string) string {
		return validConfigYAML() + "\ninstallation_metrics:\n" + inner
	}

	tests := []struct {
		name      string
		yaml      string
		wantValid bool
	}{
		{name: "absent disabled", yaml: validConfigYAML(), wantValid: true},
		{name: "empty disabled", yaml: block("  {}\n"), wantValid: true},
		{name: "https with env bearer and labels", yaml: block(`  url: https://metrics.internal/prom
  bearer_token_env: MARKETPLACE_METRICS_TOKEN
  labels:
    namespace: marketplace
    cluster: prod
`), wantValid: true},
		{name: "http endpoint without bearer", yaml: block("  url: http://metrics.internal:9090\n"), wantValid: true},
		{name: "http loopback with bearer", yaml: block(`  url: http://127.0.0.1:9090
  bearer_token_env: MARKETPLACE_METRICS_TOKEN
`), wantValid: true},
		{name: "http localhost with bearer", yaml: block(`  url: http://localhost:9090
  bearer_token_env: MARKETPLACE_METRICS_TOKEN
`), wantValid: true},
		{name: "labels without url", yaml: block(`  labels:
    namespace: marketplace
`), wantValid: false},
		{name: "bearer without url", yaml: block("  bearer_token_env: MARKETPLACE_METRICS_TOKEN\n"), wantValid: false},
		{name: "malformed url", yaml: block("  url: \"http://[::1\"\n"), wantValid: false},
		{name: "unsupported scheme", yaml: block("  url: ftp://metrics.internal\n"), wantValid: false},
		{name: "url without host", yaml: block("  url: \"https:///path\"\n"), wantValid: false},
		{name: "url with userinfo", yaml: block("  url: https://user:secret@metrics.internal\n"), wantValid: false},
		{name: "url with query", yaml: block("  url: https://metrics.internal/prom?token=abc\n"), wantValid: false},
		{name: "url with fragment", yaml: block("  url: https://metrics.internal/prom#frag\n"), wantValid: false},
		{name: "remote http with bearer", yaml: block(`  url: http://metrics.internal
  bearer_token_env: MARKETPLACE_METRICS_TOKEN
`), wantValid: false},
		{name: "bearer env not an identifier", yaml: block(`  url: https://metrics.internal
  bearer_token_env: 1BAD-NAME
`), wantValid: false},
		{name: "unsupported label key", yaml: block(`  url: https://metrics.internal
  labels:
    pod: marketplace
`), wantValid: false},
		{name: "label value with control character", yaml: block("  url: https://metrics.internal\n  labels:\n    namespace: \"market\\nplace\"\n"), wantValid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadConfig(writeConfig(t, test.yaml))
			if test.wantValid {
				if err != nil {
					t.Fatalf("LoadConfig() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("LoadConfig() error = nil, want invalid installation_metrics")
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "token=abc") {
				t.Fatalf("error leaks credential material: %v", err)
			}
		})
	}
	cfg, err := LoadConfig(writeConfig(t, validConfigYAML()))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.InstallationMetrics.URL != "" || cfg.InstallationMetrics.BearerTokenEnv != "" || len(cfg.InstallationMetrics.Labels) != 0 {
		t.Fatalf("installation_metrics should default to disabled, got %+v", cfg.InstallationMetrics)
	}
}

// spec: marketplace-api/metrics-configuration-and-requests-preserve-the-server-boundary
func TestInstallationMetricsValidateDeterministicDiagnostics(t *testing.T) {
	metrics := InstallationMetrics{
		URL: "https://metrics.internal",
		Labels: map[string]string{
			"pod":       "a",
			"namespace": strings.Repeat("x", 300),
			"container": "b",
			"cluster":   "prod",
		},
	}
	want := metrics.Validate()
	if len(want) < 3 {
		t.Fatalf("Validate() = %v, want several label errors", want)
	}
	for i := 0; i < 20; i++ {
		got := metrics.Validate()
		if len(got) != len(want) {
			t.Fatalf("Validate() count = %d, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i].Error() != want[i].Error() {
				t.Fatalf("Validate() error %d = %q, want %q", i, got[i], want[i])
			}
		}
	}
	if want[0].Error() != `installation_metrics.labels key "container" is not a supported deployment selector` {
		t.Fatalf("first error = %q, want sorted label order", want[0])
	}
}
