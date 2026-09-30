package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func canonicalWorkspacePath(t *testing.T, path string) string {
	t.Helper()
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

// TestTUIPipelineOptionsPreservesNestedLocalDiscovery pins the production TUI
// seam: launched from a monorepo subdirectory, the workspace keeps the
// repository root while the discovered nested local-config root drives
// materialization, the same fallback LoadEffective applied before the
// Workspace value existed.
func TestTUIPipelineOptionsPreservesNestedLocalDiscovery(t *testing.T) {
	repoRoot := t.TempDir()
	nested := filepath.Join(repoRoot, "sub")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(nested, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, ".agents", "config.local.yaml"), []byte("registries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	ws, err := pipeline.NewWorkspace(repoRoot, nested)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	opts := tuiPipelineOptionsForWorkspace(ws)
	if got, want := canonicalWorkspacePath(t, opts.Workspace.RepoRoot()), canonicalWorkspacePath(t, repoRoot); got != want {
		t.Fatalf("workspace repository root = %q; want %q", got, want)
	}
	if got, want := canonicalWorkspacePath(t, opts.Workspace.LocalConfigDir()), canonicalWorkspacePath(t, nested); got != want {
		t.Fatalf("workspace local-config root = %q; want discovered nested root %q", got, want)
	}
	if got, want := canonicalWorkspacePath(t, opts.Workspace.MaterializeRoot()), canonicalWorkspacePath(t, nested); got != want {
		t.Fatalf("workspace materialization root = %q; want %q", got, want)
	}
}

// TestTUIPipelineOptionsBindsLocalLoaderToWorkspace proves the TUI local
// loader reads the validated Workspace root even when cwd discovery would now
// select a different local root.
func TestTUIPipelineOptionsBindsLocalLoaderToWorkspace(t *testing.T) {
	repoRoot := t.TempDir()
	nested := filepath.Join(repoRoot, "sub")
	outside := t.TempDir()
	for _, dir := range []string{filepath.Join(repoRoot, ".agents"), filepath.Join(nested, ".agents"), filepath.Join(outside, ".agents")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(config.ProjectConfigPath(repoRoot), []byte("apiVersion: agent-marketplace/v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.LocalConfigPath(nested), []byte("skills:\n  exclude: [workspace-local]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.LocalConfigPath(outside), []byte("skills:\n  exclude: [ambient-local]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := pipeline.NewWorkspace(repoRoot, nested)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	t.Chdir(outside)

	localCfg, err := tuiPipelineOptionsForWorkspace(ws).Loaders.Local()
	if err != nil {
		t.Fatalf("workspace local loader: %v", err)
	}
	if localCfg == nil || !slices.Contains(localCfg.AllExcludes(), "workspace-local") {
		t.Fatalf("workspace local excludes = %#v; want workspace-local", localCfg)
	}
	if slices.Contains(localCfg.AllExcludes(), "ambient-local") {
		t.Fatal("workspace local loader rediscovered the ambient local root")
	}
}

func TestEditConfigLocalPathUsesWorkspaceRoot(t *testing.T) {
	repoRoot := t.TempDir()
	nested := filepath.Join(repoRoot, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := pipeline.NewWorkspace(repoRoot, nested)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	m := NewModel(repoRoot, t.TempDir(), WithWorkspace(ws))
	m.pane = paneEditConfigModal
	if got := renderEditConfigModal(m); !strings.Contains(got, config.LocalConfigPath(nested)) {
		t.Fatalf("edit config modal does not contain workspace local path %q: %s", config.LocalConfigPath(nested), got)
	}
}

// TestReadSnapshotDryRunUsesWorkspaceMaterializeRoot pins the TUI drift probe
// to the nested materialization root selected for TUI sync.
func TestReadSnapshotDryRunUsesWorkspaceMaterializeRoot(t *testing.T) {
	repoRoot := t.TempDir()
	nested := filepath.Join(repoRoot, "sub")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v2\nregistries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(nested, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, ".agents", "config.local.yaml"), []byte("registries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	var gotRoot string
	previous := runMaterialize
	runMaterialize = func(ctx context.Context, root string, artifacts []resolver.ResolvedArtifact, opts ...materialize.RunOption) (*materialize.Report, error) {
		gotRoot = root
		return &materialize.Report{}, nil
	}
	t.Cleanup(func() { runMaterialize = previous })

	ws, err := pipeline.NewWorkspace(repoRoot, nested)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	previousOrg, previousGlobal, previousRepo, previousLocal := loadOrgConfig, loadGlobalConfig, loadRepoConfig, loadLocalConfig
	loadOrgConfig = func() (*config.Config, error) { return nil, nil }
	loadGlobalConfig = func() (*config.Config, error) { return nil, nil }
	loadRepoConfig = func() (*config.Config, error) {
		return config.LoadConfigFromPath(config.ProjectConfigPath(repoRoot))
	}
	loadLocalConfig = func(string) (*config.LocalConfig, error) { return nil, nil }
	t.Cleanup(func() {
		loadOrgConfig, loadGlobalConfig, loadRepoConfig, loadLocalConfig = previousOrg, previousGlobal, previousRepo, previousLocal
	})

	snapshot := readSnapshotForWorkspace(context.Background(), ws, t.TempDir(), catalogLoadCached)
	if len(snapshot.errs) != 0 {
		t.Fatalf("snapshot errors = %v", snapshot.errs)
	}
	if got, want := canonicalWorkspacePath(t, gotRoot), canonicalWorkspacePath(t, nested); got != want {
		t.Fatalf("TUI dry-run root = %q; want workspace materialization root %q (repository root %q)", got, want, repoRoot)
	}
}

func TestReadSnapshotRejectsInvalidWorkspaceBeforeLoaders(t *testing.T) {
	var loaderCalls int
	previousOrg, previousGlobal, previousRepo, previousLocal := loadOrgConfig, loadGlobalConfig, loadRepoConfig, loadLocalConfig
	loadOrgConfig = func() (*config.Config, error) {
		loaderCalls++
		return nil, nil
	}
	loadGlobalConfig = func() (*config.Config, error) {
		loaderCalls++
		return nil, nil
	}
	loadRepoConfig = func() (*config.Config, error) {
		loaderCalls++
		return nil, nil
	}
	loadLocalConfig = func(string) (*config.LocalConfig, error) {
		loaderCalls++
		return nil, nil
	}
	t.Cleanup(func() {
		loadOrgConfig, loadGlobalConfig, loadRepoConfig, loadLocalConfig = previousOrg, previousGlobal, previousRepo, previousLocal
	})

	snapshot := readSnapshotForWorkspace(context.Background(), pipeline.Workspace{}, t.TempDir(), catalogLoadCached)
	if !errors.Is(errors.Join(snapshot.errs...), pipeline.ErrInvalidWorkspace) {
		t.Fatalf("snapshot errors = %v; want ErrInvalidWorkspace", snapshot.errs)
	}
	if loaderCalls != 0 {
		t.Fatalf("invalid workspace invoked config loaders %d times", loaderCalls)
	}
}
