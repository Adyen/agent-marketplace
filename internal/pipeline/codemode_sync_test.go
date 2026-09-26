package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func codeModeLoaders(cm *config.CodeModeConfig) Loaders {
	return Loaders{
		Org:    func() (*config.Config, error) { return &config.Config{}, nil },
		Global: func() (*config.Config, error) { return &config.Config{}, nil },
		Repo: func() (*config.Config, error) {
			return &config.Config{
				Harnesses: []string{"claude", "codex", "devin"},
				CodeMode:  cm,
			}, nil
		},
		Local: func() (*config.LocalConfig, error) { return &config.LocalConfig{}, nil },
	}
}

func TestCodeModeSyncRegistersAndUnregistersOwnedMCPEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	repoRoot := t.TempDir()

	writeFile(t, filepath.Join(repoRoot, ".mcp.json"), `{"mcpServers":{"handwritten":{"command":"echo","args":["hi"]}}}`)

	enabled := true
	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot, NoBackup: true, Offline: true,
		Loaders: codeModeLoaders(&config.CodeModeConfig{Enabled: &enabled}),
	})
	if len(result.Errors) != 0 {
		t.Fatalf("sync errors = %v", result.Errors)
	}

	assertContains := func(path string) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
		if !strings.Contains(string(data), "agent-marketplace") {
			t.Fatalf("%s missing code-mode entry:\n%s", path, data)
		}
	}
	assertContains(filepath.Join(repoRoot, ".mcp.json"))
	assertContains(filepath.Join(repoRoot, ".codex", "config.toml"))
	assertContains(filepath.Join(repoRoot, ".devin", "mcp_config.json"))

	data, _ := os.ReadFile(filepath.Join(repoRoot, ".mcp.json"))
	if !strings.Contains(string(data), "handwritten") {
		t.Fatalf("handwritten entry lost:\n%s", data)
	}

	result = Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot, NoBackup: true, Offline: true,
		Loaders: codeModeLoaders(nil),
	})
	if len(result.Errors) != 0 {
		t.Fatalf("disable sync errors = %v", result.Errors)
	}
	for _, path := range []string{
		filepath.Join(repoRoot, ".mcp.json"),
		filepath.Join(repoRoot, ".codex", "config.toml"),
		filepath.Join(repoRoot, ".devin", "mcp_config.json"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "agent-marketplace") {
			t.Fatalf("owned entry not pruned in %s:\n%s", path, data)
		}
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, ".mcp.json"))
	if err == nil && !strings.Contains(string(data), "handwritten") {
		t.Fatalf("handwritten entry pruned:\n%s", data)
	}
}

func TestCodeModeAbsentRegistersNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	repoRoot := t.TempDir()

	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot, NoBackup: true, Offline: true,
		Loaders: codeModeLoaders(nil),
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
