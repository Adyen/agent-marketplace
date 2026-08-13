package pipeline

import (
	"context"
	"errors"
	"path/filepath"
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
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v3
registries:
  - url: `+registryRoot+`
    ref: main
    source_harness: factory
    default: true
    harnesses:
      - claude
`)

	userPath := filepath.Join(t.TempDir(), "user.yaml")
	writeFile(t, userPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
`)

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
