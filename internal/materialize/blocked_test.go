package materialize

import (
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestBlockedComponentsForMapsResourcesToTheirOwnedOutput(t *testing.T) {
	tests := []struct {
		name         string
		target       hcc.Target
		machineScope bool
		resource     ir.Resource
		want         []string
	}{
		{name: "a devin hook is aggregate-scoped", target: hcc.TargetDevin, resource: ir.NewHook("h", "PostToolUse", "echo", "command", nil, ir.SourceRef{})},
		{name: "a machine devin hook is aggregate-scoped", target: hcc.TargetDevin, machineScope: true, resource: ir.NewHook("h", "PostToolUse", "echo", "command", nil, ir.SourceRef{})},
		{name: "a devin mcp server is aggregate-scoped", target: hcc.TargetDevin, resource: ir.NewMcpServer("m", "bin", nil, nil, nil, ir.SourceRef{})},
		{name: "a codex hook is aggregate-scoped", target: hcc.TargetCodex, resource: ir.NewHook("h", "Stop", "echo", "command", nil, ir.SourceRef{})},
		{name: "a codex mcp server is aggregate-scoped", target: hcc.TargetCodex, resource: ir.NewMcpServer("m", "bin", nil, nil, nil, ir.SourceRef{})},
		{name: "a devin skill blocks skills", target: hcc.TargetDevin, resource: ir.NewSkill("s", "d", "b", nil, nil, ir.SourceRef{}), want: []string{"skills"}},
		{name: "a devin command blocks skills", target: hcc.TargetDevin, resource: ir.NewCommand("c", "d", "b", nil, ir.SourceRef{}), want: []string{"skills"}},
		{name: "a devin subagent blocks agents", target: hcc.TargetDevin, resource: ir.NewSubagent("a", "d", "b", "sonnet", nil, "", nil, "", nil, ir.SourceRef{}), want: []string{"agents"}},
		{name: "an unmapped devin rule fails closed", target: hcc.TargetDevin, resource: ir.NewRule("r", "d", "b", nil, false, nil, ir.SourceRef{}), want: []string{blockedAllComponents}},
		{name: "a claude hook blocks hooks", target: hcc.TargetClaude, resource: ir.NewHook("h", "PostToolUse", "echo", "command", nil, ir.SourceRef{}), want: []string{"hooks"}},
		{name: "an unmapped claude mcp fails closed", target: hcc.TargetClaude, resource: ir.NewMcpServer("m", "bin", nil, nil, nil, ir.SourceRef{}), want: []string{blockedAllComponents}},
		{name: "a machine windsurf command blocks global workflows", target: hcc.TargetWindsurf, machineScope: true, resource: ir.NewCommand("c", "d", "b", nil, ir.SourceRef{}), want: []string{"global_workflows"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := outputKey{target: tt.target, machineScope: tt.machineScope}
			work := skillTarget{
				skill:        devinResolved("res", tt.resource, tt.machineScope),
				target:       tt.target,
				machineScope: tt.machineScope,
			}
			got := blockedComponentsFor(key, work)
			if len(got) != len(tt.want) {
				t.Fatalf("blockedComponentsFor = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("blockedComponentsFor = %v, want %v", got, tt.want)
				}
			}
		})
	}

	t.Run("a missing IR resource fails closed", func(t *testing.T) {
		got := blockedComponentsFor(outputKey{target: hcc.TargetDevin}, skillTarget{target: hcc.TargetDevin})
		if len(got) != 1 || got[0] != blockedAllComponents {
			t.Fatalf("blockedComponentsFor(nil resource) = %v, want the wildcard", got)
		}
	})
}

func TestComponentBlockedHonoursWildcardAndNormalizesPaths(t *testing.T) {
	tests := []struct {
		name      string
		blocked   map[string]bool
		component string
		want      bool
	}{
		{name: "no block", component: "skills"},
		{name: "empty block", blocked: map[string]bool{}, component: "skills"},
		{name: "exact component", blocked: map[string]bool{"skills": true}, component: "skills", want: true},
		{name: "other component", blocked: map[string]bool{"agents": true}, component: "skills"},
		{name: "wildcard blocks a component", blocked: map[string]bool{blockedAllComponents: true}, component: "skills", want: true},
		{name: "wildcard blocks the root plan", blocked: map[string]bool{blockedAllComponents: true}, want: true},
		{name: "a component does not block the root plan", blocked: map[string]bool{"skills": true}},
		{name: "nested path reduces to component", blocked: map[string]bool{"skills": true}, component: "skills/nested", want: true},
		{name: "windsurf prefix is stripped", blocked: map[string]bool{"workflows": true}, component: ".windsurf/workflows", want: true},
		{name: "shared agents prefix is stripped", blocked: map[string]bool{"skills": true}, component: ".agents/skills", want: true},
		{name: "codex prefix is stripped", blocked: map[string]bool{"agents": true}, component: ".codex/agents", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := componentBlocked(tt.blocked, tt.component); got != tt.want {
				t.Fatalf("componentBlocked(%v, %q) = %v, want %v", tt.blocked, tt.component, got, tt.want)
			}
		})
	}
}
