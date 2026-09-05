package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
)

func newDeinitCmd() *cobra.Command {
	var scopeStr string
	var purgeConfig bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "deinit",
		Short: "Remove all managed artifacts, cache, and optionally config",
		Long: `Tear down agent-marketplace artifacts for the specified scope.

By default (--scope user), removes all machine-scoped materialized artifacts
and cleans the registry cache. Use --scope repo or --scope local to remove
repo-scoped artifacts only.

Use --purge-config to also delete the config file(s) for the target scope.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := parseDeinitScope(scopeStr)
			if err != nil {
				return err
			}
			return runDeinit(cmd.Context(), cmd.OutOrStdout(), scope, purgeConfig, dryRun)
		},
	}

	cmd.Flags().StringVar(&scopeStr, "scope", "user", "Scope to deinit: user (default), repo, or local")
	cmd.Flags().BoolVar(&purgeConfig, "purge-config", false, "Also delete config file(s) for the target scope")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be removed without deleting")

	return cmd
}

type deinitScope int

const (
	deinitScopeUser deinitScope = iota
	deinitScopeRepo
	deinitScopeLocal
)

func (s deinitScope) String() string {
	switch s {
	case deinitScopeUser:
		return "user"
	case deinitScopeRepo:
		return "repo"
	case deinitScopeLocal:
		return "local"
	default:
		return fmt.Sprintf("deinitScope(%d)", int(s))
	}
}

func parseDeinitScope(s string) (deinitScope, error) {
	switch s {
	case "user", "":
		return deinitScopeUser, nil
	case "repo":
		return deinitScopeRepo, nil
	case "local":
		return deinitScopeLocal, nil
	default:
		return 0, fmt.Errorf("unknown scope %q: must be one of user, repo, local", s)
	}
}

func runDeinit(ctx context.Context, out io.Writer, scope deinitScope, purgeConfig bool, dryRun bool) error {
	repoRoot := config.FindRepoRoot()
	// deinit is the recovery command users reach for when their config is
	// broken, so an unreadable or invalid config must not block teardown.
	// Degrade to the default policy and tell the user which artifacts may
	// survive as a result.
	modes := config.DefaultMaterializationModes()
	globalCfg, globalErr := config.LoadGlobalConfig()
	repoCfg, repoErr := config.LoadRepoConfig()
	localCfg, localErr := config.LoadLocalConfig()
	if cfgErr := errors.Join(globalErr, repoErr, localErr); cfgErr != nil {
		slog.Warn("deinit config load failed; using default materialization modes", "error", cfgErr)
		if _, err := fmt.Fprintf(out, "warning: could not read config (%v); assuming %s mode — artifacts materialized in another mode may remain\n", cfgErr, modes.Project); err != nil {
			return err
		}
	} else if resolvedModes, modeErr := config.ResolveMaterializationModes(globalCfg, repoCfg, localCfg); modeErr != nil {
		slog.Warn("deinit materialization mode resolution failed; using defaults", "error", modeErr)
		if _, err := fmt.Fprintf(out, "warning: %v; assuming %s mode — artifacts materialized in another mode may remain\n", modeErr, modes.Project); err != nil {
			return err
		}
	} else {
		modes = resolvedModes
	}
	slog.Info("deinit start", "scope", scope.String(), "dry_run", dryRun, "machine_mode", modes.Machine, "project_mode", modes.Project)

	// Phase 1: Remove managed materialized artifacts. For --scope user, preserve
	// any artifacts that the current effective configuration still selects from
	// the external organization layer; user-owned artifacts are pruned because
	// they are not in the resolved set.
	var opts []materialize.RunOption
	if dryRun {
		opts = append(opts, materialize.WithDryRun())
	}

	var resolvedArtifacts []resolver.ResolvedArtifact
	releaseExactLease := func() {}
	switch scope {
	case deinitScopeUser:
		opts = append(opts, materialize.WithMachineScopeOnly())
		orgArtifacts, release, err := resolveOrgOwnedArtifacts(ctx, repoRoot)
		if err != nil {
			slog.Warn("deinit could not resolve organization-owned artifacts; organization output may be removed or may survive", "error", err)
			if _, werr := fmt.Fprintf(out, "warning: could not resolve organization-owned artifacts (%v); organization output may be removed or may survive\n", err); werr != nil {
				return werr
			}
		} else {
			resolvedArtifacts = orgArtifacts
			releaseExactLease = release
		}
	case deinitScopeRepo, deinitScopeLocal:
		opts = append(opts, materialize.WithRepoScopeOnly())
	}
	opts = append(opts, materialize.WithMaterializationModes(modes))
	report, err := func() (*materialize.Report, error) {
		defer releaseExactLease()
		return materialize.Run(context.WithoutCancel(ctx), repoRoot, resolvedArtifacts, opts...)
	}()
	if err != nil {
		return fmt.Errorf("removing artifacts: %w", err)
	}

	action := "Removed"
	if dryRun {
		action = "Would remove"
	}

	totalRemoved := 0
	if report != nil {
		totalRemoved = report.TotalRemoved()
	}

	// In dry-run mode, surface the per-artifact changes so the user can see
	// exactly which user-owned files would be removed while organization-owned
	// files are preserved.
	if dryRun && report != nil && (report.TotalWritten() > 0 || totalRemoved > 0) {
		if err := printOutputDirDetail(out, report, false); err != nil {
			return err
		}
	}

	if totalRemoved > 0 {
		if _, err := fmt.Fprintf(out, "%s %d managed artifact(s)\n", action, totalRemoved); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(out, "No managed artifacts to remove\n"); err != nil {
			return err
		}
	}

	// Phase 2: Clean orphaned registry cache directories (user scope only).
	// Preserve cache entries for organization-owned artifacts so their
	// materialized symlinks remain valid after user teardown.
	if scope == deinitScopeUser {
		preserveCfg := preservedCacheConfig(resolvedArtifacts)
		gcResult, gcErr := cache.GarbageCollect(dryRun, preserveCfg)
		if gcErr != nil {
			return fmt.Errorf("cleaning cache: %w", gcErr)
		}
		for _, removed := range gcResult.Removed {
			if dryRun {
				if _, err := fmt.Fprintf(out, "Would remove cache %s\n", removed); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprintf(out, "Removed cache %s\n", removed); err != nil {
					return err
				}
			}
		}
	}

	// Phase 3: Optionally purge config files.
	if purgeConfig {
		configFiles, cfgErr := configFilesForScope(scope, repoRoot)
		if cfgErr != nil {
			return cfgErr
		}
		for _, path := range configFiles {
			if _, statErr := os.Stat(path); statErr != nil {
				continue
			}
			if dryRun {
				if _, err := fmt.Fprintf(out, "Would remove config %s\n", path); err != nil {
					return err
				}
			} else {
				if err := os.Remove(path); err != nil {
					return fmt.Errorf("removing config %s: %w", path, err)
				}
				if _, err := fmt.Fprintf(out, "Removed config %s\n", path); err != nil {
					return err
				}
			}
		}
		// Clean up empty .agents directory after config removal.
		if !dryRun {
			for _, path := range configFiles {
				dir := filepath.Dir(path)
				removeEmptyDir(dir)
			}

			if telemetry.InventoryEnabled(ctx) {
				// Report the effective post-purge configuration from cache only. This
				// lookup is telemetry best effort: resolution errors neither add output
				// nor change the successful teardown's exit status.
				finalResult := pipeline.Resolve(ctx, pipeline.ResolveOptions{
					RepoRoot:       repoRoot,
					LocalConfigDir: config.FindLocalConfigDir(),
					CatalogMode:    catalog.LoadModeCachedNoFetch,
					SkipInventory:  true,
				})
				pipeline.RecordResolvedInventory(ctx, finalResult)
			}
		}
	}

	slog.Info("deinit end", "scope", scope.String(), "dry_run", dryRun, "removed", totalRemoved, "purge_config", purgeConfig)
	return nil
}

func configFilesForScope(scope deinitScope, repoRoot string) ([]string, error) {
	switch scope {
	case deinitScopeUser:
		globalPath, err := config.GlobalConfigPath()
		if err != nil {
			return nil, fmt.Errorf("resolve global config path: %w", err)
		}
		return []string{globalPath}, nil
	case deinitScopeRepo:
		return []string{config.ProjectConfigPath(repoRoot)}, nil
	case deinitScopeLocal:
		return []string{config.LocalConfigPath(repoRoot)}, nil
	default:
		return nil, nil
	}
}

func removeEmptyDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	if len(entries) == 0 {
		_ = os.Remove(dir)
	}
}

// resolveOrgOwnedArtifacts resolves the current effective organization
// configuration and returns any organization-source artifacts. These are the
// artifacts that a user-scoped deinit must preserve (organization and
// mandatory policy output).
//
// It uses the trusted exact Git lock view so teardown never refreshes mutable
// refs or writes the Git cache. Missing or unsafe Git snapshots remain hard
// failures. When no machine-wide target harness set is configured, there is no
// organization-owned output to preserve, so the function returns an empty
// slice rather than defaulting artifacts to a target the user never requested.
// On success the caller must release the returned exact-cache lease after its
// final use of the artifacts.
func resolveOrgOwnedArtifacts(ctx context.Context, repoRoot string) ([]resolver.ResolvedArtifact, func(), error) {
	localDir := config.FindLocalConfigDir()
	exactOption, _, exactLease, err := prepareRuntimeLocks(ctx, repoRoot, localDir, lockMode{locked: true})
	if err != nil {
		return nil, nil, err
	}
	resolveOpts := pipeline.ResolveOptions{
		RepoRoot:         repoRoot,
		LocalConfigDir:   localDir,
		CatalogMode:      catalog.LoadModeInspect,
		CatalogOption:    []catalog.LoadOption{exactOption},
		RequireTargetSet: true,
		SkipInventory:    true,
		ExactLockLease:   exactLease,
	}
	res, releaseExactLease := pipeline.ResolveWithExactLockLease(ctx, resolveOpts)
	if len(res.Errors) > 0 {
		releaseExactLease()
		var missingErr *pipeline.TargetHarnessSetMissingError
		if len(res.Errors) == 1 && errors.As(res.Errors[0], &missingErr) {
			slog.Info("deinit: no machine-wide target harness set configured; no organization-owned artifacts to preserve")
			return nil, func() {}, nil
		}
		return nil, nil, errors.Join(res.Errors...)
	}
	if unresolved := organizationUnresolvedSelections(res); len(unresolved) > 0 {
		releaseExactLease()
		return nil, nil, errors.Join(unresolved...)
	}
	if len(res.TargetHarnesses) == 0 {
		releaseExactLease()
		slog.Info("deinit: target harness set is empty; no organization-owned artifacts to preserve")
		return nil, func() {}, nil
	}
	var preserved []resolver.ResolvedArtifact
	for _, r := range res.Resolved {
		if r.Source == config.Organization {
			preserved = append(preserved, r)
		}
	}
	slog.Info("deinit: resolved organization-owned artifacts to preserve", "count", len(preserved))
	return preserved, releaseExactLease, nil
}

// organizationUnresolvedSelections promotes soft unresolved selections from
// organization registries to teardown-blocking errors. Resolve normally keeps
// these as warnings because online sync can distinguish a removed resource
// from a transient catalog failure; deinit cannot safely make that assumption
// while deciding which organization output and caches to preserve.
func organizationUnresolvedSelections(res pipeline.ResolveResult) []error {
	if res.MergedCfg == nil {
		return nil
	}
	var unresolved []error
	for _, issue := range res.ResolveErrors {
		var notFound *resolver.SelectedNotFoundError
		if errors.As(issue, &notFound) && isOrganizationRegistry(res.MergedCfg, notFound.URL, notFound.Path, notFound.Ref) {
			unresolved = append(unresolved, issue)
			continue
		}
		var malformed *resolver.SelectedMalformedError
		if errors.As(issue, &malformed) && isOrganizationRegistry(res.MergedCfg, malformed.URL, malformed.Path, malformed.Ref) {
			unresolved = append(unresolved, issue)
		}
	}
	return unresolved
}

func isOrganizationRegistry(cfg *config.Config, url, path, ref string) bool {
	for _, entry := range cfg.Registries {
		if entry.Source == config.Organization && entry.URL == url && entry.Path == path && entry.Ref == ref {
			return true
		}
	}
	return false
}

// preservedCacheConfig builds a minimal config containing the registries that
// back the preserved organization-owned artifacts. cache.GarbageCollect uses the
// registry URLs to keep the corresponding raw clone directories while removing
// caches that are no longer referenced by preserved output.
func preservedCacheConfig(artifacts []resolver.ResolvedArtifact) *config.Config {
	cfg := &config.Config{
		APIVersion: config.APIVersionV3,
		Registries: []config.RegistryEntry{},
	}
	seen := make(map[string]struct{})
	for _, a := range artifacts {
		if a.RegistryURL == "" {
			continue
		}
		key := a.RegistryURL + "@" + a.Ref
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cfg.Registries = append(cfg.Registries, config.RegistryEntry{
			URL: a.RegistryURL,
			Ref: a.Ref,
		})
	}
	return cfg
}
