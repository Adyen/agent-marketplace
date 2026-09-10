package pipeline

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func TestResolveStopsOnV3ConfigErrors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	// User config declares a v3 schema with a valid registry.
	userConfigPath := filepath.Join(dir, "user.yaml")
	writeFile(t, userConfigPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
allowed_registries:
  - ^.*
registries:
  - url: https://example.com/repo.git
    ref: main
`)

	// Repo config is v3 but contains a forbidden field for repo layer.
	repoConfigPath := filepath.Join(dir, "repo.yaml")
	writeFile(t, repoConfigPath, `apiVersion: agent-marketplace/v3
backup:
  enabled: true
registries:
  - url: https://example.com/repo.git
    ref: main
`)

	result := Resolve(context.Background(), ResolveOptions{
		RepoRoot:    dir,
		CatalogMode: catalog.LoadModeInspect,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: filepath.Join(dir, "missing-org.yaml"),
			GlobalPath:       userConfigPath,
			RepoPath:         repoConfigPath,
			LocalPath:        filepath.Join(dir, "missing-local.yaml"),
		},
	})

	if len(result.Errors) == 0 {
		t.Fatalf("expected Resolve to stop with v3 config errors")
	}

	// The repo backup error must be present, and no catalog loading should have
	// occurred because validation runs before network access.
	errStr := joinErrors(result.Errors)
	if !strings.Contains(errStr, "backup") {
		t.Fatalf("expected error to mention forbidden backup field, got: %v", result.Errors)
	}
	if len(result.CatalogLoad.Catalogs) > 0 {
		t.Fatalf("expected no catalog load when config validation fails")
	}
}

func TestResolve_LoadEffectivePathUsesOrganizationSource(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	orgPath := filepath.Join(dir, "org.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: git@gitlab.example.com:example-org/org.git
    ref: main
    required: true
`)
	writeFile(t, filepath.Join(dir, "lock.yaml"), "apiVersion: agent-marketplace-lock/v1\nscope: organization\n")

	result := Resolve(context.Background(), ResolveOptions{
		RepoRoot:    dir,
		CatalogMode: catalog.LoadModeInspect,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       filepath.Join(dir, "missing-global.yaml"),
			RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
			LocalPath:        filepath.Join(dir, "missing-local.yaml"),
		},
	})

	for _, err := range result.Errors {
		if strings.Contains(err.Error(), "allowed_registries") {
			t.Fatalf("unexpected allowlist error: %v", err)
		}
	}

	if result.OrgCfg == nil || result.OrgCfg.APIVersion != config.APIVersionV3 {
		t.Fatalf("expected v3 org config to be loaded")
	}

	// The org registry must carry the Organization source after merge. Because
	// there is no repo config, MergedCfg should contain exactly one registry.
	if result.MergedCfg == nil || len(result.MergedCfg.Registries) != 1 {
		t.Fatalf("expected 1 merged registry, got %v", result.MergedCfg)
	}
	if result.MergedCfg.Registries[0].Source != config.Organization {
		t.Fatalf("expected merged org registry source %v, got %v", config.Organization, result.MergedCfg.Registries[0].Source)
	}
}

func TestSync_InvalidOrgBlocksBeforeCatalog(t *testing.T) {
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	orgPath := filepath.Join(dir, "org.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v1
harnesses:
  - factory
registries: []
`)

	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: dir,
		NoBackup: true,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       filepath.Join(dir, "missing-global.yaml"),
			RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
			LocalPath:        filepath.Join(dir, "missing-local.yaml"),
		},
	})

	if len(result.Errors) == 0 {
		t.Fatalf("expected sync to fail on invalid organization config")
	}
	if !strings.Contains(joinErrors(result.Errors), "unsupported apiVersion") {
		t.Fatalf("expected unsupported apiVersion error, got: %v", result.Errors)
	}
	if len(result.CatalogLoad.Catalogs) != 0 {
		t.Fatalf("expected no catalog load when config validation fails, got %d", len(result.CatalogLoad.Catalogs))
	}
}

func TestSyncRevalidatesExactLocksBeforeMaterialization(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	orgPath := filepath.Join(dir, "org.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries: []
`)
	concurrent := errors.New("exact lock changed concurrently")
	called := false
	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: dir,
		NoBackup: true,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       filepath.Join(dir, "missing-global.yaml"),
			RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
			LocalPath:        filepath.Join(dir, "missing-local.yaml"),
		},
		ExactLockLease: func(context.Context) (func(), error) {
			called = true
			return nil, concurrent
		},
	})
	if !called {
		t.Fatal("exact lock lease was not acquired before materialization")
	}
	if !errors.Is(errors.Join(result.Errors...), concurrent) {
		t.Fatalf("sync errors = %v, want concurrent exact-lock error", result.Errors)
	}
}

func TestSync_NoLegacyOrgFallbackUnmanaged(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	registryRoot := filepath.Join(t.TempDir(), "registry")
	writeSkill(t, registryRoot, "local-skill", "A local skill", []string{"test"})

	globalPath := filepath.Join(t.TempDir(), "global.yaml")
	writeFile(t, globalPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries: []
`)

	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
registries:
  - url: `+registryRoot+`
    ref: main
    source_harness: claude
    skills:
      - local-skill
`)

	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot,
		NoBackup: true,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: filepath.Join(t.TempDir(), "missing-org.yaml"),
			GlobalPath:       globalPath,
			LocalPath:        filepath.Join(t.TempDir(), "missing-local.yaml"),
		},
	})

	if len(result.Errors) != 0 {
		t.Fatalf("expected sync to succeed with no org config, got errors: %v", result.Errors)
	}
	if !hasResolvedSkill(result.Resolved, "local-skill") {
		t.Fatalf("expected local-skill to be resolved, got: %#v", result.Resolved)
	}
	if result.Report == nil || result.Report.TotalWritten() == 0 {
		t.Fatalf("expected skill to be materialized, got report: %v", result.Report)
	}
}

func TestSync_OrganizationRequiredIgnoresRepoExclude(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	registryRoot := filepath.Join(t.TempDir(), "registry")
	writeSkill(t, registryRoot, "required-skill", "A required skill", []string{"test"})

	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: `+registryRoot+`
    ref: main
    source_harness: claude
    required: true
    skills:
      - required-skill
`)

	repoPath := filepath.Join(t.TempDir(), "repo.yaml")
	writeFile(t, repoPath, `apiVersion: agent-marketplace/v3
registries:
  - url: `+registryRoot+`
    ref: main
    source_harness: claude
    default: true
    skills:
      exclude:
        - required-skill
`)

	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot,
		NoBackup: true,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       filepath.Join(t.TempDir(), "missing-global.yaml"),
			RepoPath:         repoPath,
			LocalPath:        filepath.Join(t.TempDir(), "missing-local.yaml"),
		},
	})

	if len(result.Errors) != 0 {
		t.Fatalf("expected no hard sync errors, got: %v", result.Errors)
	}
	if !hasResolvedSkill(result.Resolved, "required-skill") {
		t.Fatalf("expected required-skill to survive repo exclude, got: %#v", result.Resolved)
	}
	if result.Report == nil || result.Report.TotalWritten() == 0 {
		t.Fatalf("expected required skill to be materialized")
	}
}

func TestSync_OrganizationDefaultSuppressibleByUserNotRepo(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	registryRoot := filepath.Join(t.TempDir(), "registry")
	writeSkill(t, registryRoot, "default-skill", "A default skill", []string{"test"})

	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: `+registryRoot+`
    ref: main
    source_harness: claude
    default: true
`)

	t.Run("user excludes", func(t *testing.T) {
		userPath := filepath.Join(t.TempDir(), "user.yaml")
		writeFile(t, userPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: `+registryRoot+`
    ref: main
    source_harness: claude
    skills:
      exclude:
        - default-skill
`)

		repoRoot := t.TempDir()
		result := Sync(context.Background(), ResolveOptions{
			RepoRoot: repoRoot,
			NoBackup: true,
			EffectiveOpts: config.LoadEffectiveOptions{
				OrganizationPath: orgPath,
				GlobalPath:       userPath,
				RepoPath:         filepath.Join(t.TempDir(), "missing-repo.yaml"),
				LocalPath:        filepath.Join(t.TempDir(), "missing-local.yaml"),
			},
		})

		if len(result.Errors) != 0 {
			t.Fatalf("expected no errors, got: %v", result.Errors)
		}
		if hasResolvedSkill(result.Resolved, "default-skill") {
			t.Fatalf("expected default-skill to be suppressed by user config")
		}
	})

	t.Run("repo excludes", func(t *testing.T) {
		repoRoot := t.TempDir()
		repoPath := filepath.Join(t.TempDir(), "repo.yaml")
		writeFile(t, repoPath, `apiVersion: agent-marketplace/v3
registries:
  - url: `+registryRoot+`
    ref: main
    source_harness: claude
    skills:
      exclude:
        - default-skill
`)

		result := Sync(context.Background(), ResolveOptions{
			RepoRoot: repoRoot,
			NoBackup: true,
			EffectiveOpts: config.LoadEffectiveOptions{
				OrganizationPath: orgPath,
				GlobalPath:       filepath.Join(t.TempDir(), "missing-global.yaml"),
				RepoPath:         repoPath,
				LocalPath:        filepath.Join(t.TempDir(), "missing-local.yaml"),
			},
		})

		if len(result.Errors) != 0 {
			t.Fatalf("expected no hard sync errors, got: %v", result.Errors)
		}
		if !hasResolvedSkill(result.Resolved, "default-skill") {
			t.Fatalf("expected default-skill to survive repo exclude, got: %#v", result.Resolved)
		}
		if result.Report == nil || result.Report.TotalWritten() == 0 {
			t.Fatalf("expected default skill to be materialized")
		}
	})
}

func hasResolvedSkill(resolved []resolver.ResolvedArtifact, name string) bool {
	for _, r := range resolved {
		if r.Name == name {
			return true
		}
	}
	return false
}

func joinErrors(errs []error) string {
	return errors.Join(errs...).Error()
}
