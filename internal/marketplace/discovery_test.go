package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
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
	path := cache.path(ep, query, "", DiscoveryPageLimit)
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
	path := cache.path(ep, "stored", "", DiscoveryPageLimit)
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
	dir := filepath.Dir(cache.path(ep, "query-00", "", DiscoveryPageLimit))
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
	oldest := cache.path(ep, "query-00", "", DiscoveryPageLimit)
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
	expiredPath := cache.path(ep, "expired", "", DiscoveryPageLimit)
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
	retiredDir := filepath.Dir(cache.path(retired, "", "", DiscoveryPageLimit))
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
	path := cache.path(ep, "oversized", "", DiscoveryPageLimit)
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
	path := cache.path(ep, "", "", DiscoveryPageLimit)
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
