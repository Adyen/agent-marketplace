package materialize

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func BenchmarkNativeMCPSyncDisk(b *testing.B) {
	discardBenchLogs(b)
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetGemini} {
		b.Run(string(target), func(b *testing.B) {
			home, repo := b.TempDir(), b.TempDir()
			b.Setenv("HOME", home)
			b.Setenv("XDG_STATE_HOME", b.TempDir())
			b.Setenv("XDG_CACHE_HOME", b.TempDir())
			b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			resources := nativeSourceBackedMCPResources(b, target, config.UserPersonal)
			if _, err := Run(context.Background(), repo, resources); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := Run(context.Background(), repo, resources); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
