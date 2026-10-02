package materialize

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/portable-manifest-alias-refusal-preserves-native-output
func TestRunPortableManifestAliasRefusalPreservesNativeOutput(t *testing.T) {
	home, repo, outside := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "portable-probe", Resource: ir.NewSkill("portable-probe", "portable", "original inert content", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}
	initial, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || diag.HasErrors(initial.Diags) {
		t.Fatalf("valid portable control failed: %+v %v", initial, err)
	}
	installed := filepath.Join(repo, ".factory", "skills", "portable-probe", "SKILL.md")
	before, err := os.ReadFile(installed)
	if err != nil || !bytes.Contains(before, []byte("original inert content")) {
		t.Fatalf("native positive control missing: %q %v", before, err)
	}
	installedInfo, err := os.Stat(installed)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(repo, ".agents", "materialization-manifest.yaml")
	foreign := filepath.Join(outside, "manifest.yaml")
	if err := os.Rename(manifest, foreign); err != nil {
		t.Fatal(err)
	}
	foreignBytes, err := os.ReadFile(foreign)
	if err != nil {
		t.Fatal(err)
	}
	foreignInfo, err := os.Stat(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, manifest); err != nil {
		t.Fatal(err)
	}
	resolved[0].Resource = ir.NewSkill("portable-probe", "portable", "changed inert content", nil, nil, ir.SourceRef{})
	report, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || !hasDiagCode(report.Diags, "materialize.manifest.error") {
		t.Fatalf("manifest alias did not refuse: %+v %v", report, err)
	}
	if after, err := os.ReadFile(installed); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("manifest refusal changed installed content: %q %v", after, err)
	}
	if after, err := os.Stat(installed); err != nil || !os.SameFile(installedInfo, after) || installedInfo.Mode() != after.Mode() || !installedInfo.ModTime().Equal(after.ModTime()) {
		t.Fatalf("manifest refusal changed installed metadata: %v", err)
	}
	if target, err := os.Readlink(manifest); err != nil || target != foreign {
		t.Fatalf("manifest refusal replaced alias: %q %v", target, err)
	}
	if after, err := os.ReadFile(foreign); err != nil || !bytes.Equal(foreignBytes, after) {
		t.Fatalf("manifest refusal changed foreign bytes: %q %v", after, err)
	}
	if after, err := os.Stat(foreign); err != nil || !os.SameFile(foreignInfo, after) || foreignInfo.Mode() != after.Mode() || !foreignInfo.ModTime().Equal(after.ModTime()) {
		t.Fatalf("manifest refusal changed foreign metadata: %v", err)
	}
}

// spec: materialization/contained-portable-manifest-parent-alias-retains-native-updates
func TestRunContainedPortableManifestParentAliasRetainsNativeUpdates(t *testing.T) {
	for _, machine := range []bool{false, true} {
		t.Run(map[bool]string{false: "repository", true: "machine"}[machine], func(t *testing.T) {
			home, repo := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			anchor, directory, source, out := repo, ".agents", config.RepoCommitted, filepath.Join(repo, ".factory")
			if machine {
				anchor, directory, source, out = home, ".agent-marketplace", config.UserPersonal, filepath.Join(home, ".factory")
			}
			metadata := filepath.Join(anchor, "metadata")
			if err := os.Mkdir(metadata, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("metadata", filepath.Join(anchor, directory)); err != nil {
				t.Fatal(err)
			}
			resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "portable-probe", Resource: ir.NewSkill("portable-probe", "portable", "original inert content", nil, nil, ir.SourceRef{})}, Source: source, Harnesses: []hcc.Target{hcc.TargetFactory}}}
			modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
			initial, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
			if err != nil || diag.HasErrors(initial.Diags) {
				t.Fatalf("contained manifest parent refused: %+v %v", initial, err)
			}
			if _, err := os.Stat(filepath.Join(metadata, "materialization-manifest.yaml")); err != nil {
				t.Fatal(err)
			}
			resolved[0].Resource = ir.NewSkill("portable-probe", "portable", "changed inert content", nil, nil, ir.SourceRef{})
			updated, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
			if err != nil || diag.HasErrors(updated.Diags) {
				t.Fatalf("contained native update failed: %+v %v", updated, err)
			}
			installed := filepath.Join(out, "skills", "portable-probe", "SKILL.md")
			if data, err := os.ReadFile(installed); err != nil || !bytes.Contains(data, []byte("changed inert content")) {
				t.Fatalf("contained ownership control did not update: %q %v", data, err)
			}
			if target, err := os.Readlink(filepath.Join(anchor, directory)); err != nil || target != "metadata" {
				t.Fatalf("contained parent alias replaced: %q %v", target, err)
			}
			check, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes), WithDryRun())
			if err != nil || diag.HasErrors(check.Diags) || !check.InSync() {
				t.Fatalf("contained read-only control failed: %+v %v", check, err)
			}
		})
	}
}

// spec: materialization/read-only-portable-manifest-parent-refusal-is-not-absence
func TestRunReadOnlyPortableManifestParentRefusalIsNotAbsence(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.Symlink("missing-metadata", filepath.Join(repo, ".agents")); err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "portable-probe", Resource: ir.NewSkill("portable-probe", "portable", "inert content", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	report, err := Run(t.Context(), repo, resolved, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}), WithDryRun())
	if err != nil || !hasDiagCode(report.Diags, "materialize.manifest.error") {
		t.Fatalf("unsafe missing parent became absence: %+v %v", report, err)
	}
	if target, err := os.Readlink(filepath.Join(repo, ".agents")); err != nil || target != "missing-metadata" {
		t.Fatalf("read-only manifest refusal changed alias: %q %v", target, err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".factory")); !os.IsNotExist(err) {
		t.Fatalf("read-only manifest refusal created output: %v", err)
	}
}

func TestRunPortableManifestUnsafeShapesDoNotInstall(t *testing.T) {
	for _, machine := range []bool{false, true} {
		for _, shape := range []string{"hard-link", "directory", "escaping-parent"} {
			t.Run(map[bool]string{false: "repository", true: "machine"}[machine]+"/"+shape, func(t *testing.T) {
				home, repo, foreign := t.TempDir(), t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
				t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
				anchor, dirname, source, out := repo, ".agents", config.RepoCommitted, filepath.Join(repo, ".factory")
				if machine {
					anchor, dirname, source, out = home, ".agent-marketplace", config.UserPersonal, filepath.Join(home, ".factory")
				}
				parent := filepath.Join(anchor, dirname)
				payload := filepath.Join(foreign, "materialization-manifest.yaml")
				if err := os.WriteFile(payload, []byte("version: 1\n"), 0o640); err != nil {
					t.Fatal(err)
				}
				before, err := os.Stat(payload)
				if err != nil {
					t.Fatal(err)
				}
				if shape == "escaping-parent" {
					if err := os.Symlink(foreign, parent); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Mkdir(parent, 0o755); err != nil {
						t.Fatal(err)
					}
					manifest := filepath.Join(parent, "materialization-manifest.yaml")
					if shape == "directory" {
						if err := os.Mkdir(manifest, 0o755); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := os.Link(payload, manifest); err != nil {
							t.Fatal(err)
						}
					}
				}
				resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "portable-probe", Resource: ir.NewSkill("portable-probe", "portable", "inert native control", nil, nil, ir.SourceRef{})}, Source: source, Harnesses: []hcc.Target{hcc.TargetFactory}}}
				report, err := Run(t.Context(), repo, resolved, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}))
				if err != nil || !hasDiagCode(report.Diags, "materialize.manifest.error") {
					t.Fatalf("unsafe manifest shape admitted: %+v %v", report, err)
				}
				if _, err := os.Lstat(filepath.Join(out, "skills", "portable-probe", "SKILL.md")); !os.IsNotExist(err) {
					t.Fatalf("unsafe manifest installed native payload: %v", err)
				}
				if data, err := os.ReadFile(payload); err != nil || string(data) != "version: 1\n" {
					t.Fatalf("foreign manifest bytes changed: %q %v", data, err)
				}
				if after, err := os.Stat(payload); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
					t.Fatalf("foreign manifest metadata changed: %v", err)
				}
			})
		}
	}
}
