package config

import (
	"fmt"
	"log/slog"

	"gopkg.in/yaml.v3"
)

type GUIConfig struct {
	VersionCheck *bool `yaml:"version_check,omitempty"`
}

const GUIOrganizationOnlyCode = "config.gui.organization-only"

type GUIIgnoredSettingWarning struct {
	Code   string
	Source Source
	Path   string
}

func (w *GUIIgnoredSettingWarning) Error() string {
	return fmt.Sprintf("%s: %s: gui.version_check is organization policy and is ignored in %s configuration", w.Code, w.Path, w.Source.String())
}

func GUIReleaseCheckEnabled(organization Settings) bool {
	if organization.GUI == nil || organization.GUI.VersionCheck == nil {
		return true
	}
	return *organization.GUI.VersionCheck
}

func (l *LayerIntent) warnLowerLayerGUI(gui *GUIConfig) {
	if l.Source == Organization || gui == nil || gui.VersionCheck == nil {
		return
	}
	warning := &GUIIgnoredSettingWarning{Code: GUIOrganizationOnlyCode, Source: l.Source, Path: l.Path}
	l.warnings = append(l.warnings, warning)
	slog.Warn("gui.version_check ignored outside organization configuration",
		"code", GUIOrganizationOnlyCode,
		"source", l.Source.String(),
		"path", l.Path,
	)
}

func mergeGUIConfig(dst, src *GUIConfig) *GUIConfig {
	if src == nil {
		return dst
	}
	if dst == nil {
		dst = &GUIConfig{}
	}
	if src.VersionCheck != nil {
		value := *src.VersionCheck
		dst.VersionCheck = &value
	}
	return dst
}

func validateGUIConfigNode(node *yaml.Node, kind layerKind) []error {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return []error{fmt.Errorf("line %d: gui must be a mapping in %s configuration", node.Line, kind.name())}
	}
	var errs []error
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := resolveAlias(node.Content[i])
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}
		if keyNode.Value != "version_check" {
			errs = append(errs, fmt.Errorf("line %d: forbidden or unknown gui field %q in %s configuration", keyNode.Line, keyNode.Value, kind.name()))
			continue
		}
		valNode := resolveAlias(node.Content[i+1])
		if valNode.Kind != yaml.ScalarNode || (valNode.Tag != "!!bool" && valNode.Tag != "!!null") {
			errs = append(errs, fmt.Errorf("line %d: gui.version_check must be a boolean in %s configuration", valNode.Line, kind.name()))
		}
	}
	return errs
}
