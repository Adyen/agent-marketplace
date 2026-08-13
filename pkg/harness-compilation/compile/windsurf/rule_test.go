package windsurf

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestWindsurfRule_MDC(t *testing.T) {
	rule := ir.NewRule("typescript", "TS conventions", "# Body\n", []string{"*.ts"}, false, nil, ir.SourceRef{})

	arts, diags, err := Compiler{}.Translate(rule)
	if err != nil {
		t.Fatalf("Translate(rule) error: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
	if arts[0].Path != ".windsurf/rules/typescript.mdc" {
		t.Errorf("path = %q, want .windsurf/rules/typescript.mdc", arts[0].Path)
	}
	body := string(arts[0].Bytes)
	for _, want := range []string{"description: TS conventions\n", "globs: '*.ts'\n", "alwaysApply: false\n", "# Body\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("windsurf rule .mdc missing %q, got %q", want, body)
		}
	}
}
