package materialize

import (
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// Includes filesystem root admission and physical-location inspection.
func BenchmarkClaudeDestinationPreflight(b *testing.B) {
	repo, home := b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	for _, custom := range []string{"", filepath.Join(home, ".claude-account")} {
		b.Run(map[bool]string{false: "default", true: "relocated"}[custom != ""], func(b *testing.B) {
			b.Setenv("CLAUDE_CONFIG_DIR", custom)
			result := compilePhaseResult{allOutputs: map[outputKey]bool{
				{target: hcc.TargetClaude}:                     true,
				{target: hcc.TargetClaude, machineScope: true}: true,
			}}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if d := preflightClaudeDestinations(&result, repo); d != nil {
					b.Fatal(d.Message)
				}
			}
		})
	}
}
