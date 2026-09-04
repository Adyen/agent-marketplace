package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestRemovePathOnlyLocalCollectionAliasPreservesUnrelatedSelection(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	t.Setenv("HOME", t.TempDir())
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	localA, localB := t.TempDir(), t.TempDir()

	writeSkill(t, localA, "selected-skill", "selected skill", nil)
	writeFile(t, filepath.Join(localA, "collections", "selected-bundle", "COLLECTION.yaml"), "name: selected-bundle\nskills:\n  - name: selected-skill\n")
	writeSkill(t, localB, "unrelated-skill", "unrelated skill", nil)
	writeFile(t, filepath.Join(localB, "collections", "unrelated-bundle", "COLLECTION.yaml"), "name: unrelated-bundle\nskills:\n  - name: unrelated-skill\n")

	rootPath := config.ProjectConfigPath(repoRoot)
	writeFile(t, rootPath, fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - path: %q\n    collections: [selected-bundle]\n  - path: %q\n    collections: [unrelated-bundle]\n", localA, localB))

	result, err := Remove(context.Background(), RemoveOptions{
		RepoRoot: repoRoot, ResourceName: "selected-bundle", FromURL: localA,
		Scope: ScopeRepo, ScopeSpecified: true,
	})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if result == nil || !result.IsCollection || result.RemovedName != "selected-bundle" {
		t.Fatalf("result = %#v, want removed collection", result)
	}

	cfg, err := config.LoadRootConfigFromPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Registries) != 2 {
		t.Fatalf("registries = %#v, want both path-only aliases preserved", cfg.Registries)
	}
	if slices.Contains(cfg.Registries[0].Collections.Include, "selected-bundle") || !slices.Contains(cfg.Registries[0].Collections.Exclude, "selected-bundle") {
		t.Fatalf("selected alias = %#v, want selected collection excluded", cfg.Registries[0])
	}
	if !slices.Contains(cfg.Registries[1].Collections.Include, "unrelated-bundle") || slices.Contains(cfg.Registries[1].Collections.Exclude, "unrelated-bundle") {
		t.Fatalf("unrelated alias = %#v, want untouched", cfg.Registries[1])
	}

	resolved := Resolve(context.Background(), ResolveOptions{RepoRoot: repoRoot, SkipInventory: true})
	if len(resolved.Errors) != 0 || len(resolved.Resolved) != 1 || resolved.Resolved[0].Name != "unrelated-skill" || resolved.Resolved[0].RegistryURL != localB {
		t.Fatalf("resolved = %#v, errors = %v; want only unrelated local selection", resolved.Resolved, resolved.Errors)
	}
}
