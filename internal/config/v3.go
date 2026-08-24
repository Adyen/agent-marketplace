// Package config implements the agent-marketplace configuration model and
// loaders. The strict YAML loaders in this package read the v2 canonical
// schema and accept "agent-marketplace/v3" as a backwards-compatible alias
// for the same schema; generators and documentation should emit v2.
package config

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// layerKind identifies which Configuration Layer is being loaded so that
// strict validation can reject forbidden fields per ADR 0002.
type layerKind int

const (
	organizationLayer layerKind = iota
	userLayer
	repoLayer
	localLayer
)

func (k layerKind) name() string {
	switch k {
	case organizationLayer:
		return "organization"
	case userLayer:
		return "user personal"
	case repoLayer:
		return "repository committed"
	case localLayer:
		return "repository personal"
	default:
		return "unknown"
	}
}

// topLevelAllowed lists the keys permitted at the root of a config for
// each Configuration Layer. Fields not in the allowlist are rejected before
// decoding, so unknown keys are also caught.
var topLevelAllowed = map[layerKind]map[string]bool{
	organizationLayer: {
		"include":              true,
		"apiVersion":           true,
		"harnesses":            true,
		"allowed_registries":   true,
		"allowed_marketplaces": true,
		"registries":           true,
		"marketplaces":         true,
		"collections":          true,
		"skills":               true,
		"commands":             true,
		"subagents":            true,
		"hooks":                true,
		"mcp":                  true,
		"tags":                 true,
		"backup":               true,
		"logging":              true,
		"telemetry":            true,
	},
	userLayer: {
		"include":              true,
		"apiVersion":           true,
		"harnesses":            true,
		"allowed_registries":   true,
		"allowed_marketplaces": true,
		"registries":           true,
		"marketplaces":         true,
		"collections":          true,
		"skills":               true,
		"commands":             true,
		"subagents":            true,
		"hooks":                true,
		"mcp":                  true,
		"tags":                 true,
		"backup":               true,
		"logging":              true,
		"telemetry":            true,
		"materialization":      true,
	},
	repoLayer: {
		"include":              true,
		"apiVersion":           true,
		"allowed_registries":   true,
		"allowed_marketplaces": true,
		"registries":           true,
		"marketplaces":         true,
		"collections":          true,
		"skills":               true,
		"commands":             true,
		"subagents":            true,
		"hooks":                true,
		"mcp":                  true,
		"tags":                 true,
		"materialization":      true,
	},
	localLayer: {
		"include":         true,
		"apiVersion":      true,
		"registries":      true,
		"marketplaces":    true,
		"collections":     true,
		"skills":          true,
		"commands":        true,
		"subagents":       true,
		"hooks":           true,
		"mcp":             true,
		"materialization": true,
	},
}

// registryEntryAllowed is the set of keys permitted on every registry entry.
var registryEntryAllowed = map[string]bool{
	"url":            true,
	"ref":            true,
	"path":           true,
	"source_harness": true,
	"harnesses":      true,
	"tags":           true,
	"skills":         true,
	"commands":       true,
	"subagents":      true,
	"hooks":          true,
	"mcp":            true,
	"collections":    true,
	"exclude_all":    true,
	"required":       true,
	"default":        true,
}

// resolveAlias returns the node an AliasNode points to, or n itself if it is
// not an alias. This lets strict validation traverse YAML anchors/aliases
// instead of treating them as unknown nodes.
func resolveAlias(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.AliasNode {
		return n.Alias
	}
	return n
}

// loadV3Config loads a strict Config from raw YAML. It implements the v2
// canonical schema and accepts "agent-marketplace/v3" as a compatibility
// alias for the same schema. It validates the YAML node against the
// per-layer allowlist, decodes with KnownFields, and then runs structural
// validation.
func loadV3Config(data []byte, kind layerKind, requireHarnesses bool) (*Config, []error) {
	return loadV3ConfigWithAPIVersion(data, kind, requireHarnesses, kind != localLayer)
}

func loadV3ConfigWithAPIVersion(data []byte, kind layerKind, requireHarnesses, requireAPIVersion bool) (*Config, []error) {
	if len(bytes.TrimSpace(data)) == 0 {
		var cfg Config
		if err := cfg.validate(requireHarnesses, requireAPIVersion, kind.name()); err != nil {
			return nil, []error{fmt.Errorf("validate empty %s config: %w", kind.name(), err)}
		}
		return &cfg, nil
	}

	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, []error{fmt.Errorf("parse config: %w", err)}
	}

	nodeErrs := validateV3ConfigNode(&node, kind)

	// When the node has structural or unknown-key errors, strict KnownFields
	// decoding would duplicate those errors. Decode leniently so we can still
	// run value-level validation (e.g. unknown harness targets) on the rest of
	// the document. Use strict decoding only when the node is clean.
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if len(nodeErrs) == 0 {
		decoder.KnownFields(true)
	}
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		if len(nodeErrs) > 0 {
			return nil, append(nodeErrs, fmt.Errorf("parse config: %w", err))
		}
		return nil, []error{fmt.Errorf("parse config: %w", err)}
	}
	// Canonicalize target names before validating them, so a harness written
	// with stray whitespace is trimmed rather than reported as unknown.
	normalizeConfigHarnessTargets(&cfg)

	// If the document has structural node errors, do not also require a
	// harnesses list; reporting missing harnesses on top of structural problems
	// is noisy. Value-level validation (unknown harness targets, etc.) still
	// runs so callers see the specific invalid values.
	validateRequireHarnesses := requireHarnesses
	if len(nodeErrs) > 0 {
		validateRequireHarnesses = false
	}

	if err := cfg.validate(validateRequireHarnesses, requireAPIVersion, kind.name()); err != nil {
		nodeErrs = append(nodeErrs, err)
	}

	if len(nodeErrs) > 0 {
		return &cfg, nodeErrs
	}
	return &cfg, nil
}

// loadV3LocalConfig loads a strict LocalConfig from raw YAML. It
// implements the v2 canonical schema and accepts "agent-marketplace/v3" as
// a compatibility alias for the same schema. It validates the YAML node
// against the repository personal allowlist and then runs structural
// validation.
func loadV3LocalConfig(data []byte) (*LocalConfig, []error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return &LocalConfig{}, nil
	}

	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, []error{fmt.Errorf("parse config: %w", err)}
	}

	nodeErrs := validateV3ConfigNode(&node, localLayer)

	// See loadV3Config for the rationale: avoid duplicating node-level errors
	// with strict decoding, but still validate values when the document has
	// layer-forbidden keys.
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if len(nodeErrs) == 0 {
		decoder.KnownFields(true)
	}
	var cfg LocalConfig
	if err := decoder.Decode(&cfg); err != nil {
		if len(nodeErrs) > 0 {
			return nil, append(nodeErrs, fmt.Errorf("parse config: %w", err))
		}
		return nil, []error{fmt.Errorf("parse config: %w", err)}
	}
	normalizeLocalConfigHarnessTargets(&cfg)

	if err := cfg.ValidateBasic(); err != nil {
		nodeErrs = append(nodeErrs, err)
	}

	if len(nodeErrs) > 0 {
		return &cfg, nodeErrs
	}
	return &cfg, nil
}

// validateV3ConfigNode validates the root mapping keys and every registry
// entry key against the layer allowlists. It returns a slice of errors so
// callers can surface every violation in one pass.
func validateV3ConfigNode(node *yaml.Node, kind layerKind) []error {
	if node.Kind != yaml.DocumentNode || len(node.Content) == 0 {
		return []error{fmt.Errorf("expected YAML document")}
	}

	root := node.Content[0]
	isEmpty := root.Kind == yaml.ScalarNode && root.Tag == "!!null"
	if root.Kind != yaml.MappingNode && !isEmpty {
		return []error{fmt.Errorf("expected YAML mapping at document root")}
	}

	allowed := topLevelAllowed[kind]
	var errs []error
	for i := 0; i < len(root.Content); i += 2 {
		keyNode := resolveAlias(root.Content[i])
		if keyNode.Kind != yaml.ScalarNode {
			errs = append(errs, fmt.Errorf("expected scalar key at root, got %s", keyNode.Tag))
			continue
		}

		key := keyNode.Value
		if !allowed[key] {
			if key == "harnesses" && (kind == repoLayer || kind == localLayer) {
				errs = append(errs, fmt.Errorf("line %d: %s configuration cannot declare top-level harnesses; the target harness set is machine-wide and must live in the user-personal config", keyNode.Line, kind.name()))
			} else {
				errs = append(errs, fmt.Errorf("line %d: forbidden or unknown top-level field %q in %s configuration", keyNode.Line, key, kind.name()))
			}
			continue
		}

		if i+1 >= len(root.Content) {
			continue
		}
		valNode := resolveAlias(root.Content[i+1])
		switch key {
		case "registries":
			if valNode.Kind == yaml.SequenceNode {
				for _, rawItem := range valNode.Content {
					item := resolveAlias(rawItem)
					if item.Kind == yaml.MappingNode {
						errs = append(errs, validateV3RegistryEntryNode(item, kind)...)
					}
				}
			}
		case "marketplaces":
			errs = append(errs, validateV3MarketplaceNodes(valNode)...)
		}
	}

	return errs
}

func validateV3MarketplaceNodes(node *yaml.Node) []error {
	if node.Kind != yaml.SequenceNode {
		return nil
	}
	var errs []error
	for i, rawEntry := range node.Content {
		entry := resolveAlias(rawEntry)
		path := fmt.Sprintf("marketplaces[%d]", i)
		if entry == nil || entry.Kind != yaml.MappingNode {
			errs = append(errs, fmt.Errorf("%s must be a mapping", path))
			continue
		}
		if err := validateMarketplaceMapping(entry, path, marketplaceEntryFields); err != nil {
			errs = append(errs, err)
			continue
		}
		resources := findMappingValue(entry, "resources")
		if resources == nil {
			continue
		}
		resources = resolveAlias(resources)
		if resources.Kind != yaml.SequenceNode {
			errs = append(errs, fmt.Errorf("%s.resources must be a sequence", path))
			continue
		}
		for j, rawPin := range resources.Content {
			pinPath := fmt.Sprintf("%s.resources[%d]", path, j)
			if err := validateMarketplacePinNode(rawPin, pinPath); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

func validateMarketplacePinNode(rawPin *yaml.Node, path string) error {
	pin := resolveAlias(rawPin)
	if pin == nil || pin.Kind != yaml.MappingNode {
		return fmt.Errorf("%s must be a mapping", path)
	}
	if err := validateMarketplaceMapping(pin, path, marketplacePinFields); err != nil {
		return err
	}
	if err := validateMarketplaceScalarFields(pin, path, map[string]struct{}{
		"source_id": {}, "source_slug": {}, "kind": {}, "name": {}, "revision": {}, "digest": {}, "acquisition": {}, "source_commit": {}, "source_manifest_digest": {}, "installed_name": {},
	}); err != nil {
		return err
	}
	if direct := findMappingValue(pin, "direct"); direct != nil && (direct.Kind != yaml.ScalarNode || direct.Tag != "!!bool") {
		return fmt.Errorf("%s.direct must be a boolean", path)
	}
	if selectedBy := findMappingValue(pin, "selected_by"); selectedBy != nil {
		if err := validateMarketplaceSelectedByNode(selectedBy, path+".selected_by"); err != nil {
			return err
		}
	}
	return nil
}

func validateMarketplaceSelectedByNode(rawNode *yaml.Node, path string) error {
	node := resolveAlias(rawNode)
	if node == nil || node.Kind != yaml.SequenceNode || node.Tag == "!!null" {
		return fmt.Errorf("%s must be a non-null sequence", path)
	}
	selectionFields := map[string]struct{}{"source_id": {}, "name": {}, "revision": {}}
	for i, rawSelection := range node.Content {
		selectionPath := fmt.Sprintf("%s[%d]", path, i)
		selection := resolveAlias(rawSelection)
		if selection == nil || selection.Kind != yaml.MappingNode {
			return fmt.Errorf("%s must be an exact mapping with source_id, name, and revision", selectionPath)
		}
		if err := validateMarketplaceMapping(selection, selectionPath, selectionFields); err != nil {
			return err
		}
		for _, field := range []string{"source_id", "name", "revision"} {
			if findMappingValue(selection, field) == nil {
				return fmt.Errorf("%s.%s is required", selectionPath, field)
			}
		}
		if err := validateMarketplaceStringFields(selection, selectionPath, selectionFields); err != nil {
			return err
		}
	}
	return nil
}

// validateV3RegistryEntryNode validates one registry entry mapping. It checks
// that every key is allowed for the layer.
func validateV3RegistryEntryNode(node *yaml.Node, kind layerKind) []error {
	var errs []error
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := resolveAlias(node.Content[i])
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}

		key := keyNode.Value
		if !registryEntryAllowed[key] {
			errs = append(errs, fmt.Errorf("forbidden or unknown registry entry field %q in %s configuration", key, kind.name()))
		}
	}

	return errs
}

// PeekAPIVersion reads the apiVersion field from raw YAML without decoding
// the full document. It returns the empty string when the field is absent.
func PeekAPIVersion(data []byte) string {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return ""
	}
	if node.Kind != yaml.DocumentNode || len(node.Content) == 0 {
		return ""
	}
	root := node.Content[0]
	if root.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		if keyNode.Kind == yaml.ScalarNode && keyNode.Value == "apiVersion" && i+1 < len(root.Content) {
			valNode := root.Content[i+1]
			if valNode.Kind == yaml.ScalarNode {
				return valNode.Value
			}
		}
	}
	return ""
}
