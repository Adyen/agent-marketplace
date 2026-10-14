package cursor

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

func TestCursorSubagentExtPrecedence(t *testing.T) {
	t.Run("uses ext override when present", func(t *testing.T) {
		sub := ir.NewSubagent("reviewer", "Review code", "Body", "sonnet", []string{"Read"}, "medium", nil, "", nil, ir.SourceRef{})
		sub.Ext.Cursor = &extpkg.Cursor{
			Model:  "cursor-native-model",
			Tools:  []string{"CursorTool"},
			Effort: "max",
		}

		arts, diags, err := newCursorCompiler().Translate(sub)
		if err != nil {
			t.Fatalf("Translate() error: %v", err)
		}
		if len(diags) != 1 || !diag.HasCode(diags, diag.CodeModelReasoningOmitted) {
			t.Fatalf("diagnostics = %#v, want unsupported effort warning", diags)
		}

		body := string(arts[0].Bytes)
		if !strings.Contains(body, "model: cursor-native-model") {
			t.Fatalf("frontmatter missing ext model override:\n%s", body)
		}
		if !strings.Contains(body, "- CursorTool") {
			t.Fatalf("frontmatter missing ext tools override:\n%s", body)
		}
		if strings.Contains(body, "effort:") {
			t.Fatalf("frontmatter emitted unsupported standalone effort:\n%s", body)
		}
	})

	t.Run("falls back to canonical derivation when ext missing", func(t *testing.T) {
		// Cursor has no subagent tool surface, so a canonical tools list
		// would suppress the artifact; canonical derivation is exercised
		// with the restriction absent here.
		sub := ir.NewSubagent("reviewer", "Review code", "Body", "sonnet", nil, "medium", nil, "", nil, ir.SourceRef{})

		arts, diags, err := newCursorCompiler().Translate(sub)
		if err != nil {
			t.Fatalf("Translate() error: %v", err)
		}
		if len(diags) != 2 || !diag.HasCode(diags, diag.CodeModelSelectorPinned) || !diag.HasCode(diags, diag.CodeModelReasoningOmitted) {
			t.Fatalf("diagnostics = %#v, want pin and effort loss", diags)
		}

		body := string(arts[0].Bytes)
		if !strings.Contains(body, "model: claude-sonnet-4-5-20250929") {
			t.Fatalf("frontmatter missing canonical mapped model:\n%s", body)
		}
		if strings.Contains(body, "tools:") {
			t.Fatalf("frontmatter emitted a tools field Cursor cannot enforce:\n%s", body)
		}
		if strings.Contains(body, "effort:") {
			t.Fatalf("frontmatter emitted unsupported standalone effort:\n%s", body)
		}
	})
}
