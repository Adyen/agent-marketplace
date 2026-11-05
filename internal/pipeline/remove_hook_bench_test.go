package pipeline

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// BenchmarkRemoveOwnedHookDisk measures direct pipeline removal including local
// source/config/ownership reads and writes, but no acquisition or installation.
func BenchmarkRemoveOwnedHookDisk(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	home, repo, source := b.TempDir(), b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	b.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	b.Chdir(repo)
	for _, path := range []string{filepath.Join(source, "hooks"), filepath.Join(repo, ".agents")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			b.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "hooks", "hooks.json"), []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo benchmark"}]}]}}`), 0o644); err != nil {
		b.Fatal(err)
	}
	data := []byte(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  reg: {directory: %q}\nresources:\n  reg: {include: [hook/PreToolUse-command]}\n", source))
	ws := mustWorkspace(b, repo)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		if err := os.WriteFile(config.ProjectConfigPath(repo), data, 0o644); err != nil {
			b.Fatal(err)
		}
		result := Sync(context.Background(), ResolveOptions{Workspace: ws})
		if len(result.Errors) != 0 {
			b.Fatal(result.Errors)
		}
		b.StartTimer()
		if _, err := Remove(context.Background(), RemoveOptions{
			RepoRoot: repo, ResourceName: "PreToolUse-command", Kind: ir.KindHook, HasKind: true, Scope: ScopeRepo, ScopeSpecified: true,
		}); err != nil {
			b.Fatal(err)
		}
	}
}
