package pipeline

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"

	metricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// BenchmarkResolveInventoryWithLocalDiskAndHTTPExport includes local registry
// disk I/O and synchronized local OTLP/HTTP export. Network registry access and
// materialization are excluded.
func BenchmarkResolveInventoryWithLocalDiskAndHTTPExport(b *testing.B) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(prev) })

	repoRoot := b.TempDir()
	registryRoot := b.TempDir()
	includes := make([]string, 50)
	for i := range 50 {
		includes[i] = fmt.Sprintf("bench-skill-%02d", i)
		benchWriteSkill(b, registryRoot, includes[i])
	}
	repoCfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:           registryRoot,
		SourceHarness: "claude",
		Skills:        config.KindSelector{Include: includes},
	}}}

	exported := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/metrics" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				b.Errorf("read metrics export: %v", err)
			} else {
				var request metricspb.ExportMetricsServiceRequest
				if err := proto.Unmarshal(body, &request); err != nil {
					b.Errorf("decode metrics export: %v", err)
				} else if exportedInventorySnapshot(&request) {
					exported <- struct{}{}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	b.Cleanup(server.Close)
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	state, err := telemetry.ResolveTelemetryConfig(nil, &config.Config{Telemetry: &tmcfg.TelemetryConfig{
		Level: tmcfg.LevelDetailed, Endpoint: server.URL,
	}}, "", "")
	if err != nil {
		b.Fatalf("resolve telemetry config: %v", err)
	}
	tel, err := telemetry.New(context.Background(), state, slog.Default())
	if err != nil {
		b.Fatalf("new telemetry: %v", err)
	}
	b.Cleanup(func() { _ = tel.Shutdown(context.Background()) })
	ctx := telemetry.NewContext(context.Background(), tel, "sync", time.Now(), "", false, telemetry.Span{}, false)
	opts := ResolveOptions{
		RepoRoot:    repoRoot,
		CatalogMode: catalog.LoadModeInspect,
		Loaders: Loaders{
			Org: func() (*config.Config, error) { return nil, nil },
			Global: func() (*config.Config, error) {
				return &config.Config{Harnesses: []string{"claude", "factory", "cursor"}}, nil
			},
			Repo:  func() (*config.Config, error) { return repoCfg, nil },
			Local: func() (*config.LocalConfig, error) { return nil, nil },
		},
	}
	exportTimeout := time.NewTimer(5 * time.Second)
	if !exportTimeout.Stop() {
		<-exportTimeout.C
	}
	b.Cleanup(func() { exportTimeout.Stop() })
	b.ReportAllocs()
	for b.Loop() {
		result := Resolve(ctx, opts)
		if len(result.Errors) > 0 {
			b.Fatalf("Resolve errors: %v", result.Errors)
		}
		exportTimeout.Reset(5 * time.Second)
		select {
		case <-exported:
			if !exportTimeout.Stop() {
				<-exportTimeout.C
			}
		case <-exportTimeout.C:
			b.Fatal("timed out waiting for accepted inventory export")
		}
	}
}

func exportedInventorySnapshot(request *metricspb.ExportMetricsServiceRequest) bool {
	for _, resource := range request.ResourceMetrics {
		for _, scope := range resource.ScopeMetrics {
			for _, metric := range scope.Metrics {
				if metric.Name == string(tmcfg.InventorySnapshotName) {
					return true
				}
			}
		}
	}
	return false
}

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
