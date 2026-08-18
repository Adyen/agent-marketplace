package materialize

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestMaterializePlansRouteCodexAcrossNativeDestinations(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", filepath.Join(repo, "home"))
	t.Setenv("CODEX_HOME", filepath.Join(repo, "custom-codex"))
	target := targetmeta.TargetCodex
	artifacts := []artifact.Artifact{
		{Path: ".agents/skills/demo/SKILL.md", Bytes: []byte("skill")},
		{Path: ".codex/agents/reviewer.toml", Bytes: []byte("agent")},
		{Path: ".codex/hooks.json", Bytes: []byte("{}"), MergeWithExisting: true, ExternalReconciliationRequired: true},
	}

	project := materializePlans(target, repo, artifacts, nil, false)
	canonicalRepo, _ := ownership.CanonicalRoot(repo)
	assertPlan(t, project, filepath.Join(canonicalRepo, ".agents", "skills"), "demo/SKILL.md")
	assertPlan(t, project, filepath.Join(canonicalRepo, ".codex", "agents"), "reviewer.toml")
	assertPlan(t, project, filepath.Join(canonicalRepo, ".codex"), "hooks.json")

	machine := materializePlans(target, repo, artifacts, nil, true)
	assertPlan(t, machine, filepath.Join(canonicalRepo, "home", ".agents", "skills"), "demo/SKILL.md")
	assertPlan(t, machine, filepath.Join(canonicalRepo, "custom-codex", "agents"), "reviewer.toml")
	assertPlan(t, machine, filepath.Join(canonicalRepo, "custom-codex"), "hooks.json")
}

func TestRunWritesCodexSubagentsToNativeDestination(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	target := targetmeta.TargetCodex
	resource := ir.NewSubagent("reviewer", "Review", "Review carefully.", "gpt-5.6-terra", nil, "high", nil, "", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "reviewer", Resource: resource},
		Source:    config.RepoCommitted,
		Harnesses: []targetmeta.Target{target},
	}}
	report, err := Run(context.Background(), repo, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if report == nil || len(report.Diags) != 0 {
		t.Fatalf("report = %#v", report)
	}
	path := filepath.Join(repo, ".codex", "agents", "reviewer.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("native subagent not written: %v", err)
	}
}

func TestRunWritesMachineCodexResourcesAcrossCustomRoots(t *testing.T) {
	repo, home, codexHome := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)
	target := targetmeta.TargetCodex
	skillDir := filepath.Join(repo, "source-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: Demo\n---\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resources := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Name: "demo", Path: skillDir, Resource: ir.NewSkill("demo", "Demo", "Body\n", nil, nil, ir.SourceRef{})}, Source: config.UserPersonal, Harnesses: []targetmeta.Target{target}},
		{Entry: catalog.Entry{Name: "reviewer", Resource: ir.NewSubagent("reviewer", "Review", "Review carefully.", "gpt-5.6-terra", nil, "high", nil, "", nil, ir.SourceRef{})}, Source: config.UserPersonal, Harnesses: []targetmeta.Target{target}},
	}
	report, err := Run(context.Background(), repo, resources)
	if err != nil {
		t.Fatal(err)
	}
	if report == nil || len(report.Diags) != 0 {
		t.Fatalf("report = %#v", report)
	}
	for _, path := range []string{filepath.Join(home, ".agents", "skills", "demo"), filepath.Join(codexHome, "agents", "reviewer.toml")} {
		if _, err := os.Lstat(path); err != nil {
			t.Errorf("native machine resource %s not written: %v", path, err)
		}
	}
}

func TestRunPrunesFileModeSubagentFromCustomCodexHome(t *testing.T) {
	repo, home, codexHome := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)
	resource := ir.NewSubagent("reviewer", "Review", "Review carefully.", "gpt-5.6-terra", nil, "high", nil, "", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "reviewer", Resource: resource}, Source: config.UserPersonal, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}}}
	modes := config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeFile}
	if report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes)); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed: report=%#v err=%v", report, err)
	}
	path := filepath.Join(codexHome, "agents", "reviewer.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("native subagent not written: %v", err)
	}
	report, err := Run(context.Background(), repo, nil, WithMaterializationModes(modes))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("custom CODEX_HOME subagent not pruned: %v", err)
	}
	for _, root := range report.PerRoot {
		if root.OutDir == home {
			t.Fatalf("partitioned report used synthetic HOME root: %#v", report.PerRoot)
		}
	}
}

func TestRunDoesNotApplyOldCodexHomeManifestAtNewRoot(t *testing.T) {
	repo, home, oldHome, newHome := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", oldHome)
	resource := ir.NewSubagent("reviewer", "Review", "Review carefully.", "gpt-5.6-terra", nil, "high", nil, "", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "reviewer", Resource: resource}, Source: config.UserPersonal, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}}}
	modes := config.MaterializationModes{Project: config.MaterializationModeSymlink, Machine: config.MaterializationModeFile}
	if report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes)); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed: report=%#v err=%v", report, err)
	}
	oldPath := filepath.Join(oldHome, "agents", "reviewer.toml")
	raw, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", newHome)
	newPath := filepath.Join(newHome, "agents", "reviewer.toml")
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), repo, nil, WithMaterializationModes(modes)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("old root was touched: %v", err)
	}
	if got, err := os.ReadFile(newPath); err != nil || string(got) != string(raw) {
		t.Fatalf("new root user file changed: %q, %v", got, err)
	}
}

func assertPlan(t *testing.T, plans []materializePlan, outDir, artifactPath string) {
	t.Helper()
	for _, plan := range plans {
		if plan.outDir != outDir {
			continue
		}
		for _, candidate := range plan.artifacts {
			if candidate.Path == artifactPath {
				return
			}
		}
	}
	t.Fatalf("no plan for %s/%s: %#v", outDir, artifactPath, plans)
}
