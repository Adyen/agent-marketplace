package materialize

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func BenchmarkNativeAggregateStaleCheckDisk(b *testing.B) {
	discardBenchLogs(b)
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetGemini} {
		b.Run(string(target), func(b *testing.B) {
			home, repo := b.TempDir(), b.TempDir()
			b.Setenv("HOME", home)
			b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			b.Setenv("XDG_STATE_HOME", b.TempDir())
			b.Setenv("XDG_CACHE_HOME", b.TempDir())
			desired := nativeMCPResources(target, config.UserPersonal)
			if target == hcc.TargetGemini {
				desired = desired[2:]
			}
			installed, err := Run(context.Background(), repo, desired)
			if err != nil || diag.HasErrors(installed.Diags) {
				b.Fatalf("native install: %+v %v", installed, err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				report, err := Run(context.Background(), repo, nil, WithDryRun())
				if err != nil || diag.HasErrors(report.Diags) || report.InSync() {
					b.Fatalf("stale native check: %+v %v", report, err)
				}
			}
		})
	}
}
