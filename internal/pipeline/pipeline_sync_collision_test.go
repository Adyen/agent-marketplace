package pipeline

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func TestSync_DemotesResolverNameCollisionToWarning(t *testing.T) {
	repoRoot := t.TempDir()
	registryA := filepath.Join(t.TempDir(), "registry-a")
	registryB := filepath.Join(t.TempDir(), "registry-b")

	writeSkill(t, registryA, "shared-skill", "Shared from registry A", []string{"test"})
	writeSkill(t, registryA, "only-a", "Only in A", []string{"test"})
	writeSkill(t, registryB, "shared-skill", "Shared from registry B", []string{"test"})
	writeSkill(t, registryB, "only-b", "Only in B", []string{"test"})

	repoCfg := &config.Config{
		APIVersion: "agent-marketplace/v2",
		Registries: []config.RegistryEntry{
			{
				URL:           registryA,
				SourceHarness: "claude",
				Skills:        config.KindSelector{Include: []string{"shared-skill", "only-a"}},
			},
			{
				URL:           registryB,
				SourceHarness: "claude",
				Skills:        config.KindSelector{Include: []string{"shared-skill", "only-b"}},
			},
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
		t.Fatalf("expected sync to succeed despite resolver collision, got errors: %v", result.Errors)
	}
	if result.Report == nil {
		t.Fatalf("expected materialize report for successful sync")
	}

	collisionWarnings := 0
	for _, warning := range result.Warnings {
		if resolver.IsNameCollision(warning) {
			collisionWarnings++
			if !strings.Contains(warning.Error(), "shared-skill") {
				t.Fatalf("expected collision warning to mention shared-skill, got %v", warning)
			}
		}
	}
	if collisionWarnings != 1 {
		t.Fatalf("expected exactly one collision warning, got %d warnings: %v", collisionWarnings, result.Warnings)
	}

	names := make(map[string]bool)
	for _, artifact := range result.Resolved {
		names[artifact.Name] = true
	}
	// The colliding skill is now resolved (winner picked) rather than skipped.
	if !names["shared-skill"] {
		t.Fatalf("expected colliding shared-skill to be resolved from winning source, resolved: %#v", result.Resolved)
	}
	if !names["only-a"] || !names["only-b"] {
		t.Fatalf("expected non-colliding skills to be resolved, resolved: %#v", result.Resolved)
	}
}
