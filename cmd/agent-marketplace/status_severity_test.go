package main

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// TestBuildStatusRowsOnlyErrorsMarkNotMaterialized pins `status` behaviour:
// diag.SeverityError is the ZERO value of diag.Severity, so an ordinal
// comparison used to flag every resource-attached diagnostic — including
// lossy-but-successful warnings — as "error: not materialized". Only an
// error-severity diagnostic may do that.
func TestBuildStatusRowsOnlyErrorsMarkNotMaterialized(t *testing.T) {
	const skillPath = "/cache/reg/skills/some-skill"

	tests := []struct {
		name       string
		severity   diag.Severity
		wantTarget string
	}{
		{name: "warning keeps the materialized path", severity: diag.SeverityWarning, wantTarget: skillPath},
		{name: "info keeps the materialized path", severity: diag.SeverityInfo, wantTarget: skillPath},
		{name: "error reports not materialized", severity: diag.SeverityError, wantTarget: "error: not materialized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := []resolver.ResolvedArtifact{{
				Entry: catalog.Entry{
					Kind: ir.KindSkill,
					Name: "some-skill",
					Path: skillPath,
				},
				RegistryURL: "git@example.com:org/reg.git",
				Ref:         "main",
				Source:      config.RepoCommitted,
			}}
			report := &materialize.Report{
				Diags: []diag.Diagnostic{{
					Code:     diag.CodeSubagentDevinEffortDropped,
					Severity: tt.severity,
					Resource: "some-skill",
					Message:  "lossy but successful",
				}},
			}

			rows := buildStatusRows(t.TempDir(), resolved, map[string]string{}, report)
			if len(rows) != 1 {
				t.Fatalf("buildStatusRows returned %d rows, want 1", len(rows))
			}
			if rows[0].Target != tt.wantTarget {
				t.Fatalf("row Target for a %v diagnostic = %q, want %q", tt.severity, rows[0].Target, tt.wantTarget)
			}
		})
	}
}
