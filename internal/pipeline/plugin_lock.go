package pipeline

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
)

// PluginLockContexts carries the per-root plugin-v1 verification inputs of
// one layer: the declared selection spec for each format root, the
// owning-catalog binding for each external-entry record, and the set of
// registry identities the current configuration requires as external
// records.
type PluginLockContexts struct {
	Specs     map[string]*catalog.PluginSelectionSpec
	Externals map[string]*sourcerevision.PluginExternal
	Expected  map[string]bool
}

// leaseSnapshotReadOnly opens a registry record's committed snapshot for
// read-only use during plugin-lock context construction.
func leaseSnapshotReadOnly(ctx context.Context) func(lockfile.GitRegistry, func(string) error) error {
	return func(registry lockfile.GitRegistry, consume func(string) error) error {
		oid, err := cache.ParseGitOID(registry.Commit.String())
		if err != nil {
			return err
		}
		path, release, err := cache.LeaseExactGitReadOnlyWithContext(ctx, registry.URL, oid)
		if err != nil {
			return err
		}
		defer release()
		return consume(path)
	}
}

// ExternalIdentities returns the registry identities (canonical URL + ref)
// the configuration's selected catalog entries require as external records;
// nil when the layer has no format sources.
func (c *PluginLockContexts) ExternalIdentities() map[string]bool {
	if c == nil {
		return nil
	}
	return c.Expected
}

// AllRootsPluginV1 reports whether every root of a registry record is a
// plugin-v1 root.
func AllRootsPluginV1(registry lockfile.GitRegistry) bool {
	if len(registry.Roots) == 0 {
		return false
	}
	for _, root := range registry.Roots {
		if root.RevisionSchema != "plugin-v1" {
			return false
		}
	}
	return true
}

// registryRootKey identifies a locked root within a registry record.
func registryRootKey(canonical, ref, rootPath string) string {
	return canonical + "\x00" + ref + "\x00" + rootPath
}

// externalRecordKey identifies an external-entry registry record by its
// locked repository and package root.
func externalRecordKey(canonical, rootPath string) string {
	return canonical + "\x00" + rootPath
}

// PluginContextFor returns the verification context for one locked root: the
// selection spec for a catalog/package format root, or the owning-catalog
// binding for an external-entry root.
func (c *PluginLockContexts) PluginContextFor(registry lockfile.GitRegistry, root lockfile.GitRoot) *sourcerevision.PluginVerifyContext {
	if c == nil || root.RevisionSchema != "plugin-v1" {
		return nil
	}
	if spec := c.Specs[registryRootKey(registry.URL, registry.Ref, root.Path)]; spec != nil {
		return &sourcerevision.PluginVerifyContext{Spec: spec}
	}
	if external := c.Externals[externalRecordKey(registry.URL, root.Path)]; external != nil {
		return &sourcerevision.PluginVerifyContext{External: external}
	}
	return nil
}

// BuildPluginLockContexts computes the layer's plugin-v1 verification
// inputs: it decodes each format source's catalog (from its locked snapshot
// for git sources, live for directory sources), applies the declared
// selections, and binds every selected external entry to its owning catalog.
// openSnapshot opens a readable snapshot of a registry record for the
// duration of consume.
func BuildPluginLockContexts(decls []config.SourceDecl, doc lockfile.Document, openSnapshot func(registry lockfile.GitRegistry, consume func(string) error) error) (*PluginLockContexts, error) {
	format := FormatSourceEntries(decls)
	if len(format) == 0 {
		return nil, nil
	}
	out := &PluginLockContexts{
		Specs:     map[string]*catalog.PluginSelectionSpec{},
		Externals: map[string]*sourcerevision.PluginExternal{},
		Expected:  map[string]bool{},
	}
	selectedCollections := map[string]bool{}
	for _, decl := range decls {
		for _, name := range decl.CollectionsInclude {
			selectedCollections[name] = true
		}
	}
	// Collections are discovered across the layer's locked snapshots and its
	// live directory sources, so a collection in any of the layer's sources
	// contributes its plugin edges identically at write and verify time.
	var collections []collection.Collection
	for _, registry := range doc.Registries {
		external := len(registry.Roots) > 0
		for _, root := range registry.Roots {
			if root.RevisionSchema != "plugin-v1" {
				external = false
			}
		}
		if external {
			continue
		}
		reg := registry
		err := openSnapshot(reg, func(snapshot string) error {
			for _, root := range reg.Roots {
				if root.RevisionSchema == "plugin-v1" {
					continue
				}
				rootDir := filepath.Join(snapshot, filepath.FromSlash(root.Path))
				found, err := collection.DiscoverCollections(rootDir, "", reg.URL, reg.Ref, root.Path)
				if err != nil {
					return fmt.Errorf("discover plugin-lock collections in %s: %w", reg.URL, err)
				}
				collections = append(collections, found...)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, entry := range format {
		if entry.IsLocal() {
			found, err := collection.DiscoverCollections(entry.URL, "", entry.URL, "", "")
			if err != nil {
				return nil, fmt.Errorf("discover plugin-lock collections in %s: %w", entry.URL, err)
			}
			collections = append(collections, found...)
		}
	}
	for i := range format {
		entry := format[i]
		info, known, err := pluginpolicy.LookupPackageFormat(entry.Format)
		if err != nil {
			return nil, err
		}
		if !known {
			return nil, fmt.Errorf("plugin format %q is not a recognized package or catalog format", entry.Format)
		}
		spec := catalog.PluginSelectionSpecFor(entry, collections, selectedCollections)
		if entry.IsLocal() {
			loaded, err := catalog.DiscoverPluginSource(entry.URL, entry.Format)
			if err != nil {
				return nil, fmt.Errorf("decode plugin catalog %q: %w", entry.URL, err)
			}
			bindExternalEntries(out, &spec, loaded, entry, "", "")
			continue
		}
		canonical, err := lockfile.CanonicalGitConfigURL(entry.URL)
		if err != nil {
			return nil, err
		}
		rootPath := entry.Path
		if rootPath == "" {
			rootPath = "."
		}
		specKey := registryRootKey(canonical, entry.Ref, rootPath)
		out.Specs[specKey] = &spec
		if info.Role != pluginpolicy.RoleCatalog {
			continue
		}
		registry, ok := findPluginCatalogRegistry(doc, canonical, entry.Ref, rootPath)
		if !ok {
			continue
		}
		err = openSnapshot(registry, func(snapshot string) error {
			loaded, err := catalog.DiscoverPluginSource(filepath.Join(snapshot, filepath.FromSlash(rootPath)), entry.Format)
			if err != nil {
				return fmt.Errorf("decode plugin catalog %s@%s: %w", canonical, registry.Commit.String(), err)
			}
			bindExternalEntries(out, &spec, loaded, entry, canonical, registry.Commit.String())
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// findPluginCatalogRegistry locates the lock record for a git format
// source: the registry matching the configured canonical URL, ref and root
// path.
func findPluginCatalogRegistry(doc lockfile.Document, canonical, ref, rootPath string) (lockfile.GitRegistry, bool) {
	for _, registry := range doc.Registries {
		if registry.URL != canonical || registry.Ref != ref {
			continue
		}
		for _, root := range registry.Roots {
			if root.Path == rootPath && root.RevisionSchema == "plugin-v1" {
				return registry, true
			}
		}
	}
	return lockfile.GitRegistry{}, false
}

// bindExternalEntries records the owning-catalog binding of every selected
// external catalog entry, keyed by the external registry URL and the
// package root path the lock record uses.
func bindExternalEntries(out *PluginLockContexts, spec *catalog.PluginSelectionSpec, loaded catalog.LoadedCatalog, owner config.RegistryEntry, canonical, commit string) {
	selected := map[string]bool{}
	for _, request := range spec.Explicit {
		selected[request.Name] = true
	}
	for _, edge := range spec.Collections {
		for _, name := range edge.Entries {
			selected[name] = true
		}
	}
	if spec.All {
		for _, entry := range loaded.Entries {
			selected[entry.Name] = true
		}
	}
	for _, name := range spec.Excluded {
		delete(selected, name)
	}
	kind := "directory"
	if !owner.IsLocal() {
		kind = "git"
	}
	for _, entry := range loaded.Entries {
		if entry.Plugin == nil || entry.Plugin.Source.Kind != "git" || entry.Plugin.Refusal != nil || !selected[entry.Name] {
			continue
		}
		url, err := lockfile.CanonicalGitConfigURL(entry.Plugin.Source.URL)
		if err != nil {
			continue
		}
		rootPath := entry.Plugin.Source.Subdirectory
		if rootPath == "" {
			rootPath = "."
		}
		ref := entry.Plugin.Source.Ref
		if ref == "" {
			ref = "HEAD"
		}
		out.Expected[url+"\x00"+ref] = true
		out.Externals[externalRecordKey(url, rootPath)] = &sourcerevision.PluginExternal{
			CatalogKind:   kind,
			CatalogURL:    catalogIdentityURL(owner, canonical),
			CatalogRef:    owner.Ref,
			CatalogCommit: commit,
			CatalogRoot:   ownerRootPath(owner),
			EntryName:     entry.Name,
			EntryDigest:   entry.Plugin.DefinitionDigest,
		}
	}
}

// catalogIdentityURL renders the owning catalog's lock-manifest identity:
// its canonical git URL, or the resolved directory path.
func catalogIdentityURL(owner config.RegistryEntry, canonical string) string {
	if owner.IsLocal() {
		return owner.URL
	}
	return canonical
}

func ownerRootPath(owner config.RegistryEntry) string {
	if owner.Path == "" {
		return "."
	}
	return owner.Path
}

// ExternalEntriesFor enumerates the selected external catalog entries of one
// decoded plugin source for acquisition: name, exact git requirement, and
// definition digest.
func ExternalEntriesFor(spec *catalog.PluginSelectionSpec, loaded catalog.LoadedCatalog) []sourcerevision.PluginExternalEntry {
	if spec == nil {
		return nil
	}
	selected := map[string]bool{}
	for _, request := range spec.Explicit {
		selected[request.Name] = true
	}
	for _, edge := range spec.Collections {
		for _, name := range edge.Entries {
			selected[name] = true
		}
	}
	if spec.All {
		for _, entry := range loaded.Entries {
			if entry.Plugin != nil && entry.Plugin.Refusal == nil {
				selected[entry.Name] = true
			}
		}
	}
	for _, name := range spec.Excluded {
		delete(selected, name)
	}
	var out []sourcerevision.PluginExternalEntry
	for _, entry := range loaded.Entries {
		if entry.Plugin == nil || entry.Plugin.Source.Kind != "git" || entry.Plugin.Refusal != nil || !selected[entry.Name] {
			continue
		}
		out = append(out, sourcerevision.PluginExternalEntry{Name: entry.Name, Source: entry.Plugin.Source, DefinitionDigest: entry.Plugin.DefinitionDigest})
	}
	return out
}
