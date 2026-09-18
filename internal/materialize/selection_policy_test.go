package materialize

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type selectionCandidate struct {
	label      string
	source     config.Source
	required   bool
	precedence int
	body       string
}

func resolveSelectionCandidates(t *testing.T, target hcc.Target, candidates []selectionCandidate, kind ir.ResourceKind) ([]resolver.ResolvedArtifact, *resolver.NameCollisionWarning, string) {
	t.Helper()
	base := t.TempDir()
	cfg := &config.Config{Harnesses: []string{string(target)}}
	catalogs := make(map[string][]catalog.Entry, len(candidates))
	for _, candidate := range candidates {
		dir := filepath.Join(base, candidate.label)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "SKILL.md")
		if err := os.WriteFile(path, []byte(candidate.body), 0o600); err != nil {
			t.Fatal(err)
		}
		var resource ir.Resource
		switch kind {
		case ir.KindSkill:
			resource = ir.NewSkill("shared", "selection", candidate.body, nil, nil, ir.SourceRef{File: path})
		case ir.KindMcpServer:
			resource = ir.NewMcpServer("shared", candidate.body, nil, nil, nil, ir.SourceRef{File: path})
		default:
			t.Fatalf("unsupported test kind %s", kind)
		}
		key := "catalog-" + candidate.label
		cfg.Registries = append(cfg.Registries, config.RegistryEntry{URL: "https://example.invalid/registry.git", Ref: "main", Path: candidate.label, CatalogKey: key, Source: candidate.source, Required: candidate.required, Default: true, FilePrecedence: candidate.precedence})
		catalogs[key] = []catalog.Entry{{Kind: kind, Name: "shared", Path: path, Resource: resource}}
	}
	resolved, errs := resolver.Resolve(cfg, catalogs, nil, resolver.WithTargetHarnesses([]hcc.Target{target}))
	var warning *resolver.NameCollisionWarning
	for _, err := range errs {
		if errors.As(err, &warning) {
			break
		}
	}
	if len(resolved) != 2 || warning == nil {
		t.Fatalf("Resolve = %d artifacts, errors %v", len(resolved), errs)
	}
	return resolved, warning, base
}

func isolateSelectionEnvironment(t *testing.T) (home, repo string) {
	t.Helper()
	home, repo = t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	return home, repo
}

func TestSelectionPolicySkillUsesResolverWinner(t *testing.T) {
	for _, tc := range []struct {
		name       string
		candidates []selectionCandidate
		tied       bool
	}{
		{name: "required organization", candidates: []selectionCandidate{{label: "z-loser", source: config.UserPersonal, body: "LOSER"}, {label: "a-preferred", source: config.Organization, required: true, body: "PREFERRED"}}},
		{name: "user over organization", candidates: []selectionCandidate{{label: "z-loser", source: config.Organization, body: "LOSER"}, {label: "a-preferred", source: config.UserPersonal, body: "PREFERRED"}}},
		{name: "file precedence", candidates: []selectionCandidate{{label: "z-loser", source: config.RepoCommitted, precedence: 1, body: "LOSER"}, {label: "a-preferred", source: config.RepoCommitted, precedence: 2, body: "PREFERRED"}}},
		{name: "stable config order", candidates: []selectionCandidate{{label: "a-first", source: config.RepoCommitted, body: "FIRST"}, {label: "z-second", source: config.RepoCommitted, body: "SECOND"}}, tied: true},
	} {
		for _, reverse := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/forward", true: "/reverse"}[reverse], func(t *testing.T) {
				candidates := append([]selectionCandidate(nil), tc.candidates...)
				if reverse {
					candidates[0], candidates[1] = candidates[1], candidates[0]
				}
				preferredBody := "PREFERRED"
				preferredLabel := "a-preferred"
				if tc.tied {
					preferredBody = candidates[0].body
					preferredLabel = candidates[0].label
				}
				home, repo := isolateSelectionEnvironment(t)
				resolved, warning, _ := resolveSelectionCandidates(t, hcc.TargetFactory, candidates, ir.KindSkill)
				if !strings.Contains(warning.Winner, preferredLabel) {
					t.Fatalf("warning winner = %q, want %q", warning.Winner, preferredLabel)
				}
				report, err := Run(context.Background(), repo, resolved)
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				if report == nil || len(report.Diags) != 0 {
					t.Fatalf("Run report = %#v", report)
				}
				root := repo
				if candidates[0].source.IsMachineScope() {
					root = home
				}
				data, err := os.ReadFile(filepath.Join(root, ".factory", "skills", "shared", "SKILL.md"))
				losingBody := candidates[1].body
				if !tc.tied && losingBody == "PREFERRED" {
					losingBody = candidates[0].body
				}
				if err != nil || !strings.Contains(string(data), preferredBody) || strings.Contains(string(data), losingBody) {
					t.Fatalf("materialized skill = %q err=%v", data, err)
				}
			})
		}
	}
}

func TestSelectionPolicyRunControls(t *testing.T) {
	t.Run("machine and project scopes", func(t *testing.T) {
		home, repo := isolateSelectionEnvironment(t)
		machine := resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared", Resource: ir.NewSkill("shared", "selection", "MACHINE", nil, nil, ir.SourceRef{})}, RegistryURL: "https://example.invalid/registry.git", RegistryPath: "machine", CatalogKey: "machine", Source: config.UserPersonal, Harnesses: []hcc.Target{hcc.TargetFactory}}
		project := resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared", Resource: ir.NewSkill("shared", "selection", "PROJECT", nil, nil, ir.SourceRef{})}, RegistryURL: "https://example.invalid/registry.git", RegistryPath: "project", CatalogKey: "project", Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}
		report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{machine, project})
		if err != nil || report == nil || len(report.Diags) != 0 {
			t.Fatalf("Run = %#v err=%v", report, err)
		}
		machineBytes, machineErr := os.ReadFile(filepath.Join(home, ".factory", "skills", "shared", "SKILL.md"))
		projectBytes, projectErr := os.ReadFile(filepath.Join(repo, ".factory", "skills", "shared", "SKILL.md"))
		if machineErr != nil || projectErr != nil || !strings.Contains(string(machineBytes), "MACHINE") || !strings.Contains(string(projectBytes), "PROJECT") {
			t.Fatalf("scope outputs = %q/%q errors=%v/%v", machineBytes, projectBytes, machineErr, projectErr)
		}
	})
	t.Run("nonoverlapping targets", func(t *testing.T) {
		_, repo := isolateSelectionEnvironment(t)
		factory := resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared", Resource: ir.NewSkill("shared", "selection", "FACTORY", nil, nil, ir.SourceRef{})}, RegistryURL: "https://example.invalid/registry.git", Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}
		claude := resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared", Resource: ir.NewSkill("shared", "selection", "CLAUDE", nil, nil, ir.SourceRef{})}, RegistryURL: "https://example.invalid/registry.git", Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetClaude}}
		report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{factory, claude})
		if err != nil || report == nil || len(report.Diags) != 0 {
			t.Fatalf("Run = %#v err=%v", report, err)
		}
		factoryBytes, factoryErr := os.ReadFile(filepath.Join(repo, ".factory", "skills", "shared", "SKILL.md"))
		claudeBytes, claudeErr := os.ReadFile(filepath.Join(repo, ".claude", "skills", "shared", "SKILL.md"))
		if factoryErr != nil || claudeErr != nil || !strings.Contains(string(factoryBytes), "FACTORY") || !strings.Contains(string(claudeBytes), "CLAUDE") {
			t.Fatalf("target outputs = %q/%q errors=%v/%v", factoryBytes, claudeBytes, factoryErr, claudeErr)
		}
	})
}

func TestSelectionPolicyOwnedMCPUsesResolverWinner(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetDevin, hcc.TargetGemini, hcc.TargetCodex} {
		for _, reverse := range []bool{false, true} {
			t.Run(string(target)+map[bool]string{false: "/preferred-second", true: "/preferred-first"}[reverse], func(t *testing.T) {
				home, repo := isolateSelectionEnvironment(t)
				candidates := []selectionCandidate{{label: "z-loser", source: config.UserPersonal, body: "LOSER"}, {label: "a-preferred", source: config.Organization, required: true, body: "PREFERRED"}}
				if reverse {
					candidates[0], candidates[1] = candidates[1], candidates[0]
				}
				resolved, warning, _ := resolveSelectionCandidates(t, target, candidates, ir.KindMcpServer)
				if !strings.Contains(warning.Winner, "a-preferred") {
					t.Fatalf("warning winner = %q", warning.Winner)
				}
				report, err := Run(context.Background(), repo, resolved)
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				if report == nil || len(report.Diags) != 0 {
					t.Fatalf("Run report = %#v", report)
				}
				var path string
				switch target {
				case hcc.TargetDevin:
					path = filepath.Join(home, ".config", "devin", "mcp_config.json")
				case hcc.TargetGemini:
					path = filepath.Join(home, ".gemini", "settings.json")
				case hcc.TargetCodex:
					path = filepath.Join(home, ".codex", "config.toml")
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if command := selectedMCPCommand(t, target, data); command != "PREFERRED" {
					t.Fatalf("materialized MCP command = %q", command)
				}
			})
		}
	}
}

func selectedMCPCommand(t *testing.T, target hcc.Target, data []byte) string {
	t.Helper()
	if target == hcc.TargetCodex {
		servers, err := codexMCPValues(data)
		if err != nil {
			t.Fatal(err)
		}
		server, ok := servers["shared"].(map[string]any)
		if !ok {
			t.Fatalf("Codex MCP servers = %#v", servers)
		}
		command, _ := server["command"].(string)
		return command
	}
	doc := nativeMCPDocument(t, data)
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("MCP document = %#v", doc)
	}
	server, ok := servers["shared"].(map[string]any)
	if !ok {
		t.Fatalf("MCP servers = %#v", servers)
	}
	command, _ := server["command"].(string)
	return command
}
