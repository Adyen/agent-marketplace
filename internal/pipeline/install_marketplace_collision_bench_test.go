package pipeline

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkMarketplaceCollisionSync(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	for _, count := range []int{1, 100} {
		b.Run(fmt.Sprintf("%d_resources", count), func(b *testing.B) {
			home, repo, source := b.TempDir(), b.TempDir(), b.TempDir()
			b.Setenv("HOME", home)
			b.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			b.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			config.SetTestOrganizationConfigPath(b, filepath.Join(home, "absent-org.yaml"))
			benchWriteUserConfig(b, home, []hcc.Target{hcc.TargetFactory})
			for i := range count {
				benchWriteSkill(b, source, fmt.Sprintf("source-%03d", i))
			}
			if err := os.MkdirAll(filepath.Join(repo, ".agents"), 0755); err != nil {
				b.Fatal(err)
			}
			cfg := fmt.Sprintf("apiVersion: agent-marketplace/v2\nregistries:\n  - url: %s\n    source_harness: claude\n    default: true\n", source)
			if err := os.WriteFile(config.ProjectConfigPath(repo), []byte(cfg), 0644); err != nil {
				b.Fatal(err)
			}
			opts := ResolveOptions{Workspace: mustWorkspace(b, repo), CatalogMode: catalog.LoadModeCached, NoBackup: true, marketplaceInstall: &marketplace.PreparedInstall{Pin: config.MarketplaceResourcePin{Name: "marketplace-only", Kind: "skill"}, Resolved: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "marketplace-only", Kind: ir.KindSkill}}}}
			first := Sync(context.Background(), opts)
			if len(first.Errors) != 0 || len(first.Resolved) != count || first.Report == nil {
				b.Fatalf("invalid sync fixture: resolved=%d errors=%v", len(first.Resolved), first.Errors)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result := Sync(context.Background(), opts)
				if len(result.Errors) != 0 || result.Report == nil || result.Report.TotalWritten() != 0 {
					b.Fatalf("warm sync changed output or failed: %v", result.Errors)
				}
			}
		})
	}
}
