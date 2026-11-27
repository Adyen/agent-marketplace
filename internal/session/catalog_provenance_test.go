package session

import (
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: tui-rendering/optional-organization-default-and-collection-selected-resource-show-provenance
func TestBuildRowsTracksMarketplaceCollectionProvenanceAcrossLayers(t *testing.T) {
	selectedBy := config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{
		{RegistryID: "tools", Name: "bundle", Revision: "sha256:one"},
		{RegistryID: "tools", Name: "bundle", Revision: "sha256:two"},
		{RegistryID: "tools", Name: "plugins", Revision: "sha256:three"},
	})
	pin := config.MarketplaceResourcePin{
		RegistryID: "tools",
		Kind:       "skill",
		Name:       "review",
		Revision:   "sha256:leaf",
		Digest:     "sha256:leaf",
		SelectedBy: selectedBy,
	}
	organization := config.MarketplaceEntry{
		URL:        "https://example.invalid/market",
		InstanceID: "tools",
		Source:     config.Organization,
		Resources:  []config.MarketplaceResourcePin{pin},
	}
	user := organization
	user.Source = config.UserPersonal
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{organization, user}}
	catalogKey := MarketplaceCatalogKey(organization, pin)
	resolved := []resolver.ResolvedArtifact{{
		Entry:                   catalog.Entry{Kind: ir.KindSkill, Name: "review"},
		RegistryURL:             organization.URL,
		Source:                  config.Organization,
		CatalogKey:              catalogKey,
		IsMarketplace:           true,
		MarketplaceURL:          organization.URL,
		MarketplaceInstanceID:   organization.InstanceID,
		MarketplaceRegistryID:   pin.RegistryID,
		MarketplaceResourceName: pin.Name,
	}}
	rows := BuildRows(cfg, nil, resolved, nil, materialize.DriftReport{})

	if len(rows) != 1 {
		t.Fatalf("BuildRows returned %d rows, want 1", len(rows))
	}
	row := rows[0]
	if !row.EnabledGlobal || !row.EnabledOrganization || !row.EnabledUser {
		t.Fatalf("Marketplace selection provenance = global:%t organization:%t user:%t", row.EnabledGlobal, row.EnabledOrganization, row.EnabledUser)
	}
	if !slices.Equal(row.CollectionNames(), []string{"bundle", "plugins"}) {
		t.Fatalf("Marketplace collection provenance = %v, want [bundle plugins]", row.CollectionNames())
	}
}

// spec: tui-rendering/optional-organization-default-and-collection-selected-resource-show-provenance
func TestBuildRowsShowsSiblingGitCollectionParentsOnceInOrder(t *testing.T) {
	const (
		collectionsURL = "git@example.com:collections.git"
		providerURL    = "git@example.com:provider.git"
		losingURL      = "git@example.com:losing.git"
	)
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{
			URL:         collectionsURL,
			Ref:         "main",
			Source:      config.UserPersonal,
			Collections: config.KindSelector{Include: []string{"first", "second", "third-loser"}},
		},
		{URL: providerURL, Ref: "main", Source: config.Organization},
		{URL: losingURL, Ref: "main", Source: config.Organization},
	}}
	for i := range cfg.Registries {
		entry := &cfg.Registries[i]
		entry.CatalogKey = cache.CatalogKeyForEntry(entry.URL, entry.Ref, entry.Path)
	}
	member := []ir.CollectionMember{{Name: "shared", Kind: ir.KindSkill, Registry: providerURL}}
	losingMember := []ir.CollectionMember{{Name: "shared", Kind: ir.KindSkill, Registry: losingURL}}
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {
			{Kind: ir.KindCollection, Name: "first", Resource: ir.NewCollection("first", "", "", member, nil, nil, ir.SourceRef{})},
			{Kind: ir.KindCollection, Name: "second", Resource: ir.NewCollection("second", "", "", member, nil, nil, ir.SourceRef{})},
			{Kind: ir.KindCollection, Name: "third-loser", Resource: ir.NewCollection("third-loser", "", "", losingMember, nil, nil, ir.SourceRef{})},
		},
		cfg.Registries[1].CatalogKey: {
			{Kind: ir.KindSkill, Name: "shared", Path: "/cache/provider/skills/shared"},
		},
		cfg.Registries[2].CatalogKey: {
			{Kind: ir.KindSkill, Name: "shared", Path: "/cache/losing/skills/shared"},
		},
	}

	resolved, errs := resolver.Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve errors = %v", errs)
	}
	if len(resolved) != 1 || resolved[0].Kind != ir.KindSkill || resolved[0].Name != "shared" ||
		resolved[0].Source != config.Organization || resolved[0].RegistryURL != providerURL {
		t.Fatalf("resolved sibling member count = %d, want one shared skill: %#v", len(resolved), resolved)
	}
	rows := BuildRows(cfg, nil, resolved, nil, materialize.DriftReport{})
	if len(rows) != 1 {
		t.Fatalf("BuildRows returned %d rows, want one installed member: %#v", len(rows), rows)
	}
	if rows[0].Artifact.Via != "collection:first" {
		t.Fatalf("winning Via = %q, want collection:first", rows[0].Artifact.Via)
	}
	if got := rows[0].CollectionNames(); !slices.Equal(got, []string{"first", "second"}) {
		t.Fatalf("collection parents = %v, want [first second] without Via duplicates", got)
	}
	if !rows[0].EnabledGlobal || rows[0].EnabledOrganization || !rows[0].EnabledUser {
		t.Fatalf("collection selection layers = global:%t organization:%t user:%t, want user selection only",
			rows[0].EnabledGlobal, rows[0].EnabledOrganization, rows[0].EnabledUser)
	}
}

// spec: tui-rendering/optional-organization-default-and-collection-selected-resource-show-provenance
func TestBuildRowsUsesMarketplaceSelectionLayer(t *testing.T) {
	pin := config.MarketplaceResourcePin{RegistryID: "tools", Kind: "skill", Name: "review"}
	market := config.MarketplaceEntry{
		URL:        "https://example.invalid/market",
		InstanceID: "tools",
		Source:     config.UserPersonal,
		Resources:  []config.MarketplaceResourcePin{pin},
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{market}}
	resolved := []resolver.ResolvedArtifact{{
		Entry:                   catalog.Entry{Kind: ir.KindSkill, Name: "review"},
		RegistryURL:             market.URL,
		Source:                  config.Organization,
		CatalogKey:              MarketplaceCatalogKey(market, pin),
		IsMarketplace:           true,
		MarketplaceURL:          market.URL,
		MarketplaceInstanceID:   market.InstanceID,
		MarketplaceRegistryID:   pin.RegistryID,
		MarketplaceResourceName: pin.Name,
	}}

	rows := BuildRows(cfg, nil, resolved, nil, materialize.DriftReport{})
	if len(rows) != 1 {
		t.Fatalf("BuildRows returned %d rows, want 1", len(rows))
	}
	if !rows[0].EnabledGlobal || rows[0].EnabledOrganization || !rows[0].EnabledUser {
		t.Fatalf("selection layer flags = global:%t organization:%t user:%t, want user-only global selection", rows[0].EnabledGlobal, rows[0].EnabledOrganization, rows[0].EnabledUser)
	}
}
