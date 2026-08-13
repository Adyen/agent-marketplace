package factory

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

func TestFactorySubagentExtPrecedence(t *testing.T) {
	t.Run("uses ext override when present", func(t *testing.T) {
		sub := ir.NewSubagent("reviewer", "Review code", "Body", "sonnet", []string{"Read"}, "medium", nil, "", nil, ir.SourceRef{})
		sub.Ext.Factory = &extpkg.Factory{
			ReasoningEffort: "max",
			Isolation:       "container",
		}

		arts, diags, err := (&factoryCompiler{}).Translate(sub)
		if err != nil {
			t.Fatalf("Translate() error: %v", err)
		}
		if len(arts) != 0 {
			t.Fatalf("artifacts = %d, want none when sidecar isolation is present", len(arts))
		}
		if len(diags) != 1 || diags[0].Code != diag.CodeSubagentFactoryIsolationDropped || diags[0].Severity != diag.SeverityError {
			t.Fatalf("diagnostics = %#v, want factory isolation error", diags)
		}
	})

	t.Run("falls back to canonical derivation when ext missing", func(t *testing.T) {
		sub := ir.NewSubagent("reviewer", "Review code", "Body", "sonnet", []string{"Read"}, "medium", nil, "", nil, ir.SourceRef{})

		arts, diags, err := (&factoryCompiler{}).Translate(sub)
		if err != nil {
			t.Fatalf("Translate() error: %v", err)
		}
		if len(diags) != 0 {
			t.Fatalf("diagnostics = %#v, want none", diags)
		}

		body := string(findArtifactByPath(arts, "droids/reviewer.md").Bytes)
		if !strings.Contains(body, "reasoningEffort: medium") {
			t.Fatalf("frontmatter missing canonical reasoningEffort derivation:\n%s", body)
		}
		if strings.Contains(body, "isolation:") {
			t.Fatalf("frontmatter unexpectedly included isolation without ext override:\n%s", body)
		}
	})
}
