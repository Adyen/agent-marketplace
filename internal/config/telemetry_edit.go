package config

import (
	"fmt"
	"log/slog"
	"os"

	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
	"gopkg.in/yaml.v3"
)

// SetUserTelemetryLevel sets the telemetry.level value in the user-personal
// config after validating the level. It requires an existing user config.
func SetUserTelemetryLevel(configPath, level string) error {
	if !isGlobalConfigPath(configPath) {
		return fmt.Errorf("telemetry can only be configured in the user-personal config")
	}
	normalized, err := tmcfg.NormalizeLevel(level)
	if err != nil {
		return err
	}
	return updateTelemetryScalar(configPath, "level", scalarNode(normalized))
}

// SetUserTelemetryEndpoint sets the telemetry.endpoint value in the user-personal
// config. An empty endpoint removes the key. User endpoints must use HTTPS or
// loopback; allow_insecure is an organization-only policy.
func SetUserTelemetryEndpoint(configPath, endpoint string) error {
	if !isGlobalConfigPath(configPath) {
		return fmt.Errorf("telemetry can only be configured in the user-personal config")
	}
	if endpoint == "" {
		return updateTelemetryScalar(configPath, "endpoint", nil)
	}
	if err := tmcfg.ValidateEndpoint(endpoint, false); err != nil {
		return err
	}
	return updateTelemetryScalar(configPath, "endpoint", scalarNode(endpoint))
}

// DisableTelemetry sets telemetry.level to "off" in the user-personal config.
// It is a no-op when the config file does not exist.
func DisableTelemetry(configPath string) error {
	if !isGlobalConfigPath(configPath) {
		return fmt.Errorf("telemetry can only be configured in the user-personal config")
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		slog.Debug("disable telemetry: user config does not exist; nothing to do")
		return nil
	}
	return updateTelemetryScalar(configPath, "level", scalarNode(tmcfg.LevelOff))
}

func updateTelemetryScalar(configPath, key string, value *yaml.Node) error {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("user config not found at %q; run 'agent-marketplace init' first", configPath)
	}

	doc, mode, err := readOrInitConfig(configPath)
	if err != nil {
		return err
	}

	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}

	telemetryMapping := ensureTopLevelMapping(root, "telemetry")
	if telemetryMapping == nil {
		return fmt.Errorf("update config %q: telemetry block is not a mapping", configPath)
	}

	if value == nil {
		removeMappingKeyByName(telemetryMapping, key)
	} else {
		setMappingValue(telemetryMapping, key, value)
	}

	return writeConfigDocument(configPath, mode, doc)
}
