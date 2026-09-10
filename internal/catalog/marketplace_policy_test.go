package catalog

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestLoadRegistriesPreservesAllowedRegistriesForMarketplaceResolution(t *testing.T) {
	allowed := []string{`(^|\\.)trusted\\.example/`}
	cfg := &config.Config{
		AllowedRegistries: &allowed,
		Marketplaces:      []config.MarketplaceEntry{{URL: "https://market.example", InstanceID: "market"}},
	}

	result := LoadRegistries(cfg, t.TempDir(), LoadModeRefresh)
	if result.Config == nil || result.Config.AllowedRegistries == nil {
		t.Fatal("loaded config lost AllowedRegistries")
	}
	if len(*result.Config.AllowedRegistries) != 1 || (*result.Config.AllowedRegistries)[0] != allowed[0] {
		t.Fatalf("loaded AllowedRegistries = %#v, want %#v", *result.Config.AllowedRegistries, allowed)
	}
}
