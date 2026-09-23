package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

// EnsureRegistryWithPathsAtRefInConfig ensures a URL/path/ref registry entry exists.
func EnsureRegistryWithPathsAtRefInConfig(configPath, url, ref, path string) error {
	return ensureRegistryWithPathsInConfig(configPath, url, ref, path, true)
}

func ensureRegistryWithPathsInConfig(configPath, url, ref, path string, requireRef bool) error {
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
			// URL matches (after normalization). Entries are only duplicates
			// when both their paths match (including empty). A root-level
			// entry (path="") and a sub-path entry (path="skills/X") are
			// distinct, allowing the same repo URL to have multiple entries
			// at different scopes.
			existingPath := yamlEntryPath(entryNode)
			if NormalizeRegistryPath(path) != NormalizeRegistryPath(existingPath) {
				continue // same URL, different path — not a duplicate
			}
			if requireRef {
				refNode := findMappingValue(entryNode, "ref")
				if refNode == nil || strings.TrimSpace(refNode.Value) != strings.TrimSpace(ref) {
					continue
				}
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
			if strings.TrimSpace(ref) != "" {
				if !registryEntryMatchesExactURLPathRef(entryNode, url, path, ref) {
					continue
				}
			} else if urlNode == nil || NormalizeGitURL(strings.TrimSpace(urlNode.Value)) != NormalizeGitURL(url) {
				continue
			}
			existingPath := yamlEntryPath(entryNode)
			if NormalizeRegistryPath(path) != NormalizeRegistryPath(existingPath) {
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
	return setRegistryExcludeAllInConfig(configPath, url, ref, path, false)
}

// SetRegistryExcludeAllInConfigAtRef suppresses exactly one URL/path/ref
// registry entry. Unlike the legacy helper, it never changes a sibling ref.
func SetRegistryExcludeAllInConfigAtRef(configPath, url, ref, path string) error {
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("registry ref is required")
	}
	return setRegistryExcludeAllInConfig(configPath, url, ref, path, true)
}

func setRegistryExcludeAllInConfig(configPath, url, ref, path string, requireRef bool) error {
	url = strings.TrimSpace(url)
	ref = strings.TrimSpace(ref)
	path = strings.TrimSpace(path)
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
			if requireRef {
				if !registryEntryMatchesExactURLPathRef(entryNode, url, path, ref) {
					continue
				}
			} else {
				urlNode := findRegistryURLNode(entryNode)
				if urlNode == nil || NormalizeGitURL(strings.TrimSpace(urlNode.Value)) != NormalizeGitURL(url) {
					continue
				}
				existingPath := yamlEntryPath(entryNode)
				if NormalizeRegistryPath(path) != NormalizeRegistryPath(existingPath) {
					continue // same URL, different path — not a duplicate
				}
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

	// The top-level harnesses field is schema-identical in v2/v3 and v4, and
	// the user-personal root is routinely v4 (init writes v4 directly and the
	// TUI offers startup migration), so harness editing is an explicit
	// v4-aware operation: edit the sequence in place in the parsed document
	// and validate the encoded candidate before anything is written, so a
	// refusal changes no bytes. Selection edits (install/remove) remain
	// legacy and keep rejecting v4 roots.
	doc, mode, err := readOrInitConfigWithV4(configPath, true)
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

	// Validate the exact bytes that will be written before the atomic
	// replace, so a refusal leaves the file untouched. The check is
	// deliberately not the full LoadConfigFromPath reload the first
	// v4-aware version used: the loader also enforces Marketplace lock
	// policy, so a schema-valid harness save on a marketplace-source root
	// whose acquisition lock was absent or malformed wrote the file and
	// still failed with v4.marketplace-lock-required. Lock availability is
	// runtime acquisition state, not a property of the document, and the
	// harness edit does not touch it. cfg.Validate() is deliberately not
	// run either: it hardcodes the user-personal layer flavor and would
	// mis-validate project-scope destinations.
	data, err := encodeConfigDocument(configPath, doc)
	if err != nil {
		return err
	}
	if err := validateConfigDocumentData(configPath, data); err != nil {
		slog.Debug("harnesses update rejected: edited config failed validation", "config_path", configPath, "error", err)
		return fmt.Errorf("edited config %q is not valid: %w", configPath, err)
	}
	if err := writeConfigDocumentData(configPath, mode, data); err != nil {
		return err
	}
	slog.Debug("harnesses updated in config", "config_path", configPath, "harnesses", len(harnesses))
	return nil
}

// AddResourceToExactRegistryEntry adds a per-kind include to one registry entry.
func AddResourceToExactRegistryEntry(configPath, registryURL, registryPath string, kind ir.ResourceKind, name string) error {
	if err := addValuesToExactRegistryEntry(configPath, registryURL, registryPath, kind, "include", []string{name}); err != nil {
		return err
	}
	// Clean up stale exclude for the same name so the resolver does not
	// suppress a resource that was just enabled.
	return removeValuesFromExactRegistryEntry(configPath, registryURL, registryPath, kind, "exclude", []string{name})
}

func AddResourceToExactRegistryEntryAtRef(configPath, registryURL, registryPath, registryRef string, kind ir.ResourceKind, name string) error {
	if err := addValuesToExactRegistryEntryAtRef(configPath, registryURL, registryPath, registryRef, kind, "include", []string{name}); err != nil {
		return err
	}
	return removeValuesFromExactRegistryEntryAtRef(configPath, registryURL, registryPath, registryRef, kind, "exclude", []string{name})
}

// addValuesToExactRegistryEntry adds values to a sub-key (e.g. "include" or
// "exclude") under a kind key within a registry entry matched by exact URL
// and path. When path is empty, only entries with no path (root entries)
// match — unlike addValuesToRegistrySubSequence where empty path means
// URL-only matching.
func addValuesToExactRegistryEntry(configPath, registryURL, registryPath string, kind ir.ResourceKind, subKey string, values []string) error {
	return addValuesToExactRegistryEntryWithRef(configPath, registryURL, registryPath, "", false, kind, subKey, values)
}

func addValuesToExactRegistryEntryAtRef(configPath, registryURL, registryPath, registryRef string, kind ir.ResourceKind, subKey string, values []string) error {
	return addValuesToExactRegistryEntryWithRef(configPath, registryURL, registryPath, registryRef, true, kind, subKey, values)
}

func addValuesToExactRegistryEntryWithRef(configPath, registryURL, registryPath, registryRef string, requireRef bool, kind ir.ResourceKind, subKey string, values []string) error {
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
		if requireRef {
			if registryEntryMatchesExactURLPathRef(entryNode, registryURL, registryPath, registryRef) {
				matchingEntries = append(matchingEntries, entryNode)
			}
			continue
		}
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
	if len(matchingEntries) == 0 {
		return fmt.Errorf("update config %q: registry %q with path %q not found", configPath, registryURL, pathLabel(registryPath))
	}
	if requireRef && len(matchingEntries) > 1 {
		return fmt.Errorf("update config %q: registry %q with path %q and ref %q matches multiple entries", configPath, registryURL, pathLabel(registryPath), registryRef)
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
	return removeValuesFromExactRegistryEntryWithRef(configPath, registryURL, registryPath, "", false, kind, subKey, values)
}

func removeValuesFromExactRegistryEntryAtRef(configPath, registryURL, registryPath, registryRef string, kind ir.ResourceKind, subKey string, values []string) error {
	return removeValuesFromExactRegistryEntryWithRef(configPath, registryURL, registryPath, registryRef, true, kind, subKey, values)
}

func removeValuesFromExactRegistryEntryWithRef(configPath, registryURL, registryPath, registryRef string, requireRef bool, kind ir.ResourceKind, subKey string, values []string) error {
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
		if requireRef {
			if registryEntryMatchesExactURLPathRef(entryNode, registryURL, registryPath, registryRef) {
				matchingEntries = append(matchingEntries, entryNode)
			}
			continue
		}
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
	if len(matchingEntries) == 0 {
		return nil
	}
	if requireRef && len(matchingEntries) > 1 {
		return fmt.Errorf("update config %q: registry %q with path %q and ref %q matches multiple entries", configPath, registryURL, pathLabel(registryPath), registryRef)
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

type V4RegistryBinding struct {
	Name  string
	Entry RegistryEntry
}

func EnsureV4RegistrySourceInConfig(stagedPath, originalPath string, source Source, entry RegistryEntry, localBase string) (V4RegistryBinding, error) {
	var binding V4RegistryBinding
	if err := ValidateRegistryPath(entry.Path); err != nil {
		return binding, err
	}
	if source == UserPersonal {
		if err := ValidateGlobalRegistryWrite(entry); err != nil {
			return binding, err
		}
	}
	kind, _, err := v4LayerForSource(source, originalPath)
	if err != nil {
		return binding, err
	}
	var directory string
	if entry.IsLocal() {
		if entry.Ref != "" {
			return binding, v4err("v4.source-field", "sources", 0, "directory sources do not support a Git ref")
		}
		localPath, err := entry.ResolveLocalPath(localBase)
		if err != nil {
			return binding, err
		}
		directory = entry.BundlePath(localPath)
		if err := ValidateLocalRegistryPath(directory); err != nil {
			return binding, err
		}
		binding.Entry = RegistryEntry{URL: directory, V4Kind: V4SourceDirectory, SourceHarness: entry.SourceHarness}
		if entry.IsRepoRelativeLocal() {
			base := v4DirectoryBase(originalPath, true, kind)
			if !v4PathWithin(base, directory) {
				return binding, v4err("v4.directory-path", "sources", 0, "relative directory source must remain within its source base directory")
			}
			rel, err := filepath.Rel(base, directory)
			if err != nil {
				return binding, err
			}
			directory = filepath.ToSlash(rel)
			if directory != "." {
				directory = "./" + directory
			}
		}
	} else {
		if entry.Ref == "" {
			return binding, v4err("v4.git-ref-required", "sources", 0, "git sources require an explicit ref")
		}
		binding.Entry = RegistryEntry{URL: entry.URL, Ref: entry.Ref, Path: NormalizeRegistryPath(entry.Path), V4Kind: V4SourceGit, SourceHarness: entry.SourceHarness}
	}
	err = editV4RootConfig(stagedPath, originalPath, source, func(root *yaml.Node, parsed *V4Config) (bool, error) {
		for name, existing := range parsed.Sources {
			matches := existing.SourceHarness == entry.SourceHarness
			if entry.IsLocal() {
				matches = matches && existing.Kind == V4SourceDirectory && existing.Directory == binding.Entry.URL
			} else {
				matches = matches && existing.Kind == V4SourceGit && existing.CanonicalURL == NormalizeGitURL(entry.URL) && existing.Ref == entry.Ref && existing.Path == binding.Entry.Path
			}
			if matches && (binding.Name == "" || name < binding.Name) {
				binding.Name = name
			}
		}
		if binding.Name != "" {
			return false, nil
		}
		occupied := make(map[string]v4MigrationSource, len(parsed.Sources))
		for name := range parsed.Sources {
			occupied[name] = v4MigrationSource{}
		}
		binding.Name = v4MigrationRegistrySourceName(occupied, binding.Entry.URL)
		sources := ensureTopLevelMapping(root, "sources")
		if sources == nil {
			return false, v4err("v4.type", "sources", 0, "sources must be a mapping")
		}
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		field := "git"
		value := entry.URL
		if entry.IsLocal() {
			field, value = "directory", directory
		}
		setMappingValue(node, field, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
		if !entry.IsLocal() {
			setMappingValue(node, "ref", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: entry.Ref})
			if binding.Entry.Path != "" {
				setMappingValue(node, "path", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: binding.Entry.Path})
			}
		}
		if entry.SourceHarness != "" {
			setMappingValue(node, "source_harness", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: entry.SourceHarness})
		}
		setMappingValue(sources, binding.Name, node)
		return true, nil
	})
	return binding, err
}

func EnsureV4MarketplaceSourceInConfig(stagedPath, originalPath string, source Source, marketplaceURL string) (string, error) {
	canonical, err := NormalizeMarketplaceURL(marketplaceURL)
	if err != nil {
		return "", err
	}
	var sourceName string
	err = editV4RootConfig(stagedPath, originalPath, source, func(root *yaml.Node, parsed *V4Config) (bool, error) {
		for name, existing := range parsed.Sources {
			if existing.Kind == V4SourceMarketplace && existing.CanonicalURL == canonical && (sourceName == "" || name < sourceName) {
				sourceName = name
			}
		}
		if sourceName != "" {
			return false, nil
		}
		occupied := make(map[string]v4MigrationSource, len(parsed.Sources))
		for name := range parsed.Sources {
			occupied[name] = v4MigrationSource{}
		}
		sourceName = v4MigrationMarketplaceSourceName(occupied, "")
		sources := ensureTopLevelMapping(root, "sources")
		if sources == nil {
			return false, v4err("v4.type", "sources", 0, "sources must be a mapping")
		}
		entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setMappingValue(entry, "marketplace", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: canonical})
		setMappingValue(sources, sourceName, entry)
		return true, nil
	})
	return sourceName, err
}

func AddV4MarketplaceRequestInConfig(stagedPath, originalPath string, source Source, sourceName string, pin MarketplaceResourcePin, revision string) error {
	if err := ValidateMarketplaceResourcePin(pin); err != nil {
		return err
	}
	if pin.RegistrySlug == "" {
		return v4err("v4.registry-slug", "resources."+sourceName, 0, "Marketplace request requires a verified registry slug")
	}
	locator := fmt.Sprintf("%s/%s/%s", pin.RegistrySlug, v4MigrationMarketplaceKind(pin.Kind), pin.Name)
	if _, _, _, err := parseV4Locator(locator, sourceName); err != nil {
		return v4err("v4.locator", locator, 0, "%v", err)
	}
	return editV4RootConfig(stagedPath, originalPath, source, func(root *yaml.Node, parsed *V4Config) (bool, error) {
		bound, ok := parsed.Sources[sourceName]
		if !ok || bound.Kind != V4SourceMarketplace {
			return false, v4err("v4.source-kind", "sources."+sourceName, 0, "Marketplace request requires a Marketplace source")
		}
		resources := ensureTopLevelMapping(root, "resources")
		if resources == nil {
			return false, v4err("v4.type", "resources", 0, "resources must be a mapping")
		}
		group := findMappingValue(resources, sourceName)
		if group == nil {
			group = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setMappingValue(resources, sourceName, group)
		}
		var includes *yaml.Node
		switch group.Kind {
		case yaml.SequenceNode:
			includes = group
		case yaml.MappingNode:
			var err error
			includes, err = ensureMappingSequence(group, "include")
			if err != nil {
				return false, err
			}
		default:
			return false, v4err("v4.type", "resources."+sourceName, 0, "resource group must be an include list or mapping")
		}
		for _, item := range includes.Content {
			existing := item.Value
			if item.Kind == yaml.MappingNode && len(item.Content) > 0 {
				existing = item.Content[0].Value
			}
			if existing == locator {
				wantAs := ""
				if pin.InstalledName != "" && pin.InstalledName != pin.Name {
					wantAs = pin.InstalledName
				}
				if item.Kind == yaml.ScalarNode && wantAs == "" && revision == "" {
					return false, nil
				}
				if item.Kind == yaml.MappingNode && len(item.Content) == 2 {
					as, rev := findMappingValue(item.Content[1], "as"), findMappingValue(item.Content[1], "revision")
					asValue, revValue := "", ""
					if as != nil {
						asValue = as.Value
					}
					if rev != nil {
						revValue = rev.Value
					}
					if asValue == wantAs && revValue == revision {
						return false, nil
					}
				}
				return false, v4err("v4.duplicate-request", "resources."+sourceName, 0, "Marketplace resource %q is already requested with different options", locator)
			}
		}
		item := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: locator}
		if pin.InstalledName != "" && pin.InstalledName != pin.Name || revision != "" {
			options := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			if pin.InstalledName != "" && pin.InstalledName != pin.Name {
				setMappingValue(options, "as", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: pin.InstalledName})
			}
			if revision != "" {
				setMappingValue(options, "revision", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: revision})
			}
			item = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setMappingValue(item, locator, options)
		}
		includes.Content = append(includes.Content, item)
		return true, nil
	})
}

func AddV4ResourcesToSourceInConfig(stagedPath, originalPath string, source Source, sourceName string, kind ir.ResourceKind, names []string) error {
	kindName := v4ResourceKindName(kind)
	if kindName == "unknown" {
		return v4err("v4.locator", sourceName, 0, "unsupported resource kind")
	}
	locators := make([]string, 0, len(names))
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		locator := kindName + "/" + name
		if _, _, _, err := parseV4Locator(locator, sourceName); err != nil {
			return v4err("v4.locator", locator, 0, "%v", err)
		}
		if !wanted[locator] {
			wanted[locator] = true
			locators = append(locators, locator)
		}
	}
	if len(locators) == 0 {
		return nil
	}
	return editV4RootConfig(stagedPath, originalPath, source, func(root *yaml.Node, parsed *V4Config) (bool, error) {
		bound, ok := parsed.Sources[sourceName]
		if !ok || bound.Kind == V4SourceMarketplace {
			return false, v4err("v4.source-kind", "sources."+sourceName, 0, "resource selection requires a git or directory source")
		}
		resources := ensureTopLevelMapping(root, "resources")
		if resources == nil {
			return false, v4err("v4.type", "resources", 0, "resources must be a mapping")
		}
		group := findMappingValue(resources, sourceName)
		if group == nil {
			group = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setMappingValue(resources, sourceName, group)
		}
		changed := false
		var includes *yaml.Node
		switch group.Kind {
		case yaml.SequenceNode:
			includes = group
		case yaml.MappingNode:
			if excludes := findMappingValue(group, "exclude"); excludes != nil {
				filtered := excludes.Content[:0]
				for _, node := range excludes.Content {
					if node.Value == "*" || node.Value == kindName+"/*" {
						return false, v4err("v4.selection-conflict", "resources."+sourceName+".exclude", 0, "cannot re-enable one resource while its kind is excluded")
					}
					if wanted[node.Value] {
						changed = true
						continue
					}
					filtered = append(filtered, node)
				}
				excludes.Content = filtered
				if len(filtered) == 0 {
					removeMappingKeyByName(group, "exclude")
				}
			}
			var err error
			includes, err = ensureMappingSequence(group, "include")
			if err != nil {
				return false, err
			}
		default:
			return false, v4err("v4.type", "resources."+sourceName, 0, "resource group must be an include list or mapping")
		}
		for _, item := range includes.Content {
			delete(wanted, item.Value)
		}
		for _, locator := range locators {
			if wanted[locator] {
				includes.Content = append(includes.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: locator})
				changed = true
			}
		}
		return changed, nil
	})
}

func AddV4TagToSourceInConfig(stagedPath, originalPath string, source Source, sourceName, tag string) error {
	if strings.TrimSpace(tag) == "" {
		return v4err("v4.type", "resources."+sourceName+".tags", 0, "tag must be a non-empty string")
	}
	return editV4RootConfig(stagedPath, originalPath, source, func(root *yaml.Node, parsed *V4Config) (bool, error) {
		bound, ok := parsed.Sources[sourceName]
		if !ok || bound.Kind == V4SourceMarketplace {
			return false, v4err("v4.source-kind", "sources."+sourceName, 0, "tag selection requires a git or directory source")
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
		tags, err := ensureMappingSequence(group, "tags")
		if err != nil {
			return false, err
		}
		for _, item := range tags.Content {
			if item.Value == tag {
				return false, nil
			}
		}
		tags.Content = append(tags.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: tag})
		return true, nil
	})
}

func SelectAllV4SourceInConfig(stagedPath, originalPath string, source Source, sourceName string) error {
	return editV4RootConfig(stagedPath, originalPath, source, func(root *yaml.Node, parsed *V4Config) (bool, error) {
		bound, ok := parsed.Sources[sourceName]
		if !ok || bound.Kind == V4SourceMarketplace {
			return false, v4err("v4.source-kind", "sources."+sourceName, 0, "all selection requires a git or directory source")
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
		changed := !boolMappingValue(group, "all")
		if changed {
			setMappingValue(group, "all", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
		}
		if excludes := findMappingValue(group, "exclude"); excludes != nil {
			filtered := excludes.Content[:0]
			for _, node := range excludes.Content {
				if node.Value == "*" {
					changed = true
					continue
				}
				filtered = append(filtered, node)
			}
			excludes.Content = filtered
			if len(filtered) == 0 {
				removeMappingKeyByName(group, "exclude")
			}
		}
		return changed, nil
	})
}

func editV4RootConfig(stagedPath, originalPath string, source Source, mutate func(*yaml.Node, *V4Config) (bool, error)) error {
	if !source.IsWritable() {
		return fmt.Errorf("cannot edit externally managed v4 configuration at %q", originalPath)
	}
	return withConfigPathLock(stagedPath, func() error {
		data, err := readConfigFile(stagedPath)
		if err != nil {
			return err
		}
		info, err := os.Stat(stagedPath)
		if err != nil {
			return err
		}
		doc, err := parseConfigDocument(stagedPath, data)
		if err != nil {
			return err
		}
		root, err := ensureDocumentMapping(doc)
		if err != nil {
			return err
		}
		parsed, err := LoadV4ConfigFromRootData(originalPath, source, data)
		if err != nil {
			return err
		}
		changed, err := mutate(root, parsed)
		if err != nil || !changed {
			return err
		}
		candidate, err := encodeConfigDocument(stagedPath, doc)
		if err != nil {
			return err
		}
		if _, err := LoadV4ConfigFromRootData(originalPath, source, candidate); err != nil {
			return fmt.Errorf("edited v4 config %q is not valid: %w", originalPath, err)
		}
		return writeConfigDocumentData(stagedPath, info.Mode().Perm(), candidate)
	})
}

func LoadV4ConfigFromRootData(originalPath string, source Source, data []byte) (*V4Config, error) {
	kind, organizationPath, err := v4LayerForSource(source, originalPath)
	if err != nil {
		return nil, err
	}
	parsed, err := loadV4TreeWithOverlays(originalPath, true, map[string]bool{}, nil, data, organizationPath, kind)
	if err != nil {
		return nil, err
	}
	if err := parsed.validateLayerFields(kind); err != nil {
		return nil, err
	}
	return parsed, nil
}
