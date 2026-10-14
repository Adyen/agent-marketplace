package config

func loadConfig(path string, requireHarnesses bool) (*Config, error) {
	return loadConfigGated(path, requireHarnesses, true, repoLayer.name())
}

func loadConfigGated(path string, requireHarnesses, requireAPIVersion bool, layer string) (*Config, error) {
	cfg, err := LoadConfigFromPath(path)
	if err != nil || cfg == nil {
		return cfg, err
	}
	if err := cfg.validate(requireHarnesses, requireAPIVersion, layer); err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadLocalConfig(path string) (*LocalConfig, error) {
	return LoadLocalConfigFromPath(path)
}
