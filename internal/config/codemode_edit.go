package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func codeModeMigrateScope(configPath string) string {
	switch {
	case isLocalConfigPath(configPath):
		return "local"
	case isGlobalConfigPath(configPath):
		return "user"
	default:
		return "repo"
	}
}

func requireV4CodeModeRoot(configPath string, root *yaml.Node) error {
	if version := findMappingValue(root, "apiVersion"); version == nil || version.Value != APIVersionV4 {
		return v4err("v4.writer-opt-in", configPath, 0, "codeMode requires a v4 configuration; run 'agent-marketplace migrate --scope %s' to upgrade this file first", codeModeMigrateScope(configPath))
	}
	return nil
}

func SetCodeModeEnabled(configPath string, enabled bool) error {
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
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	if os.IsNotExist(statErr) {
		setMappingValue(root, "apiVersion", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: APIVersionV4})
	} else if err := requireV4CodeModeRoot(configPath, root); err != nil {
		return err
	}
	codeModeMapping := ensureTopLevelMapping(root, "codeMode")
	if codeModeMapping == nil {
		return fmt.Errorf("update config %q: codeMode block is not a mapping", configPath)
	}
	setMappingValue(codeModeMapping, "enabled", boolNode(enabled))
	return writeConfigDocument(configPath, mode, doc)
}

func RemoveCodeMode(configPath string) error {
	if _, err := os.Lstat(configPath); os.IsNotExist(err) {
		return nil
	}
	doc, mode, err := readOrInitConfigWithV4(configPath, true)
	if err != nil {
		return err
	}
	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	codeModeMapping := findMappingValue(root, "codeMode")
	if codeModeMapping == nil {
		return nil
	}
	if err := requireV4CodeModeRoot(configPath, root); err != nil {
		return err
	}
	if codeModeMapping.Kind != yaml.MappingNode {
		return fmt.Errorf("update config %q: codeMode block is not a mapping", configPath)
	}
	removeMappingKeyByName(codeModeMapping, "enabled")
	if len(codeModeMapping.Content) == 0 {
		removeMappingKeyByName(root, "codeMode")
	}
	return writeConfigDocument(configPath, mode, doc)
}
