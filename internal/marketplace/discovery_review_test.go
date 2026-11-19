package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func TestDiscoverOverlappingPagesRemainPartialAndUncached(t *testing.T) {
	for _, mode := range []string{"browse", "kind"} {
		t.Run(mode, func(t *testing.T) {
			items := make([]mpapi.ResourceListItem, DiscoveryPageLimit)
			for i := range items {
				items[i] = canonicalSearchItem("registry", "mcpserver", fmt.Sprintf("item-%02d", i), 0)
			}
			var requests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/.well-known/agent-marketplace.json" {
					writeTestWellKnown(w, "overlapping-"+mode)
					return
				}
				requests++
				offset := 0
				_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
				if mode == "kind" {
					if r.URL.Query().Get("kind") == "" {
						_ = json.NewEncoder(w).Encode(SearchResponse{Items: []mpapi.ResourceListItem{}, Total: 0, Offset: offset, Limit: DiscoveryPageLimit})
						return
					}
					if offset == 0 {
						_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: DiscoveryPageLimit + 1, Offset: offset, Limit: DiscoveryPageLimit})
						return
					}
				} else if offset == 0 {
					_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: DiscoveryPageLimit + 1, Offset: offset, Limit: DiscoveryPageLimit})
					return
				}
				_ = json.NewEncoder(w).Encode(SearchResponse{Items: []mpapi.ResourceListItem{items[len(items)-1]}, Total: DiscoveryPageLimit + 1, Offset: offset, Limit: DiscoveryPageLimit})
			}))
			defer server.Close()

			endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: "overlapping-" + mode}
			cache := NewDiscoveryCache(t.TempDir())
			ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}}
			query := ""
			opts := DiscoveryOptions{Cache: cache}
			if mode == "kind" {
				query = "needle"
				opts.IncludeKind = "mcpserver"
			}
			var outcomes []DiscoveryOutcome
			if mode == "browse" {
				updates, err := DiscoverUpdates(context.Background(), ec, query, opts)
				if err != nil {
					t.Fatal(err)
				}
				for snapshot := range updates {
					if len(snapshot) != 1 {
						t.Fatalf("browse snapshot = %#v, want one endpoint", snapshot)
					}
					outcome := snapshot[0]
					if len(outcome.Items) < outcome.Total && !outcome.Truncated {
						t.Errorf("browse snapshot claims completion with %d items of %d", len(outcome.Items), outcome.Total)
					}
					outcomes = snapshot
				}
			} else {
				var err error
				outcomes, err = Discover(context.Background(), ec, query, opts)
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(outcomes) != 1 {
				t.Fatalf("discovery = %#v", outcomes)
			}
			if outcomes[0].Err == nil || !outcomes[0].Truncated || len(outcomes[0].Items) != DiscoveryPageLimit {
				t.Fatalf("overlapping scan = %d items, total %d, truncated %t, err %v; want partial inventory and error", len(outcomes[0].Items), outcomes[0].Total, outcomes[0].Truncated, outcomes[0].Err)
			}
			if requests < 2 {
				t.Fatalf("requests = %d, want the overlapping later page to be exercised", requests)
			}
			records, err := filepath.Glob(filepath.Join(cache.root, "*", "*.json"))
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != 0 {
				t.Fatalf("incomplete catalog was cached: %v", records)
			}
		})
	}
}

func TestDiscoverCompleteBrowseDoesNotReplaceLegacySinglePageCache(t *testing.T) {
	items := make([]mpapi.ResourceListItem, DiscoveryPageLimit+12)
	for i := range items {
		items[i] = canonicalSearchItem("registry", "skill", fmt.Sprintf("resource-%02d", i), 0)
	}
	var searches int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "legacy-browse")
			return
		}
		searches++
		offset, limit := 0, DiscoveryPageLimit
		_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
		_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: items[offset:min(offset+limit, len(items))], Total: len(items), Offset: offset, Limit: limit})
	}))
	defer server.Close()

	endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: "legacy-browse"}
	cache := NewDiscoveryCache(t.TempDir())
	legacy := DiscoveryOutcome{
		Endpoint: endpoint, Items: slices.Clone(items[:DiscoveryPageLimit]), Total: len(items),
		Truncated: true, ObservedAt: time.Now(),
	}
	if err := cache.Store(legacy, "", "", DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	legacyPath := cache.path(endpoint, "", "", "", DiscoveryPageLimit)
	before, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}

	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}}
	first, err := Discover(context.Background(), ec, "", DiscoveryOptions{Cache: cache})
	if err != nil || len(first) != 1 || len(first[0].Items) != len(items) || first[0].Err != nil {
		t.Fatalf("complete browse = %#v, err=%v", first, err)
	}
	after, err := os.ReadFile(legacyPath)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("legacy single-page cache changed: equal=%t err=%v", bytes.Equal(after, before), err)
	}
	loadedLegacy, ok := cache.Load(endpoint, "", "", DiscoveryPageLimit)
	if !ok || len(loadedLegacy.Items) != DiscoveryPageLimit || !loadedLegacy.Truncated || loadedLegacy.Items[0].Name != "resource-00" {
		t.Fatalf("legacy cache read = %#v, hit=%t", loadedLegacy, ok)
	}
	searchesAfterBrowse := searches
	second, err := Discover(context.Background(), ec, "", DiscoveryOptions{Cache: cache})
	if err != nil || len(second) != 1 || second[0].CacheState != DiscoveryCacheFresh || len(second[0].Items) != len(items) {
		t.Fatalf("full-catalog cache = %#v, err=%v", second, err)
	}
	if searches != searchesAfterBrowse {
		t.Fatalf("fresh full-catalog cache made %d extra requests", searches-searchesAfterBrowse)
	}
}

func TestDiscoverFreshBrowseCacheOverlaysReceivedRowsUntilLaterPageFails(t *testing.T) {
	cachedItems := make([]mpapi.ResourceListItem, DiscoveryPageLimit)
	for i := range cachedItems {
		cachedItems[i] = canonicalSearchItem("registry", "skill", fmt.Sprintf("cached-%02d", i), 0)
		cachedItems[i].Description = "cached"
	}
	liveItems := make([]mpapi.ResourceListItem, DiscoveryPageLimit+10)
	liveItems[0] = canonicalSearchItem("registry", "skill", "new-first", 0)
	liveItems[0].Description = "received"
	copy(liveItems[1:DiscoveryPageLimit], cachedItems[:DiscoveryPageLimit-1])
	for i := 1; i < DiscoveryPageLimit; i++ {
		liveItems[i].Description = "received"
	}
	for i := DiscoveryPageLimit; i < len(liveItems); i++ {
		liveItems[i] = canonicalSearchItem("registry", "skill", fmt.Sprintf("live-%02d", i), 0)
	}
	laterPageStarted := make(chan struct{}, 1)
	releaseLaterPage := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "fresh-overlay")
			return
		}
		offset, limit := 0, DiscoveryPageLimit
		_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
		_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		if offset > 0 {
			select {
			case laterPageStarted <- struct{}{}:
			default:
			}
			select {
			case <-releaseLaterPage:
			case <-r.Context().Done():
				return
			}
			http.Error(w, "later page unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: liveItems[offset:min(offset+limit, len(liveItems))], Total: len(liveItems), Offset: offset, Limit: limit})
	}))
	defer server.Close()
	defer func() {
		select {
		case <-releaseLaterPage:
		default:
			close(releaseLaterPage)
		}
	}()

	endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: "fresh-overlay"}
	cache := NewDiscoveryCache(t.TempDir())
	if err := cache.Store(DiscoveryOutcome{
		Endpoint: endpoint, Items: cachedItems, Total: DiscoveryPageLimit + 10,
		Truncated: true, ObservedAt: time.Now(),
	}, "", "", DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	cachePath := cache.path(endpoint, "", "", "", DiscoveryPageLimit)
	before, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}}
	updates, err := DiscoverUpdates(context.Background(), ec, "", DiscoveryOptions{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}

	var overlay DiscoveryOutcome
	for len(overlay.Items) == 0 || len(overlay.Items) == DiscoveryPageLimit {
		select {
		case snapshot, ok := <-updates:
			if !ok || len(snapshot) != 1 {
				t.Fatalf("browse stream ended before live page overlay: %#v", snapshot)
			}
			overlay = snapshot[0]
		case <-time.After(3 * time.Second):
			t.Fatal("live browse page did not overlay the cached catalog")
		}
	}
	select {
	case <-laterPageStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("later page was not held after publishing the interim overlay")
	}
	if len(overlay.Items) != DiscoveryPageLimit+1 || overlay.Items[0].Name != "new-first" || overlay.Items[1].Description != "received" || overlay.Items[DiscoveryPageLimit].Name != cachedItems[DiscoveryPageLimit-1].Name {
		t.Fatalf("interim cache overlay lost or misordered rows: %d items, first=%#v, tail=%#v", len(overlay.Items), overlay.Items[0], overlay.Items[len(overlay.Items)-1])
	}

	close(releaseLaterPage)
	var failed DiscoveryOutcome
	for snapshot := range updates {
		if len(snapshot) == 1 && snapshot[0].Err != nil {
			failed = snapshot[0]
		}
	}
	if failed.Err == nil || len(failed.Items) != DiscoveryPageLimit+1 || failed.Items[1].Description != "received" || failed.Items[DiscoveryPageLimit].Name != cachedItems[DiscoveryPageLimit-1].Name {
		t.Fatalf("failed browse lost fresh cached-only/live rows: %#v", failed)
	}
	after, err := os.ReadFile(cachePath)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("failed overlay replaced the cache: equal=%t err=%v", bytes.Equal(after, before), err)
	}
	if _, err := os.Stat(cache.catalogPath(endpoint, DiscoveryPageLimit)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed overlay wrote a catalog record: %v", err)
	}
}
