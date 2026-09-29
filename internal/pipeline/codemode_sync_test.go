package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func emptyLoaders() Loaders {
	return Loaders{
		Org:    func() (*config.Config, error) { return &config.Config{}, nil },
		Global: func() (*config.Config, error) { return &config.Config{}, nil },
		Repo: func() (*config.Config, error) {
			return &config.Config{Harnesses: []string{"claude", "codex", "devin"}}, nil
		},
		Local: func() (*config.LocalConfig, error) { return &config.LocalConfig{}, nil },
	}
}

// spec: code-mode-mcp/installing-collection-selects-skill-and-mcp
func TestCodeModeAbsentSelectsNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	repoRoot := t.TempDir()

	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot, NoBackup: true, Offline: true,
		Loaders: emptyLoaders(),
	})
	if len(result.Errors) != 0 {
		t.Fatalf("sync errors = %v", result.Errors)
	}
	for _, path := range []string{
		filepath.Join(repoRoot, ".mcp.json"),
		filepath.Join(repoRoot, ".codex", "config.toml"),
		filepath.Join(repoRoot, ".devin", "mcp_config.json"),
	} {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), "agent-marketplace") {
			t.Fatalf("unexpected code-mode entry in %s", path)
		}
	}
}

// spec: code-mode-mcp/installing-collection-selects-skill-and-mcp
func TestRequireCodeModeExecutable(t *testing.T) {
	namedArtifact := func(command string, args []string) resolver.ResolvedArtifact {
		return resolver.ResolvedArtifact{
			Entry: catalog.Entry{
				Kind:     ir.KindMcpServer,
				Name:     "agent-marketplace",
				Resource: ir.NewMcpServer("agent-marketplace", command, args, nil, nil, ir.SourceRef{}),
			},
		}
	}
	t.Setenv("PATH", t.TempDir())
	other := resolver.ResolvedArtifact{
		Entry: catalog.Entry{
			Kind:     ir.KindMcpServer,
			Name:     "unrelated",
			Resource: ir.NewMcpServer("unrelated", "missing-for-sure-binary", nil, nil, nil, ir.SourceRef{}),
		},
	}

	if err := requireCodeModeExecutable([]resolver.ResolvedArtifact{namedArtifact("agent-marketplace", []string{"mcp"})}); err == nil ||
		!strings.Contains(err.Error(), "PATH") {
		t.Fatalf("expected actionable missing-executable error, got %v", err)
	}
	if err := requireCodeModeExecutable([]resolver.ResolvedArtifact{namedArtifact("/nonexistent/abs/path/agent-marketplace", []string{"mcp"})}); err == nil ||
		!strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected actionable missing-path error, got %v", err)
	}

	dir := t.TempDir()
	dirCommand := filepath.Join(dir, "agent-marketplace")
	if err := os.MkdirAll(dirCommand, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := requireCodeModeExecutable([]resolver.ResolvedArtifact{namedArtifact(dirCommand, []string{"mcp"})}); err == nil ||
		!strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected directory command path to fail, got %v", err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nonExec := filepath.Join(binDir, "agent-marketplace")
	if err := os.WriteFile(nonExec, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := requireCodeModeExecutable([]resolver.ResolvedArtifact{namedArtifact(nonExec, []string{"mcp"})}); err == nil ||
		!strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected non-executable command path to fail, got %v", err)
	}
	if err := requireCodeModeExecutable([]resolver.ResolvedArtifact{other}); err != nil {
		t.Fatalf("unrelated MCP server restricted: %v", err)
	}
	for _, artifact := range []resolver.ResolvedArtifact{
		namedArtifact("definitely-missing-am-binary-xyz", []string{"serve"}),
		namedArtifact("definitely-missing-other-binary", []string{"mcp"}),
		namedArtifact("definitely-missing-am-binary-xyz", nil),
	} {
		if err := requireCodeModeExecutable([]resolver.ResolvedArtifact{artifact}); err != nil {
			t.Fatalf("non-runtime agent-marketplace MCP gated: %v", err)
		}
	}
}
