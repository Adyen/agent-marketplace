package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

type catalogFixture struct {
	server     *httptest.Server
	instanceID string
	version    string
	etag       string
	resources  []mpapi.CatalogResource
	pageSize   int
	requests   atomic.Int32

	// mutate swaps the catalog handler mid-test.
	mutate func(w http.ResponseWriter, r *http.Request) bool
}

func newCatalogFixture(t *testing.T, instanceID string, count int) *catalogFixture {
	t.Helper()
	f := &catalogFixture{instanceID: instanceID, version: "7", etag: `"cat-7"`, pageSize: 50}
	for i := 0; i < count; i++ {
		kind := "skill"
		if i%3 == 2 {
			kind = "mcpserver"
		}
		f.resources = append(f.resources, mpapi.CatalogResource{
			RegistryID: "reg-id", RegistrySlug: "reg", Kind: kind,
			Name: fmt.Sprintf("res-%03d", i), Revision: fmt.Sprintf("sha256:%064x", i+1),
			Availability: mpapi.AvailabilityAvailable,
		})
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(f.instanceID, "https://example.invalid"))
		case "/api/v1/catalog":
			f.requests.Add(1)
			if f.mutate != nil && f.mutate(w, r) {
				return
			}
			f.servePage(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *catalogFixture) servePage(w http.ResponseWriter, r *http.Request) {
	after := r.URL.Query().Get("after")
	if after == "" {
		if inm := r.Header.Get("If-None-Match"); inm == f.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	} else if !strings.HasPrefix(after, "cursor-") {
		http.Error(w, `{"error":"invalid catalog cursor"}`, http.StatusBadRequest)
		return
	}
	start := 0
	if after != "" {
		n, err := strconv.Atoi(strings.TrimPrefix(after, "cursor-"))
		if err != nil {
			http.Error(w, `{"error":"invalid catalog cursor"}`, http.StatusBadRequest)
			return
		}
		start = n
	}
	end := min(start+f.pageSize, len(f.resources))
	page := mpapi.CatalogPage{
		APIVersion:     mpapi.CatalogAPIVersionV1Alpha1,
		MarketplaceID:  f.instanceID,
		CatalogVersion: f.version,
		Resources:      f.resources[start:end],
	}
	if end < len(f.resources) {
		page.NextCursor = mpapi.CatalogKeysetCursor(fmt.Sprintf("cursor-%d", end))
	} else {
		page.Complete = true
	}
	w.Header().Set("ETag", f.etag)
	_ = json.NewEncoder(w).Encode(page)
}

func (f *catalogFixture) entry() config.MarketplaceEntry {
	return config.MarketplaceEntry{URL: f.server.URL, InstanceID: f.instanceID, Alias: "cat-" + f.instanceID}
}

func testCatalogCache(t *testing.T) *CatalogCache {
	t.Helper()
	return NewCatalogCache(filepath.Join(t.TempDir(), "mp"))
}

// spec: code-mode-mcp/marketplace-resources-beyond-the-first-page-are-searchable
func TestScanCatalogFollowsAllPagesAndCaches(t *testing.T) {
	fixture := newCatalogFixture(t, "mp-1", 120)
	cache := testCatalogCache(t)

	out := ScanCatalog(context.Background(), fixture.entry(), CatalogScanOptions{Cache: cache})
	if out.Err != nil || !out.Complete {
		t.Fatalf("ScanCatalog = %+v, want complete", out)
	}
	if len(out.Resources) != 120 || out.CatalogVersion != "7" || out.ETag != `"cat-7"` {
		t.Fatalf("ScanCatalog outcome = %+v", out)
	}

	fixture.server.Close()
	cached, hit := cache.Load(fixture.entry())
	if !hit || len(cached.Resources) != 120 || !cached.Complete {
		t.Fatalf("cache.Load = %+v hit %v, want complete 120", cached, hit)
	}
}

// spec: code-mode-mcp/incomplete-or-inconsistent-catalog-remains-stale
func TestScanCatalogIncompleteAndInconsistent(t *testing.T) {
	t.Run("version changes between pages", func(t *testing.T) {
		fixture := newCatalogFixture(t, "mp-1", 60)
		cache := testCatalogCache(t)
		pages := 0
		fixture.mutate = func(w http.ResponseWriter, r *http.Request) bool {
			pages++
			if pages == 2 {
				w.Header().Set("ETag", fixture.etag)
				_ = json.NewEncoder(w).Encode(mpapi.CatalogPage{
					APIVersion: mpapi.CatalogAPIVersionV1Alpha1, MarketplaceID: fixture.instanceID,
					CatalogVersion: "8", Complete: true,
					Resources: fixture.resources[50:],
				})
				return true
			}
			return false
		}
		out := ScanCatalog(context.Background(), fixture.entry(), CatalogScanOptions{Cache: cache})
		if out.Err == nil || out.Complete {
			t.Fatalf("version change outcome = %+v, want error", out)
		}
		if _, hit := cache.Load(fixture.entry()); hit {
			t.Fatal("inconsistent scan was cached")
		}
	})

	t.Run("marketplace id disagrees with well-known", func(t *testing.T) {
		fixture := newCatalogFixture(t, "mp-1", 2)
		cache := testCatalogCache(t)
		fixture.mutate = func(w http.ResponseWriter, r *http.Request) bool {
			w.Header().Set("ETag", fixture.etag)
			_ = json.NewEncoder(w).Encode(mpapi.CatalogPage{
				APIVersion: mpapi.CatalogAPIVersionV1Alpha1, MarketplaceID: "other-instance",
				CatalogVersion: fixture.version, Complete: true, Resources: fixture.resources,
			})
			return true
		}
		out := ScanCatalog(context.Background(), fixture.entry(), CatalogScanOptions{Cache: cache})
		if out.Err == nil || out.Complete {
			t.Fatalf("identity mismatch outcome = %+v, want error", out)
		}
	})

	t.Run("configured instance id mismatch", func(t *testing.T) {
		fixture := newCatalogFixture(t, "mp-1", 2)
		cache := testCatalogCache(t)
		ep := fixture.entry()
		ep.InstanceID = "wrong-id"
		out := ScanCatalog(context.Background(), ep, CatalogScanOptions{Cache: cache})
		if out.Err == nil || out.Complete {
			t.Fatalf("configured identity mismatch outcome = %+v, want error", out)
		}
	})

	t.Run("incomplete page without cursor", func(t *testing.T) {
		fixture := newCatalogFixture(t, "mp-1", 2)
		cache := testCatalogCache(t)
		fixture.mutate = func(w http.ResponseWriter, r *http.Request) bool {
			_ = json.NewEncoder(w).Encode(mpapi.CatalogPage{
				APIVersion: mpapi.CatalogAPIVersionV1Alpha1, MarketplaceID: fixture.instanceID,
				CatalogVersion: fixture.version, Complete: false, Resources: fixture.resources,
			})
			return true
		}
		out := ScanCatalog(context.Background(), fixture.entry(), CatalogScanOptions{Cache: cache})
		if out.Err == nil || out.Complete {
			t.Fatalf("truncated scan outcome = %+v, want error", out)
		}
	})

	t.Run("bounded scan exceeded", func(t *testing.T) {
		fixture := newCatalogFixture(t, "mp-1", 2)
		cache := testCatalogCache(t)
		fixture.mutate = func(w http.ResponseWriter, r *http.Request) bool {
			after, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Query().Get("after"), "cursor-"))
			_ = json.NewEncoder(w).Encode(mpapi.CatalogPage{
				APIVersion: mpapi.CatalogAPIVersionV1Alpha1, MarketplaceID: fixture.instanceID,
				CatalogVersion: fixture.version, Complete: false,
				NextCursor: mpapi.CatalogKeysetCursor(fmt.Sprintf("cursor-%d", after+1)),
				Resources:  fixture.resources,
			})
			return true
		}
		out := ScanCatalog(context.Background(), fixture.entry(), CatalogScanOptions{Cache: cache})
		if out.Err == nil || out.Complete {
			t.Fatalf("unbounded scan outcome = %+v, want error", out)
		}
	})

	t.Run("failed page aborts", func(t *testing.T) {
		fixture := newCatalogFixture(t, "mp-1", 60)
		cache := testCatalogCache(t)
		fixture.mutate = func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Query().Get("after") != "" {
				http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
				return true
			}
			return false
		}
		out := ScanCatalog(context.Background(), fixture.entry(), CatalogScanOptions{Cache: cache})
		if out.Err == nil || out.Complete {
			t.Fatalf("failed page outcome = %+v, want error", out)
		}
	})
}

// spec: code-mode-mcp/incomplete-or-inconsistent-catalog-remains-stale
func TestScanCatalogAggregateBytesBound(t *testing.T) {
	fixture := newCatalogFixture(t, "mp-1", 2)
	cache := testCatalogCache(t)
	pad := strings.Repeat("a", 12<<20)
	fixture.mutate = func(w http.ResponseWriter, r *http.Request) bool {
		after, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Query().Get("after"), "cursor-"))
		_, _ = fmt.Fprintf(w,
			`{"api_version":%q,"marketplace_id":%q,"catalog_version":%q,"resources":[],"next_cursor":"cursor-%d","pad":%q}`,
			mpapi.CatalogAPIVersionV1Alpha1, fixture.instanceID, fixture.version, after+1, pad)
		return true
	}
	out := ScanCatalog(context.Background(), fixture.entry(), CatalogScanOptions{Cache: cache})
	if out.Err == nil || out.Complete || !strings.Contains(out.Err.Error(), "bytes") {
		t.Fatalf("oversized scan outcome = %+v, want aggregate byte bound error", out)
	}
	if fixture.requests.Load() >= maxCatalogScanPages {
		t.Fatalf("scan fetched %d pages before stopping; aggregate byte bound not enforced", fixture.requests.Load())
	}
}

// spec: code-mode-mcp/incomplete-or-inconsistent-catalog-remains-stale
func TestScanCatalogPersistFailureStaysIncomplete(t *testing.T) {
	fixture := newCatalogFixture(t, "mp-1", 5)
	cache := testCatalogCache(t)
	ep := fixture.entry()

	if out := ScanCatalog(context.Background(), ep, CatalogScanOptions{Cache: cache}); out.Err != nil {
		t.Fatalf("first scan: %v", out.Err)
	}
	dir := filepath.Dir(cache.path(ep))
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	fixture.version = "8"
	fixture.etag = `"cat-8"`
	out := ScanCatalog(context.Background(), ep, CatalogScanOptions{Cache: cache})
	if out.Err == nil || out.Complete {
		t.Fatalf("persist-failed scan outcome = %+v, want incomplete with error", out)
	}
	cached, hit := cache.Load(ep)
	if !hit || cached.CatalogVersion != "7" || len(cached.Resources) != 5 {
		t.Fatalf("cache after failed persist = %+v hit %v, want previous verified record", cached, hit)
	}
}

func TestCatalogCacheRejectsSymlinkRead(t *testing.T) {
	fixture := newCatalogFixture(t, "mp-1", 3)
	cache := testCatalogCache(t)
	ep := fixture.entry()

	out := ScanCatalog(context.Background(), ep, CatalogScanOptions{Cache: cache})
	if out.Err != nil {
		t.Fatalf("scan: %v", out.Err)
	}
	path := cache.path(ep)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(target, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, hit := cache.Load(ep); hit {
		t.Fatal("symlinked catalog record was read")
	}
}

func TestScanCatalogETagReuse(t *testing.T) {
	fixture := newCatalogFixture(t, "mp-1", 10)
	cache := testCatalogCache(t)
	ep := fixture.entry()

	first := ScanCatalog(context.Background(), ep, CatalogScanOptions{Cache: cache})
	if first.Err != nil || !first.Complete {
		t.Fatalf("first scan = %+v", first)
	}
	before := fixture.requests.Load()

	second := ScanCatalog(context.Background(), ep, CatalogScanOptions{Cache: cache})
	if second.Err != nil || !second.Complete || len(second.Resources) != 10 {
		t.Fatalf("304 scan = %+v", second)
	}
	if !second.FromCache {
		t.Fatal("304 scan did not reuse the cached record")
	}
	if fixture.requests.Load() != before+1 {
		t.Fatalf("catalog requests = %d, want one conditional request", fixture.requests.Load())
	}
}

func TestScanCatalogFailureKeepsPreviousCompleteCache(t *testing.T) {
	fixture := newCatalogFixture(t, "mp-1", 5)
	cache := testCatalogCache(t)
	ep := fixture.entry()

	if out := ScanCatalog(context.Background(), ep, CatalogScanOptions{Cache: cache}); out.Err != nil {
		t.Fatalf("first scan: %v", out.Err)
	}
	fixture.mutate = func(w http.ResponseWriter, r *http.Request) bool {
		http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
		return true
	}
	if out := ScanCatalog(context.Background(), ep, CatalogScanOptions{Cache: cache}); out.Err == nil {
		t.Fatal("failing endpoint scan unexpectedly succeeded")
	}
	cached, hit := cache.Load(ep)
	if !hit || len(cached.Resources) != 5 {
		t.Fatalf("cache after failed refresh = %+v hit %v, want previous complete record", cached, hit)
	}
}

func TestCatalogCacheRejectsCorruptAndUnsafeRecords(t *testing.T) {
	fixture := newCatalogFixture(t, "mp-1", 3)
	cache := testCatalogCache(t)
	ep := fixture.entry()

	out := ScanCatalog(context.Background(), ep, CatalogScanOptions{Cache: cache})
	if out.Err != nil {
		t.Fatalf("scan: %v", out.Err)
	}
	path := cache.path(ep)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}

	for name, mutate := range map[string]func([]byte) []byte{
		"garbage":   func([]byte) []byte { return []byte("not json") },
		"truncated": func(b []byte) []byte { return b[:len(b)/2] },
		"incomplete": func(b []byte) []byte {
			return []byte(strings.Replace(string(b), `"complete":true`, `"complete":false`, 1))
		},
		"other endpoint": func(b []byte) []byte {
			return []byte(strings.Replace(string(b), ep.URL, "https://evil.example", 1))
		},
	} {
		if err := os.WriteFile(path, mutate(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, hit := cache.Load(ep); hit {
			t.Fatalf("%s record was accepted", name)
		}
	}
}

func TestCachedAndRefreshCatalogOutcomes(t *testing.T) {
	good := newCatalogFixture(t, "good-id", 4)
	bad := newCatalogFixture(t, "bad-id", 4)
	bad.mutate = func(w http.ResponseWriter, r *http.Request) bool {
		http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
		return true
	}

	cache := testCatalogCache(t)
	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{
		good.entry(), bad.entry(),
	}}}

	cached, err := CachedCatalog(ec, cache)
	if err != nil {
		t.Fatalf("CachedCatalog: %v", err)
	}
	if len(cached) != 2 {
		t.Fatalf("CachedCatalog outcomes = %+v, want one per endpoint", cached)
	}
	for _, out := range cached {
		if out.Complete || out.Err == nil {
			t.Fatalf("cold CachedCatalog outcome = %+v, want miss", out)
		}
	}

	outcomes, err := RefreshCatalog(context.Background(), ec, CatalogScanOptions{Cache: cache})
	if err != nil {
		t.Fatalf("RefreshCatalog: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("RefreshCatalog outcomes = %+v, want one per endpoint", outcomes)
	}
	var sawGood, sawBad bool
	for _, out := range outcomes {
		switch out.Endpoint.InstanceID {
		case "good-id":
			sawGood = out.Complete && out.Err == nil && len(out.Resources) == 4
		case "bad-id":
			sawBad = !out.Complete && out.Err != nil
		}
	}
	if !sawGood || !sawBad {
		t.Fatalf("RefreshCatalog outcomes = %+v, want good complete and bad failed", outcomes)
	}

	cached, err = CachedCatalog(ec, cache)
	if err != nil {
		t.Fatalf("CachedCatalog after refresh: %v", err)
	}
	var goodHit bool
	for _, out := range cached {
		if out.Endpoint.InstanceID == "good-id" {
			goodHit = out.Complete && len(out.Resources) == 4
		}
	}
	if !goodHit {
		t.Fatalf("CachedCatalog after refresh = %+v, want persisted good catalog", cached)
	}
}
