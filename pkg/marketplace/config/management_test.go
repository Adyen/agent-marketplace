package config

import (
	"gopkg.in/yaml.v3"
	"testing"
)

func TestManagementDefaultsAndProfiles(t *testing.T) {
	var cfg Config
	applyDefaults(&cfg)
	if cfg.Management.Mode != "file" {
		t.Fatal("management enabled by default")
	}
	err := yaml.Unmarshal([]byte("management:\n  mode: database\n  allowed_origins: [https://git.test]\n  profiles:\n    readonly:\n      allowed_origins: [https://git.test]\n      credentials:\n        token:\n          env: TOKEN\n"), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.Management.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"unknown", "Database"} {
		cfg.Management.Mode = mode
		if cfg.Management.Validate() == nil {
			t.Fatal("invalid mode accepted")
		}
	}
	cfg.Management.Mode = "database"
	p := cfg.Management.Profiles["readonly"]
	p.Credentials.Token.Value = "secret"
	cfg.Management.Profiles["readonly"] = p
	if cfg.Management.Validate() == nil {
		t.Fatal("inline secret accepted")
	}
}
