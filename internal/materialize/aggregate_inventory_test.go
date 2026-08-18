package materialize

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/harness"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// probeResources returns one resource of every compilable ir.ResourceKind.
//
// The map is keyed by kind rather than written as a slice so
// TestAggregateProbeCoversEveryCompilableKind can prove it is exhaustive: a new
// kind added to the ir enumeration fails that test, which is what keeps the
// inventory below from silently losing coverage of the kind that introduces the
// next aggregate file.
func probeResources() map[ir.ResourceKind]ir.Resource {
	return map[ir.ResourceKind]ir.Resource{
		ir.KindSkill:     ir.NewSkill("probe-skill", "probe", "body", nil, nil, ir.SourceRef{}),
		ir.KindCommand:   ir.NewCommand("probe-command", "probe", "body", nil, ir.SourceRef{}),
		ir.KindSubagent:  ir.NewSubagent("probe-subagent", "probe", "body", "", nil, "", nil, "", nil, ir.SourceRef{}),
		ir.KindHook:      ir.NewHook("probe-hook", "PostToolUse", "echo probe", "command", nil, ir.SourceRef{}),
		ir.KindMcpServer: ir.NewMcpServer("probe-mcp", "probe-bin", nil, nil, nil, ir.SourceRef{}),
		ir.KindRule:      ir.NewRule("probe-rule", "probe", "body", []string{"**/*.go"}, false, nil, ir.SourceRef{}),
	}
}

// materializedAggregateFiles is the DERIVED inventory: the output-root-relative
// paths a target actually merges into in the given scope.
//
// Nothing here restates a declaration. It compiles a bundle containing one
// resource of every compilable kind with the real compilers, keeps only the
// artifacts the compiler marked MergeWithExisting (its own statement that the
// artifact is a shared aggregate to be merged into whatever is already on
// disk), and runs the artifacts through materializePlans — the production
// planner, which is what decides the directory each artifact is written to,
// including Gemini's ArtifactRootPrefix strip and Windsurf's scope-dependent
// .windsurf/ nesting. The path each merge artifact would be written to,
// relative to the target's install root for that scope, is the answer.
//
// Two option choices deserve their justification:
//
//   - WithManagedBy is passed for EVERY target, including Devin. In production
//     internal/materialize deliberately withholds the marker from Devin, whose
//     aggregates are reconciled through internal/ownership instead, and Devin's
//     compilers gate MergeWithExisting on the marker being configured. That is a
//     choice of ownership MECHANISM, not a statement about which files are
//     aggregates: Devin merges into hooks.v1.json / config.json / mcp_config.json
//     exactly as Claude merges into .mcp.json. Passing the marker asks all six
//     compilers the same question — "which of your artifacts are shared
//     aggregates?" — and gets a comparable answer.
//   - WithScope(ScopeUser) is passed for the machine-scope pass. Only the Devin
//     compilers read the scope today (project hooks.v1.json versus the `hooks`
//     member of user config.json); for the other five it is inert, and passing
//     it uniformly means a future target that starts honouring scope is measured
//     correctly without anyone remembering to update this list.
func materializedAggregateFiles(t *testing.T, target hcc.Target, machineScope bool) []string {
	t.Helper()
	outDir := t.TempDir()
	if target == hcc.Target(targetmeta.TargetCodex) && machineScope {
		t.Setenv("HOME", outDir)
		t.Setenv("CODEX_HOME", filepath.Join(outDir, ".codex"))
	}

	seen := make(map[string]bool)
	for _, kind := range sortedKinds(probeResources()) {
		resource := probeResources()[kind]
		opts := []hcc.Option{hcc.WithManagedBy("_managed_by", "agent-marketplace")}
		if machineScope {
			opts = append(opts, hcc.WithScope(hcc.ScopeUser))
		}
		result, err := hcc.Compile(resource, target, opts...)
		if err != nil {
			t.Fatalf("Compile(%s, %s, machine_scope=%v): %v", kind, target, machineScope, err)
		}
		if result == nil {
			continue
		}
		for _, plan := range materializePlans(target, outDir, result.Artifacts, nil, machineScope) {
			for _, a := range plan.artifacts {
				if !a.MergeWithExisting {
					continue
				}
				rel, relErr := filepath.Rel(outDir, filepath.Join(plan.outDir, a.Path))
				if relErr != nil {
					t.Fatalf("filepath.Rel(%s, %s): %v", outDir, filepath.Join(plan.outDir, a.Path), relErr)
				}
				seen[filepath.ToSlash(rel)] = true
			}
		}
	}

	out := make([]string, 0, len(seen))
	for rel := range seen {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

func sortedKinds(m map[ir.ResourceKind]ir.Resource) []ir.ResourceKind {
	kinds := make([]ir.ResourceKind, 0, len(m))
	for kind := range m {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	return kinds
}

// sortedTargets returns every target that has TargetInfo, so a target added to
// the registry is covered by the tests below without being listed here.
func sortedTargets(t *testing.T) []hcc.Target {
	t.Helper()
	if err := harness.TargetInfoError(); err != nil {
		t.Fatalf("harness.TargetInfoError: %v", err)
	}
	targets := make([]hcc.Target, 0, len(harness.TargetInfoMap))
	for target := range harness.TargetInfoMap {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
	if len(targets) == 0 {
		t.Fatal("harness.TargetInfoMap is empty; the inventory tests would be vacuous")
	}
	return targets
}

// TestAggregateProbeCoversEveryCompilableKind keeps materializedAggregateFiles
// honest. The inventory is only as complete as the bundle it compiles, so a new
// ir.ResourceKind must either get a probe resource or be explicitly excluded
// here with a reason.
func TestAggregateProbeCoversEveryCompilableKind(t *testing.T) {
	probes := probeResources()
	for kind := ir.KindSkill; kind <= ir.KindRule; kind++ {
		if kind == ir.KindCollection {
			// A collection is a virtual grouping; ir.ResourceIdentity rejects it
			// as "not compilable", so it can emit no artifact of any sort.
			if _, ok := probes[kind]; ok {
				t.Errorf("probeResources has an entry for the non-compilable kind %s", kind)
			}
			continue
		}
		probe, ok := probes[kind]
		if !ok {
			t.Errorf("probeResources has no resource for kind %s; the aggregate inventory would silently miss any aggregate that kind writes", kind)
			continue
		}
		if probe.Kind() != kind {
			t.Errorf("probeResources[%s] reports kind %s", kind, probe.Kind())
		}
	}
}

// TestAggregateFilesMatchMaterializedMergeArtifacts is the regression guard for
// bead agent-marketplace-5exj.27.
//
// harness.TargetInfo.AggregateFiles is documented as the single source of truth
// for the files a target merges into, and both safety-backup collection
// (pipeline.CollectManagedPaths) and Devin's marker-strip fence
// (aggregateProtectedPaths) read it. Four of six declarations had drifted from
// what the compilers actually emit: Claude omitted hooks/hooks.json and
// .claude-plugin/plugin.json, Cursor omitted hooks/hooks.json, Factory declared
// a settings.json nothing writes and omitted hooks/hooks.json, Gemini declared
// an mcp.json nothing writes and omitted the settings.json it does write, and
// the Project list was empty for all five marker-merge targets.
//
// The expectation is measured, never restated: it comes from compiling every
// resource kind and running the production planner. A test that listed the
// corrected paths inline would have passed against the broken declarations too,
// because it would have been the same act of declaration a second time.
func TestAggregateFilesMatchMaterializedMergeArtifacts(t *testing.T) {
	for _, target := range sortedTargets(t) {
		for _, machineScope := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/machine_scope=%v", target, machineScope), func(t *testing.T) {
				want := materializedAggregateFiles(t, target, machineScope)
				if len(want) == 0 {
					t.Fatalf("no merge artifact was produced for %s at machine_scope=%v; every current target has at least one, so the probe bundle is broken rather than the declaration", target, machineScope)
				}

				declared := append([]string(nil), harness.TargetInfoMap[target].AggregateFilesForScope(machineScope)...)
				for i, rel := range declared {
					declared[i] = filepath.ToSlash(rel)
				}
				sort.Strings(declared)

				if !reflect.DeepEqual(declared, want) {
					t.Errorf("harness.TargetInfoMap[%q].AggregateFiles (machine_scope=%v):\n declared = %v\n produced = %v\nDeclare exactly the files the compilers merge into: undeclared files are mutated with no safety backup, and declared files nothing emits are backed up for no reason.",
						target, machineScope, declared, want)
				}
			})
		}
	}
}

// TestAggregateFilesDeclarationIsCanonical pins the shape of the declaration
// itself, so the equality above cannot be satisfied by an accidentally
// duplicated or unsorted list that merely happens to compare equal after the
// test sorts it.
func TestAggregateFilesDeclarationIsCanonical(t *testing.T) {
	for _, target := range sortedTargets(t) {
		for _, machineScope := range []bool{false, true} {
			rels := harness.TargetInfoMap[target].AggregateFilesForScope(machineScope)
			if !sort.StringsAreSorted(rels) {
				t.Errorf("%s (machine_scope=%v): AggregateFiles %v is not sorted", target, machineScope, rels)
			}
			seen := make(map[string]bool, len(rels))
			for _, rel := range rels {
				if seen[rel] {
					t.Errorf("%s (machine_scope=%v): AggregateFiles lists %q twice", target, machineScope, rel)
				}
				seen[rel] = true
			}
		}
	}
}

// TestAggregateProtectedPathsStaysDevinOnly pins the fence against the REAL
// registry: aggregateProtectedPaths keeps a target's aggregates out of the
// marker-strip prune walk, and today exactly one target — Devin — is fenced.
// Widening AggregateFiles for the five marker-merge targets (bead
// agent-marketplace-5exj.27) must NOT widen that fence: excluding their
// aggregates from the walk would silently stop pruning stale managed entries out
// of hooks.json, mcp.json, plugin.json and settings.json.
//
// Bead agent-marketplace-5exj.29 changed WHY Devin is the only fenced target:
// the predicate is now harness.TargetInfo.OwnershipReconciledAggregates rather
// than SymlinkOnly. This test stays a statement about the registry as it stands;
// TestAggregateProtectedPathsKeysOffOwnership below is the statement about the
// predicate itself, over targets that do not exist yet.
func TestAggregateProtectedPathsCoversOwnershipTargets(t *testing.T) {
	codex := hcc.Target(targetmeta.TargetCodex)
	for _, target := range sortedTargets(t) {
		for _, machineScope := range []bool{false, true} {
			protected := aggregateProtectedPaths(target, machineScope)
			if target == hcc.TargetDevin || target == codex {
				declared := harness.TargetInfoMap[target].AggregateFilesForScope(machineScope)
				if len(protected) != len(declared) {
					t.Errorf("%s (machine_scope=%v): protected %v does not cover declared %v", target, machineScope, protected, declared)
				}
				for _, rel := range declared {
					if !protected[rel] {
						t.Errorf("%s (machine_scope=%v): %q is not protected from marker stripping", target, machineScope, rel)
					}
				}
				continue
			}
			if len(protected) != 0 {
				t.Errorf("%s (machine_scope=%v): aggregateProtectedPaths = %v, want empty; marker-merge targets must keep their aggregates in the prune walk", target, machineScope, protected)
			}
		}
	}
}

// TestAggregateProtectedPathsKeysOffOwnership is the bead
// agent-marketplace-5exj.29 acceptance test: the prune fence is driven by
// harness.TargetInfo.OwnershipReconciledAggregates, not by SymlinkOnly.
//
// The interesting cases are targets that do not exist in the registry — an
// ownership-reconciled target that is NOT symlink-only (what the
// marker-to-ownership migration produces the moment Cursor, Claude, Factory,
// Windsurf or Gemini flips) and a symlink-only target that is NOT
// ownership-reconciled. Neither can be expressed by naming a real target, and
// mutating harness.TargetInfoMap to invent one would leak global state across
// tests, so the predicate is exercised through aggregateProtectedPathsFor over
// synthetic TargetInfo values instead.
//
// Both mixed rows FAIL against the old `!info.SymlinkOnly` predicate — the
// ownership-reconciled non-symlink target would be unprotected (its user-owned
// aggregates decoded and rewritten by the marker-strip walk the migration
// exists to escape), and the symlink-only marker-merge target would be
// protected (its stale managed entries never pruned).
func TestAggregateProtectedPathsKeysOffOwnership(t *testing.T) {
	aggregates := harness.AggregateFileSet{
		Project: []string{"hooks.v1.json", "mcp_config.json"},
		User:    []string{"config.json", "mcp_config.json"},
	}

	tests := []struct {
		name string
		info harness.TargetInfo
		// wantProtected is true when the target's declared aggregates for the
		// scope under test must be fenced out of the marker-strip walk.
		wantProtected bool
	}{
		{
			name: "ownership-reconciled and symlink-only: the Devin shape",
			info: harness.TargetInfo{
				AggregateFiles:                aggregates,
				OwnershipReconciledAggregates: true,
				SymlinkOnly:                   true,
			},
			wantProtected: true,
		},
		{
			name: "ownership-reconciled but NOT symlink-only: the next migrated target",
			info: harness.TargetInfo{
				AggregateFiles:                aggregates,
				OwnershipReconciledAggregates: true,
				SymlinkOnly:                   false,
			},
			wantProtected: true,
		},
		{
			name: "symlink-only but NOT ownership-reconciled: still marker-merged",
			info: harness.TargetInfo{
				AggregateFiles:                aggregates,
				OwnershipReconciledAggregates: false,
				SymlinkOnly:                   true,
			},
			wantProtected: false,
		},
		{
			name: "neither property: the five marker-merge targets",
			info: harness.TargetInfo{
				AggregateFiles:                aggregates,
				OwnershipReconciledAggregates: false,
				SymlinkOnly:                   false,
			},
			wantProtected: false,
		},
		{
			name: "ownership-reconciled with no declared aggregates protects nothing",
			info: harness.TargetInfo{
				OwnershipReconciledAggregates: true,
			},
			wantProtected: false,
		},
	}

	for _, tt := range tests {
		for _, machineScope := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/machine_scope=%v", tt.name, machineScope), func(t *testing.T) {
				got := aggregateProtectedPathsFor(tt.info, machineScope)
				declared := tt.info.AggregateFilesForScope(machineScope)
				if !tt.wantProtected {
					if len(got) != 0 {
						t.Fatalf("aggregateProtectedPathsFor = %v, want empty", got)
					}
					return
				}
				if len(declared) == 0 {
					t.Fatal("test case declares no aggregates but expects protection; the assertion would be vacuous")
				}
				if len(got) != len(declared) {
					t.Fatalf("aggregateProtectedPathsFor = %v, want exactly the declared %v", got, declared)
				}
				for _, rel := range declared {
					if !got[rel] {
						t.Errorf("%q is not fenced out of the marker-strip walk", rel)
					}
				}
			})
		}
	}
}

// TestOwnershipAndSymlinkOnlyAreIndependentProperties guards the documentation
// on the two fields. They are unrelated properties that Devin alone happens to
// hold today, and that coincidence is exactly what made the old SymlinkOnly
// proxy look correct. The registry may legitimately grow a target with either
// one alone — that is what the migration does — so this test does NOT require
// them to agree. It requires the FENCE to follow ownership: every
// ownership-reconciled target with declared aggregates is protected, and no
// other target is, whatever its SymlinkOnly value.
func TestOwnershipAndSymlinkOnlyAreIndependentProperties(t *testing.T) {
	ownership := make([]hcc.Target, 0, len(harness.TargetInfoMap))
	for _, target := range sortedTargets(t) {
		info := harness.TargetInfoMap[target]
		if info.OwnershipReconciledAggregates {
			ownership = append(ownership, target)
		}
		for _, machineScope := range []bool{false, true} {
			protected := len(aggregateProtectedPaths(target, machineScope)) > 0
			want := info.OwnershipReconciledAggregates && len(info.AggregateFilesForScope(machineScope)) > 0
			if protected != want {
				t.Errorf("%s (machine_scope=%v): protected = %v, want %v (ownership_reconciled = %v, symlink_only = %v); the fence must follow ownership, never SymlinkOnly",
					target, machineScope, protected, want, info.OwnershipReconciledAggregates, info.SymlinkOnly)
			}
		}
	}
	if len(ownership) == 0 {
		t.Fatal("no target declares OwnershipReconciledAggregates; the assertions above would be vacuous")
	}
	if len(ownership) != 1 || ownership[0] != hcc.TargetDevin {
		t.Logf("ownership-reconciled targets are now %v; that is expected as the marker-to-ownership migration proceeds", ownership)
	}
}
