package cursor

import (
	"strings"
	"testing"

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
		if len(diags) != 0 {
			t.Fatalf("diagnostics = %#v, want none", diags)
		}

		body := string(arts[0].Bytes)
		if !strings.Contains(body, "model: cursor-native-model") {
			t.Fatalf("frontmatter missing ext model override:\n%s", body)
		}
		if !strings.Contains(body, "- CursorTool") {
			t.Fatalf("frontmatter missing ext tools override:\n%s", body)
		}
		if !strings.Contains(body, "effort: max") {
			t.Fatalf("frontmatter missing ext effort override:\n%s", body)
		}
	})

	t.Run("falls back to canonical derivation when ext missing", func(t *testing.T) {
		sub := ir.NewSubagent("reviewer", "Review code", "Body", "sonnet", []string{"Read"}, "medium", nil, "", nil, ir.SourceRef{})

		arts, diags, err := newCursorCompiler().Translate(sub)
		if err != nil {
			t.Fatalf("Translate() error: %v", err)
		}
		if len(diags) != 0 {
			t.Fatalf("diagnostics = %#v, want none", diags)
		}

		body := string(arts[0].Bytes)
		if !strings.Contains(body, "model: claude-sonnet-4-5-20250929") {
			t.Fatalf("frontmatter missing canonical mapped model:\n%s", body)
		}
		if !strings.Contains(body, "- Read") {
			t.Fatalf("frontmatter missing canonical tools:\n%s", body)
		}
		if !strings.Contains(body, "effort: medium") {
			t.Fatalf("frontmatter missing canonical effort:\n%s", body)
		}
	})
}
