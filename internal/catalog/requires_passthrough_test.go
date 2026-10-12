package catalog

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// spec: configuration-settings/expansion-is-disabled-when-no-layer-sets-it
func TestLoadRegistriesPreservesRequiresSettings(t *testing.T) {
	expand := true
	cfg := &config.Config{Requires: &config.RequiresConfig{Expand: &expand}}
	result := LoadRegistries(cfg, "", LoadModeInspect)
	if result.Config == nil || !result.Config.RequiresExpansionEnabled() {
		t.Fatalf("loaded config dropped requires settings: %#v", result.Config)
	}

	result = LoadRegistries(&config.Config{}, "", LoadModeInspect)
	if result.Config == nil || result.Config.RequiresExpansionEnabled() {
		t.Fatalf("requires expansion must stay disabled by default: %#v", result.Config)
	}
}
