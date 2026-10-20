package main

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

type preparedInstallLocks struct {
	documents       map[string]lockfile.Document
	lockPaths       map[config.Source]string
	bindings        map[string][]lockfile.GitRegistry
	discoveryConfig *config.Config
	reads           []dependencytxn.ReadAssertion
	ctx             context.Context
	release         func()
}

func prepareInstallDependencies(ctx context.Context, root, local string, edits []pipeline.StagedConfigEdit) (pipeline.PreparedInstallDependencies, error) {
	state := &preparedInstallLocks{documents: make(map[string]lockfile.Document), lockPaths: make(map[config.Source]string), bindings: make(map[string][]lockfile.GitRegistry)}
	var warnings []error
	var lockMetadata []pipeline.LockMetadata
	option, _, lease, err := prepareRuntimeLocks(ctx, root, local, lockMode{stagedConfigEdits: edits, prepareInstall: state, warningSink: &warnings, lockMetadataSink: &lockMetadata})
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
		var lockMetadata []pipeline.LockMetadata
		option, _, lease, err := prepareRuntimeLocks(ctx, root, local, lockMode{offline: true, stagedConfigEdits: finalEdits, commitReads: state.reads, reuseInstall: state, warningSink: &warnings, lockMetadataSink: &lockMetadata})
		if err != nil {
			return pipeline.DependencyLockView{}, err
		}
		return pipeline.DependencyLockView{CatalogOption: option, ExactLockLease: lease, LockMetadata: lockMetadata, Warnings: warnings}, nil
	}
	bind := func(target config.Source, entry config.RegistryEntry) error {
		mu.Lock()
		defer mu.Unlock()
		if !active || used {
			return fmt.Errorf("%w: prepared install scope ended", dependencytxn.ErrGenerationConflict)
		}
		return state.bindRegistry(target, entry)
	}
	return pipeline.PreparedInstallDependencies{Context: state.ctx, DiscoveryConfig: state.discoveryConfig, View: pipeline.DependencyLockView{CatalogOption: option, ExactLockLease: lease, LockMetadata: lockMetadata, Warnings: warnings}, Reconcile: reconcile, BindRegistry: bind, Release: release}, nil
}

func installPreparationReads(ec config.EffectiveConfig, staged map[string]pipeline.StagedConfigEdit, plan *preparedInstallLocks) ([]dependencytxn.ReadAssertion, error) {
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
		lockPath, err := lockfile.PathForConfig(path)
		if err != nil {
			lockPath = scoped.source.LockPath(path)
		}
		generation, err := dependencytxn.ReadGeneration(lockPath)
		if err != nil {
			return nil, err
		}
		plan.lockPaths[scoped.source] = lockPath
		reads = append(reads, dependencytxn.ReadAssertion{Path: lockPath, RequestedPath: lockPath, Expected: generation})
		if len(layer.Reads) == 0 && !represented[path] {
			reads = append(reads, dependencytxn.ReadAssertion{Path: path, RequestedPath: layer.Path})
			represented[path] = true
		}
	}
	return reads, dependencytxn.ValidateReadAssertions(reads)
}

func (p *preparedInstallLocks) bindRegistry(target config.Source, entry config.RegistryEntry) error {
	destination, ok := p.lockPaths[target]
	if !ok || !target.IsWritable() {
		return fmt.Errorf("%w: unprepared install destination", dependencytxn.ErrGenerationConflict)
	}
	canonical, err := canonicalRegistryURL(entry.URL)
	if err != nil {
		return err
	}
	sourcePath, ok := p.lockPaths[entry.Source]
	if !ok {
		return fmt.Errorf("%w: selected registry source has no prepared lock path", dependencytxn.ErrGenerationConflict)
	}
	for _, registry := range p.documents[sourcePath].Registries {
		if registry.URL != canonical || registry.Ref != entry.Ref {
			continue
		}
		roots, matches := projectLockedRoots(registry.Roots, []config.RegistryEntry{entry})
		if !matches {
			return fmt.Errorf("%w: registry root was not discovered from a prepared source", dependencytxn.ErrGenerationConflict)
		}
		if target == entry.Source {
			return nil
		}
		registry.Roots = roots
		p.bindings[destination] = append(p.bindings[destination], registry)
		return nil
	}
	return fmt.Errorf("%w: selected registry has no prepared pin", dependencytxn.ErrGenerationConflict)
}

func reuseInstallDocument(ctx context.Context, plan *preparedInstallLocks, path string, scope lockfile.Scope, entries []config.RegistryEntry, decls []config.SourceDecl, current *lockfile.Document, planner *pluginPlanSource, layer config.Source) (reconciledDocument, bool) {
	doc, ok := plan.documents[path]
	if bindings := plan.bindings[path]; len(bindings) > 0 {
		var err error
		doc, err = mergeInstallBindings(doc, scope, bindings)
		if err != nil {
			return reconciledDocument{err: err}, false
		}
		ok = true
	}
	if !ok || !documentMatchesEntries(doc, entries, expectedExternalIdentities(ctx, planner, decls, doc, layer)) {
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

func mergeInstallBindings(base lockfile.Document, scope lockfile.Scope, bindings []lockfile.GitRegistry) (lockfile.Document, error) {
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
