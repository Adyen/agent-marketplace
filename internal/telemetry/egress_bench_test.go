package telemetry

import (
	"testing"

	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

func BenchmarkTelemetryEndpointOverride(b *testing.B) {
	defer benchDiscardLogs()()
	for _, endpoint := range []string{"https://configured.example.invalid", "https://override.example.invalid"} {
		b.Run(endpoint, func(b *testing.B) {
			b.Setenv(EnvEndpoint, endpoint)
			user := &tmcfg.TelemetryConfig{Endpoint: "https://configured.example.invalid", Headers: map[string]tmcfg.HeaderSource{"X-API-Key": {Env: "SYNTHETIC_TELEMETRY_KEY"}}}
			b.ReportAllocs()
			for b.Loop() {
				got, err := applyEnvOverrides(user, &tmcfg.TelemetryConfig{}, false)
				if err != nil || got.Endpoint != endpoint {
					b.Fatalf("override failed: %v", err)
				}
			}
		})
	}
}
