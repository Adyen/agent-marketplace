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

func BenchmarkRunPortableSourceCheck(b *testing.B) {
	discardBenchLogs(b)
	home, repo, source := b.TempDir(), b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	b.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	b.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("inert source"), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "reference.txt"), []byte("inert support"), 0o755); err != nil {
		b.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "hash-probe", Path: source, Resource: ir.NewSkill("hash-probe", "hash control", "inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		report, err := Run(b.Context(), repo, resolved, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}), WithDryRun())
		if err != nil || hasDiagCode(report.Diags, "materialize.write.error") {
			b.Fatalf("native source check failed: %+v %v", report, err)
		}
	}
}
