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
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	localA, localB := t.TempDir(), t.TempDir()

	writeSkill(t, localA, "selected-skill", "selected skill", nil)
	writeFile(t, filepath.Join(localA, "collections", "selected-bundle", "COLLECTION.yaml"), "name: selected-bundle\nskills:\n  - name: selected-skill\n")
	writeSkill(t, localB, "unrelated-skill", "unrelated skill", nil)
	writeFile(t, filepath.Join(localB, "collections", "unrelated-bundle", "COLLECTION.yaml"), "name: unrelated-bundle\nskills:\n  - name: unrelated-skill\n")

	rootPath := config.ProjectConfigPath(repoRoot)
	writeFile(t, rootPath, fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  selected: {directory: %q}\n  unrelated: {directory: %q}\nresources:\n  selected: [collection/selected-bundle]\n  unrelated: [collection/unrelated-bundle]\n", localA, localB))

	before := Resolve(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, repoRoot), SkipInventory: true})
	if len(before.Errors) != 0 || len(before.Resolved) != 2 {
		t.Fatalf("initial directory selections = %#v, errors = %v; want both collection members", before.Resolved, before.Errors)
	}
	for _, want := range []struct{ name, directory string }{{"selected-skill", localA}, {"unrelated-skill", localB}} {
		found := false
		for _, artifact := range before.Resolved {
			if artifact.Name == want.name && artifact.RegistryURL == want.directory && artifact.RegistryPath == "" && artifact.Ref == "" && artifact.Source == config.RepoCommitted {
				found = true
			}
		}
		if !found {
			t.Fatalf("initial selection %q did not retain its exact directory identity: %#v", want.name, before.Resolved)
		}
	}

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
	for i, directory := range []string{localA, localB} {
		entry := cfg.Registries[i]
		if entry.URL != directory || entry.Path != "" || entry.Ref != "" || entry.V4Kind != config.V4SourceDirectory {
			t.Fatalf("directory alias %d = %#v, want exact %q with no Git ref/path", i, entry, directory)
		}
	}
	if slices.Contains(cfg.Registries[0].Collections.Include, "selected-bundle") || !slices.Contains(cfg.Registries[0].Collections.Exclude, "selected-bundle") {
		t.Fatalf("selected alias = %#v, want selected collection excluded", cfg.Registries[0])
	}
	if !slices.Contains(cfg.Registries[1].Collections.Include, "unrelated-bundle") || slices.Contains(cfg.Registries[1].Collections.Exclude, "unrelated-bundle") {
		t.Fatalf("unrelated alias = %#v, want untouched", cfg.Registries[1])
	}

	resolved := Resolve(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, repoRoot), SkipInventory: true})
	if len(resolved.Errors) != 0 || len(resolved.Resolved) != 1 || resolved.Resolved[0].Name != "unrelated-skill" || resolved.Resolved[0].RegistryURL != localB {
		t.Fatalf("resolved = %#v, errors = %v; want only unrelated local selection", resolved.Resolved, resolved.Errors)
	}
}
