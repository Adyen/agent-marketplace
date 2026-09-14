package materialize

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func TestRuleTargetInstallDirectoriesAndPlanning(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetFactory, hcc.TargetCursor, hcc.TargetWindsurf, hcc.TargetDevin} {
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
		Path:  ".windsurf/rules/secure-coding.md",
		Bytes: []byte("---\n---\n"),
	}}, nil, false)
	if len(plans) != 1 || plans[0].outDir != "/repo/.windsurf/rules" {
		t.Fatalf("Windsurf rule output = %q, want .windsurf/rules plan", plans[0].outDir)
	}

	plans = materializePlans(hcc.TargetDevin, "/repo/.devin", []artifact.Artifact{{
		Path: "rules/secure-coding.md",
	}}, nil, false)
	if len(plans) != 1 || plans[0].outDir != filepath.Join("/repo/.devin", "rules") {
		t.Fatalf("Devin project rule plans = %#v, want .devin/rules", plans)
	}
	machinePlans := materializePlans(hcc.TargetDevin, "/home/.config/devin", []artifact.Artifact{{
		Path: "rules/secure-coding.md",
	}}, nil, true)
	if len(machinePlans) != 0 {
		t.Fatalf("Devin machine Rule plans = %#v, want no plans", machinePlans)
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

func TestRunDevinRuleProjectMaterializesAndMachineScopeIsUnsupported(t *testing.T) {
	home, repoRoot := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	rule := ir.NewRule("secure-coding", "Security", "# Secure\n", nil, false, nil, ir.SourceRef{})

	projectReport, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{{
		Entry:  catalog.Entry{Name: rule.Name(), Kind: ir.KindRule, Resource: rule},
		Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetDevin},
	}})
	if err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(repoRoot, ".devin", "rules", "secure-coding.md")
	data, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("read project Devin Rule: %v; roots=%#v", err, projectReport.PerRoot)
	}
	if string(data) == rule.Body {
		t.Fatalf("project Devin Rule lost trigger frontmatter")
	}

	machineSkill := ir.NewSkill("sibling", "Sibling", "body", nil, nil, ir.SourceRef{})
	machineRulesDir := filepath.Join(home, ".config", "devin", "rules")
	if err := os.MkdirAll(machineRulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(machineRulesDir, "handwritten.md")
	sentinelBytes := []byte("handwritten machine Devin content\n")
	if err := os.WriteFile(sentinel, sentinelBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	machineReport, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{
		{
			Entry:  catalog.Entry{Name: rule.Name(), Kind: ir.KindRule, Resource: rule},
			Source: config.UserPersonal, Harnesses: []hcc.Target{hcc.TargetDevin},
		},
		{
			Entry:  catalog.Entry{Name: machineSkill.Name(), Kind: ir.KindSkill, Resource: machineSkill},
			Source: config.UserPersonal, Harnesses: []hcc.Target{hcc.TargetDevin},
		},
	}, WithMaterializationModes(config.MaterializationModes{
		Project: config.MaterializationModeFile,
		Machine: config.MaterializationModeFile,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := findDiag(machineReport.Diags, "rule.devin.unsupported"); !ok || d.Severity != diag.SeverityWarning {
		t.Fatalf("machine Devin Rule diagnostic = %#v, want warning rule.devin.unsupported", machineReport.Diags)
	}
	warningCount := 0
	for _, d := range machineReport.Diags {
		if d.Code == diag.CodeRuleDevinUnsupported {
			warningCount++
		}
	}
	if warningCount != 1 {
		t.Fatalf("machine Devin Rule diagnostics = %#v, want exactly one unsupported warning", machineReport.Diags)
	}
	for _, path := range []string{
		filepath.Join(home, ".devin", "rules", "secure-coding.md"),
		filepath.Join(home, ".config", "devin", "rules", "secure-coding.md"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("machine Devin Rule unexpectedly materialized at %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "devin", "skills", "sibling", "SKILL.md")); err != nil {
		t.Fatalf("machine sibling skill was not materialized: %v", err)
	}
	if got, err := os.ReadFile(sentinel); err != nil {
		t.Fatalf("machine Devin Rule sentinel was inspected or removed: %v", err)
	} else if string(got) != string(sentinelBytes) {
		t.Fatalf("machine Devin Rule sentinel changed: %q", got)
	}
}

func TestRunMachineRulesUseDeclaredHarnessRoots(t *testing.T) {
	tests := []struct {
		target hcc.Target
		rel    string
	}{
		{hcc.TargetFactory, filepath.Join(".factory", "rules", "machine-rule.md")},
		{hcc.TargetCursor, filepath.Join(".cursor", "rules", "machine-rule.mdc")},
		{hcc.TargetWindsurf, filepath.Join(".codeium", "windsurf", "rules", "machine-rule.md")},
	}
	for _, tt := range tests {
		t.Run(string(tt.target), func(t *testing.T) {
			home, repoRoot := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			rule := ir.NewRule("machine-rule", "Machine rule", "# Machine\n", nil, false, nil, ir.SourceRef{})
			report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{{
				Entry:  catalog.Entry{Name: rule.Name(), Kind: ir.KindRule, Resource: rule},
				Source: config.UserPersonal, Harnesses: []hcc.Target{tt.target},
			}})
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, report.Diags)
			path := filepath.Join(home, filepath.FromSlash(tt.rel))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read machine Rule at %s: %v", path, err)
			}
			if !strings.Contains(string(data), rule.Body) {
				t.Fatalf("machine Rule at %s lost body: %q", path, data)
			}
		})
	}
}
