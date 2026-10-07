package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func TestSync_SurfacesIncompatibleOrganizationDefaultAsWarning(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	registryRoot := filepath.Join(t.TempDir(), "registry")
	writeSkill(t, registryRoot, "default-skill", "A default skill", []string{"test"})

	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  registry:
    directory: `+registryRoot+`
    source_harness: factory
resources:
  registry:
    required: false
    all: true
    harnesses:
      - claude
`)

	userPath := filepath.Join(t.TempDir(), "user.yaml")
	writeFile(t, userPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
`)

	result := Sync(context.Background(), ResolveOptions{
		Workspace: mustWorkspace(t, repoRoot),
		NoBackup:  true,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       userPath,
			RepoPath:         filepath.Join(t.TempDir(), "missing-repo.yaml"),
			LocalPath:        filepath.Join(t.TempDir(), "missing-local.yaml"),
		},
	})

	if len(result.Errors) != 0 {
		t.Fatalf("expected no hard sync errors, got: %v", result.Errors)
	}

	var found bool
	for _, warning := range result.Warnings {
		var incompatible *resolver.IncompatibleOrganizationDefault
		if errors.As(warning, &incompatible) {
			found = true
			if incompatible.Name != "default-skill" {
				t.Fatalf("expected warning for default-skill, got %q", incompatible.Name)
			}
			if len(incompatible.Targets) != 1 || incompatible.Targets[0] != hcc.TargetFactory {
				t.Fatalf("expected warning to reference factory target, got %v", incompatible.Targets)
			}
		}
	}
	if !found {
		t.Fatalf("expected IncompatibleOrganizationDefault warning, got warnings: %v", result.Warnings)
	}

	if hasResolvedSkill(result.Resolved, "default-skill") {
		t.Fatalf("expected default-skill to be skipped due to incompatible harness set")
	}

	if result.Report != nil && result.Report.TotalWritten() > 0 {
		t.Fatalf("expected no materialized skills, got report: %v", result.Report)
	}
}

// spec: config-layering/existing-unsupported-identifier-is-retained-without-materialization-authority
func TestSync_RetainsUnsupportedHarnessIDWithoutMaterializationAuthority(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	registryRoot := filepath.Join(t.TempDir(), "registry")
	writeSkill(t, registryRoot, "kept-skill", "A selected skill", []string{"test"})

	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
`)

	userPath := filepath.Join(t.TempDir(), "user.yaml")
	writeFile(t, userPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
  - future-harness
sources:
  registry:
    directory: `+registryRoot+`
    source_harness: factory
resources:
  registry:
    all: true
`)

	result := Sync(context.Background(), ResolveOptions{
		Workspace: mustWorkspace(t, repoRoot),
		NoBackup:  true,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       userPath,
			RepoPath:         filepath.Join(t.TempDir(), "missing-repo.yaml"),
			LocalPath:        filepath.Join(t.TempDir(), "missing-local.yaml"),
		},
	})

	if len(result.Errors) != 0 {
		t.Fatalf("expected no hard sync errors, got: %v", result.Errors)
	}
	if result.GlobalCfg == nil || !slices.Equal(result.GlobalCfg.Harnesses, []string{"factory", "future-harness"}) {
		t.Fatalf("raw config lost authored identifiers: %#v", result.GlobalCfg)
	}
	if !slices.Equal(result.TargetHarnesses, []hcc.Target{hcc.TargetFactory}) {
		t.Fatalf("operational targets = %v, want [factory]", result.TargetHarnesses)
	}
	var warn *config.UnsupportedHarnessWarning
	if !slices.ContainsFunc(result.Warnings, func(w error) bool { return errors.As(w, &warn) }) {
		t.Fatalf("missing unsupported-harness warning in %v", result.Warnings)
	}
	if _, err := os.Lstat(filepath.Join(homeDir, ".factory", "skills", "kept-skill")); err != nil {
		t.Fatalf("factory skill not materialized: %v", err)
	}
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected materialization output %v in repo root", entries)
	}
	if _, err := os.Lstat(filepath.Join(homeDir, ".future-harness")); !os.IsNotExist(err) {
		t.Fatalf("unsupported identifier gained materialization authority: %v", err)
	}
}

// spec: config-layering/unsupported-only-target-set-grants-no-materialization-authority
func TestSync_UnsupportedOnlyHarnessSetFailsBeforeMaterialization(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	registryRoot := filepath.Join(t.TempDir(), "registry")
	writeSkill(t, registryRoot, "kept-skill", "A selected skill", []string{"test"})

	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - claude
`)

	userPath := filepath.Join(t.TempDir(), "user.yaml")
	writeFile(t, userPath, `apiVersion: agent-marketplace/v4
harnesses:
  - future-harness
sources:
  registry:
    directory: `+registryRoot+`
    source_harness: factory
resources:
  registry:
    all: true
`)

	result := Sync(context.Background(), ResolveOptions{
		Workspace: mustWorkspace(t, repoRoot),
		NoBackup:  true,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       userPath,
			RepoPath:         filepath.Join(t.TempDir(), "missing-repo.yaml"),
			LocalPath:        filepath.Join(t.TempDir(), "missing-local.yaml"),
		},
	})

	var missing *TargetHarnessSetMissingError
	if !slices.ContainsFunc(result.Errors, func(err error) bool { return errors.As(err, &missing) }) {
		t.Fatalf("expected TargetHarnessSetMissingError, got: %v", result.Errors)
	}
	if result.Report != nil && result.Report.TotalWritten() > 0 {
		t.Fatalf("materialization ran without a supported target set: %v", result.Report)
	}
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unsupported-only set produced repo output: %v", entries)
	}
	homeEntries, err := os.ReadDir(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(homeEntries) != 0 {
		t.Fatalf("unsupported-only set produced user output (incl. org fallback): %v", homeEntries)
	}
}
