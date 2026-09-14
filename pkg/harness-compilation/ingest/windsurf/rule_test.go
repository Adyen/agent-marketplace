package windsurf

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
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

func TestWindsurfIngestPrefersNativeRuleOverLegacyDuplicate(t *testing.T) {
	src := fstest.MapFS{
		".windsurf/rules/shared.md":  {Data: []byte("---\ntrigger: manual\n---\nNative\n")},
		".windsurf/rules/shared.mdc": {Data: []byte("---\ndescription: Legacy\nalwaysApply: true\n---\nLegacy\n")},
	}
	bundle, err := Ingester{}.LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	rule := findRule(bundle.Resources, "shared")
	if rule == nil || rule.Body != "Native\n" || rule.Trigger != "manual" {
		t.Fatalf("selected rule = %#v, want native .md rule", rule)
	}
	for _, diagnostic := range bundle.Diagnostics {
		if diagnostic.Code == diag.CodeIngestWindsurfDuplicateRule && diagnostic.Resource == "shared" {
			return
		}
	}
	t.Fatalf("diagnostics = %#v, want duplicate Rule warning", bundle.Diagnostics)
}

func TestWindsurfIngestIgnoresMalformedLegacyDuplicate(t *testing.T) {
	src := fstest.MapFS{
		".windsurf/rules/shared.md":  {Data: []byte("---\ntrigger: manual\n---\nNative\n")},
		".windsurf/rules/shared.mdc": {Data: []byte("---\nalwaysApply: [\n---\nMalformed legacy\n")},
	}
	bundle, err := Ingester{}.LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle parsed ignored legacy duplicate: %v", err)
	}
	rule := findRule(bundle.Resources, "shared")
	if rule == nil || rule.Body != "Native\n" {
		t.Fatalf("selected rule = %#v, want native .md rule", rule)
	}
	if !diag.HasCode(bundle.Diagnostics, diag.CodeIngestWindsurfDuplicateRule) {
		t.Fatalf("diagnostics = %#v, want duplicate Rule warning", bundle.Diagnostics)
	}
}

func TestWindsurfIngest_WorkspaceRules(t *testing.T) {
	src := fstest.MapFS{
		".windsurf/rules/typescript.md": {Data: []byte("---\ntrigger: glob\nglobs:\n  - '*.ts'\n---\n# Body\n")},
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
	if rule.Description != "" {
		t.Errorf("description = %q, want empty for glob trigger", rule.Description)
	}
	if len(rule.Globs) != 1 || rule.Globs[0] != "*.ts" {
		t.Errorf("globs = %v, want [*.ts]", rule.Globs)
	}
}

func TestWindsurfIngest_LegacyMDCRules(t *testing.T) {
	src := fstest.MapFS{
		".windsurf/rules/typescript.mdc": {Data: []byte("---\ndescription: TypeScript\nglobs: '*.ts'\nalwaysApply: false\n---\n# Body\n")},
	}
	bundle, err := Ingester{}.LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	rule := findRule(bundle.Resources, "typescript")
	if rule == nil || rule.Description != "TypeScript" || len(rule.Globs) != 1 {
		t.Fatalf("legacy MDC rule = %#v", rule)
	}
}
