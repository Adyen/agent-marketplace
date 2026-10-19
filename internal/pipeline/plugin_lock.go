package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
)

// PluginLockContexts carries the per-root plugin-v1 verification inputs of
// one layer: the resolver's planned selection for each format root, the
// owning-catalog binding for each external-entry record, and the set of
// registry identities the current configuration requires as external
// records.
type PluginLockContexts struct {
	Plans     map[string][]catalog.PlannedPlugin
	Externals map[string]*sourcerevision.PluginExternal
	Expected  map[string]bool
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
// locked repository, ref, and package root path.
func externalRecordKey(canonical, ref, rootPath string) string {
	return canonical + "\x00" + ref + "\x00" + rootPath
}

// PluginContextFor returns the verification context for one locked root: the
// planned plugin selection for a catalog/package format root, or the
// owning-catalog binding for an external-entry root.
func (c *PluginLockContexts) PluginContextFor(registry lockfile.GitRegistry, root lockfile.GitRoot) *sourcerevision.PluginVerifyContext {
	if c == nil || root.RevisionSchema != "plugin-v1" {
		return nil
	}
	if plan := c.Plans[registryRootKey(registry.URL, registry.Ref, root.Path)]; plan != nil {
		return &sourcerevision.PluginVerifyContext{Plan: plan}
	}
	if external := c.Externals[externalRecordKey(registry.URL, registry.Ref, root.Path)]; external != nil {
		return &sourcerevision.PluginVerifyContext{External: external}
	}
	return nil
}

// BuildPluginLockContexts projects the layer's plugin-v1 verification
// inputs out of the resolver's own plugin selection plan: the planned
// plugins of each git format root and the owning-catalog binding of every
// selected external catalog entry. The layer's locked document supplies each
// git format root's recorded commit.
func BuildPluginLockContexts(plan catalog.PluginSelectionPlan, decls []config.SourceDecl, doc lockfile.Document) (*PluginLockContexts, error) {
	format := FormatSourceEntries(decls)
	if len(format) == 0 {
		return nil, nil
	}
	out := &PluginLockContexts{
		Plans:     map[string][]catalog.PlannedPlugin{},
		Externals: map[string]*sourcerevision.PluginExternal{},
		Expected:  map[string]bool{},
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
		planned := plan.ForEntry(entry)
		owner := pluginOwner{entry: entry}
		if entry.IsLocal() {
			out.bindPlannedExternals(planned, owner)
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
		out.Plans[registryRootKey(canonical, entry.Ref, rootPath)] = planned
		if info.Role != pluginpolicy.RoleCatalog {
			continue
		}
		if registry, ok := findPluginCatalogRegistry(doc, canonical, entry.Ref, rootPath); ok {
			owner.commit = registry.Commit.String()
		}
		owner.canonical = canonical
		out.bindPlannedExternals(planned, owner)
	}
	return out, nil
}

// pluginOwner is the owning-catalog identity of one format source for
// external-entry bindings.
type pluginOwner struct {
	entry     config.RegistryEntry
	canonical string
	commit    string
}

func (o pluginOwner) kind() string {
	if o.entry.IsLocal() {
		return "directory"
	}
	return "git"
}

func (o pluginOwner) url() string {
	if o.entry.IsLocal() {
		return o.entry.URL
	}
	return o.canonical
}

func (o pluginOwner) root() string {
	if o.entry.Path == "" {
		return "."
	}
	return o.entry.Path
}

// bindPlannedExternals records the owning-catalog binding of every planned
// external catalog entry, keyed by the external registry URL, ref, and
// package root path the lock record uses.
func (c *PluginLockContexts) bindPlannedExternals(planned []catalog.PlannedPlugin, owner pluginOwner) {
	for _, plugin := range planned {
		if plugin.Entry.Plugin == nil || plugin.Entry.Plugin.Source.Kind != "git" {
			continue
		}
		source := plugin.Entry.Plugin.Source
		url, err := lockfile.CanonicalGitConfigURL(source.URL)
		if err != nil {
			continue
		}
		rootPath := source.Subdirectory
		if rootPath == "" {
			rootPath = "."
		}
		ref := source.Ref
		if ref == "" {
			ref = "HEAD"
		}
		c.Expected[url+"\x00"+ref+"\x00"+rootPath] = true
		c.Externals[externalRecordKey(url, ref, rootPath)] = &sourcerevision.PluginExternal{
			CatalogKind:   owner.kind(),
			CatalogURL:    owner.url(),
			CatalogRef:    owner.entry.Ref,
			CatalogCommit: owner.commit,
			CatalogRoot:   owner.root(),
			EntryName:     plugin.Name,
			EntryDigest:   plugin.Entry.Plugin.DefinitionDigest,
			CatalogFormat: owner.entry.Format,
			EntryStrict:   plugin.Entry.Plugin.Strict,
			EntryOverlay:  plugin.Entry.Plugin.Overlay,
			Exclusions:    append([]string(nil), plugin.Exclusions...),
		}
	}
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

// PlannedExternalEntry renders one planned external plugin as the external
// entry an acquirer resolves and a verifier binds.
func PlannedExternalEntry(plugin catalog.PlannedPlugin) (sourcerevision.PluginExternalEntry, bool) {
	if plugin.Entry.Plugin == nil || plugin.Entry.Plugin.Source.Kind != "git" {
		return sourcerevision.PluginExternalEntry{}, false
	}
	return sourcerevision.PluginExternalEntry{
		Name:             plugin.Name,
		Source:           plugin.Entry.Plugin.Source,
		DefinitionDigest: plugin.Entry.Plugin.DefinitionDigest,
		EntryStrict:      plugin.Entry.Plugin.Strict,
		EntryOverlay:     plugin.Entry.Plugin.Overlay,
		Exclusions:       append([]string(nil), plugin.Exclusions...),
		Owner:            pluginOwnerLabel(plugin.Source),
	}, true
}

// pluginOwnerLabel renders a format source entry's identity for
// shared-origin diagnostics.
func pluginOwnerLabel(entry config.RegistryEntry) string {
	if entry.IsLocal() {
		return entry.URL
	}
	canonical, err := lockfile.CanonicalGitConfigURL(entry.URL)
	if err != nil {
		return entry.URL
	}
	root := entry.Path
	if root == "" {
		root = "."
	}
	return canonical + " path " + root
}

// CheckPlannedSharedOrigins refuses two selected external entries across the
// whole configuration that collapse to one lock record.
func CheckPlannedSharedOrigins(plan catalog.PluginSelectionPlan) error {
	var externals []sourcerevision.PluginExternalEntry
	for _, plugin := range plan.ExternalEntries() {
		ext, ok := PlannedExternalEntry(plugin)
		if !ok {
			continue
		}
		externals = append(externals, ext)
	}
	return sourcerevision.CheckSharedExternalOrigins(externals)
}

// BuildPluginSelectionPlan computes the resolver's plugin selection over the
// effective merged configuration: catalogs decode from the exact snapshots
// the lock commits (or live directories) through the same catalog loader the
// pipeline resolution uses, and the plan is the projected output of
// resolver.Resolve, so the plugin-v1 manifests commit exactly the selection
// the resolver performs.
func BuildPluginSelectionPlan(ctx context.Context, merged *config.Config, localCfg *config.LocalConfig, repoRoot, localConfigDir string, exact catalog.ExactSnapshotFunc) (catalog.PluginSelectionPlan, []error, error) {
	if merged == nil {
		return catalog.PluginSelectionPlan{}, nil, nil
	}
	cfg := *merged
	cfg.Registries = append([]config.RegistryEntry(nil), merged.Registries...)
	opts := []catalog.LoadOption{catalog.WithExactSnapshots(exact)}
	if localConfigDir != "" {
		opts = append(opts, catalog.WithLocalConfigDir(localConfigDir))
	}
	loaded := catalog.LoadRegistriesWithContext(ctx, &cfg, repoRoot, catalog.LoadModeCachedNoFetch, opts...)
	plan, errs := resolver.PlanPluginSelection(loaded.Config, catalog.EntriesByKey(loaded.Catalogs), localCfg, resolver.WithResources(loaded.ResourceCatalogs), resolver.WithMalformed(catalog.MalformedByKey(loaded.Catalogs)), resolver.WithContext(ctx))
	// A source whose catalog could not be loaded would silently contribute
	// neither plugins nor collection edges, so it stops the lock lifecycle
	// rather than committing or verifying a fabricated selection.
	var fatal error
	for _, entry := range loaded.Config.Registries {
		if _, ok := loaded.Catalogs[entry.CatalogKey]; !ok {
			fatal = errors.Join(fatal, fmt.Errorf("plugin lock selection cannot decode the catalog for %s", entry.URL))
		}
	}
	if err := CheckPlannedSharedOrigins(plan); err != nil {
		fatal = errors.Join(fatal, err)
	}
	return plan, errs, fatal
}

// PluginPlanErrors returns the plan diagnostics the lock lifecycle must
// surface: hard plugin and collection selection failures. Anything the
// resolver reports as a warning or a soft miss stays advisory; a format
// source whose catalog is absent still fails closed later, at manifest
// verification.
func PluginPlanErrors(errs []error) []error {
	var out []error
	for _, err := range errs {
		var memberErr *resolver.PluginMemberDirectSelectionError
		var exclusionErr *resolver.PluginUnknownExclusionError
		var refusalErr *resolver.PluginEntryRefusalError
		var notFound *resolver.SelectedNotFoundError
		var malformedErr *resolver.SelectedMalformedError
		switch {
		case errors.As(err, &memberErr), errors.As(err, &exclusionErr), errors.As(err, &refusalErr):
			out = append(out, err)
		case errors.As(err, &notFound) && (notFound.Kind == ir.KindPlugin || notFound.Kind == ir.KindCollection) && !resolver.IsSoftSelectedNotFound(err):
			out = append(out, err)
		case errors.As(err, &malformedErr) && (malformedErr.Kind == ir.KindPlugin || malformedErr.Kind == ir.KindCollection) && !resolver.IsSoftSelectedMalformed(err):
			out = append(out, err)
		}
	}
	return out
}

// DocExactSnapshots maps configured registry entries to the committed
// snapshots of the given locked documents. open supplies one snapshot of a
// record, acquiring it in writable modes or leasing it read-only, and may
// return a release invoked once loading completes.
func DocExactSnapshots(docs []lockfile.Document, open func(registry lockfile.GitRegistry) (path string, release func(), err error)) (catalog.ExactSnapshotFunc, func()) {
	var releases []func()
	releaseAll := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	fn := func(entry config.RegistryEntry) (string, string, error) {
		canonical, err := lockfile.CanonicalGitConfigURL(entry.URL)
		if err != nil {
			return "", "", err
		}
		for _, doc := range docs {
			for _, registry := range doc.Registries {
				if registry.URL != canonical || registry.Ref != entry.Ref {
					continue
				}
				path, release, err := open(registry)
				if err != nil {
					return "", "", err
				}
				if release != nil {
					releases = append(releases, release)
				}
				return path, canonical + "@" + registry.Commit.String() + ":" + entry.Path, nil
			}
		}
		return "", "", nil
	}
	return fn, releaseAll
}
