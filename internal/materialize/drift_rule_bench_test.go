package materialize

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkDriftReportRuleIdentity(b *testing.B) {
	const resourceCount = 1000
	resolved := make([]string, 0, resourceCount)
	written := make([]string, 0, resourceCount/2)
	removed := make([]string, 0, resourceCount/10)
	for i := 0; i < resourceCount; i++ {
		name := fmt.Sprintf("resource-%04d", i)
		kind := ir.KindSkill
		path := "skills/" + name + "/SKILL.md"
		if i%2 == 1 {
			kind = ir.KindRule
			path = ".windsurf/rules/" + name + ".md"
		}
		resolved = append(resolved, skill.ResourceKey(kind, name))
		if i%2 == 0 {
			written = append(written, path)
		}
		if i%10 == 0 {
			removed = append(removed, path)
		}
	}
	report := &Report{PerRoot: []TargetReport{{
		Written: written,
		Removed: removed,
	}}}
	b.ReportAllocs()
	for b.Loop() {
		_ = DriftReportFromReport(report, resolved)
	}
}
