package materialize

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// BenchmarkLinkedResourceCheckDiskIONoNetwork includes installed-cache and
// source-support reads; registry acquisition and network I/O are excluded.
func BenchmarkLinkedResourceCheckDiskIONoNetwork(b *testing.B) {
	for _, supportFiles := range []int{0, 16} {
		b.Run(fmt.Sprintf("support=%d", supportFiles), func(b *testing.B) {
			discardBenchLogs(b)
			home, repo, source := b.TempDir(), b.TempDir(), b.TempDir()
			b.Setenv("HOME", home)
			b.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			b.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			for i := 0; i < supportFiles; i++ {
				if err := os.WriteFile(filepath.Join(source, fmt.Sprintf("support-%02d.txt", i)), []byte("inert support content"), 0o644); err != nil {
					b.Fatal(err)
				}
			}
			resolved := []resolver.ResolvedArtifact{{
				Entry:  catalog.Entry{Kind: ir.KindSkill, Name: "check", Path: source, Resource: ir.NewSkill("check", "Check benchmark", "body", nil, nil, ir.SourceRef{})},
				Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetClaude},
			}}
			if report, err := Run(b.Context(), repo, resolved); err != nil || diag.HasErrors(report.Diags) || report.TotalWritten() == 0 {
				b.Fatalf("install: %+v %v", report, err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if report, err := Run(b.Context(), repo, resolved, WithDryRun()); err != nil || !report.InSync() {
					b.Fatalf("check: %+v %v", report, err)
				}
			}
		})
	}
}
