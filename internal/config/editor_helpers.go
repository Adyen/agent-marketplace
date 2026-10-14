package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"gopkg.in/yaml.v3"
)

const defaultConfigMode os.FileMode = 0o644

var configPathLocks sync.Map

// withConfigPathLock serializes read-modify-write edits for one config path
// within this process. It does not provide cross-process locking.
func withConfigPathLock(configPath string, edit func() error) error {
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("resolve config path %q: %w", configPath, err)
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		dir = filepath.Dir(abs)
	}
	base := filepath.Base(abs)
	resolved, err := filepath.EvalSymlinks(filepath.Join(dir, base))
	if err != nil {
		resolved = filepath.Join(dir, base)
	}
	lock := &sync.Mutex{}
	actual, _ := configPathLocks.LoadOrStore(filepath.Clean(resolved), lock)
	lock = actual.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	return edit()
}

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

// readOrInitConfig reads and parses the YAML at configPath. If the file does
// not exist, returns a document pre-populated with apiVersion. When the path
// is the global config (~/.agents/config.yaml), the skeleton includes
// harnesses: [factory, windsurf] (the default for global configs); for project
// or local configs, harnesses is omitted until explicitly configured. The
// returned mode is the existing file's perms, or
// defaultConfigMode for a new file.
func readOrInitConfig(configPath string) (*yaml.Node, os.FileMode, error) {
	if dependencytxn.ClassifyDestination(configPath) != dependencytxn.AuthorityLegacy {
		// Managed layer destinations read through the descriptor boundary:
		// approved links resolve to their captured target, escapes and
		// unsafe objects refuse, and the mode comes from the same inspected
		// state as the bytes.
		data, generation, mode, err := dependencytxn.ReadSnapshot(configPath)
		if err != nil {
			return nil, 0, fmt.Errorf("read config %q: %w", configPath, err)
		}
		if !generation.Exists {
			return initialConfigDocument(configPath)
		}
		if _, err := boundedConfigData(data); err != nil {
			return nil, 0, err
		}
		if err := validateConfigDocumentData(configPath, data); err != nil {
			return nil, 0, err
		}
		doc, err := parseConfigDocument(configPath, data)
		if err != nil {
			return nil, 0, err
		}
		return doc, mode, nil
	}
	data, err := readConfigFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return initialConfigDocument(configPath)
		}
		return nil, 0, fmt.Errorf("read config %q: %w", configPath, err)
	}

	if err := validateConfigDocumentData(configPath, data); err != nil {
		return nil, 0, err
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

func initialConfigDocument(configPath string) (*yaml.Node, os.FileMode, error) {
	content := []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "apiVersion"},
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: APIVersionV4},
	}
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

// isGlobalConfigPath reports whether configPath refers to the global
// (user-personal) config file at ~/.agents/config.yaml. This determines
// whether a skeleton config should include the default harnesses (global
// configs include them; project/local configs wait for explicit selection).
const UserConfigShadowMarker = ".agent-marketplace-user-layer"

func isGlobalConfigPath(configPath string) bool {
	gp, err := GlobalConfigPath()
	if err == nil && filepath.Clean(configPath) == filepath.Clean(gp) {
		return true
	}
	_, err = os.Stat(filepath.Join(filepath.Dir(configPath), UserConfigShadowMarker))
	return err == nil
}

// isLocalConfigPath reports whether configPath refers to a local/personal
// config file (config.local.yaml). These roots cannot declare harnesses.
func isLocalConfigPath(configPath string) bool {
	return strings.HasSuffix(filepath.Clean(configPath), "config.local.yaml")
}

// EnsureGlobalConfig creates the user-level config at ~/.agents/config.yaml
// when it does not exist, the same way init does: a v4 root with the default
// harnesses and nothing else. Logging is left out so the file stays clean, and
// the logging runtime falls back to its code defaults unless the user opts in
// with an explicit logging block (see docs/logging.md for the override keys).
// An existing root is validated without rewriting it; dangling links stay untouched.
func EnsureGlobalConfig() error {
	configPath, err := GlobalConfigPath()
	if err != nil {
		return err
	}
	_, err = os.Lstat(configPath)
	switch {
	case err == nil:
		_, err := loadSettings(configPath, userLayer)
		return err
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	// InitializeUserPersonalAtPath creates the file exclusively, so a
	// concurrent run that creates it first is not an error.
	if _, err := InitializeUserPersonalAtPath(configPath, DefaultHarnesses()); err != nil && !errors.Is(err, ErrAlreadyInitialized) {
		return err
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

// encodeConfigDocument renders doc to the exact bytes writeConfigDocumentData
// writes, so callers can validate the candidate document before any byte
// lands on disk.
func encodeConfigDocument(configPath string, doc *yaml.Node) ([]byte, error) {
	if isEmptyConfigDocument(doc) {
		return nil, nil
	}
	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode config %q: %w", configPath, err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close yaml encoder for %q: %w", configPath, err)
	}
	return encoded.Bytes(), nil
}

func validateConfigDocumentData(configPath string, data []byte) error {
	if data == nil {
		data = []byte{}
	}
	kind := repoLayer
	if isLocalConfigPath(configPath) {
		kind = localLayer
	} else if isGlobalConfigPath(configPath) {
		kind = userLayer
	}
	v4, err := loadV4TreeWithOverlays(configPath, true, map[string]bool{}, nil, data, "", kind)
	if err != nil {
		return err
	}
	return v4.validateLayerFields(kind)
}

func writeConfigDocument(configPath string, mode os.FileMode, doc *yaml.Node) error {
	return writeConfigDocumentWithOptions(configPath, mode, doc)
}

func writeConfigDocumentWithOptions(configPath string, mode os.FileMode, doc *yaml.Node, txnOpts ...dependencytxn.Option) error {
	data, err := encodeConfigDocument(configPath, doc)
	if err != nil {
		return err
	}
	return writeConfigDocumentDataWithOptions(configPath, mode, data, txnOpts...)
}

// writeConfigDocumentData atomically replaces the config file at configPath
// with data, preserving the existing file's mode.
func writeConfigDocumentData(configPath string, mode os.FileMode, data []byte) error {
	return writeConfigDocumentDataWithOptions(configPath, mode, data)
}

func writeConfigDocumentDataWithOptions(configPath string, mode os.FileMode, data []byte, txnOpts ...dependencytxn.Option) error {
	if err := validateConfigDocumentData(configPath, data); err != nil {
		return err
	}
	if dependencytxn.ClassifyDestination(configPath) != dependencytxn.AuthorityLegacy {
		// Managed layer destinations write through the descriptor boundary.
		// An approved link is preserved as a link and its captured target
		// receives the edit; escapes, unsafe leaves, and parent or leaf
		// replacements fail before the destination changes.
		return dependencytxn.PublishDestination(configPath, dependencytxn.Generation{}, data, mode, txnOpts...)
	}
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
			// Expand a compact resource group into its mapping form.
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

func isEmptyConfigDocument(doc *yaml.Node) bool {
	if doc == nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return true
	}
	root := doc.Content[0]
	return root == nil || (root.Kind == yaml.MappingNode && len(root.Content) == 0)
}
