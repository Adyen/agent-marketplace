package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

var ErrMarketplaceMigrationInput = errors.New("invalid Marketplace migration input")

type MarketplaceLegacySeed struct {
	URL        string
	InstanceID string
	Alias      string
	Resources  []MarketplaceResourcePin
}

type MarketplaceConfigMigration struct {
	MigrationNeeded  bool
	SimplifiedConfig []byte
	LegacySeeds      []MarketplaceLegacySeed
}

func StageMarketplaceConfigMigration(data []byte) (MarketplaceConfigMigration, error) {
	unchanged := MarketplaceConfigMigration{SimplifiedConfig: append([]byte(nil), data...)}
	if len(bytes.TrimSpace(data)) == 0 {
		return unchanged, nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return MarketplaceConfigMigration{}, marketplaceMigrationError("parse config", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return MarketplaceConfigMigration{}, marketplaceMigrationError("expected one YAML document", nil)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return MarketplaceConfigMigration{}, marketplaceMigrationError("expected YAML mapping at document root", nil)
	}
	marketplaces := findMappingValue(root, "marketplaces")
	if marketplaces == nil {
		return unchanged, nil
	}
	marketplaces = resolveAlias(marketplaces)
	if marketplaces == nil || marketplaces.Kind != yaml.SequenceNode {
		return MarketplaceConfigMigration{}, marketplaceMigrationError("marketplaces must be a sequence", nil)
	}

	type endpointPlan struct {
		resources *yaml.Node
		keep      []*yaml.Node
		seed      MarketplaceLegacySeed
	}
	plans := make([]endpointPlan, 0, len(marketplaces.Content))
	endpointEntries := make([]MarketplaceEntry, 0, len(marketplaces.Content))
	migrationNeeded := false
	for entryIndex, rawEntry := range marketplaces.Content {
		entry := resolveAlias(rawEntry)
		entryPath := fmt.Sprintf("marketplaces[%d]", entryIndex)
		if entry == nil || entry.Kind != yaml.MappingNode {
			return MarketplaceConfigMigration{}, marketplaceMigrationError(entryPath+" must be a mapping", nil)
		}
		if err := validateMarketplaceMapping(entry, entryPath, marketplaceEntryFields); err != nil {
			return MarketplaceConfigMigration{}, marketplaceMigrationError(err.Error(), nil)
		}
		var decoded MarketplaceEntry
		if err := entry.Decode(&decoded); err != nil {
			return MarketplaceConfigMigration{}, marketplaceMigrationError("decode "+entryPath, err)
		}
		entryWithoutResources := decoded
		entryWithoutResources.Resources = nil
		if errs := ValidateMarketplaceEntries([]MarketplaceEntry{entryWithoutResources}); len(errs) > 0 {
			return MarketplaceConfigMigration{}, marketplaceMigrationError(entryPath, errs[0])
		}
		endpointEntries = append(endpointEntries, entryWithoutResources)

		resources := findMappingValue(entry, "resources")
		plan := endpointPlan{resources: resources, seed: MarketplaceLegacySeed{URL: decoded.URL, InstanceID: decoded.InstanceID, Alias: decoded.Alias}}
		if resources == nil {
			plans = append(plans, plan)
			continue
		}
		resources = resolveAlias(resources)
		plan.resources = resources
		if resources == nil || resources.Kind != yaml.SequenceNode {
			return MarketplaceConfigMigration{}, marketplaceMigrationError(entryPath+".resources must be a sequence", nil)
		}
		plan.keep = make([]*yaml.Node, 0, len(resources.Content))
		for resourceIndex, rawResource := range resources.Content {
			resourcePath := fmt.Sprintf("%s.resources[%d]", entryPath, resourceIndex)
			resource := resolveAlias(rawResource)
			legacy, pin, err := classifyMarketplaceMigrationResource(resource, resourcePath)
			if err != nil {
				return MarketplaceConfigMigration{}, err
			}
			if !legacy {
				plan.keep = append(plan.keep, rawResource)
				continue
			}
			migrationNeeded = true
			plan.seed.Resources = append(plan.seed.Resources, pin)
			if pin.Direct || findMappingValue(resource, "selected_by") == nil {
				plan.keep = append(plan.keep, rawResource)
			}
		}
		plans = append(plans, plan)
	}
	if errs := ValidateMarketplaceEntries(endpointEntries); len(errs) > 0 {
		return MarketplaceConfigMigration{}, marketplaceMigrationError("validate Marketplace endpoints", errs[0])
	}

	if !migrationNeeded {
		return unchanged, nil
	}
	if err := rejectMarketplaceYAMLAliases(&doc); err != nil {
		return MarketplaceConfigMigration{}, marketplaceMigrationError("cannot stage migration", err)
	}

	result := MarketplaceConfigMigration{MigrationNeeded: true}
	for _, plan := range plans {
		if len(plan.seed.Resources) > 0 {
			result.LegacySeeds = append(result.LegacySeeds, plan.seed)
		}
		if plan.resources == nil {
			continue
		}
		plan.resources.Content = plan.keep
		for _, resource := range plan.keep {
			if isLegacyMarketplaceResource(resource) {
				reduceLegacyMarketplaceResource(resource)
			}
		}
	}
	encoded, err := encodeMarketplaceMigrationDocument(&doc, bytes.HasSuffix(data, []byte("\n")))
	if err != nil {
		return MarketplaceConfigMigration{}, marketplaceMigrationError("encode simplified config", err)
	}
	result.SimplifiedConfig = encoded
	return result, nil
}

func StageMarketplaceConfigMigrationFromPath(path string) (MarketplaceConfigMigration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MarketplaceConfigMigration{}, fmt.Errorf("read Marketplace config %q: %w", path, err)
	}
	result, err := StageMarketplaceConfigMigration(data)
	if err != nil {
		return MarketplaceConfigMigration{}, fmt.Errorf("stage Marketplace migration for %q: %w", path, err)
	}
	return result, nil
}

func classifyMarketplaceMigrationResource(node *yaml.Node, path string) (bool, MarketplaceResourcePin, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return false, MarketplaceResourcePin{}, marketplaceMigrationError(path+" must be a mapping", nil)
	}
	legacy := isLegacyMarketplaceResource(node)
	if !legacy {
		if err := validateMarketplacePinNode(node, path); err != nil {
			return false, MarketplaceResourcePin{}, marketplaceMigrationError(err.Error(), nil)
		}
		var requirement MarketplaceResourcePin
		if err := node.Decode(&requirement); err != nil {
			return false, MarketplaceResourcePin{}, marketplaceMigrationError("decode "+path, err)
		}
		if errs := validateMarketplacePin(0, 0, requirement); len(errs) > 0 {
			return false, MarketplaceResourcePin{}, marketplaceMigrationError(path, errs[0])
		}
		return false, requirement, nil
	}

	if err := validateMarketplaceLegacyPinNode(node, path); err != nil {
		return false, MarketplaceResourcePin{}, marketplaceMigrationError(err.Error(), nil)
	}
	for _, required := range []string{"source_id", "kind", "name", "revision", "digest"} {
		if findMappingValue(node, required) == nil {
			return false, MarketplaceResourcePin{}, marketplaceMigrationError(fmt.Sprintf("%s is a partial legacy requirement: %s is required", path, required), nil)
		}
	}
	var pin MarketplaceResourcePin
	if err := node.Decode(&pin); err != nil {
		return false, MarketplaceResourcePin{}, marketplaceMigrationError("decode "+path, err)
	}
	if errs := validateMarketplacePin(0, 0, pin); len(errs) > 0 {
		return false, MarketplaceResourcePin{}, marketplaceMigrationError(path, errs[0])
	}
	return true, pin, nil
}

func isLegacyMarketplaceResource(node *yaml.Node) bool {
	for _, field := range []string{"source_slug", "digest", "acquisition", "source_commit", "source_manifest_digest", "direct", "selected_by"} {
		if findMappingValue(node, field) != nil {
			return true
		}
	}
	return false
}

func reduceLegacyMarketplaceResource(node *yaml.Node) {
	for _, field := range []string{"source_slug", "revision", "digest", "acquisition", "source_commit", "source_manifest_digest", "direct", "selected_by"} {
		removeMappingKeyByName(node, field)
	}
}

func encodeMarketplaceMigrationDocument(doc *yaml.Node, finalNewline bool) ([]byte, error) {
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	encoded := out.Bytes()
	if !finalNewline {
		encoded = bytes.TrimSuffix(encoded, []byte("\n"))
	}
	return encoded, nil
}

func marketplaceMigrationError(message string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: %s", ErrMarketplaceMigrationInput, message)
	}
	return fmt.Errorf("%w: %s: %w", ErrMarketplaceMigrationInput, message, cause)
}
