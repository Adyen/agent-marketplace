package materialize

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

type ManagedLinkCleanupPlan struct {
	actions []managedLinkCleanupAction
}

type managedLinkCleanupAction struct {
	candidate  managedLinkCandidate
	key        outputKey
	repoRoot   string
	installRel string
	name       string
	path       string
}

func PrepareManagedLinkRemoval(ctx context.Context, repoRoot string, target resolver.ResolvedArtifact, remaining []resolver.ResolvedArtifact, modes config.MaterializationModes) (*ManagedLinkCleanupPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var err error
	repoRoot, err = filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	if target.Resource == nil || target.Resource.Kind() != target.Kind || target.Required || target.Unresolved || target.IsMarketplace || len(target.Harnesses) == 0 {
		return nil, fmt.Errorf("%w: cleanup requires a resolved non-required typed Git resource", ErrOwnedCleanupUnsupported)
	}
	targets := make(map[hcc.Target]bool)
	machine := target.Source.IsMachineScope()
	for _, name := range target.Harnesses {
		if modes.For(string(name), machine) != config.MaterializationModeSymlink {
			return nil, fmt.Errorf("%w: resource cleanup requires symlink mode", ErrOwnedCleanupUnsupported)
		}
		targets[name] = true
	}
	selected, err := compileManagedLinkCleanup(repoRoot, []resolver.ResolvedArtifact{target}, targets)
	if err != nil {
		return nil, err
	}
	after, err := compileManagedLinkCleanup(repoRoot, remaining, nil)
	if err != nil {
		return nil, err
	}
	occupied := make(map[string]bool)
	for key := range after.allOutputs {
		for _, group := range orderedSkillLinkGroups(after.skillLinks[key]) {
			for _, link := range group.links {
				scope, rel, path, err := managedResourceLinkLocation(key, repoRoot, group.installDirRel, link.name)
				if err != nil {
					return nil, err
				}
				occupied[path] = true
				occupied[filepath.Join(scope, filepath.FromSlash(rel))] = true
			}
		}
		if work := after.byTarget[key]; work != nil && len(work.artifacts) > 0 {
			outDir, err := managedCleanupOutputRoot(key, repoRoot)
			if err != nil {
				return nil, err
			}
			for _, plan := range materializePlans(key.target, outDir, work.artifacts, linkedComponents(after.skillLinks[key]), key.machineScope) {
				for _, a := range plan.artifacts {
					_, _, path, err := managedCleanupLocation(repoRoot, key.machineScope, plan.outDir, a.Path)
					if err != nil {
						return nil, err
					}
					occupied[path] = true
					occupied[filepath.Join(plan.outDir, filepath.FromSlash(a.Path))] = true
				}
			}
		}
		destinations, err := harness.ResolveDestinations(key.target, key.machineScope, repoRoot)
		if err != nil {
			return nil, err
		}
		for _, destination := range destinations {
			files := destination.Info.AggregateFilesForScope(key.machineScope)
			if destination.AggregateFile != "" {
				files = []string{destination.AggregateFile}
			}
			for _, file := range files {
				_, _, path, err := managedCleanupLocation(repoRoot, key.machineScope, destination.Root, file)
				if err != nil {
					return nil, err
				}
				occupied[path] = true
				occupied[filepath.Join(destination.Root, filepath.FromSlash(file))] = true
			}
		}
	}
	byPath := make(map[string]managedLinkCleanupAction)
	for name := range targets {
		key := outputKey{target: name, machineScope: machine}
		if work := selected.byTarget[key]; work != nil && len(work.artifacts) > 0 {
			return nil, fmt.Errorf("%w: resource has shared or additional output for %s", ErrOwnedCleanupUnsupported, name)
		}
		info, ok := harness.TargetInfoFor(name)
		if !ok || info.UsesFileCopy(target.Kind) {
			return nil, fmt.Errorf("%w: target does not use resource symlinks", ErrOwnedCleanupUnsupported)
		}
		covered := false
		for _, group := range orderedSkillLinkGroups(selected.skillLinks[key]) {
			for _, link := range group.links {
				if link.kind != target.Kind {
					return nil, fmt.Errorf("%w: unexpected resource kind in cleanup", ErrOwnedCleanupUnsupported)
				}
				scope, rel, path, err := managedResourceLinkLocation(key, repoRoot, group.installDirRel, link.name)
				if err != nil {
					return nil, err
				}
				for _, candidatePath := range []string{path, filepath.Join(scope, filepath.FromSlash(rel))} {
					for other := range occupied {
						inside, err := portablePathWithinRoot(candidatePath, other)
						if err != nil {
							return nil, err
						}
						contains, err := portablePathWithinRoot(other, candidatePath)
						if err != nil {
							return nil, err
						}
						if inside || contains {
							return nil, cleanupLinkConflict("resource output is still selected", nil)
						}
					}
				}
				if prior, exists := byPath[path]; exists && prior.candidate.target != link.target {
					return nil, fmt.Errorf("%w: targets disagree on a shared resource link", ErrOwnedCleanupUnsupported)
				}
				candidate, err := snapshotManagedLink(scope, rel, link.target)
				if err != nil {
					return nil, err
				}
				byPath[path] = managedLinkCleanupAction{candidate: candidate, key: key, repoRoot: repoRoot, installRel: group.installDirRel, name: link.name, path: path}
				covered = true
			}
		}
		if !covered {
			return nil, fmt.Errorf("%w: target %s has no complete resource-link output", ErrOwnedCleanupUnsupported, name)
		}
	}
	plan := &ManagedLinkCleanupPlan{}
	for _, action := range byPath {
		plan.actions = append(plan.actions, action)
	}
	sort.Slice(plan.actions, func(i, j int) bool { return plan.actions[i].path < plan.actions[j].path })
	return plan, nil
}

func compileManagedLinkCleanup(repoRoot string, resolved []resolver.ResolvedArtifact, targets map[hcc.Target]bool) (compilePhaseResult, error) {
	var work []skillTarget
	outputs := make(map[outputKey]bool)
	for _, resource := range resolver.MaterializationWinners(resolved) {
		machine := resource.Source.IsMachineScope()
		harnesses := resource.Harnesses
		if harnesses == nil {
			harnesses = []hcc.Target{hcc.TargetClaude}
		}
		for _, name := range harnesses {
			if targets != nil && !targets[name] {
				continue
			}
			if resource.Resource == nil || resource.Unresolved {
				return compilePhaseResult{}, fmt.Errorf("%w: incomplete remaining selection", ErrOwnedCleanupUnsupported)
			}
			work = append(work, skillTarget{skill: resource, target: name, machineScope: machine})
			outputs[outputKey{target: name, machineScope: machine}] = true
		}
	}
	compiled := compileWorkItems(work, nil, outputs, make(blockedOutputs), nil, repoRoot)
	if len(compiled.blocked) > 0 || diag.HasErrors(compiled.allDiags) {
		return compilePhaseResult{}, fmt.Errorf("%w: cleanup compilation is incomplete", ErrOwnedCleanupUnsupported)
	}
	return compiled, nil
}

func managedCleanupOutputRoot(key outputKey, repoRoot string) (string, error) {
	destinations, err := harness.ResolveDestinations(key.target, key.machineScope, repoRoot)
	if err != nil {
		return "", err
	}
	var roots []string
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) == 0 {
			roots = append(roots, destination.Root)
		}
	}
	if len(roots) == 1 {
		return roots[0], nil
	}
	if len(roots) == 0 {
		return "", fmt.Errorf("%w: no resource destination", ErrOwnedCleanupUnsupported)
	}
	if key.machineScope {
		return os.UserHomeDir()
	}
	return repoRoot, nil
}

func managedResourceLinkLocation(key outputKey, repoRoot, installRel, name string) (string, string, string, error) {
	if name == "." || name == ".." || filepath.Base(name) != name {
		return "", "", "", fmt.Errorf("%w: invalid resource link name", ErrOwnedCleanupUnsupported)
	}
	destinations, err := harness.ResolveDestinations(key.target, key.machineScope, repoRoot)
	if err != nil {
		return "", "", "", err
	}
	resources := destinations[:0:0]
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) == 0 {
			resources = append(resources, destination)
		}
	}
	rel := filepath.ToSlash(filepath.Join(installRel, name))
	if len(resources) == 1 {
		return managedCleanupLocation(repoRoot, key.machineScope, resources[0].Root, rel)
	}
	var root, routed string
	for _, destination := range resources {
		candidate, err := destination.RouteArtifact(rel)
		if err != nil {
			continue
		}
		if root != "" {
			return "", "", "", fmt.Errorf("%w: ambiguous resource destination", ErrOwnedCleanupUnsupported)
		}
		root, routed = destination.Root, candidate
	}
	if root == "" {
		return "", "", "", fmt.Errorf("%w: no resource destination", ErrOwnedCleanupUnsupported)
	}
	return managedCleanupLocation(repoRoot, key.machineScope, root, routed)
}

func managedCleanupLocation(repoRoot string, machine bool, root, rel string) (string, string, string, error) {
	if err := harness.ValidateRelPath(rel); err != nil {
		return "", "", "", err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", "", "", err
	}
	base := filepath.Dir(root)
	prefix := filepath.Base(root)
	if !machine {
		base, err = filepath.Abs(repoRoot)
		if err != nil {
			return "", "", "", err
		}
		canonical, err := ownership.CanonicalRoot(base)
		if err != nil {
			return "", "", "", err
		}
		inside, err := portablePathWithinRoot(base, root)
		if err != nil {
			return "", "", "", err
		}
		comparison := base
		if !inside {
			inside, err = portablePathWithinRoot(canonical, root)
			if err != nil || !inside {
				return "", "", "", cleanupLinkConflict("destination escapes repository scope", err)
			}
			comparison = canonical
		}
		prefix, err = filepath.Rel(comparison, root)
		if err != nil {
			return "", "", "", err
		}
	}
	rel = filepath.ToSlash(filepath.Join(prefix, filepath.FromSlash(rel)))
	canonical, err := ownership.CanonicalRoot(base)
	if err != nil {
		return "", "", "", err
	}
	return base, rel, filepath.Join(canonical, filepath.FromSlash(rel)), nil
}

func (a managedLinkCleanupAction) open() (*os.Root, error) {
	_, _, path, err := managedResourceLinkLocation(a.key, a.repoRoot, a.installRel, a.name)
	if err != nil {
		return nil, err
	}
	if path != a.path {
		return nil, cleanupLinkConflict("declared resource destination changed", nil)
	}
	return a.candidate.open()
}

func (p *ManagedLinkCleanupPlan) Diagnostics() []diag.Diagnostic { return nil }

func (p *ManagedLinkCleanupPlan) CommitScope(ctx context.Context, publish func() (dependencytxn.Result, error)) (result dependencytxn.Result, retErr error) {
	if p == nil || len(p.actions) == 0 || publish == nil {
		return result, fmt.Errorf("%w: empty resource cleanup plan", ErrOwnedCleanupUnsupported)
	}
	slog.Debug("managed resource link cleanup started", "links", len(p.actions))
	defer func() { slog.Debug("managed resource link cleanup finished", "links", len(p.actions), "error", retErr) }()
	for _, action := range p.actions {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		parent, err := action.open()
		if err != nil {
			return result, err
		}
		if err := parent.Close(); err != nil {
			return result, err
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result, retErr = publish()
	if retErr != nil {
		return result, retErr
	}
	for _, action := range p.actions {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		parent, err := action.open()
		if err != nil {
			return result, err
		}
		err = action.candidate.remove(parent, nil, nil)
		closeErr := parent.Close()
		if err != nil {
			return result, err
		}
		if closeErr != nil {
			return result, closeErr
		}
	}
	return result, nil
}
