package telemetry

import (
	"testing"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

func BenchmarkResolveTelemetryConfig(b *testing.B) {
	defer benchDiscardLogs()()

	b.Setenv("XDG_STATE_HOME", b.TempDir())
	org := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:              tmcfg.LevelBasic,
			Endpoint:           "https://org.example.com",
			RetentionPolicy:    "https://org.example.com/retention",
			CommunityReporting: boolPtr(true),
		},
	}
	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:              tmcfg.LevelDetailed,
			Endpoint:           "https://user.example.com",
			CommunityReporting: boolPtr(false),
		},
	}
	projectEndpoint := "https://project.example.com"
	projectRetention := "https://project.example.com/retention"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ResolveTelemetryConfig(org, user, projectEndpoint, projectRetention); err != nil {
			b.Fatal(err)
		}
	}
}
