package materialize

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/portable-manifest-removal-preserves-a-replaced-generation
func TestRunPortableManifestRemovalPreservesReplacedGeneration(t *testing.T) {
	repo, root, resolved := rootBoundaryFixture(t, rootBoundaryCases[1], false)
	initial, err := Run(t.Context(), repo, resolved, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}))
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, initial.Diags)
	installed := filepath.Join(root, "skills", "root-probe")
	if info, err := os.Lstat(installed); err != nil || !info.IsDir() {
		t.Fatalf("file-mode positive control missing: %v %v", info, err)
	}
	manifest := filepath.Join(repo, ".agents", "materialization-manifest.yaml")
	original, err := os.ReadFile(manifest)
	if err != nil || !bytes.Contains(original, []byte("skills/root-probe")) {
		t.Fatalf("native manifest control missing: %q %v", original, err)
	}
	replacement := []byte("version: 1\nentries: []\n")
	var before os.FileInfo
	hit := false
	report, err := Run(t.Context(), repo, resolved, func(cfg *runConfig) {
		cfg.linkPublicationHook = func(path string) {
			if path != installed {
				return
			}
			hit = true
			if err := os.Rename(manifest, filepath.Join(repo, "retained-manifest.yaml")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifest, replacement, 0o640); err != nil {
				t.Fatal(err)
			}
			var err error
			before, err = os.Stat(manifest)
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("mode migration did not reach native staged publication")
	}
	if info, err := os.Lstat(installed); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("native migration positive control did not publish: %v %v", info, err)
	}
	if !hasDiagCode(report.Diags, "materialize.manifest.error") {
		t.Fatalf("replacement manifest removal did not refuse: %+v", report.Diags)
	}
	if data, err := os.ReadFile(manifest); err != nil || !bytes.Equal(data, replacement) {
		t.Fatalf("removed replacement manifest: %q %v", data, err)
	}
	if after, err := os.Stat(manifest); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("replacement manifest metadata changed: %v", err)
	}
}

// spec: materialization/portable-manifest-publication-preserves-a-replaced-generation
func TestRunPortableManifestPublicationPreservesReplacedGeneration(t *testing.T) {
	repo, root, resolved := rootBoundaryFixture(t, rootBoundaryCases[1], false)
	retained := resolved[0]
	retained.Name = "retained-probe"
	retained.Resource = ir.NewSkill("retained-probe", "retained control", "inert retained body", nil, nil, ir.SourceRef{})
	initial, err := Run(t.Context(), repo, append(resolved, retained), WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}))
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, initial.Diags)
	userFile := filepath.Join(root, "skills", "retained-probe", "SKILL.md")
	userBytes, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatal(err)
	}
	userBytes = append(userBytes, []byte("\ninert handwritten edit\n")...)
	if err := os.WriteFile(userFile, userBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(repo, ".agents", "materialization-manifest.yaml")
	replacement := []byte("version: 1\nentries: []\n")
	var before os.FileInfo
	hit := false
	installed := filepath.Join(root, "skills", "root-probe")
	report, err := Run(t.Context(), repo, resolved, func(cfg *runConfig) {
		cfg.linkPublicationHook = func(path string) {
			if path != installed {
				return
			}
			hit = true
			if err := os.Rename(manifest, filepath.Join(repo, "retained-manifest.yaml")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifest, replacement, 0o640); err != nil {
				t.Fatal(err)
			}
			var err error
			before, err = os.Stat(manifest)
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("mode migration did not reach native staged publication")
	}
	if info, err := os.Lstat(installed); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("native migration positive control did not publish: %v %v", info, err)
	}
	if !hasDiagCode(report.Diags, "materialize.manifest.error") {
		t.Fatalf("replacement manifest publication did not refuse: %+v", report.Diags)
	}
	if data, err := os.ReadFile(manifest); err != nil || !bytes.Equal(data, replacement) {
		t.Fatalf("overwrote replacement manifest: %q %v", data, err)
	}
	if after, err := os.Stat(manifest); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("replacement manifest metadata changed: %v", err)
	}
	if data, err := os.ReadFile(userFile); err != nil || !bytes.Equal(data, userBytes) {
		t.Fatalf("retained native user content changed: %q %v", data, err)
	}
}

// spec: materialization/portable-manifest-repeats-preserve-identity-and-update-permissions
func TestRunPortableManifestRepeatKeepsIdentityModeAndUpdates(t *testing.T) {
	for _, machine := range []bool{false, true} {
		t.Run(map[bool]string{false: "repository", true: "machine"}[machine], func(t *testing.T) {
			repo, root, resolved := rootBoundaryFixture(t, rootBoundaryCases[1], machine)
			modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
			initial, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, initial.Diags)
			manifest := filepath.Join(repo, ".agents", "materialization-manifest.yaml")
			if machine {
				manifest = filepath.Join(os.Getenv("HOME"), ".agent-marketplace", "materialization-manifest.yaml")
			}
			if err := os.Chmod(manifest, 0o640); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(manifest)
			if err != nil {
				t.Fatal(err)
			}
			oldBytes, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			repeat, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, repeat.Diags)
			if !repeat.InSync() {
				t.Fatalf("native repeat control had drift: %+v", repeat)
			}
			if after, err := os.Stat(manifest); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("unchanged manifest was rewritten: %v", err)
			}
			resolved[0].Resource = ir.NewSkill("root-probe", "root control", "updated inert body", nil, nil, ir.SourceRef{})
			update, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, update.Diags)
			if data, err := os.ReadFile(filepath.Join(root, "skills", "root-probe", "SKILL.md")); err != nil || !bytes.Contains(data, []byte("updated inert body")) {
				t.Fatalf("native update did not occur: %q %v", data, err)
			}
			if after, err := os.Stat(manifest); err != nil || after.Mode().Perm() != 0o640 || os.SameFile(before, after) {
				t.Fatalf("manifest update lost permissions or did not publish: %v", err)
			}
			if data, err := os.ReadFile(manifest); err != nil || bytes.Equal(data, oldBytes) {
				t.Fatalf("manifest update did not record changed native ownership: %q %v", data, err)
			}
			removed, err := Run(t.Context(), repo, nil, WithMaterializationModes(modes))
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, removed.Diags)
			if _, err := os.Lstat(manifest); !os.IsNotExist(err) {
				t.Fatalf("valid empty manifest removal failed: %v", err)
			}
		})
	}
}

// spec: materialization/portable-manifest-staging-keeps-captured-namespace-authority
func TestRunPortableManifestStagingRefusesNamespaceReplacement(t *testing.T) {
	for _, machine := range []bool{false, true} {
		for _, shape := range []string{"leaf", "parent"} {
			t.Run(map[bool]string{false: "repository", true: "machine"}[machine]+"/"+shape, func(t *testing.T) {
				repo, root, resolved := rootBoundaryFixture(t, rootBoundaryCases[1], machine)
				modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
				initial, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
				if err != nil {
					t.Fatal(err)
				}
				requireNoErrorDiags(t, initial.Diags)
				anchor, parentName := repo, ".agents"
				if machine {
					anchor, parentName = os.Getenv("HOME"), ".agent-marketplace"
				}
				parent := filepath.Join(anchor, parentName)
				manifest := filepath.Join(parent, "materialization-manifest.yaml")
				retained := filepath.Join(anchor, "captured-metadata")
				outside := t.TempDir()
				replacement := []byte("version: 1\nentries: []\n")
				target := manifest
				var before os.FileInfo
				hit := false
				resolved[0].Resource = ir.NewSkill("root-probe", "root control", "updated inert staged body", nil, nil, ir.SourceRef{})
				report, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes), func(cfg *runConfig) {
					cfg.manifestPublicationHook = func(path string) {
						if path != manifest {
							return
						}
						hit = true
						entries, err := os.ReadDir(parent)
						if err != nil {
							t.Fatal(err)
						}
						staged := false
						for _, entry := range entries {
							staged = staged || strings.HasPrefix(entry.Name(), ".materialization-manifest-")
						}
						if !staged {
							t.Fatal("consumer did not stage the changed manifest")
						}
						if shape == "parent" {
							if err := os.Rename(parent, retained); err != nil {
								t.Fatal(err)
							}
							target = filepath.Join(outside, "materialization-manifest.yaml")
							if err := os.WriteFile(target, replacement, 0o640); err != nil {
								t.Fatal(err)
							}
							if err := os.Symlink(outside, parent); err != nil {
								t.Fatal(err)
							}
						} else {
							if err := os.Rename(manifest, filepath.Join(anchor, "captured-manifest.yaml")); err != nil {
								t.Fatal(err)
							}
							if err := os.WriteFile(target, replacement, 0o640); err != nil {
								t.Fatal(err)
							}
						}
						before, err = os.Stat(target)
						if err != nil {
							t.Fatal(err)
						}
					}
				})
				if err != nil {
					t.Fatal(err)
				}
				if !hit || !hasDiagCode(report.Diags, "materialize.manifest.error") {
					t.Fatalf("staged namespace replacement did not refuse: %v %+v", hit, report.Diags)
				}
				if data, err := os.ReadFile(filepath.Join(root, "skills", "root-probe", "SKILL.md")); err != nil || !bytes.Contains(data, []byte("updated inert staged body")) {
					t.Fatalf("native update positive control missing: %q %v", data, err)
				}
				if data, err := os.ReadFile(target); err != nil || !bytes.Equal(data, replacement) {
					t.Fatalf("staged manifest changed replacement: %q %v", data, err)
				}
				if after, err := os.Stat(target); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
					t.Fatalf("staged manifest changed replacement metadata: %v", err)
				}
				if shape == "parent" {
					if link, err := os.Readlink(parent); err != nil || link != outside {
						t.Fatalf("staged manifest replaced parent alias: %q %v", link, err)
					}
				}
				dirs := []string{parent}
				if shape == "parent" {
					dirs = append(dirs, retained)
				}
				for _, dir := range dirs {
					entries, err := os.ReadDir(dir)
					if err != nil {
						t.Fatal(err)
					}
					for _, entry := range entries {
						if strings.HasPrefix(entry.Name(), ".materialization-manifest-") {
							t.Fatalf("attempt-owned staging remained in %s: %s", dir, entry.Name())
						}
					}
				}
			})
		}
	}
}

// spec: materialization/missing-captured-manifest-parent-stays-absent-on-refusal
func TestRunPortableManifestMissingParentDoesNotRecreate(t *testing.T) {
	repo, root, resolved := rootBoundaryFixture(t, rootBoundaryCases[1], false)
	retained := resolved[0]
	retained.Name = "retained-probe"
	retained.Resource = ir.NewSkill("retained-probe", "retained control", "inert retained body", nil, nil, ir.SourceRef{})
	initial, err := Run(t.Context(), repo, append(resolved, retained), WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}))
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, initial.Diags)
	userFile := filepath.Join(root, "skills", "retained-probe", "SKILL.md")
	data, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userFile, append(data, []byte("\ninert handwritten edit\n")...), 0o640); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(repo, ".agents")
	captured := filepath.Join(repo, "captured-metadata")
	manifest := filepath.Join(parent, "materialization-manifest.yaml")
	before, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	hit := false
	report, err := Run(t.Context(), repo, resolved, func(cfg *runConfig) {
		cfg.linkPublicationHook = func(path string) {
			if path != filepath.Join(root, "skills", "root-probe") {
				return
			}
			hit = true
			if err := os.Rename(parent, captured); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hit || !hasDiagCode(report.Diags, "materialize.manifest.error") {
		t.Fatalf("missing loaded parent did not refuse: %v %+v", hit, report.Diags)
	}
	if _, err := os.Lstat(parent); !os.IsNotExist(err) {
		t.Fatalf("refusal recreated the replaced manifest parent: %v", err)
	}
	if after, err := os.ReadFile(filepath.Join(captured, "materialization-manifest.yaml")); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("refusal changed captured manifest: %q %v", after, err)
	}
}

func TestRunPortableManifestEmptySaveRefusesAppearedLeaf(t *testing.T) {
	repo, root, resolved := rootBoundaryFixture(t, rootBoundaryCases[1], false)
	parent := filepath.Join(repo, ".agents")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(parent, "materialization-manifest.yaml")
	appeared := []byte("version: 1\nentries: []\n")
	var before os.FileInfo
	hit := false
	report, err := Run(t.Context(), repo, resolved, func(cfg *runConfig) {
		cfg.linkPublicationHook = func(path string) {
			if path != filepath.Join(root, "skills", "root-probe") {
				return
			}
			hit = true
			if err := os.WriteFile(manifest, appeared, 0o640); err != nil {
				t.Fatal(err)
			}
			var err error
			before, err = os.Stat(manifest)
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("native update did not reach staged publication")
	}
	if data, err := os.ReadFile(filepath.Join(root, "skills", "root-probe", "SKILL.md")); err != nil || !bytes.Contains(data, []byte("inert body")) {
		t.Fatalf("native publication positive control missing: %q %v", data, err)
	}
	if !hasDiagCode(report.Diags, "materialize.manifest.error") {
		t.Fatalf("appeared manifest was silently accepted: %+v", report.Diags)
	}
	if data, err := os.ReadFile(manifest); err != nil || !bytes.Equal(data, appeared) {
		t.Fatalf("empty save changed appeared manifest: %q %v", data, err)
	}
	if after, err := os.Stat(manifest); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("empty save changed appeared manifest metadata: %v", err)
	}
}
