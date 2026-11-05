package materialize

import (
	"context"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// A run restricted to one scope reports only the shared-path displacements it
// processed.
func TestSharedPathWarningsFollowTheScopeFilter(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []RunOption
		want int
	}{
		{"all scopes", nil, 1},
		{"machine scope only", []RunOption{WithMachineScopeOnly()}, 1},
		{"repository scope only", []RunOption{WithRepoScopeOnly()}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, _ := devinEnv(t)
			skill := devinResolvedResource("audit", ir.NewSkill("audit", "Organization skill", "Skill body", nil, nil, ir.SourceRef{}), true)
			skill.Required, skill.RegistryURL = true, "https://example.invalid/org"
			command := devinResolvedResource("audit", ir.NewCommand("audit", "User command", "Command body", nil, ir.SourceRef{}), true)
			command.RegistryURL = "https://example.invalid/user"

			report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{skill, command}, tc.opts...)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got := 0
			for _, d := range report.Diags {
				if d.Code == diag.CodeMaterializeSharedPathCollision {
					got++
				}
			}
			if got != tc.want {
				t.Fatalf("shared-path warnings = %d, want %d: %+v", got, tc.want, report.Diags)
			}
		})
	}
}
