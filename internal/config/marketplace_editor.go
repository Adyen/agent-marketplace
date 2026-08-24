package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"unicode"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
	"gopkg.in/yaml.v3"
)

// EnsureMarketplacePinInConfig ensures a Marketplace entry identified by its
// canonical URL and instance_id, then adds an exact resource pin to it. It
// edits only the selected configPath using the YAML AST editor, and does not
// marshal a Config. Existing aliases and installed_name values are retained.
//
// A pin identity is (source_id, kind, name). An existing identity with a
// different revision, digest, or acquisition is a conflict and is rejected
// rather than silently changing the installed resource. SourceSlug is
// informational and does not participate in pin content identity.
func EnsureMarketplacePinInConfig(configPath, marketplaceURL, instanceID, alias string, pin MarketplaceResourcePin) error {
	_, err := EnsureMarketplacePinInConfigWithCommit(configPath, marketplaceURL, instanceID, alias, pin)
	return err
}

// EnsureMarketplacePinInConfigWithCommit applies a Marketplace pin and returns
// the exact bytes committed by this edit. The commit is captured while the
// same in-process path lock is held so rollback can use it as a compare-and-swap
// guard against a concurrent writer.
func EnsureMarketplacePinInConfigWithCommit(configPath, marketplaceURL, instanceID, alias string, pin MarketplaceResourcePin) (ConfigFileCommit, error) {
	_, commit, err := EnsureMarketplacePinInConfigWithSnapshotAndCommit(configPath, marketplaceURL, instanceID, alias, pin)
	return commit, err
}

// EnsureMarketplacePinInConfigWithSnapshotAndCommit captures the pre-edit
// config file, applies a Marketplace pin, and returns both the snapshot and
// exact bytes committed by the edit. The snapshot and mutation happen while
// holding the same per-path lock, so rollback cannot restore bytes from before
// a writer that committed immediately before this edit.
func EnsureMarketplacePinInConfigWithSnapshotAndCommit(configPath, marketplaceURL, instanceID, alias string, pin MarketplaceResourcePin) (*FileSnapshot, ConfigFileCommit, error) {
	var snapshot *FileSnapshot
	var commit ConfigFileCommit
	err := withConfigPathLock(configPath, func() error {
		var err error
		snapshot, err = SnapshotConfigFile(configPath)
		if err != nil {
			return err
		}
		if err := ensureMarketplacePinInConfig(configPath, marketplaceURL, instanceID, alias, pin); err != nil {
			return err
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("read committed config %q: %w", configPath, err)
		}
		commit = newConfigFileCommit(data)
		return nil
	})
	return snapshot, commit, err
}

// UpdateMarketplacePinInConfig replaces an existing Marketplace mirror pin only
// when its complete value still matches expected. The endpoint and resource
// identity are the lookup key; expected is the compare-and-swap value.
func UpdateMarketplacePinInConfig(configPath, marketplaceURL, instanceID string, expected, replacement MarketplaceResourcePin) error {
	_, _, err := UpdateMarketplacePinInConfigWithSnapshotAndCommit(configPath, marketplaceURL, instanceID, expected, replacement)
	return err
}

// UpdateMarketplacePinInConfigWithSnapshotAndCommit performs an AST-based
// compare-and-swap update and returns the byte snapshot and commit token needed
// for a guarded rollback. It never creates an entry or a pin.
func UpdateMarketplacePinInConfigWithSnapshotAndCommit(configPath, marketplaceURL, instanceID string, expected, replacement MarketplaceResourcePin) (*FileSnapshot, ConfigFileCommit, error) {
	var snapshot *FileSnapshot
	var commit ConfigFileCommit
	err := withConfigPathLock(configPath, func() error {
		var err error
		snapshot, err = SnapshotConfigFile(configPath)
		if err != nil {
			return err
		}
		if err := updateMarketplacePinInConfig(configPath, marketplaceURL, instanceID, expected, replacement); err != nil {
			return err
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("read committed config %q: %w", configPath, err)
		}
		commit = newConfigFileCommit(data)
		return nil
	})
	return snapshot, commit, err
}

func updateMarketplacePinInConfig(configPath, marketplaceURL, instanceID string, expected, replacement MarketplaceResourcePin) error {
	canonicalURL, err := NormalizeMarketplaceURL(marketplaceURL)
	if err != nil {
		return err
	}
	if err := validateMarketplaceSegment("marketplace instance_id", instanceID); err != nil {
		return err
	}
	if err := ValidateMarketplaceResourcePin(expected); err != nil {
		return fmt.Errorf("expected Marketplace pin: %w", err)
	}
	if err := ValidateMarketplaceResourcePin(replacement); err != nil {
		return fmt.Errorf("replacement Marketplace pin: %w", err)
	}
	if !sameMarketplacePinIdentity(expected, replacement) {
		return fmt.Errorf("marketplace pin identity cannot change during update")
	}

	doc, mode, err := readOrInitConfig(configPath)
	if err != nil {
		return err
	}
	if err := rejectMarketplaceYAMLAliases(doc); err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	marketplaces := findMappingValue(root, "marketplaces")
	if marketplaces == nil || marketplaces.Kind != yaml.SequenceNode {
		return fmt.Errorf("update config %q: marketplaces must be a sequence", configPath)
	}
	entries, err := decodeMarketplaceEntries(marketplaces)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	var entryNode *yaml.Node
	for _, candidate := range entries {
		if candidate.canonicalURL != canonicalURL || candidate.entry.InstanceID != instanceID {
			continue
		}
		if entryNode != nil {
			return fmt.Errorf("config %q contains duplicate Marketplace entry identity for URL %q and instance_id %q", configPath, canonicalURL, instanceID)
		}
		entryNode = candidate.node
	}
	if entryNode == nil {
		return fmt.Errorf("marketplace entry %q/%q not found in config %q", canonicalURL, instanceID, configPath)
	}
	resources := findMappingValue(entryNode, "resources")
	if resources == nil || resources.Kind != yaml.SequenceNode {
		return fmt.Errorf("marketplace entry %q/%q has no resources", canonicalURL, instanceID)
	}
	var pinNode *yaml.Node
	for _, node := range resources.Content {
		got, err := decodeMarketplacePin(node)
		if err != nil {
			return err
		}
		if !sameMarketplacePinIdentity(got, expected) {
			continue
		}
		if pinNode != nil {
			return fmt.Errorf("config %q contains duplicate Marketplace resource pin identity %s/%s/%s", configPath, expected.SourceID, expected.Kind, expected.Name)
		}
		pinNode = node
		if !sameMarketplacePinCASValue(got, expected) {
			return fmt.Errorf("marketplace pin compare-and-swap conflict for %s/%s/%s", expected.SourceID, expected.Kind, expected.Name)
		}
	}
	if pinNode == nil {
		return fmt.Errorf("marketplace pin %s/%s/%s not found in config %q", expected.SourceID, expected.Kind, expected.Name, configPath)
	}
	if sameMarketplacePinCASValue(expected, replacement) {
		return nil
	}
	replaceMarketplacePinNode(pinNode, replacement)
	if err := writeConfigDocument(configPath, mode, doc); err != nil {
		return err
	}
	slog.Debug("marketplace pin updated", "config_path", configPath, "url", canonicalURL, "instance_id", instanceID, "source_id", replacement.SourceID, "kind", replacement.Kind, "name", replacement.Name, "revision", replacement.Revision)
	return nil
}

func sameMarketplacePinCASValue(a, b MarketplaceResourcePin) bool {
	return a.SourceID == b.SourceID && a.SourceSlug == b.SourceSlug && a.Kind == b.Kind && a.Name == b.Name && a.Revision == b.Revision && a.Digest == b.Digest && a.Acquisition == b.Acquisition && a.SourceCommit == b.SourceCommit && a.SourceManifestDigest == b.SourceManifestDigest && a.InstalledName == b.InstalledName
}

func replaceMarketplacePinNode(node *yaml.Node, pin MarketplaceResourcePin) {
	setMappingValue(node, "source_id", scalarNode(pin.SourceID))
	if pin.SourceSlug == "" {
		removeMappingKeyByName(node, "source_slug")
	} else {
		setMappingValue(node, "source_slug", scalarNode(pin.SourceSlug))
	}
	setMappingValue(node, "kind", scalarNode(pin.Kind))
	setMappingValue(node, "name", scalarNode(pin.Name))
	setMappingValue(node, "revision", scalarNode(pin.Revision))
	setMappingValue(node, "digest", scalarNode(pin.Digest))
	setMappingValue(node, "acquisition", scalarNode(pin.Acquisition))
	if pin.SourceCommit == "" {
		removeMappingKeyByName(node, "source_commit")
	} else {
		setMappingValue(node, "source_commit", scalarNode(pin.SourceCommit))
	}
	if pin.SourceManifestDigest == "" {
		removeMappingKeyByName(node, "source_manifest_digest")
	} else {
		setMappingValue(node, "source_manifest_digest", scalarNode(pin.SourceManifestDigest))
	}
	if pin.InstalledName == "" {
		removeMappingKeyByName(node, "installed_name")
	} else {
		setMappingValue(node, "installed_name", scalarNode(pin.InstalledName))
	}
}

func ensureMarketplacePinInConfig(configPath, marketplaceURL, instanceID, alias string, pin MarketplaceResourcePin) error {
	canonicalURL, err := NormalizeMarketplaceURL(marketplaceURL)
	if err != nil {
		return err
	}
	if err := validateMarketplaceSegment("marketplace instance_id", instanceID); err != nil {
		return err
	}
	if err := ValidateMarketplaceResourcePin(pin); err != nil {
		return err
	}
	if alias != "" {
		if err := validateMarketplaceAlias("marketplace alias", alias); err != nil {
			return err
		}
	}

	slog.Debug("ensuring marketplace pin", "config_path", configPath, "url", canonicalURL, "instance_id", instanceID, "source_id", pin.SourceID, "kind", pin.Kind, "name", pin.Name)
	doc, mode, err := readOrInitConfig(configPath)
	if err != nil {
		return err
	}
	if err := rejectMarketplaceYAMLAliases(doc); err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	marketplaces, err := ensureTopLevelSequence(root, "marketplaces")
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	entries, err := decodeMarketplaceEntries(marketplaces)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	var match *yaml.Node
	for _, candidate := range entries {
		if candidate.canonicalURL != canonicalURL || candidate.entry.InstanceID != instanceID {
			continue
		}
		if match != nil {
			return fmt.Errorf("config %q contains duplicate Marketplace entry identity for URL %q and instance_id %q", configPath, canonicalURL, instanceID)
		}
		match = candidate.node
	}

	if match == nil {
		entry := marketplaceEntryNode(canonicalURL, instanceID, alias)
		resources := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		resources.Content = append(resources.Content, marketplacePinNode(pin))
		entry.Content = append(entry.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "resources"}, resources,
		)
		marketplaces.Content = append(marketplaces.Content, entry)
		if err := writeConfigDocument(configPath, mode, doc); err != nil {
			return err
		}
		slog.Debug("marketplace pin added", "config_path", configPath, "url", canonicalURL, "instance_id", instanceID)
		return nil
	}

	changed := false
	// An existing non-empty alias is authoritative. If the entry predates
	// aliases, adding the caller's alias is safe and avoids leaving a newly
	// ensured endpoint nameless; a later call can never overwrite it.
	if alias != "" {
		aliasNode := findMappingValue(match, "alias")
		if aliasNode == nil || strings.TrimSpace(aliasNode.Value) == "" {
			setMappingValue(match, "alias", scalarNode(alias))
			changed = true
		}
	}
	resources, err := ensureMarketplaceResources(match)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	pinChanged, err := addMarketplacePinNode(resources, pin)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	changed = changed || pinChanged
	if !changed {
		slog.Debug("marketplace pin already present", "config_path", configPath, "url", canonicalURL, "instance_id", instanceID)
		return nil
	}
	if err := writeConfigDocument(configPath, mode, doc); err != nil {
		return err
	}
	slog.Debug("marketplace pin added", "config_path", configPath, "url", canonicalURL, "instance_id", instanceID)
	return nil
}

// AddMarketplacePinToConfig is a descriptive alias for
// EnsureMarketplacePinInConfig.
func AddMarketplacePinToConfig(configPath, marketplaceURL, instanceID, alias string, pin MarketplaceResourcePin) error {
	return EnsureMarketplacePinInConfig(configPath, marketplaceURL, instanceID, alias, pin)
}

// EnsureMarketplaceEntryAndPinInConfig accepts the entry-shaped API used by
// callers that already have a resolved MarketplaceEntry. Resources on entry
// are ignored; pin is the only resource mutation performed.
func EnsureMarketplaceEntryAndPinInConfig(configPath string, entry MarketplaceEntry, pin MarketplaceResourcePin) error {
	return EnsureMarketplacePinInConfig(configPath, entry.URL, entry.InstanceID, entry.Alias, pin)
}

// AddMarketplaceResourcePin is the entry-shaped spelling used by install
// callers. It targets exactly the config path supplied by the caller.
func AddMarketplaceResourcePin(configPath string, entry MarketplaceEntry, pin MarketplaceResourcePin) error {
	return EnsureMarketplaceEntryAndPinInConfig(configPath, entry, pin)
}

// EnsureMarketplaceEntryInConfig creates an entry without adding a resource.
// It is useful for callers that need to stage endpoint identity separately.
func EnsureMarketplaceEntryInConfig(configPath string, entry MarketplaceEntry) error {
	return withConfigPathLock(configPath, func() error {
		return ensureMarketplaceEntryInConfig(configPath, entry)
	})
}

func ensureMarketplaceEntryInConfig(configPath string, entry MarketplaceEntry) error {
	canonicalURL, err := NormalizeMarketplaceURL(entry.URL)
	if err != nil {
		return err
	}
	if err := validateMarketplaceSegment("marketplace instance_id", entry.InstanceID); err != nil {
		return err
	}
	if entry.Alias != "" {
		if err := validateMarketplaceAlias("marketplace alias", entry.Alias); err != nil {
			return err
		}
	}
	if errs := ValidateMarketplaceEntries([]MarketplaceEntry{entry}); len(errs) > 0 {
		return errs[0]
	}
	doc, mode, err := readOrInitConfig(configPath)
	if err != nil {
		return err
	}
	if err := rejectMarketplaceYAMLAliases(doc); err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	marketplaces, err := ensureTopLevelSequence(root, "marketplaces")
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	entries, err := decodeMarketplaceEntries(marketplaces)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	var match *yaml.Node
	for _, candidate := range entries {
		if candidate.canonicalURL != canonicalURL || candidate.entry.InstanceID != entry.InstanceID {
			continue
		}
		if match != nil {
			return fmt.Errorf("config %q contains duplicate Marketplace entry identity for URL %q and instance_id %q", configPath, canonicalURL, entry.InstanceID)
		}
		match = candidate.node
	}
	if match != nil {
		return nil
	}
	marketplaces.Content = append(marketplaces.Content, marketplaceEntryNode(canonicalURL, entry.InstanceID, entry.Alias))
	return writeConfigDocument(configPath, mode, doc)
}

// ValidateMarketplaceInstalledName validates the optional installed_name pin
// field without depending on the parser package.
func ValidateMarketplaceInstalledName(name string) error {
	if name == "" {
		return nil
	}
	if err := nameutil.ValidateSafeFilename(name); err != nil {
		return fmt.Errorf("installed_name: %w", err)
	}
	return nil
}

// ValidateMarketplaceResourcePin validates the fields needed for an exact
// mirror pin before any config file is changed.
func ValidateMarketplaceResourcePin(pin MarketplaceResourcePin) error {
	if err := validateMarketplaceSegment("source_id", pin.SourceID); err != nil {
		return err
	}
	if !api.IsMarketplaceKind(pin.Kind) {
		return fmt.Errorf("kind %q is not a valid Marketplace kind", pin.Kind)
	}
	if err := validateMarketplaceSegment("name", pin.Name); err != nil {
		return err
	}
	if err := validateMarketplaceSegment("revision", pin.Revision); err != nil {
		return err
	}
	if !api.IsCanonicalSHA256Digest(pin.Digest) {
		return fmt.Errorf("digest must be sha256:<64-hex>")
	}
	if pin.SourceSlug != "" {
		if err := validateMarketplaceSegment("source_slug", pin.SourceSlug); err != nil {
			return err
		}
	}
	if pin.Acquisition != "" && pin.Acquisition != MarketplaceAcquisitionMirror && pin.Acquisition != MarketplaceAcquisitionIndexOnly {
		return fmt.Errorf("acquisition must be %q or %q", MarketplaceAcquisitionMirror, MarketplaceAcquisitionIndexOnly)
	}
	if err := validateMarketplaceOccurrenceFields(pin.Acquisition, pin.SourceCommit, pin.SourceManifestDigest); err != nil {
		return err
	}
	return ValidateMarketplaceInstalledName(pin.InstalledName)
}

type marketplaceEntryNodeInfo struct {
	node         *yaml.Node
	entry        MarketplaceEntry
	canonicalURL string
}

var marketplaceEntryFields = map[string]struct{}{
	"url": {}, "instance_id": {}, "alias": {}, "resources": {},
}

var marketplacePinFields = map[string]struct{}{
	"source_id": {}, "source_slug": {}, "kind": {}, "name": {},
	"revision": {}, "digest": {}, "acquisition": {}, "source_commit": {}, "source_manifest_digest": {}, "installed_name": {},
}

// validateMarketplaceMapping validates only the Marketplace-specific AST. The
// editor intentionally does not validate unrelated legacy top-level fields, but
// it must reject unknown keys and malformed mappings in entries and pins before
// it can perform a read-modify-write.
func validateMarketplaceMapping(node *yaml.Node, path string, allowed map[string]struct{}) error {
	if node == nil || node.Kind != yaml.MappingNode {
		return fmt.Errorf("%s must be a mapping", path)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("%s is a malformed mapping", path)
	}
	seen := make(map[string]struct{}, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		if key == nil || key.Kind != yaml.ScalarNode || key.Tag == "!!null" {
			return fmt.Errorf("%s has a non-scalar key", path)
		}
		name := key.Value
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%s contains unknown field %q", path, name)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("%s contains duplicate field %q", path, name)
		}
		seen[name] = struct{}{}
		value := node.Content[i+1]
		if value == nil {
			return fmt.Errorf("%s.%s is missing a value", path, name)
		}
	}
	return nil
}

func validateMarketplaceScalarFields(node *yaml.Node, path string, fields map[string]struct{}) error {
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		if _, ok := fields[key.Value]; !ok {
			continue
		}
		value := node.Content[i+1]
		if value.Kind != yaml.ScalarNode || value.Tag == "!!null" {
			return fmt.Errorf("%s.%s must be a scalar value", path, key.Value)
		}
	}
	return nil
}

func validateMarketplaceEntryNode(node *yaml.Node, index int) error {
	path := fmt.Sprintf("marketplaces[%d]", index)
	if err := validateMarketplaceMapping(node, path, marketplaceEntryFields); err != nil {
		return err
	}
	if err := validateMarketplaceScalarFields(node, path, map[string]struct{}{
		"url": {}, "instance_id": {}, "alias": {},
	}); err != nil {
		return err
	}
	resources := findMappingValue(node, "resources")
	if resources == nil {
		return nil
	}
	if resources.Kind != yaml.SequenceNode {
		return fmt.Errorf("%s.resources must be a sequence", path)
	}
	for i, resource := range resources.Content {
		resourcePath := fmt.Sprintf("%s.resources[%d]", path, i)
		if err := validateMarketplaceMapping(resource, resourcePath, marketplacePinFields); err != nil {
			return err
		}
		if err := validateMarketplaceScalarFields(resource, resourcePath, marketplacePinFields); err != nil {
			return err
		}
	}
	return nil
}

func decodeMarketplaceEntries(sequence *yaml.Node) ([]marketplaceEntryNodeInfo, error) {
	if sequence == nil || sequence.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("marketplace entries must be a sequence")
	}
	entries := make([]marketplaceEntryNodeInfo, 0, len(sequence.Content))
	values := make([]MarketplaceEntry, 0, len(sequence.Content))
	for i, node := range sequence.Content {
		if node == nil || node.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("marketplaces[%d] must be a mapping", i)
		}
		if err := validateMarketplaceEntryNode(node, i); err != nil {
			return nil, fmt.Errorf("invalid existing Marketplace entry: %w", err)
		}
		var entry MarketplaceEntry
		if err := node.Decode(&entry); err != nil {
			return nil, fmt.Errorf("invalid existing Marketplace entry at marketplaces[%d]: %w", i, err)
		}
		if resources := findMappingValue(node, "resources"); resources != nil {
			for j, resource := range resources.Content {
				if _, err := decodeMarketplacePin(resource); err != nil {
					return nil, fmt.Errorf("invalid existing Marketplace resource pin at marketplaces[%d].resources[%d]: %w", i, j, err)
				}
			}
		}
		if errs := ValidateMarketplaceEntries([]MarketplaceEntry{entry}); len(errs) > 0 {
			return nil, fmt.Errorf("invalid existing Marketplace entry at marketplaces[%d]: %v", i, errs[0])
		}
		canonicalURL, err := NormalizeMarketplaceURL(entry.URL)
		if err != nil {
			return nil, fmt.Errorf("invalid existing Marketplace entry at marketplaces[%d]: %w", i, err)
		}
		entries = append(entries, marketplaceEntryNodeInfo{node: node, entry: entry, canonicalURL: canonicalURL})
		values = append(values, entry)
	}
	if errs := ValidateMarketplaceEntries(values); len(errs) > 0 {
		return nil, fmt.Errorf("invalid Marketplace entries: %v", errs[0])
	}
	return entries, nil
}

func ensureMarketplaceResources(entry *yaml.Node) (*yaml.Node, error) {
	resources := findMappingValue(entry, "resources")
	if resources == nil {
		resources = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		setMappingValue(entry, "resources", resources)
		return resources, nil
	}
	if resources.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("marketplace entry resources must be a sequence")
	}
	return resources, nil
}

func addMarketplacePinNode(resources *yaml.Node, wanted MarketplaceResourcePin) (bool, error) {
	var found *yaml.Node
	for _, node := range resources.Content {
		if node.Kind != yaml.MappingNode {
			continue
		}
		got, err := decodeMarketplacePin(node)
		if err != nil {
			return false, err
		}
		if !sameMarketplacePinIdentity(got, wanted) {
			continue
		}
		if found != nil {
			return false, fmt.Errorf("conflicting duplicate Marketplace resource pin identity %s/%s/%s", wanted.SourceID, wanted.Kind, wanted.Name)
		}
		found = node
		if !sameMarketplacePinContent(got, wanted) {
			return false, fmt.Errorf("conflicting Marketplace resource pin for %s/%s/%s", wanted.SourceID, wanted.Kind, wanted.Name)
		}
	}
	if found == nil {
		resources.Content = append(resources.Content, marketplacePinNode(wanted))
		return true, nil
	}
	// installed_name belongs to the user's chosen local identity. Never
	// replace it when an existing exact pin already carries one; populate it
	// only when the existing pin has no value and the new request supplies one.
	if wanted.InstalledName != "" {
		existing := findMappingValue(found, "installed_name")
		if existing == nil || strings.TrimSpace(existing.Value) == "" {
			setMappingValue(found, "installed_name", scalarNode(wanted.InstalledName))
			return true, nil
		}
		if existing.Value != wanted.InstalledName {
			return false, fmt.Errorf("conflicting installed_name for Marketplace resource pin %s/%s/%s: existing %q, requested %q", wanted.SourceID, wanted.Kind, wanted.Name, existing.Value, wanted.InstalledName)
		}
	}
	return false, nil
}

func decodeMarketplacePin(node *yaml.Node) (MarketplaceResourcePin, error) {
	var pin MarketplaceResourcePin
	if err := node.Decode(&pin); err != nil {
		return MarketplaceResourcePin{}, fmt.Errorf("decode Marketplace resource pin: %w", err)
	}
	if err := ValidateMarketplaceResourcePin(pin); err != nil {
		return MarketplaceResourcePin{}, fmt.Errorf("invalid existing Marketplace resource pin: %w", err)
	}
	return pin, nil
}

func sameMarketplacePinIdentity(a, b MarketplaceResourcePin) bool {
	return a.SourceID == b.SourceID && a.Kind == b.Kind && a.Name == b.Name
}

func sameMarketplacePinContent(a, b MarketplaceResourcePin) bool {
	// An omitted acquisition is the schema's mirror default, so it is
	// semantically identical to an explicit mirror pin.
	acquisition := func(pin MarketplaceResourcePin) string {
		if pin.Acquisition == "" {
			return MarketplaceAcquisitionMirror
		}
		return pin.Acquisition
	}
	return a.SourceID == b.SourceID && a.Kind == b.Kind && a.Name == b.Name && a.Revision == b.Revision && a.Digest == b.Digest && acquisition(a) == acquisition(b) && a.SourceCommit == b.SourceCommit && a.SourceManifestDigest == b.SourceManifestDigest
}

func marketplaceEntryNode(endpointURL, instanceID, alias string) *yaml.Node {
	entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	entry.Content = append(entry.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "url"}, scalarNode(endpointURL),
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "instance_id"}, scalarNode(instanceID),
	)
	if alias != "" {
		entry.Content = append(entry.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "alias"}, scalarNode(alias),
		)
	}
	return entry
}

func marketplacePinNode(pin MarketplaceResourcePin) *yaml.Node {
	entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	add := func(key, value string, omit bool) {
		if omit && value == "" {
			return
		}
		entry.Content = append(entry.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, scalarNode(value),
		)
	}
	add("source_id", pin.SourceID, false)
	add("source_slug", pin.SourceSlug, true)
	add("kind", pin.Kind, false)
	add("name", pin.Name, false)
	add("revision", pin.Revision, false)
	add("digest", pin.Digest, false)
	add("acquisition", pin.Acquisition, true)
	add("source_commit", pin.SourceCommit, true)
	add("source_manifest_digest", pin.SourceManifestDigest, true)
	add("installed_name", pin.InstalledName, true)
	return entry
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func validateMarketplaceAlias(label, value string) error {
	if err := validateMarketplaceSegment(label, value); err != nil {
		return err
	}
	if value == "all" {
		return fmt.Errorf("%s %q is reserved for federated search", label, value)
	}
	return nil
}

func validateMarketplaceSegment(label, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", label)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must not have leading or trailing whitespace", label)
	}
	if value == "." || value == ".." {
		return fmt.Errorf("%s must not be a dot path component", label)
	}
	if strings.ContainsAny(value, "/\\") {
		return fmt.Errorf("%s contains an unsafe path separator", label)
	}
	if hasUnsafeMarketplaceText(value) {
		return fmt.Errorf("%s contains a control character", label)
	}
	return nil
}

func hasUnsafeMarketplaceText(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// NormalizeMarketplaceURL canonicalizes the endpoint portion of a Marketplace
// URL for entry identity matching. Browser routes are stripped by callers
// before reaching this helper; only HTTP(S) endpoint URLs are accepted.
func NormalizeMarketplaceURL(raw string) (string, error) {
	return urlutil.NormalizeEndpointURL(raw)
}
