package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

func AddSkillToConfig(configPath, skillName string) error {
	return AddSkillsToConfig(configPath, []string{skillName})
}

func AddSkillsToConfig(configPath string, skillNames []string) error {
	return AddResourcesToConfig(configPath, ir.KindSkill, skillNames)
}

func AddSkillsToRegistryInConfig(configPath, registryURL, registryPath string, skillNames []string) error {
	return AddResourcesToRegistryInConfig(configPath, registryURL, registryPath, ir.KindSkill, skillNames)
}

func AddResourceToConfig(configPath string, kind ir.ResourceKind, name string) error {
	return AddResourcesToConfig(configPath, kind, []string{name})
}

// AddResourcesToConfig writes per-kind includes to a config file.
// For LocalConfig files (config.local.yaml), this writes to the top-level
// skills/commands/etc selector. For Config files (config.yaml), this is
// a legacy path that writes to the first matching registry entry.
func AddResourcesToConfig(configPath string, kind ir.ResourceKind, names []string) error {
	key := skill.ConfigKeyForKind(kind)
	if key == "" {
		return fmt.Errorf("unsupported resource kind %q", skill.KindLabel(kind))
	}
	if err := addValuesToTopLevelSubSequence(configPath, key, "include", names); err != nil {
		return err
	}
	// Clean up stale excludes for the same names so the resolver does not
	// suppress a resource that was just re-enabled.
	if err := removeValuesFromTopLevelSubSequence(configPath, key, "exclude", names); err != nil {
		return fmt.Errorf("update config %q: include written but exclude cleanup failed: %w", configPath, err)
	}
	return nil
}

func AddResourceToRegistryInConfig(configPath, registryURL, registryPath string, kind ir.ResourceKind, name string) error {
	return AddResourcesToRegistryInConfig(configPath, registryURL, registryPath, kind, []string{name})
}

func AddResourcesToRegistryInConfig(configPath, registryURL, registryPath string, kind ir.ResourceKind, names []string) error {
	key := skill.ConfigKeyForKind(kind)
	if key == "" {
		return fmt.Errorf("unsupported resource kind %q", skill.KindLabel(kind))
	}
	if err := addValuesToRegistrySubSequence(configPath, registryURL, registryPath, key, "include", names); err != nil {
		return err
	}
	// Clean up stale excludes for the same names so the resolver does not
	// suppress a resource that was just re-enabled.
	if err := removeValuesFromRegistrySubSequence(configPath, registryURL, registryPath, key, "exclude", names); err != nil {
		return fmt.Errorf("update config %q: include written but registry exclude cleanup failed: %w", configPath, err)
	}
	return nil
}

func AddTagToConfig(configPath, tag string) error {
	return addValuesToTopLevelSequence(configPath, "tags", []string{tag})
}

// AddSkillToGlobalConfig appends skillName to the top-level skills list in
// ~/.agents/config.yaml. Idempotent; creates the file and parent directory
// if they do not exist.
func AddSkillToGlobalConfig(skillName string) error {
	path, err := GlobalConfigPath()
	if err != nil {
		return err
	}
	return AddSkillToConfig(path, skillName)
}

// AddSkillToLocalConfig appends skillName to the skills list in
// <repoRoot>/.agents/config.local.yaml. Idempotent.
func AddSkillToLocalConfig(repoRoot, skillName string) error {
	return AddSkillToConfig(LocalConfigPath(repoRoot), skillName)
}

// AddSkillToRegistryInConfig adds a skill name to the skills list of a
// specific registry entry identified by URL in the config file at configPath.
// The skill is appended to the entry's existing skills list (or a new list
// is created if the entry has none). Idempotent.
func AddSkillToRegistryInConfig(configPath, registryURL, registryPath, skillName string) error {
	return AddSkillsToRegistryInConfig(configPath, registryURL, registryPath, []string{skillName})
}

// EnsureRegistryInConfig adds a registry entry for the given URL (and
// optional ref) to the registries: list in configPath. If an entry with the
// same URL already exists, the file is left unchanged (idempotent). Creates
// the file and parent directories if they do not exist.
func EnsureRegistryInConfig(configPath, url, ref string) error {
	return EnsureRegistryWithPathsInConfig(configPath, url, ref, "")
}

// EnsureRegistryWithPathsInConfig is like EnsureRegistryInConfig but also
// sets the path field on the newly created entry. Idempotent: when path is
// empty, deduplication is URL-only (same as EnsureRegistryInConfig); when
// path is non-empty, deduplication uses the composite (URL, path) key so
// that the same repo URL can have multiple sub-path entries coexisting.
// Creates the file and parent directories if they do not exist.
func EnsureRegistryWithPathsInConfig(configPath, url, ref, path string) error {
	return ensureRegistryWithPathsInConfig(configPath, url, ref, path, false)
}

func ensureRegistryIdentityInConfig(configPath, url, ref, path string) error {
	return ensureRegistryWithPathsInConfig(configPath, url, ref, path, true)
}

func ensureRegistryWithPathsInConfig(configPath, url, ref, path string, matchRef bool) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return fmt.Errorf("registry url is required")
	}
	if err := validateRegistryWrite(configPath, RegistryEntry{URL: url}); err != nil {
		return err
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
	if registriesNode != nil && registriesNode.Kind == yaml.SequenceNode {
		for _, entryNode := range registriesNode.Content {
			if entryNode.Kind != yaml.MappingNode {
				continue
			}
			urlNode := findRegistryURLNode(entryNode)
			if urlNode == nil || NormalizeGitURL(strings.TrimSpace(urlNode.Value)) != NormalizeGitURL(url) {
				continue
			}
			existingPath := yamlEntryPath(entryNode)
			if strings.TrimSpace(path) != existingPath || (matchRef && strings.TrimSpace(ref) != yamlEntryRef(entryNode)) {
				continue
			}
			return nil // already present
		}
	}

	// Ensure the registries sequence exists.
	regsNode, err := ensureTopLevelSequence(root, "registries")
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	// Build the new registry entry mapping node.
	entryNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	entryNode.Content = append(entryNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "url"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: url},
	)
	if r := strings.TrimSpace(ref); r != "" {
		entryNode.Content = append(entryNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "ref"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: r},
		)
	}
	if p := strings.TrimSpace(path); p != "" {
		entryNode.Content = append(entryNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "path"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p},
		)
	}
	regsNode.Content = append(regsNode.Content, entryNode)

	return writeConfigDocument(configPath, mode, doc)
}

// EnsureRegistryWithDefaultInConfig is like EnsureRegistryWithPathsInConfig but
// also sets default:true on the newly created entry. Idempotent: if the entry
// already exists, the file is left unchanged (even if the existing entry does
// not have default:true — the caller should use a dedicated editor if that
// matters). Creates the file and parent directories if they do not exist.
func EnsureRegistryWithDefaultInConfig(configPath, url, ref, path string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return fmt.Errorf("registry url is required")
	}
	if err := validateRegistryWrite(configPath, RegistryEntry{URL: url}); err != nil {
		return err
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
	if registriesNode != nil && registriesNode.Kind == yaml.SequenceNode {
		for _, entryNode := range registriesNode.Content {
			if entryNode.Kind != yaml.MappingNode {
				continue
			}
			urlNode := findRegistryURLNode(entryNode)
			if urlNode == nil || NormalizeGitURL(strings.TrimSpace(urlNode.Value)) != NormalizeGitURL(url) {
				continue
			}
			existingPath := yamlEntryPath(entryNode)
			if strings.TrimSpace(path) != existingPath {
				continue // same URL, different path — not a duplicate
			}
			// Entry exists. Ensure it has default:true and no contradictory
			// exclude_all (default and exclude_all are mutually exclusive).
			changed := false
			defaultNode := findMappingValue(entryNode, "default")
			if defaultNode == nil || strings.TrimSpace(defaultNode.Value) != "true" {
				setMappingValue(entryNode, "default", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
				changed = true
			}
			if removeMappingKeyByName(entryNode, "exclude_all") {
				changed = true
			}
			if changed {
				return writeConfigDocument(configPath, mode, doc)
			}
			return nil // already default:true with no exclude_all
		}
	}

	regsNode, err := ensureTopLevelSequence(root, "registries")
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	entryNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	entryNode.Content = append(entryNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "url"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: url},
	)
	if r := strings.TrimSpace(ref); r != "" {
		entryNode.Content = append(entryNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "ref"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: r},
		)
	}
	if p := strings.TrimSpace(path); p != "" {
		entryNode.Content = append(entryNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "path"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p},
		)
	}
	entryNode.Content = append(entryNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "default"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"},
	)
	regsNode.Content = append(regsNode.Content, entryNode)

	return writeConfigDocument(configPath, mode, doc)
}

// SetRegistryExcludeAllInConfig ensures a registry entry for url (with the
// given ref/path) exists in configPath and sets exclude_all: true on it. This
// is used to suppress, at a higher-precedence scope (e.g. config.local.yaml),
// every non-required resource from a registry that is enabled at a lower layer
// (e.g. the committed project config). The entry-level exclude_all flag is
// sugar that the resolver propagates to all per-kind selectors and applies
// across layers, scoped by registry URL. Idempotent: if a matching entry
// already has exclude_all: true the file is left unchanged. Creates the file
// and parent directories if they do not exist.
func SetRegistryExcludeAllInConfig(configPath, url, ref, path string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return fmt.Errorf("registry url is required")
	}
	if err := validateRegistryWrite(configPath, RegistryEntry{URL: url}); err != nil {
		return err
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
	if registriesNode != nil && registriesNode.Kind == yaml.SequenceNode {
		for _, entryNode := range registriesNode.Content {
			if entryNode.Kind != yaml.MappingNode {
				continue
			}
			urlNode := findRegistryURLNode(entryNode)
			if urlNode == nil || NormalizeGitURL(strings.TrimSpace(urlNode.Value)) != NormalizeGitURL(url) {
				continue
			}
			existingPath := yamlEntryPath(entryNode)
			if strings.TrimSpace(path) != existingPath {
				continue // same URL, different path — not a duplicate
			}
			// Entry exists. Ensure exclude_all:true and remove contradictory
			// selection fields (default + per-kind include lists), since
			// exclude_all always wins and they would be dead config.
			changed := clearEntrySelectionsForExcludeAll(entryNode)
			excludeAllNode := findMappingValue(entryNode, "exclude_all")
			if excludeAllNode == nil || strings.TrimSpace(excludeAllNode.Value) != "true" {
				setMappingValue(entryNode, "exclude_all", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
				changed = true
			}
			if changed {
				return writeConfigDocument(configPath, mode, doc)
			}
			return nil // already exclude_all:true with no contradictory fields
		}
	}

	regsNode, err := ensureTopLevelSequence(root, "registries")
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	entryNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	entryNode.Content = append(entryNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "url"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: url},
	)
	if r := strings.TrimSpace(ref); r != "" {
		entryNode.Content = append(entryNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "ref"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: r},
		)
	}
	if p := strings.TrimSpace(path); p != "" {
		entryNode.Content = append(entryNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "path"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p},
		)
	}
	entryNode.Content = append(entryNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "exclude_all"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"},
	)
	regsNode.Content = append(regsNode.Content, entryNode)

	return writeConfigDocument(configPath, mode, doc)
}

// clearEntrySelectionsForExcludeAll removes selection fields that contradict
// exclude_all on a registry entry node: the default flag and every per-kind
// include list. Because exclude_all always wins in the resolver, these would
// be dead config that confuses readers. Returns true when anything changed.
func clearEntrySelectionsForExcludeAll(entryNode *yaml.Node) bool {
	if entryNode == nil || entryNode.Kind != yaml.MappingNode {
		return false
	}
	changed := removeMappingKeyByName(entryNode, "default")
	for _, kind := range AllKinds {
		kindKey := skill.ConfigKeyForKind(kind)
		if kindKey == "" {
			continue
		}
		kindMapping := findMappingValue(entryNode, kindKey)
		if kindMapping == nil {
			continue
		}
		if removeMappingKeyByName(kindMapping, "include") {
			changed = true
		}
		// Drop the now-empty kind mapping so the entry stays tidy.
		if kindMapping.Kind == yaml.MappingNode && len(kindMapping.Content) == 0 {
			removeMappingKeyByName(entryNode, kindKey)
		}
	}
	return changed
}

// AddCollectionToConfig adds a collection name to the collections.include
// sequence in the config file at configPath. Idempotent: if the name already
// exists the file is left unchanged. Creates the file and parent directories
// if they do not exist.
func AddCollectionToConfig(configPath, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("collection name is required")
	}
	return AddResourcesToConfig(configPath, ir.KindCollection, []string{name})
}

func addValuesToTopLevelSequence(configPath, key string, values []string) error {
	values = normalizedUniqueValues(values)
	if len(values) == 0 {
		return fmt.Errorf("%s value is required", key)
	}

	doc, mode, err := readOrInitConfig(configPath)
	if err != nil {
		return err
	}

	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	sequence, err := ensureTopLevelSequence(root, key)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	setSequenceValues(sequence, appendUniqueValues(sequenceValues(sequence), values))
	return writeConfigDocument(configPath, mode, doc)
}

// addValuesToTopLevelSubSequence adds values to a sub-key (e.g. "exclude")
// under a top-level mapping key (e.g. "skills"). This writes the nested form
// like `skills: {include: [...], exclude: [...]}`.
func addValuesToTopLevelSubSequence(configPath, topKey, subKey string, values []string) error {
	values = normalizedUniqueValues(values)
	if len(values) == 0 {
		return fmt.Errorf("%s.%s value is required", topKey, subKey)
	}

	doc, mode, err := readOrInitConfig(configPath)
	if err != nil {
		return err
	}

	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	// Ensure the top-level key exists as a mapping node.
	topNode := ensureTopLevelMapping(root, topKey)
	if topNode == nil {
		return fmt.Errorf("update config %q: failed to ensure mapping for %q", configPath, topKey)
	}

	// Ensure the sub-key sequence exists within the top-level mapping.
	sequence, err := ensureMappingSequence(topNode, subKey)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	setSequenceValues(sequence, appendUniqueValues(sequenceValues(sequence), values))
	return writeConfigDocument(configPath, mode, doc)
}

// addValuesToRegistrySubSequence adds values to a sub-key (e.g. "exclude")
// under a kind key (e.g. "skills") within a specific registry entry.
// When registryPath is non-empty, it matches on both URL and path
// (composite key). When registryPath is empty, it matches on URL only.
// If multiple entries share the same URL and no path is specified, a
// disambiguation error lists the available paths.
func addValuesToRegistrySubSequence(configPath, registryURL, registryPath, topKey, subKey string, values []string) error {
	registryURL = strings.TrimSpace(registryURL)
	if registryURL == "" {
		return fmt.Errorf("registry url is required")
	}
	registryPath = strings.TrimSpace(registryPath)
	values = normalizedUniqueValues(values)
	if len(values) == 0 {
		return fmt.Errorf("registries[].%s.%s value is required", topKey, subKey)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
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
		return fmt.Errorf("update config %q: registry %q not found", configPath, registryURL)
	}

	var matchingEntries []*yaml.Node
	for _, entryNode := range registriesNode.Content {
		if entryNode.Kind != yaml.MappingNode {
			continue
		}
		urlNode := findRegistryURLNode(entryNode)
		if urlNode == nil || NormalizeGitURL(strings.TrimSpace(urlNode.Value)) != NormalizeGitURL(registryURL) {
			continue
		}
		// When registryPath is specified, also match on path.
		if registryPath != "" {
			if !registryPathMatches(yamlEntryPath(entryNode), registryPath) {
				continue
			}
		}
		matchingEntries = append(matchingEntries, entryNode)
	}
	// When multiple entries match via prefix, prefer the exact-path entry so
	// that e.g. registryPath="plugins/personal" picks the "plugins/personal"
	// entry rather than the parent "plugins" entry.
	matchingEntries = preferExactPathMatch(matchingEntries, registryPath)
	switch len(matchingEntries) {
	case 0:
		if registryPath != "" {
			return fmt.Errorf("update config %q: registry %q with path %q not found", configPath, registryURL, registryPath)
		}
		return fmt.Errorf("update config %q: registry %q not found", configPath, registryURL)
	case 1:
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

	// See addValuesToExactRegistryEntry: never add an explicit include to an
	// entry that already auto-selects this kind, or its default:true /
	// required:true selection would be silently disabled. The caller drops any
	// stale exclude separately.
	if kind, ok := skill.ParseKind(topKey); ok && subKey == "include" && entryNodeAutoSelectsKind(entryNode, kind) {
		return nil
	}

	// Ensure the topKey (e.g. "skills") exists as a mapping within the entry.
	kindMapping := ensureEntryMapping(entryNode, topKey)

	// Ensure the subKey sequence exists within the kind mapping.
	sequence, err := ensureMappingSequence(kindMapping, subKey)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	existingValues := sequenceValues(sequence)

	setSequenceValues(sequence, appendUniqueValues(existingValues, values))

	// Adding an explicit include opts the resource back in; exclude_all on the
	// same entry would silently nullify it (exclude_all always wins), so clear
	// the contradictory flag.
	if subKey == "include" {
		clearEntryExcludeAll(entryNode, topKey)
	}

	return writeConfigDocument(configPath, info.Mode().Perm(), doc)
}

func SetHarnessesInConfig(configPath string, harnesses []string) error {
	if isLocalConfigPath(configPath) {
		return fmt.Errorf("harnesses cannot be set in repository personal config %q", configPath)
	}

	// Normalize, dedupe, and reject unknown or non-compiler targets before any
	// file is written, so a typo cannot land in the target harness set.
	harnesses, err := canonicalCompilerHarnesses("harnesses", harnesses)
	if err != nil {
		return err
	}

	doc, mode, err := readOrInitConfig(configPath)
	if err != nil {
		return err
	}

	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	sequence, err := ensureTopLevelSequence(root, "harnesses")
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	setSequenceValues(sequence, harnesses)
	return writeConfigDocument(configPath, mode, doc)
}

// AddResourceToExactRegistryEntry adds a per-kind include to a specific
// registry entry identified by exact URL and path. Unlike
// AddResourceToRegistryInConfig, when path is empty this matches only
// entries with no path (root entries), avoiding "matches multiple entries"
// errors when the same URL has entries at different paths.
func AddResourceToExactRegistryEntry(configPath, registryURL, registryPath string, kind ir.ResourceKind, name string) error {
	if err := addValuesToExactRegistryEntry(configPath, registryURL, registryPath, kind, "include", []string{name}); err != nil {
		return err
	}
	// Clean up stale exclude for the same name so the resolver does not
	// suppress a resource that was just enabled.
	return removeValuesFromExactRegistryEntry(configPath, registryURL, registryPath, kind, "exclude", []string{name})
}

// addValuesToExactRegistryEntry adds values to a sub-key (e.g. "include" or
// "exclude") under a kind key within a registry entry matched by exact URL
// and path. When path is empty, only entries with no path (root entries)
// match — unlike addValuesToRegistrySubSequence where empty path means
// URL-only matching.
func addValuesToExactRegistryEntry(configPath, registryURL, registryPath string, kind ir.ResourceKind, subKey string, values []string) error {
	key := skill.ConfigKeyForKind(kind)
	if key == "" {
		return fmt.Errorf("unsupported resource kind %q", skill.KindLabel(kind))
	}
	registryURL = strings.TrimSpace(registryURL)
	registryPath = strings.TrimSpace(registryPath)
	values = normalizedUniqueValues(values)
	if len(values) == 0 {
		return fmt.Errorf("registries[].%s.%s value is required", key, subKey)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
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
		return fmt.Errorf("update config %q: registry %q not found", configPath, registryURL)
	}

	var matchingEntries []*yaml.Node
	for _, entryNode := range registriesNode.Content {
		if entryNode.Kind != yaml.MappingNode {
			continue
		}
		urlNode := findRegistryURLNode(entryNode)
		if urlNode == nil || NormalizeGitURL(strings.TrimSpace(urlNode.Value)) != NormalizeGitURL(registryURL) {
			continue
		}
		if registryPathMatches(yamlEntryPath(entryNode), registryPath) {
			matchingEntries = append(matchingEntries, entryNode)
		}
	}
	// When the same URL has both a parent (e.g. "plugins") and a child
	// ("plugins/personal") entry, a prefix match would pick whichever comes
	// first. Prefer the entry whose path exactly equals registryPath so writes
	// land on the intended entry (the one EnsureRegistryWithPathsInConfig just
	// created), falling back to the prefix match only when no exact entry exists.
	matchingEntries = preferExactPathMatch(matchingEntries, registryPath)
	if len(matchingEntries) == 0 {
		return fmt.Errorf("update config %q: registry %q with path %q not found", configPath, registryURL, pathLabel(registryPath))
	}
	matchingEntry := matchingEntries[0]

	// When re-enabling a resource on an entry that already auto-selects this
	// kind (default:true / required:true, no tags, no explicit includes, no
	// exclude_all dropping the kind), the resource is already selected
	// implicitly. Writing an explicit include would flip the entry into
	// explicit-selection mode and silently deselect everything else, so skip
	// the include write — the caller drops any stale exclude separately.
	// (Guarded to include-writes; exclude writes proceed.)
	if subKey == "include" && entryNodeAutoSelectsKind(matchingEntry, kind) {
		return nil
	}

	kindMapping := ensureEntryMapping(matchingEntry, key)
	if kindMapping == nil {
		return fmt.Errorf(
			"update config %q: registry %q with path %q has invalid %q selector type",
			configPath, registryURL, pathLabel(registryPath), key,
		)
	}
	sequence, err := ensureMappingSequence(kindMapping, subKey)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	existingValues := sequenceValues(sequence)

	setSequenceValues(sequence, appendUniqueValues(existingValues, values))

	// Adding an explicit include opts the resource back in; exclude_all on the
	// same entry would silently nullify it (exclude_all always wins), so clear
	// the contradictory flag.
	if subKey == "include" {
		clearEntryExcludeAll(matchingEntry, key)
	}

	return writeConfigDocument(configPath, info.Mode().Perm(), doc)
}

// removeValuesFromExactRegistryEntry removes values from a sub-key within a
// registry entry matched by exact URL and path. When path is empty, only
// entries with no path (root entries) match.
func removeValuesFromExactRegistryEntry(configPath, registryURL, registryPath string, kind ir.ResourceKind, subKey string, values []string) error {
	key := skill.ConfigKeyForKind(kind)
	if key == "" {
		return fmt.Errorf("unsupported resource kind %q", skill.KindLabel(kind))
	}
	registryURL = strings.TrimSpace(registryURL)
	registryPath = strings.TrimSpace(registryPath)
	values = normalizedUniqueValues(values)
	if len(values) == 0 {
		return nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
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
	for _, entryNode := range registriesNode.Content {
		if entryNode.Kind != yaml.MappingNode {
			continue
		}
		urlNode := findRegistryURLNode(entryNode)
		if urlNode == nil || NormalizeGitURL(strings.TrimSpace(urlNode.Value)) != NormalizeGitURL(registryURL) {
			continue
		}
		if registryPathMatches(yamlEntryPath(entryNode), registryPath) {
			matchingEntries = append(matchingEntries, entryNode)
		}
	}
	// Prefer the exact-path entry over a parent prefix match so removal targets
	// the intended entry when the same URL has both parent and child paths.
	matchingEntries = preferExactPathMatch(matchingEntries, registryPath)
	if len(matchingEntries) == 0 {
		return nil
	}
	matchingEntry := matchingEntries[0]

	kindIdx, kindNode := findEntryMapping(matchingEntry, key)
	if kindIdx >= 0 && kindNode == nil {
		return fmt.Errorf("update config %q: registry %q with path %q has invalid %q selector type", configPath, registryURL, pathLabel(registryPath), key)
	}
	if kindNode == nil || kindNode.Kind != yaml.MappingNode {
		return nil
	}

	subIdx, subNode := findEntrySequence(kindNode, subKey)
	if subNode == nil {
		return nil
	}
	if subNode.Kind != yaml.SequenceNode {
		return fmt.Errorf("update config %q: registry %q with path %q has invalid %q.%s selector type", configPath, registryURL, pathLabel(registryPath), key, subKey)
	}

	toRemove := make(map[string]struct{}, len(values))
	for _, v := range values {
		toRemove[v] = struct{}{}
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
		removeMappingKey(matchingEntry, kindIdx)
	}

	return writeConfigDocument(configPath, info.Mode().Perm(), doc)
}
