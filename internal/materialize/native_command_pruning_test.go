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

// spec: materialization/native-command-pruning-preserves-replacement
func TestRunNativeCommandPruneRefusesReplacement(t *testing.T) {
	for _, grouped := range []bool{false, true} {
		t.Run(map[bool]string{false: "uncovered", true: "grouped"}[grouped], func(t *testing.T) {
			home, repo := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			original := resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "stale-command", Resource: ir.NewCommand("stale-command", "prune", "original inert body", nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}
			installed, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{original})
			if err != nil || diag.HasErrors(installed.Diags) {
				t.Fatalf("native command install failed: %+v %v", installed, err)
			}
			leaf := filepath.Join(repo, ".factory", "commands", "stale-command.md")
			before, err := os.ReadFile(leaf)
			if err != nil || !bytes.Contains(before, []byte("original inert body")) {
				t.Fatalf("native command control missing: %q %v", before, err)
			}
			resource := ir.Resource(ir.NewSkill("kept", "prune", "kept inert body", nil, nil, ir.SourceRef{}))
			if grouped {
				resource = ir.NewCommand("kept", "prune", "kept inert body", nil, ir.SourceRef{})
			}
			keep := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "kept", Resource: resource}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
			retained := filepath.Join(t.TempDir(), "retained.md")
			hit := false
			report, err := Run(t.Context(), repo, keep, func(cfg *runConfig) {
				cfg.linkPublicationHook = func(path string) {
					if path != leaf {
						return
					}
					hit = true
					if err := os.Rename(leaf, retained); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(leaf, []byte("handwritten replacement"), 0o640); err != nil {
						t.Fatal(err)
					}
				}
			})
			if !hit {
				t.Fatal("native command prune fixture did not run")
			}
			if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
				t.Fatalf("native command prune did not refuse: %+v %v", report, err)
			}
			if data, err := os.ReadFile(leaf); err != nil || string(data) != "handwritten replacement" {
				t.Fatalf("prune removed user bytes: %q %v", data, err)
			}
			if data, err := os.ReadFile(retained); err != nil || !bytes.Equal(data, before) {
				t.Fatalf("prune changed admitted bytes: %q %v", data, err)
			}
			controlRepo := t.TempDir()
			control, err := Run(t.Context(), controlRepo, []resolver.ResolvedArtifact{original})
			if err != nil || diag.HasErrors(control.Diags) {
				t.Fatalf("healthy native prune install failed: %+v %v", control, err)
			}
			pruned, err := Run(t.Context(), controlRepo, keep)
			if err != nil || diag.HasErrors(pruned.Diags) {
				t.Fatalf("healthy native prune failed: %+v %v", pruned, err)
			}
			if _, err := os.Lstat(filepath.Join(controlRepo, ".factory", "commands", "stale-command.md")); !os.IsNotExist(err) {
				t.Fatalf("healthy stale command was not pruned: %v", err)
			}
		})
	}
}
