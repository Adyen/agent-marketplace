package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func searchServer(tb testing.TB, name string, fail bool, items ...mpapi.ResourceListItem) *httptest.Server {
	tb.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(map[string]string{"instance_id": "mp-" + name})
			return
		}
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		if fail {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"down"}`))
			return
		}
		q := r.URL.Query()
		limit := 100
		if l := q.Get("limit"); l != "" {
			if _, err := fmt.Sscanf(l, "%d", &limit); err != nil {
				http.Error(w, "bad limit", http.StatusBadRequest)
				return
			}
		}
		result := items
		if limit > 0 && limit < len(items) {
			result = items[:limit]
		}
		resp := SearchResponse{Items: result, Total: len(items), Offset: 0, Limit: limit}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		_ = enc.Encode(resp)
	}))
}

func TestFederatedSearchAllConfigured(t *testing.T) {
	alpha := searchServer(t, "alpha", false, mpapi.ResourceListItem{SourceSlug: "src-a", Kind: "skill", Name: "review", Score: 1.0})
	beta := searchServer(t, "beta", false, mpapi.ResourceListItem{SourceSlug: "src-b", Kind: "command", Name: "deploy", Score: 0.5})
	defer alpha.Close()
	defer beta.Close()

	ec := config.EffectiveConfig{
		Merged: &config.Config{
			Marketplaces: []config.MarketplaceEntry{
				{URL: alpha.URL, InstanceID: "mp-alpha", Alias: "alpha"},
				{URL: beta.URL, InstanceID: "mp-beta", Alias: "beta"},
			},
		},
	}

	results, endpointErrs, err := FederatedSearch(context.Background(), ec, "review", SearchOpts{})
	if err != nil {
		t.Fatalf("FederatedSearch: %v", err)
	}
	if len(endpointErrs) != 0 {
		t.Fatalf("unexpected endpoint errors: %v", endpointErrs)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Name != "review" || results[1].Name != "deploy" {
		t.Fatalf("unexpected order: %v, %v", results[0].Name, results[1].Name)
	}
}

func TestFederatedSearchSingleAlias(t *testing.T) {
	alpha := searchServer(t, "alpha", false, mpapi.ResourceListItem{SourceSlug: "src-a", Kind: "skill", Name: "review", Score: 1.0})
	beta := searchServer(t, "beta", false, mpapi.ResourceListItem{SourceSlug: "src-b", Kind: "skill", Name: "other", Score: 1.0})
	defer alpha.Close()
	defer beta.Close()

	ec := config.EffectiveConfig{
		Merged: &config.Config{
			Marketplaces: []config.MarketplaceEntry{
				{URL: alpha.URL, InstanceID: "mp-alpha", Alias: "alpha"},
				{URL: beta.URL, InstanceID: "mp-beta", Alias: "beta"},
			},
		},
	}

	results, endpointErrs, err := FederatedSearch(context.Background(), ec, "", SearchOpts{Marketplace: "alpha"})
	if err != nil {
		t.Fatalf("FederatedSearch: %v", err)
	}
	if len(endpointErrs) != 0 {
		t.Fatalf("unexpected endpoint errors: %v", endpointErrs)
	}
	if len(results) != 1 || results[0].Name != "review" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestFederatedSearchAdHocURL(t *testing.T) {
	alpha := searchServer(t, "alpha", false, mpapi.ResourceListItem{SourceSlug: "src-a", Kind: "skill", Name: "review", Score: 1.0})
	defer alpha.Close()

	ec := config.EffectiveConfig{Merged: &config.Config{}}

	results, endpointErrs, err := FederatedSearch(context.Background(), ec, "review", SearchOpts{Marketplace: alpha.URL})
	if err != nil {
		t.Fatalf("FederatedSearch: %v", err)
	}
	if len(endpointErrs) != 0 {
		t.Fatalf("unexpected endpoint errors: %v", endpointErrs)
	}
	if len(results) != 1 || results[0].Name != "review" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestFederatedSearchCancellationWhileSlotsAreBusy(t *testing.T) {
	searchStarted := make(chan struct{}, MaxSearchConcurrency+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(map[string]string{"instance_id": "mp-cancel"})
		case "/api/v1/search":
			searchStarted <- struct{}{}
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	marketplaces := make([]config.MarketplaceEntry, MaxSearchConcurrency+1)
	for i := range marketplaces {
		marketplaces[i] = config.MarketplaceEntry{
			URL:        server.URL,
			InstanceID: "mp-cancel",
			Alias:      fmt.Sprintf("cancel-%d", i),
		}
	}

	ec := config.EffectiveConfig{Merged: &config.Config{Marketplaces: marketplaces}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	var results []SearchResult
	var endpointErrs []EndpointError
	var err error
	go func() {
		results, endpointErrs, err = FederatedSearch(ctx, ec, "review", SearchOpts{})
		close(done)
	}()

	for i := 0; i < MaxSearchConcurrency; i++ {
		select {
		case <-searchStarted:
		case <-time.After(time.Second):
			t.Fatal("FederatedSearch did not fill all search slots")
		}
	}
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("FederatedSearch did not return after context cancellation")
	}
	if err != nil {
		t.Fatalf("FederatedSearch: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results after cancellation, got %d", len(results))
	}
	if len(endpointErrs) != MaxSearchConcurrency {
		t.Fatalf("expected errors from busy endpoints only, got %d errors", len(endpointErrs))
	}
}

func TestFederatedSearchInstanceIDMismatch(t *testing.T) {
	var searchRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(map[string]string{"instance_id": "mp-actual"})
		case "/api/v1/search":
			searchRequests++
			_ = json.NewEncoder(w).Encode(SearchResponse{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ec := config.EffectiveConfig{
		Merged: &config.Config{
			Marketplaces: []config.MarketplaceEntry{
				{URL: server.URL, InstanceID: "mp-configured", Alias: "mismatch"},
			},
		},
	}

	results, endpointErrs, err := FederatedSearch(context.Background(), ec, "review", SearchOpts{})
	if err != nil {
		t.Fatalf("FederatedSearch: %v", err)
	}
	if len(results) != 0 || len(endpointErrs) != 1 {
		t.Fatalf("expected mismatch endpoint error and no results, got results=%d errors=%d", len(results), len(endpointErrs))
	}
	if !strings.Contains(endpointErrs[0].Err.Error(), "instance ID mismatch") {
		t.Fatalf("expected instance ID mismatch, got: %v", endpointErrs[0].Err)
	}
	if searchRequests != 0 {
		t.Fatalf("expected no search requests, got %d", searchRequests)
	}
}

func TestFederatedSearchAllowlistBlock(t *testing.T) {
	alpha := searchServer(t, "alpha", false)
	defer alpha.Close()

	ec := config.EffectiveConfig{
		Merged: &config.Config{
			Marketplaces: []config.MarketplaceEntry{
				{URL: alpha.URL, InstanceID: "mp-alpha", Alias: "alpha"},
			},
		},
		AllowedMarketplaces: []string{`^other\.com$`},
	}

	_, _, err := FederatedSearch(context.Background(), ec, "", SearchOpts{})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected blocked error, got: %v", err)
	}
}

func TestFederatedSearchPartialFailure(t *testing.T) {
	alpha := searchServer(t, "alpha", false, mpapi.ResourceListItem{SourceSlug: "src-a", Kind: "skill", Name: "review", Score: 1.0})
	beta := searchServer(t, "beta", true)
	defer alpha.Close()
	defer beta.Close()

	ec := config.EffectiveConfig{
		Merged: &config.Config{
			Marketplaces: []config.MarketplaceEntry{
				{URL: alpha.URL, InstanceID: "mp-alpha", Alias: "alpha"},
				{URL: beta.URL, InstanceID: "mp-beta", Alias: "beta"},
			},
		},
	}

	results, endpointErrs, err := FederatedSearch(context.Background(), ec, "review", SearchOpts{})
	if err != nil {
		t.Fatalf("FederatedSearch: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if len(endpointErrs) != 1 || endpointErrs[0].Marketplace != "beta" {
		t.Fatalf("expected one beta endpoint error, got: %v", endpointErrs)
	}
}

func TestFederatedSearchAllFailure(t *testing.T) {
	alpha := searchServer(t, "alpha", true)
	defer alpha.Close()

	ec := config.EffectiveConfig{
		Merged: &config.Config{
			Marketplaces: []config.MarketplaceEntry{
				{URL: alpha.URL, InstanceID: "mp-alpha", Alias: "alpha"},
			},
		},
	}

	results, endpointErrs, err := FederatedSearch(context.Background(), ec, "review", SearchOpts{})
	if err != nil {
		t.Fatalf("FederatedSearch: %v", err)
	}
	if len(results) != 0 || len(endpointErrs) != 1 {
		t.Fatalf("expected no results and one error, got results=%d errors=%d", len(results), len(endpointErrs))
	}
}

func TestFederatedSearchLimitOffset(t *testing.T) {
	items := make([]mpapi.ResourceListItem, 10)
	for i := range items {
		items[i] = mpapi.ResourceListItem{SourceSlug: "src", Kind: "skill", Name: fmt.Sprintf("skill-%02d", i), Score: 1.0}
	}
	server := searchServer(t, "single", false, items...)
	defer server.Close()

	ec := config.EffectiveConfig{
		Merged: &config.Config{
			Marketplaces: []config.MarketplaceEntry{
				{URL: server.URL, InstanceID: "mp-single", Alias: "single"},
			},
		},
	}

	results, _, err := FederatedSearch(context.Background(), ec, "", SearchOpts{Limit: 3, Offset: 2})
	if err != nil {
		t.Fatalf("FederatedSearch: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Name != "skill-02" {
		t.Fatalf("expected first result skill-02, got %s", results[0].Name)
	}
}

func BenchmarkFederatedSearch(b *testing.B) {
	items := make([]mpapi.ResourceListItem, 100)
	for i := range items {
		items[i] = mpapi.ResourceListItem{SourceSlug: "src", Kind: "skill", Name: fmt.Sprintf("skill-%d", i), Score: 0.5}
	}
	server := searchServer(b, "bench", false, items...)
	defer server.Close()

	ec := config.EffectiveConfig{
		Merged: &config.Config{
			Marketplaces: []config.MarketplaceEntry{
				{URL: server.URL, InstanceID: "mp-bench", Alias: "bench"},
			},
		},
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := FederatedSearch(ctx, ec, "skill", SearchOpts{})
		if err != nil {
			b.Fatalf("FederatedSearch: %v", err)
		}
	}
}
