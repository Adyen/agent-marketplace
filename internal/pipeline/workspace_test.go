package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
)

// mustWorkspace builds a repository-root workspace for tests that do not
// exercise workspace validation itself.
func mustWorkspace(t testing.TB, repoRoot string) Workspace {
	t.Helper()
	ws, err := NewWorkspace(repoRoot, "")
	if err != nil {
		t.Fatalf("NewWorkspace(%q): %v", repoRoot, err)
	}
	return ws
}

// mustNestedWorkspace builds a nested-local workspace for tests that do not
// exercise workspace validation itself.
func mustNestedWorkspace(t testing.TB, repoRoot, localConfigDir string) Workspace {
	t.Helper()
	ws, err := NewWorkspace(repoRoot, localConfigDir)
	if err != nil {
		t.Fatalf("NewWorkspace(%q, %q): %v", repoRoot, localConfigDir, err)
	}
	return ws
}

func TestNewWorkspaceRepositoryRootDerivesAllRoots(t *testing.T) {
	repoRoot := t.TempDir()

	ws, err := NewWorkspace(repoRoot, "")
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	if ws.RepoRoot() != repoRoot {
		t.Fatalf("RepoRoot() = %q; want %q", ws.RepoRoot(), repoRoot)
	}
	if ws.LocalConfigDir() != repoRoot {
		t.Fatalf("LocalConfigDir() = %q; want repository root %q", ws.LocalConfigDir(), repoRoot)
	}
	if ws.MaterializeRoot() != repoRoot {
		t.Fatalf("MaterializeRoot() = %q; want repository root %q", ws.MaterializeRoot(), repoRoot)
	}
}

func TestNewWorkspaceNestedLocalDerivesMaterializationRoot(t *testing.T) {
	repoRoot := t.TempDir()
	localDir := filepath.Join(repoRoot, "subdir")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatalf("mkdir nested local dir: %v", err)
	}

	ws, err := NewWorkspace(repoRoot, localDir)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	if ws.RepoRoot() != repoRoot {
		t.Fatalf("RepoRoot() = %q; want %q", ws.RepoRoot(), repoRoot)
	}
	if ws.LocalConfigDir() != localDir {
		t.Fatalf("LocalConfigDir() = %q; want %q", ws.LocalConfigDir(), localDir)
	}
	if ws.MaterializeRoot() != localDir {
		t.Fatalf("MaterializeRoot() = %q; want nested local root %q", ws.MaterializeRoot(), localDir)
	}
}

func TestNewWorkspaceRejectsEmptyRepositoryRoot(t *testing.T) {
	if _, err := NewWorkspace("", ""); !errors.Is(err, ErrInvalidWorkspace) {
		t.Fatalf("NewWorkspace empty repository root error = %v; want ErrInvalidWorkspace", err)
	}
}

func TestNewWorkspaceRejectsLocalConfigDirOutsideRepository(t *testing.T) {
	repoRoot := t.TempDir()
	outside := t.TempDir()

	if _, err := NewWorkspace(repoRoot, outside); !errors.Is(err, ErrInvalidWorkspace) {
		t.Fatalf("NewWorkspace outside local root error = %v; want ErrInvalidWorkspace", err)
	}
}

func TestResolveRejectsZeroWorkspaceBeforeLoaders(t *testing.T) {
	loaderRan := false
	result := Resolve(context.Background(), ResolveOptions{
		CatalogMode: catalog.LoadModeInspect,
		Loaders: Loaders{
			Repo: func() (*config.Config, error) {
				loaderRan = true
				return &config.Config{}, nil
			},
		},
	})

	if loaderRan {
		t.Fatal("zero Workspace must be rejected before any config loader runs")
	}
	if !errors.Is(errors.Join(result.Errors...), ErrInvalidWorkspace) {
		t.Fatalf("Resolve errors = %v; want ErrInvalidWorkspace", result.Errors)
	}
}

func TestResolveRejectsConflictingEffectiveOptsBeforeLoaders(t *testing.T) {
	repoRoot := t.TempDir()
	other := t.TempDir()
	ws := mustWorkspace(t, repoRoot)

	loaderRan := false
	loaders := Loaders{
		Repo: func() (*config.Config, error) {
			loaderRan = true
			return &config.Config{}, nil
		},
	}

	result := Resolve(context.Background(), ResolveOptions{
		Workspace:   ws,
		CatalogMode: catalog.LoadModeInspect,
		Loaders:     loaders,
		EffectiveOpts: config.LoadEffectiveOptions{
			RepoRoot: other,
		},
	})
	if loaderRan {
		t.Fatal("conflicting EffectiveOpts.RepoRoot must be rejected before any config loader runs")
	}
	if !errors.Is(errors.Join(result.Errors...), ErrInvalidWorkspace) {
		t.Fatalf("Resolve errors = %v; want ErrInvalidWorkspace", result.Errors)
	}

	result = Resolve(context.Background(), ResolveOptions{
		Workspace:   ws,
		CatalogMode: catalog.LoadModeInspect,
		Loaders:     loaders,
		EffectiveOpts: config.LoadEffectiveOptions{
			LocalConfigDir: other,
		},
	})
	if loaderRan {
		t.Fatal("conflicting EffectiveOpts.LocalConfigDir must be rejected before any config loader runs")
	}
	if !errors.Is(errors.Join(result.Errors...), ErrInvalidWorkspace) {
		t.Fatalf("Resolve errors = %v; want ErrInvalidWorkspace", result.Errors)
	}
}

func TestDiscoverWorkspaceFromNestedDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	subdir := filepath.Join(repoRoot, "subdir")
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
registries: []
`)
	writeFile(t, filepath.Join(subdir, ".agents", "config.local.yaml"), `registries: []
`)
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("chdir subdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
		config.ResetRepoRootCache()
	})
	config.ResetRepoRootCache()

	ws, err := DiscoverWorkspace()
	if err != nil {
		t.Fatalf("DiscoverWorkspace: %v", err)
	}
	if !workspaceSamePath(ws.RepoRoot(), repoRoot) {
		t.Fatalf("RepoRoot() = %q; want %q", ws.RepoRoot(), repoRoot)
	}
	if !workspaceSamePath(ws.LocalConfigDir(), subdir) {
		t.Fatalf("LocalConfigDir() = %q; want nested %q", ws.LocalConfigDir(), subdir)
	}
	if !workspaceSamePath(ws.MaterializeRoot(), subdir) {
		t.Fatalf("MaterializeRoot() = %q; want nested %q", ws.MaterializeRoot(), subdir)
	}
}

func TestSyncNestedLocalLoadsRepoConfigAndMaterializesBelowIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	subdir := filepath.Join(repoRoot, "subdir")

	registryRoot := filepath.Join(t.TempDir(), "registry")
	writeSkill(t, registryRoot, "nested-skill", "A nested-local skill", []string{"test"})

	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: `+registryRoot+`
    ref: main
    skills:
      - nested-skill
`)
	writeFile(t, filepath.Join(subdir, ".agents", "config.local.yaml"), `registries: []
`)

	globalPath := filepath.Join(t.TempDir(), "global.yaml")
	writeFile(t, globalPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries: []
`)

	ws := mustNestedWorkspace(t, repoRoot, subdir)
	result := Sync(context.Background(), ResolveOptions{
		Workspace: ws,
		NoBackup:  true,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: filepath.Join(t.TempDir(), "missing-org.yaml"),
			GlobalPath:       globalPath,
		},
	})

	if len(result.Errors) != 0 {
		t.Fatalf("nested-local sync errors = %v", result.Errors)
	}
	if !hasResolvedSkill(result.Resolved, "nested-skill") {
		t.Fatalf("expected nested-skill resolved from the repository config, got %#v", result.Resolved)
	}
	if _, err := os.Stat(filepath.Join(subdir, ".factory", "skills", "nested-skill")); err != nil {
		t.Fatalf("expected materialization below the nested local root: %v", err)
	}
}
