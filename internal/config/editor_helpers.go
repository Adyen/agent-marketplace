package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

const defaultConfigMode os.FileMode = 0o644

// GlobalConfigPath returns the filesystem path of the global config
// (~/.agents/config.yaml). Returns an error if the home directory cannot
// be resolved.
func GlobalConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(homeDir, ".agents", "config.yaml"), nil
}

// LocalConfigPath returns the filesystem path of the per-repo local config
// (<repoRoot>/.agents/config.local.yaml).
func LocalConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".agents", "config.local.yaml")
}

// ProjectConfigPath returns the absolute path to the project config file
// (<repoRoot>/.agents/config.yaml).
func ProjectConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".agents", "config.yaml")
}

func entryResourcesForKind(entry RegistryEntry, kind ir.ResourceKind) []string {
	return entry.SelectorForKind(kind).Include
}

// readOrInitConfig reads and parses the YAML at configPath. If the file does
// not exist, returns a document pre-populated with apiVersion. When the path
// is the global config (~/.agents/config.yaml), the skeleton includes
// harnesses: [factory, windsurf] (required for global configs); for project
// or local configs, harnesses is omitted so they inherit from the org/global
// layer during merge. The returned mode is the existing file's perms, or
// defaultConfigMode for a new file.
func readOrInitConfig(configPath string) (*yaml.Node, os.FileMode, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			var content []*yaml.Node
			// Local config files use the LocalConfig schema which does not
			// have apiVersion or harnesses. Only write apiVersion for
			// Config-type files (global, repo, org).
			if !isLocalConfigPath(configPath) {
				content = append(content,
					&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "apiVersion"},
					&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: APIVersionV2},
				)
			}
			// Include harnesses only for the global config path, where
			// they are required. Project/local configs omit harnesses
			// to inherit from the org/global layer during merge.
			if isGlobalConfigPath(configPath) {
				harnesses := DefaultHarnesses()
				seqContent := make([]*yaml.Node, 0, len(harnesses))
				for _, h := range harnesses {
					seqContent = append(seqContent, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: h})
				}
				content = append(content,
					&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "harnesses"},
					&yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: seqContent},
				)
			}
			doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{
				Kind: yaml.MappingNode, Tag: "!!map",
				Content: content,
			}}}
			return doc, defaultConfigMode, nil
		}
		return nil, 0, fmt.Errorf("read config %q: %w", configPath, err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		return nil, 0, fmt.Errorf("stat config %q: %w", configPath, err)
	}

	doc, err := parseConfigDocument(configPath, data)
	if err != nil {
		return nil, 0, err
	}

	return doc, info.Mode().Perm(), nil
}

// isGlobalConfigPath reports whether configPath refers to the global
// (user-personal) config file at ~/.agents/config.yaml. This determines
// whether a skeleton config should include harnesses (required on global
// configs, omitted on project/local configs to inherit from merge).
func isGlobalConfigPath(configPath string) bool {
	gp, err := GlobalConfigPath()
	if err != nil {
		return false
	}
	return filepath.Clean(configPath) == filepath.Clean(gp)
}

// isLocalConfigPath reports whether configPath refers to a local/personal
// config file (config.local.yaml). These files use the LocalConfig schema
// which does not have apiVersion or harnesses fields.
func isLocalConfigPath(configPath string) bool {
	return strings.HasSuffix(filepath.Clean(configPath), "config.local.yaml")
}

// EnsureGlobalConfig creates the user-level config skeleton at
// ~/.agents/config.yaml if it does not already exist. The skeleton carries
// only the required global fields (apiVersion, harnesses); logging is left
// out so the file stays clean, and the logging runtime falls back to its
// code defaults unless the user opts in with an explicit logging block. See
// docs/logging.md for the override keys.
func EnsureGlobalConfig() error {
	configPath, err := GlobalConfigPath()
	if err != nil {
		return err
	}
	doc, mode, err := readOrInitConfig(configPath)
	if err != nil {
		return err
	}

	var data []byte
	if !isEmptyConfigDocument(doc) {
		var encoded bytes.Buffer
		encoder := yaml.NewEncoder(&encoded)
		encoder.SetIndent(2)
		if err := encoder.Encode(doc); err != nil {
			return fmt.Errorf("encode config %q: %w", configPath, err)
		}
		if err := encoder.Close(); err != nil {
			return fmt.Errorf("close yaml encoder for %q: %w", configPath, err)
		}
		data = encoded.Bytes()
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("create config directory for %q: %w", configPath, err)
	}

	// Exclusively create the file so concurrent runs cannot clobber a config
	// another process is writing. If it already exists, this is a no-op.
	f, err := os.OpenFile(configPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return fmt.Errorf("create config %q: %w", configPath, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write config %q: %w", configPath, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close config %q: %w", configPath, err)
	}
	return nil
}

func parseConfigDocument(configPath string, data []byte) (*yaml.Node, error) {
	doc := &yaml.Node{}
	if len(bytes.TrimSpace(data)) == 0 {
		doc.Kind = yaml.DocumentNode
		return doc, nil
	}

	if err := yaml.Unmarshal(data, doc); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", configPath, err)
	}

	// yaml.Unmarshal into a single Node keeps only the FIRST document of a
	// multi-document stream, and writeConfigDocument then encodes just that
	// one back over the whole file — silently DELETING every `---` section
	// after the first. Refusing the edit is the only safe answer: every caller
	// surfaces the error, whereas the truncation was invisible. Found while
	// reviewing bead agent-marketplace-tbjf, where `sync --prune-stale` edits
	// the config without the user naming a file, so nobody would look.
	if extra, err := countExtraYAMLDocuments(data); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", configPath, err)
	} else if extra > 0 {
		return nil, fmt.Errorf("edit config %q: file contains %d additional YAML document(s) after the first; automated edits would delete them — split the file or remove the extra '---' documents", configPath, extra)
	}

	return doc, nil
}

// countExtraYAMLDocuments reports how many YAML documents follow the first one
// in data. A trailing "---" with nothing after it decodes to no document and
// therefore does not count.
func countExtraYAMLDocuments(data []byte) (int, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	count := 0
	for {
		var node yaml.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, err
		}
		count++
	}
	if count <= 1 {
		return 0, nil
	}
	return count - 1, nil
}

func ensureDocumentMapping(doc *yaml.Node) (*yaml.Node, error) {
	if doc.Kind == 0 {
		doc.Kind = yaml.DocumentNode
	}

	if doc.Kind != yaml.DocumentNode {
		return nil, fmt.Errorf("expected YAML document node, got kind %d", doc.Kind)
	}

	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
		}}
	}

	root := doc.Content[0]
	if root.Kind == 0 {
		root.Kind = yaml.MappingNode
		root.Tag = "!!map"
	}

	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected top-level mapping node, got kind %d", root.Kind)
	}

	return root, nil
}

func findTopLevelSequence(mapping *yaml.Node, key string) (int, *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		keyNode := mapping.Content[i]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Value != key {
			continue
		}

		return i, mapping.Content[i+1]
	}

	return -1, nil
}

func ensureTopLevelSequence(mapping *yaml.Node, key string) (*yaml.Node, error) {
	return ensureMappingSequence(mapping, key)
}

func ensureMappingSequence(mapping *yaml.Node, key string) (*yaml.Node, error) {
	if _, valueNode := findTopLevelSequence(mapping, key); valueNode != nil {
		if valueNode.Kind == 0 {
			valueNode.Kind = yaml.SequenceNode
			valueNode.Tag = "!!seq"
		}

		if valueNode.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("expected %q to be a sequence", key)
		}

		return valueNode, nil
	}

	keyNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: key,
	}
	valueNode := &yaml.Node{
		Kind: yaml.SequenceNode,
		Tag:  "!!seq",
	}
	mapping.Content = append(mapping.Content, keyNode, valueNode)

	return valueNode, nil
}

func removeTopLevelKey(mapping *yaml.Node, keyIndex int) {
	removeMappingKey(mapping, keyIndex)
}

// removeMappingKey removes a key-value pair at the given index from a
// mapping node. No-op if the index is out of range.
func removeMappingKey(mapping *yaml.Node, keyIndex int) {
	if keyIndex < 0 || keyIndex+1 >= len(mapping.Content) {
		return
	}
	mapping.Content = append(mapping.Content[:keyIndex], mapping.Content[keyIndex+2:]...)
}

// removeMappingKeyByName removes the key/value pair with the given key name
// from a mapping node. No-op when the node is nil/not a mapping or the key is
// absent. Returns true when something was removed.
func removeMappingKeyByName(mapping *yaml.Node, key string) bool {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Kind == yaml.ScalarNode && mapping.Content[i].Value == key {
			removeMappingKey(mapping, i)
			return true
		}
	}
	return false
}

// clearEntryExcludeAll removes exclude_all flags from a registry entry node so
// that an explicit include is not silently nullified. Because exclude_all
// always wins in the resolver, "include X" alongside "exclude_all: true" is a
// contradictory entry; enabling a resource therefore clears both the
// entry-level exclude_all sugar and the per-kind <kindKey>.exclude_all flag.
// (Per-kind exclude_all for OTHER kinds is left untouched.)
func clearEntryExcludeAll(entryNode *yaml.Node, kindKey string) {
	if entryNode == nil || entryNode.Kind != yaml.MappingNode {
		return
	}
	removeMappingKeyByName(entryNode, "exclude_all")
	if kindKey != "" {
		if kindMapping := findMappingValue(entryNode, kindKey); kindMapping != nil {
			removeMappingKeyByName(kindMapping, "exclude_all")
		}
	}
}

// findEntrySequence finds a key-value pair in a mapping node where the value
// is a sequence. Returns (keyIndex, valueNode). Returns (-1, nil) if not
// found.
func findEntrySequence(mapping *yaml.Node, key string) (int, *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		keyNode := mapping.Content[i]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Value != key {
			continue
		}
		return i, mapping.Content[i+1]
	}
	return -1, nil
}

func findMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(mapping.Content); i += 2 {
		keyNode := mapping.Content[i]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Value != key {
			continue
		}

		return mapping.Content[i+1]
	}

	return nil
}

// boolMappingValue reports whether a mapping entry's value decodes to the YAML
// boolean true. It decodes the node through yaml.v3 rather than comparing raw
// text so that YAML 1.1 spellings (true/True/yes/on/...) are interpreted the
// same way the loader interprets RegistryEntry.Default / Required / ExcludeAll.
func boolMappingValue(mapping *yaml.Node, key string) bool {
	node := findMappingValue(mapping, key)
	if node == nil {
		return false
	}
	var b bool
	if err := node.Decode(&b); err != nil {
		return false
	}
	return b
}

// entryNodeAutoSelectsKind reports whether a registry entry node would
// auto-select resources of the given kind without an explicit include — the
// same trigger the resolver uses for autoSelectAll (see
// resolver.resolveSelectedResources / hasExplicitSelection): default:true or
// required:true, with no tags, no non-empty include on ANY kind, and no
// exclude_all that would drop this kind.
//
// Non-required hooks and MCP servers are never auto-selected or tag-selected
// because they can execute commands or start processes. Re-enabling either kind
// therefore always needs an explicit include; required organization policy is
// trusted and retains broad selection.
//
// When this holds, a resource is already selected implicitly, so re-enabling
// it must NOT add an explicit include: doing so flips the entry into
// explicit-selection mode and silently deselects everything else. Callers only
// need to drop any stale exclude in that case.
//
// exclude_all (entry-level or per-kind) deselects resources, so a non-required
// entry carrying it does NOT auto-select the affected kind — the reinstall
// must add an explicit include and clear the stale exclude_all instead of
// no-oping. required:true entries are immune to excludes in the resolver, so
// exclude_all does not disqualify them.
func entryNodeAutoSelectsKind(entryNode *yaml.Node, kind ir.ResourceKind) bool {
	if entryNode == nil || entryNode.Kind != yaml.MappingNode {
		return false
	}
	required := boolMappingValue(entryNode, "required")
	if !required && KindRequiresExplicitSelection(kind) {
		return false
	}
	if !required && !boolMappingValue(entryNode, "default") {
		return false
	}
	// A tags key (even empty) is an explicit selection signal, mirroring the
	// resolver's `entry.Tags != nil` check.
	if findMappingValue(entryNode, "tags") != nil {
		return false
	}
	// An entry-level exclude_all deselects every kind (non-required entries).
	if !required && boolMappingValue(entryNode, "exclude_all") {
		return false
	}
	targetKey := skill.ConfigKeyForKind(kind)
	for _, k := range AllKinds {
		kindKey := skill.ConfigKeyForKind(k)
		if kindKey == "" {
			continue
		}
		kindMapping := findMappingValue(entryNode, kindKey)
		if kindMapping == nil {
			continue
		}
		// A non-empty include on ANY kind puts the entry into
		// explicit-selection mode, disabling auto-select-all.
		if _, incNode := findEntrySequence(kindMapping, "include"); incNode != nil && len(incNode.Content) > 0 {
			return false
		}
		// A per-kind exclude_all deselects only that kind (non-required).
		if !required && kindKey == targetKey && boolMappingValue(kindMapping, "exclude_all") {
			return false
		}
	}
	return true
}

// findRegistryURLNode returns the YAML node whose value is the registry's
// location (URL or path). It prefers the "url" key; if absent or empty, it
// falls back to "path" (which is an alias for "url" when "url" is not
// specified). The empty-value fallback mirrors RegistryEntry.UnmarshalYAML,
// which treats `url: ""` as absent and uses "path" as the location alias.
func findRegistryURLNode(entryNode *yaml.Node) *yaml.Node {
	if urlNode := findMappingValue(entryNode, "url"); urlNode != nil {
		if strings.TrimSpace(urlNode.Value) != "" {
			return urlNode
		}
	}
	return findMappingValue(entryNode, "path")
}

// setMappingValue sets or replaces the value node for a key in a mapping node.
// If the key already exists, its value is replaced in-place. If not, the
// key-value pair is appended.
func setMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Kind == yaml.ScalarNode && mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

func stringSliceContains(values []string, wanted string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == wanted {
			return true
		}
	}
	return false
}

func sequenceValues(sequence *yaml.Node) []string {
	values := make([]string, 0, len(sequence.Content))
	for _, node := range sequence.Content {
		if node.Kind != yaml.ScalarNode {
			continue
		}

		value := strings.TrimSpace(node.Value)
		if value == "" {
			continue
		}

		values = append(values, value)
	}

	return normalizedUniqueValues(values)
}

func setSequenceValues(sequence *yaml.Node, values []string) {
	existing := make(map[string]*yaml.Node, len(sequence.Content))
	for _, node := range sequence.Content {
		if node.Kind != yaml.ScalarNode {
			continue
		}

		value := strings.TrimSpace(node.Value)
		if value == "" {
			continue
		}

		if _, ok := existing[value]; !ok {
			existing[value] = node
		}
	}

	normalized := normalizedUniqueValues(values)
	content := make([]*yaml.Node, 0, len(normalized))
	for _, value := range normalized {
		node, ok := existing[value]
		if !ok {
			node = &yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: value,
			}
		} else {
			node.Value = value
		}
		content = append(content, node)
	}

	sequence.Content = content
}

func normalizedUniqueValues(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		if _, ok := seen[value]; ok {
			continue
		}

		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}

	return normalized
}

func appendUniqueValues(existing, additions []string) []string {
	return append(normalizedUniqueValues(existing), normalizedUniqueValues(additions)...)
}

func writeConfigDocument(configPath string, mode os.FileMode, doc *yaml.Node) error {
	// Edit through a symlink, not over it. The atomic rename below replaces
	// whatever `configPath` names, so on the common dotfile setup where
	// .agents/config.yaml is a symlink into a shared/checked-out file, the
	// edit would DETACH the repo from that file and leave the real config
	// untouched — while the CLI reported success. Resolving first keeps the
	// link intact and edits the file the user actually reads.
	// Found while reviewing bead agent-marketplace-tbjf: `sync --prune-stale`
	// performs this edit unattended.
	if resolved, err := filepath.EvalSymlinks(configPath); err == nil {
		configPath = resolved
	} else if info, lerr := os.Lstat(configPath); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("resolve config symlink %q: %w", configPath, err)
	}
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory for %q: %w", configPath, err)
	}

	var data []byte
	if !isEmptyConfigDocument(doc) {
		var encoded bytes.Buffer
		encoder := yaml.NewEncoder(&encoded)
		encoder.SetIndent(2)
		if err := encoder.Encode(doc); err != nil {
			return fmt.Errorf("encode config %q: %w", configPath, err)
		}
		if err := encoder.Close(); err != nil {
			return fmt.Errorf("close yaml encoder for %q: %w", configPath, err)
		}
		data = encoded.Bytes()
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(configPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp config for %q: %w", configPath, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp config for %q: %w", configPath, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config for %q: %w", configPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config for %q: %w", configPath, err)
	}

	if err := os.Rename(tmp.Name(), configPath); err != nil {
		return fmt.Errorf("replace config %q: %w", configPath, err)
	}

	return nil
}

// ensureTopLevelMapping ensures the top-level key exists as a mapping node.
// Returns the mapping node, or nil on error.
func ensureTopLevelMapping(root *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		if keyNode.Kind == yaml.ScalarNode && keyNode.Value == key {
			valueNode := root.Content[i+1]
			// If it's a sequence (legacy list form), convert to mapping.
			if valueNode.Kind == yaml.SequenceNode {
				// Convert [a, b] → {include: [a, b]}
				newMapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				includeKey := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "include"}
				newMapping.Content = append(newMapping.Content, includeKey, valueNode)
				root.Content[i+1] = newMapping
				return newMapping
			}
			if valueNode.Kind == yaml.MappingNode {
				return valueNode
			}
			if valueNode.Kind == 0 {
				valueNode.Kind = yaml.MappingNode
				valueNode.Tag = "!!map"
				return valueNode
			}
			return nil // unexpected type
		}
	}

	// Key doesn't exist — create a new mapping node.
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valueNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content, keyNode, valueNode)
	return valueNode
}

// findEntryMapping finds a key-value pair in a mapping node where the value
// is a mapping. Returns (keyIndex, valueNode). Returns (-1, nil) if not found.
func findEntryMapping(mapping *yaml.Node, key string) (int, *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		keyNode := mapping.Content[i]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Value != key {
			continue
		}
		valueNode := mapping.Content[i+1]
		if valueNode.Kind == yaml.MappingNode {
			return i, valueNode
		}
		// Legacy sequence form — treat as mapping with include only.
		if valueNode.Kind == yaml.SequenceNode {
			newMapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			includeKey := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "include"}
			newMapping.Content = append(newMapping.Content, includeKey, valueNode)
			mapping.Content[i+1] = newMapping
			return i, newMapping
		}
		return i, nil
	}
	return -1, nil
}

// ensureEntryMapping ensures a key within an entry mapping exists as a
// mapping node. If the key exists but is a sequence (legacy form), it is
// converted to a mapping with the sequence as "include".
func ensureEntryMapping(entryNode *yaml.Node, key string) *yaml.Node {
	idx, found := findEntryMapping(entryNode, key)
	if found != nil {
		return found
	}
	if idx >= 0 {
		// Key exists but value isn't a mapping — shouldn't happen after findEntryMapping conversion.
		return nil
	}

	// Create new mapping.
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valueNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	entryNode.Content = append(entryNode.Content, keyNode, valueNode)
	return valueNode
}

func isEmptyConfigDocument(doc *yaml.Node) bool {
	if doc == nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return true
	}
	root := doc.Content[0]
	return root == nil || (root.Kind == yaml.MappingNode && len(root.Content) == 0)
}

// yamlEntryPath returns the path of a YAML registry entry node, or "" if it
// has no path (root entry). When "url" is present, the subdirectory is taken
// from "path" (subdirectory). When "url" is absent, "path" is a URL alias,
// so there is no subdirectory.
func yamlEntryPath(entryNode *yaml.Node) string {
	urlNode := findMappingValue(entryNode, "url")
	if urlNode == nil || strings.TrimSpace(urlNode.Value) == "" {
		// No URL: "path" is a URL alias, not a subdirectory.
		return ""
	}
	if pathNode := findMappingValue(entryNode, "path"); pathNode != nil {
		return strings.TrimSpace(pathNode.Value)
	}
	return ""
}

// pathLabel returns a human-readable label for a registry path value,
// using "(root)" for the empty-string root path.
func pathLabel(p string) string {
	if p == "" {
		return "(root)"
	}
	return p
}

// registryPathMatches reports whether a resolved artifact's registryPath
// corresponds to a config entry's path. The resolved artifact may carry a
// sub-bundle path below the entry's configured path (e.g. entry has
// path="plugins" but the artifact has registryPath="plugins/personal"),
// so this uses prefix matching rather than exact equality. Both the exact
// match and a trailing-slash prefix match are accepted.
func registryPathMatches(entryPath, artifactPath string) bool {
	ep := strings.TrimSpace(entryPath)
	ap := strings.TrimSpace(artifactPath)
	if ep == ap {
		return true
	}
	if ep != "" && strings.HasPrefix(ap, ep+"/") {
		return true
	}
	return false
}

// preferExactPathMatch narrows a set of YAML entry nodes to only those whose
// path exactly matches registryPath, when more than one entry matched via
// prefix (registryPathMatches). If no exact match exists the original slice is
// returned unchanged so the caller can still fall back to prefix semantics.
// When registryPath is empty the slice is returned as-is.
func preferExactPathMatch(entries []*yaml.Node, registryPath string) []*yaml.Node {
	if registryPath == "" || len(entries) <= 1 {
		return entries
	}
	var exact []*yaml.Node
	for _, e := range entries {
		if strings.TrimSpace(yamlEntryPath(e)) == registryPath {
			exact = append(exact, e)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return entries
}
