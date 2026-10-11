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

// spec: materialization/portable-pruning-refuses-replacement-generation
func TestRunPortablePruneRefusesReplacedTree(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "prune-probe", Resource: ir.NewSkill("prune-probe", "portable", "installed inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}
	installed, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || diag.HasErrors(installed.Diags) {
		t.Fatalf("native prune install control failed: %+v %v", installed, err)
	}
	tree := filepath.Join(repo, ".factory", "skills", "prune-probe")
	before, err := os.ReadFile(filepath.Join(tree, "SKILL.md"))
	if err != nil || !bytes.Contains(before, []byte("installed inert body")) {
		t.Fatalf("native prune positive control missing: %q %v", before, err)
	}
	manifest := filepath.Join(repo, ".agents", "materialization-manifest.yaml")
	manifestBefore, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(filepath.Dir(tree), "retained-tree")
	hit := false
	report, err := Run(t.Context(), repo, nil, WithMaterializationModes(modes), func(cfg *runConfig) {
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
		t.Fatal("native manifest-driven prune fixture did not run")
	}
	if err != nil || !hasDiagCode(report.Diags, "materialize.manifest.error") {
		t.Fatalf("portable prune replacement did not refuse: %+v %v", report, err)
	}
	if after, err := os.ReadFile(filepath.Join(tree, "SKILL.md")); err != nil || string(after) != "handwritten replacement" {
		t.Fatalf("prune removed replacement tree: %q %v", after, err)
	}
	if after, err := os.ReadFile(filepath.Join(retained, "SKILL.md")); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("prune changed admitted tree: %q %v", after, err)
	}
	if after, err := os.ReadFile(manifest); err != nil || !bytes.Equal(after, manifestBefore) {
		t.Fatalf("prune refusal changed manifest: %q %v", after, err)
	}
	controlRepo := t.TempDir()
	control, err := Run(t.Context(), controlRepo, resolved, WithMaterializationModes(modes))
	if err != nil || diag.HasErrors(control.Diags) {
		t.Fatalf("healthy prune install control failed: %+v %v", control, err)
	}
	pruned, err := Run(t.Context(), controlRepo, nil, WithMaterializationModes(modes))
	if err != nil || diag.HasErrors(pruned.Diags) {
		t.Fatalf("healthy prune failed: %+v %v", pruned, err)
	}
	if _, err := os.Lstat(filepath.Join(controlRepo, ".factory", "skills", "prune-probe")); !os.IsNotExist(err) {
		t.Fatalf("healthy owned tree was not pruned: %v", err)
	}
}
