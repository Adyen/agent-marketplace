package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestMarketplaceResolvedCollision(t *testing.T) {
	plan := &marketplace.PreparedInstall{
		Entry:    config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
		Pin:      config.MarketplaceResourcePin{Name: "review", Kind: "skill", InstalledName: "alias", RegistryID: "source"},
		Resolved: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}},
	}
	collection := &marketplace.PreparedInstall{CollectionMembers: []*marketplace.PreparedInstall{plan}}
	for _, source := range []config.Source{config.RepoCommitted, config.UserPersonal, config.Organization} {
		resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "alias"}, Source: source}}
		for _, tc := range []struct {
			name string
			plan *marketplace.PreparedInstall
		}{
			{"leaf", plan},
			{"collection", collection},
			{"nested", &marketplace.PreparedInstall{CollectionMembers: []*marketplace.PreparedInstall{collection}}},
		} {
			t.Run(source.String()+"/"+tc.name, func(t *testing.T) {
				if err := checkResolvedMarketplaceInstallCollision(resolved, tc.plan); !errors.Is(err, ErrMarketplaceInstallCollision) {
					t.Fatalf("collision error = %v", err)
				}
			})
		}
	}
	for _, tc := range []struct {
		name      string
		existing  resolver.ResolvedArtifact
		collision bool
	}{
		{"same pin", resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}, InstalledName: "alias", IsMarketplace: true, MarketplaceURL: "https://market.example", MarketplaceInstanceID: "mp", MarketplaceRegistryID: "source"}, false},
		{"other Marketplace", resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}, InstalledName: "alias", IsMarketplace: true, MarketplaceURL: "https://other.example", MarketplaceInstanceID: "mp", MarketplaceRegistryID: "source"}, true},
		{"other kind", resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindCommand, Name: "alias"}}, false},
		{"other name", resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "different"}}, false},
		{"different installed name", resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "alias"}, InstalledName: "different"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkResolvedMarketplaceInstallCollision([]resolver.ResolvedArtifact{tc.existing}, collection)
			if tc.collision != errors.Is(err, ErrMarketplaceInstallCollision) || (!tc.collision && err != nil) {
				t.Fatalf("collision error=%v want collision=%v", err, tc.collision)
			}
			if tc.collision && !strings.Contains(err.Error(), "--as <name>") {
				t.Fatalf("collision lacks alias guidance: %v", err)
			}
		})
	}
}

func TestMarketplaceInstallCollisionHonorsCollectionExclusions(t *testing.T) {
	plan := &marketplace.PreparedInstall{
		Entry:    config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
		Pin:      config.MarketplaceResourcePin{RegistryID: "new-source", Kind: "skill", Name: "review"},
		Resolved: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}},
	}
	for _, tc := range []struct {
		name            string
		source          config.Source
		exclusionSource config.Source
		exclude         bool
		required        bool
		direct          bool
		collision       bool
	}{
		{name: "active member", source: config.Organization, collision: true},
		{name: "excluded organization member", source: config.Organization, exclusionSource: config.UserPersonal, exclude: true},
		{name: "excluded user member", source: config.UserPersonal, exclusionSource: config.UserPersonal, exclude: true},
		{name: "excluded repo member", source: config.RepoCommitted, exclusionSource: config.RepoCommitted, exclude: true},
		{name: "excluded local member", source: config.RepoPersonal, exclusionSource: config.RepoPersonal, exclude: true},
		{name: "required member", source: config.Organization, exclusionSource: config.UserPersonal, exclude: true, required: true, collision: true},
		{name: "direct user pin", source: config.UserPersonal, exclusionSource: config.UserPersonal, exclude: true, direct: true, collision: true},
		{name: "wrong exclusion scope", source: config.Organization, exclusionSource: config.RepoCommitted, exclude: true, collision: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
				URL: "https://market.example", InstanceID: "mp", Source: tc.source, Required: tc.required,
				Resources: []config.MarketplaceResourcePin{{RegistryID: "old-source", RegistrySlug: "old", Kind: "skill", Name: "review", Direct: tc.direct}},
			}}}
			if tc.exclude {
				cfg.MarketplaceExclusions = []config.MarketplaceExclusion{{URL: "https://market.example", Locator: "old/skill/review", Source: tc.exclusionSource}}
			}
			err := checkMarketplaceInstallCollision(context.Background(), InstallOptions{RepoRoot: t.TempDir()}, &config.EffectiveConfig{Merged: cfg}, plan)
			if tc.collision != errors.Is(err, ErrMarketplaceInstallCollision) || (!tc.collision && err != nil) {
				t.Fatalf("collision error=%v want collision=%v", err, tc.collision)
			}
			if tc.collision && !strings.Contains(err.Error(), "--as <name>") {
				t.Fatalf("collision lacks alias guidance: %v", err)
			}
		})
	}
}
