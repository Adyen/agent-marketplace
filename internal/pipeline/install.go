package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type PreparedInstallDependencies struct {
	Context         context.Context
	DiscoveryConfig *config.Config
	View            DependencyLockView
	Reconcile       DependencyReconciler
	BindRegistry    func(config.Source, config.RegistryEntry) error
	ResolvedCommit  func(config.RegistryEntry) (PreparedSourceCommit, error)
	Release         func()
}

// PreparedSourceCommit is the exact commit a prepared install will lock for a source.
type PreparedSourceCommit struct {
	Commit string
	Locked bool // kept from the target layer's existing acquisition lock
}

type InstallDependencyPreparer func(context.Context, string, string, []StagedConfigEdit) (PreparedInstallDependencies, error)

// InstallOptions holds the arguments for an install operation.
type InstallOptions struct {
	RepoRoot                string
	Args                    []string // resource name args (0 or more)
	Tag                     string   // --tag flag
	InstallAll              bool     // --all flag
	Registry                string   // --registry filter for --all
	Kind                    ir.ResourceKind
	HasKind                 bool
	Scope                   Scope
	ScopeSpecified          bool
	FromURL                 string // --from flag: ad-hoc registry URL
	FromRef                 string // --ref flag: git ref for --from
	FromPath                string // --path flag: subdirectory for --from
	Locators                []config.ResourceLocator
	ExpectedCommit          string
	InstalledName           string // --as for Marketplace installs
	AcceptRisk              bool   // --accept-risk/--yes for Marketplace installs
	Marketplace             string // --marketplace alias or endpoint selector
	ExactMarketplaceLocator *marketplace.MarketplaceLocator
	CatalogOption           []catalog.LoadOption

	// LocalConfigDir overrides the directory where the local config
	// (config.local.yaml) is written. Set by the Install function when
	// --scope local is used in a monorepo subdirectory: the config file
	// goes to cwd, but the repo root (for catalog loading and
	// materialization) stays as the parent. Empty means use the default
	// (ScopedConfigPath).
	LocalConfigDir      string
	DependencyReconcile DependencyReconciler
	PrepareDependencies InstallDependencyPreparer
	bindRegistry        func(config.RegistryEntry) error
	configStaging       *configStagingManager
	workspace           *Workspace
}

// silentSyncOptsForInstall builds silentSync options from InstallOptions.
// When LocalConfigDir is set (monorepo subdir with --scope local), the
// Workspace built by syncAfterInstall carries it so that materialization
// happens in the subdirectory and local entries' relative paths resolve
// against it.
func silentSyncOptsForInstall(opts InstallOptions) []silentSyncOpt {
	var syncOpts []silentSyncOpt
	syncOpts = append(syncOpts, withCatalogMode(catalog.LoadModeCached), withMarketplaceMode(resolver.MarketplaceResolveModeRefresh))
	if len(opts.CatalogOption) > 0 {
		syncOpts = append(syncOpts, withCatalogOptions(opts.CatalogOption))
	}
	if opts.DependencyReconcile != nil {
		syncOpts = append(syncOpts, withDependencyReconciler(opts.DependencyReconcile), withConfigStaging(opts.configStaging))
	}
	return syncOpts
}

// InstallResult holds the outcome of an install operation.
type InstallResult struct {
	Report    *materialize.Report
	Installed []string
	// NotSelected names resources the install asked for that the effective
	// configuration still does not select, for example because an exclusion
	// wins, so they were not installed.
	NotSelected                  []string
	IsCollection                 bool
	CollectionLabel              string
	RequirementWarnings          []error
	MarketplacePreview           string
	MarketplaceSummary           MarketplaceInstallSummary
	MarketplaceEndpoint          string
	MarketplaceConfigPath        string
	MarketplaceAlreadyConfigured bool
	// TransportNotice reports that install --from recorded the HTTPS form of
	// a web URL because SSH could not reach it; "" otherwise.
	TransportNotice string
}

type resourceIdentity struct {
	kind ir.ResourceKind
	name string
}

// Install adds resources to the appropriate config layer and re-syncs.
// It handles --tag, --all, --from, collection, and named-resource installs.
func Install(ctx context.Context, opts InstallOptions) (result *InstallResult, retErr error) {
	if opts.PrepareDependencies != nil && opts.DependencyReconcile == nil {
		return nil, errors.New("install dependency preparation requires a dependency reconciler")
	}
	marketplaceEndpointRequested := isMarketplaceEndpointRegistration(opts)
	if marketplaceEndpointRequested && !opts.ScopeSpecified {
		opts.Scope = ScopeUser
	}

	tel := telemetry.TelemetryFromContext(ctx)
	var installSpan telemetry.Span
	if tel != nil {
		ctx, installSpan = tel.StartSpan(ctx, "agent_marketplace.pipeline.install", installOptionAttrs(opts))
	}
	defer func() {
		if installSpan != (telemetry.Span{}) {
			installSpan.SetAttributes(installResultAttrs(result, retErr))
			installSpan.End()
		}
	}()

	kind, hasKind := opts.Kind, opts.HasKind
	repoRoot := opts.RepoRoot
	workspace, err := workspaceForRoots(repoRoot, opts.LocalConfigDir)
	if err != nil {
		return nil, err
	}
	if err := RecoverDependencyTransactions(ctx); err != nil {
		return nil, err
	}
	if err := ValidateConsumerIntent(ctx, workspace.RepoRoot(), workspace.LocalConfigDir()); err != nil {
		return nil, err
	}
	opts.workspace = &workspace
	marketplaceInstallRequested := isMarketplaceInstall(opts) && !marketplaceEndpointRequested
	if marketplaceInstallRequested {
		prepared, err := PrepareMarketplaceInstall(ctx, opts)
		if err != nil {
			return nil, err
		}
		if err := evaluateMarketplaceConsent(prepared.plan, opts.AcceptRisk); err != nil {
			return nil, &MarketplaceConsentError{
				Preview: prepared.preview, Prepared: prepared, Summary: marketplaceInstallSummary(prepared.plan),
				Acceptable: evaluateMarketplaceConsent(prepared.plan, true) == nil, Err: err,
			}
		}
		return CommitPreparedMarketplaceInstall(ctx, prepared)
	}

	configPath, err := ScopedConfigPath(repoRoot, opts.Scope)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	marketplaceMutationRequested := marketplaceEndpointRequested

	// Guard: when the resolved repo root is the home directory and scope is
	// repo (default), the project config path collapses into the global
	// config path. Writing a skeleton there would overwrite the user's
	// global config. Refuse and suggest --scope user instead.
	if opts.Scope == ScopeRepo && config.IsHomeDir(repoRoot) {
		return nil, fmt.Errorf("cannot install to repo config from home directory: the project config path would overwrite %s; use --scope user to write to the global config instead", configPath)
	}

	// When --scope repo and FindRepoRoot() returned a parent directory
	// (not cwd), create a skeleton .agents/config.yaml in cwd so the
	// install writes to the current directory rather than the parent.
	// This makes the subdirectory a new project root: FindRepoRoot()
	// returns cwd on subsequent calls, and the parent's config.yaml is
	// NOT inherited (the subdir is a separate project).
	//
	// When --scope local and FindRepoRoot() returned a parent directory,
	// redirect the config file path and materialization root to cwd.
	// The repo root stays as the parent so the parent's config.yaml is
	// still loaded as the repo layer (base). The local config (gitignored)
	// in the subdir layers on top. Relative paths stay relative; they
	// are resolved against the local config dir at catalog load time
	// via the LocalConfigDir option.
	switch opts.Scope {
	case ScopeRepo:
		// Resolve cwd explicitly: a failure here must surface as an error
		// rather than silently falling back to the ancestor repoRoot, which
		// would write/sync the wrong config target in this subdirectory flow.
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get current working directory for repo-scope install: %w", err)
		}
		if filepath.Clean(cwd) != filepath.Clean(repoRoot) {
			originalRepoRoot := repoRoot
			cwdProjectConfig := config.ProjectConfigPath(cwd)
			// Only seed a skeleton (and redirect) when the subdir config is
			// genuinely absent. A non-NotExist stat error (permission, I/O)
			// must be surfaced instead of clobbering an existing config.
			if _, statErr := os.Stat(cwdProjectConfig); errors.Is(statErr, os.ErrNotExist) {
				if _, err := workspaceForRoots(filepath.Clean(cwd), opts.LocalConfigDir); err != nil {
					return nil, err
				}
				if marketplaceMutationRequested {
					repoRoot = filepath.Clean(cwd)
					configPath = config.ProjectConfigPath(repoRoot)
					opts.RepoRoot = repoRoot
					slog.Info("install: redirecting Marketplace install to cwd without creating a config before preflight", "original_repo_root", originalRepoRoot, "cwd", filepath.Clean(cwd), "install_target", configPath)
					break
				}
				if opts.DependencyReconcile == nil {
					if err := seedMissingLayerRoot(config.LayerTarget{Source: config.RepoCommitted, StagedPath: cwdProjectConfig}); err != nil {
						return nil, fmt.Errorf("create skeleton config: %w", err)
					}
				}
				repoRoot = filepath.Clean(cwd)
				configPath = config.ProjectConfigPath(repoRoot)
				opts.RepoRoot = repoRoot // propagate to installFromAdHoc
				slog.Info("install: redirecting repo scope to cwd (monorepo subdirectory)",
					"original_repo_root", originalRepoRoot,
					"cwd", filepath.Clean(cwd),
					"install_target", configPath,
					"skeleton_created", true,
				)
			} else if statErr != nil {
				return nil, fmt.Errorf("stat cwd project config %s: %w", cwdProjectConfig, statErr)
			}
		}
	case ScopeLocal:
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get current working directory for local-scope install: %w", err)
		}
		if filepath.Clean(cwd) != filepath.Clean(repoRoot) {
			// Redirect config file path and materialization root to cwd.
			// Keep repoRoot as the parent so the parent's config.yaml is
			// loaded as the repo layer (base). LocalConfigDir tells
			// catalog loading to resolve local entries' relative paths
			// against cwd, not the parent.
			cwdClean := filepath.Clean(cwd)
			configPath = config.LocalConfigPath(cwdClean)
			opts.LocalConfigDir = cwdClean // propagate to installFromAdHoc
			slog.Info("install: redirecting local config path to cwd (monorepo subdirectory)",
				"repo_root", repoRoot,
				"cwd", cwdClean,
				"install_target", configPath,
			)
		}
	}

	configDestination := configPath
	workspace, err = workspaceForRoots(repoRoot, opts.LocalConfigDir)
	if err != nil {
		return nil, err
	}
	opts.workspace = &workspace
	if opts.DependencyReconcile != nil && !marketplaceEndpointRequested {
		opts.configStaging, err = newConfigStagingManager()
		if err != nil {
			return nil, err
		}
		defer closeConfigStaging(opts.configStaging, "install", &retErr)
		configPath, err = opts.configStaging.Path(configDestination)
		if err != nil {
			return nil, err
		}
	}
	layer := config.LayerTarget{Source: scopeToConfigSource(opts.Scope), StagedPath: configPath, DeclaringPath: configDestination}
	editInstallLayer := func(mutate func(*LayerMutation) error) error {
		return EditLayerAtPath(layer, mutate)
	}

	// Load the effective configuration once to validate config versions and resolve
	// the registry allowlist before mutating any config files.
	ec := LoadEffective(ctx, ConfigLoadOnline, config.LoadEffectiveOptions{
		RepoRoot:       workspace.RepoRoot(),
		LocalConfigDir: workspace.LocalConfigDir(),
	}, nil)
	if len(ec.Errors) > 0 {
		return nil, errors.Join(ec.Errors...)
	}

	if marketplaceEndpointRequested {
		return registerMarketplaceEndpoint(ctx, opts, configPath, &ec)
	}
	// --from ad-hoc registry path: validate URL, persist entry, then
	// discover and install resources from the new entry.
	if fromURL := strings.TrimSpace(opts.FromURL); fromURL != "" {
		return installFromAdHoc(ctx, opts, fromURL, kind, hasKind, &ec)
	}

	discoveryCfg, err := loadInstallDiscoveryConfig(ctx, opts, &ec)
	if err != nil {
		return nil, err
	}

	if discoveryCfg == nil || len(discoveryCfg.Registries) == 0 {
		return nil, fmt.Errorf("no registries configured; add at least one registry to the config")
	}

	// Configured installs select only from the exact inventory represented by
	// the current layer locks. A mutable ref may have advanced since the lock
	// was written, but that must not make unreviewed resources installable.
	preparationCtx := ctx
	ctx, exactView := exactLockViewWithContext(ctx, ec, false)
	if len(exactView.errors) > 0 && !onlyMissingInstallLocks(exactView.errors) {
		exactView.release()
		return nil, errors.Join(exactView.errors...)
	}
	// Keep every available layer pinned to its exact inventory. Only an entry
	// whose own layer lock is missing may bootstrap from its mutable ref.
	installExactSnapshot := catalog.ExactSnapshotFunc(func(entry config.RegistryEntry) (string, string, error) {
		snapshot, identity, snapshotErr := exactView.exactSnapshot(entry)
		var lockErr *LockViewError
		if errors.As(snapshotErr, &lockErr) && lockErr.Code == "lock.missing" {
			return "", "", nil
		}
		return snapshot, identity, snapshotErr
	})
	discoveryOptions := []catalog.LoadOption{catalog.WithExactSnapshots(installExactSnapshot)}
	releaseDiscovery := exactView.release
	if opts.PrepareDependencies != nil {
		for _, entry := range discoveryCfg.Registries {
			if entry.IsLocal() {
				continue
			}
			if _, _, err := installExactSnapshot(entry); err != nil {
				releaseDiscovery()
				return nil, err
			}
		}
		releaseDiscovery()
		releaseDiscovery = func() {}
		prepared, err := prepareInstallDiscovery(preparationCtx, &opts)
		if err != nil {
			return nil, err
		}
		defer prepared.Release()
		ctx, discoveryCfg = prepared.Context, prepared.DiscoveryConfig
		discoveryOptions = []catalog.LoadOption{prepared.View.CatalogOption}
	}
	catalogs, discoveredCollections, err := loadCatalogsForInstall(ctx, discoveryCfg, opts.RepoRoot, opts.LocalConfigDir, discoveryOptions...)
	releaseDiscovery()
	if err != nil {
		return nil, err
	}

	var installed []string
	var installedResources []resourceIdentity
	var mutateInstall func(*LayerMutation) error
	if trimmedTag := strings.TrimSpace(opts.Tag); trimmedTag != "" {
		if hasKind {
			installed = matchingResourceNamesByTag(catalogs, trimmedTag, kind)
			if len(installed) == 0 {
				return nil, fmt.Errorf("tag %q not found for %s resources in configured registries", trimmedTag, skill.KindLabel(kind))
			}
			mutateInstall = func(m *LayerMutation) error {
				return addResourcesToAllRegistries(m, discoveryCfg.Registries, catalogs, kind, installed, opts.bindRegistry)
			}
			installedResources = resourceIdentities(kind, installed)
		} else {
			installed = matchingResourceNamesByTag(catalogs, trimmedTag, ir.KindSkill)
			if len(installed) == 0 {
				return nil, fmt.Errorf("tag %q not found in configured registries", trimmedTag)
			}
			mutateInstall = func(m *LayerMutation) error {
				for i := range discoveryCfg.Registries {
					entry := &discoveryCfg.Registries[i]
					if _, ok := catalogs[entry.CatalogKey]; !ok {
						continue
					}
					if opts.bindRegistry != nil {
						if err := opts.bindRegistry(*entry); err != nil {
							return err
						}
					}
					binding, err := m.EnsureRegistry(*entry)
					if err != nil {
						return fmt.Errorf("ensure registry %s in config: %w", entry.URL, err)
					}
					if err := m.Apply(binding, config.SelectionChange{Operation: config.SelectionAddTag, Tag: trimmedTag}); err != nil {
						return fmt.Errorf("add tag %q to registry %s: %w", trimmedTag, entry.URL, err)
					}
				}
				return nil
			}
			installedResources = resourceIdentities(ir.KindSkill, installed)
		}
	} else if opts.InstallAll {
		target := scopeToConfigSource(opts.Scope)
		selectedEntries, err := installRegistrySelection(discoveryCfg.Registries, opts.Registry, target)
		if err != nil {
			return nil, err
		}
		if hasKind {
			// Filter to the specified kind only.
			installed, err = allDiscoveredResourceNames(discoveryCfg.Registries, catalogs, opts.Registry, target, kind)
			if err != nil {
				return nil, err
			}
			mutateInstall = func(m *LayerMutation) error {
				return addResourcesToAllRegistries(m, selectedEntries, catalogs, kind, installed, opts.bindRegistry)
			}
			installedResources = resourceIdentities(kind, installed)
		} else {
			// No --kind specified: install all resource kinds.
			var allInstalled []string
			var steps []func(*LayerMutation) error
			for _, k := range config.AllKinds {
				names, err := allDiscoveredResourceNames(discoveryCfg.Registries, catalogs, opts.Registry, target, k)
				if err != nil {
					// If no resources of this kind exist, skip silently.
					// Propagate all other errors (e.g. collisions).
					if isNoResourcesError(err) {
						continue
					}
					return nil, err
				}
				steps = append(steps, func(m *LayerMutation) error {
					return addResourcesToAllRegistries(m, selectedEntries, catalogs, k, names, opts.bindRegistry)
				})
				allInstalled = append(allInstalled, names...)
				installedResources = append(installedResources, resourceIdentities(k, names)...)
			}
			if len(allInstalled) == 0 {
				return nil, fmt.Errorf("no resources found in configured registries")
			}
			slices.Sort(allInstalled)
			installed = allInstalled
			mutateInstall = func(m *LayerMutation) error {
				for _, step := range steps {
					if err := step(m); err != nil {
						return err
					}
				}
				return nil
			}
		}
	} else {
		if len(opts.Args) == 0 {
			return nil, fmt.Errorf("resource name is required when not using --tag or --all")
		}
		plan, warnIdentities, err := planNamedInstalls(opts.Args, catalogs, discoveredCollections, discoveredCollections, discoveryCfg, kind, hasKind, opts.Scope, nil)
		if err != nil {
			return nil, err
		}
		// Always write includes scoped to the providing registry entry,
		// creating a shadow entry in the target scope's config when the
		// registry is only defined at a lower layer. Keeping every selection
		// registry-scoped (instead of a top-level skills.include) gives one
		// consistent representation across scopes and avoids the
		// scoped/non-scoped mix users found confusing.
		if err := applyNamedInstalls(editInstallLayer, plan, opts.bindRegistry); err != nil {
			return nil, err
		}
		if err := clearNamedInstallExcludes(repoRoot, plan, opts.configStaging); err != nil {
			return nil, err
		}
		return syncNamedInstallResult(ctx, opts, plan, warnIdentities, silentSyncOptsForInstall(opts)...)
	}

	if err := editInstallLayer(mutateInstall); err != nil {
		return nil, err
	}

	installedResources, err = expandInstalledCollectionResources(installedResources, discoveredCollections, discoveryCfg.Registries, opts.Scope)
	if err != nil {
		return nil, err
	}
	return syncInstalledResources(ctx, opts, installed, installedResources, silentSyncOptsForInstall(opts)...)
}

func resourceIdentities(kind ir.ResourceKind, names []string) []resourceIdentity {
	resources := make([]resourceIdentity, 0, len(names))
	for _, name := range names {
		resources = append(resources, resourceIdentity{kind: kind, name: strings.TrimSpace(name)})
	}
	return resources
}

type namedInstallSelection struct {
	locator    config.ResourceLocator
	entry      config.RegistryEntry
	excludes   config.RegistryEntry
	collection bool
	label      string
	members    []resourceIdentity
}

type namedInstallSource struct {
	apply    config.RegistryEntry
	excludes config.RegistryEntry
	fromURL  string
}

func planNamedInstalls(args []string, catalogs map[string]catalog.LoadedCatalog, collections, expansion []collection.Collection, cfg *config.Config, kind ir.ResourceKind, hasKind bool, scope Scope, src *namedInstallSource) ([]namedInstallSelection, []resourceIdentity, error) {
	seen := make(map[string]bool, len(args))
	var plan []namedInstallSelection
	var warnings []resourceIdentity
	for _, raw := range args {
		name := strings.TrimSpace(raw)
		if name == "" {
			return nil, nil, fmt.Errorf("resource name must not be empty")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		// Detect collection install unless the caller has fixed another kind.
		if col := collection.Find(collections, name); col != nil && (!hasKind || kind == ir.KindCollection) {
			members, err := collectionInstallResources(col.Name, expansion, cfg.Registries, scope)
			if err != nil {
				return nil, nil, err
			}
			var providerEntry config.RegistryEntry
			if src != nil {
				providerEntry = src.apply
			} else {
				found := false
				for i := range cfg.Registries {
					if cfg.Registries[i].CatalogKey == col.SourceCatalogKey {
						providerEntry = cfg.Registries[i]
						found = true
						break
					}
				}
				if !found {
					return nil, nil, fmt.Errorf("collection %q was found but no providing registry entry could be identified", col.Name)
				}
			}
			if err := validateRegistryWriteScope(scope, providerEntry); err != nil {
				return nil, nil, err
			}
			plan = append(plan, namedInstallSelection{
				locator:    config.ResourceLocator{Kind: ir.KindCollection, Name: col.Name},
				entry:      providerEntry,
				collection: true,
				label:      col.Label(),
				members:    members,
			})
			warnings = append(warnings, members...)
			continue
		}
		if malformedErr := malformedSkillInstallError(catalogs, name, malformedKindFilter(kind, hasKind)); malformedErr != nil {
			return nil, nil, malformedErr
		}
		match, err := findCatalogResource(catalogs, name, kind, hasKind)
		if err != nil {
			return nil, nil, err
		}
		if match == nil {
			if src != nil {
				return nil, nil, fmt.Errorf("resource or collection %q not found in registry %s", name, src.fromURL)
			}
			return nil, nil, fmt.Errorf("resource or collection %q not found in configured registries", name)
		}
		var apply, exclude config.RegistryEntry
		if src != nil {
			apply, exclude = src.apply, src.excludes
		} else {
			entry := findProvidingEntry(cfg, catalogs, match.kind, match.name)
			if entry == nil {
				return nil, nil, fmt.Errorf("%s %q was found but no providing registry entry could be identified", skill.KindLabel(match.kind), match.name)
			}
			apply, exclude = *entry, *entry
		}
		// Repo-relative local registries are project-local and cannot be
		// shadowed into the user-personal (global) config.
		if err := validateRegistryWriteScope(scope, apply); err != nil {
			return nil, nil, err
		}
		plan = append(plan, namedInstallSelection{
			locator:  config.ResourceLocator{Kind: match.kind, Name: name},
			entry:    apply,
			excludes: exclude,
		})
		warnings = append(warnings, resourceIdentity{kind: match.kind, name: name})
	}
	if len(plan) == 0 {
		return nil, nil, fmt.Errorf("resource name is required when not using --tag or --all")
	}
	return plan, warnings, nil
}

func applyNamedInstalls(edit func(func(*LayerMutation) error) error, plan []namedInstallSelection, bind func(config.RegistryEntry) error) error {
	if bind != nil {
		bound := make(map[string]bool, len(plan))
		for _, sel := range plan {
			key := installRegistrySourceKey(sel.entry)
			if bound[key] {
				continue
			}
			bound[key] = true
			if err := bind(sel.entry); err != nil {
				return err
			}
		}
	}
	return edit(func(m *LayerMutation) error {
		for _, sel := range plan {
			binding, err := m.EnsureRegistry(sel.entry)
			if err != nil {
				return fmt.Errorf("ensure registry %s in config: %w", sel.entry.URL, err)
			}
			if err := m.Apply(binding, config.SelectionChange{Operation: config.SelectionInclude, Locators: []config.ResourceLocator{sel.locator}}); err != nil {
				return err
			}
		}
		return nil
	})
}

func clearNamedInstallExcludes(repoRoot string, plan []namedInstallSelection, staging *configStagingManager) error {
	for _, sel := range plan {
		if sel.collection {
			continue
		}
		if err := clearInstallExcludes(repoRoot, sel.excludes, sel.locator.Kind, sel.locator.Name, staging); err != nil {
			return err
		}
	}
	return nil
}

func namedInstallNames(plan []namedInstallSelection) []string {
	var names []string
	seen := map[string]bool{}
	for _, sel := range plan {
		reported := []resourceIdentity{{kind: sel.locator.Kind, name: sel.locator.Name}}
		if sel.collection {
			reported = sel.members
		}
		for _, member := range reported {
			name := strings.TrimSpace(member.name)
			if name != "" && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

func syncNamedInstallResult(ctx context.Context, opts InstallOptions, plan []namedInstallSelection, warnings []resourceIdentity, syncOpts ...silentSyncOpt) (*InstallResult, error) {
	if len(plan) == 1 && plan[0].collection {
		synced, err := syncAfterInstall(ctx, opts, plan[0].members, syncOpts...)
		if err != nil && synced.Report == nil {
			return nil, err
		}
		return &InstallResult{
			IsCollection:        true,
			CollectionLabel:     plan[0].label,
			RequirementWarnings: synced.Warnings,
			Report:              synced.Report,
		}, err
	}
	return syncInstalledResources(ctx, opts, namedInstallNames(plan), warnings, syncOpts...)
}

func collectionInstallResources(name string, available []collection.Collection, registries []config.RegistryEntry, scope Scope) ([]resourceIdentity, error) {
	expanded, expandErrs := collection.Expand([]string{name}, available, registries)
	if err := errors.Join(expandErrs...); err != nil {
		return nil, err
	}
	providerEntries := make([]config.RegistryEntry, 0, len(expanded.SyntheticEntries)+1)
	if col := collection.Find(available, name); col != nil {
		providerEntries = append(providerEntries, config.RegistryEntry{URL: col.SourceRegistryURL})
	}
	providerEntries = append(providerEntries, expanded.SyntheticEntries...)
	if err := validateRegistryWriteScope(scope, providerEntries...); err != nil {
		return nil, err
	}
	var resources []resourceIdentity
	for _, entry := range expanded.SyntheticEntries {
		for _, kind := range config.AllKinds {
			resources = append(resources, resourceIdentities(kind, entry.SelectorForKind(kind).Include)...)
		}
	}
	return resources, nil
}

func expandInstalledCollectionResources(installed []resourceIdentity, available []collection.Collection, registries []config.RegistryEntry, scope Scope) ([]resourceIdentity, error) {
	expanded := slices.Clone(installed)
	for _, resource := range installed {
		if resource.kind != ir.KindCollection {
			continue
		}
		members, err := collectionInstallResources(resource.name, available, registries, scope)
		if err != nil {
			return nil, err
		}
		expanded = append(expanded, members...)
	}
	return expanded, nil
}

// syncInstalledResources syncs after an install and reports the named
// resources the final resolution selected separately from those it did not.
func syncInstalledResources(ctx context.Context, iopts InstallOptions, names []string, resources []resourceIdentity, opts ...silentSyncOpt) (*InstallResult, error) {
	var resolved []resolver.ResolvedArtifact
	synced, err := syncAfterInstall(ctx, iopts, resources, append(slices.Clip(opts), withResolvedCapture(&resolved))...)
	if err != nil && synced.Report == nil {
		return nil, err
	}
	installed, notSelected := partitionInstalledNames(names, resources, resolved)
	return &InstallResult{Installed: installed, NotSelected: notSelected, RequirementWarnings: synced.Warnings, Report: synced.Report}, err
}

// partitionInstalledNames keeps a name as installed when the resolution
// selected it in any of its installed kinds; a name without a known kind is
// kept rather than reported as unselected without evidence.
func partitionInstalledNames(names []string, resources []resourceIdentity, resolved []resolver.ResolvedArtifact) (installed, notSelected []string) {
	selected := make(map[resourceIdentity]bool, len(resolved))
	for _, artifact := range resolved {
		selected[resourceIdentity{kind: artifact.Kind, name: strings.TrimSpace(artifact.Name)}] = true
	}
	kinds := make(map[string][]ir.ResourceKind, len(resources))
	for _, resource := range resources {
		kinds[strings.TrimSpace(resource.name)] = append(kinds[strings.TrimSpace(resource.name)], resource.kind)
	}
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		found := len(kinds[trimmed]) == 0
		for _, kind := range kinds[trimmed] {
			found = found || selected[resourceIdentity{kind: kind, name: trimmed}]
		}
		if found {
			installed = append(installed, name)
		} else {
			notSelected = append(notSelected, name)
		}
	}
	return installed, notSelected
}

func syncAfterInstall(ctx context.Context, iopts InstallOptions, installed []resourceIdentity, opts ...silentSyncOpt) (SyncResult, error) {
	ws := iopts.workspace
	if ws == nil {
		resolved, err := workspaceForRoots(iopts.RepoRoot, iopts.LocalConfigDir)
		if err != nil {
			return SyncResult{}, err
		}
		ws = &resolved
	}
	synced, err := silentSyncCollect(ctx, *ws, opts...)
	synced.Warnings = filterInstallWarnings(synced.Warnings, installed)
	return synced, err
}

func filterInstallWarnings(warnings []error, installed []resourceIdentity) []error {
	var installedResources map[resourceIdentity]struct{}
	if len(installed) > 0 {
		installedResources = make(map[resourceIdentity]struct{}, len(installed))
		for _, resource := range installed {
			resource.name = strings.TrimSpace(resource.name)
			installedResources[resource] = struct{}{}
		}
	}

	installWarnings := make([]error, 0)
	for _, warning := range warnings {
		if IsPromotedDiagnostic(warning) {
			installWarnings = append(installWarnings, warning)
			continue
		}
		var requirement *resolver.UnmetRequirementWarning
		var resource resourceIdentity
		if errors.As(warning, &requirement) && requirement != nil {
			resource = resourceIdentity{kind: requirement.ResourceKind, name: strings.TrimSpace(requirement.ResourceName)}
		} else {
			var collision *resolver.NameCollisionWarning
			if !errors.As(warning, &collision) || collision == nil {
				continue
			}
			resource = resourceIdentity{kind: collision.Kind, name: strings.TrimSpace(collision.Name)}
		}
		if installedResources != nil {
			if _, ok := installedResources[resource]; !ok {
				continue
			}
		}
		installWarnings = append(installWarnings, warning)
	}
	slog.Debug("install warnings filtered", "warnings", len(warnings), "installed_resources", len(installed), "kept", len(installWarnings))
	return installWarnings
}

func prepareInstallDiscovery(ctx context.Context, opts *InstallOptions) (PreparedInstallDependencies, error) {
	if opts.configStaging == nil {
		return PreparedInstallDependencies{}, errors.New("exact install preparation requires staged configuration")
	}
	edits, err := opts.configStaging.Edits()
	if err != nil {
		return PreparedInstallDependencies{}, err
	}
	prepared, err := opts.PrepareDependencies(ctx, opts.RepoRoot, opts.LocalConfigDir, edits)
	if err != nil {
		return PreparedInstallDependencies{}, err
	}
	if prepared.Context == nil || prepared.DiscoveryConfig == nil || prepared.View.CatalogOption == nil || prepared.Reconcile == nil || prepared.Release == nil {
		if prepared.Release != nil {
			prepared.Release()
		}
		return PreparedInstallDependencies{}, errors.New("exact install preparation returned an incomplete dependency view")
	}
	opts.DependencyReconcile = prepared.Reconcile
	source := config.RepoCommitted
	switch opts.Scope {
	case ScopeUser:
		source = config.UserPersonal
	case ScopeLocal:
		source = config.RepoPersonal
	}
	opts.bindRegistry = func(entry config.RegistryEntry) error {
		if entry.IsLocal() {
			return nil
		}
		if prepared.BindRegistry == nil {
			return errors.New("exact install preparation cannot bind a registry selection")
		}
		return prepared.BindRegistry(source, entry)
	}
	return prepared, nil
}

func loadInstallDiscoveryConfig(ctx context.Context, opts InstallOptions, ec *config.EffectiveConfig) (*config.Config, error) {
	if ec == nil {
		ws, err := workspaceForRoots(opts.RepoRoot, opts.LocalConfigDir)
		if err != nil {
			return nil, err
		}
		ec = new(config.EffectiveConfig)
		*ec = LoadEffective(ctx, ConfigLoadOnline, config.LoadEffectiveOptions{
			RepoRoot:       ws.RepoRoot(),
			LocalConfigDir: ws.LocalConfigDir(),
		}, nil)
	}
	if len(ec.Errors) > 0 {
		return nil, errors.Join(ec.Errors...)
	}

	// LoadEffective already merged the layers, stamped registry
	// sources, and enforced the resolved allowlist.
	return ec.Merged, nil
}

func onlyMissingInstallLocks(errs []error) bool {
	for _, err := range errs {
		var lockErr *LockViewError
		if !errors.As(err, &lockErr) || lockErr.Code != "lock.missing" {
			return false
		}
	}
	return len(errs) > 0
}

func loadCatalogsForInstall(ctx context.Context, cfg *config.Config, repoRoot, localConfigDir string, opts ...catalog.LoadOption) (map[string]catalog.LoadedCatalog, []collection.Collection, error) {
	if localConfigDir != "" && localConfigDir != repoRoot {
		opts = append(opts, catalog.WithLocalConfigDir(localConfigDir))
	}
	result := catalog.LoadRegistriesWithContext(ctx, cfg, repoRoot, catalog.LoadModeRefresh, opts...)
	if len(result.Errors) > 0 {
		return result.Catalogs, result.Collections, errors.Join(result.Errors...)
	}
	return result.Catalogs, result.Collections, nil
}

func clearInstallExcludes(repoRoot string, entry config.RegistryEntry, kind ir.ResourceKind, name string, staging *configStagingManager) error {
	destination := config.LocalConfigPath(repoRoot)
	if _, err := os.Stat(destination); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	mapped := destination
	if staging != nil {
		var err error
		mapped, err = staging.Path(destination)
		if err != nil {
			return err
		}
	}
	return EditLayerAtPath(config.LayerTarget{Source: config.RepoPersonal, StagedPath: mapped, DeclaringPath: destination}, func(m *LayerMutation) error {
		return m.ClearInstallExclude(entry, config.ResourceLocator{Kind: kind, Name: name})
	})
}

// installRegistrySelection returns every entry without a filter. With a
// --registry filter it returns one entry for the one physical source the
// filter names, preferring the target layer's entry and then the highest
// precedence one: the same source declared in several layers, or spelled
// differently, is not ambiguous.
func installRegistrySelection(registries []config.RegistryEntry, registryFilter string, target config.Source) ([]config.RegistryEntry, error) {
	registryFilter = strings.TrimSpace(registryFilter)
	if registryFilter == "" {
		return append([]config.RegistryEntry(nil), registries...), nil
	}

	wanted := installRegistryURLKey(registryFilter)
	var matched []config.RegistryEntry
	chosen := map[string]config.RegistryEntry{}
	var sources []string
	for _, entry := range registries {
		if installRegistryURLKey(entry.URL) != wanted {
			continue
		}
		matched = append(matched, entry)
		key := installRegistrySourceKey(entry)
		if current, seen := chosen[key]; !seen {
			sources = append(sources, key)
			chosen[key] = entry
		} else if preferInstallRegistryEntry(current, entry, target) {
			chosen[key] = entry
		}
	}
	switch {
	case len(matched) == 0:
		return nil, fmt.Errorf("registry %q is not declared in any config layer", registryFilter)
	case len(sources) > 1:
		return nil, fmt.Errorf("registry %q matches %d configured entries with different refs or paths: %s; install --all --registry needs exactly one, so install those resources by name or remove one of the entries", registryFilter, len(sources), describeInstallRegistrySources(matched))
	}
	return []config.RegistryEntry{chosen[sources[0]]}, nil
}

// preferInstallRegistryEntry reports whether candidate should represent a
// source instead of current: the target layer's entry wins, then the entry
// with the higher layer and file precedence, whose attributes the resolver
// would apply.
func preferInstallRegistryEntry(current, candidate config.RegistryEntry, target config.Source) bool {
	if (candidate.Source == target) != (current.Source == target) {
		return candidate.Source == target
	}
	if candidate.Source != current.Source {
		return candidate.Source > current.Source
	}
	return candidate.FilePrecedence > current.FilePrecedence
}

func installRegistryURLKey(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if canonical, err := canonicalUpdateRegistryURL(trimmed); err == nil {
		return canonical
	}
	return trimmed
}

func installRegistrySourceKey(entry config.RegistryEntry) string {
	return installRegistryURLKey(entry.URL) + "\x00" + strings.TrimSpace(entry.Ref) + "\x00" + installRegistryPathLabel(entry.Path)
}

func installRegistryPathLabel(raw string) string {
	if trimmed := strings.Trim(filepath.ToSlash(strings.TrimSpace(raw)), "/"); trimmed != "." {
		return trimmed
	}
	return ""
}

// describeInstallRegistrySources lists each distinct source once, with its
// ref, path, and the config layers that declare it.
func describeInstallRegistrySources(entries []config.RegistryEntry) string {
	var order []string
	layers := map[string][]string{}
	labels := map[string]string{}
	for _, entry := range entries {
		key := installRegistrySourceKey(entry)
		if _, seen := labels[key]; !seen {
			order = append(order, key)
			label := "repository root"
			if trimmed := installRegistryPathLabel(entry.Path); trimmed != "" {
				label = "path " + trimmed
			}
			if ref := strings.TrimSpace(entry.Ref); ref != "" {
				label = "ref " + ref + ", " + label
			}
			labels[key] = label
		}
		if layer := installRegistryLayerLabel(entry.Source); !slices.Contains(layers[key], layer) {
			layers[key] = append(layers[key], layer)
		}
	}
	descriptions := make([]string, 0, len(order))
	for _, key := range order {
		descriptions = append(descriptions, labels[key]+" in the "+strings.Join(layers[key], " and "))
	}
	return strings.Join(descriptions, "; ")
}

func installRegistryLayerLabel(source config.Source) string {
	switch source {
	case config.Organization:
		return "organization config"
	case config.UserPersonal:
		return "user config"
	case config.RepoCommitted:
		return "repository config"
	case config.RepoPersonal:
		return "personal-local config"
	default:
		return "config"
	}
}

func allDiscoveredResourceNames(registries []config.RegistryEntry, catalogs map[string]catalog.LoadedCatalog, registryFilter string, target config.Source, kind ir.ResourceKind) ([]string, error) {
	registryFilter = strings.TrimSpace(registryFilter)

	selectedEntries, err := installRegistrySelection(registries, registryFilter, target)
	if err != nil {
		return nil, err
	}

	selectedNames := make(map[string]struct{})
	nameSources := make(map[string]map[string]struct{})

	for _, entry := range selectedEntries {
		catalogForEntry := catalogs[entry.CatalogKey]
		for _, discovered := range catalogForEntry.Entries {
			if discovered.Kind != kind {
				continue
			}
			name := strings.TrimSpace(discovered.Name)
			if name == "" {
				continue
			}

			selectedNames[name] = struct{}{}
			if _, ok := nameSources[name]; !ok {
				nameSources[name] = make(map[string]struct{})
			}
			nameSources[name][entry.CatalogKey] = struct{}{}
		}
	}

	if len(selectedNames) == 0 {
		if registryFilter != "" {
			return nil, noResourcesError{kind: kind, registry: registryFilter}
		}
		return nil, noResourcesError{kind: kind}
	}

	var collisionErrs []error
	for name, sources := range nameSources {
		if len(sources) < 2 || resolver.HasAdditiveSources(kind) {
			continue
		}

		sourceList := make([]string, 0, len(sources))
		for source := range sources {
			sourceList = append(sourceList, source)
		}
		slices.Sort(sourceList)

		collisionErrs = append(collisionErrs, fmt.Errorf("%s %q is provided by multiple registries: %s; rerun with --registry <url> to limit install --all or exclude one source in config", skill.KindLabel(kind), name, strings.Join(sourceList, ", ")))
	}
	if len(collisionErrs) > 0 {
		return nil, errors.Join(collisionErrs...)
	}

	names := make([]string, 0, len(selectedNames))
	for name := range selectedNames {
		names = append(names, name)
	}
	slices.Sort(names)

	return names, nil
}

func findProvidingEntry(cfg *config.Config, catalogs map[string]catalog.LoadedCatalog, kind ir.ResourceKind, resourceName string) *config.RegistryEntry {
	resourceName = strings.TrimSpace(resourceName)
	for i := range cfg.Registries {
		entry := &cfg.Registries[i]
		catalogForEntry, ok := catalogs[entry.CatalogKey]
		if !ok {
			continue
		}
		for _, s := range catalogForEntry.Entries {
			if s.Kind == kind && strings.TrimSpace(s.Name) == resourceName {
				return entry
			}
		}
	}
	return nil
}

func addResourcesToAllRegistries(m *LayerMutation, entries []config.RegistryEntry, catalogs map[string]catalog.LoadedCatalog, kind ir.ResourceKind, names []string, bind func(config.RegistryEntry) error) error {
	for i := range entries {
		entry := &entries[i]
		catalogForEntry, ok := catalogs[entry.CatalogKey]
		if !ok {
			continue
		}
		var locators []config.ResourceLocator
		for _, name := range names {
			for _, s := range catalogForEntry.Entries {
				if s.Kind == kind && strings.TrimSpace(s.Name) == strings.TrimSpace(name) {
					locators = append(locators, config.ResourceLocator{Kind: kind, Name: name})
					break
				}
			}
		}
		if len(locators) == 0 {
			continue
		}
		if bind != nil {
			if err := bind(*entry); err != nil {
				return err
			}
		}
		binding, err := m.EnsureRegistry(*entry)
		if err != nil {
			return fmt.Errorf("ensure registry %s in config: %w", entry.URL, err)
		}
		if err := m.Apply(binding, config.SelectionChange{Operation: config.SelectionInclude, Locators: locators}); err != nil {
			return fmt.Errorf("add %s to registry %s: %w", skill.KindLabel(kind), entry.URL, err)
		}
	}
	return nil
}

type catalogResourceMatch struct {
	kind ir.ResourceKind
	name string
}

func findCatalogResource(catalogs map[string]catalog.LoadedCatalog, wanted string, kind ir.ResourceKind, hasKind bool) (*catalogResourceMatch, error) {
	wanted = strings.TrimSpace(wanted)
	if wanted == "" {
		return nil, nil
	}

	matches := make(map[string]catalogResourceMatch)
	for _, catalogForEntry := range catalogs {
		for _, candidate := range catalogForEntry.Entries {
			if strings.TrimSpace(candidate.Name) != wanted {
				continue
			}
			if hasKind && candidate.Kind != kind {
				continue
			}
			matches[skill.ResourceKey(candidate.Kind, candidate.Name)] = catalogResourceMatch{kind: candidate.Kind, name: candidate.Name}
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		var labels []string
		for _, match := range matches {
			labels = append(labels, skill.KindLabel(match.kind))
		}
		slices.Sort(labels)
		return nil, fmt.Errorf("resource %q is ambiguous across kinds (%s); rerun with --kind", wanted, strings.Join(labels, ", "))
	}
	for _, match := range matches {
		return &match, nil
	}
	return nil, nil
}

func matchingResourceNamesByTag(catalogs map[string]catalog.LoadedCatalog, tag string, kind ir.ResourceKind) []string {
	matches := make(map[string]struct{})

	for _, catalogForEntry := range catalogs {
		for _, candidate := range catalogForEntry.Entries {
			if candidate.Kind != kind {
				continue
			}
			if config.HasMatchingTag(candidate.Tags, []string{tag}) {
				matches[candidate.Name] = struct{}{}
			}
		}
	}

	names := make([]string, 0, len(matches))
	for name := range matches {
		names = append(names, name)
	}
	slices.Sort(names)

	return names
}

// malformedSkillInstallError reports that the requested resource exists in a
// registry but failed ingest validation, so it was dropped from the catalog and
// cannot be installed.
//
// The correlation itself lives in skill.MatchMalformed, which resolver.Resolve
// also calls for the sync path (resolver.SelectedMalformedError). Keeping both
// paths on one matcher is deliberate: they diagnose the same condition, and when
// install and sync disagree about whether a selection is malformed or missing,
// the user is told to delete a selection that install just refused to re-add.
func malformedSkillInstallError(catalogs map[string]catalog.LoadedCatalog, wanted, kind string) error {
	wanted = strings.TrimSpace(wanted)
	if wanted == "" {
		return nil
	}

	for _, catalogForEntry := range catalogs {
		if match, found := skill.MatchMalformedForKind(catalogForEntry.Malformed, kind, wanted); found {
			return fmt.Errorf("skill %q cannot be installed: %s: %w", wanted, skill.ParseFailure(match), match.Error)
		}
	}

	return nil
}

// malformedKindFilter returns the kind label malformed-resource correlation
// should match, or "" when the request names no kind.
func malformedKindFilter(kind ir.ResourceKind, hasKind bool) string {
	if !hasKind {
		return ""
	}
	return kind.String()
}

func validateRegistryWriteScope(scope Scope, entries ...config.RegistryEntry) error {
	if scope != ScopeUser {
		return nil
	}
	if err := config.ValidateGlobalRegistryWrite(entries...); err != nil {
		slog.Warn("install rejected registry write", "scope", scope, "error", err)
		return err
	}
	return nil
}

// InstallScopeLabel returns a human-readable label for the install scope.
func InstallScopeLabel(scope Scope) string {
	switch scope {
	case ScopeUser:
		return "user-wide"
	case ScopeLocal:
		return "locally"
	default:
		return "in this repo"
	}
}

// pathBase returns the last component of a slash-separated path.
// e.g. "skills/helm-chart-mirroring" → "helm-chart-mirroring"
func pathBase(p string) string {
	p = strings.TrimSuffix(p, "/")
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		return p[idx+1:]
	}
	return p
}

// installFromAdHoc handles the --from code path: validate the URL against
// policy, persist the registry entry, discover resources, and install.
func installFromAdHoc(ctx context.Context, opts InstallOptions, fromURL string, kind ir.ResourceKind, hasKind bool, ec *config.EffectiveConfig) (result *InstallResult, retErr error) {
	ws, err := workspaceForRoots(opts.RepoRoot, opts.LocalConfigDir)
	if err != nil {
		return nil, err
	}
	if ec == nil {
		ec = new(config.EffectiveConfig)
		*ec = LoadEffective(ctx, ConfigLoadOnline, config.LoadEffectiveOptions{
			RepoRoot:       ws.RepoRoot(),
			LocalConfigDir: ws.LocalConfigDir(),
		}, nil)
	}
	if len(ec.Errors) > 0 {
		return nil, errors.Join(ec.Errors...)
	}

	repoRoot := opts.RepoRoot

	// Resolve the URL: parse web URLs (GitLab/GitHub) into git URL + ref + path.
	// Explicit --ref/--path flags take precedence over values parsed from the URL.
	rawFromURL := fromURL
	resolvedURL, resolvedRef, resolvedPath, _ := ResolveFromURL(fromURL, opts.FromRef, opts.FromPath)
	fromURL = resolvedURL
	fromRef := resolvedRef
	fromPath := resolvedPath

	// Validate the --from URL against the effective allowlist before
	// writing to config or fetching anything.
	syntheticEntry := config.RegistryEntry{URL: fromURL, Ref: fromRef, Path: fromPath}
	if opts.ExpectedCommit != "" && syntheticEntry.IsLocal() {
		return nil, errors.New("expected commit cannot be used with a directory source")
	}
	if err := config.ValidateRegistryURLs([]config.RegistryEntry{syntheticEntry}, ec.Allowed); err != nil {
		return nil, err
	}
	if err := config.ValidateRegistryPath(syntheticEntry.Path); err != nil {
		slog.Warn("install rejected invalid registry path", "error", err)
		return nil, err
	}
	if opts.ExpectedCommit != "" && opts.PrepareDependencies == nil {
		return nil, errors.New("expected commit requires an exact prepared source commit")
	}
	for _, locator := range opts.Locators {
		if strings.TrimSpace(locator.Name) == "" {
			return nil, errors.New("kind-qualified resource locator requires a kind and name")
		}
		if _, ok := syntheticEntry.SelectorForKindOk(locator.Kind); !ok {
			return nil, fmt.Errorf("resource locator has invalid kind %q", locator.Kind)
		}
	}
	// A web URL becomes an SSH URL. Reuse the form a configuration already
	// records for it, so install does not add a second source for the same
	// repository; otherwise fall back to HTTPS when SSH cannot reach the
	// registry, e.g. a public repository without SSH keys.
	choice := SelectAdHocTransport(ctx, ec.Merged, scopeToConfigSource(opts.Scope), rawFromURL, fromURL, fromPath, fromRef, RegistryURLPolicy(syntheticEntry, ec.Allowed))
	fromURL, probedBranch := choice.URL, choice.DefaultBranch
	if notice := choice.Notice("recorded"); notice != "" {
		defer func() {
			if result != nil && retErr == nil {
				result.TransportNotice = notice
			}
		}()
	}
	syntheticEntry.URL = fromURL
	if !syntheticEntry.IsLocal() && fromRef == "" {
		defaultBranch := probedBranch
		if defaultBranch == "" {
			var err error
			if defaultBranch, err = cache.DefaultBranch(fromURL); err != nil {
				return nil, err
			}
		}
		fromRef = defaultBranch
		syntheticEntry.Ref = fromRef
		slog.Info("install using registry default branch", "ref", fromRef)
	}

	// Repo-relative local registries are project-local and cannot be written to
	// the user-personal (global) config.
	if err := validateRegistryWriteScope(opts.Scope, syntheticEntry); err != nil {
		return nil, err
	}

	// For local filesystem registries, verify the path exists before writing
	// the registry entry to the config, so a typo fails fast instead of
	// leaving a broken entry behind.
	if syntheticEntry.IsLocal() {
		baseDir := repoRoot
		if opts.LocalConfigDir != "" {
			baseDir = opts.LocalConfigDir
		}
		localPath, err := syntheticEntry.ResolveLocalPath(baseDir)
		if err != nil {
			return nil, fmt.Errorf("resolve local registry path: %w", err)
		}
		bundlePath := syntheticEntry.BundlePath(localPath)
		if err := config.ValidateLocalRegistryPath(bundlePath); err != nil {
			slog.Warn("install rejected local registry path", "path", bundlePath, "error", err)
			return nil, err
		}
	}

	// Persist the registry entry to the target config scope.
	configDestination, err := ScopedConfigPath(repoRoot, opts.Scope)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	// Override with redirected local config path when set (monorepo subdir).
	if opts.LocalConfigDir != "" && opts.Scope == ScopeLocal {
		configDestination = config.LocalConfigPath(opts.LocalConfigDir)
	}
	configPath := configDestination
	if opts.configStaging != nil {
		configPath, err = opts.configStaging.Path(configDestination)
		if err != nil {
			return nil, err
		}
	}
	layer := config.LayerTarget{Source: scopeToConfigSource(opts.Scope), StagedPath: configPath, DeclaringPath: configDestination}
	var binding config.SourceBinding
	if err := EditLayerAtPath(layer, func(m *LayerMutation) error {
		var bindErr error
		binding, bindErr = m.EnsureRegistry(syntheticEntry)
		return bindErr
	}); err != nil {
		return nil, fmt.Errorf("ensure registry in %s config: %w", opts.Scope, err)
	}

	// Reload the effective config so the discovery helper sees the newly
	// persisted registry entry.
	var overlays map[string][]byte
	if opts.configStaging != nil {
		overlays, err = opts.configStaging.Overlays()
		if err != nil {
			return nil, err
		}
	}
	*ec = LoadEffective(ctx, ConfigLoadOnline, config.LoadEffectiveOptions{
		RepoRoot:       ws.RepoRoot(),
		LocalConfigDir: ws.LocalConfigDir(),
		FileOverlays:   overlays,
	}, nil)
	if len(ec.Errors) > 0 {
		return nil, errors.Join(ec.Errors...)
	}

	// Now load the full discovery config (which includes the new entry)
	// and catalogs.
	discoveryCfg, err := loadInstallDiscoveryConfig(ctx, opts, ec)
	if err != nil {
		return nil, err
	}
	var discoveryOptions []catalog.LoadOption
	if opts.PrepareDependencies != nil && !syntheticEntry.IsLocal() {
		prepared, err := prepareInstallDiscovery(ctx, &opts)
		if err != nil {
			return nil, err
		}
		defer prepared.Release()
		if opts.ExpectedCommit != "" {
			if prepared.ResolvedCommit == nil {
				return nil, errors.New("expected commit cannot be checked because exact source commit was not prepared")
			}
			actual, err := prepared.ResolvedCommit(syntheticEntry)
			if err != nil {
				return nil, fmt.Errorf("resolve prepared source commit: %w", err)
			}
			if actual.Commit != opts.ExpectedCommit {
				if actual.Locked {
					return nil, &SourceLockedError{URL: binding.Entry.URL, Ref: fromRef, Scope: opts.Scope, Locked: actual.Commit, Expected: opts.ExpectedCommit}
				}
				return nil, &SourceMovedError{Ref: fromRef, Expected: opts.ExpectedCommit, Actual: actual.Commit}
			}
		}
		ctx = prepared.Context
		discoveryCfg = prepared.DiscoveryConfig
		discoveryOptions = append(discoveryOptions, prepared.View.CatalogOption)
	}
	catalogs, discoveredCollections, err := loadCatalogsForInstall(ctx, discoveryCfg, opts.RepoRoot, opts.LocalConfigDir, discoveryOptions...)
	if err != nil {
		return nil, err
	}

	// Find the catalog key for the --from entry. CatalogKey is populated
	// by catalog.LoadRegistries during the load step above, so we need to
	// look it up in the loaded discovery config.
	catalogKeys := map[string]bool{}
	for i := range discoveryCfg.Registries {
		entry := &discoveryCfg.Registries[i]
		if entry.Source != layer.Source || !adHocEntryMatchesBinding(*entry, binding.Entry) {
			continue
		}
		catalogKeys[entry.CatalogKey] = true
	}
	if len(catalogKeys) != 1 {
		return nil, fmt.Errorf("registry %s (ref %s, path %s) did not resolve to exactly one catalog in %s config: %d matches", fromURL, fromRef, fromPath, opts.Scope, len(catalogKeys))
	}
	var catalogKey string
	for key := range catalogKeys {
		catalogKey = key
	}
	if catalogKey == "" {
		return nil, fmt.Errorf("no catalog found for registry %s; check that the URL and ref are correct", fromURL)
	}
	selectedCatalogs := map[string]catalog.LoadedCatalog{catalogKey: catalogs[catalogKey]}
	addSelectedResource := func(resourceKind ir.ResourceKind, resourceName string) error {
		return EditLayerAtPath(layer, func(m *LayerMutation) error {
			bound, err := m.EnsureRegistry(syntheticEntry)
			if err != nil {
				return err
			}
			return m.Apply(bound, config.SelectionChange{Operation: config.SelectionInclude, Locators: []config.ResourceLocator{{Kind: resourceKind, Name: resourceName}}})
		})
	}

	if len(opts.Args) > 0 || len(opts.Locators) > 0 {
		selectedCollections := make([]collection.Collection, 0)
		for _, col := range discoveredCollections {
			if col.SourceCatalogKey == catalogKey {
				selectedCollections = append(selectedCollections, col)
			}
		}
		source := &namedInstallSource{apply: syntheticEntry, excludes: binding.Entry, fromURL: fromURL}
		var plan []namedInstallSelection
		var warnIdentities []resourceIdentity
		if len(opts.Locators) > 0 {
			for _, locator := range opts.Locators {
				locatorPlan, locatorWarnings, err := planNamedInstalls([]string{locator.Name}, selectedCatalogs, selectedCollections, discoveredCollections, discoveryCfg, locator.Kind, true, opts.Scope, source)
				if err != nil {
					return nil, err
				}
				plan = append(plan, locatorPlan...)
				warnIdentities = append(warnIdentities, locatorWarnings...)
			}
		} else {
			plan, warnIdentities, err = planNamedInstalls(opts.Args, selectedCatalogs, selectedCollections, discoveredCollections, discoveryCfg, kind, hasKind, opts.Scope, source)
			if err != nil {
				return nil, err
			}
		}
		if err := applyNamedInstalls(func(mutate func(*LayerMutation) error) error {
			return EditLayerAtPath(layer, mutate)
		}, plan, nil); err != nil {
			return nil, err
		}
		if err := clearNamedInstallExcludes(repoRoot, plan, opts.configStaging); err != nil {
			return nil, err
		}
		return syncNamedInstallResult(ctx, opts, plan, warnIdentities, silentSyncOptsForInstall(opts)...)
	}

	// No name arg: try to infer a single resource name from the URL path.
	// When the user pastes a URL like .../-/tree/main/skills/helm-chart-mirroring,
	// the last path component is likely the resource they want.
	// If it matches exactly one resource in the catalog, install just that one.
	// Otherwise fall back to bundle mode (install all).
	inferredName := ""
	if resolvedPath != "" {
		inferredName = pathBase(resolvedPath)
	}

	if inferredName != "" {
		// Try to find the inferred name in the catalog.
		if _, ok := selectedCatalogs[catalogKey]; ok {
			if malformedErr := malformedSkillInstallError(selectedCatalogs, inferredName, malformedKindFilter(kind, hasKind)); malformedErr != nil {
				return nil, malformedErr
			}
			match, err := findCatalogResource(selectedCatalogs, inferredName, kind, hasKind)
			if err != nil {
				return nil, err
			}
			if match != nil {
				if err := addSelectedResource(match.kind, inferredName); err != nil {
					return nil, err
				}
				if err := clearInstallExcludes(repoRoot, binding.Entry, match.kind, inferredName, opts.configStaging); err != nil {
					return nil, err
				}
				return syncInstalledResources(ctx, opts, []string{inferredName}, []resourceIdentity{{kind: match.kind, name: inferredName}}, silentSyncOptsForInstall(opts)...)
			}
		}
		// Inferred name not found — fall through to bundle mode.
	}

	// Bundle mode: no specific resource targeted. Discover resources first,
	// then set default:true only after confirming there are installable
	// entries — avoids mutating config on failure.
	catalogForEntry, ok := catalogs[catalogKey]
	if !ok {
		return nil, fmt.Errorf("no catalog found for registry %s; check that the URL and ref are correct", fromURL)
	}

	var allInstalled []string
	var installedResources []resourceIdentity
	for _, discovered := range catalogForEntry.Entries {
		if hasKind && discovered.Kind != kind {
			continue
		}
		name := strings.TrimSpace(discovered.Name)
		if name == "" {
			continue
		}
		allInstalled = append(allInstalled, name)
		installedResources = append(installedResources, resourceIdentity{kind: discovered.Kind, name: name})
	}
	if len(allInstalled) == 0 {
		if hasKind {
			return nil, fmt.Errorf("no %ss found in registry %s", skill.KindLabel(kind), fromURL)
		}
		return nil, fmt.Errorf("no resources found in registry %s", fromURL)
	}
	slices.Sort(allInstalled)

	if err := EditLayerAtPath(layer, func(m *LayerMutation) error {
		bound, err := m.EnsureRegistry(syntheticEntry)
		if err != nil {
			return err
		}
		return m.Apply(bound, config.SelectionChange{Operation: config.SelectionSelectAll})
	}); err != nil {
		return nil, fmt.Errorf("set default on registry in %s config: %w", opts.Scope, err)
	}
	installedResources, err = expandInstalledCollectionResources(installedResources, discoveredCollections, discoveryCfg.Registries, opts.Scope)
	if err != nil {
		return nil, err
	}
	return syncInstalledResources(ctx, opts, allInstalled, installedResources, silentSyncOptsForInstall(opts)...)
}

// SourceMovedError reports that an explicit source no longer matches its previewed commit.
type SourceMovedError struct {
	Ref      string
	Expected string
	Actual   string
}

func (e *SourceMovedError) Error() string {
	return fmt.Sprintf("source ref %q moved since preview: expected commit %s, actual commit %s", e.Ref, e.Expected, e.Actual)
}

// SourceLockedError reports that the target layer's lock keeps an explicit source at a commit other than the previewed one.
type SourceLockedError struct {
	URL      string
	Ref      string
	Scope    Scope
	Locked   string
	Expected string
}

// UpdateCommand returns the update command that advances the locked source in its scope.
func (e *SourceLockedError) UpdateCommand() string {
	command := "agent-marketplace update --registry " + e.URL
	if e.Ref != "" {
		command += " --ref " + e.Ref
	}
	return command + " --scope " + string(e.Scope)
}

func (e *SourceLockedError) Error() string {
	return fmt.Sprintf("%s lock pins source ref %q at commit %s, not the expected commit %s; run %q to advance it", e.Scope, e.Ref, e.Locked, e.Expected, e.UpdateCommand())
}

func adHocEntryMatchesBinding(candidate, bound config.RegistryEntry) bool {
	candidateLocal, boundLocal := candidate.IsLocal(), bound.IsLocal()
	if candidateLocal != boundLocal {
		return false
	}
	if boundLocal {
		return candidate.CatalogKey != "" && filepath.Clean(candidate.CatalogKey) == filepath.Clean(bound.URL)
	}
	return config.NormalizeGitURL(candidate.URL) == config.NormalizeGitURL(bound.URL) &&
		strings.TrimSpace(candidate.Ref) == strings.TrimSpace(bound.Ref) &&
		config.NormalizeRegistryPath(candidate.Path) == config.NormalizeRegistryPath(bound.Path)
}

// noResourcesError is returned by allDiscoveredResourceNames when no
// resources of the requested kind are found. In multi-kind install mode,
// these errors are expected and skipped silently for kinds that have no
// resources in the registry.
type noResourcesError struct {
	kind     ir.ResourceKind
	registry string // empty when no --registry filter
}

func (e noResourcesError) Error() string {
	if e.registry != "" {
		return fmt.Sprintf("no %ss found in registry %q", skill.KindLabel(e.kind), e.registry)
	}
	return fmt.Sprintf("no %ss found in configured registries", skill.KindLabel(e.kind))
}

// isNoResourcesError reports whether err is a "no resources of this kind
// found" error from allDiscoveredResourceNames. These are expected when
// iterating over all kinds in bundle mode and should be skipped silently.
func isNoResourcesError(err error) bool {
	var nr noResourcesError
	return errors.As(err, &nr)
}
