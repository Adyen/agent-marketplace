package codemode

import (
	"context"
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func catalogTestEntry() config.MarketplaceEntry {
	return config.MarketplaceEntry{Alias: "pub", URL: "https://mp.example", InstanceID: "mp-1"}
}

func catalogResource(kind, name, rev string, availability mpapi.Availability) mpapi.CatalogResource {
	return mpapi.CatalogResource{
		RegistryID: "reg-id", RegistrySlug: "tools", Kind: kind,
		Name: name, Revision: rev, Availability: availability,
	}
}

// spec: code-mode-mcp/marketplace-resources-beyond-the-first-page-are-searchable
func TestSnapshotCompleteCatalogBeyondFirstPage(t *testing.T) {
	snapshotOfflineHome(t)
	var resources []mpapi.CatalogResource
	for i := 0; i < 60; i++ {
		resources = append(resources, catalogResource("skill", fmt.Sprintf("skill-%02d", i), fmt.Sprintf("sha256:%064x", i+1), mpapi.AvailabilityAvailable))
	}
	resources = append(resources, catalogResource("mcpserver", "late-server", fmt.Sprintf("sha256:%064x", 999), mpapi.AvailabilityAvailable))

	builder := &SnapshotBuilder{
		CatalogMode: catalog.LoadModeCached,
		Catalog: func(context.Context, config.EffectiveConfig, catalog.LoadMode) ([]marketplace.CatalogOutcome, error) {
			return []marketplace.CatalogOutcome{{
				Endpoint: catalogTestEntry(), Resources: resources, Complete: true, FromCache: true,
			}}, nil
		},
	}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{catalogTestEntry()}}}
	snap, errs := builder.BuildSnapshot(context.Background(), ec)
	if len(errs) > 0 {
		t.Fatalf("BuildSnapshot errors: %v", errs)
	}
	if snap.Stale {
		t.Fatal("complete catalog snapshot marked stale")
	}
	for _, name := range []string{"skill-59", "late-server"} {
		found := false
		for i := range snap.Resources {
			if snap.Resources[i].Name == name {
				found = true
				if name == "late-server" && snap.Resources[i].Tools != "unknown" {
					t.Fatalf("mcp resource tools = %v, want unknown", snap.Resources[i].Tools)
				}
			}
		}
		if !found {
			t.Fatalf("resource %q missing from snapshot %+v", name, snap.Resources)
		}
	}
	res := snap.Find("pub:tools/mcp/late-server")
	if res == nil {
		t.Fatalf("Marketplace MCP ref missing: %+v", snap.Resources)
	}
	if res.MarketplaceOrigin() == nil || res.MarketplaceOrigin().RegistryID != "reg-id" {
		t.Fatalf("Marketplace origin = %+v", res.MarketplaceOrigin())
	}
}

// spec: code-mode-mcp/cold-catalog-reports-stale-and-later-session-reuses-refresh
func TestSnapshotColdStaleThenReusesCompleteCache(t *testing.T) {
	snapshotOfflineHome(t)
	var cachedCalls int
	builder := &SnapshotBuilder{
		CatalogMode: catalog.LoadModeCached,
		Catalog: func(_ context.Context, _ config.EffectiveConfig, mode catalog.LoadMode) ([]marketplace.CatalogOutcome, error) {
			if mode == catalog.LoadModeCached {
				cachedCalls++
				if cachedCalls == 1 {
					return []marketplace.CatalogOutcome{{Endpoint: catalogTestEntry()}}, nil
				}
			}
			return []marketplace.CatalogOutcome{{
				Endpoint:  catalogTestEntry(),
				Complete:  true,
				FromCache: mode == catalog.LoadModeCached,
				Resources: []mpapi.CatalogResource{
					catalogResource("skill", "one", fmt.Sprintf("sha256:%064x", 1), mpapi.AvailabilityAvailable),
				},
			}}, nil
		},
	}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{catalogTestEntry()}}}

	cold, errs := builder.BuildSnapshot(context.Background(), ec)
	if len(errs) > 0 {
		t.Fatalf("cold BuildSnapshot errors: %v", errs)
	}
	if !cold.Stale || len(cold.Resources) != 0 {
		t.Fatalf("cold snapshot = stale %v resources %+v, want stale and empty", cold.Stale, cold.Resources)
	}

	refresh := &SnapshotBuilder{CatalogMode: catalog.LoadModeRefresh, Catalog: builder.Catalog}
	fresh, errs := refresh.BuildSnapshot(context.Background(), ec)
	if len(errs) > 0 {
		t.Fatalf("refresh BuildSnapshot errors: %v", errs)
	}
	if fresh.Stale || len(fresh.Resources) != 1 {
		t.Fatalf("refreshed snapshot = stale %v resources %+v", fresh.Stale, fresh.Resources)
	}

	later, errs := builder.BuildSnapshot(context.Background(), ec)
	if len(errs) > 0 {
		t.Fatalf("later BuildSnapshot errors: %v", errs)
	}
	if later.Stale || len(later.Resources) != 1 {
		t.Fatalf("later cached snapshot = stale %v resources %+v, want reused complete catalog", later.Stale, later.Resources)
	}
}

// spec: code-mode-mcp/incomplete-or-inconsistent-catalog-remains-stale
func TestSnapshotIncompleteCatalogStaysStale(t *testing.T) {
	snapshotOfflineHome(t)
	builder := &SnapshotBuilder{
		CatalogMode: catalog.LoadModeCached,
		Catalog: func(context.Context, config.EffectiveConfig, catalog.LoadMode) ([]marketplace.CatalogOutcome, error) {
			return []marketplace.CatalogOutcome{{
				Endpoint: catalogTestEntry(),
				Err:      fmt.Errorf("scan aborted: catalog version changed"),
				Resources: []mpapi.CatalogResource{
					catalogResource("skill", "partial", fmt.Sprintf("sha256:%064x", 1), mpapi.AvailabilityAvailable),
				},
			}}, nil
		},
	}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{catalogTestEntry()}}}
	snap, _ := builder.BuildSnapshot(context.Background(), ec)
	if !snap.Stale {
		t.Fatal("incomplete catalog published as complete")
	}
	if snap.Find("pub:tools/skill/partial") != nil {
		t.Fatal("partial scan resource was published")
	}
}

// spec: code-mode-mcp/unavailable-or-rejected-marketplace-resource-is-not-executable
func TestSnapshotCatalogFiltersUnavailable(t *testing.T) {
	snapshotOfflineHome(t)
	builder := &SnapshotBuilder{
		CatalogMode: catalog.LoadModeCached,
		Catalog: func(context.Context, config.EffectiveConfig, catalog.LoadMode) ([]marketplace.CatalogOutcome, error) {
			return []marketplace.CatalogOutcome{{
				Endpoint: catalogTestEntry(), Complete: true, FromCache: true,
				Resources: []mpapi.CatalogResource{
					catalogResource("skill", "good", fmt.Sprintf("sha256:%064x", 1), mpapi.AvailabilityAvailable),
					catalogResource("skill", "revoked", fmt.Sprintf("sha256:%064x", 2), mpapi.AvailabilityRevoked),
					catalogResource("skill", "blocked", fmt.Sprintf("sha256:%064x", 3), mpapi.AvailabilityBlocked),
					catalogResource("skill", "no-artifact", fmt.Sprintf("sha256:%064x", 4), mpapi.AvailabilityNoArtifact),
				},
			}}, nil
		},
	}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{catalogTestEntry()}}}
	snap, errs := builder.BuildSnapshot(context.Background(), ec)
	if len(errs) > 0 {
		t.Fatalf("BuildSnapshot errors: %v", errs)
	}
	if snap.Find("pub:tools/skill/good") == nil {
		t.Fatalf("available resource missing: %+v", snap.Resources)
	}
	for _, ref := range []string{"pub:tools/skill/revoked", "pub:tools/skill/blocked", "pub:tools/skill/no-artifact"} {
		if snap.Find(ref) != nil {
			t.Fatalf("unavailable ref %q was offered", ref)
		}
	}
}
