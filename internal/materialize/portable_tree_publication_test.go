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

// spec: materialization/portable-tree-publication-preserves-replacement-generation
func TestRunPortableTreePublicationRefusesReplacedTree(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "tree-probe", Resource: ir.NewSkill("tree-probe", "portable", "original inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}
	initial, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || diag.HasErrors(initial.Diags) {
		t.Fatalf("native tree control failed: %+v %v", initial, err)
	}
	tree := filepath.Join(repo, ".factory", "skills", "tree-probe")
	before, err := os.ReadFile(filepath.Join(tree, "SKILL.md"))
	if err != nil || !bytes.Contains(before, []byte("original inert body")) {
		t.Fatalf("native tree control missing: %q %v", before, err)
	}
	resolved[0].Resource = ir.NewSkill("tree-probe", "portable", "valid updated inert body", nil, nil, ir.SourceRef{})
	updated, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || diag.HasErrors(updated.Diags) {
		t.Fatalf("valid native tree update failed: %+v %v", updated, err)
	}
	before, err = os.ReadFile(filepath.Join(tree, "SKILL.md"))
	if err != nil || !bytes.Contains(before, []byte("valid updated inert body")) {
		t.Fatalf("native tree update did not occur: %q %v", before, err)
	}
	manifest := filepath.Join(repo, ".agents", "materialization-manifest.yaml")
	manifestBefore, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(filepath.Dir(tree), "retained-tree")
	resolved[0].Resource = ir.NewSkill("tree-probe", "portable", "changed inert body", nil, nil, ir.SourceRef{})
	hit := false
	report, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes), func(cfg *runConfig) {
		cfg.linkPublicationHook = func(path string) {
			if path != tree {
				return
			}
			hit = true
			if err := os.Rename(tree, retained); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(tree, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tree, "SKILL.md"), []byte("handwritten replacement"), 0o640); err != nil {
				t.Fatal(err)
			}
		}
	})
	if !hit {
		t.Fatal("native portable tree publication fixture did not run")
	}
	if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("replacement tree did not refuse: %+v %v", report, err)
	}
	if after, err := os.ReadFile(filepath.Join(tree, "SKILL.md")); err != nil || string(after) != "handwritten replacement" {
		t.Fatalf("publication overwrote replacement tree: %q %v", after, err)
	}
	if after, err := os.ReadFile(filepath.Join(retained, "SKILL.md")); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("publication changed admitted tree: %q %v", after, err)
	}
	if after, err := os.ReadFile(manifest); err != nil || !bytes.Equal(after, manifestBefore) {
		t.Fatalf("tree refusal changed manifest: %q %v", after, err)
	}
	if residue, err := filepath.Glob(filepath.Join(filepath.Dir(tree), ".agent-marketplace-portable-tree-*")); err != nil || len(residue) != 0 {
		t.Fatalf("tree staging residue survived: %v %v", residue, err)
	}
}
