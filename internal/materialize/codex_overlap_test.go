package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestRunRejectsCodexWritesWhenRepoIsHome(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", repo)
	t.Setenv("CODEX_HOME", "")

	selection := []resolver.ResolvedArtifact{
		codexSubagent("repo-reviewer", "Review repository changes.", config.RepoCommitted),
		codexSubagent("machine-reviewer", "Review machine changes.", config.UserPersonal),
	}
	report, err := Run(context.Background(), repo, selection)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findDiag(report.Diags, diag.CodeMaterializeDestinationOverlap); !ok {
		t.Fatalf("missing Codex root overlap diagnostic: %v", report.Diags)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("overlap preflight mutated the Codex destination: %v", err)
	}
}

func TestRunRejectsByteIdenticalCodexWritesAcrossAliasedScopes(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", repo)
	t.Setenv("CODEX_HOME", "")

	resource := ir.NewSubagent("reviewer", "Review", "Review carefully.", "gpt-5.6-terra", nil, "high", nil, "", nil, ir.SourceRef{})
	base := resolver.ResolvedArtifact{
		Entry:     catalog.Entry{Name: "reviewer", Resource: resource},
		Source:    config.RepoCommitted,
		Harnesses: []targetmeta.Target{targetmeta.TargetCodex},
	}
	machine := base
	machine.Source = config.UserPersonal
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{base, machine})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findDiag(report.Diags, diag.CodeMaterializeDestinationOverlap); !ok {
		t.Fatalf("missing Codex root overlap diagnostic: %v", report.Diags)
	}
}

func TestRunRefusesConflictingCodexWritesBeforeMutation(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", repo)
	t.Setenv("CODEX_HOME", "")

	selection := []resolver.ResolvedArtifact{
		codexSubagent("reviewer", "Repository definition.", config.RepoCommitted),
		codexSubagent("reviewer", "Machine definition.", config.UserPersonal),
	}
	report, err := Run(context.Background(), repo, selection)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := findDiag(report.Diags, diag.CodeMaterializeDestinationOverlap)
	if !ok {
		t.Fatalf("missing %s diagnostic: %+v", diag.CodeMaterializeDestinationOverlap, report.Diags)
	}
	if d.Hint == "" {
		t.Fatalf("overlap diagnostic is not actionable: %+v", d)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("conflicting preflight mutated the Codex destination: %v", err)
	}
}

func TestRunRejectsCustomCodexHomeOverlappingProjectPartition(t *testing.T) {
	repo, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(repo, ".codex"))
	selection := []resolver.ResolvedArtifact{
		codexSubagent("repo-reviewer", "Review repository changes.", config.RepoCommitted),
		codexSubagent("machine-reviewer", "Review machine changes.", config.UserPersonal),
	}
	report, err := Run(context.Background(), repo, selection)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findDiag(report.Diags, diag.CodeMaterializeDestinationOverlap); !ok {
		t.Fatalf("missing Codex cross-scope root overlap diagnostic: %v", report.Diags)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("overlap preflight mutated the Codex destination: %v", err)
	}
}

func TestRunRejectsCodexPartitionRootsOverlappingAcrossScopes(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", repo)
	t.Setenv("CODEX_HOME", t.TempDir())

	selection := []resolver.ResolvedArtifact{
		codexSkill(t, "repo-skill", config.RepoCommitted),
		codexSkill(t, "machine-skill", config.UserPersonal),
	}
	report, err := Run(context.Background(), repo, selection)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findDiag(report.Diags, diag.CodeMaterializeDestinationOverlap); !ok {
		t.Fatalf("missing Codex root overlap diagnostic: %v", report.Diags)
	}
}

func TestRunRejectsOverlappingCodexMachineRoots(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".agents"))

	selection := []resolver.ResolvedArtifact{
		codexSkill(t, "demo", config.UserPersonal),
		codexSubagent("reviewer", "Review carefully.", config.UserPersonal),
	}
	report, err := Run(context.Background(), repo, selection)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findDiag(report.Diags, diag.CodeMaterializeDestinationOverlap); !ok {
		t.Fatalf("missing Codex root overlap diagnostic: %v", report.Diags)
	}
}

func TestRunRefusesAncestorCodexManagedPathsBeforeMutation(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".agents", "skills"))

	selection := []resolver.ResolvedArtifact{
		codexSkill(t, "demo", config.UserPersonal),
		codexSubagent("reviewer", "Review carefully.", config.UserPersonal),
	}
	report, err := Run(context.Background(), repo, selection)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := findDiag(report.Diags, diag.CodeMaterializeDestinationOverlap)
	if !ok {
		t.Fatalf("missing %s diagnostic: %+v", diag.CodeMaterializeDestinationOverlap, report.Diags)
	}
	if !strings.Contains(d.Message, filepath.Join(home, ".agents", "skills")) || d.Hint == "" {
		t.Fatalf("ancestor diagnostic is not actionable: %+v", d)
	}
	if _, err := os.Lstat(filepath.Join(home, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("ancestor preflight mutated the Codex destination: %v", err)
	}
}

func TestRunIgnoresInvalidMachineCodexRootForRepoOnlyOutput(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "relative-codex-home")

	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{
		codexSubagent("reviewer", "Review carefully.", config.RepoCommitted),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, report.Diags)
	if _, err := os.Lstat(filepath.Join(repo, ".codex", "agents", "reviewer.toml")); err != nil {
		t.Fatalf("repo-only output missing: %v", err)
	}
}

func TestRunIgnoresInvalidRepoCodexRootForMachineOnlyOutput(t *testing.T) {
	repo, outside, home := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	if err := os.Symlink(outside, filepath.Join(repo, ".codex")); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{
		codexSubagent("reviewer", "Review carefully.", config.UserPersonal),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, report.Diags)
	if _, err := os.Lstat(filepath.Join(home, ".codex", "agents", "reviewer.toml")); err != nil {
		t.Fatalf("machine-only output missing: %v", err)
	}
}

func TestCodexPreflightRefusesAncestorDesiredWrites(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", repo)
	t.Setenv("CODEX_HOME", "")
	repoKey := outputKey{target: hcc.Target(targetmeta.TargetCodex)}
	machineKey := outputKey{target: hcc.Target(targetmeta.TargetCodex), machineScope: true}
	result := compilePhaseResult{
		byTarget: map[outputKey]*targetWork{
			repoKey:    {artifacts: []artifact.Artifact{{Path: ".codex/agents/reviewer.toml", Bytes: []byte("parent")}}},
			machineKey: {artifacts: []artifact.Artifact{{Path: ".codex/agents/reviewer.toml/child", Bytes: []byte("child")}}},
		},
		skillLinks:      make(map[outputKey]map[string]*skillLinkGroup),
		allOutputs:      map[outputKey]bool{repoKey: true, machineKey: true},
		blocked:         make(blockedOutputs),
		artifactMeta:    make(map[outputKey]map[string]skillLink),
		codexAggregates: make(map[outputKey]*ownedJSONAggregateWork),
		codexMCP:        make(map[outputKey][]codexMCPEntry),
		codexMCPBlocked: make(map[outputKey]bool),
	}
	if _, d := preflightCodexDestinations(&result, repo, config.DefaultMaterializationModes()); d == nil || d.Code != diag.CodeMaterializeDestinationOverlap {
		t.Fatalf("ancestor desired writes were accepted: %+v", d)
	}
}

func TestRunRejectsCodexSharedAggregateRootsAcrossScopes(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", repo)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	repoHook := ir.NewHookWithMatcher("repo-hook", "PreToolUse", "Bash", "./repo-hook", "command", nil, ir.SourceRef{})
	machineHook := ir.NewHookWithMatcher("machine-hook", "PostToolUse", "Bash", "./machine-hook", "command", nil, ir.SourceRef{})
	selection := []resolver.ResolvedArtifact{
		codexResource("repo-hook", repoHook, config.RepoCommitted),
		codexResource("machine-hook", machineHook, config.UserPersonal),
		codexResource("repo-mcp", ir.NewMcpServer("repo-mcp", "npx", []string{"repo"}, nil, nil, ir.SourceRef{}), config.RepoCommitted),
		codexResource("machine-mcp", ir.NewMcpServer("machine-mcp", "npx", []string{"machine"}, nil, nil, ir.SourceRef{}), config.UserPersonal),
	}
	report, err := Run(context.Background(), repo, selection)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findDiag(report.Diags, diag.CodeMaterializeDestinationOverlap); !ok {
		t.Fatalf("missing Codex root overlap diagnostic: %v", report.Diags)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("overlap preflight mutated the Codex destination: %v", err)
	}
}

func TestRunRefusesConflictingCodexMCPBeforeMutation(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", repo)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	selection := []resolver.ResolvedArtifact{
		codexResource("shared", ir.NewMcpServer("shared", "npx", []string{"repo"}, nil, nil, ir.SourceRef{}), config.RepoCommitted),
		codexResource("shared", ir.NewMcpServer("shared", "npx", []string{"machine"}, nil, nil, ir.SourceRef{}), config.UserPersonal),
	}
	report, err := Run(context.Background(), repo, selection)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findDiag(report.Diags, diag.CodeMaterializeDestinationOverlap); !ok {
		t.Fatalf("missing actionable MCP overlap diagnostic: %+v", report.Diags)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("MCP conflict preflight mutated the Codex destination: %v", err)
	}
}

func codexSubagent(name, prompt string, source config.Source) resolver.ResolvedArtifact {
	resource := ir.NewSubagent(name, "Review", prompt, "gpt-5.6-terra", nil, "high", nil, "", nil, ir.SourceRef{})
	return codexResource(name, resource, source)
}

func codexResource(name string, resource ir.Resource, source config.Source) resolver.ResolvedArtifact {
	return resolver.ResolvedArtifact{
		Entry:     catalog.Entry{Name: name, Resource: resource},
		Source:    source,
		Harnesses: []targetmeta.Target{targetmeta.TargetCodex},
	}
}

func codexSkill(t *testing.T, name string, source config.Source) resolver.ResolvedArtifact {
	t.Helper()
	sourceDir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("---\nname: " + name + "\ndescription: Test skill\n---\n\nBody\n")
	if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	resource := ir.NewSkill(name, "Test skill", "Body\n", nil, nil, ir.SourceRef{})
	return resolver.ResolvedArtifact{
		Entry:     catalog.Entry{Name: name, Path: sourceDir, Resource: resource},
		Source:    source,
		Harnesses: []targetmeta.Target{targetmeta.TargetCodex},
	}
}
