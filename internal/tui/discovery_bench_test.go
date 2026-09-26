package tui

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

// BenchmarkMarketplaceDiscoveryMerge includes deterministic merge, display
// deduplication, filtering, sorting, tree rebuilding, and rendering at a
// representative terminal size; it excludes network and disk I/O.
func BenchmarkMarketplaceDiscoveryMerge(b *testing.B) {
	outcomes := make([]marketplace.DiscoveryOutcome, 12)
	for endpoint := range outcomes {
		items := make([]mpapi.ResourceListItem, marketplace.DiscoveryPageLimit)
		for i := range items {
			items[i] = mpapi.ResourceListItem{RegistryID: fmt.Sprintf("registry-%02d", endpoint), RegistrySlug: fmt.Sprintf("team-%02d", endpoint), Kind: "skill", Name: fmt.Sprintf("resource-%02d-%02d", endpoint, i), Revision: fmt.Sprintf("revision-%02d", i), ContentDigest: fmt.Sprintf("sha256:%064d", i), Description: "A realistic remotely discovered resource", Availability: mpapi.AvailabilityAvailable, Score: float64(50 - i)}
		}
		outcomes[endpoint] = marketplace.DiscoveryOutcome{Endpoint: config.MarketplaceEntry{URL: fmt.Sprintf("https://market-%02d.example", endpoint), InstanceID: fmt.Sprintf("market-%02d", endpoint), Alias: fmt.Sprintf("market-%02d", endpoint)}, Items: items, Total: 5000, Truncated: true, ObservedAt: time.Now()}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m := newTestModel()
		m.loading = false
		m.width, m.height = 120, 40
		m.filter = "resource team"
		m.applyDiscovery("resource team", outcomes)
		_ = m.View()
	}
}

func storeCachedBrowseFixture(b *testing.B) config.MarketplaceEntry {
	b.Helper()
	b.Setenv("XDG_CACHE_HOME", b.TempDir())
	return seedCachedBrowseFixture(b, config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market", Alias: "market"})
}

func seedCachedBrowseFixture(b *testing.B, endpoint config.MarketplaceEntry) config.MarketplaceEntry {
	b.Helper()
	items := make([]mpapi.ResourceListItem, marketplace.DiscoveryPageLimit)
	for i := range items {
		items[i] = mpapi.ResourceListItem{RegistryID: "tools", RegistrySlug: "tools", Kind: "skill", Name: fmt.Sprintf("resource-%02d", i), Revision: fmt.Sprintf("sha256:%064d", i), ContentDigest: fmt.Sprintf("sha256:%064d", i), Availability: mpapi.AvailabilityAvailable}
	}
	if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{Endpoint: endpoint, Items: items, Total: len(items), ObservedAt: time.Now()}, "", "", marketplace.DiscoveryPageLimit); err != nil {
		b.Fatal(err)
	}
	return endpoint
}

func BenchmarkTUIMarketplaceBrowseWarmCacheDiskIONoNetwork(b *testing.B) {
	defer benchDiscardLogs()()
	endpoint := storeCachedBrowseFixture(b)
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m := newTestModel()
		m.loading = false
		m.width, m.height = 120, 40
		msg := loadMarketplaceDiscovery(context.Background(), cfg, "", 1)().(discoveryLoadedMsg)
		if msg.err != nil || len(msg.outcomes) != 1 || len(msg.outcomes[0].Items) != marketplace.DiscoveryPageLimit {
			b.Fatalf("cached browse: %+v", msg)
		}
		m.applyDiscovery("", msg.outcomes)
		for !msg.done {
			msg = waitMarketplaceDiscoveryUpdate(1, msg.sequence+1, "", msg.updates)().(discoveryLoadedMsg)
			if msg.err != nil {
				b.Fatal(msg.err)
			}
		}
		_ = m.View()
	}
}
