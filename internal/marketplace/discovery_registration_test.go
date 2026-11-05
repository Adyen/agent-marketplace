package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

type discoveryRegistrationFixture struct {
	url      string
	requests atomic.Int32
	metadata atomic.Int32
	searches atomic.Int32
}

func newDiscoveryRegistrationFixture(t *testing.T, changeMetadata func(*mpapi.WellKnown), oversized bool) *discoveryRegistrationFixture {
	t.Helper()
	f := &discoveryRegistrationFixture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/catalog/.well-known/agent-marketplace.json":
			f.metadata.Add(1)
			// Public canonical metadata must not retarget the configured transport URL.
			wk := mpapi.DefaultWellKnown("deployed-discovery-instance", "https://public.example/marketplace")
			if changeMetadata != nil {
				changeMetadata(&wk)
			}
			_ = json.NewEncoder(w).Encode(wk)
		case "/catalog/api/v1/search":
			f.searches.Add(1)
			if r.URL.Query().Get("limit") != "50" || r.URL.Query().Get("q") != "review code" {
				t.Errorf("unbounded or unnormalized discovery query: %s", r.URL.RawQuery)
			}
			item := mpapi.ResourceListItem{RegistryID: "registry-id", RegistrySlug: "tools", Kind: "skill", Name: "review", Description: "Review code", Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: "sha256:" + strings.Repeat("a", 64), Availability: mpapi.AvailabilityAvailable}
			items := []mpapi.ResourceListItem{item}
			if oversized {
				items = make([]mpapi.ResourceListItem, 51)
				for i := range items {
					items[i] = item
				}
			}
			_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: len(items), Limit: 50})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	f.url = server.URL + "/catalog"
	return f
}

func TestDiscoveryRegistrationLiveBindsOnlyVerifiedIdentity(t *testing.T) {
	f := newDiscoveryRegistrationFixture(t, nil, false)
	authoredURL := "HTTP" + strings.TrimPrefix(f.url, "http") + "/"
	ec := sourceOnlyRegistration(t, f.url)
	ec.Merged.Marketplaces[0].URL = authoredURL
	cache := NewDiscoveryCache(t.TempDir())
	// A blank-ID page must not suppress identity verification or provide results.
	blank := ec.Merged.Marketplaces[0]
	blank.URL = f.url
	writeDiscoveryRecord(t, cache, blank, "review code", time.Now())
	outcomes, err := Discover(context.Background(), ec, "  review   code  ", DiscoveryOptions{Cache: cache, Limit: 500})
	if err != nil || len(outcomes) != 1 || outcomes[0].Err != nil {
		t.Fatalf("live source-only discovery = %#v, error = %v", outcomes, err)
	}
	out := outcomes[0]
	if out.Endpoint.URL != f.url || out.Endpoint.InstanceID != "deployed-discovery-instance" || out.Endpoint.Alias != "public" || len(out.Endpoint.Resources) != 0 || len(out.Items) != 1 || out.Items[0].Name != "review" || out.CacheState != DiscoveryCacheMiss {
		t.Fatalf("discovery did not retain the verified endpoint and page: %#v", out)
	}
	if f.metadata.Load() != 1 || f.searches.Load() != 1 || f.requests.Load() != 2 {
		t.Fatalf("requests = %d metadata / %d search / %d total, want 1 / 1 / 2", f.metadata.Load(), f.searches.Load(), f.requests.Load())
	}
	if ec.Merged.Marketplaces[0].InstanceID != "" || ec.Merged.Marketplaces[0].URL != authoredURL || len(ec.Merged.Marketplaces[0].Resources) != 0 {
		t.Fatalf("live discovery changed authored configuration: %#v", ec.Merged.Marketplaces)
	}
	if cached, ok := cache.Load(out.Endpoint, "review code", "", 50); !ok || len(cached.Items) != 1 || cached.Items[0].Name != "review" {
		t.Fatalf("verified identity was not used as the cache key: %#v, hit = %t", cached, ok)
	}
	// Exact identity is sufficient for the existing no-network cache path.
	ec.Merged.Marketplaces = []config.MarketplaceEntry{out.Endpoint}
	cached, needsNetwork, err := CachedDiscovery(ec, "review code", DiscoveryOptions{Cache: cache})
	if err != nil || needsNetwork || len(cached) != 1 || cached[0].Endpoint.InstanceID != "deployed-discovery-instance" || f.requests.Load() != 2 {
		t.Fatalf("verified offline control = %#v, needs network = %t, error = %v", cached, needsNetwork, err)
	}
}

func TestDiscoveryRegistrationExactIdentityNeverRebound(t *testing.T) {
	for _, tc := range []struct {
		name     string
		id       string
		pins     bool
		metadata int32
		valid    bool
	}{
		{name: "verified exact pin", id: "deployed-discovery-instance", pins: true, metadata: 1, valid: true},
		{name: "wrong pin identity", id: "other-instance", pins: true, metadata: 1},
		{name: "explicit identity without pins", id: "other-instance", metadata: 1},
		{name: "pin without identity", pins: true},
		{name: "whitespace is not source-only", id: " \t"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDiscoveryRegistrationFixture(t, nil, false)
			entry := config.MarketplaceEntry{URL: f.url, Alias: "public", InstanceID: tc.id}
			if tc.pins {
				entry.Resources = []config.MarketplaceResourcePin{{RegistryID: "registry-id", RegistrySlug: "tools", Kind: "skill", Name: "review", Revision: "sha256:" + strings.Repeat("a", 64), Digest: "sha256:" + strings.Repeat("a", 64)}}
			}
			ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}}
			outcomes, err := Discover(context.Background(), ec, "review code", DiscoveryOptions{Cache: NewDiscoveryCache(t.TempDir())})
			if tc.valid {
				if err != nil || len(outcomes) != 1 || outcomes[0].Err != nil || len(outcomes[0].Items) != 1 || outcomes[0].Endpoint.InstanceID != tc.id || f.searches.Load() != 1 {
					t.Fatalf("valid exact-pin control = %#v, %v", outcomes, err)
				}
			} else {
				if err == nil && (len(outcomes) != 1 || outcomes[0].Err == nil || len(outcomes[0].Items) != 0) {
					t.Fatalf("invalid identity adopted live metadata: %#v, %v", outcomes, err)
				}
				if f.searches.Load() != 0 {
					t.Fatal("invalid identity reached resource search")
				}
			}
			if f.metadata.Load() != tc.metadata || ec.Merged.Marketplaces[0].InstanceID != tc.id {
				t.Fatalf("identity verification changed: metadata = %d, entry = %#v", f.metadata.Load(), ec.Merged.Marketplaces[0])
			}
		})
	}
}

func TestDiscoveryRegistrationRejectsInvalidMetadataAndOversizedPage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		metadata  func(*mpapi.WellKnown)
		oversized bool
		searches  int32
	}{
		{name: "missing identity", metadata: func(wk *mpapi.WellKnown) { wk.InstanceID = "" }},
		{name: "unsafe identity", metadata: func(wk *mpapi.WellKnown) { wk.InstanceID = "bad/id" }},
		{name: "missing capabilities", metadata: func(wk *mpapi.WellKnown) { wk.Capabilities = mpapi.Capabilities{} }},
		{name: "invalid canonical URL", metadata: func(wk *mpapi.WellKnown) { wk.CanonicalURL = "https://public.example?token=secret" }},
		{name: "oversized page", oversized: true, searches: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDiscoveryRegistrationFixture(t, tc.metadata, tc.oversized)
			ec := sourceOnlyRegistration(t, f.url)
			cache := NewDiscoveryCache(t.TempDir())
			outcomes, err := Discover(context.Background(), ec, "review code", DiscoveryOptions{Cache: cache})
			if err != nil || len(outcomes) != 1 || outcomes[0].Err == nil || len(outcomes[0].Items) != 0 || outcomes[0].Endpoint.InstanceID != "" {
				t.Fatalf("invalid metadata/page became a discovery result: %#v, %v", outcomes, err)
			}
			if f.metadata.Load() != 1 || f.searches.Load() != tc.searches || f.requests.Load() != 1+tc.searches {
				t.Fatalf("metadata failure was bypassed or retried: metadata = %d, searches = %d, total = %d", f.metadata.Load(), f.searches.Load(), f.requests.Load())
			}
			if _, err := os.Stat(cache.root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed discovery published cached metadata: %v", err)
			}
		})
	}
}

func TestDiscoveryRegistrationDeclarationPolicyBeforeMetadata(t *testing.T) {
	f := newDiscoveryRegistrationFixture(t, nil, false)
	for _, tc := range []struct {
		name    string
		entries []config.MarketplaceEntry
		allowed []string
		blocked bool
	}{
		{name: "deny all", entries: []config.MarketplaceEntry{{URL: f.url, Alias: "public"}}, allowed: []string{}, blocked: true},
		{name: "invalid allowlist", entries: []config.MarketplaceEntry{{URL: f.url, Alias: "public"}}, allowed: []string{"["}},
		{name: "unsafe URL", entries: []config.MarketplaceEntry{{URL: f.url + "?token=secret", Alias: "public"}}},
		{name: "reserved alias", entries: []config.MarketplaceEntry{{URL: f.url, Alias: "all"}}},
		{name: "unsafe alias", entries: []config.MarketplaceEntry{{URL: f.url, Alias: "public\n"}}},
		{name: "conflicting alias", entries: []config.MarketplaceEntry{{URL: f.url, Alias: "public"}, {URL: "https://other.example", Alias: "public"}}},
		{name: "alias hides pinned identity", entries: []config.MarketplaceEntry{{URL: f.url, Alias: "public", Source: config.UserPersonal}, {URL: f.url, Alias: "public", InstanceID: "other-instance", Source: config.RepoCommitted}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: tc.entries}, AllowedMarketplaces: tc.allowed}
			outcomes, err := Discover(context.Background(), ec, "review code", DiscoveryOptions{Cache: NewDiscoveryCache(t.TempDir())})
			if err == nil || len(outcomes) != 0 || f.requests.Load() != 0 {
				t.Fatalf("invalid declaration reached live metadata: %#v, %v, requests = %d", outcomes, err, f.requests.Load())
			}
			if tc.blocked {
				var blocked *config.MarketplaceURLBlockedError
				if !errors.As(err, &blocked) {
					t.Fatalf("policy error = %v, want MarketplaceURLBlockedError", err)
				}
			}
		})
	}
	outcomes, err := Discover(context.Background(), sourceOnlyRegistration(t, f.url), "review code", DiscoveryOptions{Cache: NewDiscoveryCache(t.TempDir())})
	if err != nil || len(outcomes) != 1 || outcomes[0].Err != nil || f.metadata.Load() != 1 || f.searches.Load() != 1 {
		t.Fatalf("valid declaration did not reach the served metadata: %#v, %v", outcomes, err)
	}
}

func TestDiscoveryRegistrationOfflineRejectsBlankAndArbitraryNamespaces(t *testing.T) {
	f := newDiscoveryRegistrationFixture(t, nil, false)
	cache := NewDiscoveryCache(t.TempDir())
	entry := config.MarketplaceEntry{URL: f.url, Alias: "public"}
	for _, id := range []string{"", " \t", "deployed-discovery-instance", "other-instance"} {
		entry.InstanceID = id
		writeDiscoveryRecord(t, cache, entry, "review code", time.Now())
	}
	for _, id := range []string{"", " \t"} {
		entry.InstanceID = id
		if out, hit := cache.Load(entry, "review code", "", 50); hit {
			t.Fatalf("blank identity accepted directly from cache: %#v", out)
		}
		ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}}
		outcomes, _, err := CachedDiscovery(ec, "review code", DiscoveryOptions{Cache: cache})
		if err == nil || len(outcomes) != 0 || f.requests.Load() != 0 {
			t.Fatalf("offline declaration adopted a namespace or fetched identity: %#v, %v, requests = %d", outcomes, err, f.requests.Load())
		}
	}
	entry.InstanceID = "deployed-discovery-instance"
	outcomes, needsNetwork, err := CachedDiscovery(config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}}, "review code", DiscoveryOptions{Cache: cache})
	if err != nil || needsNetwork || len(outcomes) != 1 || outcomes[0].Endpoint.InstanceID != "deployed-discovery-instance" || f.requests.Load() != 0 {
		t.Fatalf("non-vacuous exact offline control = %#v, needs network = %t, error = %v", outcomes, needsNetwork, err)
	}
}
