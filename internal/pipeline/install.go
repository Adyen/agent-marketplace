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

	"gopkg.in/yaml.v3"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
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
	Release         func()
}

type InstallDependencyPreparer func(context.Context, string, string, []StagedConfigEdit) (PreparedInstallDependencies, error)

type LegacyMarketplacePreview struct {
	Entry config.MarketplaceEntry
	Pin   config.MarketplaceResourcePin
}

// InstallOptions holds the arguments for an install operation.
type InstallOptions struct {
	RepoRoot                string
	Args                    []string // resource name args (0 or 1)
	Tag                     string   // --tag flag
	InstallAll              bool     // --all flag
	Registry                string   // --registry filter for --all
	Kind                    ir.ResourceKind
	HasKind                 bool
	Scope                   Scope
	ScopeSpecified          bool
	PreserveScopeRoot       bool
	FromURL                 string // --from flag: ad-hoc registry URL
	FromRef                 string // --ref flag: git ref for --from
	FromPath                string // --path flag: subdirectory for --from
	InstalledName           string // --as for Marketplace installs
	AcceptRisk              bool   // --accept-risk/--yes for Marketplace installs
	Marketplace             string // --marketplace alias or endpoint selector
	ExactMarketplaceLocator *marketplace.MarketplaceLocator
	ReacquireLegacyPin      *LegacyMarketplacePreview
	DeferMarketplaceSync    bool
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
}

// silentSyncOptsForInstall builds silentSync options from InstallOptions.
// When LocalConfigDir is set (monorepo subdir with --scope local), it
// passes both MaterializeRoot and LocalConfigDir so that materialization
// happens in the subdirectory and local entries' relative paths resolve
// against it.
func silentSyncOptsForInstall(opts InstallOptions) []silentSyncOpt {
	syncOpts := silentSyncOptsForPaths(opts.RepoRoot, opts.LocalConfigDir, opts.LocalConfigDir)
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
	Installed                    []string
	IsCollection                 bool
	CollectionLabel              string
	RequirementWarnings          []error
	MarketplacePreview           string
	MarketplaceSummary           MarketplaceInstallSummary
	MarketplaceEndpoint          string
	MarketplaceConfigPath        string
	MarketplaceAlreadyConfigured bool
	PendingSync                  bool
}

type resourceIdentity struct {
	kind ir.ResourceKind
	name string
}

// Install adds resources to the appropriate config layer and re-syncs.
// It handles --tag, --all, --from, collection, and single-resource installs.
func Install(ctx context.Context, opts InstallOptions) (result *InstallResult, retErr error) {
	ctx = WithTransientOrganizationMarketplaceLockCache(ctx)
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
				if marketplaceMutationRequested {
					repoRoot = filepath.Clean(cwd)
					configPath = config.ProjectConfigPath(repoRoot)
					opts.RepoRoot = repoRoot
					slog.Info("install: redirecting Marketplace install to cwd without creating a config before preflight", "original_repo_root", originalRepoRoot, "cwd", filepath.Clean(cwd), "install_target", configPath)
					break
				}
				if opts.DependencyReconcile == nil {
					if err := os.MkdirAll(filepath.Dir(cwdProjectConfig), 0755); err != nil {
						return nil, fmt.Errorf("create .agents directory: %w", err)
					}
					skeleton := config.Config{APIVersion: config.APIVersionV2}
					data, err := yaml.Marshal(&skeleton)
					if err != nil {
						return nil, fmt.Errorf("marshal skeleton config: %w", err)
					}
					if err := os.WriteFile(cwdProjectConfig, data, 0644); err != nil {
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
	if opts.DependencyReconcile != nil && !marketplaceEndpointRequested {
		opts.configStaging, err = newConfigStagingManager()
		if err != nil {
			return nil, err
		}
		defer closeConfigStaging(opts.configStaging, "install", &retErr)
		configPath, err = opts.configStaging.Path(configPath)
		if err != nil {
			return nil, err
		}
	}

	// Load the effective configuration once to validate config versions and resolve
	// the registry allowlist before mutating any config files.
	ec := LoadEffectiveOnline(ctx, config.LoadEffectiveOptions{
		RepoRoot:       repoRoot,
		LocalConfigDir: opts.LocalConfigDir,
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

	targetSource := scopeToConfigSource(opts.Scope)
	v4Target, err := config.LoadV4ConfigIfPresent(configDestination, targetSource)
	if err != nil {
		return nil, err
	}
	repoCfg := ec.Repo.Config

	// If repo config doesn't exist (and we're writing to repo scope), create skeleton
	if repoCfg == nil && opts.Scope == ScopeRepo {
		repoConfigPath := configPath
		if err := os.MkdirAll(filepath.Dir(repoConfigPath), 0755); err != nil {
			return nil, fmt.Errorf("create .agents directory: %w", err)
		}
		skeleton := config.Config{
			APIVersion: config.APIVersionV2,
			// Harnesses intentionally omitted: project configs inherit
			// from the org/global layer during merge (per AGENTS.md).
		}
		data, err := yaml.Marshal(&skeleton)
		if err != nil {
			return nil, fmt.Errorf("marshal skeleton config: %w", err)
		}
		if err := os.WriteFile(repoConfigPath, data, 0644); err != nil {
			return nil, fmt.Errorf("create skeleton config: %w", err)
		}

		// The discovery helper needs the newly created repo layer to be
		// reflected in the effective config.
		var overlays map[string][]byte
		if opts.configStaging != nil {
			overlays, err = opts.configStaging.Overlays()
			if err != nil {
				return nil, err
			}
		}
		ec = LoadEffectiveOnline(ctx, config.LoadEffectiveOptions{
			RepoRoot:       repoRoot,
			LocalConfigDir: opts.LocalConfigDir,
			FileOverlays:   overlays,
		}, nil)
		if len(ec.Errors) > 0 {
			return nil, errors.Join(ec.Errors...)
		}
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
	var ensureV4Source func(config.RegistryEntry) (string, error)
	var addV4Resources func(config.RegistryEntry, ir.ResourceKind, []string) error
	if v4Target != nil {
		localBase := repoRoot
		if opts.LocalConfigDir != "" {
			localBase = opts.LocalConfigDir
		}
		ensureV4Source = func(entry config.RegistryEntry) (string, error) {
			binding, err := config.EnsureV4RegistrySourceInConfig(configPath, configDestination, targetSource, entry, localBase)
			return binding.Name, err
		}
		addV4Resources = func(entry config.RegistryEntry, kind ir.ResourceKind, names []string) error {
			if len(names) == 1 {
				handled, err := config.EditV4ResourceSelection(configPath, configDestination, entry, kind, names[0], config.V4SelectionEnable)
				if err != nil {
					return err
				}
				if !handled {
					return fmt.Errorf("v4 selection editor did not handle %s", configDestination)
				}
				return nil
			}
			sourceName, err := ensureV4Source(entry)
			if err != nil {
				return err
			}
			return config.AddV4ResourcesToSourceInConfig(configPath, configDestination, targetSource, sourceName, kind, names)
		}
	}
	allEntries := discoveryCfg.Registries
	if opts.Registry != "" {
		allEntries = nil
		for _, entry := range discoveryCfg.Registries {
			if strings.TrimSpace(entry.URL) == strings.TrimSpace(opts.Registry) {
				allEntries = append(allEntries, entry)
			}
		}
	}
	if trimmedTag := strings.TrimSpace(opts.Tag); trimmedTag != "" {
		if opts.Scope != ScopeRepo {
			return nil, fmt.Errorf("--tag is only supported with --scope repo (or no --scope)")
		}
		if hasKind {
			installed = matchingResourceNamesByTag(catalogs, trimmedTag, kind)
			if len(installed) == 0 {
				return nil, fmt.Errorf("tag %q not found for %s resources in configured registries", trimmedTag, skill.KindLabel(kind))
			}
			if err := addResourcesToAllRegistries(configPath, allEntries, catalogs, kind, installed, opts.bindRegistry, addV4Resources); err != nil {
				return nil, err
			}
			installedResources = resourceIdentities(kind, installed)
		} else {
			installed = matchingResourceNamesByTag(catalogs, trimmedTag, ir.KindSkill)
			if len(installed) == 0 {
				return nil, fmt.Errorf("tag %q not found in configured registries", trimmedTag)
			}
			if v4Target != nil {
				for _, entry := range discoveryCfg.Registries {
					if _, ok := catalogs[entry.CatalogKey]; !ok {
						continue
					}
					if opts.bindRegistry != nil {
						if err := opts.bindRegistry(entry); err != nil {
							return nil, err
						}
					}
					sourceName, err := ensureV4Source(entry)
					if err != nil {
						return nil, err
					}
					if err := config.AddV4TagToSourceInConfig(configPath, configDestination, targetSource, sourceName, trimmedTag); err != nil {
						return nil, err
					}
				}
			} else if err := config.AddTagToConfig(configPath, trimmedTag); err != nil {
				return nil, err
			}
			installedResources = resourceIdentities(ir.KindSkill, installed)
		}
	} else if opts.InstallAll {
		if opts.Scope != ScopeRepo {
			return nil, fmt.Errorf("--all is only supported with --scope repo (or no --scope)")
		}
		if hasKind {
			// Filter to the specified kind only.
			installed, err = allDiscoveredResourceNames(discoveryCfg.Registries, catalogs, opts.Registry, kind)
			if err != nil {
				return nil, err
			}
			if opts.Registry != "" && v4Target == nil {
				if err := config.AddResourcesToRegistryInConfig(configPath, opts.Registry, "", kind, installed); err != nil {
					return nil, err
				}
			} else {
				if err := addResourcesToAllRegistries(configPath, allEntries, catalogs, kind, installed, opts.bindRegistry, addV4Resources); err != nil {
					return nil, err
				}
			}
			installedResources = resourceIdentities(kind, installed)
		} else {
			// No --kind specified: install all resource kinds.
			var allInstalled []string
			for _, k := range config.AllKinds {
				names, err := allDiscoveredResourceNames(discoveryCfg.Registries, catalogs, opts.Registry, k)
				if err != nil {
					// If no resources of this kind exist, skip silently.
					// Propagate all other errors (e.g. collisions).
					if isNoResourcesError(err) {
						continue
					}
					return nil, err
				}
				if opts.Registry != "" && v4Target == nil {
					if err := config.AddResourcesToRegistryInConfig(configPath, opts.Registry, "", k, names); err != nil {
						return nil, err
					}
				} else {
					if err := addResourcesToAllRegistries(configPath, allEntries, catalogs, k, names, opts.bindRegistry, addV4Resources); err != nil {
						return nil, err
					}
				}
				allInstalled = append(allInstalled, names...)
				installedResources = append(installedResources, resourceIdentities(k, names)...)
			}
			if len(allInstalled) == 0 {
				return nil, fmt.Errorf("no resources found in configured registries")
			}
			slices.Sort(allInstalled)
			installed = allInstalled
		}
	} else {
		if len(opts.Args) == 0 {
			return nil, fmt.Errorf("resource name is required when not using --tag or --all")
		}
		arg := strings.TrimSpace(opts.Args[0])
		// Detect collection install.
		if col := collection.Find(discoveredCollections, arg); col != nil {
			collectionResources, err := collectionInstallResources(col.Name, discoveredCollections, discoveryCfg.Registries, opts.Scope)
			if err != nil {
				return nil, err
			}
			if v4Target != nil {
				var provider *config.RegistryEntry
				for i := range discoveryCfg.Registries {
					if discoveryCfg.Registries[i].CatalogKey == col.SourceCatalogKey {
						provider = &discoveryCfg.Registries[i]
						break
					}
				}
				if provider == nil {
					return nil, fmt.Errorf("collection %q has no providing registry source", col.Name)
				}
				if opts.bindRegistry != nil {
					if err := opts.bindRegistry(*provider); err != nil {
						return nil, err
					}
				}
				if err := addV4Resources(*provider, ir.KindCollection, []string{col.Name}); err != nil {
					return nil, err
				}
			} else if err := config.AddCollectionToConfig(configPath, col.Name); err != nil {
				return nil, err
			}
			requirementWarnings, err := syncAfterInstall(ctx, repoRoot, collectionResources, silentSyncOptsForInstall(opts)...)
			if err != nil {
				return nil, err
			}
			return &InstallResult{
				IsCollection:        true,
				CollectionLabel:     col.Label(),
				RequirementWarnings: requirementWarnings,
			}, nil
		}

		resourceName := arg
		if malformedErr := malformedSkillInstallError(catalogs, resourceName); malformedErr != nil {
			return nil, malformedErr
		}
		match, err := findCatalogResource(catalogs, resourceName, kind, hasKind)
		if err != nil {
			return nil, err
		}
		if match == nil {
			return nil, fmt.Errorf("resource or collection %q not found in configured registries", resourceName)
		}

		installed = []string{resourceName}
		installedResources = []resourceIdentity{{kind: match.kind, name: resourceName}}
		entry := findProvidingEntry(discoveryCfg, catalogs, match.kind, resourceName)
		if entry == nil {
			// The resource was found in a catalog but no providing registry
			// entry could be identified (should not normally happen). For local
			// scope, fall back to a top-level include — the resolver maps it to
			// the providing registry — rather than failing the install.
			if opts.Scope == ScopeLocal {
				if err := config.AddResourceToConfig(configPath, match.kind, resourceName); err != nil {
					return nil, err
				}
			} else {
				return nil, fmt.Errorf("%s %q was found but no providing registry entry could be identified", skill.KindLabel(match.kind), resourceName)
			}
		} else {
			// Repo-relative local registries are project-local and cannot be
			// shadowed into the user-personal (global) config.
			if err := validateRegistryWriteScope(opts.Scope, *entry); err != nil {
				return nil, err
			}
			// Always write the include scoped to the providing registry entry,
			// creating a shadow entry in the target scope's config when the
			// registry is only defined at a lower layer. Keeping every selection
			// registry-scoped (instead of a top-level skills.include) gives one
			// consistent representation across scopes and avoids the
			// scoped/non-scoped mix users found confusing.
			if opts.bindRegistry != nil {
				if err := opts.bindRegistry(*entry); err != nil {
					return nil, err
				}
			}
			if v4Target != nil {
				if err := addV4Resources(*entry, match.kind, []string{resourceName}); err != nil {
					return nil, err
				}
			} else {
				if err := config.EnsureRegistryWithPathsInConfig(configPath, entry.URL, entry.Ref, entry.Path); err != nil {
					return nil, fmt.Errorf("ensure registry in %s config: %w", opts.Scope, err)
				}
				// Use exact-entry matching (URL+path, where empty path = root entry
				// specifically) to avoid "matches multiple entries" errors when the
				// same URL has entries at different paths. AddResourceToExactRegistryEntry
				// also clears any contradictory exclude_all on the entry.
				if err := config.AddResourceToExactRegistryEntry(configPath, entry.URL, entry.Path, match.kind, resourceName); err != nil {
					return nil, err
				}
			}
		}
		if v4Target == nil {
			if err := clearInstallExcludes(repoRoot, opts.Scope, match.kind, resourceName, opts.configStaging); err != nil {
				return nil, err
			}
		}
	}

	installedResources, err = expandInstalledCollectionResources(installedResources, discoveredCollections, discoveryCfg.Registries, opts.Scope)
	if err != nil {
		return nil, err
	}
	requirementWarnings, err := syncAfterInstall(ctx, repoRoot, installedResources, silentSyncOptsForInstall(opts)...)
	if err != nil {
		return nil, err
	}

	return &InstallResult{
		Installed:           installed,
		RequirementWarnings: requirementWarnings,
	}, nil
}

func resourceIdentities(kind ir.ResourceKind, names []string) []resourceIdentity {
	resources := make([]resourceIdentity, 0, len(names))
	for _, name := range names {
		resources = append(resources, resourceIdentity{kind: kind, name: strings.TrimSpace(name)})
	}
	return resources
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

func syncAfterInstall(ctx context.Context, repoRoot string, installed []resourceIdentity, opts ...silentSyncOpt) ([]error, error) {
	warnings, err := silentSyncCollect(ctx, repoRoot, opts...)
	if err != nil {
		return nil, err
	}
	return filterInstallWarnings(warnings, installed), nil
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
		ec = new(config.EffectiveConfig)
		*ec = LoadEffectiveOnline(ctx, config.LoadEffectiveOptions{
			RepoRoot:       opts.RepoRoot,
			LocalConfigDir: opts.LocalConfigDir,
		}, nil)
	}
	if len(ec.Errors) > 0 {
		return nil, errors.Join(ec.Errors...)
	}

	// config.LoadEffective already merged the layers, stamped registry
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

func clearInstallExcludes(repoRoot string, scope Scope, kind ir.ResourceKind, name string, staging *configStagingManager) error {
	stagedPath := func(path string) (string, error) {
		if staging == nil {
			return path, nil
		}
		return staging.Path(path)
	}

	localPath, err := stagedPath(config.LocalConfigPath(repoRoot))
	if err != nil {
		return err
	}
	if err := config.RemoveKindExcludeFromConfig(localPath, kind, name); err != nil {
		return err
	}

	var path string
	switch scope {
	case ScopeUser:
		path, err = config.GlobalConfigPath()
	case ScopeRepo:
		path = config.ProjectConfigPath(repoRoot)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	path, err = stagedPath(path)
	if err != nil {
		return err
	}
	return config.RemoveAllKindExcludesFromRegistryEntries(path, kind, name)
}

func allDiscoveredResourceNames(registries []config.RegistryEntry, catalogs map[string]catalog.LoadedCatalog, registryFilter string, kind ir.ResourceKind) ([]string, error) {
	registryFilter = strings.TrimSpace(registryFilter)

	selectedEntries := make([]config.RegistryEntry, 0, len(registries))
	for _, entry := range registries {
		if registryFilter != "" && strings.TrimSpace(entry.URL) != registryFilter {
			continue
		}
		selectedEntries = append(selectedEntries, entry)
	}

	switch {
	case registryFilter != "" && len(selectedEntries) == 0:
		return nil, fmt.Errorf("registry %q not found in repo config", registryFilter)
	case registryFilter != "" && len(selectedEntries) > 1:
		return nil, fmt.Errorf("registry %q matches multiple configured entries; rerun without --registry or keep only one matching repo registry entry", registryFilter)
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

func addResourcesToAllRegistries(configPath string, entries []config.RegistryEntry, catalogs map[string]catalog.LoadedCatalog, kind ir.ResourceKind, names []string, bind func(config.RegistryEntry) error, v4Add func(config.RegistryEntry, ir.ResourceKind, []string) error) error {
	for i := range entries {
		entry := &entries[i]
		catalogForEntry, ok := catalogs[entry.CatalogKey]
		if !ok {
			continue
		}
		var matching []string
		for _, name := range names {
			for _, s := range catalogForEntry.Entries {
				if s.Kind == kind && strings.TrimSpace(s.Name) == strings.TrimSpace(name) {
					matching = append(matching, name)
					break
				}
			}
		}
		if len(matching) == 0 {
			continue
		}
		if bind != nil {
			if err := bind(*entry); err != nil {
				return err
			}
		}
		if v4Add != nil {
			if err := v4Add(*entry, kind, matching); err != nil {
				return fmt.Errorf("add %s to v4 source %s: %w", skill.KindLabel(kind), entry.URL, err)
			}
			continue
		}
		if err := config.EnsureRegistryWithPathsInConfig(configPath, entry.URL, entry.Ref, entry.Path); err != nil {
			return fmt.Errorf("ensure registry %s in config: %w", entry.URL, err)
		}
		if err := config.AddResourcesToRegistryInConfig(configPath, entry.URL, entry.Path, kind, matching); err != nil {
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
func malformedSkillInstallError(catalogs map[string]catalog.LoadedCatalog, wanted string) error {
	wanted = strings.TrimSpace(wanted)
	if wanted == "" {
		return nil
	}

	for _, catalogForEntry := range catalogs {
		if match, found := skill.MatchMalformed(catalogForEntry.Malformed, wanted); found {
			return fmt.Errorf("skill %q cannot be installed: %s: %w", wanted, skill.ParseFailure(match), match.Error)
		}
	}

	return nil
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
	if ec == nil {
		ec = new(config.EffectiveConfig)
		*ec = LoadEffectiveOnline(ctx, config.LoadEffectiveOptions{
			RepoRoot:       opts.RepoRoot,
			LocalConfigDir: opts.LocalConfigDir,
		}, nil)
	}
	if len(ec.Errors) > 0 {
		return nil, errors.Join(ec.Errors...)
	}

	repoRoot := opts.RepoRoot

	// Resolve the URL: parse web URLs (GitLab/GitHub) into git URL + ref + path.
	// Explicit --ref/--path flags take precedence over values parsed from the URL.
	resolvedURL, resolvedRef, resolvedPath, _ := ResolveFromURL(fromURL, opts.FromRef, opts.FromPath)
	fromURL = resolvedURL
	fromRef := resolvedRef
	fromPath := resolvedPath

	// Validate the --from URL against the effective allowlist before
	// writing to config or fetching anything.
	syntheticEntry := config.RegistryEntry{URL: fromURL, Ref: fromRef, Path: fromPath}
	if err := config.ValidateRegistryURLs([]config.RegistryEntry{syntheticEntry}, ec.Allowed); err != nil {
		return nil, err
	}
	if err := config.ValidateRegistryPath(syntheticEntry.Path); err != nil {
		slog.Warn("install rejected invalid registry path", "error", err)
		return nil, err
	}
	if !syntheticEntry.IsLocal() && fromRef == "" {
		defaultBranch, err := cache.DefaultBranch(fromURL)
		if err != nil {
			return nil, err
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
	localBase := repoRoot
	if opts.LocalConfigDir != "" {
		localBase = opts.LocalConfigDir
	}
	if syntheticEntry.IsLocal() {
		localPath, err := syntheticEntry.ResolveLocalPath(localBase)
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
	configPath, err := ScopedConfigPath(repoRoot, opts.Scope)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	// Override with redirected local config path when set (monorepo subdir).
	if opts.LocalConfigDir != "" && opts.Scope == ScopeLocal {
		configPath = config.LocalConfigPath(opts.LocalConfigDir)
	}
	configDestination := configPath
	if opts.configStaging != nil {
		configPath, err = opts.configStaging.Path(configPath)
		if err != nil {
			return nil, err
		}
	}
	source := scopeToConfigSource(opts.Scope)
	v4, err := config.LoadV4ConfigIfPresent(configDestination, source)
	if err != nil {
		return nil, err
	}
	v4Source := ""
	lookupEntry := syntheticEntry
	if v4 != nil {
		binding, err := config.EnsureV4RegistrySourceInConfig(configPath, configDestination, source, syntheticEntry, localBase)
		if err != nil {
			return nil, fmt.Errorf("ensure v4 registry source in %s config: %w", opts.Scope, err)
		}
		v4Source, lookupEntry = binding.Name, binding.Entry
	} else {
		ensureRegistry := config.EnsureRegistryWithPathsInConfig
		if fromRef != "" {
			ensureRegistry = config.EnsureRegistryWithPathsAtRefInConfig
		}
		if err := ensureRegistry(configPath, fromURL, fromRef, fromPath); err != nil {
			return nil, fmt.Errorf("ensure registry in %s config: %w", opts.Scope, err)
		}
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
	*ec = LoadEffectiveOnline(ctx, config.LoadEffectiveOptions{
		RepoRoot:       repoRoot,
		LocalConfigDir: opts.LocalConfigDir,
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
	var catalogKey string
	for i := range discoveryCfg.Registries {
		entry := &discoveryCfg.Registries[i]
		if strings.TrimSpace(entry.URL) != lookupEntry.URL || config.NormalizeRegistryPath(entry.Path) != config.NormalizeRegistryPath(lookupEntry.Path) {
			continue
		}
		if lookupEntry.Ref != "" && strings.TrimSpace(entry.Ref) != lookupEntry.Ref {
			continue
		}
		catalogKey = entry.CatalogKey
		break
	}
	if catalogKey == "" {
		return nil, fmt.Errorf("no catalog found for registry %s; check that the URL and ref are correct", fromURL)
	}
	selectedCatalogs := map[string]catalog.LoadedCatalog{catalogKey: catalogs[catalogKey]}
	addSelectedResource := func(resourceKind ir.ResourceKind, resourceName string) error {
		if v4Source != "" {
			handled, err := config.EditV4ResourceSelection(configPath, configDestination, lookupEntry, resourceKind, resourceName, config.V4SelectionEnable)
			if err != nil {
				return err
			}
			if !handled {
				return fmt.Errorf("v4 selection editor did not handle %s", configDestination)
			}
			return nil
		}
		if fromRef != "" {
			return config.AddResourceToExactRegistryEntryAtRef(configPath, fromURL, fromPath, fromRef, resourceKind, resourceName)
		}
		return config.AddResourceToExactRegistryEntry(configPath, fromURL, fromPath, resourceKind, resourceName)
	}
	clearSelectedExcludes := func(resourceKind ir.ResourceKind, resourceName string) error {
		if v4Source != "" {
			return nil
		}
		return clearInstallExcludes(repoRoot, opts.Scope, resourceKind, resourceName, opts.configStaging)
	}

	// If a name arg is given, install just that resource.
	if len(opts.Args) > 0 {
		resourceName := strings.TrimSpace(opts.Args[0])

		// Check collections.
		selectedCollections := make([]collection.Collection, 0)
		for _, col := range discoveredCollections {
			if col.SourceCatalogKey == catalogKey {
				selectedCollections = append(selectedCollections, col)
			}
		}
		if col := collection.Find(selectedCollections, resourceName); col != nil {
			collectionResources, err := collectionInstallResources(col.Name, discoveredCollections, discoveryCfg.Registries, opts.Scope)
			if err != nil {
				return nil, err
			}
			if err := addSelectedResource(ir.KindCollection, col.Name); err != nil {
				return nil, err
			}
			requirementWarnings, err := syncAfterInstall(ctx, repoRoot, collectionResources, silentSyncOptsForInstall(opts)...)
			if err != nil {
				return nil, err
			}
			return &InstallResult{
				IsCollection:        true,
				CollectionLabel:     col.Label(),
				RequirementWarnings: requirementWarnings,
			}, nil
		}

		if malformedErr := malformedSkillInstallError(selectedCatalogs, resourceName); malformedErr != nil {
			return nil, malformedErr
		}
		match, err := findCatalogResource(selectedCatalogs, resourceName, kind, hasKind)
		if err != nil {
			return nil, err
		}
		if match == nil {
			return nil, fmt.Errorf("resource or collection %q not found in registry %s", resourceName, fromURL)
		}

		if err := addSelectedResource(match.kind, resourceName); err != nil {
			return nil, err
		}
		if err := clearSelectedExcludes(match.kind, resourceName); err != nil {
			return nil, err
		}
		requirementWarnings, err := syncAfterInstall(ctx, repoRoot, []resourceIdentity{{kind: match.kind, name: resourceName}}, silentSyncOptsForInstall(opts)...)
		if err != nil {
			return nil, err
		}
		return &InstallResult{
			Installed:           []string{resourceName},
			RequirementWarnings: requirementWarnings,
		}, nil
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
			if malformedErr := malformedSkillInstallError(selectedCatalogs, inferredName); malformedErr != nil {
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
				if err := clearSelectedExcludes(match.kind, inferredName); err != nil {
					return nil, err
				}
				requirementWarnings, err := syncAfterInstall(ctx, repoRoot, []resourceIdentity{{kind: match.kind, name: inferredName}}, silentSyncOptsForInstall(opts)...)
				if err != nil {
					return nil, err
				}
				return &InstallResult{
					Installed:           []string{inferredName},
					RequirementWarnings: requirementWarnings,
				}, nil
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

	if v4Source != "" {
		if err := config.SelectAllV4SourceInConfig(configPath, configDestination, source, v4Source); err != nil {
			return nil, fmt.Errorf("select all resources from v4 source in %s config: %w", opts.Scope, err)
		}
	} else if err := config.EnsureRegistryWithDefaultInConfig(configPath, fromURL, fromRef, fromPath); err != nil {
		return nil, fmt.Errorf("set default on registry in %s config: %w", opts.Scope, err)
	}
	installedResources, err = expandInstalledCollectionResources(installedResources, discoveredCollections, discoveryCfg.Registries, opts.Scope)
	if err != nil {
		return nil, err
	}
	requirementWarnings, err := syncAfterInstall(ctx, repoRoot, installedResources, silentSyncOptsForInstall(opts)...)
	if err != nil {
		return nil, err
	}
	return &InstallResult{
		Installed:           allInstalled,
		RequirementWarnings: requirementWarnings,
	}, nil
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
