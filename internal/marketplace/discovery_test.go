package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func TestDiscoverFreshCacheAvoidsSecondNetworkRequest(t *testing.T) {
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp-alpha")
		case "/api/v1/search":
			searches.Add(1)
			if got := r.URL.Query().Get("limit"); got != "50" {
				t.Errorf("limit = %q, want 50", got)
			}
			_ = json.NewEncoder(w).Encode(SearchResponse{Items: []mpapi.ResourceListItem{canonicalSearchItem("registry-a", "skill", "review", 1)}, Total: 72, Limit: 50})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ep := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp-alpha", Alias: "alpha"}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{ep}}}
	cache := NewDiscoveryCache(t.TempDir())

	first, err := Discover(context.Background(), ec, "", DiscoveryOptions{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Discover(context.Background(), ec, "", DiscoveryOptions{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	if got := searches.Load(); got != 1 {
		t.Fatalf("search requests = %d, want 1", got)
	}
	if len(first) != 1 || first[0].Total != 72 || !first[0].Truncated {
		t.Fatalf("first outcome = %#v", first)
	}
	if len(second) != 1 || second[0].CacheState != DiscoveryCacheFresh || second[0].Items[0].Name != "review" {
		t.Fatalf("second outcome = %#v", second)
	}
}

func TestDiscoveryCacheRetainsEmptyBrowseAndFiftyRecentQueries(t *testing.T) {
	cache := NewDiscoveryCache(t.TempDir())
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	store := func(query string, observed time.Time) {
		t.Helper()
		outcome := DiscoveryOutcome{Endpoint: ep, ObservedAt: observed, Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", query+"-result", 1)}}
		if err := cache.Store(outcome, query, "", DiscoveryPageLimit); err != nil {
			t.Fatal(err)
		}
	}
	store("", now.Add(-time.Hour))
	base := now.Add(-2 * time.Minute)
	for i := 0; i < 52; i++ {
		store(fmt.Sprintf("query-%02d", i), base.Add(time.Duration(i)*time.Second))
	}
	if _, ok := cache.Load(ep, "", "", DiscoveryPageLimit); !ok {
		t.Fatal("empty browse page was evicted")
	}
	if _, ok := cache.Load(ep, "query-00", "", DiscoveryPageLimit); ok {
		t.Fatal("oldest non-empty query was retained")
	}
	if _, ok := cache.Load(ep, "query-51", "", DiscoveryPageLimit); !ok {
		t.Fatal("newest non-empty query was evicted")
	}
}

func TestDiscoveryCacheRejectsSymlinkNamespaceWithoutWritingOutside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "discovery")); err != nil {
		t.Fatal(err)
	}
	cache := NewDiscoveryCache(root)
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	outcome := DiscoveryOutcome{Endpoint: ep, ObservedAt: time.Now(), Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "review", 1)}}
	if err := cache.Store(outcome, "private query", "", DiscoveryPageLimit); err == nil {
		t.Fatal("Store succeeded through symlink namespace")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("outside directory changed: %v", entries)
	}
}

func TestDiscoverRetainsStaleEndpointAfterPartialFailure(t *testing.T) {
	good := searchServer(t, "good", false, canonicalSearchItem("registry-good", "skill", "healthy", 1))
	defer good.Close()
	bad := searchServer(t, "bad", true)
	defer bad.Close()
	goodEP := config.MarketplaceEntry{URL: good.URL, InstanceID: "mp-good", Alias: "good"}
	badEP := config.MarketplaceEntry{URL: bad.URL, InstanceID: "mp-bad", Alias: "bad"}
	cache := NewDiscoveryCache(t.TempDir())
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	stale := DiscoveryOutcome{Endpoint: badEP, Items: []mpapi.ResourceListItem{canonicalSearchItem("registry-bad", "skill", "cached", 1)}, Total: 1, ObservedAt: now.Add(-6 * time.Minute)}
	if err := cache.Store(stale, "", "", DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{goodEP, badEP}}}

	outcomes, err := Discover(context.Background(), ec, "", DiscoveryOptions{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes = %d, want 2", len(outcomes))
	}
	if outcomes[0].Endpoint.Alias != "bad" || outcomes[0].CacheState != DiscoveryCacheStale || outcomes[0].Err == nil || outcomes[0].Items[0].Name != "cached" {
		t.Fatalf("bad outcome = %#v", outcomes[0])
	}
	if outcomes[1].Endpoint.Alias != "good" || outcomes[1].Err != nil || outcomes[1].Items[0].Name != "healthy" {
		t.Fatalf("good outcome = %#v", outcomes[1])
	}
}
