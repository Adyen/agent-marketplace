package cursor

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestCursorRule_MDC(t *testing.T) {
	c := &cursorCompiler{}
	rule := ir.NewRule("typescript", "TS conventions", "# Body\n", []string{"*.ts", "*.tsx"}, true, nil, ir.SourceRef{})

	arts, diags, err := c.Translate(rule)
	if err != nil {
		t.Fatalf("Translate(rule) error: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
	if arts[0].Path != "rules/typescript.mdc" {
		t.Errorf("path = %q, want rules/typescript.mdc", arts[0].Path)
	}
	body := string(arts[0].Bytes)
	for _, want := range []string{"---\n", "description: TS conventions\n", "globs: '*.ts,*.tsx'\n", "alwaysApply: true\n", "# Body\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("cursor rule .mdc missing %q, got %q", want, body)
		}
	}
}
