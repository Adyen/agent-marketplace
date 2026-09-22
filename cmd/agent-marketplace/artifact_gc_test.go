package main

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestMarketplaceGCRetainsShadowedLayerPins(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	user := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Alias: "market", Resources: []config.MarketplaceResourcePin{{Name: "old", LockArchiveDigest: "legacy"}}}
	repo := config.MarketplaceEntry{URL: user.URL, InstanceID: "mp", Alias: "market", Resources: []config.MarketplaceResourcePin{{Name: "new", LockArtifactSchema: "v2", LockArtifactDigest: "artifact", LockBlobDigest: "blob"}}}
	effective := config.EffectiveConfig{User: config.LayerResult{Source: config.UserPersonal, Config: &config.Config{Marketplaces: []config.MarketplaceEntry{user}}}, Repo: config.LayerResult{Source: config.RepoCommitted, Config: &config.Config{Marketplaces: []config.MarketplaceEntry{repo}}}, Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{repo}}}
	retained := marketplaceGCConfig(effective)
	if len(retained.Marketplaces) != 2 || retained.Marketplaces[0].Resources[0].LockArchiveDigest != "legacy" || retained.Marketplaces[1].Resources[0].LockBlobDigest != "blob" {
		t.Fatalf("shadowed references lost: %+v", retained.Marketplaces)
	}
	if user.Alias != "market" || effective.User.Config.Marketplaces[0].Alias != "market" {
		t.Fatal("GC changed configured alias")
	}
}
