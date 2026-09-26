package config

import (
	"fmt"
	"os"
)

func SetCodeModeEnabled(configPath string, enabled bool) error {
	doc, mode, err := readOrInitConfigWithV4(configPath, true)
	if err != nil {
		return err
	}
	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	codeModeMapping := ensureTopLevelMapping(root, "codeMode")
	if codeModeMapping == nil {
		return fmt.Errorf("update config %q: codeMode block is not a mapping", configPath)
	}
	setMappingValue(codeModeMapping, "enabled", boolNode(enabled))
	return writeConfigDocument(configPath, mode, doc)
}

func RemoveCodeMode(configPath string) error {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
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
	codeModeMapping := ensureTopLevelMapping(root, "codeMode")
	if codeModeMapping == nil {
		return fmt.Errorf("update config %q: codeMode block is not a mapping", configPath)
	}
	removeMappingKeyByName(codeModeMapping, "enabled")
	if len(codeModeMapping.Content) == 0 {
		removeMappingKeyByName(root, "codeMode")
	}
	return writeConfigDocument(configPath, mode, doc)
}
