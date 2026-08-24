package materialize

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func TestRuleTargetInstallDirectoriesAndPlanning(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetFactory, hcc.TargetCursor, hcc.TargetWindsurf} {
		info, ok := harness.TargetInfoMap[target]
		if !ok {
			t.Fatalf("missing target info for %s", target)
		}
		if got := info.ResourceInstallDir(ir.KindRule); got == "" {
			t.Fatalf("%s rule install directory is empty", target)
		}
	}

	plans := materializePlans(hcc.TargetFactory, "/repo/.factory", []artifact.Artifact{{
		Path:  "rules/secure-coding.md",
		Bytes: []byte("# Secure coding\n"),
	}}, nil, false)
	if len(plans) != 1 || plans[0].reportPrefix != "rules" {
		t.Fatalf("Factory rule plans = %#v, want one rules plan", plans)
	}
	if plans[0].outDir != "/repo/.factory/rules" {
		t.Fatalf("Factory rule output = %q, want /repo/.factory/rules", plans[0].outDir)
	}

	plans = materializePlans(hcc.TargetWindsurf, "/repo", []artifact.Artifact{{
		Path:  ".windsurf/rules/secure-coding.mdc",
		Bytes: []byte("---\n---\n"),
	}}, nil, false)
	if len(plans) != 1 || plans[0].outDir != "/repo/.windsurf/rules" {
		t.Fatalf("Windsurf rule output = %q, want .windsurf/rules plan", plans[0].outDir)
	}
}

func TestRunRuleMaterializesAndPrunesFactoryOutput(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	rule := ir.NewRule("secure-coding", "Secure coding", "# Secure coding\n", nil, false, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: rule.Name(), Kind: ir.KindRule, Resource: rule},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}}
	first, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if first.TotalWritten() == 0 {
		t.Fatal("first Run wrote no rule artifact")
	}
	rulePath := filepath.Join(repoRoot, ".factory", "rules", "secure-coding.md")
	if _, err := os.Stat(rulePath); err != nil {
		t.Fatalf("materialized rule missing at %s: %v", rulePath, err)
	}

	second, err := Run(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatalf("prune Run: %v", err)
	}
	if _, err := os.Lstat(rulePath); !os.IsNotExist(err) {
		t.Fatalf("expected stale rule to be pruned, err=%v", err)
	}
	if !slices.Contains(second.PerTarget[hcc.TargetFactory].Removed, filepath.Join("rules", "secure-coding.md")) {
		t.Fatalf("expected rule removal in report, got %v", second.PerTarget[hcc.TargetFactory].Removed)
	}
}
