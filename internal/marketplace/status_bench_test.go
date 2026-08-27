package marketplace

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func BenchmarkMarketplaceStatusInspection(b *testing.B) {
	const pinCount = 32
	contentDigest := "sha256:" + strings.Repeat("a", 64)
	archiveDigest := "sha256:" + strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "mp")
			return
		}
		_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "resource-" + strconv.Itoa(pinCount)},
			ContentDigest: store.RevisionDigest(contentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		}))
	}))
	defer server.Close()

	cache := NewCache(b.TempDir())
	pins := make([]config.MarketplaceResourcePin, pinCount)
	for i := range pins {
		name := "resource-" + strconv.Itoa(i)
		pins[i] = config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: name, Revision: "v1", Digest: contentDigest}
		if _, err := cache.WriteResourceRevision(server.URL, "src", "skill", name, contentDigest, assessedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: name},
			ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
		})); err != nil {
			b.Fatal(err)
		}
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: pins}}}
	inspector := &Resolver{Cache: cache}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inspection := inspector.Inspect(b.Context(), cfg, nil, resolver.MarketplaceResolveModeCached)
		if len(inspection.Pins) != pinCount {
			b.Fatalf("inspected %d pins, want %d", len(inspection.Pins), pinCount)
		}
	}
}
