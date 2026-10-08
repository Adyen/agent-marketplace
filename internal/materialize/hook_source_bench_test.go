package materialize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkRunHookSupportDisk(b *testing.B) {
	discardBenchLogs(b)
	home, repo, source := b.TempDir(), b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	b.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	b.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.WriteFile(filepath.Join(source, "helper.txt"), []byte("inert support fixture"), 0o755); err != nil {
		b.Fatal(err)
	}
	hook := ir.NewHookWithMatcher("support-probe", "PostToolUse", "Edit", "inert hook fixture", "command", nil, ir.SourceRef{File: filepath.Join(source, "hooks.json")})
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "support-probe", Kind: ir.KindHook, Resource: hook, Path: filepath.Join(source, "hooks.json")}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	if _, err := Run(b.Context(), repo, resolved); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := Run(b.Context(), repo, resolved); err != nil {
			b.Fatal(err)
		}
	}
}
