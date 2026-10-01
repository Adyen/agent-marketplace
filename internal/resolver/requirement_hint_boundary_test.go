package resolver

import (
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: resource-prerequisites/requirement-remediation-keeps-shell-syntax-literal
func TestRequirementRemediationKeepsShellSyntaxLiteral(t *testing.T) {
	warning := &UnmetRequirementWarning{Requirement: catalog.Requirement{Kind: ir.KindMcpServer, Name: "audit$(inert-value)", Registry: "https://example.com/team$(inert-origin).git", Path: "team's $(inert-path)"}}
	const want = `agent-marketplace install 'audit$(inert-value)' --kind mcp --from 'https://example.com/team$(inert-origin).git' --path 'team'\''s $(inert-path)'`
	if got := warning.Remediation(); got != want {
		t.Fatalf("remediation arguments are not shell literals: %q, want %q", got, want)
	}
}

// spec: resource-prerequisites/resolver-keeps-ordinary-prerequisite-warnings-and-selection
func TestResolveRequirementHintRetainsSelectionAndTypedWarning(t *testing.T) {
	cfg := &config.Config{Registries: []config.RegistryEntry{{URL: "git@example.com:skills.git", Ref: "main", Skills: ks("selected-skill")}}}
	setCatalogKeys(cfg)
	entries := map[string][]catalog.Entry{ck("git@example.com:skills.git", "main"): {{Kind: ir.KindSkill, Name: "selected-skill", Resource: requirementTestSkillNamed("selected-skill", "audit$(inert-value)")}}}
	resolved, failures := Resolve(cfg, entries, nil)
	if len(resolved) != 1 || resolved[0].Name != "selected-skill" || len(failures) != 1 {
		t.Fatalf("resolver changed prerequisite selection: %+v %v", resolved, failures)
	}
	var warning *UnmetRequirementWarning
	if !errors.As(failures[0], &warning) || !IsWarning(failures[0]) {
		t.Fatalf("resolver lost typed prerequisite warning: %v", failures)
	}
	const want = `agent-marketplace install 'audit$(inert-value)' --kind mcp`
	if got := warning.Remediation(); got != want {
		t.Fatalf("registry-authored requirement bypassed literal argument formatting: %q", got)
	}
}
