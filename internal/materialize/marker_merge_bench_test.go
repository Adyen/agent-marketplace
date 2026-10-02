package materialize

import (
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

func BenchmarkMarkerAliasMCPSyncDisk(b *testing.B) {
	discardBenchLogs(b)
	home, repo := b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	b.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	b.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	root := filepath.Join(repo, ".factory")
	if err := os.Mkdir(root, 0o755); err != nil {
		b.Fatal(err)
	}
	target, leaf := filepath.Join(repo, "handwritten.json"), filepath.Join(root, "mcp.json")
	if err := os.WriteFile(target, []byte(`{"mcpServers":{"manual":{"command":"inert"}}}`), 0o640); err != nil {
		b.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "safe-server", Resource: ir.NewMcpServer("safe-server", "inert-command", nil, nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := os.Remove(leaf); err != nil && !os.IsNotExist(err) {
			b.Fatal(err)
		}
		if err := os.Symlink(target, leaf); err != nil {
			b.Fatal(err)
		}
		report, err := Run(b.Context(), repo, resolved)
		if err != nil {
			b.Fatal(err)
		}
		for _, d := range report.Diags {
			if d.Severity == diag.SeverityError {
				b.Fatalf("native merge refusal: %+v", d)
			}
		}
	}
}
