package devin

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestLoadBundleIngestsWorkspaceAndDirectRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "workspace", path: ".devin/rules/secure-coding.md"},
		{name: "direct user root", path: "rules/secure-coding.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := LoadBundle(fstest.MapFS{tc.path: {Data: []byte("---\ntrigger: agent\ndescription: Security guidance\n---\n# Secure coding\n")}})
			if err != nil {
				t.Fatal(err)
			}
			for _, resource := range got.Resources {
				if rule, ok := resource.(*ir.Rule); ok {
					if rule.Name() != "secure-coding" || rule.Body != "# Secure coding\n" || rule.Description != "Security guidance" ||
						rule.Trigger != "agent" {
						t.Fatalf("rule = %#v", rule)
					}
					return
				}
			}
			t.Fatalf("no rule in resources: %v", got.Resources)
		})
	}
}

func TestMalformedRuleDoesNotDiscardValidSiblings(t *testing.T) {
	got, err := LoadBundle(fstest.MapFS{
		".devin/rules/broken.md":      {Data: []byte("---\ntrigger: glob\n---\nBroken\n")},
		".devin/rules/valid.md":       {Data: []byte("---\ntrigger: manual\n---\nValid\n")},
		".devin/skills/tool/SKILL.md": {Data: []byte("---\nname: tool\ndescription: Tool\n---\nBody\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	var validRule, skill bool
	for _, resource := range got.Resources {
		validRule = validRule || (resource.Kind() == ir.KindRule && resource.Name() == "valid")
		skill = skill || (resource.Kind() == ir.KindSkill && resource.Name() == "tool")
	}
	if !validRule || !skill {
		t.Fatalf("resources = %#v, want valid Rule and Skill siblings", got.Resources)
	}
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Code == diag.CodeIngestDevinLoadRuleFailed && diagnostic.Resource == "broken" {
			return
		}
	}
	t.Fatalf("diagnostics = %#v, want malformed Rule diagnostic", got.Diagnostics)
}
