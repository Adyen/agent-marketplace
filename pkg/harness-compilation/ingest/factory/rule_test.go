package factory

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func findRule(resources []ir.Resource, name string) *ir.Rule {
	for _, r := range resources {
		if rule, ok := r.(*ir.Rule); ok && rule.Name() == name {
			return rule
		}
	}
	return nil
}

func TestFactoryIngest_WorkspaceRules(t *testing.T) {
	src := fstest.MapFS{
		".factory/rules/typescript.md": {Data: []byte("# TypeScript Rules\nUse interfaces.\n")},
		".factory/rules/security.md":   {Data: []byte("# Security\nNo hardcoded secrets.\n")},
	}

	if !New().Detect(src) {
		t.Fatal("Detect should match a .factory/rules workspace")
	}

	bundle, err := New().LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	ts := findRule(bundle.Resources, "typescript")
	if ts == nil {
		t.Fatalf("expected a typescript rule, got resources %v", bundle.Resources)
		return
	}
	if ts.Body != "# TypeScript Rules\nUse interfaces.\n" {
		t.Errorf("rule body = %q", ts.Body)
	}
	if findRule(bundle.Resources, "security") == nil {
		t.Error("expected a security rule")
	}
}

func TestFactoryIngest_BareRulesDirDetected(t *testing.T) {
	src := fstest.MapFS{
		"rules/typescript.md": {Data: []byte("# TS\n")},
	}
	if !New().Detect(src) {
		t.Fatal("Detect should match a bare rules/ directory")
	}
	bundle, err := New().LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if findRule(bundle.Resources, "typescript") == nil {
		t.Fatalf("expected typescript rule from bare rules/, got %v", bundle.Resources)
	}
}
