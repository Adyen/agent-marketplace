package materialize

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkDriftReportFromReport(b *testing.B) {
	resolved := make([]string, 500)
	report := &Report{PerRoot: []TargetReport{{Target: hcc.TargetFactory, OutDir: "/repo/.factory"}}}
	for i := range resolved {
		name := fmt.Sprintf("skill-%03d", i)
		resolved[i] = skill.ResourceKey(ir.KindSkill, name)
		if i < 250 {
			report.PerRoot[0].Written = append(report.PerRoot[0].Written, "skills/"+name+"/SKILL.md")
		}
		if i < 100 {
			report.PerRoot[0].Removed = append(report.PerRoot[0].Removed, "skills/orphan-"+name+"/SKILL.md")
		}
		if i < 50 {
			report.Diags = append(report.Diags, diag.Diagnostic{Severity: diag.SeverityError, Resource: name})
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = DriftReportFromReport(report, resolved)
	}
}
