package windsurf

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

func TestWindsurfIngest_WorkspaceRules(t *testing.T) {
	src := fstest.MapFS{
		".windsurf/rules/typescript.mdc": {Data: []byte("---\ndescription: TS conventions\nglobs: '*.ts'\nalwaysApply: false\n---\n# Body\n")},
	}

	if !(Ingester{}).Detect(src) {
		t.Fatal("Detect should match a .windsurf/rules workspace")
	}

	bundle, err := Ingester{}.LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	rule := findRule(bundle.Resources, "typescript")
	if rule == nil {
		t.Fatalf("expected a typescript rule, got %v", bundle.Resources)
		return
	}
	if rule.Description != "TS conventions" {
		t.Errorf("description = %q", rule.Description)
	}
	if len(rule.Globs) != 1 || rule.Globs[0] != "*.ts" {
		t.Errorf("globs = %v, want [*.ts]", rule.Globs)
	}
}
