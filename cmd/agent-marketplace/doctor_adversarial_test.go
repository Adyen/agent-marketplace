package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// TestWriteRepairDiagnosticsNeverCollapsesTwoDifferentFacts attacks the dedup
// with pairs of diagnostics that render identically or nearly so but state
// DIFFERENT facts. Losing a per-file (or per-target, or per-resource) failure is
// the failure mode; printing one line twice is only noise.
func TestWriteRepairDiagnosticsNeverCollapsesTwoDifferentFacts(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	base := diag.Diagnostic{
		Code:     diag.CodeOwnershipStateMissing,
		Severity: diag.SeverityWarning,
		Target:   "devin",
		Source:   diag.SourceRef{File: harness.DevinMcpConfigFile},
		Message:  "/repo/.devin/mcp_config.json: ownership: no durable ownership record",
	}

	cases := []struct {
		name   string
		mutate func(d diag.Diagnostic) diag.Diagnostic
	}{
		{"the same relative filename in the other scope", func(d diag.Diagnostic) diag.Diagnostic {
			// mcp_config.json exists in BOTH the repo and the machine scope, so
			// Source, Code and Resource are all identical; only the root differs.
			d.Message = "/root/.config/devin/mcp_config.json: ownership: no durable ownership record"
			return d
		}},
		{"a different aggregate under the same root", func(d diag.Diagnostic) diag.Diagnostic {
			d.Source = diag.SourceRef{File: harness.DevinUserConfigFile}
			d.Message = "/repo/.devin/config.json: ownership: no durable ownership record"
			return d
		}},
		{"a different resource", func(d diag.Diagnostic) diag.Diagnostic {
			d.Resource = "other-resource"
			return d
		}},
		{"a different target", func(d diag.Diagnostic) diag.Diagnostic {
			d.Target = "claude"
			return d
		}},
		{"a different parse line in one file", func(d diag.Diagnostic) diag.Diagnostic {
			d.Source.Line = 12
			return d
		}},
		{"a different severity", func(d diag.Diagnostic) diag.Diagnostic {
			d.Severity = diag.SeverityError
			return d
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			other := tc.mutate(base)
			var out bytes.Buffer
			preview := &materialize.RepairPreview{Diags: []diag.Diagnostic{base, other}}
			if err := writeRepairDiagnostics(&out, preview); err != nil {
				t.Fatalf("writeRepairDiagnostics: %v", err)
			}
			lines := 0
			for _, l := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
				if strings.TrimSpace(l) != "" {
					lines++
				}
			}
			if lines != 2 {
				t.Fatalf("two different facts collapsed into %d line(s):\n%s", lines, out.String())
			}
		})
	}

	// The control: a diagnostic identical in every field a consumer can see is
	// still printed once.
	var out bytes.Buffer
	preview := &materialize.RepairPreview{Diags: []diag.Diagnostic{base, base, base}}
	if err := writeRepairDiagnostics(&out, preview); err != nil {
		t.Fatalf("writeRepairDiagnostics: %v", err)
	}
	if got := strings.Count(out.String(), "\n"); got != 1 {
		t.Fatalf("an identical diagnostic printed %d times:\n%s", got, out.String())
	}
}
