package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// mcpRegistry writes a single-server Claude-layout registry and returns its path.
func mcpRegistry(t *testing.T, dir, name, command string) string {
	t.Helper()
	registry := filepath.Join(t.TempDir(), dir)
	writeFile(t, filepath.Join(registry, ".mcp.json"), `{
  "mcpServers": {
    "`+name+`": {"command": "`+command+`", "args": ["run"]}
  }
}
`)
	return registry
}

// TestSyncFailsClosedOnExecutableCollision is the pipeline half of the refusal:
// a contested MCP identity must reach SyncResult.Errors (exit 1) rather than
// being demoted into Warnings the way a skill collision is, and no aggregate
// file may be written for it.
func TestSyncFailsClosedOnExecutableCollision(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	repoRoot := t.TempDir()

	registryA := mcpRegistry(t, "registry-a", "shared-mcp", "attacker-binary")
	registryB := mcpRegistry(t, "registry-b", "shared-mcp", "trusted-binary")

	repoCfg := &config.Config{
		APIVersion: "agent-marketplace/v2",
		Registries: []config.RegistryEntry{
			{URL: registryA, SourceHarness: "claude", MCP: config.KindSelector{Include: []string{"shared-mcp"}}},
			{URL: registryB, SourceHarness: "claude", MCP: config.KindSelector{Include: []string{"shared-mcp"}}},
		},
	}

	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot,
		NoBackup: true,
		Loaders: Loaders{
			Org:    func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) { return &config.Config{Harnesses: []string{"factory"}}, nil },
			Repo:   func() (*config.Config, error) { return repoCfg, nil },
			Local:  func() (*config.LocalConfig, error) { return nil, nil },
		},
	})

	refusals := 0
	for _, err := range result.Errors {
		if resolver.IsExecutableCollision(err) {
			refusals++
			if !strings.Contains(err.Error(), "shared-mcp") {
				t.Errorf("refusal does not name the contested resource: %v", err)
			}
		}
	}
	if refusals != 1 {
		t.Fatalf("expected exactly one executable collision in Errors, got %d (errors: %v)", refusals, result.Errors)
	}
	for _, warning := range result.Warnings {
		if resolver.IsExecutableCollision(warning) {
			t.Fatalf("executable collision must not be demoted to a warning: %v", warning)
		}
	}
	for _, artifact := range result.Resolved {
		if artifact.Name == "shared-mcp" {
			t.Fatalf("contested MCP server resolved from %s; nothing may be selected", artifact.RegistryURL)
		}
	}

	// A pre-materialization refusal must leave the harness untouched: neither
	// definition may reach mcp.json, not even the one that would have won.
	mcpPath := filepath.Join(repoRoot, ".factory", "mcp.json")
	if data, err := os.ReadFile(mcpPath); err == nil {
		t.Fatalf("%s was written despite the refusal: %s", mcpPath, data)
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected error reading %s: %v", mcpPath, err)
	}
}

// TestSyncFailsClosedOnExecutableCollisionAcrossUserAndRepoScope is the
// acceptance case for the scope split: a repo-committed registry must not be able
// to publish an MCP identity a user-personal registry already owns. The two
// definitions land in different roots, so nothing is overwritten on disk and the
// refusal is the only thing standing between the user and a checked-out
// repository whose command line every merging harness prefers at runtime.
func TestSyncFailsClosedOnExecutableCollisionAcrossUserAndRepoScope(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	repoRoot := t.TempDir()

	userRegistry := mcpRegistry(t, "user-registry", "shared-mcp", "trusted-binary")
	repoRegistry := mcpRegistry(t, "repo-registry", "shared-mcp", "attacker-binary")

	globalCfg := &config.Config{
		APIVersion: "agent-marketplace/v2",
		Harnesses:  []string{"factory"},
		Registries: []config.RegistryEntry{
			{URL: userRegistry, SourceHarness: "claude", MCP: config.KindSelector{Include: []string{"shared-mcp"}}},
		},
	}
	repoCfg := &config.Config{
		APIVersion: "agent-marketplace/v2",
		Registries: []config.RegistryEntry{
			{URL: repoRegistry, SourceHarness: "claude", MCP: config.KindSelector{Include: []string{"shared-mcp"}}},
		},
	}

	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot,
		NoBackup: true,
		Loaders: Loaders{
			Org:    func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) { return globalCfg, nil },
			Repo:   func() (*config.Config, error) { return repoCfg, nil },
			Local:  func() (*config.LocalConfig, error) { return nil, nil },
		},
	})

	refusals := 0
	for _, err := range result.Errors {
		if resolver.IsExecutableCollision(err) {
			refusals++
		}
	}
	if refusals != 1 {
		t.Fatalf("expected exactly one executable collision in Errors, got %d (errors: %v)", refusals, result.Errors)
	}
	for _, artifact := range result.Resolved {
		if artifact.Name == "shared-mcp" {
			t.Fatalf("contested MCP server resolved from %s at scope %v; nothing may be selected", artifact.RegistryURL, artifact.Source)
		}
	}

	for _, root := range []string{filepath.Join(homeDir, ".factory"), filepath.Join(repoRoot, ".factory")} {
		mcpPath := filepath.Join(root, "mcp.json")
		if data, err := os.ReadFile(mcpPath); err == nil {
			t.Fatalf("%s was written despite the refusal: %s", mcpPath, data)
		} else if !os.IsNotExist(err) {
			t.Fatalf("unexpected error reading %s: %v", mcpPath, err)
		}
	}
}

// TestSyncInstallsExecutableOnceAmbiguityIsExcluded pins the escape hatch end to
// end: the refusal is judged after excludes, so naming the source the user does
// not want makes the sync succeed with the remaining definition installed.
func TestSyncInstallsExecutableOnceAmbiguityIsExcluded(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	repoRoot := t.TempDir()

	registryA := mcpRegistry(t, "registry-a", "shared-mcp", "attacker-binary")
	registryB := mcpRegistry(t, "registry-b", "shared-mcp", "trusted-binary")

	repoCfg := &config.Config{
		APIVersion: "agent-marketplace/v2",
		Registries: []config.RegistryEntry{
			{URL: registryA, SourceHarness: "claude", MCP: config.KindSelector{Exclude: []string{"shared-mcp"}}},
			{URL: registryB, SourceHarness: "claude", MCP: config.KindSelector{Include: []string{"shared-mcp"}}},
		},
	}

	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot,
		NoBackup: true,
		Loaders: Loaders{
			Org:    func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) { return &config.Config{Harnesses: []string{"factory"}}, nil },
			Repo:   func() (*config.Config, error) { return repoCfg, nil },
			Local:  func() (*config.LocalConfig, error) { return nil, nil },
		},
	})

	if len(result.Errors) != 0 {
		t.Fatalf("expected sync to succeed once the ambiguity is excluded, got errors: %v", result.Errors)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "mcp.json"))
	if err != nil {
		t.Fatalf("expected mcp.json to be written: %v", err)
	}
	if !strings.Contains(string(data), "trusted-binary") {
		t.Fatalf("mcp.json does not carry the remaining definition: %s", data)
	}
	if strings.Contains(string(data), "attacker-binary") {
		t.Fatalf("mcp.json carries the excluded definition: %s", data)
	}
}
