package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
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

	if collection, entry, ok := marketplaceCollectionByName(mergedCfg, name, opts.Kind, opts.HasKind); ok {
		if entry.Source == config.Organization {
			return nil, fmt.Errorf("collection %q is required and managed externally", name)
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
			return nil, fmt.Errorf("%s %q is required and managed externally", skill.KindLabel(opts.Kind), name)
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

	// A collection leaf is not a registry resource and has no safe legacy
	// removal operation. Stop here instead of allowing the resolver to fall
	// through to a same-named legacy registry resource.
	if pin, ok := marketplaceCollectionOnlyPinByName(mergedCfg, name, opts.Kind, opts.HasKind); ok {
		collectionName := "an explicitly selected collection"
		if selections := pin.SelectedBy.List(); len(selections) > 0 {
			collectionName = selections[0].Name
		}
		return nil, fmt.Errorf("marketplace %s %q is selected only through collection %s; remove that collection instead", pin.Kind, name, collectionName)
	}

	// Check if the arg refers to a legacy registry collection in the merged config.
	if isCollectionInstalled(mergedCfg, name) {
		effective := opts.Scope
		if !opts.ScopeSpecified {
			effective = ScopeRepo
		}
		configPath, err := scopedConfigPath(opts.RepoRoot, effective)
		if err != nil {
			return nil, fmt.Errorf("resolve config path: %w", err)
		}
		if err := config.RemoveCollectionFromConfig(configPath, name); err != nil {
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

	target, err := resolvedResourceByName(resolved, name, opts.Kind, opts.HasKind, mergedCfg)
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
			// Check whether the registry entry exists in config.local.yaml.
			// When it does, remove from include and add exclude in that entry.
			// When it doesn't (the resource was installed via the top-level
			// selector), remove from the top-level selector and add to the
			// top-level exclude list instead.
			localCfg, err := config.LoadLocalConfig()
			if err != nil {
				return fmt.Errorf("load local config: %w", err)
			}
			if localCfg != nil && localRegistryEntryExists(localCfg, target.RegistryURL, target.RegistryPath) {
				if err := config.RemoveResourceFromRegistryInConfig(localCfgPath, target.RegistryURL, target.RegistryPath, target.Kind, name); err != nil {
					return fmt.Errorf("remove resource from registry entry: %w", err)
				}
				return config.AddKindExcludeToRegistryInConfig(localCfgPath, target.RegistryURL, target.RegistryPath, target.Kind, name)
			}
			// Resource is in the top-level selector (or a new shadow entry
			// is needed): remove from the top-level include and add a
			// registry-level exclude so cross-layer exclusion suppresses it.
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

	// Load the target-scope config to see whether the --from registry entry
	// is defined there. LoadConfigFromPath does not require apiVersion for
	// local configs (config.local.yaml), which are generated without it.
	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config %q: %w", configPath, err)
	}
	entryInTarget := cfg != nil && registryEntryInList(cfg.Registries, fromURL, fromPath)

	// Load the merged config so we can (a) validate the registry exists at
	// all, and (b) handle the cross-layer case where the entry is enabled in
	// a lower-precedence layer (e.g. committed project config) but the user
	// wants to suppress it at a higher scope (e.g. config.local.yaml).
	mergedCfg, _, err := loadRemoveConfig(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	// findRegistryInMerged returns the lowest-precedence layer that defines the
	// registry (MergeLayers appends org→user→repo→local in order). When that
	// layer is below the target scope, the registry is inherited and cannot be
	// deleted at this scope — it must be suppressed via exclude_all instead.
	mergedEntry, entryInMerged := findRegistryInMerged(mergedCfg, fromURL, fromPath)
	targetSource := scopeToConfigSource(opts.Scope)
	inheritedFromLower := entryInMerged && mergedEntry.Source.Precedence() < targetSource.Precedence()
	// Prefer the defining layer's ref when writing a shadow entry, so git
	// shadow entries pass ref-required validation even if the user omitted --ref.
	shadowRef := fromRef
	if entryInMerged && strings.TrimSpace(mergedEntry.Ref) != "" {
		shadowRef = mergedEntry.Ref
	}

	// Without a name: suppress / remove the entire registry at this scope.
	name := strings.TrimSpace(opts.ResourceName)
	if name == "" {
		// Registry inherited from a lower layer: deleting a target-scope entry
		// would not stop the inherited resources from resolving, so suppress
		// them with an exclude_all shadow entry instead.
		if inheritedFromLower {
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

	// Check if it's a collection.
	if isCollectionInstalled(mergedCfg, name) {
		if err := config.RemoveCollectionFromConfig(configPath, name); err != nil {
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

// isCollectionInstalled reports whether any layer of the merged config
// contains a collection ref with the given name, including registry-scoped
// collection selections.
func isCollectionInstalled(cfg *config.Config, name string) bool {
	if cfg == nil {
		return false
	}
	name = strings.TrimSpace(name)
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
