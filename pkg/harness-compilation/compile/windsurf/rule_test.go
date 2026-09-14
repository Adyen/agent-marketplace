package windsurf

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestWindsurfRuleNativeGlob(t *testing.T) {
	rule := ir.NewRule("typescript", "TS conventions", "# Body\n", []string{"*.ts"}, false, nil, ir.SourceRef{})

	arts, diags, err := Compiler{}.Translate(rule)
	if err != nil {
		t.Fatalf("Translate(rule) error: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("description is legal with glob; diagnostics = %v", diags)
	}
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
	if arts[0].Path != ".windsurf/rules/typescript.md" {
		t.Errorf("path = %q, want .windsurf/rules/typescript.md", arts[0].Path)
	}
	body := string(arts[0].Bytes)
	for _, want := range []string{"trigger: glob\n", "- '*.ts'\n", "# Body\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("windsurf rule missing %q, got %q", want, body)
		}
	}
	if !strings.Contains(body, "description: TS conventions\n") {
		t.Fatalf("description missing from glob rule: %q", body)
	}
}
