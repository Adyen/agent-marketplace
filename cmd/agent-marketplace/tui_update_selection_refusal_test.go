package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// spec: resource-selection/tui-update-refuses-a-newly-missing-selection
func TestTUIUpdateRefusesNewlyMissingSelection(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	seedBundle := func(t *testing.T, working string) {
		writeSkill(t, working, "alpha", "Alpha v1", nil)
		writeSkill(t, working, "beta", "Beta v1", nil)
		writeCollection(t, working, "bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: bundle
version: 1.0.0
skills:
  - name: alpha
  - name: beta
`)
	}
	for _, tc := range []struct {
		name      string
		seed      func(t *testing.T, working string)
		resources string
		mutate    func(t *testing.T, working string)
		missing   string
	}{
		{
			name: "direct-renamed",
			seed: func(t *testing.T, working string) {
				writeSkill(t, working, "alpha", "Alpha v1", nil)
				writeSkill(t, working, "beta", "Beta v1", nil)
			},
			resources: "    include: [skill/alpha, skill/beta]\n",
			mutate: func(t *testing.T, working string) {
				if err := os.RemoveAll(filepath.Join(working, "skills", "alpha")); err != nil {
					t.Fatal(err)
				}
				writeSkill(t, working, "alpha-renamed", "Alpha v2", nil)
			},
			missing: `skill "alpha"`,
		},
		{
			name: "required-removed",
			seed: func(t *testing.T, working string) {
				writeSkill(t, working, "required-skill", "Required v1", nil)
				writeSkill(t, working, "kept", "Kept v1", nil)
			},
			resources: "    include: [skill/required-skill, skill/kept]\n    required: true\n",
			mutate: func(t *testing.T, working string) {
				if err := os.RemoveAll(filepath.Join(working, "skills", "required-skill")); err != nil {
					t.Fatal(err)
				}
			},
			missing: `skill "required-skill"`,
		},
		{
			name:      "collection-removed",
			seed:      seedBundle,
			resources: "    include: [collection/bundle]\n",
			mutate: func(t *testing.T, working string) {
				if err := os.RemoveAll(filepath.Join(working, "collections", "bundle")); err != nil {
					t.Fatal(err)
				}
			},
			missing: `collection "bundle"`,
		},
		{
			name:      "listed-member-renamed",
			seed:      seedBundle,
			resources: "    include: [collection/bundle]\n",
			mutate: func(t *testing.T, working string) {
				if err := os.RemoveAll(filepath.Join(working, "skills", "beta")); err != nil {
					t.Fatal(err)
				}
				writeSkill(t, working, "beta-renamed", "Beta v2", nil)
			},
			missing: `skill "beta"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, repoRoot, snapshot := updateRefusalWorkspace(t)
			origin, working := createBareRepoWithWorkingClone(t, "tui-refusal-"+tc.name+".git")
			tc.seed(t, working)
			commitAndPushAll(t, working, "seed")

			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  registry:\n    git: file://"+origin+"\n    ref: main\nresources:\n  registry:\n"+tc.resources)
			withWorkingDir(t, repoRoot)
			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("initial sync: %v\n%s", err, output)
			}
			before := snapshot()

			tc.mutate(t, working)
			commitAndPushAll(t, working, "mutate upstream")

			_, err := runTUIDependencyLifecycle(context.Background(), mustWorkspace(t, repoRoot), true)
			assertUpdateRefusedUnchanged(t, before, snapshot(), "", err, tc.missing)
		})
	}
}

// spec: resource-selection/tui-update-proceeds-when-no-selection-newly-stops-resolving
func TestTUIUpdateProceedsWhenNoSelectionNewlyStopsResolving(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name      string
		seed      func(t *testing.T, working string)
		resources string
		mutate    func(t *testing.T, working string)
	}{
		{
			name: "selection-already-missing",
			seed: func(t *testing.T, working string) {
				writeSkill(t, working, "kept", "Kept v1", nil)
			},
			resources: "    include: [skill/kept, skill/ghost]\n",
			mutate: func(t *testing.T, working string) {
				writeSkill(t, working, "kept", "Kept v2", nil)
			},
		},
		{
			name: "all-resources-renamed",
			seed: func(t *testing.T, working string) {
				writeSkill(t, working, "alpha", "Alpha v1", nil)
				writeSkill(t, working, "beta", "Beta v1", nil)
			},
			resources: "    all: true\n",
			mutate: func(t *testing.T, working string) {
				if err := os.RemoveAll(filepath.Join(working, "skills", "beta")); err != nil {
					t.Fatal(err)
				}
				writeSkill(t, working, "beta-renamed", "Beta v2", nil)
			},
		},
		{
			name: "member-removed-from-definition",
			seed: func(t *testing.T, working string) {
				writeSkill(t, working, "alpha", "Alpha v1", nil)
				writeSkill(t, working, "beta", "Beta v1", nil)
				writeCollection(t, working, "bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: bundle
version: 1.0.0
skills:
  - name: alpha
  - name: beta
`)
			},
			resources: "    include: [collection/bundle]\n",
			mutate: func(t *testing.T, working string) {
				if err := os.RemoveAll(filepath.Join(working, "skills", "beta")); err != nil {
					t.Fatal(err)
				}
				writeCollection(t, working, "bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: bundle
version: 1.1.0
skills:
  - name: alpha
`)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, repoRoot, _ := updateRefusalWorkspace(t)
			origin, working := createBareRepoWithWorkingClone(t, "tui-proceeds-"+tc.name+".git")
			tc.seed(t, working)
			commitAndPushAll(t, working, "seed")

			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  registry:\n    git: file://"+origin+"\n    ref: main\nresources:\n  registry:\n"+tc.resources)
			withWorkingDir(t, repoRoot)
			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("initial sync: %v\n%s", err, output)
			}
			lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
			beforeCommit := readLockDocument(t, lockPath).Registries[0].Commit

			tc.mutate(t, working)
			commitAndPushAll(t, working, "mutate upstream")

			if _, err := runTUIDependencyLifecycle(context.Background(), mustWorkspace(t, repoRoot), true); err != nil {
				t.Fatalf("TUI update refused: %v", err)
			}
			if got := readLockDocument(t, lockPath).Registries[0].Commit; got == beforeCommit {
				t.Fatal("TUI update did not advance the acquisition lock")
			}
		})
	}
}

func TestTUIDependencyLifecycleSyncDoesNotRefuse(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, repoRoot, _ := updateRefusalWorkspace(t)
	origin, working := createBareRepoWithWorkingClone(t, "tui-sync-rename.git")
	writeSkill(t, working, "alpha", "Alpha v1", nil)
	writeSkill(t, working, "beta", "Beta v1", nil)
	commitAndPushAll(t, working, "seed")

	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  registry:\n    git: file://"+origin+"\n    ref: main\nresources:\n  registry:\n    include: [skill/alpha, skill/beta]\n")
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}

	if err := os.RemoveAll(filepath.Join(working, "skills", "alpha")); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, working, "alpha-renamed", "Alpha v2", nil)
	commitAndPushAll(t, working, "rename alpha")

	_, err := runTUIDependencyLifecycle(context.Background(), mustWorkspace(t, repoRoot), false)
	if err != nil {
		t.Fatalf("TUI sync failed: %v", err)
	}
}
