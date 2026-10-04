package materialize

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
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

func TestCodexMaterializePlansDoNotPruneLinkedSkills(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	canonicalRepo, err := ownership.CanonicalRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	skillsDir := filepath.Join(canonicalRepo, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	linked := linkedComponents(map[string]*skillLinkGroup{
		".agents/skills": {installDirRel: ".agents/skills"},
	})
	plans := materializePlans(targetmeta.TargetCodex, repo, nil, linked, false)
	for _, plan := range plans {
		if plan.outDir == skillsDir {
			if plan.prune {
				t.Fatal("Codex linked-skills plan prunes live skill links")
			}
			return
		}
	}
	t.Fatalf("no Codex skills plan for %s", skillsDir)
}

func TestRunPreservesCodexSkillsWhenAnotherSkillIsSuppressed(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	resolved := func(name string, resource ir.Resource) resolver.ResolvedArtifact {
		return resolver.ResolvedArtifact{
			Entry:     catalog.Entry{Name: name, Resource: resource},
			Source:    config.RepoCommitted,
			Harnesses: []targetmeta.Target{targetmeta.TargetCodex},
		}
	}

	stale := resolved("stale", ir.NewSkill("stale", "stale", "Body\n", nil, nil, ir.SourceRef{}))
	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{stale}); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	stalePath := filepath.Join(repo, ".agents", "skills", "stale")
	if _, err := os.Lstat(stalePath); err != nil {
		t.Fatalf("seed skill was not materialized: %v", err)
	}

	restricted := ir.NewSkillWithOptions("restricted", "restricted", "Body\n", nil, nil, ir.SourceRef{},
		ir.WithSkillAllowedTools([]string{"read"}))
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved("restricted", restricted)})
	if err != nil {
		t.Fatalf("suppressed Run: %v", err)
	}
	if _, ok := findDiag(report.Diags, diag.CodeSkillCodexSecurityRestrictionUnsupported); !ok {
		t.Fatalf("missing suppressed-skill diagnostic: %v", report.Diags)
	}
	if _, err := os.Lstat(stalePath); err != nil {
		t.Fatalf("suppressed Codex skill pruned the existing skills component: %v", err)
	}
}

func TestCodexDryRunLabelsSelectedSkillAndCommandDriftWithAgentsRoot(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	selection := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "repo-skill", Resource: ir.NewSkill("repo-skill", "skill", "Body\n", nil, nil, ir.SourceRef{})},
			Source:    config.RepoCommitted,
			Harnesses: []targetmeta.Target{targetmeta.TargetCodex},
		},
		{
			Entry:     catalog.Entry{Name: "repo-command", Resource: ir.NewCommand("repo-command", "command", "Body\n", nil, ir.SourceRef{})},
			Source:    config.RepoCommitted,
			Harnesses: []targetmeta.Target{targetmeta.TargetCodex},
		},
	}
	if _, err := Run(context.Background(), repo, selection); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	for _, name := range []string{"repo-skill", "repo-command"} {
		if err := os.Remove(filepath.Join(repo, ".agents", "skills", name)); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Run(context.Background(), repo, selection, WithDryRun())
	if err != nil {
		t.Fatalf("dry Run: %v", err)
	}
	var written []string
	for _, tr := range report.PerRoot {
		if tr.RootID == "repo-agents" {
			written = append(written, tr.Written...)
		}
	}
	for _, name := range []string{"repo-skill", "repo-command"} {
		if !slices.Contains(written, name) {
			t.Fatalf("repo-agents Written = %v, missing %q (reports = %#v)", written, name, report.PerRoot)
		}
	}
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

func TestRunExpandsCodexPluginRootFromBundle(t *testing.T) {
	repo, home, state, bundleRoot := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", state)
	hookPath := filepath.Join(bundleRoot, "hooks", "hooks.json")
	hookCommand := "${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"
	hookDefinition := fmt.Sprintf(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":%q}]}]}`, hookCommand)
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookPath, []byte(hookDefinition), 0o644); err != nil {
		t.Fatal(err)
	}
	hookScript := filepath.Join(bundleRoot, "hooks", "scripts", "check.sh")
	if err := os.MkdirAll(filepath.Dir(hookScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookScript, []byte("echo codex bundle\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(bundleRoot, ".mcp.json")
	if err := os.WriteFile(mcpPath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	hook := ir.NewHookWithMatcherOptions("check", "PreToolUse", "Bash", hookCommand, "command", nil, ir.SourceRef{File: "hooks/hooks.json"}, ir.WithHookSource("claude"))
	mcp := ir.NewMcpServer("docs", "node", []string{"${CLAUDE_PLUGIN_ROOT}/server.js"}, nil, nil, ir.SourceRef{File: ".mcp.json"})
	resolved := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Name: hook.Name(), Path: hookPath, Resource: hook, SourceHarness: "claude"}, RegistryURL: bundleRoot, CatalogKey: bundleRoot, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}},
		{Entry: catalog.Entry{Name: mcp.Name(), Path: mcpPath, Resource: mcp}, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}},
	}
	if report, err := Run(context.Background(), repo, resolved); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Run: report=%#v err=%v", report, err)
	}
	loc := nativeSupportLocationFor(t, hcc.Target(targetmeta.TargetCodex))
	hookFile := filepath.Join(repo, ".codex", "hooks.json")
	commands := nativeSupportCommands(t, loc, hookFile, 1)
	installed, ref := loc.installedRef(t, commands[0], repo, home, false)
	wantCommand := loc.repoBase + filepath.ToSlash(ref)
	if commands[0] != wantCommand {
		t.Fatalf("Codex hook command = %q, want installed support path %q", commands[0], wantCommand)
	}
	if data, err := os.ReadFile(installed); err != nil || string(data) != "echo codex bundle\n" {
		t.Fatalf("Codex support script = %q (%v)", data, err)
	}
	mcpRaw, err := os.ReadFile(filepath.Join(repo, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mcpRaw), "${CLAUDE_PLUGIN_ROOT}") || !strings.Contains(string(mcpRaw), filepath.ToSlash(bundleRoot)) {
		t.Errorf(".codex/config.toml did not expand plugin root from bundle:\n%s", mcpRaw)
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

func TestRunPrunesSymlinkModeCodexSkillWithoutCodexRoot(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "")

	skillDir := filepath.Join(repo, "source-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: Demo\n---\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "demo", Path: skillDir, Resource: ir.NewSkill("demo", "Demo", "Body\n", nil, nil, ir.SourceRef{})},
		Source:    config.RepoCommitted,
		Harnesses: []targetmeta.Target{targetmeta.TargetCodex},
	}}
	if report, err := Run(context.Background(), repo, resolved); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed: report=%#v err=%v", report, err)
	}
	link := filepath.Join(repo, ".agents", "skills", "demo")
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("native skill symlink not written: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("skill-only sync unexpectedly created .codex: %v", err)
	}

	if report, err := Run(context.Background(), repo, nil); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("deinit: report=%#v err=%v", report, err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("native skill symlink not pruned: %v", err)
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
