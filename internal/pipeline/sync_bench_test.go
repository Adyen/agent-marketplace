package pipeline

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// BenchmarkResolveMaterialize benchmarks the hot Resolve + materialize dry-run
// path used by sync and status with a local registry and varying sizes of the
// machine-wide target harness set.
func BenchmarkResolveMaterialize(b *testing.B) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(prev) })

	homeDir := b.TempDir()
	b.Setenv("HOME", homeDir)

	registryRoot := b.TempDir()
	benchWriteSkill(b, registryRoot, "bench-skill")

	repoRoot := b.TempDir()
	benchWriteRepoConfig(b, repoRoot, registryRoot)

	allTargets := []hcc.Target{
		hcc.TargetClaude,
		hcc.TargetFactory,
		hcc.TargetCursor,
		hcc.TargetWindsurf,
		hcc.TargetGemini,
	}

	for _, n := range []int{1, 3, 5} {
		targets := allTargets[:n]
		b.Run(fmt.Sprintf("targets=%d", n), func(b *testing.B) {
			benchWriteUserConfig(b, homeDir, targets)
			ctx := context.Background()
			resolveOpts := ResolveOptions{
				RepoRoot:    repoRoot,
				CatalogMode: catalog.LoadModeInspect,
				EffectiveOpts: config.LoadEffectiveOptions{
					RepoRoot: repoRoot,
				},
			}
			b.ReportAllocs()
			for b.Loop() {
				res := Resolve(ctx, resolveOpts)
				if len(res.Errors) > 0 {
					b.Fatalf("Resolve errors: %v", res.Errors)
				}
				_, _ = materialize.Run(ctx, repoRoot, res.Resolved, materialize.WithDryRun())
			}
		})
	}
}

func benchWriteSkill(b *testing.B, root, name string) {
	b.Helper()
	skillDir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		b.Fatalf("mkdir skill dir: %v", err)
	}
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("name: " + name + "\n")
	sb.WriteString("description: " + name + "\n")
	sb.WriteString("metadata:\n  tags:\n    - bench\n")
	sb.WriteString("---\n\n# " + name + "\n")
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(sb.String()), 0o644); err != nil {
		b.Fatalf("write skill: %v", err)
	}
}

func benchWriteRepoConfig(b *testing.B, repoRoot, registryRoot string) {
	b.Helper()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		b.Fatalf("mkdir .agents: %v", err)
	}
	content := fmt.Sprintf(`apiVersion: agent-marketplace/v2
registries:
  - url: %s
    source_harness: claude
    skills:
      - bench-skill
`, registryRoot)
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(content), 0o644); err != nil {
		b.Fatalf("write repo config: %v", err)
	}
}

func benchWriteUserConfig(b *testing.B, homeDir string, targets []hcc.Target) {
	b.Helper()
	agentsDir := filepath.Join(homeDir, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		b.Fatalf("mkdir .agents: %v", err)
	}
	var sb strings.Builder
	sb.WriteString("apiVersion: agent-marketplace/v2\nharnesses:\n")
	for _, t := range targets {
		sb.WriteString("  - " + string(t) + "\n")
	}
	sb.WriteString("registries: []\n")
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(sb.String()), 0o644); err != nil {
		b.Fatalf("write user config: %v", err)
	}
}
