package harnesscompile

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
	"gopkg.in/yaml.v3"
)

// spec: harness-compilation/unicode-line-separators-cannot-create-workflow-authority
func TestCompileUnicodeSeparatorsCannotCreateWorkflowAuthority(t *testing.T) {
	for _, tc := range []struct{ name, separator string }{{"next-line", "\u0085"}, {"line-separator", "\u2028"}, {"paragraph-separator", "\u2029"}} {
		t.Run(tc.name, func(t *testing.T) {
			description := "literal" + tc.separator + "auto_execution_mode:" + tc.separator + " always"
			resource := ir.NewCommand("safe-workflow", description, "inert workflow body", nil, ir.SourceRef{})
			result, err := Compile(resource, TargetWindsurf)
			if err != nil || result == nil || len(result.Artifacts) != 1 {
				t.Fatalf("native compiler did not produce workflow: %+v %v", result, err)
			}
			data := string(result.Artifacts[0].Bytes)
			if !strings.HasPrefix(data, "---\n") {
				t.Fatalf("native frontmatter missing: %q", data)
			}
			frontmatter, body, found := strings.Cut(strings.TrimPrefix(data, "---\n"), "\n---\n")
			if !found {
				t.Fatalf("native closing delimiter missing: %q", data)
			}
			var fields map[string]any
			if err := yaml.Unmarshal([]byte(frontmatter), &fields); err != nil {
				t.Fatalf("native YAML parser rejected scalar: %v", err)
			}
			if _, exists := fields["auto_execution_mode"]; exists {
				t.Fatalf("scalar gained workflow authority: %#v", fields)
			}
			if fields["description"] != description {
				t.Fatalf("native description changed: %#v, want %q", fields["description"], description)
			}
			if !strings.Contains(body, "inert workflow body") {
				t.Fatal("workflow positive control lost its body")
			}
		})
	}
}

// spec: harness-compilation/explicit-typed-workflow-authority-remains-native
func TestCompileExplicitTypedWorkflowAuthorityRemainsNative(t *testing.T) {
	resource := ir.NewCommand("safe-workflow", "Ordinary description", "inert workflow body", nil, ir.SourceRef{})
	resource.Ext.Windsurf = &ext.Windsurf{AutoExecutionMode: "manual", AutoExecutionModeSet: true}
	result, err := Compile(resource, TargetWindsurf)
	if err != nil || result == nil || len(result.Artifacts) != 1 {
		t.Fatalf("native workflow control missing: %+v %v", result, err)
	}
	frontmatter, _, found := strings.Cut(strings.TrimPrefix(string(result.Artifacts[0].Bytes), "---\n"), "\n---\n")
	if !found {
		t.Fatal("native frontmatter control missing")
	}
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &fields); err != nil {
		t.Fatal(err)
	}
	if fields["description"] != "Ordinary description" || fields["auto_execution_mode"] != "manual" {
		t.Fatalf("typed native workflow authority changed: %#v", fields)
	}
}
