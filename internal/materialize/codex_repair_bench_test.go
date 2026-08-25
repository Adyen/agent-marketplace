package materialize

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func codexBenchRepairFixture(b *testing.B, n int) (string, []resolver.ResolvedArtifact) {
	b.Helper()
	discardBenchLogs(b)
	repoRoot := b.TempDir()
	b.Setenv("HOME", b.TempDir())
	b.Setenv("XDG_STATE_HOME", filepath.Join(b.TempDir(), "state"))
	b.Setenv("XDG_CACHE_HOME", filepath.Join(b.TempDir(), "cache"))

	selection := make([]resolver.ResolvedArtifact, 0, n)
	for i := range n {
		name := fmt.Sprintf("bench-mcp-%03d", i)
		selection = append(selection, codexRepairResolved(
			name,
			ir.NewMcpServer(name, name+"-binary", []string{"--flag"}, map[string]string{"K": "V"}, nil, ir.SourceRef{}),
			config.RepoCommitted,
		))
	}
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		b.Fatal(err)
	}
	root, err := ownership.CanonicalRoot(filepath.Join(repoRoot, ".codex"))
	if err != nil {
		b.Fatal(err)
	}
	store, err := ownership.Open(root)
	if err != nil {
		b.Fatal(err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		b.Fatal(err)
	}
	return repoRoot, selection
}

func BenchmarkCodexRepairPreview(b *testing.B) {
	for _, n := range []int{8, 64} {
		b.Run(fmt.Sprintf("%d_entries", n), func(b *testing.B) {
			repoRoot, selection := codexBenchRepairFixture(b, n)
			ctx := context.Background()
			b.ReportAllocs()
			for b.Loop() {
				preview, err := CodexRepairPreview(ctx, repoRoot, selection)
				if err != nil {
					b.Fatal(err)
				}
				if !preview.Actionable() {
					b.Fatal("benchmark fixture produced no repair candidates")
				}
			}
		})
	}
}
