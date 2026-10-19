package main

import (
	"context"
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
)

// pluginPlanSource computes the resolver's plugin selection for the merged
// effective configuration once, on demand: catalogs decode from the exact
// snapshots this run acquired or the deployed locks recorded, plus live
// directory sources, through the same catalog loader the pipeline resolution
// uses. The plugin-v1 manifests therefore commit exactly the selection the
// resolver performs.
// acquiredSnap records one freshly acquired registry snapshot the plan's
// catalog load may consume.
type acquiredSnap struct {
	path   string
	commit string
}

type pluginPlanSource struct {
	merged    *config.Config
	localCfg  *config.LocalConfig
	repoRoot  string
	localDir  string
	offline   bool
	readOnly  bool
	snapshots map[string]acquiredSnap // gitLockIdentity → acquired snapshot
	fetchURLs map[string]string       // gitLockIdentity → authored fetch URL
	docs      func() []lockfile.Document
	built     bool
	plan      catalog.PluginSelectionPlan
	errs      []error
	err       error
}

func (s *pluginPlanSource) get(ctx context.Context) (catalog.PluginSelectionPlan, []error, error) {
	if s.built {
		return s.plan, s.errs, s.err
	}
	s.built = true
	openDoc := func(registry lockfile.GitRegistry) (string, func(), error) {
		if snap, ok := s.snapshots[gitLockIdentity(registry.URL, registry.Ref)]; ok {
			return snap.path, nil, nil
		}
		oid, err := cache.ParseGitOID(registry.Commit.String())
		if err != nil {
			return "", nil, err
		}
		if s.offline || s.readOnly {
			path, release, err := cache.LeaseExactGitReadOnlyWithContext(ctx, registry.URL, oid)
			return path, release, err
		}
		path, err := cache.AcquireExactGitWithContext(ctx, registry.URL, fetchURLFor(registry, s.fetchURLs), oid)
		return path, nil, err
	}
	exactDocs, release := pipeline.DocExactSnapshots(s.docs(), openDoc)
	defer release()
	// Acquired registries are not yet present in any lock document, so their
	// snapshots bind ahead of document records.
	exact := func(entry config.RegistryEntry) (string, string, error) {
		canonical, err := lockfile.CanonicalGitConfigURL(entry.URL)
		if err != nil {
			return "", "", err
		}
		if snap, ok := s.snapshots[gitLockIdentity(canonical, entry.Ref)]; ok {
			return snap.path, canonical + "@" + snap.commit + ":" + entry.Path, nil
		}
		return exactDocs(entry)
	}
	s.plan, s.errs, s.err = pipeline.BuildPluginSelectionPlan(ctx, s.merged, s.localCfg, s.repoRoot, s.localDir, exact)
	return s.plan, s.errs, s.err
}

// finalizePluginRoots completes the deferred format roots of one layer after
// every git acquisition: each plugin-v1 manifest renders the resolver's
// planned selection for that root, and every planned external catalog entry
// is acquired as its own registry record.
func finalizePluginRoots(ctx context.Context, pending []pendingFormatRoot, decls []config.SourceDecl, plan catalog.PluginSelectionPlan, old map[string]lockfile.GitRegistry, upgrade, readOnly bool, allowed *[]string) ([]lockfile.GitRegistry, error) {
	seen := map[string]bool{}
	var out []lockfile.GitRegistry
	for i := range pending {
		item := pending[i]
		built, err := sourcerevision.Build(sourcerevision.Input{SnapshotRoot: item.snapshot, RootPath: item.rootPath, SourceHarness: item.entry.Format, RevisionSchema: "plugin-v1", PluginSelection: plan.ForEntry(item.entry)})
		if err != nil {
			return nil, fmt.Errorf("build plugin source revision for %s/%s: %w", item.group.canonical, item.rootPath, err)
		}
		item.registry.Roots = append(item.registry.Roots, lockfile.GitRoot{Path: item.rootPath, SourceHarness: item.entry.Format, RevisionSchema: "plugin-v1", RevisionDigest: lockfile.Digest(built.Manifest.RevisionDigest), Resources: revisionResources(built)})
		for _, ext := range built.PluginExternals {
			binding := &sourcerevision.PluginExternal{
				CatalogKind:   "git",
				CatalogURL:    item.group.canonical,
				CatalogRef:    item.group.ref,
				CatalogCommit: item.oid.String(),
				CatalogRoot:   item.rootPath,
				EntryName:     ext.Name,
				CatalogFormat: item.entry.Format,
				EntryStrict:   ext.EntryStrict,
				EntryOverlay:  ext.EntryOverlay,
				Exclusions:    ext.Exclusions,
			}
			registry, err := acquireExternalPluginRegistry(ctx, ext, binding, plugindecode.CatalogPluginFormat(item.entry.Format), pinnedExternal(old, ext, upgrade, readOnly), readOnly, allowed)
			if err != nil {
				return nil, err
			}
			key := registry.URL + "\x00" + registry.Ref + "\x00" + registry.Roots[0].Path
			if !seen[key] {
				seen[key] = true
				out = append(out, registry)
			}
		}
	}
	for _, decl := range decls {
		if decl.Format == "" || decl.Kind != config.V4SourceDirectory {
			continue
		}
		entry := pipeline.FormatSourceEntries([]config.SourceDecl{decl})[0]
		for _, plugin := range plan.ForEntry(entry) {
			ext, ok := pipeline.PlannedExternalEntry(plugin)
			if !ok {
				continue
			}
			binding := &sourcerevision.PluginExternal{
				CatalogKind:   "directory",
				CatalogURL:    decl.URL,
				CatalogRoot:   ".",
				EntryName:     ext.Name,
				CatalogFormat: decl.Format,
				EntryStrict:   ext.EntryStrict,
				EntryOverlay:  ext.EntryOverlay,
				Exclusions:    ext.Exclusions,
			}
			registry, err := acquireExternalPluginRegistry(ctx, ext, binding, plugindecode.CatalogPluginFormat(decl.Format), pinnedExternal(old, ext, upgrade, readOnly), readOnly, allowed)
			if err != nil {
				return nil, err
			}
			key := registry.URL + "\x00" + registry.Ref + "\x00" + registry.Roots[0].Path
			if !seen[key] {
				seen[key] = true
				out = append(out, registry)
			}
		}
	}
	return out, nil
}

// directorySelectsExternalPlugin reports whether a remote-free layer still
// needs a lock: a directory format source whose planned selection names at
// least one external catalog entry produces a registry record; anything less
// writes nothing.
func directorySelectsExternalPlugin(ctx context.Context, planner *pluginPlanSource, decls []config.SourceDecl) bool {
	format := false
	for _, decl := range decls {
		if decl.Format != "" && decl.Kind == config.V4SourceDirectory {
			format = true
			break
		}
	}
	if !format {
		return false
	}
	plan, errs, err := planner.get(ctx)
	if err != nil || len(pipeline.PluginPlanErrors(errs)) > 0 {
		return true
	}
	for _, decl := range decls {
		if decl.Format == "" || decl.Kind != config.V4SourceDirectory {
			continue
		}
		entry := pipeline.FormatSourceEntries([]config.SourceDecl{decl})[0]
		for _, plugin := range plan.ForEntry(entry) {
			if plugin.Entry.Plugin != nil && plugin.Entry.Plugin.Source.Kind == "git" {
				return true
			}
		}
	}
	return false
}

// pinnedExternal returns the existing locked record for an external entry
// origin: locked and check modes rebuild it from the recorded commit rather
// than tracking the entry ref. A writable sync pins the record only while it
// still names the ref the current entry requests, so an edited ref resolves
// afresh. Read-only modes always rebuild from the recorded commit, which
// still reports the edited entry stale, and never resolve a ref the lock
// does not record.
func pinnedExternal(old map[string]lockfile.GitRegistry, ext sourcerevision.PluginExternalEntry, upgrade, readOnly bool) *lockfile.GitRegistry {
	if upgrade {
		return nil
	}
	canonical, err := canonicalRegistryURL(ext.Source.URL)
	if err != nil {
		return nil
	}
	ref := ext.Source.Ref
	if ref == "" {
		ref = "HEAD"
	}
	rootPath := ext.Source.Subdirectory
	if rootPath == "" {
		rootPath = "."
	}
	for _, registry := range old {
		if registry.URL != canonical || len(registry.Roots) != 1 || registry.Roots[0].Path != rootPath {
			continue
		}
		if !readOnly && registry.Ref != ref {
			continue
		}
		locked := registry
		return &locked
	}
	return nil
}

// acquireExternalPluginRegistry checks the entry origin against the
// allowlist, acquires it through the exact Git path, and produces its own
// registry record: the plugin package's root becomes a plugin-v1 root whose
// manifest names the owning catalog entry and overlay digest.
func acquireExternalPluginRegistry(ctx context.Context, ext sourcerevision.PluginExternalEntry, binding *sourcerevision.PluginExternal, packageFormat string, pinned *lockfile.GitRegistry, readOnly bool, allowed *[]string) (lockfile.GitRegistry, error) {
	if allowed != nil {
		if err := config.ValidateRegistryURLs([]config.RegistryEntry{{URL: ext.Source.URL}}, *allowed); err != nil {
			return lockfile.GitRegistry{}, fmt.Errorf("plugin catalog entry %q lists origin %s: %w", binding.EntryName, ext.Source.URL, err)
		}
	}
	if readOnly && pinned == nil {
		return lockfile.GitRegistry{}, &lockLifecycleError{Code: "lock.stale", Err: fmt.Errorf("plugin catalog entry %q has no locked external record", binding.EntryName)}
	}
	canonical, err := canonicalRegistryURL(ext.Source.URL)
	if err != nil {
		return lockfile.GitRegistry{}, fmt.Errorf("plugin catalog entry %q origin %s: %w", binding.EntryName, ext.Source.URL, err)
	}
	var oid cache.GitOID
	ref := ext.Source.Ref
	if pinned != nil {
		parsed, err := cache.ParseGitOID(pinned.Commit.String())
		if err != nil {
			return lockfile.GitRegistry{}, err
		}
		oid = parsed
		ref = pinned.Ref
	} else {
		if ref == "" {
			ref = "HEAD"
		}
		resolved, err := cache.ResolveExactGitRefWithContext(ctx, canonical, ext.Source.URL, ref)
		if err != nil {
			return lockfile.GitRegistry{}, fmt.Errorf("resolve exact Git ref %s@%s for plugin catalog entry %q: %w", canonical, ref, binding.EntryName, err)
		}
		oid = resolved
	}
	snapshot, err := cache.AcquireExactGitWithContext(ctx, canonical, ext.Source.URL, oid)
	if err != nil {
		return lockfile.GitRegistry{}, fmt.Errorf("acquire exact Git snapshot %s for plugin catalog entry %q: %w", oid.String(), binding.EntryName, err)
	}
	rootPath := ext.Source.Subdirectory
	if rootPath == "" {
		rootPath = "."
	}
	binding.EntryDigest = ext.DefinitionDigest
	built, err := sourcerevision.Build(sourcerevision.Input{SnapshotRoot: snapshot, RootPath: rootPath, SourceHarness: packageFormat, RevisionSchema: "plugin-v1", PluginExternal: binding})
	if err != nil {
		return lockfile.GitRegistry{}, fmt.Errorf("build plugin source revision for %s/%s: %w", canonical, rootPath, err)
	}
	commit, err := lockfile.ParseGitOID(oid.String())
	if err != nil {
		return lockfile.GitRegistry{}, err
	}
	root := lockfile.GitRoot{Path: rootPath, SourceHarness: built.SourceHarness, RevisionSchema: "plugin-v1", RevisionDigest: lockfile.Digest(built.Manifest.RevisionDigest), Resources: revisionResources(built)}
	return lockfile.GitRegistry{URL: canonical, Ref: ref, Commit: commit, Roots: []lockfile.GitRoot{root}}, nil
}
