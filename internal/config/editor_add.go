package config

import (
	"fmt"
	"log/slog"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

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
	locator := registrySlug + "/" + v4ResourceKindName(kind) + "/" + name
	if err := validateV4ExclusionForSource(locator, V4SourceMarketplace); err != nil {
		return err
	}
	return withConfigPathLock(configPath, func() error {
		doc, mode, err := readOrInitConfig(configPath)
		if err != nil {
			return err
		}
		root, err := ensureDocumentMapping(doc)
		if err != nil {
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
			sequence.Content = append(sequence.Content, scalarNode(locator))
		}
		return writeConfigDocument(configPath, mode, doc)
	})
}

func SetHarnessesInConfig(configPath string, harnesses []string) error {
	if isLocalConfigPath(configPath) {
		return fmt.Errorf("harnesses cannot be set in repository personal config %q", configPath)
	}
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
	if err := writeConfigDocument(configPath, mode, doc); err != nil {
		return err
	}
	slog.Debug("harnesses updated in config", "config_path", configPath, "harnesses", len(harnesses))
	return nil
}
