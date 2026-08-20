package telemetry

import (
	"context"
	"path/filepath"
	"testing"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
)

func BenchmarkGetStatusInfo(b *testing.B) {
	defer benchDiscardLogs()()

	benchDir := b.TempDir()
	b.Setenv("HOME", benchDir)
	b.Setenv("XDG_STATE_HOME", benchDir)
	b.Setenv("XDG_CONFIG_HOME", benchDir)

	// Redirect the organization config path so the benchmark does not touch
	// the host /etc directory. A missing file is treated as unmanaged.
	missingOrg := filepath.Join(benchDir, "missing-org-config.yaml")
	amconfig.SetTestV3OrganizationConfigPath(b, missingOrg)

	// Isolate from any host telemetry env overrides.
	b.Setenv(EnvEndpoint, "")
	b.Setenv(EnvLevel, "")
	b.Setenv(EnvCommunityReporting, "")

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := GetStatusInfo(ctx); err != nil {
			b.Fatalf("GetStatusInfo: %v", err)
		}
	}
}
