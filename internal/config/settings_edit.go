package config

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type settingUpdate struct {
	key   string
	value *yaml.Node
}

// settingNode validates one user-settable value and encodes it; a nil node
// removes the key.
type settingNode func(value any) (*yaml.Node, error)

// userSettingSpecs lists the user-personal sections and fields the GUI may write.
var userSettingSpecs = map[string]map[string]settingNode{
	"backup": {
		"enabled":     clearable(boolSetting),
		"max_days":    clearable(intAtLeast(0)),
		"max_per_day": clearable(intAtLeast(0)),
	},
	"logging": {
		"enabled":      clearable(boolSetting),
		"level":        clearable(choiceSetting("debug", "info", "warn", "error")),
		"file":         clearable(pathSetting),
		"max_size_mb":  clearable(intAtLeast(1)),
		"max_backups":  clearable(intAtLeast(0)),
		"max_age_days": clearable(intAtLeast(0)),
		"compress":     clearable(boolSetting),
	},
	"update_check": {
		"enabled": clearable(boolSetting),
	},
	"materialization": {
		"mode":    clearable(choiceSetting(string(MaterializationModeSymlink), string(MaterializationModeFile))),
		"targets": clearable(materializationTargetsSetting),
	},
}

// ValidateUserSettings reports whether fields are valid writes for section without touching the file.
func ValidateUserSettings(section string, fields map[string]any) error {
	_, err := settingUpdates(section, fields)
	return err
}

// SetUserSettings updates fields of one section of the user-personal config.
// A nil field value removes that key, and a section left empty is removed.
// Every value is validated before the file is read for writing, so a refused
// update leaves the file unchanged.
func SetUserSettings(configPath, section string, fields map[string]any) error {
	if !isGlobalConfigPath(configPath) {
		return fmt.Errorf("settings can only be configured in the user-personal config")
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("user config not found at %q; run 'agent-marketplace init' first", configPath)
	}
	updates, err := settingUpdates(section, fields)
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
	sectionMapping := ensureTopLevelMapping(root, section)
	if sectionMapping == nil {
		return fmt.Errorf("update config %q: %s block is not a mapping", configPath, section)
	}
	for _, update := range updates {
		if update.value == nil {
			removeMappingKeyByName(sectionMapping, update.key)
		} else {
			setMappingValue(sectionMapping, update.key, update.value)
		}
	}
	if len(sectionMapping.Content) == 0 {
		removeMappingKeyByName(root, section)
	}
	return writeConfigDocument(configPath, mode, doc)
}

func settingUpdates(section string, fields map[string]any) ([]settingUpdate, error) {
	specs, ok := userSettingSpecs[section]
	if !ok {
		return nil, fmt.Errorf("unknown settings section %q", section)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	updates := make([]settingUpdate, 0, len(keys))
	for _, key := range keys {
		spec, ok := specs[key]
		if !ok {
			return nil, fmt.Errorf("unknown %s setting %q", section, key)
		}
		value, err := spec(fields[key])
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", section, key, err)
		}
		updates = append(updates, settingUpdate{key: key, value: value})
	}
	return updates, nil
}

func clearable(encode settingNode) settingNode {
	return func(value any) (*yaml.Node, error) {
		if value == nil {
			return nil, nil
		}
		return encode(value)
	}
}

func boolSetting(value any) (*yaml.Node, error) {
	flag, ok := value.(bool)
	if !ok {
		return nil, fmt.Errorf("expected a boolean")
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(flag)}, nil
}

func intAtLeast(minimum int) settingNode {
	return func(value any) (*yaml.Node, error) {
		number, ok := value.(int)
		if !ok || number < minimum {
			return nil, fmt.Errorf("expected an integer of at least %d", minimum)
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(number)}, nil
	}
}

func choiceSetting(choices ...string) settingNode {
	return func(value any) (*yaml.Node, error) {
		text, ok := value.(string)
		if !ok || !containsString(choices, text) {
			return nil, fmt.Errorf("expected one of %s", strings.Join(choices, ", "))
		}
		return scalarNode(text), nil
	}
}

func pathSetting(value any) (*yaml.Node, error) {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("expected a non-empty path")
	}
	return scalarNode(text), nil
}

// materializationTargetsSetting encodes a harness-to-mode map; an empty map
// clears the targets key.
func materializationTargetsSetting(value any) (*yaml.Node, error) {
	targets, ok := value.(map[string]string)
	if !ok {
		return nil, fmt.Errorf("expected a map of harness to mode")
	}
	if len(targets) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(targets))
	for id := range targets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, id := range ids {
		if !IsSupportedHarness(id) {
			return nil, fmt.Errorf("unknown harness %q", id)
		}
		mode := targets[id]
		if mode != string(MaterializationModeSymlink) && mode != string(MaterializationModeFile) {
			return nil, fmt.Errorf("unknown materialization mode %q for %s", mode, id)
		}
		mapping.Content = append(mapping.Content, scalarNode(id), scalarNode(mode))
	}
	return mapping, nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
