package lifecycle

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

type PreparedInstallLocks struct {
	Documents       map[string]lockfile.Document
	LockPaths       map[config.Source]string
	Bindings        map[string][]lockfile.GitRegistry
	commits         map[string]string
	locked          map[string]bool
	targetLockPaths map[string]bool
	discoveryConfig *config.Config
	reads           []dependencytxn.ReadAssertion
	ctx             context.Context
	release         func()
}

func PrepareInstallDependencies(ctx context.Context, root, local string, edits []pipeline.StagedConfigEdit) (pipeline.PreparedInstallDependencies, error) {
	state := &PreparedInstallLocks{
		Documents: make(map[string]lockfile.Document), LockPaths: make(map[config.Source]string),
		Bindings: make(map[string][]lockfile.GitRegistry), commits: make(map[string]string),
		locked: make(map[string]bool), targetLockPaths: make(map[string]bool),
	}
	for _, edit := range edits {
		LockPath, err := lockfile.PathForConfig(edit.Path)
		if err != nil {
			return pipeline.PreparedInstallDependencies{}, err
		}
		state.targetLockPaths[LockPath] = true
	}
	var warnings []error
	option, _, lease, err := PrepareRuntimeLocks(ctx, root, local, LockMode{StagedConfigEdits: edits, PrepareInstall: state, WarningSink: &warnings})
	if err != nil {
		return pipeline.PreparedInstallDependencies{}, err
	}
	var mu sync.Mutex
	active, used := true, false
	release := func() {
		mu.Lock()
		defer mu.Unlock()
		if active {
			active = false
			if state.release != nil {
				state.release()
			}
		}
	}
	reconcile := func(ctx context.Context, repoRoot, localDir string, ignoreBlocked bool, finalEdits []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
		mu.Lock()
		defer mu.Unlock()
		if !active || used || repoRoot != root || localDir != local || ignoreBlocked {
			return pipeline.DependencyLockView{}, fmt.Errorf("%w: prepared install scope changed or ended", dependencytxn.ErrGenerationConflict)
		}
		used = true
		var warnings []error
		option, _, lease, err := PrepareRuntimeLocks(ctx, root, local, LockMode{Offline: true, StagedConfigEdits: finalEdits, CommitReads: state.reads, ReuseInstall: state, WarningSink: &warnings})
		if err != nil {
			return pipeline.DependencyLockView{}, err
		}
		return pipeline.DependencyLockView{CatalogOption: option, ExactLockLease: lease, Warnings: warnings}, nil
	}
	bind := func(target config.Source, entry config.RegistryEntry) error {
		mu.Lock()
		defer mu.Unlock()
		if !active || used {
			return fmt.Errorf("%w: prepared install scope ended", dependencytxn.ErrGenerationConflict)
		}
		return state.BindRegistry(target, entry)
	}
	resolvedCommit := func(entry config.RegistryEntry) (pipeline.PreparedSourceCommit, error) {
		canonical, err := CanonicalRegistryURL(entry.URL)
		if err != nil {
			return pipeline.PreparedSourceCommit{}, err
		}
		identity := gitLockIdentity(canonical, entry.Ref)
		commit, ok := state.commits[identity]
		if !ok || commit == "" {
			return pipeline.PreparedSourceCommit{}, fmt.Errorf("prepared install has no exact commit for %s@%s", canonical, entry.Ref)
		}
		return pipeline.PreparedSourceCommit{Commit: commit, Locked: state.locked[identity]}, nil
	}
	return pipeline.PreparedInstallDependencies{
		Context: state.ctx, DiscoveryConfig: state.discoveryConfig,
		View:      pipeline.DependencyLockView{CatalogOption: option, ExactLockLease: lease, Warnings: warnings},
		Reconcile: reconcile, BindRegistry: bind, ResolvedCommit: resolvedCommit, Release: release,
	}, nil
}

// lockedCommitIs reports whether the existing lock already pins identity at commit.
func lockedCommitIs(current *lockfile.Document, identity, commit string) bool {
	if current == nil {
		return false
	}
	for _, registry := range current.Registries {
		if gitLockIdentity(registry.URL, registry.Ref) == identity {
			return registry.Commit.Hex() == commit
		}
	}
	return false
}

func installPreparationReads(ec config.EffectiveConfig, staged map[string]pipeline.StagedConfigEdit, plan *PreparedInstallLocks) ([]dependencytxn.ReadAssertion, error) {
	reads, err := effectiveReadAssertions(ec, staged, nil)
	if err != nil {
		return nil, err
	}
	represented := make(map[string]bool, len(reads))
	for _, read := range reads {
		represented[read.Path] = true
	}
	for _, scoped := range []struct {
		source config.Source
		layer  config.LayerResult
	}{
		{config.Organization, ec.Organization},
		{config.UserPersonal, ec.User},
		{config.RepoCommitted, ec.Repo},
		{config.RepoPersonal, ec.LocalLayer},
	} {
		layer := scoped.layer
		if layer.Path == "" {
			continue
		}
		path, err := canonicalConfigDependencyPath(layer.Path)
		if err != nil {
			return nil, err
		}
		LockPath, err := lockfile.PathForConfig(path)
		if err != nil {
			LockPath = scoped.source.LockPath(path)
		}
		generation, err := dependencytxn.ReadGeneration(LockPath)
		if err != nil {
			return nil, err
		}
		plan.LockPaths[scoped.source] = LockPath
		reads = append(reads, dependencytxn.ReadAssertion{Path: LockPath, RequestedPath: LockPath, Expected: generation})
		if len(layer.Reads) == 0 && !represented[path] {
			reads = append(reads, dependencytxn.ReadAssertion{Path: path, RequestedPath: layer.Path})
			represented[path] = true
		}
	}
	return reads, dependencytxn.ValidateReadAssertions(reads)
}

func (p *PreparedInstallLocks) BindRegistry(target config.Source, entry config.RegistryEntry) error {
	destination, ok := p.LockPaths[target]
	if !ok || !target.IsWritable() {
		return fmt.Errorf("%w: unprepared install destination", dependencytxn.ErrGenerationConflict)
	}
	canonical, err := CanonicalRegistryURL(entry.URL)
	if err != nil {
		return err
	}
	sourcePath, ok := p.LockPaths[entry.Source]
	if !ok {
		return fmt.Errorf("%w: selected registry source has no prepared lock path", dependencytxn.ErrGenerationConflict)
	}
	for _, registry := range p.Documents[sourcePath].Registries {
		if registry.URL != canonical || registry.Ref != entry.Ref {
			continue
		}
		roots, matches := ProjectLockedRoots(registry.Roots, []config.RegistryEntry{entry})
		if !matches {
			return fmt.Errorf("%w: registry root was not discovered from a prepared source", dependencytxn.ErrGenerationConflict)
		}
		if target == entry.Source {
			return nil
		}
		registry.Roots = roots
		p.Bindings[destination] = append(p.Bindings[destination], registry)
		return nil
	}
	return fmt.Errorf("%w: selected registry has no prepared pin", dependencytxn.ErrGenerationConflict)
}

func reuseInstallDocument(plan *PreparedInstallLocks, path string, scope lockfile.Scope, entries []config.RegistryEntry, current *lockfile.Document) (reconciledDocument, bool) {
	doc, ok := plan.Documents[path]
	if bindings := plan.Bindings[path]; len(bindings) > 0 {
		var err error
		doc, err = MergeInstallBindings(doc, scope, bindings)
		if err != nil {
			return reconciledDocument{err: err}, false
		}
		ok = true
	}
	if !ok || !documentMatchesEntries(doc, entries) {
		return reconciledDocument{err: fmt.Errorf("%w: prepared install requirements changed", dependencytxn.ErrGenerationConflict)}, false
	}
	if current == nil {
		return reconciledDocument{doc: doc}, false
	}
	before, err := lockfile.Encode(*current)
	if err != nil {
		return reconciledDocument{err: err}, false
	}
	after, err := lockfile.Encode(doc)
	return reconciledDocument{doc: doc, err: err}, err == nil && bytes.Equal(before, after)
}

func MergeInstallBindings(base lockfile.Document, scope lockfile.Scope, bindings []lockfile.GitRegistry) (lockfile.Document, error) {
	byIdentity := make(map[string]lockfile.GitRegistry)
	for _, registries := range [][]lockfile.GitRegistry{base.Registries, bindings} {
		for _, registry := range registries {
			identity := gitLockIdentity(registry.URL, registry.Ref)
			merged, exists := byIdentity[identity]
			if !exists {
				registry.Roots = append([]lockfile.GitRoot(nil), registry.Roots...)
				byIdentity[identity] = registry
				continue
			}
			if merged.Commit != registry.Commit {
				return lockfile.Document{}, fmt.Errorf("%w: selected sources require incompatible exact commits in one scope", dependencytxn.ErrGenerationConflict)
			}
			for _, root := range registry.Roots {
				found := false
				for _, prior := range merged.Roots {
					if prior.Path == root.Path && prior.SourceHarness == root.SourceHarness {
						if !reflect.DeepEqual(prior, root) {
							return lockfile.Document{}, fmt.Errorf("%w: selected source manifests disagree", dependencytxn.ErrGenerationConflict)
						}
						found = true
						break
					}
				}
				if !found {
					merged.Roots = append(merged.Roots, root)
				}
			}
			byIdentity[identity] = merged
		}
	}
	doc := gitReconciliationDocument(scope, &base)
	for _, registry := range byIdentity {
		doc.Registries = append(doc.Registries, registry)
	}
	return lockfile.Normalize(doc)
}
