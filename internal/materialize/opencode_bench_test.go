package materialize

import (
	"context"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"path/filepath"
	"testing"
)

func BenchmarkOpenCodeWorkflow(b *testing.B) {
	for _, action := range []string{"sync", "check", "repair-preview"} {
		b.Run(action, func(b *testing.B) {
			discardBenchLogs(b)
			home, repo := b.TempDir(), b.TempDir()
			b.Setenv("HOME", home)
			b.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			b.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			b.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			b.Setenv("OPENCODE_CONFIG", "")
			b.Setenv("OPENCODE_CONFIG_DIR", "")
			resources := []resolver.ResolvedArtifact{openCodeResource(ir.NewMcpServer("docs", "docs-server", nil, nil, nil, ir.SourceRef{}))}
			if report, err := Run(context.Background(), repo, resources); err != nil || diag.HasErrors(report.Diags) {
				b.Fatalf("setup: %v", err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if action == "repair-preview" {
					if _, err := OpenCodeRepairPreview(context.Background(), repo, resources); err != nil {
						b.Fatal(err)
					}
				} else {
					var options []RunOption
					if action == "check" {
						options = append(options, WithDryRun())
					}
					if report, err := Run(context.Background(), repo, resources, options...); err != nil || diag.HasErrors(report.Diags) {
						b.Fatalf("run: %v", err)
					}
				}
			}
		})
	}
}
