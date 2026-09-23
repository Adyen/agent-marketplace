package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

func ExcludeAllV4SourceInConfig(stagedPath, originalPath string, source Source, sourceName string) error {
	return editV4RootConfig(stagedPath, originalPath, source, func(root *yaml.Node, parsed *V4Config) (bool, error) {
		bound, ok := parsed.Sources[sourceName]
		if !ok || bound.Kind == V4SourceMarketplace {
			return false, v4err("v4.source-kind", "sources."+sourceName, 0, "source exclusion requires a git or directory source")
		}
		resources := ensureTopLevelMapping(root, "resources")
		if resources == nil {
			return false, v4err("v4.type", "resources", 0, "resources must be a mapping")
		}
		group := findMappingValue(resources, sourceName)
		if group == nil {
			group = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setMappingValue(resources, sourceName, group)
		} else if group.Kind == yaml.SequenceNode {
			includes := group
			group = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setMappingValue(group, "include", includes)
			setMappingValue(resources, sourceName, group)
		}
		if group.Kind != yaml.MappingNode {
			return false, v4err("v4.type", "resources."+sourceName, 0, "resource group must be a mapping")
		}
		excludes, err := ensureMappingSequence(group, "exclude")
		if err != nil {
			return false, err
		}
		for _, item := range excludes.Content {
			if item.Value == "*" {
				return false, nil
			}
		}
		excludes.Content = append(excludes.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "*"})
		return true, nil
	})
}

func RemoveV4MarketplaceRequestInConfig(stagedPath, originalPath string, source Source, sourceName string, pin MarketplaceResourcePin) error {
	locator := fmt.Sprintf("%s/%s/%s", pin.RegistrySlug, v4MigrationMarketplaceKind(pin.Kind), pin.Name)
	if _, _, _, err := parseV4Locator(locator, sourceName); err != nil {
		return v4err("v4.locator", locator, 0, "%v", err)
	}
	return editV4RootConfig(stagedPath, originalPath, source, func(root *yaml.Node, parsed *V4Config) (bool, error) {
		bound, ok := parsed.Sources[sourceName]
		if !ok || bound.Kind != V4SourceMarketplace {
			return false, v4err("v4.source-kind", "sources."+sourceName, 0, "Marketplace request requires a Marketplace source")
		}
		resources := findMappingValue(root, "resources")
		group := findMappingValue(resources, sourceName)
		if group == nil {
			return false, v4err("v4.include-owned", "resources."+sourceName, 0, "Marketplace request is not in the editable root")
		}
		var includes *yaml.Node
		switch group.Kind {
		case yaml.SequenceNode:
			includes = group
		case yaml.MappingNode:
			includes = findMappingValue(group, "include")
		}
		if includes == nil || includes.Kind != yaml.SequenceNode {
			return false, v4err("v4.include-owned", "resources."+sourceName, 0, "Marketplace request is not in the editable root")
		}
		filtered := includes.Content[:0]
		removed := false
		for _, item := range includes.Content {
			selected := item.Kind == yaml.ScalarNode && item.Value == locator || item.Kind == yaml.MappingNode && len(item.Content) == 2 && item.Content[0].Value == locator
			if selected {
				removed = true
				continue
			}
			filtered = append(filtered, item)
		}
		if !removed {
			return false, v4err("v4.include-owned", "resources."+sourceName, 0, "Marketplace request is not in the editable root")
		}
		includes.Content = filtered
		if len(filtered) == 0 {
			if group.Kind == yaml.SequenceNode {
				removeMappingKeyByName(resources, sourceName)
			} else {
				removeMappingKeyByName(group, "include")
				if len(group.Content) == 0 {
					removeMappingKeyByName(resources, sourceName)
				}
			}
		}
		if len(resources.Content) == 0 {
			removeMappingKeyByName(root, "resources")
		}
		return true, nil
	})
}

func RemoveV4RegistrySourceInConfig(stagedPath, originalPath string, source Source, entry RegistryEntry) error {
	canonical, err := canonicalConfigPath(originalPath)
	if err != nil {
		return err
	}
	return editV4RootConfig(stagedPath, originalPath, source, func(root *yaml.Node, parsed *V4Config) (bool, error) {
		sources := findMappingValue(root, "sources")
		for alias, candidate := range parsed.Sources {
			if candidate.Kind == V4SourceMarketplace || candidate.URL != entry.URL || candidate.Ref != entry.Ref || candidate.Path != NormalizeRegistryPath(entry.Path) {
				continue
			}
			if candidate.File != canonical || !removeMappingKeyByName(sources, alias) {
				return false, v4err("v4.include-owned", "sources."+alias, 0, "source is owned by an included configuration")
			}
			if len(sources.Content) == 0 {
				removeMappingKeyByName(root, "sources")
			}
			if resources := findMappingValue(root, "resources"); resources != nil {
				removeMappingKeyByName(resources, alias)
				if len(resources.Content) == 0 {
					removeMappingKeyByName(root, "resources")
				}
			}
			return true, nil
		}
		return false, v4err("v4.source-not-found", "sources", 0, "registry source is not defined in this v4 root")
	})
}

// DisableResourceWithShadowFallback disables a resource in the target config.
// When the registry entry already exists in the config, it delegates to
// DisableResourceInRegistryInConfig (remove from include + add exclude).
// When the registry entry does NOT exist in the config — because the
// resource originates from a different layer (e.g. org config) — it adds
// the exclude to an existing entry for the same logical registry (matched
// by normalized URL) or, if none exists, creates a shadow registry entry
// with just the exclude so that the resolver's cross-layer exclusion logic
// can suppress the resource during resolution.
//
// The ref parameter is used only when creating a brand-new shadow entry;
// it is required for git registries so the entry passes validation and
// shares the same cache as the original entry.
func DisableResourceWithShadowFallback(configPath, registryURL, registryPath, ref string, kind ir.ResourceKind, name string) error {
	return disableResourceWithShadowFallback(configPath, registryURL, registryPath, ref, false, kind, name)
}

// DisableResourceWithShadowFallbackAtRef disables a resource on a URL/path/ref entry.
func DisableResourceWithShadowFallbackAtRef(configPath, registryURL, registryPath, ref string, kind ir.ResourceKind, name string) error {
	return disableResourceWithShadowFallback(configPath, registryURL, registryPath, ref, true, kind, name)
}

func disableResourceWithShadowFallback(configPath, registryURL, registryPath, ref string, requireRef bool, kind ir.ResourceKind, name string) error {
	registryURL = strings.TrimSpace(registryURL)
	registryPath = strings.TrimSpace(registryPath)

	cfg, err := LoadRootConfigFromPath(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load config %q: %w", configPath, err)
	}
	if cfg == nil {
		// Config doesn't exist or is empty — create a shadow entry.
		ensureRegistry := EnsureRegistryWithPathsInConfig
		if requireRef {
			ensureRegistry = EnsureRegistryWithPathsAtRefInConfig
		}
		if err := ensureRegistry(configPath, registryURL, ref, registryPath); err != nil {
			return fmt.Errorf("ensure shadow registry entry: %w", err)
		}
		if requireRef {
			return DisableResourceInRegistryInConfigAtRef(configPath, registryURL, registryPath, ref, kind, name)
		}
		return AddKindExcludeToRegistryInConfig(configPath, registryURL, registryPath, kind, name)
	}

	// Try exact URL+path+ref match first when the caller supplied a known ref.
	// This is deliberately separate from the legacy URL/path matching so other
	// resource removal callers retain their existing ambiguity semantics.
	if requireRef {
		if registryExistsInConfigAtRef(cfg, registryURL, registryPath, ref) {
			// An omitted path retains the legacy URL-only behavior only when it
			// identifies one entry at this ref. Multiple paths are ambiguous and
			// must not be resolved by file order.
			if registryPath == "" {
				if countEntriesByURLAndRef(cfg, registryURL, ref) > 1 {
					return fmt.Errorf("update config %q: registry %q at ref %q matches multiple entries; use --path to disambiguate", configPath, registryURL, ref)
				}
				if actualURL, actualPath := findNormalizedURLAndPathAtRef(cfg, registryURL, registryPath, ref); actualURL != "" {
					return DisableResourceInRegistryInConfigAtRef(configPath, actualURL, actualPath, ref, kind, name)
				}
			}
			return DisableResourceInRegistryInConfigAtRef(configPath, registryURL, registryPath, ref, kind, name)
		}
	} else if registryExistsInConfig(cfg, registryURL, registryPath) {
		// Try exact URL+path match first. When the registry exists with the
		// exact URL and path, use the standard disable path.
		// If registryPath is empty (URL-only match), there might be
		// multiple entries with the same URL. In that case, the
		// URL-only DisableResourceInRegistryInConfig would fail with
		// "matches multiple entries". Use exact-entry matching instead,
		// targeting the entry that actually selects this resource.
		if registryPath == "" && countEntriesByURL(cfg, registryURL) > 1 {
			if exactURL, exactPath := findEntrySelectingResource(cfg, registryURL, kind, name); exactURL != "" {
				return disableExactEntry(configPath, exactURL, exactPath, kind, name)
			}
			// Resource not in any entry's include — just add exclude
			// to the root entry (no path).
			return addKindExcludeToExactEntry(configPath, registryURL, "", kind, name)
		}
		return DisableResourceInRegistryInConfig(configPath, registryURL, registryPath, kind, name)
	}

	// No exact match. Check for a normalized-URL match — the target
	// config may have the same registry with a slightly different URL
	// (e.g. with/without .git suffix). In that case, add the exclude
	// to the existing entry rather than creating a duplicate entry.
	if requireRef {
		if existingURL, existingPath := findNormalizedURLAndPathAtRef(cfg, registryURL, registryPath, ref); existingURL != "" {
			return DisableResourceInRegistryInConfigAtRef(configPath, existingURL, existingPath, ref, kind, name)
		}
	} else if existingURL, existingPath := findNormalizedURLAndPath(cfg, registryURL, registryPath); existingURL != "" {
		// Same ambiguity issue: if there are multiple entries with the
		// same normalized URL and no path constraint, find the one that
		// actually selects this resource.
		if existingPath == "" && countEntriesByURL(cfg, existingURL) > 1 {
			if exactURL, exactPath := findEntrySelectingResource(cfg, existingURL, kind, name); exactURL != "" {
				return disableExactEntry(configPath, exactURL, exactPath, kind, name)
			}
			return addKindExcludeToExactEntry(configPath, existingURL, "", kind, name)
		}
		return addKindExcludeToExactEntry(configPath, existingURL, existingPath, kind, name)
	}

	// No entry in the target config at all. Create a shadow entry.
	ensureRegistry := EnsureRegistryWithPathsInConfig
	if requireRef {
		ensureRegistry = EnsureRegistryWithPathsAtRefInConfig
	}
	if err := ensureRegistry(configPath, registryURL, ref, registryPath); err != nil {
		return fmt.Errorf("ensure shadow registry entry: %w", err)
	}
	if requireRef {
		return DisableResourceInRegistryInConfigAtRef(configPath, registryURL, registryPath, ref, kind, name)
	}
	return AddKindExcludeToRegistryInConfig(configPath, registryURL, registryPath, kind, name)
}

func matchingRegistryCount(cfg *Config, url, path string) int {
	if cfg == nil {
		return 0
	}
	count := 0
	normalizedURL := NormalizeGitURL(strings.TrimSpace(url))
	normalizedPath := NormalizeRegistryPath(path)
	for _, entry := range cfg.Registries {
		if NormalizeGitURL(strings.TrimSpace(entry.URL)) != normalizedURL {
			continue
		}
		if NormalizeRegistryPath(entry.Path) == normalizedPath {
			count++
		}
	}
	return count
}

func hasIncludedRegistryDefinition(configPath string, root *Config, url, path string) (bool, error) {
	if root == nil || len(root.Include) == 0 {
		return false, nil
	}
	expanded, err := LoadConfigFromPath(configPath)
	if err != nil {
		return false, err
	}
	return matchingRegistryCount(expanded, url, path) > matchingRegistryCount(root, url, path), nil
}

func countEntriesByURL(cfg *Config, url string) int {
	count := 0
	for _, entry := range cfg.Registries {
		if NormalizeGitURL(strings.TrimSpace(entry.URL)) == NormalizeGitURL(url) {
			count++
		}
	}
	return count
}

func countEntriesByURLAndRef(cfg *Config, url, ref string) int {
	if cfg == nil {
		return 0
	}
	count := 0
	for _, entry := range cfg.Registries {
		if NormalizeGitURL(strings.TrimSpace(entry.URL)) != NormalizeGitURL(strings.TrimSpace(url)) {
			continue
		}
		if strings.TrimSpace(entry.Ref) == strings.TrimSpace(ref) {
			count++
		}
	}
	return count
}

// findEntrySelectingResource finds the first registry entry with the given
// URL that has the named resource in its per-kind include list (or would
// select it via default:true or tags). Returns the entry's (URL, Path) pair,
// or ("", "") if none found.
func findEntrySelectingResource(cfg *Config, url string, kind ir.ResourceKind, name string) (string, string) {
	for _, entry := range cfg.Registries {
		if NormalizeGitURL(strings.TrimSpace(entry.URL)) != NormalizeGitURL(url) {
			continue
		}
		if stringSliceContains(entry.SelectorForKind(kind).Include, name) {
			return strings.TrimSpace(entry.URL), strings.TrimSpace(entry.Path)
		}
		if entry.Default || entry.Required || len(entry.Tags) > 0 {
			// default:true / required:true auto-select all resources;
			// tags may select this resource without an explicit include.
			return strings.TrimSpace(entry.URL), strings.TrimSpace(entry.Path)
		}
	}
	return "", ""
}

// disableExactEntry removes a resource from one exact URL/path entry.
func disableExactEntry(configPath, registryURL, registryPath string, kind ir.ResourceKind, name string) error {
	return disableExactEntryWithRef(configPath, registryURL, registryPath, "", false, kind, name)
}

func disableExactEntryAtRef(configPath, registryURL, registryPath, ref string, kind ir.ResourceKind, name string) error {
	return disableExactEntryWithRef(configPath, registryURL, registryPath, ref, true, kind, name)
}

func disableExactEntryWithRef(configPath, registryURL, registryPath, ref string, requireRef bool, kind ir.ResourceKind, name string) error {
	// Remove from include.
	var err error
	if requireRef {
		err = removeValuesFromExactRegistryEntryAtRef(configPath, registryURL, registryPath, ref, kind, "include", []string{name})
	} else {
		err = removeValuesFromExactRegistryEntry(configPath, registryURL, registryPath, kind, "include", []string{name})
	}
	if err != nil {
		return fmt.Errorf("remove from include: %w", err)
	}
	// Add to exclude (always, to prevent re-selection by default/required/tags).
	if requireRef {
		return addValuesToExactRegistryEntryAtRef(configPath, registryURL, registryPath, ref, kind, "exclude", []string{name})
	}
	return addKindExcludeToExactEntry(configPath, registryURL, registryPath, kind, name)
}

// findNormalizedURLAndPath finds a matching normalized URL/path entry.
func findNormalizedURLAndPath(cfg *Config, url, registryPath string) (string, string) {
	return findNormalizedURLAndPathWithRef(cfg, url, registryPath, "", false)
}

func findNormalizedURLAndPathAtRef(cfg *Config, url, registryPath, ref string) (string, string) {
	return findNormalizedURLAndPathWithRef(cfg, url, registryPath, ref, true)
}

func findNormalizedURLAndPathWithRef(cfg *Config, url, registryPath, ref string, requireRef bool) (string, string) {
	norm := NormalizeGitURL(url)

	// First pass: look for normalized-URL match with matching path.
	// When registryPath is empty, this finds entries with no path.
	var fallbackURL, fallbackPath string
	for _, entry := range cfg.Registries {
		if NormalizeGitURL(entry.URL) != norm {
			continue
		}
		if requireRef && strings.TrimSpace(entry.Ref) != strings.TrimSpace(ref) {
			continue
		}
		entryPath := NormalizeRegistryPath(entry.Path)
		if registryPath != "" && entryPath == NormalizeRegistryPath(registryPath) {
			return strings.TrimSpace(entry.URL), entryPath
		}
		if registryPath == "" && entryPath == "" {
			return strings.TrimSpace(entry.URL), ""
		}
		// Remember the first entry with matching normalized URL as a
		// fallback for the case where no path-matching entry exists.
		if fallbackURL == "" {
			fallbackURL = strings.TrimSpace(entry.URL)
			fallbackPath = entryPath
		}
	}

	// When registryPath is non-empty and no exact path match was found,
	// don't fall back — adding to a different path entry would be wrong.
	if registryPath != "" {
		return "", ""
	}

	return fallbackURL, fallbackPath
}

// addKindExcludeToExactEntry adds a per-kind exclude to one registry entry.
func addKindExcludeToExactEntry(configPath, registryURL, registryPath string, kind ir.ResourceKind, name string) error {
	return addValuesToExactRegistryEntry(configPath, registryURL, registryPath, kind, "exclude", []string{name})
}

// registryExistsInConfig reports whether a registry entry with the given URL
// (and optionally path) exists in the config. When path is empty, any entry
// with the matching URL qualifies (URL-only match).
func registryExistsInConfig(cfg *Config, url, path string) bool {
	return registryExistsInConfigWithRef(cfg, url, path, "", false)
}

func registryExistsInConfigAtRef(cfg *Config, url, path, ref string) bool {
	return registryExistsInConfigWithRef(cfg, url, path, ref, true)
}

func registryExistsInConfigWithRef(cfg *Config, url, path, ref string, requireRef bool) bool {
	for _, entry := range cfg.Registries {
		if NormalizeGitURL(strings.TrimSpace(entry.URL)) != NormalizeGitURL(strings.TrimSpace(url)) {
			continue
		}
		if strings.TrimSpace(path) != "" && NormalizeRegistryPath(entry.Path) != NormalizeRegistryPath(path) {
			continue
		}
		if requireRef && strings.TrimSpace(entry.Ref) != strings.TrimSpace(ref) {
			continue
		}
		return true
	}
	return false
}

func RemoveResourceFromConfig(configPath string, kind ir.ResourceKind, name string) error {
	key := skill.ConfigKeyForKind(kind)
	if key == "" {
		return fmt.Errorf("unsupported resource kind %q", skill.KindLabel(kind))
	}
	return removeValueFromTopLevelSubSequence(configPath, key, "include", name)
}

func DisableResourceInRegistryInConfig(configPath, registryURL, registryPath string, kind ir.ResourceKind, name string) error {
	key := skill.ConfigKeyForKind(kind)
	if key == "" {
		return fmt.Errorf("unsupported resource kind %q", skill.KindLabel(kind))
	}
	registryURL = strings.TrimSpace(registryURL)
	if registryURL == "" {
		return fmt.Errorf("registry url is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%s name is required", skill.KindLabel(kind))
	}

	cfg, err := LoadRootConfigFromPath(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if cfg == nil {
		return nil
	}
	if cfg.APIVersion == APIVersionV4 {
		return v4WriterOptIn(configPath)
	}
	for _, entry := range cfg.Registries {
		if NormalizeGitURL(strings.TrimSpace(entry.URL)) != NormalizeGitURL(registryURL) {
			continue
		}
		// When registryPath is specified, match the configured entry path
		// exactly after normalizing separators and dot segments. A requested
		// descendant must not silently target its configured parent.
		if registryPath != "" && NormalizeRegistryPath(entry.Path) != NormalizeRegistryPath(registryPath) {
			continue
		}
		entryResources := entryResourcesForKind(entry, kind)
		if entryResources != nil && stringSliceContains(entryResources, name) {
			includedDefinition, err := hasIncludedRegistryDefinition(configPath, cfg, registryURL, entry.Path)
			if err != nil {
				return err
			}
			if err := RemoveResourceFromRegistryInConfig(configPath, registryURL, entry.Path, kind, name); err != nil {
				return err
			}
			// Retain an exclusion when policy or a lower included definition
			// could re-select the resource after removing the root include.
			if entry.Default || entry.Required || len(entry.Tags) > 0 || includedDefinition {
				return AddKindExcludeToRegistryInConfig(configPath, registryURL, entry.Path, kind, name)
			}
			return nil
		}
		return AddKindExcludeToRegistryInConfig(configPath, registryURL, entry.Path, kind, name)
	}
	return fmt.Errorf("registry %q not found in config", registryURL)
}

// DisableResourceInRegistryInConfigAtRef disables a resource on a URL/path/ref entry.
func DisableResourceInRegistryInConfigAtRef(configPath, registryURL, registryPath, registryRef string, kind ir.ResourceKind, name string) error {
	registryURL = strings.TrimSpace(registryURL)
	registryPath = strings.TrimSpace(registryPath)
	registryRef = strings.TrimSpace(registryRef)
	if registryURL == "" {
		return fmt.Errorf("registry url is required")
	}
	if registryRef == "" {
		return fmt.Errorf("registry ref is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%s name is required", skill.KindLabel(kind))
	}
	return disableExactEntryAtRef(configPath, registryURL, registryPath, registryRef, kind, name)
}

func AddKindExcludeToRegistryInConfig(configPath, registryURL, registryPath string, kind ir.ResourceKind, name string) error {
	key := skill.ConfigKeyForKind(kind)
	if key == "" {
		return fmt.Errorf("unsupported resource kind %q", skill.KindLabel(kind))
	}
	return addValuesToRegistrySubSequence(configPath, registryURL, registryPath, key, "exclude", []string{name})
}

func AddKindExcludeToConfig(configPath string, kind ir.ResourceKind, name string) error {
	key := skill.ConfigKeyForKind(kind)
	if key == "" {
		return fmt.Errorf("unsupported resource kind %q", skill.KindLabel(kind))
	}
	return addValuesToTopLevelSubSequence(configPath, key, "exclude", []string{name})
}

func RemoveKindExcludeFromConfig(configPath string, kind ir.ResourceKind, name string) error {
	key := skill.ConfigKeyForKind(kind)
	if key == "" {
		return fmt.Errorf("unsupported resource kind %q", skill.KindLabel(kind))
	}
	return removeValueFromTopLevelSubSequence(configPath, key, "exclude", name)
}

func RemoveKindExcludeFromRegistryInConfig(configPath, registryURL, registryPath string, kind ir.ResourceKind, name string) error {
	key := skill.ConfigKeyForKind(kind)
	if key == "" {
		return fmt.Errorf("unsupported resource kind %q", skill.KindLabel(kind))
	}
	return removeValueFromRegistrySubSequence(configPath, registryURL, registryPath, key, "exclude", name)
}

// RemoveAllKindExcludesFromRegistryEntries removes name from the per-kind
// exclude list of every registry entry in the config file. Uses exact-entry
// matching (URL+path) to avoid "matches multiple entries" errors. Idempotent.
func RemoveAllKindExcludesFromRegistryEntries(configPath string, kind ir.ResourceKind, name string) error {
	key := skill.ConfigKeyForKind(kind)
	if key == "" {
		return fmt.Errorf("unsupported resource kind %q", skill.KindLabel(kind))
	}

	cfg, err := LoadRootConfigFromPath(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if cfg == nil {
		return nil
	}

	for _, entry := range cfg.Registries {
		if err := removeValuesFromExactRegistryEntry(configPath, entry.URL, entry.Path, kind, "exclude", []string{name}); err != nil {
			return err
		}
	}
	return nil
}

func AddExcludeToConfig(configPath, skillName string) error {
	return AddKindExcludeToConfig(configPath, ir.KindSkill, skillName)
}

func RemoveExcludeFromConfig(configPath, skillName string) error {
	return RemoveKindExcludeFromConfig(configPath, ir.KindSkill, skillName)
}

func RemoveSkillFromConfig(configPath, skillName string) error {
	return RemoveResourceFromConfig(configPath, ir.KindSkill, skillName)
}

func DisableSkillInRegistryInConfig(configPath, registryURL, registryPath, skillName string) error {
	return DisableResourceInRegistryInConfig(configPath, registryURL, registryPath, ir.KindSkill, skillName)
}

// RemoveSkillFromGlobalConfig removes skillName from ~/.agents/config.yaml's
// top-level skills list. Idempotent; no-op if the file or entry is missing.
func RemoveSkillFromGlobalConfig(skillName string) error {
	path, err := GlobalConfigPath()
	if err != nil {
		return err
	}
	return RemoveSkillFromConfig(path, skillName)
}

// AddExcludeToGlobalConfig appends skillName to the exclude list in
// ~/.agents/config.yaml. Idempotent; creates file and directory if needed.
func AddExcludeToGlobalConfig(skillName string) error {
	path, err := GlobalConfigPath()
	if err != nil {
		return err
	}
	return AddExcludeToConfig(path, skillName)
}

// RemoveExcludeFromGlobalConfig removes skillName from the exclude list in
// ~/.agents/config.yaml. Idempotent.
func RemoveExcludeFromGlobalConfig(skillName string) error {
	path, err := GlobalConfigPath()
	if err != nil {
		return err
	}
	return RemoveKindExcludeFromConfig(path, ir.KindSkill, skillName)
}

// AddKindExcludeToRegistryInGlobalConfig adds name to the per-kind exclude
// list of the given registry entry in ~/.agents/config.yaml. Idempotent.
func AddKindExcludeToRegistryInGlobalConfig(registryURL, registryPath string, kind ir.ResourceKind, name string) error {
	path, err := GlobalConfigPath()
	if err != nil {
		return err
	}
	return AddKindExcludeToRegistryInConfig(path, registryURL, registryPath, kind, name)
}

// RemoveKindExcludeFromGlobalConfig removes name from the per-kind exclude
// lists across all registry entries in ~/.agents/config.yaml. Idempotent.
func RemoveKindExcludeFromGlobalConfig(kind ir.ResourceKind, name string) error {
	path, err := GlobalConfigPath()
	if err != nil {
		return err
	}
	return RemoveAllKindExcludesFromRegistryEntries(path, kind, name)
}

// AddKindExcludeToLocalConfig appends name to the per-kind exclude list in
// <repoRoot>/.agents/config.local.yaml. Idempotent.
func AddKindExcludeToLocalConfig(repoRoot string, kind ir.ResourceKind, name string) error {
	return AddKindExcludeToConfig(LocalConfigPath(repoRoot), kind, name)
}

// RemoveKindExcludeFromLocalConfig removes name from the per-kind exclude
// list in <repoRoot>/.agents/config.local.yaml. Idempotent.
func RemoveKindExcludeFromLocalConfig(repoRoot string, kind ir.ResourceKind, name string) error {
	return RemoveKindExcludeFromConfig(LocalConfigPath(repoRoot), kind, name)
}

// RemoveSkillFromLocalConfig removes skillName from
// <repoRoot>/.agents/config.local.yaml's skills list. Idempotent.
func RemoveSkillFromLocalConfig(repoRoot, skillName string) error {
	return RemoveSkillFromConfig(LocalConfigPath(repoRoot), skillName)
}

// AddToLocalExclude appends skillName to the exclude list in
// <repoRoot>/.agents/config.local.yaml. Idempotent.
func AddToLocalExclude(repoRoot, skillName string) error {
	return AddExcludeToConfig(LocalConfigPath(repoRoot), skillName)
}

// RemoveFromLocalExclude removes skillName from the exclude list in
// <repoRoot>/.agents/config.local.yaml. Idempotent.
func RemoveFromLocalExclude(repoRoot, skillName string) error {
	return RemoveKindExcludeFromConfig(LocalConfigPath(repoRoot), ir.KindSkill, skillName)
}

func RemoveResourceFromRegistryInConfig(configPath, registryURL, registryPath string, kind ir.ResourceKind, name string) error {
	key := skill.ConfigKeyForKind(kind)
	if key == "" {
		return fmt.Errorf("unsupported resource kind %q", skill.KindLabel(kind))
	}
	return removeValueFromRegistrySubSequence(configPath, registryURL, registryPath, key, "include", name)
}

// RemoveSkillFromRegistryInConfig removes a skill name from the skills
// list of a specific registry entry identified by URL. If the entry has no
// skills list, this is a no-op. If removal leaves the list empty, the
// skills key is removed from the entry. Idempotent.
func RemoveSkillFromRegistryInConfig(configPath, registryURL, registryPath, skillName string) error {
	return RemoveResourceFromRegistryInConfig(configPath, registryURL, registryPath, ir.KindSkill, skillName)
}

// RemoveCollectionFromConfig removes a collection name from the
// collections.include sequence. Idempotent; no-op if the file or entry
// is missing.
func RemoveCollectionFromConfig(configPath, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("collection name is required")
	}
	return RemoveResourceFromConfig(configPath, ir.KindCollection, name)
}

// removeValueFromTopLevelSubSequence removes a value from a sub-key
// (e.g. "exclude") under a top-level mapping key (e.g. "skills").
func removeValueFromTopLevelSubSequence(configPath, topKey, subKey, value string) error {
	return removeValuesFromTopLevelSubSequence(configPath, topKey, subKey, []string{value})
}

func removeValuesFromTopLevelSubSequence(configPath, topKey, subKey string, values []string) error {
	values = normalizedUniqueValues(values)
	if len(values) == 0 {
		return fmt.Errorf("%s.%s value is required", topKey, subKey)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read config %q: %w", configPath, err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		return fmt.Errorf("stat config %q: %w", configPath, err)
	}

	doc, err := parseConfigDocument(configPath, data)
	if err != nil {
		return err
	}

	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	// Find top-level key.
	_, topNode := findTopLevelSequence(root, topKey)
	if topNode == nil {
		return nil // nothing to remove
	}
	// If the value is a sequence (legacy list form), convert to mapping first.
	if topNode.Kind == yaml.SequenceNode {
		// Convert [a, b] → {include: [a, b]}
		newMapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		includeKey := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "include"}
		newMapping.Content = append(newMapping.Content, includeKey, topNode)
		// Replace the old sequence node with the new mapping.
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Kind == yaml.ScalarNode && root.Content[i].Value == topKey {
				root.Content[i+1] = newMapping
				topNode = newMapping
				break
			}
		}
	}
	if topNode.Kind != yaml.MappingNode {
		return nil // nothing to remove
	}

	subIdx, subNode := findEntrySequence(topNode, subKey)
	if subNode == nil {
		return nil
	}

	toRemove := make(map[string]struct{}, len(values))
	for _, value := range values {
		toRemove[value] = struct{}{}
	}

	filtered := subNode.Content[:0]
	for _, node := range subNode.Content {
		if node.Kind == yaml.ScalarNode {
			if _, ok := toRemove[strings.TrimSpace(node.Value)]; ok {
				continue
			}
		}
		filtered = append(filtered, node)
	}
	subNode.Content = filtered

	if len(subNode.Content) == 0 {
		removeMappingKey(topNode, subIdx)
	}
	// If top-level mapping is now empty, remove the top-level key.
	if len(topNode.Content) == 0 {
		topIdx, _ := findTopLevelSequence(root, topKey)
		removeTopLevelKey(root, topIdx)
	}

	return writeConfigDocument(configPath, info.Mode().Perm(), doc)
}

// removeValueFromRegistrySubSequence removes a value from a sub-key
// (e.g. "exclude") under a kind key (e.g. "skills") within a specific registry entry.
// When registryPath is non-empty, it matches the URL and normalized path
// exactly (composite key); a descendant path never targets its parent. When
// registryPath is empty, it matches on URL only. If multiple entries share the
// same URL and no path is specified, a disambiguation error lists the paths.
func removeValueFromRegistrySubSequence(configPath, registryURL, registryPath, topKey, subKey, value string) error {
	return removeValuesFromRegistrySubSequence(configPath, registryURL, registryPath, topKey, subKey, []string{value})
}

func removeValuesFromRegistrySubSequence(configPath, registryURL, registryPath, topKey, subKey string, values []string) error {
	registryURL = strings.TrimSpace(registryURL)
	if registryURL == "" {
		return fmt.Errorf("registry url is required")
	}
	registryPath = strings.TrimSpace(registryPath)
	values = normalizedUniqueValues(values)
	if len(values) == 0 {
		return fmt.Errorf("%s.%s value is required", topKey, subKey)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read config %q: %w", configPath, err)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		return fmt.Errorf("stat config %q: %w", configPath, err)
	}

	doc, err := parseConfigDocument(configPath, data)
	if err != nil {
		return err
	}
	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	_, registriesNode := findTopLevelSequence(root, "registries")
	if registriesNode == nil {
		return nil
	}

	var matchingEntries []*yaml.Node
	toRemove := make(map[string]struct{}, len(values))
	for _, value := range values {
		toRemove[value] = struct{}{}
	}

	for _, entryNode := range registriesNode.Content {
		if entryNode.Kind != yaml.MappingNode {
			continue
		}
		urlNode := findRegistryURLNode(entryNode)
		if urlNode == nil || NormalizeGitURL(strings.TrimSpace(urlNode.Value)) != NormalizeGitURL(registryURL) {
			continue
		}
		// When registryPath is specified, also match via URL-aware yamlEntryPath.
		if registryPath != "" {
			if !registryPathMatches(yamlEntryPath(entryNode), registryPath) {
				continue
			}
		}
		matchingEntries = append(matchingEntries, entryNode)
	}
	switch len(matchingEntries) {
	case 0:
		return nil
	case 1:
		// proceed
	default:
		// Multiple entries match the URL without a path disambiguator.
		var paths []string
		for _, entryNode := range matchingEntries {
			if ep := yamlEntryPath(entryNode); ep != "" {
				paths = append(paths, ep)
			} else {
				paths = append(paths, "(root)")
			}
		}
		return fmt.Errorf("update config %q: registry %q matches multiple entries; use --path to disambiguate (available: %s)", configPath, registryURL, strings.Join(paths, ", "))
	}

	entryNode := matchingEntries[0]

	// Find the kind mapping (e.g. "skills") within the entry.
	kindIdx, kindNode := findEntryMapping(entryNode, topKey)
	if kindNode == nil || kindNode.Kind != yaml.MappingNode {
		return nil
	}

	subIdx, subNode := findEntrySequence(kindNode, subKey)
	if subNode == nil {
		return nil
	}

	filtered := subNode.Content[:0]
	for _, node := range subNode.Content {
		if node.Kind == yaml.ScalarNode {
			if _, ok := toRemove[strings.TrimSpace(node.Value)]; ok {
				continue
			}
		}
		filtered = append(filtered, node)
	}
	subNode.Content = filtered

	if len(subNode.Content) == 0 {
		removeMappingKey(kindNode, subIdx)
	}
	if len(kindNode.Content) == 0 {
		removeMappingKey(entryNode, kindIdx)
	}

	return writeConfigDocument(configPath, info.Mode().Perm(), doc)
}

func UpdateRegistryRefInConfig(configPath, url, oldRef, newRef string) (int, error) {
	url = strings.TrimSpace(url)
	oldRef = strings.TrimSpace(oldRef)
	newRef = strings.TrimSpace(newRef)
	switch {
	case url == "":
		return 0, fmt.Errorf("registry url is required")
	case oldRef == "":
		return 0, fmt.Errorf("old ref is required")
	case newRef == "":
		return 0, fmt.Errorf("new ref is required")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return 0, fmt.Errorf("read config %q: %w", configPath, err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		return 0, fmt.Errorf("stat config %q: %w", configPath, err)
	}

	doc, err := parseConfigDocument(configPath, data)
	if err != nil {
		return 0, err
	}

	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return 0, fmt.Errorf("update config %q: %w", configPath, err)
	}

	_, registriesNode := findTopLevelSequence(root, "registries")
	if registriesNode == nil {
		return 0, nil
	}
	if registriesNode.Kind != yaml.SequenceNode {
		return 0, fmt.Errorf("update config %q: expected %q to be a sequence", configPath, "registries")
	}

	updated := 0
	for _, entryNode := range registriesNode.Content {
		if entryNode.Kind != yaml.MappingNode {
			continue
		}

		urlNode := findRegistryURLNode(entryNode)
		refNode := findMappingValue(entryNode, "ref")
		if urlNode == nil || refNode == nil || refNode.Kind != yaml.ScalarNode {
			continue
		}

		if NormalizeGitURL(strings.TrimSpace(urlNode.Value)) != NormalizeGitURL(url) || strings.TrimSpace(refNode.Value) != oldRef {
			continue
		}

		refNode.Value = newRef
		updated++
	}

	if updated == 0 {
		return 0, nil
	}

	if err := writeConfigDocument(configPath, info.Mode().Perm(), doc); err != nil {
		return 0, err
	}

	return updated, nil
}

// ValidateScopeBoundary returns an error if targetScope is repo-local
// (RepoCommitted or RepoPersonal) and skillSource is machine-scope
// (Organization or UserPersonal). Returns nil if the operation is allowed.
//
// This ensures repo/local layers cannot exclude org/user skills — those
// skills are installed at the user-wide scope and the exclude would be
// silently ineffective.
func ValidateScopeBoundary(skillSource Source, targetScope Source) error {
	if !targetScope.IsMachineScope() && skillSource.IsMachineScope() {
		return &ScopeBoundaryError{
			Source:     skillSource,
			Target:     targetScope,
			Suggestion: "Use --scope user to exclude at the user level.",
		}
	}
	return nil
}

// RemoveRegistryEntryFromConfig removes an entire registry entry identified
// by exact URL and path from the registries: sequence. When path is empty,
// only entries with no path (root entries) are removed. Idempotent; no-op if
// the entry is not found. If removal leaves the registries: sequence empty,
// the key is removed from the document.
func RemoveRegistryEntryFromConfig(configPath, registryURL, registryPath string) error {
	return removeRegistryEntryFromConfig(configPath, registryURL, registryPath, "", false)
}

// RemoveRegistryEntryFromConfigAtRef removes exactly one URL/path/ref entry,
// preserving sibling refs and their comments.
func RemoveRegistryEntryFromConfigAtRef(configPath, registryURL, registryPath, registryRef string) error {
	registryRef = strings.TrimSpace(registryRef)
	if registryRef == "" {
		return fmt.Errorf("registry ref is required")
	}
	return removeRegistryEntryFromConfig(configPath, registryURL, registryPath, registryRef, true)
}

func removeRegistryEntryFromConfig(configPath, registryURL, registryPath, registryRef string, requireRef bool) error {
	registryURL = strings.TrimSpace(registryURL)
	registryPath = strings.TrimSpace(registryPath)
	registryRef = strings.TrimSpace(registryRef)
	if registryURL == "" {
		return fmt.Errorf("registry url is required")
	}

	doc, mode, err := readOrInitConfig(configPath)
	if err != nil {
		return err
	}

	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	_, registriesNode := findTopLevelSequence(root, "registries")
	if registriesNode == nil || registriesNode.Kind != yaml.SequenceNode {
		return nil
	}

	kept := make([]*yaml.Node, 0, len(registriesNode.Content))
	removed := false
	for _, entryNode := range registriesNode.Content {
		if entryNode.Kind != yaml.MappingNode {
			kept = append(kept, entryNode)
			continue
		}
		entryURL, entryPath := entryURLAndPath(entryNode)
		matches := NormalizeGitURL(strings.TrimSpace(entryURL)) == NormalizeGitURL(registryURL) && NormalizeRegistryPath(entryPath) == NormalizeRegistryPath(registryPath)
		if matches && requireRef {
			matches = registryEntryMatchesExactURLPathRef(entryNode, registryURL, registryPath, registryRef)
		}
		if matches {
			removed = true
			continue
		}
		kept = append(kept, entryNode)
	}

	if !removed {
		return nil
	}

	registriesNode.Content = kept
	if len(kept) == 0 {
		// Remove the registries key from the mapping.
		keyIdx := -1
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value == "registries" {
				keyIdx = i
				break
			}
		}
		if keyIdx >= 0 {
			removeTopLevelKey(root, keyIdx)
		}
	}

	return writeConfigDocument(configPath, mode, doc)
}

// entryURLAndPath extracts the url and path fields from a registry entry
// YAML mapping node. When "url" is present, the subdirectory is taken from
// "path". When "url" is absent, "path" is a URL alias (not a subdir), so
// the returned path is empty. It delegates the path rule to yamlEntryPath so
// add and remove operations stay consistent.
func entryURLAndPath(entryNode *yaml.Node) (string, string) {
	urlNode := findMappingValue(entryNode, "url")
	url := ""
	if urlNode != nil && strings.TrimSpace(urlNode.Value) != "" {
		url = strings.TrimSpace(urlNode.Value)
	} else if pathNode := findMappingValue(entryNode, "path"); pathNode != nil {
		url = strings.TrimSpace(pathNode.Value)
	}
	return url, yamlEntryPath(entryNode)
}
