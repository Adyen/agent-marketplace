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

func devinPruneCaseFixtures(t *testing.T) devinPruneFixtures {
	f := devinPruneFixtures{
		keepSkill:  devinResolved(t, "keep-skill", ir.NewSkill("keep-skill", "kept", "Body\n", nil, nil, ir.SourceRef{}), false),
		keepHook:   devinResolved(t, "keep-hook", ir.NewHook("keep-hook", "PostToolUse", "echo keep", "command", nil, ir.SourceRef{}), false),
		staleSkill: devinResolved(t, "stale-skill", ir.NewSkill("stale-skill", "stale", "Body\n", nil, nil, ir.SourceRef{}), false),
		staleAgent: devinResolved(t, "stale-agent", ir.NewSubagent("stale-agent", "stale", "Body\n", "sonnet", nil, "", nil, "", nil, ir.SourceRef{}), false),
		staleHook:  devinResolved(t, "stale-hook", ir.NewHook("stale-hook", "PostToolUse", "echo stale", "command", nil, ir.SourceRef{}), false),
		staleMcp:   devinResolved(t, "stale-mcp", ir.NewMcpServer("stale-mcp", "stale-bin", nil, nil, nil, ir.SourceRef{}), false),

		// hook.devin.event-unmappable: Devin documents no SubagentStop event,
		// so the entry is skipped and (since .20) reported as an error.
		badHook: devinResolved(t, "bad-hook", ir.NewHook("bad-hook", "SubagentStop", "echo never", "command", nil, ir.SourceRef{}), false),
		// mcp.devin.transport-unsupported.
		badMcp: devinResolved(t, "bad-mcp", ir.NewMcpServerWithOptions("bad-mcp", "", nil, nil, nil, ir.SourceRef{},
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
		badSubagent: devinResolved(t, "bad-agent", ir.NewSubagent("bad-agent", "bad", "Body\n", "sonnet",
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
			name: "an erroring hook prunes stale hooks it does not select",
			second: func(f devinPruneFixtures) []resolver.ResolvedArtifact {
				return []resolver.ResolvedArtifact{f.keepSkill, f.keepHook, f.badHook}
			},
			wantErrorCode: diag.CodeHookDevinEventUnmappable,
			// The skipped hook writes to no component directory, so the
			// symlink components prune exactly as they would on a clean run.
			wantPruned: []string{filepath.Join("skills", "stale-skill"), filepath.Join("agents", "stale-agent.md")},
			wantAggregatePrunes: map[string]string{
				harness.DevinProjectHooksFile: "echo stale",
				harness.DevinMcpConfigFile:    "stale-mcp",
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
			f := devinPruneCaseFixtures(t)

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
