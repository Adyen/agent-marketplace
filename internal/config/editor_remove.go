package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

// DisableResourceWithShadowFallback disables a resource in every matching
// URL+path+ref entry, creating a shadow entry when the identity is inherited.
func DisableResourceWithShadowFallback(configPath, registryURL, registryPath, ref string, kind ir.ResourceKind, name string) error {
	registryURL = strings.TrimSpace(registryURL)
	registryPath = strings.TrimSpace(registryPath)
	if err := ensureRegistryIdentityInConfig(configPath, registryURL, ref, registryPath); err != nil {
		return fmt.Errorf("ensure shadow registry entry: %w", err)
	}
	return disableExactRegistryEntries(configPath, registryURL, registryPath, ref, kind, name)
}

func disableExactRegistryEntries(configPath, registryURL, registryPath, ref string, kind ir.ResourceKind, name string) error {
	key := skill.ConfigKeyForKind(kind)
	if key == "" {
		return fmt.Errorf("unsupported resource kind %q", skill.KindLabel(kind))
	}
	return editExactRegistryEntries(configPath, registryURL, registryPath, ref, func(matches []*yaml.Node) error {
		selectingMatches := slices.DeleteFunc(slices.Clone(matches), func(entryNode *yaml.Node) bool {
			return !yamlEntryHasExplicitResource(entryNode, key, name) && !yamlEntryAutoSelectsResource(entryNode)
		})
		if len(selectingMatches) > 0 {
			matches = selectingMatches
		} else {
			matches = matches[:1]
		}
		for _, entryNode := range matches {
			needsExclude := yamlEntryAutoSelectsResource(entryNode) || !yamlEntryHasExplicitResource(entryNode, key, name)
			kindIdx, kindNode := findEntryMapping(entryNode, key)
			if kindIdx >= 0 && kindNode == nil {
				return fmt.Errorf("registry %q has invalid %q selector type", registryURL, key)
			}
			if kindNode != nil {
				if subIdx, includeNode := findEntrySequence(kindNode, "include"); includeNode != nil {
					includeNode.Content = slices.DeleteFunc(includeNode.Content, func(node *yaml.Node) bool {
						return node.Kind == yaml.ScalarNode && strings.TrimSpace(node.Value) == strings.TrimSpace(name)
					})
					if len(includeNode.Content) == 0 {
						removeMappingKey(kindNode, subIdx)
					}
				}
			}
			if !needsExclude {
				if kindNode != nil && len(kindNode.Content) == 0 {
					removeMappingKey(entryNode, kindIdx)
				}
				continue
			}
			if err := addKindExcludeToEntry(entryNode, key, name); err != nil {
				return fmt.Errorf("registry %q: %w", registryURL, err)
			}
		}
		return nil
	})
}

// AddKindExcludeToRegistryIdentityInConfig adds an exclude to every registry
// entry with the exact normalized URL+path+ref identity.
func AddKindExcludeToRegistryIdentityInConfig(configPath, registryURL, registryPath, ref string, kind ir.ResourceKind, name string) error {
	key := skill.ConfigKeyForKind(kind)
	if key == "" {
		return fmt.Errorf("unsupported resource kind %q", skill.KindLabel(kind))
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%s name is required", skill.KindLabel(kind))
	}
	return editExactRegistryEntries(configPath, registryURL, registryPath, ref, func(matches []*yaml.Node) error {
		for _, entryNode := range matches {
			if err := addKindExcludeToEntry(entryNode, key, name); err != nil {
				return fmt.Errorf("registry %q: %w", registryURL, err)
			}
		}
		return nil
	})
}

func editExactRegistryEntries(configPath, registryURL, registryPath, ref string, editEntries func([]*yaml.Node) error) error {
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
		return fmt.Errorf("update config %q: registry %q not found", configPath, registryURL)
	}

	registryURL = strings.TrimSpace(registryURL)
	registryPath = strings.TrimSpace(registryPath)
	ref = strings.TrimSpace(ref)
	var matches []*yaml.Node
	for _, entryNode := range registriesNode.Content {
		if entryNode.Kind != yaml.MappingNode {
			continue
		}
		urlNode := findRegistryURLNode(entryNode)
		if urlNode != nil && NormalizeGitURL(strings.TrimSpace(urlNode.Value)) == NormalizeGitURL(registryURL) &&
			yamlEntryPath(entryNode) == registryPath && yamlEntryRef(entryNode) == ref {
			matches = append(matches, entryNode)
		}
	}
	if len(matches) == 0 {
		return fmt.Errorf("update config %q: registry %q with path %q and ref %q not found", configPath, registryURL, pathLabel(registryPath), ref)
	}
	if err := editEntries(matches); err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	return writeConfigDocument(configPath, mode, doc)
}

func addKindExcludeToEntry(entryNode *yaml.Node, key, name string) error {
	kindNode := ensureEntryMapping(entryNode, key)
	if kindNode == nil {
		return fmt.Errorf("invalid %q selector type", key)
	}
	excludeNode, err := ensureMappingSequence(kindNode, "exclude")
	if err != nil {
		return err
	}
	setSequenceValues(excludeNode, appendUniqueValues(sequenceValues(excludeNode), []string{name}))
	return nil
}

func yamlEntryHasExplicitResource(entryNode *yaml.Node, key, name string) bool {
	kindNode := findMappingValue(entryNode, key)
	if kindNode == nil {
		return false
	}
	includeNode := kindNode
	if kindNode.Kind == yaml.MappingNode {
		includeNode = findMappingValue(kindNode, "include")
	}
	if includeNode == nil || includeNode.Kind != yaml.SequenceNode {
		return false
	}
	for _, valueNode := range includeNode.Content {
		if valueNode.Kind == yaml.ScalarNode && strings.TrimSpace(valueNode.Value) == strings.TrimSpace(name) {
			return true
		}
	}
	return false
}

func yamlEntryAutoSelectsResource(entryNode *yaml.Node) bool {
	for _, field := range []string{"default", "required"} {
		if boolMappingValue(entryNode, field) {
			return true
		}
	}
	tagsNode := findMappingValue(entryNode, "tags")
	return tagsNode != nil && len(tagsNode.Content) > 0
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

	cfg, err := LoadConfigFromPath(configPath)
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
		if NormalizeGitURL(strings.TrimSpace(entry.URL)) != NormalizeGitURL(registryURL) {
			continue
		}
		// When registryPath is specified, also match on entry.Path.
		if registryPath != "" && strings.TrimSpace(entry.Path) != registryPath {
			continue
		}
		entryResources := entryResourcesForKind(entry, kind)
		if entryResources != nil && stringSliceContains(entryResources, name) {
			if err := RemoveResourceFromRegistryInConfig(configPath, registryURL, entry.Path, kind, name); err != nil {
				return err
			}
			// If the entry has default:true, required:true, or tags that would
			// re-select the resource after include removal, also add to exclude
			// so the disable takes effect in a single press.
			if entry.Default || entry.Required || len(entry.Tags) > 0 {
				return AddKindExcludeToRegistryInConfig(configPath, registryURL, entry.Path, kind, name)
			}
			return nil
		}
		return AddKindExcludeToRegistryInConfig(configPath, registryURL, entry.Path, kind, name)
	}
	return fmt.Errorf("registry %q not found in config", registryURL)
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

	cfg, err := LoadConfigFromPath(configPath)
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
// When registryPath is non-empty, it matches on both URL and path
// (composite key). When registryPath is empty, it matches on URL only.
// If multiple entries share the same URL and no path is specified, a
// disambiguation error lists the available paths.
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
	// Prefer exact-path entry when multiple entries match via prefix.
	matchingEntries = preferExactPathMatch(matchingEntries, registryPath)

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

// RemoveRegistryEntryByIdentityFromConfig removes entries matching the full
// URL+path+ref identity, including indistinguishable exact duplicates.
func RemoveRegistryEntryByIdentityFromConfig(configPath, registryURL, registryPath, registryRef string) error {
	return removeRegistryEntryFromConfig(configPath, registryURL, registryPath, registryRef, true)
}

func removeRegistryEntryFromConfig(configPath, registryURL, registryPath, registryRef string, matchRef bool) error {
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
		if NormalizeGitURL(strings.TrimSpace(entryURL)) == NormalizeGitURL(registryURL) && strings.TrimSpace(entryPath) == registryPath &&
			(!matchRef || yamlEntryRef(entryNode) == registryRef) {
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
	if urlNode != nil {
		url = strings.TrimSpace(urlNode.Value)
	}
	return url, yamlEntryPath(entryNode)
}
