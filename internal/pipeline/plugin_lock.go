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
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
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

// ExternalIdentities returns the external record identities (canonical URL +
// ref + sole root path, see ExternalRecordIdentity) the configuration's
// selected catalog entries require in the lock; nil when the layer has no
// format sources.
func (c *PluginLockContexts) ExternalIdentities() map[string]bool {
	if c == nil {
		return nil
	}
	return c.Expected
}

// ExternalRecordIdentity is the Expected-set key of one locked external
// record: its canonical URL, ref, and sole root path. Records without
// exactly one root have no external identity.
func ExternalRecordIdentity(registry lockfile.GitRegistry) string {
	if len(registry.Roots) != 1 {
		return ""
	}
	return registry.URL + "\x00" + registry.Ref + "\x00" + registry.Roots[0].Path
}

// ExpectedExternalRecord reports whether registry is a plugin-v1 external
// record whose identity the expected set names.
func ExpectedExternalRecord(registry lockfile.GitRegistry, expected map[string]bool) bool {
	return expected[ExternalRecordIdentity(registry)] && AllRootsPluginV1(registry)
}

// ExpectedExternalsLocked reports whether registries carry a plugin-v1
// external record for every identity in expected: a newly selected or
// re-pointed catalog entry has no locked record and must report stale
// rather than be resolved during read-only verification.
func ExpectedExternalsLocked(registries []lockfile.GitRegistry, expected map[string]bool) bool {
	for key := range expected {
		found := false
		for _, registry := range registries {
			if ExternalRecordIdentity(registry) == key && AllRootsPluginV1(registry) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
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
	selectedCollections := SelectedCollections(decls)
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
			if err := bindExternalEntries(out, &spec, loaded, entry, "", ""); err != nil {
				return nil, err
			}
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
			return bindExternalEntries(out, &spec, loaded, entry, canonical, registry.Commit.String())
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SelectedCollections returns the collection names the declarations' ordered
// decisions select, matching how the resolver admits collection selections.
func SelectedCollections(decls []config.SourceDecl) map[string]bool {
	selected := map[string]bool{}
	for _, decl := range decls {
		for _, sel := range catalog.SelectedResources(registryEntryForDecl(decl), nil) {
			if sel.Kind == ir.KindCollection {
				selected[sel.Name] = true
			}
		}
	}
	return selected
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
func bindExternalEntries(out *PluginLockContexts, spec *catalog.PluginSelectionSpec, loaded catalog.LoadedCatalog, owner config.RegistryEntry, canonical, commit string) error {
	externals, err := ExternalEntriesFor(spec, loaded)
	if err != nil {
		return err
	}
	kind := "directory"
	if !owner.IsLocal() {
		kind = "git"
	}
	for _, ext := range externals {
		url, err := lockfile.CanonicalGitConfigURL(ext.Source.URL)
		if err != nil {
			continue
		}
		rootPath := ext.Source.Subdirectory
		if rootPath == "" {
			rootPath = "."
		}
		ref := ext.Source.Ref
		if ref == "" {
			ref = "HEAD"
		}
		out.Expected[url+"\x00"+ref+"\x00"+rootPath] = true
		out.Externals[externalRecordKey(url, rootPath)] = &sourcerevision.PluginExternal{
			CatalogKind:   kind,
			CatalogURL:    catalogIdentityURL(owner, canonical),
			CatalogRef:    owner.Ref,
			CatalogCommit: commit,
			CatalogRoot:   ownerRootPath(owner),
			EntryName:     ext.Name,
			EntryDigest:   ext.DefinitionDigest,
			CatalogFormat: owner.Format,
			EntryStrict:   ext.EntryStrict,
			EntryOverlay:  ext.EntryOverlay,
		}
	}
	return nil
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
// definition digest. Two selected entries resolving to the same external
// origin are refused.
func ExternalEntriesFor(spec *catalog.PluginSelectionSpec, loaded catalog.LoadedCatalog) ([]sourcerevision.PluginExternalEntry, error) {
	if spec == nil {
		return nil, nil
	}
	selected := catalog.PluginSelectedNames(spec, loaded.Entries)
	var out []sourcerevision.PluginExternalEntry
	for _, entry := range loaded.Entries {
		if entry.Plugin == nil || entry.Plugin.Source.Kind != "git" || entry.Plugin.Refusal != nil || !selected[entry.Name] {
			continue
		}
		out = append(out, sourcerevision.PluginExternalEntry{Name: entry.Name, Source: entry.Plugin.Source, DefinitionDigest: entry.Plugin.DefinitionDigest, EntryStrict: entry.Plugin.Strict, EntryOverlay: entry.Plugin.Overlay})
	}
	return out, sourcerevision.CheckSharedExternalOrigins(out)
}
