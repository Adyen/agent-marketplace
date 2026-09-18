package main

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

type preparedInstallLocks struct {
	documents       map[string]lockfile.Document
	discoveryConfig *config.Config
	reads           []dependencytxn.ReadAssertion
	ctx             context.Context
	release         func()
}

func prepareInstallDependencies(ctx context.Context, root, local string, edits []pipeline.StagedConfigEdit) (pipeline.PreparedInstallDependencies, error) {
	state := &preparedInstallLocks{documents: make(map[string]lockfile.Document)}
	var warnings []error
	option, _, lease, err := prepareRuntimeLocks(ctx, root, local, lockMode{stagedConfigEdits: edits, prepareInstall: state, warningSink: &warnings})
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
		option, _, lease, err := prepareRuntimeLocks(ctx, root, local, lockMode{offline: true, stagedConfigEdits: finalEdits, commitReads: state.reads, reuseInstall: state, warningSink: &warnings})
		if err != nil {
			return pipeline.DependencyLockView{}, err
		}
		return pipeline.DependencyLockView{CatalogOption: option, ExactLockLease: lease, Warnings: warnings}, nil
	}
	return pipeline.PreparedInstallDependencies{Context: state.ctx, DiscoveryConfig: state.discoveryConfig, View: pipeline.DependencyLockView{CatalogOption: option, ExactLockLease: lease, Warnings: warnings}, Reconcile: reconcile, Release: release}, nil
}

func installPreparationReads(ec config.EffectiveConfig, staged map[string]pipeline.StagedConfigEdit) ([]dependencytxn.ReadAssertion, error) {
	reads, err := effectiveReadAssertions(ec, staged)
	if err != nil {
		return nil, err
	}
	represented := make(map[string]bool, len(reads))
	for _, read := range reads {
		represented[read.Path] = true
	}
	for _, layer := range []config.LayerResult{ec.Organization, ec.User, ec.Repo, ec.LocalLayer} {
		if layer.Path == "" || len(layer.Reads) != 0 {
			continue
		}
		path, err := canonicalConfigDependencyPath(layer.Path)
		if err != nil {
			return nil, err
		}
		if !represented[path] {
			reads = append(reads, dependencytxn.ReadAssertion{Path: path, RequestedPath: layer.Path})
			represented[path] = true
		}
	}
	return reads, dependencytxn.ValidateReadAssertions(reads)
}

func reuseInstallDocument(plan *preparedInstallLocks, path string, entries []config.RegistryEntry, current *lockfile.Document) (reconciledDocument, bool) {
	doc, ok := plan.documents[path]
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
