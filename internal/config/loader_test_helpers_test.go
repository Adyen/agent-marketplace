package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// loadConfig reads and validates a Config from an explicit path for tests.
// It is kept as a test helper because it supports toggling the harnesses
// requirement, which the public LoadConfigFromPath does not expose.
func loadConfig(path string, requireHarnesses bool) (*Config, error) {
	return loadConfigGated(path, requireHarnesses, true, repoLayer.name())
}

func loadConfigGated(path string, requireHarnesses, requireAPIVersion bool, layer string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	normalizeConfigHarnessTargets(&cfg)

	if err := cfg.validate(requireHarnesses, requireAPIVersion, layer); err != nil {
		return nil, fmt.Errorf("validate config %q: %w", path, err)
	}
	return &cfg, nil
}

// loadLocalConfig reads and validates a LocalConfig from an explicit path for
// tests. It is a thin wrapper around loadV3LocalConfig.
func loadLocalConfig(path string) (*LocalConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read local config %q: %w", path, err)
	}
	cfg, errs := loadV3LocalConfig(data)
	if len(errs) > 0 {
		return nil, fmt.Errorf("validate local config %q: %w", path, errors.Join(errs...))
	}
	return cfg, nil
}
