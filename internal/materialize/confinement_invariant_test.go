package materialize

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// A confinement root that IS the repository (or the home directory) confines to
// nothing: every in-repo path becomes a legal managed write and a legal prune
// target, so a committed harness-root symlink or a committed manifest entry can
// reach .git/hooks or .github/workflows.
//
// This is driven by the target registry rather than by a fixed list, so a target
// definition added later is covered the moment it is registered. Windsurf is the
// case that already exists: it declares RepoRoot "." and is confined to
// <repo>/.windsurf through repoConfinementDirs.
func TestConfinementRootIsNeverTheRepositoryOrTheHomeDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	installRoots := harness.MakeInstallRoots()
	if len(installRoots) != len(targetmeta.Definitions()) {
		t.Fatalf("install roots (%d) do not cover every registered target (%d)", len(installRoots), len(targetmeta.Definitions()))
	}
	for _, definition := range targetmeta.Definitions() {
		target := hcc.Target(definition.Target)
		roots, ok := installRoots[target]
		if !ok {
			t.Fatalf("target %q has no install roots", target)
		}
		for _, machineScope := range []bool{false, true} {
			root, err := confinementRoot(target, roots, repoRoot, machineScope)
			if err != nil {
				t.Fatalf("target %q machine_scope=%v: %v", target, machineScope, err)
			}
			boundary := repoRoot
			if machineScope {
				boundary = homeDir
			}
			if containsPath(root, boundary) {
				t.Fatalf("target %q machine_scope=%v confines to %q, which contains %q", target, machineScope, root, boundary)
			}
		}
	}
}

// A target added later that declares the repository as its repo root, without
// naming the single directory its artifacts live under, must be refused rather
// than silently granted the whole checkout.
func TestConfinementRootRefusesATargetWithNoRootOfItsOwn(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	future := hcc.Target("future-harness")
	if _, ok := repoConfinementDirs[future]; ok {
		t.Fatalf("%q is registered in repoConfinementDirs; pick a target this test can treat as unknown", future)
	}

	if _, err := confinementRoot(future, harness.Roots{Repo: ".", Machine: filepath.Join(homeDir, ".future")}, repoRoot, false); err == nil {
		t.Fatal("expected a repo-scope target declaring \".\" to be refused")
	}
	if _, err := confinementRoot(future, harness.Roots{Repo: ".future", Machine: homeDir}, repoRoot, true); err == nil {
		t.Fatal("expected a machine-scope target declaring the home directory to be refused")
	}
	if _, err := confinementRoot(future, harness.Roots{Repo: ".future", Machine: filepath.Dir(homeDir)}, repoRoot, true); err == nil {
		t.Fatal("expected a machine-scope target above the home directory to be refused")
	}
}

// Every confinement root is an open directory file descriptor. A refusal that
// returns without closing it leaks that descriptor for the rest of the process,
// which stays invisible until a long-running sync exhausts the table.
func TestRunClosesEveryConfinementRootIncludingRefusedTargets(t *testing.T) {
	cases := []struct {
		name  string
		run   func(t *testing.T, repoRoot string) error
		scope config.Source
	}{
		{
			name: "successful sync",
			run: func(t *testing.T, repoRoot string) error {
				_, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{{
					Entry:     catalog.Entry{Name: "closes", Resource: ir.NewSkill("closes", "Closes", "Body", nil, nil, ir.SourceRef{})},
					Source:    config.RepoCommitted,
					Harnesses: []hcc.Target{hcc.TargetFactory},
				}})
				return err
			},
		},
		{
			name: "refused symlink-only target in file mode",
			run: func(t *testing.T, repoRoot string) error {
				_, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{{
					Entry:     catalog.Entry{Name: "refused", Resource: ir.NewSkill("refused", "Refused", "Body", nil, nil, ir.SourceRef{})},
					Source:    config.RepoCommitted,
					Harnesses: []hcc.Target{hcc.TargetDevin},
				}}, WithMaterializationModes(config.MaterializationModes{
					Project: config.MaterializationModeFile,
					Machine: config.MaterializationModeFile,
				}))
				return err
			},
		},
		{
			name: "refused symlinked harness root",
			run: func(t *testing.T, repoRoot string) error {
				if err := os.Symlink(t.TempDir(), filepath.Join(repoRoot, ".factory")); err != nil {
					t.Fatal(err)
				}
				_, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{{
					Entry:     catalog.Entry{Name: "planted", Resource: ir.NewSkill("planted", "Planted", "Body", nil, nil, ir.SourceRef{})},
					Source:    config.RepoCommitted,
					Harnesses: []hcc.Target{hcc.TargetFactory},
				}})
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
			repoRoot := t.TempDir()

			before := openConfinedRoots.Load()
			if err := tc.run(t, repoRoot); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if after := openConfinedRoots.Load(); after != before {
				t.Fatalf("confinement roots leaked: %d open before the run, %d after", before, after)
			}
		})
	}
}
