package materialize

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// `doctor --repair` is interactive, so its preview is on a user-visible latency
// path and earns a benchmark the first time it is written, per the repository's
// performance policy.

// benchRepairFixture syncs n MCP servers into a Devin project root and then
// drops the durable ownership record, which is the worst realistic case for the
// preview: EVERY existing entry has to be classified, matched against the
// desired set, and checked against retained evidence.
func benchRepairFixture(b *testing.B, n int) (repoRoot string, selection []resolver.ResolvedArtifact) {
	b.Helper()
	discardBenchLogs(b)
	repoRoot = b.TempDir()
	b.Setenv("HOME", b.TempDir())
	b.Setenv("XDG_STATE_HOME", filepath.Join(b.TempDir(), "state"))
	b.Setenv("XDG_CACHE_HOME", filepath.Join(b.TempDir(), "cache"))

	selection = make([]resolver.ResolvedArtifact, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("bench-mcp-%03d", i)
		selection = append(selection, resolver.ResolvedArtifact{
			Entry:     catalog.Entry{Name: name, Resource: ir.NewMcpServer(name, name+"-binary", []string{"--flag"}, map[string]string{"K": "V"}, nil, ir.SourceRef{})},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetDevin},
		})
	}
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		b.Fatal(err)
	}
	store, err := ownership.Open(devinProjectRoot(repoRoot))
	if err != nil {
		b.Fatal(err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		b.Fatal(err)
	}
	return repoRoot, selection
}

func BenchmarkDevinRepairPreview(b *testing.B) {
	for _, n := range []int{8, 64} {
		b.Run(fmt.Sprintf("%d_entries", n), func(b *testing.B) {
			repoRoot, selection := benchRepairFixture(b, n)
			ctx := context.Background()

			b.ReportAllocs()
			for b.Loop() {
				preview, err := DevinRepairPreview(ctx, repoRoot, selection)
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
