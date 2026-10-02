package materialize

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// BenchmarkMirroredHookMaterialization includes local disk I/O and warm
// reconciliation, but excludes Marketplace acquisition and network I/O.
func BenchmarkMirroredHookMaterialization(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	home, repo, source := b.TempDir(), b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	b.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	data := []byte("#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(filepath.Join(source, "run.sh"), data, 0o755); err != nil {
		b.Fatal(err)
	}
	hook := ir.NewHook("guard", "PreToolUse", "${CLAUDE_PLUGIN_ROOT}/hooks/run.sh", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	hook.SupportFiles = map[string]string{"run.sh": artifact.SupportFileDigest(artifact.SupportFile{Data: data, Executable: true})}
	resolved := []resolver.ResolvedArtifact{{
		Entry:         catalog.Entry{Name: "guard", Kind: ir.KindHook, Path: source, Resource: hook},
		IsMarketplace: true, MarketplaceAcquisition: config.MarketplaceAcquisitionMirror,
		Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory},
	}}
	run := func() {
		report, err := Run(b.Context(), repo, resolved, WithRepoScopeOnly())
		if err != nil || diag.HasErrors(report.Diags) {
			b.Fatalf("materialization: %v, %v", report, err)
		}
	}
	run()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		run()
	}
}
