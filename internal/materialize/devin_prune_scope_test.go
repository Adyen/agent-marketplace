package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// Prune-scope tests for bead agent-marketplace-5exj.20.
//
// Escalating the Devin hook and MCP entry-skip diagnostics to errors routes
// them into compileWorkItems' `diag.HasErrors` branch, which used to set an
// output-key-wide prune block. These tests pin the narrowed contract from both
// sides, so neither direction can regress silently:
//
//   - the component (or aggregate file) the skipped resource would have written
//     into keeps its stale content, because a resource skipped this run is
//     indistinguishable from one that was removed;
//   - every OTHER component and aggregate file of the same output root prunes
//     normally.

// devinStalePruneCase is one "second run" of a two-run scenario. The first run
// materializes everything in `first`; the second run resolves `second`, which
// contains a resource that now errors.
type devinStalePruneCase struct {
	name string
	// second is built from the fixtures below by the runner.
	second func(fixtures devinPruneFixtures) []resolver.ResolvedArtifact
	// wantErrorCode is the error diagnostic the second run must report.
	wantErrorCode string
	// wantRetained and wantPruned are output-root-relative paths.
	wantRetained []string
	wantPruned   []string
	// wantAggregateRetains / wantAggregatePrunes are substrings of an aggregate
	// file that must survive / must be gone after the second run.
	wantAggregateRetains map[string]string
	wantAggregatePrunes  map[string]string
}

type devinPruneFixtures struct {
	keepSkill    resolver.ResolvedArtifact
	keepHook     resolver.ResolvedArtifact
	badHook      resolver.ResolvedArtifact
	badMcp       resolver.ResolvedArtifact
	badSubagent  resolver.ResolvedArtifact
	staleSkill   resolver.ResolvedArtifact
	staleAgent   resolver.ResolvedArtifact
	staleHook    resolver.ResolvedArtifact
	staleMcp     resolver.ResolvedArtifact
	firstRunSet  []resolver.ResolvedArtifact
	hooksAbsRel  string
	mcpAbsRel    string
	skillLinkRel string
	agentLinkRel string
}

func devinPruneCaseFixtures() devinPruneFixtures {
	f := devinPruneFixtures{
		keepSkill:  devinResolved("keep-skill", ir.NewSkill("keep-skill", "kept", "Body\n", nil, nil, ir.SourceRef{}), false),
		keepHook:   devinResolved("keep-hook", ir.NewHook("keep-hook", "PostToolUse", "echo keep", "command", nil, ir.SourceRef{}), false),
		staleSkill: devinResolved("stale-skill", ir.NewSkill("stale-skill", "stale", "Body\n", nil, nil, ir.SourceRef{}), false),
		staleAgent: devinResolved("stale-agent", ir.NewSubagent("stale-agent", "stale", "Body\n", "sonnet", nil, "", nil, "", nil, ir.SourceRef{}), false),
		staleHook:  devinResolved("stale-hook", ir.NewHook("stale-hook", "PostToolUse", "echo stale", "command", nil, ir.SourceRef{}), false),
		staleMcp:   devinResolved("stale-mcp", ir.NewMcpServer("stale-mcp", "stale-bin", nil, nil, nil, ir.SourceRef{}), false),

		// hook.devin.event-unmappable: Devin documents no SubagentStop event,
		// so the entry is skipped and (since .20) reported as an error.
		badHook: devinResolved("bad-hook", ir.NewHook("bad-hook", "SubagentStop", "echo never", "command", nil, ir.SourceRef{}), false),
		// mcp.devin.transport-unsupported.
		badMcp: devinResolved("bad-mcp", ir.NewMcpServerWithOptions("bad-mcp", "", nil, nil, nil, ir.SourceRef{},
			ir.WithMcpURL("https://example.invalid/mcp"), ir.WithMcpTransport("websocket")), false),
		// subagent.security-restriction-unsupported: an error that writes into a
		// component DIRECTORY rather than an aggregate file, which is the
		// symlink/plan half of the same contract.
		//
		// This fixture used to reach that state through
		// subagent.devin.tool-unknown (`tools: [WebFetch]`). Bead
		// agent-marketplace-pu6b made an unrepresentable allowed-tools
		// restriction a WARNING that omits the key and still publishes the
		// profile, so it no longer suppresses anything and cannot exercise this
		// test. `isolation` is used instead: Devin has no isolation field at all,
		// so that suppression is unchanged and still fails closed.
		badSubagent: devinResolved("bad-agent", ir.NewSubagent("bad-agent", "bad", "Body\n", "sonnet",
			nil, "", nil, "worktree", nil, ir.SourceRef{}), false),

		hooksAbsRel:  harness.DevinProjectHooksFile,
		mcpAbsRel:    harness.DevinMcpConfigFile,
		skillLinkRel: filepath.Join("skills", "stale-skill"),
		agentLinkRel: filepath.Join("agents", "stale-agent.md"),
	}
	f.firstRunSet = []resolver.ResolvedArtifact{
		f.keepSkill, f.keepHook, f.staleSkill, f.staleAgent, f.staleHook, f.staleMcp,
	}
	return f
}

func TestDevinEscalatedSkipPrunesEverythingItDidNotTouch(t *testing.T) {
	tests := []devinStalePruneCase{
		{
			name: "an erroring hook holds back hooks.v1.json only",
			second: func(f devinPruneFixtures) []resolver.ResolvedArtifact {
				return []resolver.ResolvedArtifact{f.keepSkill, f.keepHook, f.badHook}
			},
			wantErrorCode: diag.CodeHookDevinEventUnmappable,
			// The skipped hook writes to no component directory, so the
			// symlink components prune exactly as they would on a clean run.
			wantPruned: []string{filepath.Join("skills", "stale-skill"), filepath.Join("agents", "stale-agent.md")},
			wantAggregateRetains: map[string]string{
				harness.DevinProjectHooksFile: "echo stale",
			},
			wantAggregatePrunes: map[string]string{
				harness.DevinMcpConfigFile: "stale-mcp",
			},
		},
		{
			name: "an erroring MCP server holds back mcp_config.json only",
			second: func(f devinPruneFixtures) []resolver.ResolvedArtifact {
				return []resolver.ResolvedArtifact{f.keepSkill, f.keepHook, f.badMcp}
			},
			wantErrorCode: diag.CodeMcpDevinTransportUnsupported,
			wantPruned:    []string{filepath.Join("skills", "stale-skill"), filepath.Join("agents", "stale-agent.md")},
			wantAggregateRetains: map[string]string{
				harness.DevinMcpConfigFile: "stale-mcp",
			},
			wantAggregatePrunes: map[string]string{
				harness.DevinProjectHooksFile: "echo stale",
			},
		},
		{
			name: "an erroring subagent holds back the agents component only",
			second: func(f devinPruneFixtures) []resolver.ResolvedArtifact {
				return []resolver.ResolvedArtifact{f.keepSkill, f.keepHook, f.badSubagent}
			},
			wantErrorCode: diag.CodeSubagentSecurityRestrictionUnsupported,
			wantRetained:  []string{filepath.Join("agents", "stale-agent.md")},
			wantPruned:    []string{filepath.Join("skills", "stale-skill")},
			wantAggregatePrunes: map[string]string{
				harness.DevinProjectHooksFile: "echo stale",
				harness.DevinMcpConfigFile:    "stale-mcp",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoRoot, _ := devinEnv(t)
			root := devinProjectRoot(repoRoot)
			f := devinPruneCaseFixtures()

			if _, err := Run(context.Background(), repoRoot, f.firstRunSet); err != nil {
				t.Fatalf("first Run: %v", err)
			}
			for _, rel := range []string{f.skillLinkRel, f.agentLinkRel} {
				if _, err := os.Lstat(filepath.Join(root, rel)); err != nil {
					t.Fatalf("setup: %s was not materialized: %v", rel, err)
				}
			}
			if got := readFileString(t, filepath.Join(root, f.hooksAbsRel)); !strings.Contains(got, "echo stale") {
				t.Fatalf("setup: the stale hook was not written:\n%s", got)
			}
			if got := readFileString(t, filepath.Join(root, f.mcpAbsRel)); !strings.Contains(got, "stale-mcp") {
				t.Fatalf("setup: the stale MCP server was not written:\n%s", got)
			}

			report, err := Run(context.Background(), repoRoot, tt.second(f))
			if err != nil {
				t.Fatalf("second Run: %v", err)
			}
			d, ok := findDiag(report.Diags, tt.wantErrorCode)
			if !ok {
				t.Fatalf("expected %s, got %v", tt.wantErrorCode, report.Diags)
			}
			if d.Severity != diag.SeverityError {
				t.Fatalf("%s severity = %v, want error so sync exits non-zero", d.Code, d.Severity)
			}
			if d.Target != string(hcc.TargetDevin) {
				t.Fatalf("%s target = %q, want %q", d.Code, d.Target, hcc.TargetDevin)
			}

			for _, rel := range tt.wantRetained {
				if _, statErr := os.Lstat(filepath.Join(root, rel)); statErr != nil {
					t.Errorf("%s was pruned even though the skipped resource writes into that component: %v", rel, statErr)
				}
			}
			for _, rel := range tt.wantPruned {
				if _, statErr := os.Lstat(filepath.Join(root, rel)); !os.IsNotExist(statErr) {
					t.Errorf("%s survived: prune suppression leaked outside the skipped resource's own output (stat = %v)", rel, statErr)
				}
			}
			for rel, want := range tt.wantAggregateRetains {
				if got := readFileString(t, filepath.Join(root, rel)); !strings.Contains(got, want) {
					t.Errorf("%s no longer holds %q; a postponed prune must leave the file untouched:\n%s", rel, want, got)
				}
			}
			for rel, unwanted := range tt.wantAggregatePrunes {
				if got := readFileString(t, filepath.Join(root, rel)); strings.Contains(got, unwanted) {
					t.Errorf("%s still holds the stale entry %q; prune suppression leaked to an unrelated aggregate:\n%s", rel, unwanted, got)
				}
			}
		})
	}
}

// TestBlockedComponentsForMapsEveryDevinKind pins the mapping the narrowed
// prune block depends on. It is the unit-level companion to the on-disk test
// above: if a kind ever stops mapping to its component, the fallback must be
// the fail-closed wildcard, never "nothing".
func TestBlockedComponentsForMapsEveryDevinKind(t *testing.T) {
	tests := []struct {
		name         string
		target       hcc.Target
		machineScope bool
		resource     ir.Resource
		want         []string
	}{
		{
			name:     "a devin hook writes to an aggregate file, so it blocks no component",
			target:   hcc.TargetDevin,
			resource: ir.NewHook("h", "PostToolUse", "echo", "command", nil, ir.SourceRef{}),
			want:     nil,
		},
		{
			name:         "a machine-scope devin hook is equally file-scoped",
			target:       hcc.TargetDevin,
			machineScope: true,
			resource:     ir.NewHook("h", "PostToolUse", "echo", "command", nil, ir.SourceRef{}),
			want:         nil,
		},
		{
			name:     "a devin mcp server writes to an aggregate file",
			target:   hcc.TargetDevin,
			resource: ir.NewMcpServer("m", "bin", nil, nil, nil, ir.SourceRef{}),
			want:     nil,
		},
		{
			name:     "a codex hook writes to an aggregate file, so it blocks no component",
			target:   hcc.TargetCodex,
			resource: ir.NewHook("h", "Stop", "echo", "command", nil, ir.SourceRef{}),
			want:     nil,
		},
		{
			name:     "a codex mcp server writes to an aggregate file",
			target:   hcc.TargetCodex,
			resource: ir.NewMcpServer("m", "bin", nil, nil, nil, ir.SourceRef{}),
			want:     nil,
		},
		{
			name:     "a devin skill blocks the skills component",
			target:   hcc.TargetDevin,
			resource: ir.NewSkill("s", "d", "b", nil, nil, ir.SourceRef{}),
			want:     []string{"skills"},
		},
		{
			name:     "a devin command compiles to a skill directory, so it blocks skills",
			target:   hcc.TargetDevin,
			resource: ir.NewCommand("c", "d", "b", nil, ir.SourceRef{}),
			want:     []string{"skills"},
		},
		{
			name:     "a devin subagent blocks the agents component",
			target:   hcc.TargetDevin,
			resource: ir.NewSubagent("a", "d", "b", "sonnet", nil, "", nil, "", nil, ir.SourceRef{}),
			want:     []string{"agents"},
		},
		{
			name:     "a devin rule maps to no component and falls back to the wildcard",
			target:   hcc.TargetDevin,
			resource: ir.NewRule("r", "d", "b", nil, false, nil, ir.SourceRef{}),
			want:     []string{blockedAllComponents},
		},
		{
			name:     "a claude hook still blocks its own hooks component",
			target:   hcc.TargetClaude,
			resource: ir.NewHook("h", "PostToolUse", "echo", "command", nil, ir.SourceRef{}),
			want:     []string{"hooks"},
		},
		{
			name:     "a claude mcp server has no component directory and falls back to the wildcard",
			target:   hcc.TargetClaude,
			resource: ir.NewMcpServer("m", "bin", nil, nil, nil, ir.SourceRef{}),
			want:     []string{blockedAllComponents},
		},
		{
			name:         "a machine-scope windsurf command uses the machine component directory",
			target:       hcc.TargetWindsurf,
			machineScope: true,
			resource:     ir.NewCommand("c", "d", "b", nil, ir.SourceRef{}),
			want:         []string{"global_workflows"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := outputKey{target: tt.target, machineScope: tt.machineScope}
			w := skillTarget{
				skill:        devinResolved("res", tt.resource, tt.machineScope),
				target:       tt.target,
				machineScope: tt.machineScope,
			}
			got := blockedComponentsFor(key, w)
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

	t.Run("a resolved artifact with no IR resource blocks the whole output", func(t *testing.T) {
		key := outputKey{target: hcc.TargetDevin}
		got := blockedComponentsFor(key, skillTarget{target: hcc.TargetDevin})
		if len(got) != 1 || got[0] != blockedAllComponents {
			t.Fatalf("blockedComponentsFor(nil resource) = %v, want the wildcard", got)
		}
	})
}

// TestComponentBlockedHonoursTheWildcardAndNormalizesPaths pins the two lookup
// rules the plan/link prune sites depend on: the wildcard blocks everything,
// and an install-dir-relative path is reduced to its component name first.
func TestComponentBlockedHonoursTheWildcardAndNormalizesPaths(t *testing.T) {
	tests := []struct {
		name      string
		blocked   map[string]bool
		component string
		want      bool
	}{
		{name: "no block at all", blocked: nil, component: "skills", want: false},
		{name: "registered but empty blocks nothing", blocked: map[string]bool{}, component: "skills", want: false},
		{name: "exact component", blocked: map[string]bool{"skills": true}, component: "skills", want: true},
		{name: "other component", blocked: map[string]bool{"agents": true}, component: "skills", want: false},
		{name: "wildcard blocks a named component", blocked: map[string]bool{blockedAllComponents: true}, component: "skills", want: true},
		{name: "wildcard blocks the unnamed root plan", blocked: map[string]bool{blockedAllComponents: true}, component: "", want: true},
		{name: "a named component never blocks the root plan", blocked: map[string]bool{"skills": true}, component: "", want: false},
		{name: "nested install dirs reduce to their component", blocked: map[string]bool{"skills": true}, component: "skills/nested", want: true},
		{name: "the windsurf repo-scope prefix is stripped", blocked: map[string]bool{"workflows": true}, component: ".windsurf/workflows", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := componentBlocked(tt.blocked, tt.component); got != tt.want {
				t.Fatalf("componentBlocked(%v, %q) = %v, want %v", tt.blocked, tt.component, got, tt.want)
			}
		})
	}
}
