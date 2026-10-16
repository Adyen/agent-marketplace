package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

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

func SetUserMarketplaceExclusion(configPath, endpoint, registrySlug string, kind ir.ResourceKind, name string, excluded bool) error {
	if !isGlobalConfigPath(configPath) {
		return fmt.Errorf("marketplace override requires the user configuration")
	}
	url, err := NormalizeMarketplaceURL(endpoint)
	if err != nil {
		return err
	}
	if kind == ir.KindCollection {
		return v4err("v4.marketplace-runtime-unsupported", "resources.exclude", 0, "Marketplace overrides target individual resources, not collections")
	}
	v4Kind := kind.String()
	if kind == ir.KindMcpServer {
		v4Kind = "mcp"
	}
	locator := registrySlug + "/" + v4Kind + "/" + name
	if err := validateV4ExclusionForSource(locator, V4SourceMarketplace); err != nil {
		return err
	}
	return withConfigPathLock(configPath, func() error {
		_, statErr := os.Lstat(configPath)
		if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		doc, mode, err := readOrInitConfigWithV4(configPath, true)
		if err != nil {
			return err
		}
		root, err := ensureDocumentMapping(doc)
		if err != nil {
			return err
		}
		if os.IsNotExist(statErr) {
			setMappingValue(root, "apiVersion", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: APIVersionV4})
		}
		if version := findMappingValue(root, "apiVersion"); version == nil || version.Value != APIVersionV4 {
			return v4err("v4.writer-opt-in", configPath, 0, "Marketplace overrides require a v4 user configuration")
		}
		original, err := encodeConfigDocument(configPath, doc)
		if err != nil {
			return err
		}
		if err := validateConfigDocumentData(configPath, original); err != nil {
			return err
		}
		sources := ensureTopLevelMapping(root, "sources")
		if sources == nil {
			return fmt.Errorf("sources must be a mapping")
		}
		alias := ""
		for i := 0; i+1 < len(sources.Content); i += 2 {
			if sourceURL := findMappingValue(sources.Content[i+1], "marketplace"); sourceURL != nil {
				canonical, err := NormalizeMarketplaceURL(sourceURL.Value)
				if err != nil {
					return err
				}
				if canonical == url {
					alias = sources.Content[i].Value
					break
				}
			}
		}
		if alias == "" {
			if !excluded {
				return nil
			}
			alias = "marketplace-override"
			for i := 2; findMappingValue(sources, alias) != nil; i++ {
				alias = fmt.Sprintf("marketplace-override-%d", i)
			}
			setMappingValue(sources, alias, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "marketplace"},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: url},
			}})
		}
		resources := ensureTopLevelMapping(root, "resources")
		if resources == nil {
			return fmt.Errorf("resources must be a mapping")
		}
		group := ensureTopLevelMapping(resources, alias)
		if group == nil {
			return fmt.Errorf("resource group %q must be a mapping", alias)
		}
		sequence := findMappingValue(group, "exclude")
		if sequence == nil && !excluded {
			return nil
		}
		sequence, err = ensureMappingSequence(group, "exclude")
		if err != nil {
			return err
		}
		for i, node := range sequence.Content {
			if node.Value != locator {
				continue
			}
			if excluded {
				return nil
			}
			sequence.Content = append(sequence.Content[:i], sequence.Content[i+1:]...)
			break
		}
		if excluded {
			sequence.Content = append(sequence.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: locator})
		}
		data, err := encodeConfigDocument(configPath, doc)
		if err != nil {
			return err
		}
		if err := validateConfigDocumentData(configPath, data); err != nil {
			return err
		}
		return writeConfigDocumentData(configPath, mode, data)
	})
}

func SetHarnessesInConfig(configPath string, harnesses []string) error {
	if isLocalConfigPath(configPath) {
		return fmt.Errorf("harnesses cannot be set in repository personal config %q", configPath)
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

	existing := make(map[string]bool, len(sequence.Content))
	for _, node := range sequence.Content {
		item := resolveAlias(node)
		if item.Kind == yaml.ScalarNode && item.Tag == "!!str" {
			existing[strings.TrimSpace(item.Value)] = true
		}
	}

	// Normalize, dedupe, and reject unknown or non-compiler targets before any
	// file is written, so a typo cannot land in the target harness set.
	canonical, strictErr := canonicalCompilerHarnesses("harnesses", harnesses)
	if strictErr != nil {
		normalized := normalizedUniqueValues(harnesses)
		if len(normalized) == 0 {
			return strictErr
		}
		for _, harness := range normalized {
			if !IsSupportedHarness(harness) && !existing[harness] {
				return strictErr
			}
		}
		canonical = normalized
	}

	setSequenceValues(sequence, canonical)

	// Validate the exact bytes that will be written before the atomic
	// replace, so a refusal leaves the file untouched. The check is
	// deliberately not the full LoadConfigFromPath reload the first
	// v4-aware version used: the loader also enforces Marketplace lock
	// policy, so a schema-valid harness save on a marketplace-source root
	// whose acquisition lock was absent or malformed wrote the file and
	// still failed with a Marketplace lock validation error. Lock availability is
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
