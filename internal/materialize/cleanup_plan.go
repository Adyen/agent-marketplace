package materialize

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var ErrOwnedCleanupUnsupported = errors.New("materialize: owned cleanup is unsupported")

type OwnedCleanupPlan struct {
	actions     []ownedCleanupAction
	diagnostics []diag.Diagnostic
}

type ownedCleanupAction struct {
	file *RepairFile
	want ownership.EntryValue
}

// PrepareOwnedMCPRemoval builds a deterministic cleanup plan for a
// non-required MCP resource whose owned entries are no longer selected. It
// rejects unsupported targets and stale ownership evidence without applying
// removals; the returned plan must be committed separately.
func PrepareOwnedMCPRemoval(ctx context.Context, repoRoot string, target resolver.ResolvedArtifact, remaining []resolver.ResolvedArtifact) (*OwnedCleanupPlan, error) {
	if target.Kind != ir.KindMcpServer || target.Resource == nil || target.Resource.Kind() != ir.KindMcpServer || target.Required {
		return nil, fmt.Errorf("%w: owned MCP cleanup requires a non-required typed MCP target", ErrOwnedCleanupUnsupported)
	}
	if len(target.Harnesses) == 0 {
		return nil, fmt.Errorf("%w: owned MCP cleanup requires explicit adapter targets", ErrOwnedCleanupUnsupported)
	}
	composition, err := newMaterializeAdapterComposition()
	if err != nil {
		return nil, err
	}
	targets := make(map[string]struct{}, len(target.Harnesses))
	for _, harnessTarget := range target.Harnesses {
		if _, ok := composition.bindings[harnessTarget]; !ok {
			return nil, fmt.Errorf("%w: target %q has no owned cleanup adapter", ErrOwnedCleanupUnsupported, harnessTarget)
		}
		targets[string(harnessTarget)] = struct{}{}
	}
	targetViews, err := RepairPreviews(ctx, repoRoot, []resolver.ResolvedArtifact{target}, nil)
	if err != nil {
		return nil, err
	}
	remainingViews, err := RepairPreviews(ctx, repoRoot, remaining, nil)
	if err != nil {
		return nil, err
	}
	remainingFiles := map[cleanupFileID]*RepairFile{}
	stores := map[string]*ownership.Store{}
	for _, view := range remainingViews {
		if _, wanted := targets[string(view.Target)]; !wanted {
			continue
		}
		if view.Preview == nil || view.Preview.HasErrors() {
			return nil, fmt.Errorf("%w: owned cleanup preview for %q is unavailable", ErrOwnedCleanupUnsupported, view.Target)
		}
		for _, file := range view.Preview.Files {
			root := file.store.CanonicalRoot()
			if shared, ok := stores[root]; ok {
				file.store = shared
			} else {
				stores[root] = file.store
			}
			remainingFiles[cleanupFileKey(string(file.Target), file.MachineScope, root, file.Rel)] = file
		}
	}
	actions := map[cleanupActionID]ownedCleanupAction{}
	covered := map[string]bool{}
	for _, view := range targetViews {
		if _, wanted := targets[string(view.Target)]; !wanted {
			continue
		}
		if view.Preview == nil || view.Preview.HasErrors() {
			return nil, fmt.Errorf("%w: owned cleanup target preview for %q is unavailable", ErrOwnedCleanupUnsupported, view.Target)
		}
		for _, targetFile := range view.Preview.Files {
			for _, desired := range targetFile.desired {
				root := targetFile.store.CanonicalRoot()
				file := remainingFiles[cleanupFileKey(string(view.Target), target.Source.IsMachineScope(), root, targetFile.Rel)]
				if file == nil {
					copy := *targetFile
					copy.desired = nil
					if shared, ok := stores[root]; ok {
						copy.store = shared
					} else {
						stores[root] = copy.store
					}
					file = &copy
				}
				for _, remainingDesired := range file.repairDesiredValues() {
					if remainingDesired.ID == desired.value.ID {
						return nil, fmt.Errorf("%w: entry is still selected", ErrRepairCandidateStale)
					}
				}
				key := cleanupActionKey(root, file.Rel, desired.value.ID)
				if prior, ok := actions[key]; ok && prior.want != desired.value {
					return nil, fmt.Errorf("%w: contradictory owned cleanup request", ErrOwnedCleanupUnsupported)
				}
				actions[key] = ownedCleanupAction{file: file, want: desired.value}
				covered[string(view.Target)] = true
			}
		}
	}
	for target := range targets {
		if !covered[target] {
			return nil, fmt.Errorf("%w: no proof-backed owned cleanup action for target %q", ErrOwnedCleanupUnsupported, target)
		}
	}
	plan := &OwnedCleanupPlan{actions: make([]ownedCleanupAction, 0, len(actions))}
	for _, action := range actions {
		if _, err := action.file.ownedRemovalMutation(action.want); err != nil {
			return nil, err
		}
		plan.actions = append(plan.actions, action)
	}
	sort.Slice(plan.actions, func(i, j int) bool {
		a, b := plan.actions[i], plan.actions[j]
		if a.file.store.CanonicalRoot() != b.file.store.CanonicalRoot() {
			return a.file.store.CanonicalRoot() < b.file.store.CanonicalRoot()
		}
		if a.file.Rel != b.file.Rel {
			return a.file.Rel < b.file.Rel
		}
		if a.want.ID.Kind != b.want.ID.Kind {
			return a.want.ID.Kind < b.want.ID.Kind
		}
		if a.want.ID.Container != b.want.ID.Container {
			return a.want.ID.Container < b.want.ID.Container
		}
		if a.want.ID.Key != b.want.ID.Key {
			return a.want.ID.Key < b.want.ID.Key
		}
		return a.want.ID.IdentityDigest < b.want.ID.IdentityDigest
	})
	return plan, nil
}

func (p *OwnedCleanupPlan) Diagnostics() []diag.Diagnostic {
	if p == nil {
		return nil
	}
	return append([]diag.Diagnostic(nil), p.diagnostics...)
}

func (p *OwnedCleanupPlan) CommitScope(ctx context.Context, publish func() (dependencytxn.Result, error)) (dependencytxn.Result, error) {
	if p == nil || len(p.actions) == 0 {
		return dependencytxn.Result{}, fmt.Errorf("%w: owned cleanup plan is empty", ErrOwnedCleanupUnsupported)
	}
	roots := make([]string, 0)
	stores := map[string]*ownership.Store{}
	for _, action := range p.actions {
		root := action.file.store.CanonicalRoot()
		if _, ok := stores[root]; !ok {
			stores[root] = action.file.store
			roots = append(roots, root)
		}
	}
	sort.Strings(roots)
	var result dependencytxn.Result
	var run func(int) error
	run = func(index int) error {
		if index < len(roots) {
			return stores[roots[index]].WithWriteSessionContext(ctx, "cleanup-owned-plan", func() error { return run(index + 1) })
		}
		for _, action := range p.actions {
			if _, err := action.file.ownedRemovalMutation(action.want); err != nil {
				return err
			}
		}
		published, err := publish()
		result = published
		if err != nil {
			return err
		}
		for _, action := range p.actions {
			if _, err := action.file.RemoveOwned(ctx, action.want, true); err != nil {
				return err
			}
			report := &Report{}
			appendCodexTrustReviewDiagnostic(report, outputKey{target: action.file.Target, machineScope: action.file.MachineScope}, action.file.Rel, false)
			p.diagnostics = append(p.diagnostics, report.Diags...)
		}
		return nil
	}
	if err := run(0); err != nil {
		return result, err
	}
	return result, nil
}

type cleanupFileID struct {
	target  string
	machine bool
	root    string
	rel     string
}

type cleanupActionID struct {
	root string
	rel  string
	id   ownership.EntryID
}

func cleanupFileKey(target string, machine bool, root, rel string) cleanupFileID {
	return cleanupFileID{target, machine, root, rel}
}

func cleanupActionKey(root, rel string, id ownership.EntryID) cleanupActionID {
	return cleanupActionID{root, rel, id}
}
