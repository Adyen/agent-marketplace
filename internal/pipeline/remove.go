package pipeline

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
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

// RemoveOptions holds the arguments for a remove operation.
type RemoveOptions struct {
	RepoRoot       string
	ResourceName   string
	Kind           ir.ResourceKind
	HasKind        bool
	Scope          Scope
	ScopeSpecified bool
	FromURL        string // --from flag: ad-hoc registry URL
	FromRef        string // --ref flag: git ref for --from
	FromPath       string // --path flag: subdirectory for --from
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
}

// Remove removes a resource from the appropriate config layer and re-syncs.
// It handles collection detection, scope inference, scope boundary validation,
// per-scope config editing, and --from ad-hoc registry removal.
func Remove(ctx context.Context, opts RemoveOptions) (result *RemoveResult, retErr error) {
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

	mergedCfg, repoCfg, err := loadRemoveConfig(ctx, opts.RepoRoot)
	if err != nil {
		return nil, err
	}

	// Check if the arg refers to a collection in the merged config.
	if isCollectionInstalled(mergedCfg, name) {
		effective := opts.Scope
		if !opts.ScopeSpecified {
			effective = ScopeRepo // collections are repo-scoped by default
		}
		configPath, err := scopedConfigPath(opts.RepoRoot, effective)
		if err != nil {
			return nil, fmt.Errorf("resolve config path: %w", err)
		}
		if err := removeOrExcludeCollection(configPath, name); err != nil {
			return nil, err
		}
		if err := silentSync(ctx, opts.RepoRoot); err != nil {
			return nil, err
		}
		return &RemoveResult{RemovedName: name, IsCollection: true}, nil
	}

	resolvedResult := Resolve(ctx, ResolveOptions{RepoRoot: opts.RepoRoot, SkipInventory: true})
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
		if err := excludeCollectionMember(opts.RepoRoot, effective, target, name); err != nil {
			return nil, err
		}
		if err := silentSync(ctx, opts.RepoRoot); err != nil {
			return nil, err
		}
		return &RemoveResult{RemovedName: name, CollectionVia: target.Via, InferredUserScope: inferredUserScope}, nil
	}

	if err := removeResource(opts.RepoRoot, effective, repoCfg, target, name, opts.ScopeSpecified); err != nil {
		return nil, enrichRemoveError(err, name, target)
	}

	if err := silentSync(ctx, opts.RepoRoot); err != nil {
		return nil, err
	}
	return &RemoveResult{RemovedName: name, InferredUserScope: inferredUserScope}, nil
}

func disableCollectionInRegistry(configPath, registryURL, registryPath, ref, name string) error {
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

func removeOrExcludeCollection(configPath, name string) error {
	rootCfg, err := config.LoadRootConfigFromPath(configPath)
	if err != nil {
		return fmt.Errorf("load root config %q: %w", configPath, err)
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

func loadRemoveConfig(ctx context.Context, repoRoot string) (mergedCfg *config.Config, repoCfg *config.Config, err error) {
	ec := config.LoadEffective(ctx, config.LoadEffectiveOptions{RepoRoot: repoRoot})
	if len(ec.Errors) > 0 && !config.OnlyRegistryURLBlockedErrors(ec.Errors) {
		return nil, nil, errors.Join(ec.Errors...)
	}
	return ec.Merged, ec.Repo.Config, nil
}

func excludeCollectionMember(repoRoot string, scope Scope, target *resolver.ResolvedArtifact, name string) error {
	switch scope {
	case ScopeUser:
		globalPath, err := config.GlobalConfigPath()
		if err != nil {
			return err
		}
		// When the registry exists in the user config, add the exclude there.
		// Otherwise, create a shadow entry so cross-layer exclusion works.
		return config.DisableResourceWithShadowFallback(globalPath, target.RegistryURL, target.RegistryPath, target.Ref, target.Kind, name)
	case ScopeLocal:
		if target.Source == config.RepoPersonal {
			return config.AddKindExcludeToRegistryInConfig(config.LocalConfigPath(repoRoot), target.RegistryURL, target.RegistryPath, target.Kind, name)
		}
		return config.AddKindExcludeToLocalConfig(repoRoot, target.Kind, name)
	default:
		return config.AddKindExcludeToRegistryInConfig(config.ProjectConfigPath(repoRoot), target.RegistryURL, target.RegistryPath, target.Kind, name)
	}
}

func removeResource(repoRoot string, scope Scope, repoCfg *config.Config, target *resolver.ResolvedArtifact, name string, scopeSpecified bool) error {
	switch scope {
	case ScopeUser:
		globalPath, err := config.GlobalConfigPath()
		if err != nil {
			return err
		}
		// DisableResourceWithShadowFallback handles three cases:
		//  1. Registry exists in user config with exact URL+path → modify that entry
		//  2. Registry exists with normalized-URL match (e.g. .git suffix) → add exclude there
		//  3. Registry not in user config at all → create shadow entry
		// Pass the resolved artifact's RegistryPath so case 2 can find
		// the right entry when multiple entries share a URL.
		return config.DisableResourceWithShadowFallback(globalPath, target.RegistryURL, target.RegistryPath, target.Ref, target.Kind, name)

	case ScopeLocal:
		if target.Source == config.RepoPersonal {
			localCfgPath := config.LocalConfigPath(repoRoot)
			rootCfg, err := config.LoadRootConfigFromPath(localCfgPath)
			if err != nil {
				return fmt.Errorf("load local root config: %w", err)
			}
			if rootCfg != nil && registryEntryInList(rootCfg.Registries, target.RegistryURL, target.RegistryPath) {
				if err := config.RemoveResourceFromRegistryInConfig(localCfgPath, target.RegistryURL, target.RegistryPath, target.Kind, name); err != nil {
					return fmt.Errorf("remove resource from registry entry: %w", err)
				}
				return config.AddKindExcludeToRegistryInConfig(localCfgPath, target.RegistryURL, target.RegistryPath, target.Kind, name)
			}
			localCfg, err := config.LoadLocalConfigFromPath(localCfgPath)
			if err != nil {
				return fmt.Errorf("load local config: %w", err)
			}
			if localCfg != nil && localRegistryEntryExists(localCfg, target.RegistryURL, target.RegistryPath) {
				if err := config.DisableResourceWithShadowFallback(localCfgPath, target.RegistryURL, target.RegistryPath, target.Ref, target.Kind, name); err != nil {
					return err
				}
				return config.AddKindExcludeToLocalConfig(repoRoot, target.Kind, name)
			}
			// A top-level local selector owns the resource directly, so remove
			// that selection and retain the established top-level exclude.
			if err := config.RemoveKindExcludeFromLocalConfig(repoRoot, target.Kind, name); err != nil {
				return fmt.Errorf("clear stale exclude from local config: %w", err)
			}
			if err := config.RemoveSkillFromLocalConfig(repoRoot, name); err != nil {
				return fmt.Errorf("remove skill from local config include: %w", err)
			}
			return config.AddKindExcludeToLocalConfig(repoRoot, target.Kind, name)
		}
		return config.AddKindExcludeToLocalConfig(repoRoot, target.Kind, name)

	default:
		if repoCfg == nil {
			return fmt.Errorf("no repo config found; cannot remove %q", name)
		}
		// When the user didn't explicitly specify a scope, or the skill
		// originates from a machine-scope layer, and the project config
		// doesn't directly select this resource (neither explicitly nor
		// via tag matching), fall back to a local exclude so the
		// committed project config is not modified without intent.
		if (!scopeSpecified || target.Source.IsMachineScope()) && !projectExplicitlySelects(repoCfg, *target) && !projectSelectsByTag(repoCfg, *target) {
			return config.AddKindExcludeToLocalConfig(repoRoot, target.Kind, name)
		}
		configPath := config.ProjectConfigPath(repoRoot)
		return config.DisableResourceWithShadowFallback(configPath, target.RegistryURL, target.RegistryPath, target.Ref, target.Kind, name)
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

// registryEntryInList reports whether the list contains an entry that exactly
// matches url and path (the same exact-match rule the editor uses when writing
// to a specific scope's config).
func registryEntryInList(entries []config.RegistryEntry, url, path string) bool {
	for _, entry := range entries {
		if strings.TrimSpace(entry.URL) == url && strings.TrimSpace(entry.Path) == path {
			return true
		}
	}
	return false
}

func registryEntryCount(entries []config.RegistryEntry, url, path string) int {
	count := 0
	normalizedURL := config.NormalizeGitURL(strings.TrimSpace(url))
	for _, entry := range entries {
		if config.NormalizeGitURL(strings.TrimSpace(entry.URL)) != normalizedURL {
			continue
		}
		if path != "" && strings.TrimSpace(entry.Path) != strings.TrimSpace(path) {
			continue
		}
		count++
	}
	return count
}

// findRegistryInMerged finds a registry entry in the merged config matching
// url (normalized) and path. When path is empty it matches any entry with the
// URL (URL-only match). It returns the matched entry (carrying its Source
// layer) so callers can validate scope boundaries.
func findRegistryInMerged(cfg *config.Config, url, path string) (config.RegistryEntry, bool) {
	if cfg == nil {
		return config.RegistryEntry{}, false
	}
	nu := config.NormalizeGitURL(strings.TrimSpace(url))
	for _, entry := range cfg.Registries {
		if config.NormalizeGitURL(strings.TrimSpace(entry.URL)) != nu {
			continue
		}
		if path == "" || strings.TrimSpace(entry.Path) == strings.TrimSpace(path) {
			return entry, true
		}
	}
	return config.RegistryEntry{}, false
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
// requested installation scope before checking ambiguity. Machine and repo
// occurrences are intentionally retained by the resolver, so a plain name
// lookup can now find two valid artifacts. An explicit --scope identifies the
// occurrence the user intends to edit; if that scope has no match, retain the
// unfiltered set so existing scope-boundary errors remain actionable.
func resolvedResourceByNameInScope(resolved []resolver.ResolvedArtifact, name string, kind ir.ResourceKind, hasKind bool, cfg *config.Config, scope Scope, scopeSpecified bool) (*resolver.ResolvedArtifact, error) {
	matches := resourceMatches(resolved, name, kind, hasKind)
	if scopeSpecified {
		scoped := make([]resolver.ResolvedArtifact, 0, len(matches))
		for _, match := range matches {
			if sourceMatchesRemoveScope(match.Source, scope) {
				scoped = append(scoped, match)
			}
		}
		if len(scoped) > 0 {
			matches = scoped
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
	nu := config.NormalizeGitURL(strings.TrimSpace(url))
	wantPath := strings.TrimSpace(path)
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
	ep := strings.TrimSpace(entryPath)
	if ep == "" {
		return true
	}
	ap := strings.TrimSpace(artifactPath)
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
		if strings.TrimSpace(entry.URL) != strings.TrimSpace(registryURL) {
			continue
		}
		if registryPath != "" {
			ep := strings.TrimSpace(entry.Path)
			rp := strings.TrimSpace(registryPath)
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
	ec := config.LoadEffective(ctx, config.LoadEffectiveOptions{RepoRoot: repoRoot})
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
	entryInTarget := cfg != nil && registryEntryInList(cfg.Registries, fromURL, fromPath)

	// The merged config finds entries defined by includes or lower layers.
	mergedCfg, _, err := loadRemoveConfig(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	// A matching entry outside the target root must remain suppressed.
	mergedEntry, entryInMerged := findRegistryInMerged(mergedCfg, fromURL, fromPath)
	var rootEntries []config.RegistryEntry
	if cfg != nil {
		rootEntries = cfg.Registries
	}
	requiresShadow := entryInMerged && registryEntryCount(mergedCfg.Registries, fromURL, fromPath) > registryEntryCount(rootEntries, fromURL, fromPath)
	// Prefer the defining layer's ref when writing a shadow entry, so git
	// shadow entries pass ref-required validation even if the user omitted --ref.
	shadowRef := fromRef
	if entryInMerged && strings.TrimSpace(mergedEntry.Ref) != "" {
		shadowRef = mergedEntry.Ref
	}

	// An empty name targets the registry.
	name := strings.TrimSpace(opts.ResourceName)
	if name == "" {
		if requiresShadow {
			if err := validateRegistryScopeBoundary(mergedEntry.Source, opts.Scope, fromURL); err != nil {
				return nil, err
			}
			if err := config.SetRegistryExcludeAllInConfig(configPath, fromURL, shadowRef, fromPath); err != nil {
				return nil, fmt.Errorf("exclude registry in %s config: %w", opts.Scope, err)
			}
			if err := silentSync(ctx, repoRoot, WithIgnoreBlockedRegistries()); err != nil {
				return nil, err
			}
			return &RemoveResult{ExcludedRegistry: true, ExcludedURL: fromURL, ExcludedScope: opts.Scope}, nil
		}
		// Registry is defined at this scope (and not inherited from below):
		// deleting the entry fully removes it.
		if entryInTarget {
			if err := config.RemoveRegistryEntryFromConfig(configPath, fromURL, fromPath); err != nil {
				return nil, fmt.Errorf("remove registry entry from %s config: %w", opts.Scope, err)
			}
			if err := silentSync(ctx, repoRoot, WithIgnoreBlockedRegistries()); err != nil {
				return nil, err
			}
			return &RemoveResult{RemovedRegistry: true}, nil
		}
		return nil, fmt.Errorf("registry entry for %q (path=%q) not found in any config; nothing to remove", fromURL, fromPath)
	}

	// With a name: remove the resource from the --from registry entry. The
	// entry may live in the target config (delete from include / add exclude)
	// or in a lower layer (write a shadow exclude). Either way it must exist
	// somewhere; otherwise it's a typo.
	if !entryInTarget && !entryInMerged {
		return nil, fmt.Errorf("registry entry for %q (path=%q) not found in any config", fromURL, fromPath)
	}

	// A named --from collection remains scoped to the selected registry.
	if collectionInstalledInRegistry(mergedCfg, name, fromURL, fromPath) {
		if !entryInTarget {
			if err := validateRegistryScopeBoundary(mergedEntry.Source, opts.Scope, fromURL); err != nil {
				return nil, err
			}
		}
		if err := disableCollectionInRegistry(configPath, fromURL, fromPath, shadowRef, name); err != nil {
			return nil, err
		}
		if err := silentSync(ctx, repoRoot, WithIgnoreBlockedRegistries()); err != nil {
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
	resolvedResult := Resolve(ctx, ResolveOptions{RepoRoot: repoRoot, IgnoreBlockedRegistries: true, SkipInventory: true})
	if len(resolvedResult.Errors) > 0 {
		return nil, errors.Join(resolvedResult.Errors...)
	}
	target, err := resolvedResourceInRegistry(resolvedResult.Resolved, name, opts.Kind, opts.HasKind, fromURL, fromPath)
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

	// Remove the resource from the specific registry entry. shadowRef ensures a
	// newly created shadow entry (cross-layer case) passes ref-required
	// validation for git registries.
	if err := config.DisableResourceWithShadowFallback(configPath, fromURL, fromPath, shadowRef, resolvedKind, name); err != nil {
		return nil, err
	}

	if err := silentSync(ctx, repoRoot, WithIgnoreBlockedRegistries()); err != nil {
		return nil, err
	}
	return &RemoveResult{RemovedName: name}, nil
}

func collectionInstalledInRegistry(cfg *config.Config, name, registryURL, registryPath string) bool {
	if cfg == nil {
		return false
	}
	normalizedURL := config.NormalizeGitURL(strings.TrimSpace(registryURL))
	for _, entry := range cfg.Registries {
		if config.NormalizeGitURL(strings.TrimSpace(entry.URL)) != normalizedURL {
			continue
		}
		if registryPath != "" && strings.TrimSpace(entry.Path) != strings.TrimSpace(registryPath) {
			continue
		}
		for _, selected := range entry.Collections.Include {
			if strings.TrimSpace(selected) == name {
				return true
			}
		}
	}
	return false
}

// isCollectionInstalled reports whether any layer of the merged config
// contains a collection ref with the given name, including registry-scoped
// collection selections.
func isCollectionInstalled(cfg *config.Config, name string) bool {
	if cfg == nil {
		return false
	}
	name = strings.TrimSpace(name)
	for _, n := range cfg.Collections.Exclude {
		if strings.TrimSpace(n) == name {
			return false
		}
	}
	for _, n := range cfg.Collections.Include {
		if strings.TrimSpace(n) == name {
			return true
		}
	}
	for _, entry := range cfg.Registries {
		for _, n := range entry.Collections.Include {
			if strings.TrimSpace(n) == name {
				return true
			}
		}
	}
	return false
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
