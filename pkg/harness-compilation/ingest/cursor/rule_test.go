package cursor

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

func TestCursorIngest_WorkspaceRules(t *testing.T) {
	src := fstest.MapFS{
		".cursor/rules/typescript.mdc": {Data: []byte("---\ndescription: TS conventions\nglobs: '*.ts,*.tsx'\nalwaysApply: true\n---\n# Body\n")},
	}

	if !(&cursorIngester{}).Detect(src) {
		t.Fatal("Detect should match a .cursor/rules workspace")
	}

	bundle, err := LoadBundle(src)
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
	if len(rule.Globs) != 2 || rule.Globs[0] != "*.ts" {
		t.Errorf("globs = %v, want [*.ts *.tsx]", rule.Globs)
	}
	if !rule.AlwaysApply {
		t.Error("alwaysApply = false, want true")
	}
	// Source path should be normalized (no .cursor/ prefix).
	if rule.Source.File != "rules/typescript.mdc" {
		t.Errorf("source file = %q, want rules/typescript.mdc", rule.Source.File)
	}
}
