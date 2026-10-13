package claude

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

func TestClaudeSubagentExtPrecedence(t *testing.T) {
	t.Run("uses ext override when present", func(t *testing.T) {
		sub := ir.NewSubagent("reviewer", "Review code", "Body", "sonnet", []string{"Read", "Bash"}, "medium", nil, "", nil, ir.SourceRef{})
		sub.Ext.Claude = &extpkg.Claude{
			Model:     "claude-opus-4-1-20260101",
			Tools:     []string{"Bash"},
			Effort:    "max",
			Isolation: "container",
		}

		arts, diags, err := (&claudeCompiler{}).Translate(sub)
		if err != nil {
			t.Fatalf("Translate() error: %v", err)
		}
		if len(diags) != 0 {
			t.Fatalf("diagnostics = %#v, want none", diags)
		}

		body := string(findArtifactByPath(arts, "agents/reviewer.md").Bytes)
		if !strings.Contains(body, "model: claude-opus-4-1-20260101") {
			t.Fatalf("frontmatter missing ext model override:\n%s", body)
		}
		if !strings.Contains(body, "effort: max") {
			t.Fatalf("frontmatter missing ext effort override:\n%s", body)
		}
		if !strings.Contains(body, "  - Bash") {
			t.Fatalf("frontmatter missing ext tool override:\n%s", body)
		}
		if !strings.Contains(body, "isolation: container") {
			t.Fatalf("frontmatter missing ext isolation override:\n%s", body)
		}
	})

	t.Run("refuses an ext tool list that widens the canonical tools", func(t *testing.T) {
		sub := ir.NewSubagent("reviewer", "Review code", "Body", "sonnet", []string{"Read"}, "medium", nil, "", nil, ir.SourceRef{})
		sub.Ext.Claude = &extpkg.Claude{Tools: []string{"Bash"}}

		arts, diags, err := (&claudeCompiler{}).Translate(sub)
		if err != nil {
			t.Fatalf("Translate() error: %v", err)
		}
		if len(arts) != 0 || len(diags) != 1 || diags[0].Code != diag.CodeSubagentSecurityRestrictionUnsupported || diags[0].Severity != diag.SeverityError {
			t.Fatalf("artifacts = %d, diagnostics = %#v, want the widening sidecar refused", len(arts), diags)
		}
	})

	t.Run("falls back to canonical fields when ext missing", func(t *testing.T) {
		sub := ir.NewSubagent("reviewer", "Review code", "Body", "sonnet", []string{"Read"}, "medium", nil, "worktree", nil, ir.SourceRef{})

		arts, _, err := (&claudeCompiler{}).Translate(sub)
		if err != nil {
			t.Fatalf("Translate() error: %v", err)
		}

		body := string(findArtifactByPath(arts, "agents/reviewer.md").Bytes)
		if !strings.Contains(body, "model: sonnet") || !strings.Contains(body, "effort: medium") || !strings.Contains(body, "isolation: worktree") {
			t.Fatalf("frontmatter did not fall back to canonical fields:\n%s", body)
		}
	})
}
