package config

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// layerKind identifies which Configuration Layer is being loaded so that
// strict v3 validation can reject forbidden fields per ADR 0002.
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

// topLevelAllowed lists the keys permitted at the root of a v3 config for
// each Configuration Layer. Fields not in the allowlist are rejected before
// decoding, so unknown keys are also caught.
var topLevelAllowed = map[layerKind]map[string]bool{
	organizationLayer: {
		"apiVersion":         true,
		"harnesses":          true,
		"allowed_registries": true,
		"registries":         true,
		"collections":        true,
		"skills":             true,
		"commands":           true,
		"subagents":          true,
		"hooks":              true,
		"mcp":                true,
		"tags":               true,
		"backup":             true,
		"logging":            true,
	},
	userLayer: {
		"apiVersion":         true,
		"harnesses":          true,
		"allowed_registries": true,
		"registries":         true,
		"collections":        true,
		"skills":             true,
		"commands":           true,
		"subagents":          true,
		"hooks":              true,
		"mcp":                true,
		"tags":               true,
		"backup":             true,
		"logging":            true,
	},
	repoLayer: {
		"apiVersion":         true,
		"allowed_registries": true,
		"registries":         true,
		"collections":        true,
		"skills":             true,
		"commands":           true,
		"subagents":          true,
		"hooks":              true,
		"mcp":                true,
		"tags":               true,
		"materialization":    true,
	},
	localLayer: {
		"apiVersion":      true,
		"registries":      true,
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
// required and default are validated separately because they are forbidden
// only in the repository personal layer.
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

// loadV3Config loads a strict agent-marketplace/v3 Config from raw YAML.
// It validates the YAML node against the per-layer allowlist, decodes with
// KnownFields, and then runs structural validation.
func loadV3Config(data []byte, kind layerKind, requireHarnesses bool) (*Config, []error) {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, []error{fmt.Errorf("parse %s config: %w", kind.name(), err)}
	}

	if errs := validateV3ConfigNode(&node, kind); len(errs) > 0 {
		return nil, errs
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		return nil, []error{fmt.Errorf("decode %s config: %w", kind.name(), err)}
	}
	// Canonicalize target names before validating them, so a harness written
	// with stray whitespace is trimmed rather than reported as unknown.
	normalizeConfigHarnessTargets(&cfg)

	if err := cfg.validate(requireHarnesses, false); err != nil {
		return nil, []error{fmt.Errorf("validate %s config: %w", kind.name(), err)}
	}

	return &cfg, nil
}

// loadV3LocalConfig loads a strict agent-marketplace/v3 LocalConfig from raw
// YAML. It validates the YAML node against the repository personal allowlist
// and then runs structural validation.
func loadV3LocalConfig(data []byte) (*LocalConfig, []error) {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, []error{fmt.Errorf("parse repository personal config: %w", err)}
	}

	if errs := validateV3ConfigNode(&node, localLayer); len(errs) > 0 {
		return nil, errs
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var cfg LocalConfig
	if err := decoder.Decode(&cfg); err != nil {
		return nil, []error{fmt.Errorf("decode repository personal config: %w", err)}
	}
	normalizeLocalConfigHarnessTargets(&cfg)

	if err := cfg.ValidateBasic(); err != nil {
		return nil, []error{fmt.Errorf("validate repository personal config: %w", err)}
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
			errs = append(errs, fmt.Errorf("line %d: forbidden or unknown top-level field %q in %s configuration", keyNode.Line, key, kind.name()))
			continue
		}

		if key == "registries" && i+1 < len(root.Content) {
			valNode := resolveAlias(root.Content[i+1])
			if valNode.Kind == yaml.SequenceNode {
				for _, rawItem := range valNode.Content {
					item := resolveAlias(rawItem)
					if item.Kind == yaml.MappingNode {
						errs = append(errs, validateV3RegistryEntryNode(item, kind)...)
					}
				}
			}
		}
	}

	return errs
}

// validateV3RegistryEntryNode validates one registry entry mapping. It checks
// that every key is allowed for the layer and that required/default are not
// used in repository personal configs.
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
			continue
		}

		if kind == localLayer && (key == "required" || key == "default") {
			errs = append(errs, fmt.Errorf("forbidden registry entry field %q in repository personal configuration", key))
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
