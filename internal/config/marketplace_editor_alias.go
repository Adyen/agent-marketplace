package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// rejectMarketplaceYAMLAliases prevents the AST editor from mutating a node
// that may be shared by YAML anchors. Marketplace entries are intentionally
// rejected rather than expanded because expansion would silently change the
// meaning of other aliases in the same document.
func rejectMarketplaceYAMLAliases(doc *yaml.Node) error {
	if doc == nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	marketplaces := findMappingValue(root, "marketplaces")
	if marketplaces == nil {
		return nil
	}
	return rejectMarketplaceYAMLAliasesNode(marketplaces, "marketplaces")
}

func rejectMarketplaceYAMLAliasesNode(node *yaml.Node, path string) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return fmt.Errorf("%s uses a YAML alias or anchor; Marketplace config editing refuses to mutate shared YAML anchors", path)
	}
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			if err := rejectMarketplaceYAMLAliasesNode(node.Content[i], path+" key"); err != nil {
				return err
			}
			if err := rejectMarketplaceYAMLAliasesNode(node.Content[i+1], path); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i, child := range node.Content {
			if err := rejectMarketplaceYAMLAliasesNode(child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}
