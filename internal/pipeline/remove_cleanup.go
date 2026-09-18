package pipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type ownedRemovalPreparation struct {
	ctx     context.Context
	release func()
	target  resolver.ResolvedArtifact
	reads   []dependencytxn.ReadAssertion
	option  catalog.LoadOption
}

func prepareBlockedOwnedMCPRemoval(ctx context.Context, opts RemoveOptions, ec config.EffectiveConfig, original resolver.ResolvedArtifact, name, url, path, ref string, kind ir.ResourceKind) (*ownedRemovalPreparation, error) {
	if opts.configStaging == nil || opts.CleanupDependencies == nil || kind != ir.KindMcpServer {
		return nil, fmt.Errorf("%w: cleanup transaction is unavailable", materialize.ErrOwnedCleanupUnsupported)
	}
	if ec.Merged == nil {
		return nil, errors.New("blocked MCP cleanup has no effective configuration")
	}
	for _, entry := range ec.Merged.Registries {
		if entry.IsLocal() {
			return nil, fmt.Errorf("%w: live local registries are not supported", materialize.ErrOwnedCleanupUnsupported)
		}
	}
	reads, err := cleanupReadAssertions(ec)
	if err != nil {
		return nil, err
	}
	if err := dependencytxn.ValidateReadAssertions(reads); err != nil {
		return nil, err
	}
	ctx, view := exactLockViewWithContext(ctx, ec, false)
	if len(view.errors) > 0 {
		view.release()
		return nil, errors.Join(view.errors...)
	}
	for _, entry := range ec.Merged.Registries {
		snapshot, _, err := view.exactSnapshot(entry)
		if err != nil || snapshot == "" {
			view.release()
			if err == nil {
				err = errors.New("exact snapshot is unavailable")
			}
			return nil, fmt.Errorf("blocked MCP cleanup requires exact registry input: %w", err)
		}
	}
	result := Resolve(ctx, ResolveOptions{RepoRoot: opts.RepoRoot, CatalogMode: catalog.LoadModeInspect, Offline: true, IgnoreBlockedRegistries: true, SkipInventory: true, CatalogOption: []catalog.LoadOption{view.catalogOption}})
	if len(result.Errors) > 0 {
		view.release()
		return nil, errors.Join(result.Errors...)
	}
	if err := dependencytxn.ValidateReadAssertions(reads); err != nil {
		view.release()
		return nil, err
	}
	var selected *resolver.ResolvedArtifact
	if ref != "" {
		selected, err = resolvedResourceInRegistryAtRef(result.Resolved, name, kind, true, url, path, ref)
	} else {
		selected, err = resolvedResourceInRegistry(result.Resolved, name, kind, true, url, path)
	}
	if err != nil || selected == nil {
		view.release()
		if err == nil {
			err = errors.New("selected MCP target is unresolved")
		}
		return nil, err
	}
	if !sameCleanupTarget(original, *selected) {
		view.release()
		return nil, fmt.Errorf("%w: blocked MCP cleanup target changed", dependencytxn.ErrGenerationConflict)
	}
	machine := opts.Scope == ScopeUser
	if selected.Source.IsMachineScope() != machine {
		view.release()
		return nil, fmt.Errorf("%w: blocked MCP cleanup scope does not match selected source", materialize.ErrOwnedCleanupUnsupported)
	}
	return &ownedRemovalPreparation{ctx: ctx, release: view.release, target: *selected, reads: reads, option: view.catalogOption}, nil
}

func (p *ownedRemovalPreparation) commit(opts RemoveOptions) error {
	overlays, err := opts.configStaging.Overlays()
	if err != nil {
		return err
	}
	after := Resolve(p.ctx, ResolveOptions{RepoRoot: opts.RepoRoot, CatalogMode: catalog.LoadModeInspect, Offline: true, IgnoreBlockedRegistries: true, SkipInventory: true, CatalogOption: []catalog.LoadOption{p.option}, EffectiveOpts: config.LoadEffectiveOptions{RepoRoot: opts.RepoRoot, FileOverlays: overlays}})
	if len(after.Errors) > 0 {
		return errors.Join(after.Errors...)
	}
	if err := dependencytxn.ValidateReadAssertions(p.reads); err != nil {
		return err
	}
	plan, err := materialize.PrepareOwnedMCPRemoval(p.ctx, opts.RepoRoot, p.target, resolver.MaterializationWinners(after.Resolved))
	if err != nil {
		return err
	}
	edits, err := opts.configStaging.Edits()
	if err != nil {
		return err
	}
	commitErr := opts.CleanupDependencies(p.ctx, opts.RepoRoot, edits, p.reads, plan.CommitScope)
	for _, diagnostic := range plan.Diagnostics() {
		issue := newDiagIssue(diagnostic)
		if opts.warningSink != nil {
			*opts.warningSink = append(*opts.warningSink, issue)
		}
		if commitErr != nil {
			commitErr = errors.Join(commitErr, issue)
		}
	}
	return commitErr
}

func cleanupReadAssertions(ec config.EffectiveConfig) ([]dependencytxn.ReadAssertion, error) {
	layers := []config.LayerResult{ec.Organization, ec.User, ec.Repo, ec.LocalLayer}
	byRequested := map[string]dependencytxn.ReadAssertion{}
	represented := map[string]bool{}
	for _, layer := range layers {
		for _, read := range layer.Reads {
			assertion := dependencytxn.ReadAssertion{Path: read.Path, RequestedPath: read.RequestedPath, Expected: dependencytxn.Generation{Exists: true, Digest: read.Digest}}
			if prior, ok := byRequested[assertion.RequestedPath]; ok && prior != assertion {
				return nil, fmt.Errorf("%w: inconsistent config read", dependencytxn.ErrGenerationConflict)
			}
			byRequested[assertion.RequestedPath] = assertion
			represented[assertion.Path] = true
		}
	}
	for _, layer := range layers {
		if len(layer.Reads) != 0 || layer.Path == "" {
			continue
		}
		canonical, err := dependencytxn.CanonicalConfigDestination(layer.Path)
		if err != nil {
			return nil, err
		}
		if represented[canonical] {
			continue
		}
		if _, exists := byRequested[layer.Path]; !exists {
			byRequested[layer.Path] = dependencytxn.ReadAssertion{Path: canonical, RequestedPath: layer.Path}
			represented[canonical] = true
		}
	}
	for _, layer := range layers {
		if len(readOnlyRemoteEntries(layer)) == 0 || layer.Path == "" {
			continue
		}
		lockPath, err := lockfile.PathForConfig(layer.Path)
		if err != nil {
			name := "lock.yaml"
			if layer.Source == config.RepoPersonal {
				name = "lock.local.yaml"
			}
			lockPath = filepath.Join(filepath.Dir(layer.Path), name)
		}
		canonical, err := dependencytxn.CanonicalConfigDestination(lockPath)
		if err != nil {
			return nil, err
		}
		generation, err := dependencytxn.ReadGeneration(canonical)
		if err != nil {
			return nil, err
		}
		byRequested[lockPath] = dependencytxn.ReadAssertion{Path: canonical, RequestedPath: lockPath, Expected: generation}
	}
	reads := make([]dependencytxn.ReadAssertion, 0, len(byRequested))
	for _, read := range byRequested {
		reads = append(reads, read)
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i].RequestedPath < reads[j].RequestedPath })
	return reads, nil
}

func sameCleanupTarget(a, b resolver.ResolvedArtifact) bool {
	if a.Kind != b.Kind || a.Name != b.Name || config.NormalizeGitURL(a.RegistryURL) != config.NormalizeGitURL(b.RegistryURL) || config.NormalizeRegistryPath(a.RegistryPath) != config.NormalizeRegistryPath(b.RegistryPath) || a.Ref != b.Ref || a.Source != b.Source || a.Required != b.Required || len(a.Harnesses) != len(b.Harnesses) {
		return false
	}
	set := make(map[string]int, len(a.Harnesses))
	for _, target := range a.Harnesses {
		set[string(target)]++
	}
	for _, target := range b.Harnesses {
		set[string(target)]--
	}
	for _, count := range set {
		if count != 0 {
			return false
		}
	}
	return true
}
