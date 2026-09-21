package tui

import (
	"os"
	"path/filepath"
	"testing"
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

	opts := tuiPipelineOptions(repoRoot)
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

// TestTUIWorkspaceFallsBackToRepositoryRootWhenDiscoveryIsOutside pins the
// explicit-construction path: when cwd-based discovery lands outside the
// given repository root, the workspace derives every root from the
// repository root rather than failing or adopting an unrelated root.
func TestTUIWorkspaceFallsBackToRepositoryRootWhenDiscoveryIsOutside(t *testing.T) {
	repoRoot := t.TempDir()

	ws := tuiWorkspace(repoRoot)
	if got, want := canonicalWorkspacePath(t, ws.RepoRoot()), canonicalWorkspacePath(t, repoRoot); got != want {
		t.Fatalf("workspace repository root = %q; want %q", got, want)
	}
	if got, want := canonicalWorkspacePath(t, ws.LocalConfigDir()), canonicalWorkspacePath(t, repoRoot); got != want {
		t.Fatalf("workspace local-config root = %q; want repository root %q", got, want)
	}
}
