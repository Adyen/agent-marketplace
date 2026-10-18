package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
)

// finalizePluginRoots completes the deferred format roots of one layer after
// every git acquisition: it discovers the layer's collections, builds each
// plugin-v1 manifest, and acquires every selected external catalog entry as
// its own registry record.
func finalizePluginRoots(ctx context.Context, scan []lockfile.GitRegistry, pending []pendingFormatRoot, decls []config.SourceDecl, entries []config.RegistryEntry, old map[string]lockfile.GitRegistry, upgrade bool, allowed *[]string) ([]lockfile.GitRegistry, error) {
	selected := map[string]bool{}
	for _, decl := range decls {
		for _, name := range decl.CollectionsInclude {
			selected[name] = true
		}
	}
	collections, err := discoverLockCollections(ctx, scan, decls, entries)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []lockfile.GitRegistry
	for i := range pending {
		item := pending[i]
		spec := catalog.PluginSelectionSpecFor(item.entry, collections, selected)
		built, err := sourcerevision.Build(sourcerevision.Input{SnapshotRoot: item.snapshot, RootPath: item.rootPath, SourceHarness: item.entry.Format, RevisionSchema: "plugin-v1", PluginSpec: &spec})
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
			}
			registry, err := acquireExternalPluginRegistry(ctx, ext, binding, plugindecode.CatalogPluginFormat(item.entry.Format), pinnedExternal(old, ext, upgrade), allowed)
			if err != nil {
				return nil, err
			}
			key := registry.URL + "\x00" + registry.Roots[0].Path
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
		spec := catalog.PluginSelectionSpecFor(pipeline.FormatSourceEntries([]config.SourceDecl{decl})[0], collections, selected)
		loaded, err := catalog.DiscoverPluginSource(decl.URL, decl.Format)
		if err != nil {
			return nil, fmt.Errorf("decode plugin catalog %q: %w", decl.URL, err)
		}
		for _, ext := range pipeline.ExternalEntriesFor(&spec, loaded) {
			binding := &sourcerevision.PluginExternal{
				CatalogKind:   "directory",
				CatalogURL:    decl.URL,
				CatalogRoot:   ".",
				EntryName:     ext.Name,
				CatalogFormat: decl.Format,
				EntryStrict:   ext.EntryStrict,
				EntryOverlay:  ext.EntryOverlay,
			}
			registry, err := acquireExternalPluginRegistry(ctx, ext, binding, plugindecode.CatalogPluginFormat(decl.Format), pinnedExternal(old, ext, upgrade), allowed)
			if err != nil {
				return nil, err
			}
			key := registry.URL + "\x00" + registry.Roots[0].Path
			if !seen[key] {
				seen[key] = true
				out = append(out, registry)
			}
		}
	}
	return out, nil
}

// discoverLockCollections discovers the collections of every registry in the
// layer's lock and every directory source: they can name plugin members of a
// format source, which the plugin-v1 manifest commits as collection edges.
func discoverLockCollections(ctx context.Context, scan []lockfile.GitRegistry, decls []config.SourceDecl, entries []config.RegistryEntry) ([]collection.Collection, error) {
	fetchURLs := map[string]string{}
	for _, entry := range entries {
		canonical, err := canonicalRegistryURL(entry.URL)
		if err != nil {
			return nil, err
		}
		fetchURLs[gitLockIdentity(canonical, entry.Ref)] = entry.URL
	}
	var out []collection.Collection
	for _, registry := range scan {
		allPlugin := len(registry.Roots) > 0
		for _, root := range registry.Roots {
			if root.RevisionSchema != "plugin-v1" {
				allPlugin = false
			}
		}
		if allPlugin {
			continue
		}
		oid, err := cache.ParseGitOID(registry.Commit.String())
		if err != nil {
			return nil, err
		}
		var snapshot string
		if err := cache.WithAcquiredExactGit(ctx, registry.URL, fetchURLs[gitLockIdentity(registry.URL, registry.Ref)], oid, func(path string) error {
			snapshot = path
			return nil
		}); err != nil {
			return nil, err
		}
		for _, root := range registry.Roots {
			found, err := collection.DiscoverCollections(filepath.Join(snapshot, filepath.FromSlash(root.Path)), "", registry.URL, registry.Ref, root.Path)
			if err != nil {
				return nil, fmt.Errorf("discover collections in %s@%s root %q: %w", registry.URL, registry.Ref, root.Path, err)
			}
			out = append(out, found...)
		}
	}
	for _, decl := range decls {
		if decl.Kind != config.V4SourceDirectory {
			continue
		}
		found, err := collection.DiscoverCollections(decl.URL, "", decl.URL, "", "")
		if err != nil {
			return nil, fmt.Errorf("discover collections in %s: %w", decl.URL, err)
		}
		out = append(out, found...)
	}
	return out, nil
}

// directorySelectsExternalPlugin reports whether a remote-free layer still
// needs a lock: a directory format source whose declared selection names at
// least one external catalog entry produces a registry record; anything less
// writes nothing.
func directorySelectsExternalPlugin(decls []config.SourceDecl) bool {
	selected := map[string]bool{}
	var collections []collection.Collection
	for _, decl := range decls {
		for _, name := range decl.CollectionsInclude {
			selected[name] = true
		}
		if decl.Kind == config.V4SourceDirectory {
			found, err := collection.DiscoverCollections(decl.URL, "", decl.URL, "", "")
			if err == nil {
				collections = append(collections, found...)
			}
		}
	}
	for _, decl := range decls {
		if decl.Format == "" || decl.Kind != config.V4SourceDirectory {
			continue
		}
		spec := catalog.PluginSelectionSpecFor(pipeline.FormatSourceEntries([]config.SourceDecl{decl})[0], collections, selected)
		loaded, err := catalog.DiscoverPluginSource(decl.URL, decl.Format)
		if err != nil {
			return true
		}
		if len(pipeline.ExternalEntriesFor(&spec, loaded)) > 0 {
			return true
		}
	}
	return false
}

// pinnedExternal returns the existing locked record for an external entry
// origin: locked and check modes rebuild it from the recorded commit rather
// than tracking the entry ref.
func pinnedExternal(old map[string]lockfile.GitRegistry, ext sourcerevision.PluginExternalEntry, upgrade bool) *lockfile.GitRegistry {
	if upgrade {
		return nil
	}
	canonical, err := canonicalRegistryURL(ext.Source.URL)
	if err != nil {
		return nil
	}
	rootPath := ext.Source.Subdirectory
	if rootPath == "" {
		rootPath = "."
	}
	for _, registry := range old {
		if registry.URL != canonical || len(registry.Roots) != 1 || registry.Roots[0].Path != rootPath {
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
func acquireExternalPluginRegistry(ctx context.Context, ext sourcerevision.PluginExternalEntry, binding *sourcerevision.PluginExternal, packageFormat string, pinned *lockfile.GitRegistry, allowed *[]string) (lockfile.GitRegistry, error) {
	if allowed != nil {
		if err := config.ValidateRegistryURLs([]config.RegistryEntry{{URL: ext.Source.URL}}, *allowed); err != nil {
			return lockfile.GitRegistry{}, fmt.Errorf("plugin catalog entry %q lists origin %s: %w", binding.EntryName, ext.Source.URL, err)
		}
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
