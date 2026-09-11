package materialize

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
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
		ir.KindHook:      ir.NewHookWithMatcher("probe-hook", "PreToolUse", "Bash", "echo probe", "command", nil, ir.SourceRef{}),
		ir.KindMcpServer: ir.NewMcpServer("probe-mcp", "probe-bin", nil, nil, nil, ir.SourceRef{}),
		ir.KindRule:      ir.NewRule("probe-rule", "probe", "body", []string{"**/*.go"}, false, nil, ir.SourceRef{}),
	}
}

// materializedAggregateFiles is the derived inventory: the output-root-relative
// paths a target reconciles in the given scope.
//
// It compiles a bundle containing one resource of every compilable kind with
// the production compilers, keeps only the artifacts whose reconciliation
// policy identifies them as shared aggregates
// (either marker-reconciled or externally reconciled), and runs the artifacts
// through materializePlans — the production planner, which is what decides the
// directory each artifact is written to, including Gemini's ArtifactRootPrefix
// strip and Windsurf's scope-dependent .windsurf/ nesting. The path each shared
// artifact would be written to, relative to the target's install root for that
// scope, is the answer.
//
// Two option choices deserve their justification:
//
//   - WithManagedBy is passed for EVERY target, including Devin. In production
//     internal/materialize deliberately withholds the marker from Devin, whose
//     aggregates are reconciled through internal/ownership instead. Marker
//     metadata lets marker-capable compilers identify their shared aggregates;
//     externally reconciled compilers identify theirs through their policy. The
//     derived inventory accepts both ownership mechanisms.
//   - WithScope(ScopeUser) is passed for the machine-scope pass. Only the Devin
//     compilers read the scope today (project hooks.v1.json versus the `hooks`
//     member of user config.json); for the other five it is inert, and passing
//     it uniformly means a future target that starts honouring scope is measured
//     correctly without anyone remembering to update this list.
func materializedAggregateFiles(t *testing.T, target hcc.Target, machineScope bool) []string {
	t.Helper()
	outDir := t.TempDir()
	relativeRoot := outDir
	if target == hcc.Target(targetmeta.TargetCodex) {
		relativeRoot, _ = ownership.CanonicalRoot(outDir)
	}
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
				policy, policyErr := a.Policy()
				if policyErr != nil {
					t.Fatalf("artifact %q policy: %v", a.Path, policyErr)
				}
				if !a.MergeWithExisting && !policy.IsExternal() {
					continue
				}
				rel, relErr := filepath.Rel(relativeRoot, filepath.Join(plan.outDir, a.Path))
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

// sortedTargets returns every legacy target plus every validated installation
// adapter target.
func sortedTargets(t *testing.T) []hcc.Target {
	t.Helper()
	if err := harness.TargetInfoError(); err != nil {
		t.Fatalf("harness.TargetInfoError: %v", err)
	}
	seen := make(map[hcc.Target]bool)
	for target := range harness.TargetInfoMap {
		seen[target] = true
	}
	registry, err := harness.NewInstallationAdapterRegistry()
	if err != nil {
		t.Fatalf("installation adapter registry: %v", err)
	}
	for _, target := range registry.Targets() {
		seen[target] = true
	}
	targets := make([]hcc.Target, 0, len(seen))
	for target := range seen {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
	if len(targets) == 0 {
		t.Fatal("no target metadata or adapters; the inventory tests would be vacuous")
	}
	return targets
}

func targetInfoForTest(t *testing.T, target hcc.Target) harness.TargetInfo {
	t.Helper()
	info, ok := harness.TargetInfoFor(target)
	if !ok {
		t.Fatalf("target %q has no validated TargetInfo", target)
	}
	return info
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
// The TargetInfo returned by either the legacy map or an installation adapter
// carries AggregateFiles as the single source of truth for the files a target
// merges into. Both safety-backup collection (pipeline.CollectManagedPaths) and
// the ownership-reconciled aggregate fence (aggregateProtectedPaths) read it.
// Four of six declarations had drifted from what the compilers actually emit:
// Claude omitted hooks/hooks.json and
// .claude-plugin/plugin.json, Cursor omitted hooks/hooks.json, Factory declared
// a settings.json nothing writes and omitted hooks/hooks.json, Gemini declared
// an mcp.json nothing writes and omitted the settings.json it does write, and
// the Project list was empty for all five marker-merge targets.
//
// The expectation comes from compiling every resource kind and running the
// production planner. A test that listed the corrected paths inline would have
// passed against the broken declarations too,
// because it would have been the same act of declaration a second time.
func TestAggregateFilesMatchMaterializedMergeArtifacts(t *testing.T) {
	for _, target := range sortedTargets(t) {
		for _, machineScope := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/machine_scope=%v", target, machineScope), func(t *testing.T) {
				want := materializedAggregateFiles(t, target, machineScope)
				if len(want) == 0 {
					t.Fatalf("no shared aggregate artifact was produced for %s at machine_scope=%v; every current target has at least one, so the probe bundle is broken rather than the declaration", target, machineScope)
				}

				declared := append([]string(nil), targetInfoForTest(t, target).AggregateFilesForScope(machineScope)...)
				for i, rel := range declared {
					declared[i] = filepath.ToSlash(rel)
				}
				sort.Strings(declared)

				if !reflect.DeepEqual(declared, want) {
					t.Errorf("target metadata for %q has incorrect AggregateFiles (machine_scope=%v):\n declared = %v\n produced = %v\nDeclare exactly the files the compilers merge into: undeclared files are mutated with no safety backup, and declared files nothing emits are backed up for no reason.",
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
			rels := targetInfoForTest(t, target).AggregateFilesForScope(machineScope)
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

// aggregateProtectedPaths keeps a target's aggregates out of the marker-strip
// prune walk, and today Devin and Codex are fenced. Widening AggregateFiles for
// the five marker-merge targets (bead agent-marketplace-5exj.27) must NOT widen
// that fence: excluding their aggregates from the walk would silently stop
// pruning stale managed entries out of hooks.json, mcp.json, plugin.json and
// settings.json.
//
// Bead agent-marketplace-5exj.29 changed why ownership-reconciled targets are
// fenced: the predicate is now harness.TargetInfo.OwnershipReconciledAggregates
// rather than SymlinkOnly. This test stays a statement about the registry as it
// stands; TestAggregateProtectedPathsKeysOffOwnership below is the statement
// about the predicate itself, over targets that do not exist yet.
func TestAggregateProtectedPathsCoversOwnershipTargets(t *testing.T) {
	codex := hcc.Target(targetmeta.TargetCodex)
	for _, target := range sortedTargets(t) {
		for _, machineScope := range []bool{false, true} {
			protected := aggregateProtectedPaths(target, machineScope)
			if target == hcc.TargetDevin || target == codex {
				declared := targetInfoForTest(t, target).AggregateFilesForScope(machineScope)
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

func TestOwnedAggregateHandlerInventoryMatchesOwnershipTargets(t *testing.T) {
	for _, target := range sortedTargets(t) {
		hasHandler := ownedAggregateHandlerFor(target) != nil
		wantHandler := targetInfoForTest(t, target).OwnershipReconciledAggregates
		if hasHandler != wantHandler {
			t.Errorf("%s: owned aggregate handler = %v, ownership-reconciled metadata = %v", target, hasHandler, wantHandler)
		}
	}
}

// TestAggregateProtectedPathsKeysOffOwnership retains the bead
// agent-marketplace-5exj.29 acceptance contract: the prune fence follows
// harness.TargetInfo.OwnershipReconciledAggregates, not installation mode.
//
// Synthetic declarations exercise both reconciliation policies without
// mutating the global target registry. File and symlink installation share
// the same aggregate fence; the former SymlinkOnly capability is removed.
//
// Ownership-reconciled aggregates must remain protected from marker stripping,
// while marker-merged aggregates must remain eligible for stale-entry pruning.
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
			name: "ownership-reconciled aggregates",
			info: harness.TargetInfo{
				AggregateFiles:                aggregates,
				OwnershipReconciledAggregates: true,
			},
			wantProtected: true,
		},
		{
			name: "marker-merged aggregates",
			info: harness.TargetInfo{
				AggregateFiles:                aggregates,
				OwnershipReconciledAggregates: false,
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

// TestAggregateFenceFollowsOwnership guards the registry-wide contract:
// every ownership-reconciled target with declared aggregates is protected,
// and no other target is. Installation mode is not a target capability.
func TestAggregateFenceFollowsOwnership(t *testing.T) {
	ownership := make([]hcc.Target, 0, len(harness.TargetInfoMap))
	for _, target := range sortedTargets(t) {
		info := targetInfoForTest(t, target)
		if info.OwnershipReconciledAggregates {
			ownership = append(ownership, target)
		}
		for _, machineScope := range []bool{false, true} {
			protected := len(aggregateProtectedPaths(target, machineScope)) > 0
			want := info.OwnershipReconciledAggregates && len(info.AggregateFilesForScope(machineScope)) > 0
			if protected != want {
				t.Errorf("%s (machine_scope=%v): protected = %v, want %v (ownership_reconciled = %v); the fence must follow ownership",
					target, machineScope, protected, want, info.OwnershipReconciledAggregates)
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
