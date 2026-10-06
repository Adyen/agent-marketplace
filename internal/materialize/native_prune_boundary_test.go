package materialize

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
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

// spec: materialization/native-managed-link-pruning-refuses-replacement-generation
func TestRunNativeManagedLinkPruneClassifiedReplacement(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprint("dry-run=", dryRun), func(t *testing.T) {
			home, repo := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "stale", Resource: ir.NewSkill("stale", "native", "inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
			initial, err := Run(t.Context(), repo, resolved)
			if err != nil || diag.HasErrors(initial.Diags) {
				t.Fatalf("install: %+v %v", initial, err)
			}
			leaf := filepath.Join(repo, ".factory", "skills", "stale")
			text := filepath.Join(cache.CacheDir(), "another-sync-generation")
			var replacement os.FileInfo
			opts := []RunOption{func(cfg *runConfig) {
				cfg.pruneClassificationHook = func(path string) {
					if path != leaf {
						return
					}
					if err := os.Rename(leaf, filepath.Join(repo, "retained-original")); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(text, leaf); err != nil {
						t.Fatal(err)
					}
					var err error
					replacement, err = os.Lstat(leaf)
					if err != nil {
						t.Fatal(err)
					}
				}
			}}
			if dryRun {
				opts = append(opts, WithDryRun())
			}
			report, err := Run(t.Context(), repo, nil, opts...)
			if err != nil || replacement == nil || !hasDiagCode(report.Diags, "materialize.write.error") {
				t.Fatalf("native coded refusal missing: %+v %v", report, err)
			}
			for _, tr := range report.PerRoot {
				if slices.Contains(tr.Removed, "stale") || slices.Contains(tr.Removed, "skills/stale") {
					t.Fatalf("refused object reported removed: %+v", tr)
				}
			}
			current, statErr := os.Lstat(leaf)
			got, linkErr := os.Readlink(leaf)
			if statErr != nil || linkErr != nil || got != text || !os.SameFile(current, replacement) || current.Mode() != replacement.Mode() || !current.ModTime().Equal(replacement.ModTime()) {
				t.Fatalf("native replacement changed: %q %v %v", got, statErr, linkErr)
			}
		})
	}
}
