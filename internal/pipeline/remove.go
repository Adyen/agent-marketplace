package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Scope is the destination layer for an install/remove operation.
type Scope string

const (
	ScopeRepo  Scope = "repo"  // <repo>/.agents/config.yaml (default)
	ScopeUser  Scope = "user"  // ~/.agents/config.yaml
	ScopeLocal Scope = "local" // <repo>/.agents/config.local.yaml
)

// ParseScope parses a scope string, returning an error for unknown values.
func ParseScope(s string) (Scope, error) {
	switch Scope(strings.ToLower(strings.TrimSpace(s))) {
	case ScopeRepo, Scope(""):
		return ScopeRepo, nil
	case ScopeUser:
		return ScopeUser, nil
	case ScopeLocal:
		return ScopeLocal, nil
	default:
		return "", fmt.Errorf("unknown scope %q: must be one of repo, user, local", s)
	}
}

type CleanupDependencyCommitter func(context.Context, string, []StagedConfigEdit, []dependencytxn.ReadAssertion, dependencytxn.CommitScope) error

// RemoveOptions holds the arguments for a remove operation.
type RemoveOptions struct {
	RepoRoot            string
	ResourceName        string
	Kind                ir.ResourceKind
	HasKind             bool
	Scope               Scope
	ScopeSpecified      bool
	FromURL             string // --from flag: ad-hoc registry URL
	FromRef             string // --ref flag: git ref for --from
	FromPath            string // --path flag: subdirectory for --from
	DependencyReconcile DependencyReconciler
	CleanupDependencies CleanupDependencyCommitter
	configStaging       *configStagingManager
	warningSink         *[]error
}

// RemoveResult holds the outcome of a remove operation.
type RemoveResult struct {
	RemovedName       string
	IsCollection      bool
	CollectionVia     string // non-empty when a collection member was excluded
	RemovedRegistry   bool   // true when --from was used without a name (entire entry removed)
	ExcludedRegistry  bool   // true when an entire registry from a lower layer was suppressed via exclude_all
	ExcludedURL       string // the registry URL suppressed when ExcludedRegistry is true
	ExcludedScope     Scope  // the scope at which the registry was suppressed
	InferredUserScope bool   // true when scope was not specified and inferred to user scope
	Warnings          []error
}

// Remove removes a resource from the appropriate config layer and re-syncs.
// It handles collection detection, scope inference, scope boundary validation,
// per-scope config editing, and --from ad-hoc registry removal.
func syncAfterRemove(ctx context.Context, opts RemoveOptions, extra ...silentSyncOpt) error {
	if opts.DependencyReconcile != nil {
		extra = append(extra, withDependencyReconciler(opts.DependencyReconcile), withConfigStaging(opts.configStaging))
	}
	warnings, err := silentSyncCollect(ctx, opts.RepoRoot, extra...)
	if opts.warningSink != nil {
		*opts.warningSink = append(*opts.warningSink, warnings...)
	}
	return err
}

func Remove(ctx context.Context, opts RemoveOptions) (result *RemoveResult, retErr error) {
	var warnings []error
	opts.warningSink = &warnings
	defer func() {
		if result != nil {
			result.Warnings = warnings
		}
	}()
	if opts.DependencyReconcile != nil {
		var err error
		opts.configStaging, err = newConfigStagingManager()
		if err != nil {
			return nil, err
		}
		defer closeConfigStaging(opts.configStaging, "remove", &retErr)
	}

	tel := telemetry.TelemetryFromContext(ctx)
	var removeSpan telemetry.Span
	if tel != nil {
		ctx, removeSpan = tel.StartSpan(ctx, "agent_marketplace.pipeline.remove", removeOptionAttrs(opts))
	}
	defer func() {
		if removeSpan != (telemetry.Span{}) {
			removeSpan.SetAttributes(removeResultAttrs(result, retErr))
			removeSpan.End()
		}
	}()

	// --from ad-hoc registry path.
	if fromURL := strings.TrimSpace(opts.FromURL); fromURL != "" {
		return removeFromAdHoc(ctx, opts, fromURL)
	}

	name := strings.TrimSpace(opts.ResourceName)
	if name == "" {
		return nil, fmt.Errorf("resource name is required")
	}

	mergedCfg, repoCfg, orgCfg, err := loadRemoveConfig(ctx, opts.RepoRoot)
	if err != nil {
		return nil, err
	}

	if collection, entry, ok := marketplaceCollectionByName(mergedCfg, name, opts.Kind, opts.HasKind); ok {
		if entry.Source == config.Organization {
			if entry.Required {
				return nil, fmt.Errorf("collection %q is required and managed externally", name)
			}
			return nil, fmt.Errorf("collection %q is managed externally", name)
		}
		effective := opts.Scope
		if !opts.ScopeSpecified {
			effective = inferMarketplaceRemoveScope(entry.Source)
		}
		configPath, err := scopedConfigPath(opts.RepoRoot, effective)
		if err != nil {
			return nil, fmt.Errorf("resolve config path: %w", err)
		}
		if err := removeMarketplaceAndSync(ctx, opts.RepoRoot, configPath, func() (*config.FileSnapshot, config.ConfigFileCommit, error) {
			return config.RemoveMarketplaceCollectionWithSnapshotAndCommit(configPath, entry.URL, entry.InstanceID, collection)
		}); err != nil {
			return nil, err
		}
		return &RemoveResult{RemovedName: name, IsCollection: true}, nil
	}

	if pin, entry, ok := marketplaceDirectPinByName(mergedCfg, name, opts.Kind, opts.HasKind); ok {
		if entry.Source == config.Organization {
			if entry.Required {
				return nil, fmt.Errorf("%s %q is required and managed externally", skill.KindLabel(opts.Kind), name)
			}
			return nil, fmt.Errorf("%s %q is managed externally", skill.KindLabel(opts.Kind), name)
		}
		effective := opts.Scope
		if !opts.ScopeSpecified {
			effective = inferMarketplaceRemoveScope(entry.Source)
		}
		if err := config.ValidateScopeBoundary(entry.Source, scopeToConfigSource(effective)); err != nil {
			return nil, err
		}
		configPath, err := scopedConfigPath(opts.RepoRoot, effective)
		if err != nil {
			return nil, fmt.Errorf("resolve config path: %w", err)
		}
		if err := removeMarketplaceAndSync(ctx, opts.RepoRoot, configPath, func() (*config.FileSnapshot, config.ConfigFileCommit, error) {
			return config.RemoveMarketplaceDirectPinWithSnapshotAndCommit(configPath, entry.URL, entry.InstanceID, pin)
		}); err != nil {
			return nil, err
		}
		return &RemoveResult{RemovedName: name}, nil
	}

	if pin, ok := marketplaceCollectionOnlyPinByName(mergedCfg, name, opts.Kind, opts.HasKind); ok {
		collectionName := "an explicitly selected collection"
		if selections := pin.SelectedBy.List(); len(selections) > 0 {
			collectionName = selections[0].Name
		}
		return nil, fmt.Errorf("marketplace %s %q is selected only through collection %s; remove that collection instead", pin.Kind, name, collectionName)
	}

	// Collections disappear during expansion, so locate their owning selection
	// before resolving ordinary resources.
	if !opts.HasKind || opts.Kind == ir.KindCollection {
		selection, effective, err := findCollectionRemovalSelection(opts.RepoRoot, mergedCfg, orgCfg, name, opts.Scope, opts.ScopeSpecified)

		if err != nil {
			return nil, err
		}
		if selection != nil {
			if selection.Required {
				return nil, fmt.Errorf("collection %q is required and cannot be removed", name)
			}
			if err := config.ValidateScopeBoundary(selection.Source, scopeToConfigSource(effective)); err != nil {
				var boundaryErr *config.ScopeBoundaryError
				if errors.As(err, &boundaryErr) {
					return nil, fmt.Errorf("cannot exclude collection %q at %s scope: collection is managed at %s level. %s", name, effective, selection.Source, boundaryErr.Suggestion)
				}
				return nil, err
			}
			configPath, err := scopedConfigPath(opts.RepoRoot, effective)
			if err != nil {
				return nil, fmt.Errorf("resolve config path: %w", err)
			}
			configPath, err = stageConfigPath(opts.configStaging, configPath)
			if err != nil {
				return nil, err
			}
			if selection.RegistryScoped {
				registryURL, registryPath := collectionRegistryLocation(selection)
				if err := disableCollectionInRegistry(configPath, registryURL, registryPath, selection.Ref, name); err != nil {
					return nil, err
				}
			} else if err := removeOrExcludeCollection(configPath, name); err != nil {
				return nil, err
			}
			if err := syncAfterRemove(ctx, opts); err != nil {
				return nil, err
			}
			return &RemoveResult{RemovedName: name, IsCollection: true, InferredUserScope: !opts.ScopeSpecified && effective == ScopeUser}, nil
		}
	}

	resolvedResult := Resolve(ctx, ResolveOptions{RepoRoot: opts.RepoRoot, SkipInventory: true, ReadOnlyExactLocks: true})
	if len(resolvedResult.Errors) > 0 {
		return nil, errors.Join(resolvedResult.Errors...)
	}
	resolved := resolvedResult.Resolved

	target, err := resolvedResourceByNameInScope(resolved, name, opts.Kind, opts.HasKind, mergedCfg, opts.Scope, opts.ScopeSpecified)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, fmt.Errorf("resource %q is not currently enabled", name)
	}

	// required:true skills are immune to remove from any scope.
	if target.Required {
		return nil, fmt.Errorf("%s %q is required and cannot be removed", skill.KindLabel(target.Kind), name)
	}

	// Determine the effective scope: caller-specified takes precedence;
	// otherwise infer from the skill's source layer.
	effective := opts.Scope
	inferredUserScope := false
	if !opts.ScopeSpecified {
		effective = inferRemoveScope(*target)
		inferredUserScope = effective == ScopeUser
	}
	if err := config.ValidateScopeBoundary(target.Source, scopeToConfigSource(effective)); err != nil {
		var boundaryErr *config.ScopeBoundaryError
		if errors.As(err, &boundaryErr) {
			return nil, fmt.Errorf("cannot exclude %s %q at %s scope: resource is managed at %s level. %s",
				skill.KindLabel(target.Kind), name, effective, target.Source, boundaryErr.Suggestion)
		}
		return nil, err
	}

	// Skill comes from a collection — write an exclude in the active scope so
	// the collection stays installed but this member is suppressed.
	if target.Via != "" {
		if err := excludeCollectionMember(opts.RepoRoot, effective, opts.configStaging, target, name); err != nil {
			return nil, err
		}
		if err := syncAfterRemove(ctx, opts); err != nil {
			return nil, err
		}
		return &RemoveResult{RemovedName: name, CollectionVia: target.Via, InferredUserScope: inferredUserScope}, nil
	}

	if err := removeResource(opts.RepoRoot, effective, opts.configStaging, repoCfg, target, name, opts.ScopeSpecified); err != nil {
		return nil, enrichRemoveError(err, name, target)
	}

	if err := syncAfterRemove(ctx, opts); err != nil {
		return nil, err
	}
	return &RemoveResult{RemovedName: name, InferredUserScope: inferredUserScope}, nil
}

func disableCollectionInRegistry(configPath, registryURL, registryPath, ref, name string) error {
	if strings.TrimSpace(ref) != "" {
		return config.DisableResourceWithShadowFallbackAtRef(configPath, registryURL, registryPath, ref, ir.KindCollection, name)
	}
	if err := config.DisableResourceWithShadowFallback(configPath, registryURL, registryPath, ref, ir.KindCollection, name); err != nil {
		return err
	}
	// Retain an exclusion after removing a root selection so an included or
	// lower-layer selection cannot re-enable the collection.
	return config.AddKindExcludeToRegistryInConfig(configPath, registryURL, registryPath, ir.KindCollection, name)
}

func hasTopLevelCollectionSelection(cfg *config.Config, name string) bool {
	if cfg == nil {
		return false
	}
	name = strings.TrimSpace(name)
	for _, selected := range cfg.Collections.Include {
		if strings.TrimSpace(selected) == name {
			return true
		}
	}
	return false
}

type collectionRemovalSelection struct {
	Source         config.Source
	Required       bool
	RegistryScoped bool
	RegistryURL    string
	RegistryPath   string
	Ref            string
}

// findCollectionRemovalSelection finds the collection occurrence owned by the
// requested scope. Merged config is used for policy, not file selection.
func findCollectionRemovalSelection(repoRoot string, mergedCfg, orgCfg *config.Config, name string, requested Scope, specified bool) (*collectionRemovalSelection, Scope, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", nil
	}

	scopes := []Scope{ScopeRepo, ScopeLocal, ScopeUser}
	if specified {
		scopes = []Scope{requested}
	}
	for _, scope := range scopes {
		selection, err := collectionSelectionAtScope(repoRoot, scope, name, mergedCfg)
		if err != nil {
			return nil, "", err
		}
		if selection != nil {
			return selection, scope, nil
		}
	}

	// Preserve machine ownership so an explicit repo/local attempt can return a
	// scope-boundary refusal. Never use another repo/local occurrence as a
	// fallback: that would edit an ineffective scope and report success.
	if specified {
		for _, scope := range []Scope{ScopeUser, ScopeRepo, ScopeLocal} {
			selection, err := collectionSelectionAtScope(repoRoot, scope, name, mergedCfg)
			if err != nil {
				return nil, "", err
			}
			if selection != nil && selection.Source.IsMachineScope() {
				return selection, requested, nil
			}
		}
	}

	// Preserve organization ownership so unscoped removal can infer user scope
	// and explicit repo/local removal can enforce the boundary.
	if selection := collectionSelectionInConfig(orgCfg, config.Organization, name, mergedCfg); selection != nil {
		if specified {
			return selection, requested, nil
		}
		return selection, inferCollectionRemoveScope(selection), nil
	}

	if specified {
		if mergedCfg != nil {
			for _, entry := range mergedCfg.Registries {
				if !entry.Source.IsMachineScope() || !collectionSelectedInEntry(entry, name) {
					continue
				}
				return &collectionRemovalSelection{
					Source:         entry.Source,
					Required:       entry.Required,
					RegistryScoped: true,
					RegistryURL:    entry.URL,
					RegistryPath:   entry.Path,
					Ref:            entry.Ref,
				}, requested, nil
			}
		}
	}
	return nil, "", nil
}

func collectionSelectionAtScope(repoRoot string, scope Scope, name string, mergedCfg *config.Config) (*collectionRemovalSelection, error) {
	configPath, err := scopedConfigPath(repoRoot, scope)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	return collectionSelectionAtPath(configPath, scopeToConfigSource(scope), name, mergedCfg)
}

func collectionSelectionAtPath(configPath string, source config.Source, name string, mergedCfg *config.Config) (*collectionRemovalSelection, error) {
	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config %q: %w", configPath, err)
	}
	return collectionSelectionInConfig(cfg, source, name, mergedCfg), nil
}

func collectionSelectionInConfig(cfg *config.Config, source config.Source, name string, mergedCfg *config.Config) *collectionRemovalSelection {
	if cfg == nil {
		return nil
	}

	if hasTopLevelCollectionSelection(cfg, name) {
		return &collectionRemovalSelection{
			Source: source,
			// A top-level selection has no provider identity, so only explicit
			// required collection selections can be attributed safely.
			Required: collectionRequiredInConfig(mergedCfg, name),
		}
	}
	for _, entry := range cfg.Registries {
		if !collectionSelectedInEntry(entry, name) {
			continue
		}
		entrySource := entry.Source
		if entrySource == config.SourceUnset {
			entrySource = source
		}
		selectedEntry := entry
		selectedEntry.Source = entrySource
		return &collectionRemovalSelection{
			Source:         entrySource,
			Required:       collectionRequiredInRegistry(mergedCfg, name, selectedEntry),
			RegistryScoped: true,
			RegistryURL:    entry.URL,
			RegistryPath:   entry.Path,
			Ref:            entry.Ref,
		}
	}
	return nil
}

func inferCollectionRemoveScope(selection *collectionRemovalSelection) Scope {
	if selection == nil {
		return ScopeRepo
	}
	if selection.Source == config.RepoPersonal {
		return ScopeLocal
	}
	if selection.Source.IsMachineScope() {
		return ScopeUser
	}
	return ScopeRepo
}

func collectionRegistryLocation(selection *collectionRemovalSelection) (url, path string) {
	url = strings.TrimSpace(selection.RegistryURL)
	path = strings.TrimSpace(selection.RegistryPath)
	// A path-only registry is represented as Path in the in-memory entry.
	if url == "" && path != "" {
		return path, ""
	}
	return url, path
}

func collectionSelectedInEntry(entry config.RegistryEntry, name string) bool {
	for _, selected := range entry.Collections.Include {
		if strings.TrimSpace(selected) == name {
			return true
		}
	}
	return false
}

// collectionRequiredInConfig checks only explicit required collection
// selections. Provider-wide policy needs the selected registry identity and is
// handled by collectionRequiredInRegistry.
func collectionRequiredInConfig(cfg *config.Config, name string) bool {
	if cfg == nil {
		return false
	}
	for _, entry := range cfg.Registries {
		if entry.Required && collectionSelectedInEntry(entry, name) {
			return true
		}
	}
	return false
}

// collectionRequiredInRegistry applies required policy only to the registry
// entry that actually provides the selected collection. A required provider at
// another physical URL/path/ref must not make an optional collection immutable;
// config-layer source is intentionally not part of physical identity.
// Tag policy uses actual collection tags when callers have catalog context.
// Without that context, a non-empty required tag policy remains protected
// conservatively so an unknown collection cannot bypass required immunity.
func collectionRequiredInRegistry(cfg *config.Config, name string, selected config.RegistryEntry, collectionTags ...[]string) bool {
	if selected.Required {
		return true
	}
	if cfg == nil {
		return false
	}
	tagsProvided := len(collectionTags) > 0
	var tags []string
	if tagsProvided {
		tags = collectionTags[0]
	}
	for _, entry := range cfg.Registries {
		if !entry.Required || !registryEntriesShareIdentity(entry, selected) {
			continue
		}
		if collectionSelectedInEntry(entry, name) || requiredCollectionAutoSelects(entry) {
			return true
		}
		if len(entry.Tags) > 0 && (!tagsProvided || config.HasMatchingTag(tags, entry.Tags)) {
			return true
		}
	}
	return false
}

func registryEntriesShareIdentity(a, b config.RegistryEntry) bool {
	return config.NormalizeGitURL(a.URL) == config.NormalizeGitURL(b.URL) &&
		config.NormalizeRegistryPath(a.Path) == config.NormalizeRegistryPath(b.Path) &&
		strings.TrimSpace(a.Ref) == strings.TrimSpace(b.Ref)
}

// requiredCollectionAutoSelects mirrors resolver selection policy for the
// collection kind. Required providers select every kind when no explicit
// include exists; tag providers are intentionally excluded because matching
// requires the collection's catalog metadata.
func requiredCollectionAutoSelects(entry config.RegistryEntry) bool {
	collectionSelector := entry.SelectorForKind(ir.KindCollection)
	if collectionSelector.ExcludeAll {
		return false
	}
	if entry.Tags != nil {
		return false
	}
	for _, kind := range config.AllKinds {
		if len(entry.SelectorForKind(kind).Include) > 0 {
			return false
		}
	}
	return true
}

func removeOrExcludeCollection(configPath, name string) error {
	rootCfg, err := config.LoadRootConfigFromPath(configPath)
	if err != nil {
		return fmt.Errorf("load root config %q: %w", configPath, err)
	}
	// Validate includes before mutating the root selection.
	if _, err := config.LoadConfigFromPath(configPath); err != nil {
		return fmt.Errorf("validate config includes %q: %w", configPath, err)
	}
	if hasTopLevelCollectionSelection(rootCfg, name) {
		if err := config.RemoveCollectionFromConfig(configPath, name); err != nil {
			return err
		}
		expandedCfg, err := config.LoadConfigFromPath(configPath)
		if err != nil {
			return fmt.Errorf("reload expanded config %q: %w", configPath, err)
		}
		if !hasTopLevelCollectionSelection(expandedCfg, name) {
			return nil
		}
	}
	return config.AddKindExcludeToConfig(configPath, ir.KindCollection, name)
}

func loadRemoveConfig(ctx context.Context, repoRoot string) (mergedCfg, repoCfg, orgCfg *config.Config, err error) {
	ec := LoadEffective(ctx, ConfigLoadOnline, config.LoadEffectiveOptions{RepoRoot: repoRoot}, nil)
	if len(ec.Errors) > 0 && !config.OnlyRegistryURLBlockedErrors(ec.Errors) {
		return nil, nil, nil, errors.Join(ec.Errors...)
	}
	return ec.Merged, ec.Repo.Config, ec.Organization.Config, nil
}

func excludeCollectionMember(repoRoot string, scope Scope, staging *configStagingManager, target *resolver.ResolvedArtifact, name string) error {
	path, err := ScopedConfigPath(repoRoot, scope)
	if err != nil {
		return err
	}
	path, err = stageConfigPath(staging, path)
	if err != nil {
		return err
	}
	switch scope {
	case ScopeUser:
		return config.DisableResourceWithShadowFallback(path, target.RegistryURL, target.RegistryPath, target.Ref, target.Kind, name)
	case ScopeLocal:
		if target.Source == config.RepoPersonal {
			return config.AddKindExcludeToRegistryInConfig(path, target.RegistryURL, target.RegistryPath, target.Kind, name)
		}
		return config.AddKindExcludeToConfig(path, target.Kind, name)
	default:
		return config.DisableResourceWithShadowFallback(path, target.RegistryURL, target.RegistryPath, target.Ref, target.Kind, name)
	}
}

func removeResource(repoRoot string, scope Scope, staging *configStagingManager, repoCfg *config.Config, target *resolver.ResolvedArtifact, name string, scopeSpecified bool) error {
	path, err := ScopedConfigPath(repoRoot, scope)
	if err != nil {
		return err
	}
	path, err = stageConfigPath(staging, path)
	if err != nil {
		return err
	}
	switch scope {
	case ScopeUser:
		return config.DisableResourceWithShadowFallback(path, target.RegistryURL, target.RegistryPath, target.Ref, target.Kind, name)

	case ScopeLocal:
		if target.Source == config.RepoPersonal {
			rootCfg, err := config.LoadRootConfigFromPath(path)
			if err != nil {
				return fmt.Errorf("load local root config: %w", err)
			}
			if rootCfg != nil && registryEntryInList(rootCfg.Registries, target.RegistryURL, target.RegistryPath) {
				if err := config.RemoveResourceFromRegistryInConfig(path, target.RegistryURL, target.RegistryPath, target.Kind, name); err != nil {
					return fmt.Errorf("remove resource from registry entry: %w", err)
				}
				return config.AddKindExcludeToRegistryInConfig(path, target.RegistryURL, target.RegistryPath, target.Kind, name)
			}
			localCfg, err := config.LoadLocalConfigFromPath(path)
			if err != nil {
				return fmt.Errorf("load local config: %w", err)
			}
			if localCfg != nil && localRegistryEntryExists(localCfg, target.RegistryURL, target.RegistryPath) {
				if err := config.DisableResourceWithShadowFallback(path, target.RegistryURL, target.RegistryPath, target.Ref, target.Kind, name); err != nil {
					return err
				}
				return config.AddKindExcludeToConfig(path, target.Kind, name)
			}
			if err := config.RemoveKindExcludeFromConfig(path, target.Kind, name); err != nil {
				return fmt.Errorf("clear stale exclude from local config: %w", err)
			}
			if err := config.RemoveSkillFromConfig(path, name); err != nil {
				return fmt.Errorf("remove skill from local config include: %w", err)
			}
			return config.AddKindExcludeToConfig(path, target.Kind, name)
		}
		return config.AddKindExcludeToConfig(path, target.Kind, name)

	default:
		if repoCfg == nil {
			return fmt.Errorf("no repo config found; cannot remove %q", name)
		}
		if (!scopeSpecified || target.Source.IsMachineScope()) && !projectExplicitlySelects(repoCfg, *target) && !projectSelectsByTag(repoCfg, *target) {
			localPath, err := stageConfigPath(staging, config.LocalConfigPath(repoRoot))
			if err != nil {
				return err
			}
			return config.AddKindExcludeToConfig(localPath, target.Kind, name)
		}
		return config.DisableResourceWithShadowFallback(path, target.RegistryURL, target.RegistryPath, target.Ref, target.Kind, name)
	}
}

// inferRemoveScope returns the scope to use for remove when none is explicitly
// specified by the caller. Machine-scope (org/team/user) resources default to
// ScopeUser so the exclude is written to the user config where it is
// effective. Repo-personal resources default to ScopeLocal; all others
// default to ScopeRepo.
func inferRemoveScope(target resolver.ResolvedArtifact) Scope {
	if target.Source == config.RepoPersonal {
		return ScopeLocal
	}
	if target.Source.IsMachineScope() {
		return ScopeUser
	}
	return ScopeRepo
}

func scopeToConfigSource(scope Scope) config.Source {
	switch scope {
	case ScopeUser:
		return config.UserPersonal
	case ScopeLocal:
		return config.RepoPersonal
	default:
		return config.RepoCommitted
	}
}

// ScopedConfigPath returns the config file path for the given scope.
func ScopedConfigPath(repoRoot string, scope Scope) (string, error) {
	switch scope {
	case ScopeUser:
		return config.GlobalConfigPath()
	case ScopeLocal:
		return config.LocalConfigPath(repoRoot), nil
	default:
		return config.ProjectConfigPath(repoRoot), nil
	}
}

// scopedConfigPath is the unexported version used internally.
func scopedConfigPath(repoRoot string, scope Scope) (string, error) {
	return ScopedConfigPath(repoRoot, scope)
}

func stageConfigPath(staging *configStagingManager, path string) (string, error) {
	if staging == nil {
		return path, nil
	}
	return staging.Path(path)
}

// registryEntryInList reports whether the list contains an entry that exactly
// matches url and path (the same exact-match rule the editor uses when writing
// to a specific scope's config).
func registryEntryInList(entries []config.RegistryEntry, url, registryPath string) bool {
	for _, entry := range entries {
		if config.NormalizeGitURL(entry.URL) == config.NormalizeGitURL(url) && config.NormalizeRegistryPath(entry.Path) == config.NormalizeRegistryPath(registryPath) {
			return true
		}
	}
	return false
}

// selectAdHocRegistryEntry resolves one configured entry for --from. An
// omitted path or ref is a wildcard only while it identifies one logical
// location; distinct paths or refs are ambiguous and fail closed.
func selectAdHocRegistryEntry(cfg *config.Config, url, path, ref string) (config.RegistryEntry, bool, error) {
	if cfg == nil {
		return config.RegistryEntry{}, false, nil
	}
	var matches []config.RegistryEntry
	for _, entry := range cfg.Registries {
		if adHocRegistryEntryMatches(entry, url, path, ref) {
			matches = append(matches, entry)
		}
	}
	if len(matches) == 0 {
		return config.RegistryEntry{}, false, nil
	}
	first := matches[0]
	for _, entry := range matches[1:] {
		if config.NormalizeRegistryPath(entry.Path) != config.NormalizeRegistryPath(first.Path) || strings.TrimSpace(entry.Ref) != strings.TrimSpace(first.Ref) {
			return config.RegistryEntry{}, false, fmt.Errorf("registry %q matches multiple entries; use --path and --ref to disambiguate", url)
		}
	}
	return first, true, nil
}

func adHocRegistryEntryMatches(entry config.RegistryEntry, url, registryPath, ref string) bool {
	pathOmitted := strings.TrimSpace(registryPath) == ""
	return config.NormalizeGitURL(entry.URL) == config.NormalizeGitURL(url) &&
		(pathOmitted || config.NormalizeRegistryPath(entry.Path) == config.NormalizeRegistryPath(registryPath)) &&
		(strings.TrimSpace(ref) == "" || strings.TrimSpace(entry.Ref) == strings.TrimSpace(ref))
}

func countAdHocRegistryEntries(entries []config.RegistryEntry, url, path, ref string) int {
	count := 0
	for _, entry := range entries {
		if adHocRegistryEntryMatches(entry, url, path, ref) {
			count++
		}
	}
	return count
}

// validateRegistryScopeBoundary rejects suppressing a machine-scope registry
// (org/team/user) from a repo-local scope (repo/local), where the exclude
// would be silently ineffective. It mirrors config.ValidateScopeBoundary but
// frames the error in terms of the whole registry.
func validateRegistryScopeBoundary(source config.Source, scope Scope, url string) error {
	if err := config.ValidateScopeBoundary(source, scopeToConfigSource(scope)); err != nil {
		var boundaryErr *config.ScopeBoundaryError
		if errors.As(err, &boundaryErr) {
			return fmt.Errorf("cannot exclude registry %q at %s scope: it is managed at %s level. %s",
				url, scope, source, boundaryErr.Suggestion)
		}
		return err
	}
	return nil
}

func resolvedResourceByName(resolved []resolver.ResolvedArtifact, name string, kind ir.ResourceKind, hasKind bool, cfg *config.Config) (*resolver.ResolvedArtifact, error) {
	return resolveResourceMatches(resourceMatches(resolved, name, kind, hasKind), name, cfg)
}

// resolvedResourceByNameInScope narrows a same-kind name lookup to the
// requested installation scope before checking ambiguity. An explicit scope
// with no occurrence is not allowed to fall through to a different scope. The
// sole exception is a machine-owned occurrence targeted from repo/local scope,
// which is retained so ValidateScopeBoundary can return the actionable refusal.
func resolvedResourceByNameInScope(resolved []resolver.ResolvedArtifact, name string, kind ir.ResourceKind, hasKind bool, cfg *config.Config, scope Scope, scopeSpecified bool) (*resolver.ResolvedArtifact, error) {
	matches := resourceMatches(resolved, name, kind, hasKind)
	if scopeSpecified {
		scoped := make([]resolver.ResolvedArtifact, 0, len(matches))
		for _, match := range matches {
			if sourceMatchesRemoveScope(match.Source, scope) {
				scoped = append(scoped, match)
			}
		}
		if len(scoped) == 0 && scope == ScopeLocal {
			// Local scope is an effective exclusion layer for committed repo
			// resources and collection members, without editing their owning
			// selection. Do not mix this fallback with a real local occurrence:
			// an explicit local occurrence must remain the only target.
			for _, match := range matches {
				if match.Source == config.RepoCommitted || (match.Via != "" && !match.Source.IsMachineScope()) {
					scoped = append(scoped, match)
				}
			}
		}
		if len(scoped) > 0 {
			matches = scoped
		} else {
			machineBoundary := scope == ScopeRepo || scope == ScopeLocal
			for _, match := range matches {
				if machineBoundary && match.Source.IsMachineScope() {
					return resolveResourceMatches([]resolver.ResolvedArtifact{match}, name, cfg)
				}
			}
			return nil, nil
		}
	}
	return resolveResourceMatches(matches, name, cfg)
}

func resourceMatches(resolved []resolver.ResolvedArtifact, name string, kind ir.ResourceKind, hasKind bool) []resolver.ResolvedArtifact {
	var matches []resolver.ResolvedArtifact
	for _, candidate := range resolved {
		if candidate.Name != name {
			continue
		}
		if hasKind && candidate.Kind != kind {
			continue
		}
		matches = append(matches, candidate)
	}
	return matches
}

func resolveResourceMatches(matches []resolver.ResolvedArtifact, name string, cfg *config.Config) (*resolver.ResolvedArtifact, error) {
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	// Multiple matches: report the *right* kind of ambiguity.
	//   - Different resource kinds share the name (e.g. a skill and a command):
	//     --kind disambiguates.
	//   - Same kind, enabled from multiple config layers (e.g. the same skill
	//     enabled at both user and local scope): --kind cannot help — the
	//     caller must pick a layer with --scope (naming the entry with
	//     --from/--path).
	if labels := distinctKindLabels(matches); len(labels) > 1 {
		return nil, fmt.Errorf("resource %q is ambiguous across kinds (%s); rerun with --kind", name, strings.Join(labels, ", "))
	}
	return nil, ambiguousRegistryError(name, matches, cfg)
}

func sourceMatchesRemoveScope(source config.Source, scope Scope) bool {
	switch scope {
	case ScopeUser:
		return source.IsMachineScope()
	case ScopeLocal:
		return source == config.RepoPersonal
	default:
		return source == config.RepoCommitted || source == config.SourceUnset
	}
}

// distinctKindLabels returns the sorted, de-duplicated set of kind labels
// present in matches. A length > 1 means the ambiguity spans resource kinds
// (resolvable with --kind); a length of 1 means every match is the same kind.
func distinctKindLabels(matches []resolver.ResolvedArtifact) []string {
	seen := make(map[string]struct{}, len(matches))
	labels := make([]string, 0, len(matches))
	for _, match := range matches {
		label := skill.KindLabel(match.Kind)
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	slices.Sort(labels)
	return labels
}

// ambiguousRegistryError builds an actionable error for the case where a single
// resource name+kind is enabled from more than one config layer. It prints a
// ready-to-run remove invocation per candidate, choosing the --scope that makes
// the removal effective and the *configured* (raw) registry --from/--path the
// user actually wrote — not the artifact's resolved sub-bundle path, which may
// be a deeper discovered directory that matches no config entry.
func ambiguousRegistryError(name string, matches []resolver.ResolvedArtifact, cfg *config.Config) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %q is enabled from multiple config layers; re-run with the --scope for the copy you want to remove:",
		skill.KindLabel(matches[0].Kind), name)
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		url, path, ref := owningRegistryEntry(cfg, match)
		var line strings.Builder
		fmt.Fprintf(&line, "\n  --scope %s --from %q", inferRemoveScope(match), url)
		if r := strings.TrimSpace(ref); r != "" {
			fmt.Fprintf(&line, " --ref %q", r)
		}
		if p := strings.TrimSpace(path); p != "" {
			fmt.Fprintf(&line, " --path %q", p)
		}
		fmt.Fprintf(&line, "   (%s)", match.Source)
		s := line.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		b.WriteString(s)
	}
	return errors.New(b.String())
}

// owningRegistryEntry maps a resolved artifact back to the configured (raw)
// registry entry that produced it, returning that entry's url, path, and ref.
// A parent entry (e.g. path "teams/pm/plugins" with default:true) expands into
// per-sub-bundle artifacts whose RegistryPath is the deeper discovered path
// (e.g. "teams/pm/plugins/pm-data-analytics"); the raw path the user can pass
// to --from/--path is the parent, so we pick the most specific configured entry
// whose path owns the artifact's path. When the same URL+path is configured in
// multiple layers, prefer the entry whose Source matches the artifact's Source
// so the suggested --scope/--path/--ref describe the layer that actually
// produced this copy — otherwise the hint could point at a sibling layer's ref.
// Falls back to the artifact's own values when cfg is nil or no configured
// entry owns it.
func owningRegistryEntry(cfg *config.Config, m resolver.ResolvedArtifact) (url, path, ref string) {
	url, path, ref = m.RegistryURL, m.RegistryPath, m.Ref
	if cfg == nil {
		return url, path, ref
	}
	nu := config.NormalizeGitURL(strings.TrimSpace(m.RegistryURL))
	bestLen := -1
	bestSourceMatch := false
	for _, entry := range cfg.Registries {
		if config.NormalizeGitURL(strings.TrimSpace(entry.URL)) != nu {
			continue
		}
		ep := strings.TrimSpace(entry.Path)
		if !registryPathOwns(ep, strings.TrimSpace(m.RegistryPath)) {
			continue
		}
		// Prefer an entry from the artifact's own config layer. Once a
		// source-matching entry is found, ignore non-matching ones; the first
		// source match resets the path-specificity search among that layer.
		sourceMatch := entry.Source == m.Source
		if bestSourceMatch && !sourceMatch {
			continue
		}
		if sourceMatch && !bestSourceMatch {
			bestSourceMatch = true
			bestLen = -1
		}
		if len(ep) > bestLen { // prefer the most specific owning entry path
			bestLen = len(ep)
			url, path, ref = entry.URL, entry.Path, entry.Ref
		}
	}
	return url, path, ref
}

// resolvedResourceInRegistry finds a resolved artifact by name (and optional
// kind) restricted to a single registry entry identified by url (+ optional
// path). The --from remove path uses this so the user-supplied --from/--path
// flags actually narrow the lookup: a resource that resolves from several
// registry entries or config layers is no longer spuriously "ambiguous" once a
// specific entry is named. When multiple same-kind artifacts still match the
// same entry (e.g. the resource is enabled at more than one scope for that
// entry) the first is returned — they refer to the same underlying resource,
// so Required status and Kind are identical. When --kind was NOT supplied and
// the matches span multiple kinds (e.g. a skill and a command sharing a name in
// the same entry), an error is returned so the caller asks for --kind rather
// than silently disabling an arbitrary kind.
func resolvedResourceInRegistry(resolved []resolver.ResolvedArtifact, name string, kind ir.ResourceKind, hasKind bool, url, path string) (*resolver.ResolvedArtifact, error) {
	return resolvedResourceInRegistryWithRef(resolved, name, kind, hasKind, url, path, "", false)
}

func resolvedResourceInRegistryAtRef(resolved []resolver.ResolvedArtifact, name string, kind ir.ResourceKind, hasKind bool, url, path, ref string) (*resolver.ResolvedArtifact, error) {
	return resolvedResourceInRegistryWithRef(resolved, name, kind, hasKind, url, path, ref, true)
}

func resolvedResourceInRegistryWithRef(resolved []resolver.ResolvedArtifact, name string, kind ir.ResourceKind, hasKind bool, url, path, ref string, requireRef bool) (*resolver.ResolvedArtifact, error) {
	nu := config.NormalizeGitURL(strings.TrimSpace(url))
	wantPath := strings.TrimSpace(path)
	wantRef := strings.TrimSpace(ref)
	var matches []resolver.ResolvedArtifact
	for i := range resolved {
		candidate := resolved[i]
		if candidate.Name != name {
			continue
		}
		if hasKind && candidate.Kind != kind {
			continue
		}
		if config.NormalizeGitURL(strings.TrimSpace(candidate.RegistryURL)) != nu {
			continue
		}
		if requireRef && strings.TrimSpace(candidate.Ref) != wantRef {
			continue
		}
		if !registryPathOwns(wantPath, candidate.RegistryPath) {
			continue
		}
		matches = append(matches, candidate)
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if labels := distinctKindLabels(matches); !hasKind && len(labels) > 1 {
		return nil, fmt.Errorf("%q is ambiguous across kinds (%s); rerun with --kind", name, strings.Join(labels, ", "))
	}
	return &matches[0], nil
}

// registryPathOwns reports whether the configured registry entry path
// entryPath "owns" an artifact discovered at artifactPath. An empty entryPath
// matches everything (URL-only targeting). Otherwise the artifact belongs to
// the entry when its path equals entryPath or lives beneath it — the resolver
// records the discovered sub-bundle path on an artifact, which can be deeper
// than the configured entry path (e.g. entry "teams/pm/plugins" discovers
// "teams/pm/plugins/pm-data-analytics"). Mirrors localRegistryEntryExists.
func registryPathOwns(entryPath, artifactPath string) bool {
	ep := config.NormalizeRegistryPath(entryPath)
	if ep == "" {
		return true
	}
	ap := config.NormalizeRegistryPath(artifactPath)
	return ap == ep || strings.HasPrefix(ap, ep+"/")
}

func projectExplicitlySelects(cfg *config.Config, target resolver.ResolvedArtifact) bool {
	if cfg == nil {
		return false
	}
	if stringListContains(configResourcesForKind(cfg, target.Kind), target.Name) {
		return true
	}
	for _, entry := range cfg.Registries {
		if strings.TrimSpace(entry.URL) == strings.TrimSpace(target.RegistryURL) && stringListContains(entryResourcesForKind(entry, target.Kind), target.Name) {
			return true
		}
	}
	return false
}

func configResourcesForKind(cfg *config.Config, kind ir.ResourceKind) []string {
	var out []string
	for _, entry := range cfg.Registries {
		out = append(out, entryResourcesForKind(entry, kind)...)
	}
	return out
}

func entryResourcesForKind(entry config.RegistryEntry, kind ir.ResourceKind) []string {
	return entry.SelectorForKind(kind).Include
}

func projectSelectsByTag(cfg *config.Config, target resolver.ResolvedArtifact) bool {
	if cfg == nil {
		return false
	}
	for _, entry := range cfg.Registries {
		if strings.TrimSpace(entry.URL) != strings.TrimSpace(target.RegistryURL) {
			continue
		}
		tags := cfg.Tags
		if entry.Tags != nil {
			tags = entry.Tags
		}
		if config.HasMatchingTag(target.Tags, tags) {
			return true
		}
	}
	return false
}

func stringListContains(values []string, wanted string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == strings.TrimSpace(wanted) {
			return true
		}
	}
	return false
}

// localRegistryEntryExists reports whether a registry entry matching the
// given URL and path exists in the local config. Used to determine whether
// a RepoPersonal-sourced resource was installed into a registry entry's
// include list or the top-level kind selector.
func localRegistryEntryExists(localCfg *config.LocalConfig, registryURL, registryPath string) bool {
	for _, entry := range localCfg.Registries {
		if config.NormalizeGitURL(entry.URL) != config.NormalizeGitURL(registryURL) {
			continue
		}
		if registryPath != "" {
			ep := config.NormalizeRegistryPath(entry.Path)
			rp := config.NormalizeRegistryPath(registryPath)
			// The resolved artifact's RegistryPath may include a sub-bundle
			// directory below the entry's configured Path (e.g. entry.Path="plugins"
			// but RegistryPath="plugins/personal"). Use prefix match so that
			// a registry entry with path=plugins correctly owns resources at
			// plugins/personal, plugins/memory, etc.
			if ep != rp && !strings.HasPrefix(rp, ep+"/") {
				continue
			}
		}
		return true
	}
	return false
}

// removeFromAdHoc handles the --from code path for remove.
// With a name: remove the resource from the --from registry entry.
// Without a name: remove the entire registry entry from the config.
func removeFromAdHoc(ctx context.Context, opts RemoveOptions, fromURL string) (*RemoveResult, error) {
	repoRoot := opts.RepoRoot

	// Resolve web URLs into git URL + ref + path. Explicit flags win.
	resolvedURL, resolvedRef, resolvedPath, _ := ResolveFromURL(fromURL, opts.FromRef, opts.FromPath)
	fromURL = resolvedURL
	fromRef := resolvedRef
	fromPath := resolvedPath

	// Validate the --from URL against the effective allowlist before
	// writing to config or fetching anything. Blocked-registry errors are
	// ignored on remove: a registry that was previously allowed and is now
	// blocked by a tightened allowed_registries policy must still be removable.
	syntheticEntry := config.RegistryEntry{URL: fromURL, Ref: fromRef, Path: fromPath}
	ec := LoadEffective(ctx, ConfigLoadOnline, config.LoadEffectiveOptions{RepoRoot: repoRoot}, nil)
	if len(ec.Errors) > 0 && !config.OnlyRegistryURLBlockedErrors(ec.Errors) {
		return nil, errors.Join(ec.Errors...)
	}
	if err := config.ValidateRegistryURLs([]config.RegistryEntry{syntheticEntry}, ec.Allowed); err != nil && !config.IsRegistryURLBlockedError(err) {
		return nil, err
	}

	// Determine the config file to modify.
	configPath, err := ScopedConfigPath(repoRoot, opts.Scope)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	// Load only the target root to decide whether the entry is defined there;
	// included entries belong to their included files and must not be edited.
	cfg, err := config.LoadRootConfigFromPath(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config %q: %w", configPath, err)
	}
	// The merged config finds entries defined by includes or lower layers. A
	// missing --path/--ref may identify one entry, but never silently selects
	// among distinct paths or refs.
	mergedCfg, _, _, err := loadRemoveConfig(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	mergedEntry, entryInMerged, err := selectAdHocRegistryEntry(mergedCfg, fromURL, fromPath, fromRef)
	if err != nil {
		return nil, err
	}
	var rootEntries []config.RegistryEntry
	if cfg != nil {
		rootEntries = cfg.Registries
	}
	targetEntry, entryInTarget, err := selectAdHocRegistryEntry(&config.Config{Registries: rootEntries}, fromURL, fromPath, fromRef)
	if err != nil {
		return nil, err
	}
	// If --path was omitted, carry the uniquely selected entry's path into all
	// mutations. This is especially important for URL-only removal of a local
	// path alias and prevents a root entry from being edited instead.
	selectedPath := fromPath
	if selectedPath == "" && entryInMerged {
		selectedPath = strings.TrimSpace(mergedEntry.Path)
	} else if selectedPath == "" && entryInTarget {
		selectedPath = strings.TrimSpace(targetEntry.Path)
	}
	requiresShadow := entryInMerged && countAdHocRegistryEntries(mergedCfg.Registries, fromURL, fromPath, fromRef) > countAdHocRegistryEntries(rootEntries, fromURL, fromPath, fromRef)
	// Prefer the defining layer's ref when writing a shadow entry, so git
	// shadow entries pass ref-required validation even if the user omitted --ref.
	shadowRef := fromRef
	if entryInMerged && strings.TrimSpace(mergedEntry.Ref) != "" {
		shadowRef = mergedEntry.Ref
	}
	declaringPath := configPath
	configPath, err = stageConfigPath(opts.configStaging, configPath)
	if err != nil {
		return nil, err
	}

	// An empty name targets the registry.
	name := strings.TrimSpace(opts.ResourceName)
	if name == "" {
		if requiresShadow {
			if err := validateRegistryScopeBoundary(mergedEntry.Source, opts.Scope, fromURL); err != nil {
				return nil, err
			}
			var setErr error
			if strings.TrimSpace(shadowRef) != "" {
				setErr = config.SetRegistryExcludeAllInConfigAtRef(configPath, fromURL, shadowRef, selectedPath)
			} else {
				setErr = config.SetRegistryExcludeAllInConfig(configPath, fromURL, shadowRef, selectedPath)
			}
			if err := setErr; err != nil {
				return nil, fmt.Errorf("exclude registry in %s config: %w", opts.Scope, err)
			}
			if err := syncAfterRemove(ctx, opts, WithIgnoreBlockedRegistries()); err != nil {
				return nil, err
			}
			return &RemoveResult{ExcludedRegistry: true, ExcludedURL: fromURL, ExcludedScope: opts.Scope}, nil
		}
		// Registry is defined at this scope (and not inherited from below):
		// deleting the entry fully removes it.
		if entryInTarget {
			var removeErr error
			if strings.TrimSpace(fromRef) != "" {
				removeErr = config.RemoveRegistryEntryFromConfigAtRef(configPath, fromURL, selectedPath, fromRef)
			} else {
				removeErr = config.RemoveRegistryEntryFromConfig(configPath, fromURL, selectedPath)
			}
			if err := removeErr; err != nil {
				return nil, fmt.Errorf("remove registry entry from %s config: %w", opts.Scope, err)
			}
			if err := syncAfterRemove(ctx, opts, WithIgnoreBlockedRegistries()); err != nil {
				return nil, err
			}
			return &RemoveResult{RemovedRegistry: true}, nil
		}
		return nil, fmt.Errorf("registry entry for %q (path=%q) not found in any config; nothing to remove", fromURL, selectedPath)
	}

	// With a name: remove the resource from the --from registry entry. The
	// entry may live in the target config (delete from include / add exclude)
	// or in a lower layer (write a shadow exclude). Either way it must exist
	// somewhere; otherwise it's a typo.
	if !entryInTarget && !entryInMerged {
		return nil, fmt.Errorf("registry entry for %q (path=%q) not found in any config", fromURL, fromPath)
	}

	// Scope a named --from collection to the one selected URL/path/ref entry.
	// The entry lookup above has already failed closed on ambiguous paths/refs.
	collectionEntryInTarget := entryInTarget
	collectionMergedEntry, collectionEntryInMerged := mergedEntry, entryInMerged
	if collectionEntryInMerged && collectionSelectedInEntry(collectionMergedEntry, name) {
		if collectionRequiredInRegistry(mergedCfg, name, collectionMergedEntry) {
			return nil, fmt.Errorf("collection %q is required and cannot be removed", name)
		}
		if !collectionEntryInTarget && collectionEntryInMerged {
			if err := validateRegistryScopeBoundary(collectionMergedEntry.Source, opts.Scope, fromURL); err != nil {
				return nil, err
			}
		}
		collectionShadowRef := fromRef
		if collectionEntryInMerged && strings.TrimSpace(collectionMergedEntry.Ref) != "" {
			collectionShadowRef = collectionMergedEntry.Ref
		}
		if err := disableCollectionInRegistry(configPath, fromURL, selectedPath, collectionShadowRef, name); err != nil {
			return nil, err
		}
		if err := syncAfterRemove(ctx, opts, WithIgnoreBlockedRegistries()); err != nil {
			return nil, err
		}
		return &RemoveResult{RemovedName: name, IsCollection: true}, nil
	}

	// Check required status. Scope the lookup to the --from registry entry so
	// the user-supplied --from/--path disambiguate a resource that resolves
	// from several entries or config layers (otherwise a same-kind, multi-layer
	// resource like a skill enabled at both user and repo scope would be
	// reported as spuriously "ambiguous" even though --from already names the
	// entry to operate on).
	resolvedResult := Resolve(ctx, ResolveOptions{RepoRoot: repoRoot, IgnoreBlockedRegistries: true, SkipInventory: true, ReadOnlyExactLocks: true})
	if len(resolvedResult.Errors) > 0 {
		return nil, errors.Join(resolvedResult.Errors...)
	}
	var target *resolver.ResolvedArtifact
	if strings.TrimSpace(fromRef) != "" {
		target, err = resolvedResourceInRegistryAtRef(resolvedResult.Resolved, name, opts.Kind, opts.HasKind, fromURL, selectedPath, fromRef)
	} else {
		target, err = resolvedResourceInRegistry(resolvedResult.Resolved, name, opts.Kind, opts.HasKind, fromURL, selectedPath)
	}
	if err != nil {
		return nil, err
	}
	if target != nil && target.Required {
		return nil, fmt.Errorf("%s %q is required and cannot be removed", skill.KindLabel(target.Kind), name)
	}

	// Use the resolved kind (not opts.Kind) so non-skill resources are
	// excluded from the correct selector when --kind is omitted.
	resolvedKind := opts.Kind
	if target != nil {
		resolvedKind = target.Kind
	}
	cleanupOnly := config.OnlyRegistryURLBlockedErrors(ec.Errors) && blockedOwnedRemovalKind(resolvedKind)

	// Cross-layer named exclude: when the entry lives only in a lower layer,
	// validate that suppressing it at this scope is meaningful (a local/repo
	// scope cannot suppress a machine-scope registry).
	if !entryInTarget {
		boundarySource := mergedEntry.Source
		if target != nil {
			boundarySource = target.Source
		}
		if err := validateRegistryScopeBoundary(boundarySource, opts.Scope, fromURL); err != nil {
			return nil, err
		}
	}
	var cleanup *ownedRemovalPreparation
	if cleanupOnly {
		if target == nil {
			return nil, fmt.Errorf("%w: blocked cleanup target is unresolved", materialize.ErrOwnedCleanupUnsupported)
		}
		cleanup, err = prepareBlockedOwnedRemoval(ctx, opts, ec, *target, name, fromURL, selectedPath, shadowRef, resolvedKind)
		if err != nil {
			return nil, err
		}
		defer cleanup.release()
	}

	// Remove the resource from the specific registry entry. shadowRef ensures a
	// newly created shadow entry (cross-layer case) passes ref-required
	// validation for git registries.
	editEntry := mergedEntry
	if entryInTarget {
		editEntry = targetEntry
	}
	handled, disableErr := config.EditV4ResourceSelection(configPath, declaringPath, editEntry, resolvedKind, name, config.V4SelectionDisable)
	if handled {
		slog.Debug("remove v4 selection staged", "kind", resolvedKind.String(), "scope", opts.Scope, "error", disableErr)
	} else if disableErr == nil {
		if strings.TrimSpace(shadowRef) != "" {
			disableErr = config.DisableResourceWithShadowFallbackAtRef(configPath, fromURL, selectedPath, shadowRef, resolvedKind, name)
		} else {
			disableErr = config.DisableResourceWithShadowFallback(configPath, fromURL, selectedPath, shadowRef, resolvedKind, name)
		}
	}
	if err := disableErr; err != nil {
		return nil, err
	}
	if cleanup != nil {
		if err := cleanup.commit(opts); err != nil {
			return nil, err
		}
		return &RemoveResult{RemovedName: name}, nil
	}

	if err := syncAfterRemove(ctx, opts, WithIgnoreBlockedRegistries()); err != nil {
		return nil, err
	}
	return &RemoveResult{RemovedName: name}, nil
}

func removeMarketplaceAndSync(ctx context.Context, repoRoot, configPath string, mutate func() (*config.FileSnapshot, config.ConfigFileCommit, error)) (retErr error) {
	snapshot, commit, err := mutate()
	if err != nil {
		return err
	}
	defer func() {
		if retErr == nil {
			return
		}
		if rollbackErr := snapshot.RestoreIfCurrent(commit); rollbackErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("rollback Marketplace config: %w", rollbackErr))
		}
	}()
	if err := silentSync(ctx, repoRoot); err != nil {
		return err
	}
	current, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read Marketplace config after remove: %w", err)
	}
	if !bytes.Equal(current, commit.Bytes) {
		return fmt.Errorf("marketplace config changed concurrently during remove")
	}
	return nil
}

func marketplaceCollectionByName(cfg *config.Config, name string, kind ir.ResourceKind, hasKind bool) (config.MarketplaceResourcePin, config.MarketplaceEntry, bool) {
	if cfg == nil || (hasKind && marketplaceProtocolKind(kind) != "collection") {
		return config.MarketplaceResourcePin{}, config.MarketplaceEntry{}, false
	}
	var found config.MarketplaceResourcePin
	var foundEntry config.MarketplaceEntry
	count := 0
	for _, entry := range cfg.Marketplaces {
		for _, pin := range entry.Resources {
			if pin.Kind == "collection" && pin.Name == strings.TrimSpace(name) {
				found, foundEntry, count = pin, entry, count+1
			}
		}
	}
	return found, foundEntry, count == 1
}

func marketplaceDirectPinByName(cfg *config.Config, name string, kind ir.ResourceKind, hasKind bool) (config.MarketplaceResourcePin, config.MarketplaceEntry, bool) {
	if cfg == nil {
		return config.MarketplaceResourcePin{}, config.MarketplaceEntry{}, false
	}
	var found config.MarketplaceResourcePin
	var foundEntry config.MarketplaceEntry
	count := 0
	for _, entry := range cfg.Marketplaces {
		for _, pin := range entry.Resources {
			if pin.Kind == "collection" || pin.Name != strings.TrimSpace(name) {
				continue
			}
			if hasKind && marketplaceProtocolKind(kind) != pin.Kind {
				continue
			}
			if !pin.Direct && len(pin.SelectedBy.List()) > 0 {
				continue
			}
			found, foundEntry, count = pin, entry, count+1
		}
	}
	return found, foundEntry, count == 1
}

func marketplaceCollectionOnlyPinByName(cfg *config.Config, name string, kind ir.ResourceKind, hasKind bool) (config.MarketplaceResourcePin, bool) {
	if cfg == nil {
		return config.MarketplaceResourcePin{}, false
	}
	for _, entry := range cfg.Marketplaces {
		for _, pin := range entry.Resources {
			if pin.Kind == "collection" || pin.Name != strings.TrimSpace(name) {
				continue
			}
			if hasKind && marketplaceProtocolKind(kind) != pin.Kind {
				continue
			}
			if !pin.Direct && len(pin.SelectedBy.List()) > 0 {
				return pin, true
			}
		}
	}
	return config.MarketplaceResourcePin{}, false
}

func inferMarketplaceRemoveScope(source config.Source) Scope {
	if source == config.UserPersonal || source == config.Organization {
		return ScopeUser
	}
	if source == config.RepoPersonal {
		return ScopeLocal
	}
	return ScopeRepo
}

// enrichRemoveError wraps config-level disambiguation errors with an
// actionable hint that tells the user which --from/--path flags to use.
func enrichRemoveError(err error, name string, target *resolver.ResolvedArtifact) error {
	if err == nil || target == nil {
		return err
	}
	if !strings.Contains(err.Error(), "matches multiple entries") {
		return err
	}
	kindFlag := ""
	if target.Kind != ir.KindSkill {
		kindFlag = fmt.Sprintf(" --kind %s", skill.KindLabel(target.Kind))
	}
	scopeFlag := ""
	if target.Source.IsMachineScope() {
		scopeFlag = " --scope user"
	}
	hint := fmt.Sprintf(
		"use: agent-marketplace remove%s%s %q --from %q --path %q",
		kindFlag, scopeFlag, name, target.RegistryURL, target.RegistryPath,
	)
	return fmt.Errorf("%w\nto target a specific registry entry, %s", err, hint)
}
