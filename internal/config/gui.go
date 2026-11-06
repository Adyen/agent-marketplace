package config

import (
	"fmt"
	"log/slog"
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
