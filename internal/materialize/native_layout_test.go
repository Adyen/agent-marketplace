package materialize

import (
	"slices"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// TestNativeLayoutMatchesCompiledArtifactPaths pins each described target's
// native-layout descriptor against independently authored expectations and
// the paths the compilers actually emit, generalizing
// TestDevinAggregateNamesMatchCompiledArtifactPaths to every target with a
// registered NativeLayout. A descriptor that drifts from compiler output, or
// a compiler change nobody reflected in the descriptor, fails here.
func TestNativeLayoutMatchesCompiledArtifactPaths(t *testing.T) {
	hook := ir.NewHook("probe-hook", "PostToolUse", "echo probe", "command", nil, ir.SourceRef{})
	mcp := ir.NewMcpServer("probe-mcp", "probe-bin", nil, nil, nil, ir.SourceRef{})
	tests := []struct {
		name         string
		target       hcc.Target
		machineScope bool
		resource     ir.Resource
		// wantPaths is the independently authored emitted-artifact path set.
		wantPaths []string
		// wantMember is the envelope member the descriptor declares for this
		// kind's aggregate in this scope, "" when the kind has no aggregate or
		// the file's top level carries the entries directly.
		wantMember string
	}{
		{"claude skill", hcc.TargetClaude, false, ir.NewSkill("probe-skill", "probe", "body", nil, nil, ir.SourceRef{}),
			[]string{"skills/probe-skill/SKILL.md"}, ""},
		{"claude command", hcc.TargetClaude, false, ir.NewCommand("probe-command", "probe", "body", nil, ir.SourceRef{}),
			[]string{"commands/probe-command.md"}, ""},
		{"claude subagent", hcc.TargetClaude, false, ir.NewSubagent("probe-subagent", "probe", "body", "", nil, "", nil, "", nil, ir.SourceRef{}),
			[]string{"agents/probe-subagent.md"}, ""},
		{"claude hook", hcc.TargetClaude, false, hook,
			[]string{"hooks/hooks.json"}, "hooks"},
		{"claude hook user scope is invariant", hcc.TargetClaude, true, hook,
			[]string{"hooks/hooks.json"}, "hooks"},
		{"claude mcp", hcc.TargetClaude, false, mcp,
			[]string{".mcp.json"}, "mcpServers"},
		{"devin skill", hcc.TargetDevin, false, ir.NewSkill("probe-skill", "probe", "body", nil, nil, ir.SourceRef{}),
			[]string{"skills/probe-skill/SKILL.md"}, ""},
		{"devin command", hcc.TargetDevin, false, ir.NewCommand("probe-command", "probe", "body", nil, ir.SourceRef{}),
			[]string{"skills/probe-command/SKILL.md"}, ""},
		{"devin subagent", hcc.TargetDevin, false, ir.NewSubagent("probe-subagent", "probe", "body", "", nil, "", nil, "", nil, ir.SourceRef{}),
			[]string{"agents/probe-subagent.md"}, ""},
		{"devin hook project scope", hcc.TargetDevin, false, hook,
			[]string{"hooks.v1.json"}, ""},
		{"devin hook user scope", hcc.TargetDevin, true, hook,
			[]string{"config.json"}, "hooks"},
		{"devin mcp", hcc.TargetDevin, false, mcp,
			[]string{"mcp_config.json"}, "mcpServers"},
		{"devin mcp user scope is invariant", hcc.TargetDevin, true, mcp,
			[]string{"mcp_config.json"}, "mcpServers"},
		{"codex skill", hcc.TargetCodex, false, ir.NewSkill("probe-skill", "probe", "body", nil, nil, ir.SourceRef{}),
			[]string{".agents/skills/probe-skill/SKILL.md"}, ""},
		{"codex command", hcc.TargetCodex, false, ir.NewCommand("probe-command", "probe", "body", nil, ir.SourceRef{}),
			[]string{".agents/skills/probe-command/SKILL.md"}, ""},
		{"codex subagent", hcc.TargetCodex, false, ir.NewSubagent("probe-subagent", "probe", "body", "", nil, "", nil, "", nil, ir.SourceRef{}),
			[]string{".codex/agents/probe-subagent.toml"}, ""},
		{"codex hook", hcc.TargetCodex, false, hook,
			[]string{".codex/hooks.json"}, "hooks"},
		{"codex mcp", hcc.TargetCodex, false, mcp,
			[]string{".codex/config.toml"}, "mcp_servers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind := tt.resource.Kind()
			_, name, err := ir.ValidateResource(tt.resource)
			if err != nil {
				t.Fatalf("ValidateResource(%s): %v", kind, err)
			}
			opts := []hcc.Option{hcc.WithManagedBy("_managed_by", "agent-marketplace")}
			if tt.machineScope {
				opts = append(opts, hcc.WithScope(hcc.ScopeUser))
			}
			result, err := hcc.Compile(tt.resource, tt.target, opts...)
			if err != nil {
				t.Fatalf("Compile(%s, %s): %v", kind, tt.target, err)
			}
			var got []string
			for _, a := range result.Artifacts {
				got = append(got, a.Path)
			}
			slices.Sort(got)
			want := slices.Clone(tt.wantPaths)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("Compile(%s, %s, machine_scope=%v) emitted %v, want %v", kind, tt.target, tt.machineScope, got, want)
			}

			// Every emitted path must be one the descriptor predicts for this
			// kind: its per-resource path or its declared aggregate file.
			predicted := map[string]bool{}
			if resourcePath, ok := targetmeta.NativeResourcePath(tt.target, kind, name); ok {
				predicted[resourcePath] = true
			}
			aggregate, hasAggregate := targetmeta.NativeAggregateFileForScope(tt.target, kind, tt.machineScope)
			if hasAggregate {
				predicted[aggregate.Path] = true
				// Declared legacy locations also predict default compilation output.
				for _, legacy := range aggregate.Legacy {
					predicted[legacy] = true
				}
				if aggregate.Member != tt.wantMember {
					t.Errorf("descriptor member for %s aggregate = %q, want %q", kind, aggregate.Member, tt.wantMember)
				}
			} else if tt.wantMember != "" {
				t.Errorf("descriptor declares no aggregate for %s, want member %q", kind, tt.wantMember)
			}
			for _, p := range got {
				if !predicted[p] {
					t.Errorf("emitted path %q is not predicted by the %s native layout for %s", p, tt.target, kind)
				}
			}
		})
	}
}

// TestDescribedTargetsDeclareNoRuleTopology pins that the three described
// targets declare no rule resource path or aggregate: rules are an explicit
// scope-out for Devin and Codex and a no-primitive diagnostic for Claude, so
// the descriptor declares no rule topology.
func TestDescribedTargetsDeclareNoRuleTopology(t *testing.T) {
	for _, target := range []targetmeta.Target{targetmeta.TargetClaude, targetmeta.TargetDevin, targetmeta.TargetCodex} {
		if _, ok := targetmeta.NativeResourcePattern(target, ir.KindRule); ok {
			t.Errorf("%s declares a rule resource path, but no compiler emits one", target)
		}
		for _, machineScope := range []bool{false, true} {
			if file, ok := targetmeta.NativeAggregateFileForScope(target, ir.KindRule, machineScope); ok {
				t.Errorf("%s declares a rule aggregate %q, but no compiler emits one", target, file.Path)
			}
		}
	}
}
