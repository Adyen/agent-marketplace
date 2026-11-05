package resolver

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// BenchmarkMaterializationWinners projects 240 resources of five kinds from
// two layers over four targets, with every tenth name selected twice. The
// shared-paths case also selects a same-name command for 12 of the 48 skills,
// which Devin and Codex install at the skill's path; the case-variant case
// names those commands with an upper-case first letter. Pure in-memory work;
// no disk or network is involved.
func BenchmarkMaterializationWinners(b *testing.B) {
	targets := []hcc.Target{hcc.TargetClaude, hcc.TargetFactory, hcc.TargetDevin, hcc.TargetCodex}
	kinds := []ir.ResourceKind{ir.KindSkill, ir.KindCommand, ir.KindSubagent, ir.KindMcpServer, ir.KindHook}
	fixture := func(shared, caseVariant bool) []ResolvedArtifact {
		var resolved []ResolvedArtifact
		for i := 0; i < 240; i++ {
			kind := kinds[i%len(kinds)]
			name := fmt.Sprintf("resource-%03d", i)
			source := config.UserPersonal
			if i%2 == 1 {
				source = config.RepoCommitted
			}
			resolved = append(resolved, materializationArtifact(name, fmt.Sprintf("body-%d", i), kind, source, targets))
			if i%10 == 0 {
				resolved = append(resolved, materializationArtifact(name, fmt.Sprintf("rival-%d", i), kind, source, targets))
			}
			if shared && i%20 == 0 {
				commandName := name
				if caseVariant {
					commandName = "R" + name[1:]
				}
				resolved = append(resolved, materializationArtifact(commandName, fmt.Sprintf("command-%d", i), ir.KindCommand, source, targets))
			}
		}
		return resolved
	}
	for _, tc := range []struct {
		name                string
		shared, caseVariant bool
	}{{"separate-paths", false, false}, {"shared-paths", true, false}, {"case-variant-paths", true, true}} {
		resolved := fixture(tc.shared, tc.caseVariant)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if out := MaterializationWinners(resolved); len(out) == 0 {
					b.Fatal("empty projection")
				}
			}
		})
	}
}
