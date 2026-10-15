package materialize

import (
	"context"
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

func BenchmarkNativeHookActivationNoopDisk(b *testing.B) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		b.Run(string(mode), func(b *testing.B) {
			discardBenchLogs(b)
			base := b.TempDir()
			home := filepath.Join(base, "home")
			repo := filepath.Join(base, "repo")
			source := filepath.Join(base, "source")
			b.Setenv("HOME", home)
			b.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
			b.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
			b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			for _, dir := range []string{home, repo, filepath.Join(source, ".claude", "scripts")} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					b.Fatal(err)
				}
			}
			definition := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"}]}]}}`)
			if err := os.WriteFile(filepath.Join(source, ".claude", "settings.json"), definition, 0o644); err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, ".claude", "scripts", "check.sh"), []byte("#!/bin/sh\necho benchmark\n"), 0o755); err != nil {
				b.Fatal(err)
			}
			loaded, err := catalog.Load(source, "claude")
			if err != nil {
				b.Fatal(err)
			}
			if len(loaded.Entries) != 1 || loaded.Entries[0].Kind != ir.KindHook {
				b.Fatalf("expected exactly one ingested hook: %+v", loaded.Entries)
			}
			resolved := []resolver.ResolvedArtifact{{
				Entry: loaded.Entries[0], RegistryURL: source, CatalogKey: source,
				Source: config.RepoCommitted, V4Kind: config.V4SourceDirectory,
				Harnesses: []hcc.Target{hcc.TargetFactory},
			}}
			options := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
			run := func() {
				report, err := Run(context.Background(), repo, resolved, options...)
				if err != nil {
					b.Fatal(err)
				}
				if report == nil || diag.HasErrors(report.Diags) {
					b.Fatalf("hook sync was refused: %+v", report)
				}
			}
			run()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				run()
			}
		})
	}
}
