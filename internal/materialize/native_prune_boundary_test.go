package materialize

import (
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

// spec: materialization/native-managed-link-pruning-refuses-replacement-generation
func TestRunNativeManagedLinkPruneRefusesReplacedLeaf(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "native-prune", Resource: ir.NewSkill("native-prune", "native", "inert native body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	initial, err := Run(t.Context(), repo, resolved)
	if err != nil || diag.HasErrors(initial.Diags) {
		t.Fatalf("native link install control failed: %+v %v", initial, err)
	}
	leaf := filepath.Join(repo, ".factory", "skills", "native-prune")
	if _, err := os.Readlink(leaf); err != nil {
		t.Fatalf("native managed link missing: %v", err)
	}
	hit := false
	keep := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "native-keep", Resource: ir.NewSkill("native-keep", "native", "inert kept body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	report, err := Run(t.Context(), repo, keep, func(cfg *runConfig) {
		cfg.linkPublicationHook = func(path string) {
			if path != leaf {
				return
			}
			hit = true
			if err := os.Rename(leaf, filepath.Join(filepath.Dir(leaf), "retained-link")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(leaf, []byte("handwritten replacement"), 0o640); err != nil {
				t.Fatal(err)
			}
		}
	})
	if !hit {
		t.Fatal("native managed-link prune fixture did not run")
	}
	if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("native link replacement did not refuse: %+v %v", report, err)
	}
	if data, err := os.ReadFile(leaf); err != nil || string(data) != "handwritten replacement" {
		t.Fatalf("native prune removed replacement: %q %v", data, err)
	}
	controlRepo := t.TempDir()
	control, err := Run(t.Context(), controlRepo, resolved)
	if err != nil || diag.HasErrors(control.Diags) {
		t.Fatalf("healthy native link install failed: %+v %v", control, err)
	}
	pruned, err := Run(t.Context(), controlRepo, keep)
	if err != nil || diag.HasErrors(pruned.Diags) {
		t.Fatalf("healthy native prune failed: %+v %v", pruned, err)
	}
	if _, err := os.Lstat(filepath.Join(controlRepo, ".factory", "skills", "native-prune")); !os.IsNotExist(err) {
		t.Fatalf("healthy stale native link was not pruned: %v", err)
	}
}
