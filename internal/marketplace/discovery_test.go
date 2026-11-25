package marketplace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func TestDiscoverFreshCacheAvoidsSecondNetworkRequest(t *testing.T) {
	var searches atomic.Int32
	items := make([]mpapi.ResourceListItem, 72)
	for i := range items {
		items[i] = canonicalSearchItem("registry-a", "skill", fmt.Sprintf("review-%02d", i), float64(len(items)-i))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp-alpha")
		case "/api/v1/search":
			searches.Add(1)
			if got := r.URL.Query().Get("limit"); got != "50" {
				t.Errorf("limit = %q, want 50", got)
			}
			offset, limit := 0, DiscoveryPageLimit
			_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
			_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
			end := min(offset+limit, len(items))
			_ = json.NewEncoder(w).Encode(SearchResponse{Items: items[offset:end], Total: len(items), Offset: offset, Limit: limit})
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
	if got := searches.Load(); got != 2 {
		t.Fatalf("search requests = %d, want two pages in the complete browse", got)
	}
	if len(first) != 1 || len(first[0].Items) != len(items) || first[0].Total != len(items) || first[0].Truncated {
		t.Fatalf("first outcome = %#v", first)
	}
	if len(second) != 1 || second[0].CacheState != DiscoveryCacheFresh || len(second[0].Items) != len(items) || second[0].Items[len(items)-1].Name != "review-71" {
		t.Fatalf("second outcome = %#v", second)
	}
}

func TestDiscoverBrowsePaginatesAndPublishesCumulativeSnapshots(t *testing.T) {
	items := make([]mpapi.ResourceListItem, DiscoveryPageLimit+13)
	for i := range items {
		items[i] = canonicalSearchItem("registry", "skill", fmt.Sprintf("resource-%02d", i), 0)
	}
	var mu sync.Mutex
	offsets := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "paged-browse")
			return
		}
		offset, limit := 0, DiscoveryPageLimit
		_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
		_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		mu.Lock()
		offsets = append(offsets, offset)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: items[offset:min(offset+limit, len(items))], Total: len(items), Offset: offset, Limit: limit})
	}))
	defer server.Close()
	endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: "paged-browse", Alias: "paged"}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}}
	cache := NewDiscoveryCache(t.TempDir())
	updates, err := DiscoverUpdates(context.Background(), ec, "", DiscoveryOptions{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	var snapshots [][]DiscoveryOutcome
	for update := range updates {
		snapshots = append(snapshots, update)
	}
	if len(snapshots) < 3 {
		t.Fatalf("snapshots = %d, want a cumulative snapshot after both pages and a completed result", len(snapshots))
	}
	firstPage := snapshots[0][0]
	if len(firstPage.Items) != DiscoveryPageLimit || firstPage.Total != len(items) || !firstPage.Truncated {
		t.Fatalf("first-page snapshot = %d items, total %d, truncated %t", len(firstPage.Items), firstPage.Total, firstPage.Truncated)
	}
	finalPage := snapshots[len(snapshots)-1][0]
	if len(finalPage.Items) != len(items) || finalPage.Truncated || finalPage.Err != nil {
		t.Fatalf("completed snapshot = %d items, truncated %t, error %v", len(finalPage.Items), finalPage.Truncated, finalPage.Err)
	}
	mu.Lock()
	gotOffsets := slices.Clone(offsets)
	mu.Unlock()
	if !slices.Equal(gotOffsets, []int{0, DiscoveryPageLimit}) {
		t.Fatalf("page offsets = %v, want [0 %d]", gotOffsets, DiscoveryPageLimit)
	}
	cached, ok := cache.loadBrowse(endpoint, DiscoveryPageLimit)
	if !ok || len(cached.Items) != len(items) || cached.Total != len(items) || cached.Truncated {
		t.Fatalf("complete browse cache = %d items, total %d, truncated %t, hit %t", len(cached.Items), cached.Total, cached.Truncated, ok)
	}
}

func TestDiscoverBrowseEmptyEndpointMakesOneSearchRequest(t *testing.T) {
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "empty-browse")
			return
		}
		searches.Add(1)
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: []mpapi.ResourceListItem{}, Total: 0, Offset: 0, Limit: DiscoveryPageLimit})
	}))
	defer server.Close()
	ep := config.MarketplaceEntry{URL: server.URL, InstanceID: "empty-browse"}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{ep}}}
	outcomes, err := Discover(context.Background(), ec, "", DiscoveryOptions{Cache: NewDiscoveryCache(t.TempDir())})
	if err != nil || len(outcomes) != 1 || len(outcomes[0].Items) != 0 || outcomes[0].Err != nil {
		t.Fatalf("empty browse = %#v, err=%v", outcomes, err)
	}
	if got := searches.Load(); got != 1 {
		t.Fatalf("empty endpoint search requests = %d, want one", got)
	}
}

func TestDiscoverBrowseEndpointsProgressIndependently(t *testing.T) {
	laterPageStarted := make(chan struct{}, 1)
	releaseLaterPage := make(chan struct{}, 1)
	heldItems := make([]mpapi.ResourceListItem, DiscoveryPageLimit+1)
	for i := range heldItems {
		heldItems[i] = canonicalSearchItem("held-registry", "skill", fmt.Sprintf("held-%02d", i), 0)
	}
	held := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "held-endpoint")
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
		}
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: heldItems[offset:min(offset+limit, len(heldItems))], Total: len(heldItems), Offset: offset, Limit: limit})
	}))
	defer held.Close()
	defer func() {
		select {
		case releaseLaterPage <- struct{}{}:
		default:
		}
	}()
	completed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "completed-endpoint")
			return
		}
		items := []mpapi.ResourceListItem{canonicalSearchItem("complete-registry", "skill", "complete", 0)}
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: len(items), Offset: 0, Limit: DiscoveryPageLimit})
	}))
	defer completed.Close()
	endpoints := []config.MarketplaceEntry{
		{URL: held.URL, InstanceID: "held-endpoint"},
		{URL: completed.URL, InstanceID: "completed-endpoint"},
	}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: endpoints}}
	updates, err := DiscoverUpdates(context.Background(), ec, "", DiscoveryOptions{Cache: NewDiscoveryCache(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-laterPageStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("held endpoint did not start its later page")
	}
	gotHeldPage, gotCompletedEndpoint := false, false
	deadline := time.After(2 * time.Second)
	for !gotHeldPage || !gotCompletedEndpoint {
		select {
		case snapshot, ok := <-updates:
			if !ok {
				t.Fatal("discovery stream closed before independent endpoint progress arrived")
			}
			for _, outcome := range snapshot {
				switch outcome.Endpoint.InstanceID {
				case "held-endpoint":
					gotHeldPage = len(outcome.Items) == DiscoveryPageLimit && outcome.Truncated && outcome.Err == nil
				case "completed-endpoint":
					gotCompletedEndpoint = len(outcome.Items) == 1 && !outcome.Truncated && outcome.Err == nil
				}
			}
		case <-deadline:
			t.Fatalf("progress while one endpoint was held = held page %t, completed endpoint %t", gotHeldPage, gotCompletedEndpoint)
		}
	}
	releaseLaterPage <- struct{}{}
	for range updates {
	}
}

func TestDiscoverFreshTruncatedBrowseCacheStillFetchesMissingPages(t *testing.T) {
	var offsets []int
	items := make([]mpapi.ResourceListItem, DiscoveryPageLimit+3)
	for i := range items {
		items[i] = canonicalSearchItem("registry", "skill", fmt.Sprintf("resource-%02d", i), 0)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "truncated-cache")
			return
		}
		offset, limit := 0, DiscoveryPageLimit
		_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
		_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		offsets = append(offsets, offset)
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: items[offset:min(offset+limit, len(items))], Total: len(items), Offset: offset, Limit: limit})
	}))
	defer server.Close()
	ep := config.MarketplaceEntry{URL: server.URL, InstanceID: "truncated-cache"}
	cache := NewDiscoveryCache(t.TempDir())
	page := items[:DiscoveryPageLimit]
	if err := cache.Store(DiscoveryOutcome{Endpoint: ep, Items: page, Total: len(items), Truncated: true, ObservedAt: time.Now()}, "", "", DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{ep}}}
	outcomes, err := Discover(context.Background(), ec, "", DiscoveryOptions{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || len(outcomes[0].Items) != len(items) || outcomes[0].Truncated {
		t.Fatalf("browse after truncated cache = %#v", outcomes)
	}
	if !slices.Equal(offsets, []int{0, DiscoveryPageLimit}) {
		t.Fatalf("refreshed offsets = %v, want [0 %d]", offsets, DiscoveryPageLimit)
	}
}

func TestDiscoverBrowseLaterPageFailureKeepsItemsWithoutCachingPartialCatalog(t *testing.T) {
	var searches atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "partial-browse")
			return
		}

		searches.Add(1)

		offset, limit := 0, DiscoveryPageLimit
		_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
		_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		if offset > 0 {
			http.Error(w, "later page unavailable", http.StatusServiceUnavailable)
			return
		}
		items := make([]mpapi.ResourceListItem, DiscoveryPageLimit)
		for i := range items {
			items[i] = canonicalSearchItem("registry", "skill", fmt.Sprintf("resource-%02d", i), 0)
		}
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: DiscoveryPageLimit + 4, Offset: offset, Limit: limit})
	}))
	defer server.Close()
	ep := config.MarketplaceEntry{URL: server.URL, InstanceID: "partial-browse"}
	cache := NewDiscoveryCache(t.TempDir())
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{ep}}}
	outcomes, err := Discover(context.Background(), ec, "", DiscoveryOptions{Cache: cache})
	if err != nil || len(outcomes) != 1 {
		t.Fatalf("partial browse = %#v, err=%v", outcomes, err)
	}
	if len(outcomes[0].Items) != DiscoveryPageLimit || outcomes[0].Err == nil || !outcomes[0].Truncated {
		t.Fatalf("partial browse outcome = %d items, truncated %t, error %v", len(outcomes[0].Items), outcomes[0].Truncated, outcomes[0].Err)
	}
	if _, err := os.Stat(cache.path(ep, "", "", "", DiscoveryPageLimit)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial catalog was cached: %v", err)
	}
	if _, err := os.Stat(cache.catalogPath(ep, DiscoveryPageLimit)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial catalog record was cached: %v", err)
	}

	again, err := Discover(context.Background(), ec, "", DiscoveryOptions{Cache: cache})
	if err != nil || len(again) != 1 || again[0].Err == nil || len(again[0].Items) != DiscoveryPageLimit {
		t.Fatalf("new load after partial browse = %#v, err=%v", again, err)
	}
	if searches.Load() != 4 {
		t.Fatalf("partial browse made %d page requests across two loads, want 4", searches.Load())
	}
}

func TestDiscoverReturnsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "https://market.example", InstanceID: "market",
	}}}}
	outcomes, err := Discover(ctx, ec, "needle", DiscoveryOptions{Cache: NewDiscoveryCache(t.TempDir())})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("discovery error = %v, want caller cancellation; outcomes=%#v", err, outcomes)
	}
}

func TestDiscoverBrowseStaleCacheMergesReceivedRowsAfterLaterPageFailure(t *testing.T) {
	staleItems := make([]mpapi.ResourceListItem, DiscoveryPageLimit+10)
	for i := range staleItems {
		staleItems[i] = canonicalSearchItem("registry", "skill", fmt.Sprintf("resource-%02d", i), 0)
		staleItems[i].Description = "cached"
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "stale-partial-browse")
			return
		}
		offset, limit := 0, DiscoveryPageLimit
		_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
		_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		if offset >= DiscoveryPageLimit {
			http.Error(w, "later page unavailable", http.StatusServiceUnavailable)
			return
		}
		items := make([]mpapi.ResourceListItem, 0, min(limit, len(staleItems)-offset))
		for i := offset; i < min(offset+limit, len(staleItems)); i++ {
			item := staleItems[i]
			item.Description = "received"
			items = append(items, item)
		}
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: len(staleItems), Offset: offset, Limit: limit})
	}))
	defer server.Close()

	ep := config.MarketplaceEntry{URL: server.URL, InstanceID: "stale-partial-browse"}
	cache := NewDiscoveryCache(t.TempDir())
	if err := cache.storeBrowse(DiscoveryOutcome{
		Endpoint:   ep,
		Items:      staleItems,
		Total:      len(staleItems),
		ObservedAt: time.Now().Add(-time.Hour),
	}, DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	cachePath := cache.catalogPath(ep, DiscoveryPageLimit)
	before, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	effective := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{ep}}}

	outcomes, err := Discover(context.Background(), effective, "", DiscoveryOptions{Cache: cache})
	if err != nil || len(outcomes) != 1 {
		t.Fatalf("partial browse = %#v, err=%v", outcomes, err)
	}
	out := outcomes[0]
	if out.Total != len(staleItems) || len(out.Items) != len(staleItems) {
		t.Fatalf("partial stale browse has total/items %d/%d, want %d/%d", out.Total, len(out.Items), len(staleItems), len(staleItems))
	}
	for i, item := range out.Items {
		wantDescription := "cached"
		if i < DiscoveryPageLimit {
			wantDescription = "received"
		}
		if item.Name != fmt.Sprintf("resource-%02d", i) || item.Description != wantDescription {
			t.Fatalf("merged item %d = %#v, want resource-%02d with %q description", i, item, i, wantDescription)
		}
	}
	if out.Err == nil || !out.Truncated || out.CacheState != DiscoveryCacheStale {
		t.Fatalf("partial stale browse state = %q, truncated=%t, error=%v; want stale, truncated, and failed", out.CacheState, out.Truncated, out.Err)
	}
	after, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("partial merged browse replaced the stale cache record")
	}
}

func TestDiscoverStaleBrowseCachePrecedesHeldRefreshAndDoesNotRepeat(t *testing.T) {
	release := make(chan struct{})
	searchRequests := make(chan struct{}, 4)
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "stale-before-refresh")
			return
		}
		searches.Add(1)
		select {
		case searchRequests <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		items := []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "fresh-remote", 1)}
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: 1, Limit: DiscoveryPageLimit})
	}))
	defer server.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()

	ep := config.MarketplaceEntry{URL: server.URL, InstanceID: "stale-before-refresh"}
	cache := NewDiscoveryCache(t.TempDir())
	if err := cache.Store(DiscoveryOutcome{
		Endpoint:   ep,
		Items:      []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "cached-local", 1)},
		Total:      1,
		ObservedAt: time.Now().Add(-time.Hour),
	}, "", "", DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	effective := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{ep}}}
	updates, err := DiscoverUpdates(context.Background(), effective, "", DiscoveryOptions{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case snapshot, ok := <-updates:
		if !ok || len(snapshot) != 1 || snapshot[0].CacheState != DiscoveryCacheStale || len(snapshot[0].Items) != 1 || snapshot[0].Items[0].Name != "cached-local" {
			t.Fatalf("first snapshot = %#v, open=%t; want stale cached rows", snapshot, ok)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stale cached rows were not published while refresh was held")
	}
	select {
	case <-searchRequests:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh request did not start")
	}
	close(release)
	for range updates {
	}
	if got := searches.Load(); got != 1 {
		t.Fatalf("search requests after refresh = %d, want one", got)
	}
	select {
	case <-searchRequests:
		t.Fatal("an additional browse request arrived after refresh completion")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestDiscoverBrowseDeniedEndpointReceivesNoRequests(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	defer server.Close()
	ep := config.MarketplaceEntry{URL: server.URL, InstanceID: "denied-browse"}
	ec := config.EffectiveConfig{
		Merged:              &config.Config{Marketplaces: []config.MarketplaceEntry{ep}},
		AllowedMarketplaces: []string{"^https://allowed\\.example$"},
	}
	cache := NewDiscoveryCache(t.TempDir())
	outcomes, err := Discover(context.Background(), ec, "", DiscoveryOptions{Cache: cache})
	var blocked *config.MarketplaceURLBlockedError
	if !errors.As(err, &blocked) || len(outcomes) != 0 {
		t.Fatalf("denied browse = %#v, err=%v; want MarketplaceURLBlockedError before outcomes", outcomes, err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("denied endpoint received %d requests", got)
	}
}

func TestDiscoveryLogsCountsWithoutRawQuery(t *testing.T) {
	server := searchServer(t, "logging", false, canonicalSearchItem("registry", "skill", "review", 1))
	defer server.Close()
	ep := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp-logging"}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{ep}}}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	query := "private-query-sentinel"
	if _, err := Discover(context.Background(), ec, query, DiscoveryOptions{Cache: NewDiscoveryCache(t.TempDir())}); err != nil {
		t.Fatal(err)
	}
	output := logs.String()
	if strings.Contains(output, query) {
		t.Fatalf("raw query appeared in logs: %s", output)
	}
	for _, field := range []string{"cache_miss=1", "cache_fresh=0", "cache_stale=0"} {
		if !strings.Contains(output, field) {
			t.Fatalf("completion log missing %q: %s", field, output)
		}
	}
}

func TestDiscoveryCacheSchemaOwnsDistinctPath(t *testing.T) {
	cache := NewDiscoveryCache(t.TempDir())
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	query := "review"
	priorSchema := discoveryCacheSchema - 1
	priorKey := sha256.Sum256([]byte(fmt.Sprint(priorSchema) + "\x00" + ep.URL + "\x00" + ep.InstanceID + "\x00" + query + "\x00\x00" + fmt.Sprint(DiscoveryPageLimit)))
	priorPath := filepath.Join(cache.root, discoveryEndpointDirectoryName(ep), hex.EncodeToString(priorKey[:])+".json")
	priorRecord := discoveryCacheRecord{Schema: priorSchema, Endpoint: ep.URL, InstanceID: ep.InstanceID, Query: query, Limit: DiscoveryPageLimit, ObservedAt: time.Now(), Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "prior", 1)}, Total: 1}
	priorData, err := json.Marshal(priorRecord)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(priorPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(priorPath, priorData, 0o600); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(filepath.Dir(priorPath), "unrelated")
	if err := os.WriteFile(unrelated, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome := DiscoveryOutcome{Endpoint: ep, ObservedAt: time.Now(), Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "current", 1)}, Total: 1}
	if err := cache.Store(outcome, query, "", DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	currentKey := sha256.Sum256([]byte(fmt.Sprint(discoveryCacheSchema) + "\x00" + ep.URL + "\x00" + ep.InstanceID + "\x00" + query + "\x00\x00\x00" + fmt.Sprint(DiscoveryPageLimit)))
	currentPath := filepath.Join(cache.root, discoveryEndpointDirectoryName(ep), hex.EncodeToString(currentKey[:])+".json")
	if currentPath == priorPath {
		t.Fatal("schema versions share a cache path")
	}
	if _, err := os.Stat(currentPath); err != nil {
		t.Fatalf("current-schema record missing: %v", err)
	}
	if got, err := os.ReadFile(priorPath); err != nil || !bytes.Equal(got, priorData) {
		t.Fatalf("prior-schema record changed: data=%q err=%v", got, err)
	}
	if got, err := os.ReadFile(unrelated); err != nil || string(got) != "preserve" {
		t.Fatalf("unrelated file changed: data=%q err=%v", got, err)
	}
	loaded, ok := cache.Load(ep, query, "", DiscoveryPageLimit)
	if !ok || len(loaded.Items) != 1 || loaded.Items[0].Name != "current" {
		t.Fatalf("current-schema load = %#v, %t", loaded, ok)
	}
}

func TestDiscoveryCacheRetainsEmptyBrowseAndFiftyRecentQueries(t *testing.T) {
	cache := NewDiscoveryCache(t.TempDir())
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	store := func(query string, observed time.Time) {
		t.Helper()
		outcome := DiscoveryOutcome{Endpoint: ep, ObservedAt: observed, Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", query+"-result", 1)}, Total: 1}
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
	dir := filepath.Dir(cache.path(ep, "", "", "", DiscoveryPageLimit))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	recognized, browse := 0, 0
	for _, entry := range entries {
		record, ok := cache.readRecognizedRecord(dir, entry.Name())
		if !ok {
			continue
		}
		recognized++
		if record.Query == "" {
			browse++
		}
	}
	if recognized != DiscoveryQueryLimit+1 || browse != 1 {
		t.Fatalf("recognized records = %d with %d browse, want %d with 1 browse", recognized, browse, DiscoveryQueryLimit+1)
	}
}

func TestDiscoveryCacheRetentionPreservesFutureRecordWithoutDisplacingRecentQueries(t *testing.T) {
	cache := NewDiscoveryCache(t.TempDir())
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	var recent []string
	for i := 0; i < DiscoveryQueryLimit; i++ {
		recent = append(recent, writeDiscoveryRecord(t, cache, ep, fmt.Sprintf("query-%02d", i), now.Add(-time.Minute+time.Duration(i)*time.Second)))
	}
	futurePath := writeDiscoveryRecord(t, cache, ep, "future", now.Add(time.Hour))
	if _, ok := cache.Load(ep, "future", "", DiscoveryPageLimit); ok {
		t.Fatal("future-dated record loaded")
	}
	dir := filepath.Dir(futurePath)
	if err := cache.retain(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(futurePath); err != nil {
		t.Fatalf("future-dated unknown record changed: %v", err)
	}
	for _, path := range recent {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("recent valid record displaced: %s: %v", path, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	recognized := 0
	for _, entry := range entries {
		if _, ok := cache.readRecognizedRecord(dir, entry.Name()); ok {
			recognized++
		}
	}
	if recognized != DiscoveryQueryLimit {
		t.Fatalf("recognized retention candidates = %d, want %d", recognized, DiscoveryQueryLimit)
	}
}

func TestDiscoveryCacheRetainsOnlyNewestEmptyQueryAcrossKindAndLimit(t *testing.T) {
	cache := NewDiscoveryCache(t.TempDir())
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	variants := []struct {
		kind     string
		limit    int
		observed time.Time
	}{
		{kind: "skill", limit: 10, observed: now.Add(-3 * time.Minute)},
		{kind: "command", limit: 20, observed: now.Add(-time.Minute)},
		{kind: "mcp", limit: 30, observed: now.Add(-2 * time.Minute)},
	}
	for _, variant := range variants {
		outcome := DiscoveryOutcome{Endpoint: ep, ObservedAt: variant.observed, Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", variant.kind, 1)}, Total: 1}
		if err := cache.Store(outcome, "", variant.kind, variant.limit); err != nil {
			t.Fatal(err)
		}
	}
	for _, variant := range variants {
		_, ok := cache.Load(ep, "", variant.kind, variant.limit)
		want := variant.kind == "command"
		if ok != want {
			t.Errorf("Load(empty, %q, %d) = %t, want %t", variant.kind, variant.limit, ok, want)
		}
	}
}

func writeDiscoveryRecord(t *testing.T, cache *DiscoveryCache, ep config.MarketplaceEntry, query string, observed time.Time) string {
	t.Helper()
	record := discoveryCacheRecord{
		Schema: discoveryCacheSchema, Endpoint: ep.URL, InstanceID: ep.InstanceID, Query: query,
		Limit: DiscoveryPageLimit, ObservedAt: observed,
		Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", query+"-result", 1)}, Total: 1,
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	path := cache.path(ep, query, "", "", DiscoveryPageLimit)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoveryCacheStoreDoesNotOpenFIFO(t *testing.T) {
	cache := NewDiscoveryCache(t.TempDir())
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	path := cache.path(ep, "stored", "", "", DiscoveryPageLimit)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(filepath.Dir(path), strings.Repeat("a", 64)+".json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	fifoFD, err := os.OpenFile(fifo, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = fifoFD.Close()
		}
	})
	outcome := DiscoveryOutcome{Endpoint: ep, ObservedAt: time.Now(), Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "stored", 1)}, Total: 1}
	done := make(chan error, 1)
	go func() { done <- cache.Store(outcome, "stored", "", DiscoveryPageLimit) }()
	select {
	case err := <-done:
		closed = true
		_ = fifoFD.Close()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		closed = true
		_ = fifoFD.Close()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		t.Fatal("Store blocked while retaining beside a FIFO")
	}
	if info, err := os.Lstat(fifo); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("FIFO was not preserved: info=%v err=%v", info, err)
	}
}

func TestDiscoveryCacheRetentionPreservesArbitraryNameJSON(t *testing.T) {
	cache := NewDiscoveryCache(t.TempDir())
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	for i := 0; i < DiscoveryQueryLimit; i++ {
		writeDiscoveryRecord(t, cache, ep, fmt.Sprintf("query-%02d", i), now.Add(time.Duration(i)*time.Second))
	}
	dir := filepath.Dir(cache.path(ep, "query-00", "", "", DiscoveryPageLimit))
	arbitrary := filepath.Join(dir, "arbitrary.json")
	record := discoveryCacheRecord{Schema: discoveryCacheSchema, Endpoint: ep.URL, InstanceID: ep.InstanceID, Query: "arbitrary", Limit: DiscoveryPageLimit, ObservedAt: now.Add(-time.Hour), Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "arbitrary", 1)}, Total: 1}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(arbitrary, data, 0o600); err != nil {
		t.Fatal(err)
	}
	newest := writeDiscoveryRecord(t, cache, ep, "query-50", now.Add(time.Minute))
	if err := cache.retain(dir); err != nil {
		t.Fatal(err)
	}
	for _, preserved := range []string{arbitrary, newest} {
		if _, err := os.Lstat(preserved); err != nil {
			t.Fatalf("retention removed %s: %v", preserved, err)
		}
	}
	oldest := cache.path(ep, "query-00", "", "", DiscoveryPageLimit)
	if _, err := os.Lstat(oldest); !os.IsNotExist(err) {
		t.Fatalf("retention did not evict the oldest recognized record: %v", err)
	}
}

func TestDiscoveryCacheOverCapDirectoryFailsClosed(t *testing.T) {
	cache := NewDiscoveryCache(t.TempDir())
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	var oldest string
	for i := 0; i <= DiscoveryQueryLimit; i++ {
		path := writeDiscoveryRecord(t, cache, ep, fmt.Sprintf("query-%03d", i), now.Add(-DiscoveryStaleTTL-time.Minute+time.Duration(i)*time.Second))
		if i == 0 {
			oldest = path
		}
	}
	dir := filepath.Dir(oldest)
	for i := DiscoveryQueryLimit + 1; i <= maxDiscoveryDirEntries; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("unknown-%03d", i)), []byte("preserve"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := cache.retain(dir); err != nil {
		t.Fatal(err)
	}
	if err := cache.maintain([]config.MarketplaceEntry{ep}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(oldest); err != nil {
		t.Fatalf("over-cap scan deleted recognized content: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, fmt.Sprintf("unknown-%03d", maxDiscoveryDirEntries))); err != nil {
		t.Fatalf("over-cap scan deleted unknown content: %v", err)
	}
}

func TestDiscoveryCacheStoreRejectsOverCapEndpointBeforeMutation(t *testing.T) {
	cache := NewDiscoveryCache(t.TempDir())
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	for i := 0; i < DiscoveryQueryLimit; i++ {
		writeDiscoveryRecord(t, cache, ep, fmt.Sprintf("query-%03d", i), now.Add(time.Duration(i)*time.Second))
	}
	dir := filepath.Dir(cache.path(ep, "query-000", "", "", DiscoveryPageLimit))
	for i := DiscoveryQueryLimit; i <= maxDiscoveryDirEntries; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("unknown-%03d", i)), []byte("preserve"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		query := fmt.Sprintf("new-%03d", i)
		outcome := DiscoveryOutcome{Endpoint: ep, ObservedAt: now, Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", query, 1)}, Total: 1}
		if err := cache.Store(outcome, query, "", DiscoveryPageLimit); err == nil {
			t.Fatalf("Store(%q) succeeded in over-cap endpoint directory", query)
		}
		if _, err := os.Lstat(cache.path(ep, query, "", "", DiscoveryPageLimit)); !os.IsNotExist(err) {
			t.Fatalf("Store(%q) created a record: %v", query, err)
		}
	}
	after, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("endpoint entries changed from %d to %d", len(before), len(after))
	}
	recognized := 0
	for _, entry := range after {
		if _, ok := cache.readRecognizedRecord(dir, entry.Name()); ok {
			recognized++
		}
		if strings.HasPrefix(entry.Name(), ".discovery-") {
			t.Fatalf("temporary residue remains: %s", entry.Name())
		}
	}
	if recognized != DiscoveryQueryLimit {
		t.Fatalf("recognized records = %d, want %d", recognized, DiscoveryQueryLimit)
	}
}

func fillDiscoveryEndpointToCount(t *testing.T, destination string, count int) string {
	t.Helper()
	dir := filepath.Dir(destination)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(entries); i < count; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("unknown-%03d", i)), []byte("preserve"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDiscoveryCacheStoreReplacesRecognizedRecordAtCapacity(t *testing.T) {
	cache := NewDiscoveryCache(t.TempDir())
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	path := writeDiscoveryRecord(t, cache, ep, "same", now.Add(-time.Hour))
	dir := fillDiscoveryEndpointToCount(t, path, maxDiscoveryDirEntries)
	outcome := DiscoveryOutcome{Endpoint: ep, ObservedAt: now, Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "replacement", 1)}, Total: 1}
	if err := cache.Store(outcome, "same", "", DiscoveryPageLimit); err != nil {
		t.Fatalf("count-neutral replacement failed: %v", err)
	}
	loaded, ok := cache.Load(ep, "same", "", DiscoveryPageLimit)
	if !ok || loaded.Items[0].Name != "replacement" {
		t.Fatalf("replacement was not loaded: ok=%t outcome=%#v", ok, loaded)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxDiscoveryDirEntries {
		t.Fatalf("entries = %d, want %d", len(entries), maxDiscoveryDirEntries)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".discovery-") {
			t.Fatalf("temporary residue remains: %s", entry.Name())
		}
	}
}

func TestDiscoveryCacheStoreDoesNotReplaceCorruptExactPathAtCapacity(t *testing.T) {
	cache := NewDiscoveryCache(t.TempDir())
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	path := cache.path(ep, "same", "", "", DiscoveryPageLimit)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("corrupt-owned-looking-path")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	dir := fillDiscoveryEndpointToCount(t, path, maxDiscoveryDirEntries)
	outcome := DiscoveryOutcome{Endpoint: ep, ObservedAt: now, Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "replacement", 1)}, Total: 1}
	if err := cache.Store(outcome, "same", "", DiscoveryPageLimit); err == nil {
		t.Fatal("Store overwrote corrupt exact-path collision")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("corrupt collision changed: %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxDiscoveryDirEntries {
		t.Fatalf("entries = %d, want %d", len(entries), maxDiscoveryDirEntries)
	}
}

func TestDiscoveryCacheStoreSerializesInProcessCapacityChecks(t *testing.T) {
	root := t.TempDir()
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	cache := NewDiscoveryCache(root)
	dir := fillDiscoveryEndpointToCount(t, cache.path(ep, "unused", "", "", DiscoveryPageLimit), maxDiscoveryDirEntries-1)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, query := range []string{"first", "second"} {
		wg.Add(1)
		go func(query string) {
			defer wg.Done()
			<-start
			outcome := DiscoveryOutcome{Endpoint: ep, ObservedAt: time.Now(), Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", query, 1)}, Total: 1}
			errs <- NewDiscoveryCache(root).Store(outcome, query, "", DiscoveryPageLimit)
		}(query)
	}
	close(start)
	wg.Wait()
	close(errs)
	succeeded := 0
	for err := range errs {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("successful stores = %d, want 1", succeeded)
	}
	assertDiscoveryEndpointBound(t, dir)
}

func assertDiscoveryEndpointBound(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > maxDiscoveryDirEntries {
		t.Fatalf("endpoint entries = %d, exceed %d", len(entries), maxDiscoveryDirEntries)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".discovery-") {
			t.Fatalf("temporary residue remains: %s", entry.Name())
		}
	}
}

func TestDiscoveryCacheStoreChildProcess(t *testing.T) {
	if os.Getenv("AGENT_MARKETPLACE_DISCOVERY_STORE_CHILD") != "1" {
		return
	}
	root, query := os.Getenv("AGENT_MARKETPLACE_DISCOVERY_ROOT"), os.Getenv("AGENT_MARKETPLACE_DISCOVERY_QUERY")
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	outcome := DiscoveryOutcome{Endpoint: ep, ObservedAt: time.Now(), Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", query, 1)}, Total: 1}
	cache := NewDiscoveryCache(root)
	var err error
	if held, release := os.Getenv("AGENT_MARKETPLACE_DISCOVERY_HELD"), os.Getenv("AGENT_MARKETPLACE_DISCOVERY_RELEASE"); held != "" {
		record := discoveryCacheRecord{Schema: discoveryCacheSchema, Endpoint: ep.URL, InstanceID: ep.InstanceID, Query: query, Limit: DiscoveryPageLimit, ObservedAt: outcome.ObservedAt, Items: outcome.Items, Total: outcome.Total}
		data, marshalErr := json.Marshal(record)
		if marshalErr != nil {
			os.Exit(4)
		}
		err = cache.withFilesystemLock(true, func() error {
			if writeErr := os.WriteFile(held, nil, 0o600); writeErr != nil {
				return writeErr
			}
			for {
				if _, statErr := os.Stat(release); statErr == nil {
					break
				} else if !errors.Is(statErr, os.ErrNotExist) {
					return statErr
				}
				time.Sleep(time.Millisecond)
			}
			return cache.storeLocked(ep, query, "", "", DiscoveryPageLimit, data)
		})
	} else {
		err = cache.Store(outcome, query, "", DiscoveryPageLimit)
	}
	if err != nil {
		os.Exit(3)
	}
}

func TestDiscoveryCacheStoreSerializesTwoProcessesAtCapacity(t *testing.T) {
	root := t.TempDir()
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	cache := NewDiscoveryCache(root)
	dir := fillDiscoveryEndpointToCount(t, cache.path(ep, "unused", "", "", DiscoveryPageLimit), maxDiscoveryDirEntries-1)
	rendezvous := t.TempDir()
	held, release := filepath.Join(rendezvous, "held"), filepath.Join(rendezvous, "release")
	results := make(chan error, 2)
	startChild := func(query string, holder bool) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestDiscoveryCacheStoreChildProcess$", "-test.timeout=30s")
		cmd.Env = append(os.Environ(), "AGENT_MARKETPLACE_DISCOVERY_STORE_CHILD=1", "AGENT_MARKETPLACE_DISCOVERY_ROOT="+root, "AGENT_MARKETPLACE_DISCOVERY_QUERY="+query)
		if holder {
			cmd.Env = append(cmd.Env, "AGENT_MARKETPLACE_DISCOVERY_HELD="+held, "AGENT_MARKETPLACE_DISCOVERY_RELEASE="+release)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		go func() { results <- cmd.Wait() }()
	}
	startChild("first", true)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(held); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("holder child did not acquire the discovery cache lock")
		}
		time.Sleep(time.Millisecond)
	}
	startChild("second", false)
	select {
	case err := <-results:
		t.Fatalf("child completed while the capacity lock was held: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	succeeded := 0
	for range 2 {
		if err := <-results; err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("successful child stores = %d, want 1", succeeded)
	}
	assertDiscoveryEndpointBound(t, dir)
}

func TestDiscoveryCacheStoreRejectsSymlinkLockPath(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	original := []byte("outside")
	if err := os.WriteFile(outside, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, discoveryCacheLockName)); err != nil {
		t.Fatal(err)
	}
	cache := NewDiscoveryCache(root)
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	outcome := DiscoveryOutcome{Endpoint: ep, ObservedAt: time.Now(), Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "query", 1)}, Total: 1}
	if err := cache.Store(outcome, "query", "", DiscoveryPageLimit); err == nil {
		t.Fatal("Store accepted a symlink lock path")
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("symlink target changed: %q", got)
	}
	if _, err := os.Lstat(cache.root); !os.IsNotExist(err) {
		t.Fatalf("cache root created despite unsafe lock: %v", err)
	}
}

func TestDiscoveryCacheStoreRejectsFullRootBeforeCreatingEndpoint(t *testing.T) {
	cache := NewDiscoveryCache(t.TempDir())
	if err := os.MkdirAll(cache.root, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxDiscoveryMaintenanceDirs; i++ {
		if err := os.WriteFile(filepath.Join(cache.root, fmt.Sprintf("unknown-%04d", i)), []byte("preserve"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ep := config.MarketplaceEntry{URL: "https://new.example", InstanceID: "new"}
	outcome := DiscoveryOutcome{Endpoint: ep, ObservedAt: time.Now(), Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "new", 1)}, Total: 1}
	if err := cache.Store(outcome, "new", "", DiscoveryPageLimit); err == nil {
		t.Fatal("Store succeeded with a full discovery root")
	}
	if _, err := os.Lstat(filepath.Dir(cache.path(ep, "new", "", "", DiscoveryPageLimit))); !os.IsNotExist(err) {
		t.Fatalf("endpoint directory was created: %v", err)
	}
	entries, err := os.ReadDir(cache.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxDiscoveryMaintenanceDirs {
		t.Fatalf("root entries = %d, want %d", len(entries), maxDiscoveryMaintenanceDirs)
	}
}

func TestDiscoveryCacheMaintenanceExpiresRecognizedRecordsAndPreservesUnknownAndSymlinks(t *testing.T) {
	root := t.TempDir()
	cache := NewDiscoveryCache(root)
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	expired := DiscoveryOutcome{Endpoint: ep, ObservedAt: now.Add(-DiscoveryStaleTTL - time.Second), Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "expired", 1)}, Total: 1}
	if err := cache.Store(expired, "expired", "", DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	expiredPath := cache.path(ep, "expired", "", "", DiscoveryPageLimit)
	dir := filepath.Dir(expiredPath)
	unknown := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(unknown, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{ep}}}
	if _, _, err := CachedDiscovery(ec, "", DiscoveryOptions{Cache: cache}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(expiredPath); !os.IsNotExist(err) {
		t.Fatalf("expired recognized record remains: %v", err)
	}
	for _, preserved := range []string{unknown, link, outside} {
		if _, err := os.Lstat(preserved); err != nil {
			t.Fatalf("safe maintenance removed %s: %v", preserved, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".discovery-") {
			t.Fatalf("maintenance residue remains: %s", entry.Name())
		}
	}
}

func TestDiscoveryCacheMaintenanceRemovesOnlySafelyRecognizedRetiredEndpoints(t *testing.T) {
	root := t.TempDir()
	cache := NewDiscoveryCache(root)
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	current := config.MarketplaceEntry{URL: "https://current.example", InstanceID: "current"}
	retired := config.MarketplaceEntry{URL: "https://retired.example", InstanceID: "retired"}
	outcome := DiscoveryOutcome{Endpoint: retired, ObservedAt: now, Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "retired", 1)}, Total: 1}
	if err := cache.Store(outcome, "", "", DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	retiredDir := filepath.Dir(cache.path(retired, "", "", "", DiscoveryPageLimit))
	ambiguousDir := filepath.Join(cache.root, strings.Repeat("a", 64))
	if err := os.Mkdir(ambiguousDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ambiguousDir, "unknown"), []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	linkedDir := filepath.Join(cache.root, strings.Repeat("b", 64))
	if err := os.Symlink(outside, linkedDir); err != nil {
		t.Fatal(err)
	}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{current}}}
	if _, _, err := CachedDiscovery(ec, "", DiscoveryOptions{Cache: cache}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(retiredDir); !os.IsNotExist(err) {
		t.Fatalf("safely removable retired endpoint remains: %v", err)
	}
	for _, preserved := range []string{ambiguousDir, filepath.Join(ambiguousDir, "unknown"), linkedDir, outside} {
		if _, err := os.Lstat(preserved); err != nil {
			t.Fatalf("ambiguous or linked entry removed %s: %v", preserved, err)
		}
	}
}

func TestDiscoveryCacheRejectsOversizedRecordBeforeCreatingTemporaryFile(t *testing.T) {
	root := t.TempDir()
	cache := NewDiscoveryCache(root)
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	item := canonicalSearchItem("registry", "skill", "review", 1)
	item.Metadata = map[string]string{"oversized": strings.Repeat("x", maxDiscoveryCacheSize)}
	outcome := DiscoveryOutcome{Endpoint: ep, ObservedAt: time.Now(), Items: []mpapi.ResourceListItem{item}, Total: 1}

	if err := cache.Store(outcome, "oversized", "", DiscoveryPageLimit); err == nil {
		t.Fatal("oversized discovery cache record was stored")
	}
	path := cache.path(ep, "oversized", "", "", DiscoveryPageLimit)
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("published cache exists after oversized record: %v", err)
	}
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary or staging residue remains: %v", entries)
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

func TestDiscoveryRejectsOverLimitNetworkResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp-over-limit")
		case "/api/v1/search":
			items := make([]mpapi.ResourceListItem, DiscoveryPageLimit+1)
			for i := range items {
				items[i] = canonicalSearchItem("registry", "skill", fmt.Sprintf("item-%02d", i), 1)
			}
			_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: len(items), Limit: DiscoveryPageLimit})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ep := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp-over-limit"}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{ep}}}

	outcomes, err := Discover(context.Background(), ec, "", DiscoveryOptions{Cache: NewDiscoveryCache(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Err == nil || len(outcomes[0].Items) != 0 {
		t.Fatalf("outcomes = %#v, want one closed failure", outcomes)
	}
}

func TestDiscoveryRejectsTrailingNetworkJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "mp-trailing")
		case "/api/v1/search":
			response := SearchResponse{Items: []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "review", 1)}, Total: 1, Limit: DiscoveryPageLimit}
			data, _ := json.Marshal(response)
			_, _ = w.Write(append(data, []byte(` {"extra":true}`)...))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ep := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp-trailing"}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{ep}}}
	outcomes, err := Discover(context.Background(), ec, "", DiscoveryOptions{Cache: NewDiscoveryCache(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Err == nil || len(outcomes[0].Items) != 0 {
		t.Fatalf("outcomes = %#v, want trailing JSON failure", outcomes)
	}
}

func TestDiscoveryCacheRejectsSemanticCorruptionAndTrailingJSON(t *testing.T) {
	cache := NewDiscoveryCache(t.TempDir())
	ep := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market"}
	path := cache.path(ep, "", "", "", DiscoveryPageLimit)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	valid := discoveryCacheRecord{Schema: discoveryCacheSchema, Endpoint: ep.URL, InstanceID: ep.InstanceID, Limit: DiscoveryPageLimit, ObservedAt: time.Now(), Total: 1, Items: []mpapi.ResourceListItem{{RegistryID: "registry", Kind: "skill", Name: "bad", Revision: "not-a-digest", ContentDigest: "not-a-digest"}}}
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		data []byte
	}{
		{name: "semantic", data: data},
		{name: "trailing", data: append(append([]byte(nil), data...), []byte(` {"extra":true}`)...)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, test.data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, ok := cache.Load(ep, "", "", DiscoveryPageLimit); ok {
				t.Fatal("corrupt cache record was accepted")
			}
		})
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

func TestDiscoverIncludesEveryKindAcrossPagesAndCachesTheUnion(t *testing.T) {
	for _, kind := range []string{"skill", "command", "subagent", "hook", "mcpserver", "collection"} {
		t.Run(kind, func(t *testing.T) {
			type searchRequest struct {
				query, kind string
				offset      int
			}
			var mu sync.Mutex
			requests := map[string][]searchRequest{}
			servers := make([]*httptest.Server, 2)
			endpoints := make([]config.MarketplaceEntry, 2)
			for i := range servers {
				index := i
				alias := fmt.Sprintf("market-%d", index)
				instanceID := "kind-search-" + alias
				registryID := "registry-" + alias
				servers[index] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/.well-known/agent-marketplace.json" {
						writeTestWellKnown(w, instanceID)
						return
					}
					if r.URL.Path != "/api/v1/search" {
						http.NotFound(w, r)
						return
					}
					query, requestedKind := r.URL.Query().Get("q"), r.URL.Query().Get("kind")
					offset, limit := 0, DiscoveryPageLimit
					_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
					_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
					mu.Lock()
					requests[alias] = append(requests[alias], searchRequest{query: query, kind: requestedKind, offset: offset})
					mu.Unlock()

					var all []mpapi.ResourceListItem
					total := 0
					switch {
					case query == "needle" && requestedKind == "":
						total = 60
						all = make([]mpapi.ResourceListItem, DiscoveryPageLimit)
						for j := range all {
							name := fmt.Sprintf("text-%02d", j)
							if j == 0 {
								name = "shared"
							}
							all[j] = canonicalSearchItem(registryID, kind, name, float64(DiscoveryPageLimit-j))
							all[j].Description = "needle appears only in metadata"
						}
					case query == "" && requestedKind == kind:
						total = 61
						all = make([]mpapi.ResourceListItem, total)
						for j := range all {
							name := fmt.Sprintf("opaque-%02d", j)
							if j == 0 {
								name = "shared"
							}
							all[j] = canonicalSearchItem(registryID, kind, name, 0)
							all[j].Description = "opaque resource details"
						}
					}
					end := min(offset+limit, len(all))
					items := []mpapi.ResourceListItem{}
					if offset < end {
						items = all[offset:end]
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: total, Offset: offset, Limit: limit})
				}))
				endpoints[index] = config.MarketplaceEntry{URL: servers[index].URL, InstanceID: instanceID, Alias: alias}
				t.Cleanup(servers[index].Close)
			}
			ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: endpoints}}
			cache := NewDiscoveryCache(t.TempDir())
			opts := DiscoveryOptions{Limit: DiscoveryPageLimit, IncludeKind: kind, Cache: cache}
			outcomes, err := Discover(context.Background(), ec, "needle", opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(outcomes) != len(endpoints) {
				t.Fatalf("outcomes = %d, want %d", len(outcomes), len(endpoints))
			}
			for _, outcome := range outcomes {
				if outcome.Err != nil {
					t.Fatalf("%s discovery: %v", outcome.Endpoint.Alias, outcome.Err)
				}
				if len(outcome.Items) != 110 || !outcome.Truncated {
					t.Fatalf("%s result count/truncation = %d/%t, want 110/true", outcome.Endpoint.Alias, len(outcome.Items), outcome.Truncated)
				}
				if outcome.Items[0].Name != "shared" || outcome.Items[1].Name != "text-01" || outcome.Items[49].Name != "text-49" || outcome.Items[50].Name != "opaque-01" || outcome.Items[109].Name != "opaque-60" {
					t.Fatalf("%s union order = first %q/%q, boundary %q/%q, last %q", outcome.Endpoint.Alias, outcome.Items[0].Name, outcome.Items[1].Name, outcome.Items[49].Name, outcome.Items[50].Name, outcome.Items[109].Name)
				}
				coordinates := map[string]bool{}
				for _, item := range outcome.Items {
					coordinate := item.RegistryID + "\x00" + item.Kind + "\x00" + item.Name
					if coordinates[coordinate] {
						t.Fatalf("%s duplicate coordinate %q", outcome.Endpoint.Alias, coordinate)
					}
					coordinates[coordinate] = true
				}
			}
			for alias, observed := range requests {
				if len(observed) != 3 {
					t.Fatalf("%s requests = %#v, want free-text plus kind pages at offsets 0 and 50", alias, observed)
				}
			}
			cached, needsNetwork, err := CachedDiscovery(ec, "needle", opts)
			if err != nil || needsNetwork || len(cached) != len(outcomes) {
				t.Fatalf("cached kind discovery = %d outcomes, network=%t, err=%v", len(cached), needsNetwork, err)
			}
			for i := range outcomes {
				if !reflect.DeepEqual(cached[i].Items, outcomes[i].Items) || cached[i].Truncated != outcomes[i].Truncated {
					t.Fatalf("cached outcome %d differs from live union: cached=%#v live=%#v", i, cached[i], outcomes[i])
				}
			}
			textOnly, needsNetwork, err := CachedDiscovery(ec, "needle", DiscoveryOptions{Limit: DiscoveryPageLimit, Cache: cache})
			if err != nil || !needsNetwork || len(textOnly) != 0 {
				t.Fatalf("text-only cache lookup collided with kind results: outcomes=%#v network=%t err=%v", textOnly, needsNetwork, err)
			}
		})
	}
}

func TestDiscoverKindPaginationRejectsNonProgressingPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "kind-stalled")
			return
		}
		offset, limit := 0, DiscoveryPageLimit
		_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
		_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		items := []mpapi.ResourceListItem{}
		total := 0
		if r.URL.Query().Get("kind") == "mcpserver" {
			total = 51
			if offset == 0 {
				items = make([]mpapi.ResourceListItem, DiscoveryPageLimit)
				for i := range items {
					items[i] = canonicalSearchItem("registry", "mcpserver", fmt.Sprintf("opaque-%02d", i), 0)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: total, Offset: offset, Limit: limit})
	}))
	defer server.Close()
	endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: "kind-stalled", Alias: "stalled"}
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}}
	outcomes, err := Discover(context.Background(), ec, "mcp", DiscoveryOptions{IncludeKind: "mcpserver", Cache: NewDiscoveryCache(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Err == nil || !strings.Contains(outcomes[0].Err.Error(), "no progress") {
		t.Fatalf("stalled pagination outcome = %#v, want endpoint no-progress error", outcomes)
	}
}

func TestDiscoverUpdatesKeepsAllowedEndpointWhenAnotherIsDenied(t *testing.T) {
	var allowedRequests, deniedRequests atomic.Int32
	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowedRequests.Add(1)
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "allowed-market")
			return
		}
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		items := []mpapi.ResourceListItem{}
		if r.URL.Query().Get("kind") == "mcpserver" {
			items = []mpapi.ResourceListItem{canonicalSearchItem("registry", "mcpserver", "allowed-opaque", 0)}
		}
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: len(items), Limit: DiscoveryPageLimit})
	}))
	defer allowed.Close()
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deniedRequests.Add(1)
		http.NotFound(w, r)
	}))
	defer denied.Close()

	allowedEndpoint := config.MarketplaceEntry{URL: allowed.URL, InstanceID: "allowed-market", Alias: "allowed"}
	deniedEndpoint := config.MarketplaceEntry{URL: denied.URL, InstanceID: "denied-market", Alias: "denied"}
	cache := NewDiscoveryCache(t.TempDir())
	cached := DiscoveryOutcome{
		Endpoint:   deniedEndpoint,
		Items:      []mpapi.ResourceListItem{canonicalSearchItem("registry", "mcpserver", "cached-denied", 0)},
		Total:      1,
		ObservedAt: time.Now(),
	}
	if err := cache.store(cached, "mcp", "", "mcpserver", DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	cachePath := cache.path(deniedEndpoint, "mcp", "", "mcpserver", DiscoveryPageLimit)
	cacheBefore, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}

	effective := config.EffectiveConfig{
		Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{allowedEndpoint, deniedEndpoint}},
		AllowedMarketplaces: []string{
			"^" + regexp.QuoteMeta(allowed.URL) + "$",
		},
	}
	outcomes, err := Discover(context.Background(), effective, "mcp", DiscoveryOptions{
		IncludeKind: "mcpserver",
		Cache:       cache,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes = %d, want one per configured endpoint: %#v", len(outcomes), outcomes)
	}
	var allowedOutcome, deniedOutcome *DiscoveryOutcome
	for i := range outcomes {
		switch outcomes[i].Endpoint.URL {
		case allowed.URL:
			allowedOutcome = &outcomes[i]
		case denied.URL:
			deniedOutcome = &outcomes[i]
		}
	}
	if allowedOutcome == nil || allowedOutcome.Err != nil || len(allowedOutcome.Items) != 1 || allowedOutcome.Items[0].Name != "allowed-opaque" {
		t.Fatalf("allowed endpoint outcome = %#v", allowedOutcome)
	}
	var blocked *config.MarketplaceURLBlockedError
	if deniedOutcome == nil || !errors.As(deniedOutcome.Err, &blocked) || len(blocked.BlockedURLs) != 1 || blocked.BlockedURLs[0] != denied.URL {
		t.Fatalf("denied endpoint outcome = %#v, want its own MarketplaceURLBlockedError", deniedOutcome)
	}
	if len(deniedOutcome.Items) != 0 {
		t.Fatalf("denied endpoint exposed cached resources: %#v", deniedOutcome.Items)
	}
	if allowedRequests.Load() == 0 || deniedRequests.Load() != 0 {
		t.Fatalf("request counts = allowed %d, denied %d; want allowed requests and no denied requests", allowedRequests.Load(), deniedRequests.Load())
	}
	cacheAfter, err := os.ReadFile(cachePath)
	if err != nil || !reflect.DeepEqual(cacheAfter, cacheBefore) {
		t.Fatalf("denied endpoint cache changed: err=%v", err)
	}
}

func TestDiscoverUpdatesMaintainsCacheWhenAnotherEndpointIsDenied(t *testing.T) {
	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "allowed-market")
			return
		}
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: []mpapi.ResourceListItem{}, Limit: DiscoveryPageLimit})
	}))
	defer allowed.Close()
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer denied.Close()

	allowedEndpoint := config.MarketplaceEntry{URL: allowed.URL, InstanceID: "allowed-market", Alias: "allowed"}
	deniedEndpoint := config.MarketplaceEntry{URL: denied.URL, InstanceID: "denied-market", Alias: "denied"}
	cache := NewDiscoveryCache(t.TempDir())
	if err := cache.store(DiscoveryOutcome{Endpoint: allowedEndpoint, ObservedAt: time.Now().Add(-DiscoveryStaleTTL - time.Hour)}, "old", "", "", DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	if err := cache.store(DiscoveryOutcome{Endpoint: deniedEndpoint, ObservedAt: time.Now()}, "old", "", "", DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	staleAllowedPath := cache.path(allowedEndpoint, "old", "", "", DiscoveryPageLimit)
	deniedPath := cache.path(deniedEndpoint, "old", "", "", DiscoveryPageLimit)

	effective := config.EffectiveConfig{
		Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{allowedEndpoint, deniedEndpoint}},
		AllowedMarketplaces: []string{
			"^" + regexp.QuoteMeta(allowed.URL) + "$",
		},
	}
	if _, err := Discover(context.Background(), effective, "fresh", DiscoveryOptions{Cache: cache}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staleAllowedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale allowed endpoint record was not pruned: err=%v", err)
	}
	if _, err := os.Stat(deniedPath); err != nil {
		t.Fatalf("denied endpoint record was not retained: %v", err)
	}
}

func TestDiscoverUpdatesReturnsPartialKindResultsOnKindFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "partial-market")
		case "/api/v1/search":
			if r.URL.Query().Get("kind") != "" {
				http.Error(w, "kind search unavailable", http.StatusInternalServerError)
				return
			}
			items := []mpapi.ResourceListItem{canonicalSearchItem("registry", "mcpserver", "widget-match", 0)}
			_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: len(items), Limit: DiscoveryPageLimit})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ep := config.MarketplaceEntry{URL: server.URL, Alias: "partial"}
	acquired := ep
	acquired.InstanceID = "partial-market"
	cache := NewDiscoveryCache(t.TempDir())
	effective := config.EffectiveConfig{
		Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{ep}},
		AllowedMarketplaces: []string{
			"^" + regexp.QuoteMeta(server.URL) + "$",
		},
	}
	outcomes, err := Discover(context.Background(), effective, "widget", DiscoveryOptions{
		IncludeKind: "mcpserver",
		Cache:       cache,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes = %d, want one: %#v", len(outcomes), outcomes)
	}
	if outcomes[0].Err == nil {
		t.Fatal("kind failure outcome has no error")
	}
	if len(outcomes[0].Items) != 1 || outcomes[0].Items[0].Name != "widget-match" {
		t.Fatalf("kind failure dropped usable text matches: %#v", outcomes[0].Items)
	}
	if outcomes[0].Endpoint.InstanceID != acquired.InstanceID {
		t.Fatalf("kind failure endpoint = %#v, want acquired identity %#v", outcomes[0].Endpoint, acquired)
	}
	if _, err := os.Stat(cache.path(acquired, "widget", "", "mcpserver", DiscoveryPageLimit)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial kind results were cached: err=%v", err)
	}
}

func TestDiscoverKindPaginationUsesConfiguredTimeout(t *testing.T) {
	kindPageStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			writeTestWellKnown(w, "kind-timeout")
		case "/api/v1/search":
			if r.URL.Query().Get("kind") == "" {
				items := []mpapi.ResourceListItem{canonicalSearchItem("registry", "skill", "text-match", 1)}
				_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: len(items), Limit: DiscoveryPageLimit})
				return
			}
			offset := 0
			_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
			if offset == 0 {
				time.Sleep(350 * time.Millisecond)
				items := []mpapi.ResourceListItem{canonicalSearchItem("registry", "mcpserver", "kind-match", 1)}
				_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: 2, Offset: offset, Limit: DiscoveryPageLimit})
				return
			}
			close(kindPageStarted)
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ep := config.MarketplaceEntry{URL: server.URL, InstanceID: "kind-timeout"}
	effective := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{ep}}}
	timeout := 600 * time.Millisecond
	cache := NewDiscoveryCache(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		outcomes []DiscoveryOutcome
		err      error
	}
	done := make(chan result, 1)
	started := time.Now()
	go func() {
		outcomes, err := Discover(ctx, effective, "widget", DiscoveryOptions{
			IncludeKind: "mcpserver",
			Timeout:     timeout,
			Cache:       cache,
		})
		done <- result{outcomes: outcomes, err: err}
	}()
	select {
	case <-kindPageStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("kind pagination did not reach its later page")
	}
	select {
	case got := <-done:
		elapsed := time.Since(started)
		if elapsed > timeout+150*time.Millisecond {
			t.Fatalf("kind discovery returned after %s, beyond its shared %s deadline", elapsed, timeout)
		}
		if got.err != nil || len(got.outcomes) != 1 || got.outcomes[0].Err == nil {
			t.Fatalf("kind timeout outcome = %#v, err=%v; want an endpoint error", got.outcomes, got.err)
		}
		if len(got.outcomes[0].Items) == 0 || got.outcomes[0].Items[0].Name != "text-match" {
			t.Fatalf("kind timeout dropped text matches: %#v", got.outcomes[0].Items)
		}
	case <-time.After(timeout + 500*time.Millisecond):
		t.Fatalf("kind discovery did not return within %s after its later page blocked", timeout+500*time.Millisecond)
	}
}

func BenchmarkDiscoverKindQueryAndUnion(b *testing.B) {
	kindItems := make([]mpapi.ResourceListItem, 51)
	kindItems[0] = canonicalSearchItem("registry", "mcpserver", "shared", 1)
	for i := 1; i < len(kindItems); i++ {
		kindItems[i] = canonicalSearchItem("registry", "mcpserver", fmt.Sprintf("opaque-%02d", i-1), 0)
		kindItems[i].Description = "opaque resource details"
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			writeTestWellKnown(w, "bench-market")
			return
		}
		offset, limit := 0, DiscoveryPageLimit
		_, _ = fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
		_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
		items := []mpapi.ResourceListItem{
			canonicalSearchItem("registry", "mcpserver", "shared", 1),
			canonicalSearchItem("registry", "command", "text-match", 0),
		}
		total := len(items)
		if r.URL.Query().Get("kind") == "mcpserver" {
			total = len(kindItems)
			end := min(offset+limit, total)
			items = kindItems[offset:end]
		}
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: items, Total: total, Offset: offset, Limit: limit})
	}))
	defer server.Close()
	endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: "bench-market"}
	opts := DiscoveryOptions{Limit: DiscoveryPageLimit, IncludeKind: "mcpserver"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		outcome, err := discoverOne(context.Background(), endpoint, false, "needle", opts, DiscoveryPageLimit)
		if err != nil || len(outcome.Items) != 52 {
			b.Fatalf("kind discovery = %d items, err=%v", len(outcome.Items), err)
		}
	}
}
