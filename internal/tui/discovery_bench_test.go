package tui

import (
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
