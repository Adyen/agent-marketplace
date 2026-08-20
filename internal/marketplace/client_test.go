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

	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func TestNewClientValidatesBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr string
	}{
		{"empty", "", "marketplace base URL is required"},
		{"ftp", "ftp://example.com", "must use http or https"},
		{"not-url", "://bad", "invalid marketplace base URL"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClient(tc.baseURL)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("NewClient(%q) error = %v, want containing %q", tc.baseURL, err, tc.wantErr)
			}
		})
	}
}

func TestClientWellKnown(t *testing.T) {
	want := mpapi.WellKnown{
		InstanceID:   "mp-test",
		CanonicalURL: "https://example.com",
		APIBase:      "/api/v1",
		Capabilities: mpapi.DefaultCapabilities(),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-marketplace.json" {
			http.NotFound(w, r)
			return
		}
		enc := json.NewEncoder(w)
		_ = enc.Encode(want)
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := c.WellKnown(context.Background())
	if err != nil {
		t.Fatalf("WellKnown: %v", err)
	}
	if got.InstanceID != want.InstanceID {
		t.Fatalf("InstanceID = %q, want %q", got.InstanceID, want.InstanceID)
	}
	if got.APIBase != want.APIBase {
		t.Fatalf("APIBase = %q, want %q", got.APIBase, want.APIBase)
	}
}

func TestClientSearch(t *testing.T) {
	resp := SearchResponse{
		Items: []mpapi.ResourceListItem{
			{SourceID: "src-test", SourceSlug: "test-source", Kind: "skill", Name: "review", Score: 1.0},
		},
		Total:  1,
		Offset: 0,
		Limit:  100,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("q") != "review" || q.Get("kind") != "skill" {
			http.Error(w, "unexpected query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		_ = enc.Encode(resp)
	}))
	defer server.Close()

	c, err := NewClient(server.URL, WithUserAgent("test/1.0"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := c.Search(context.Background(), SearchRequest{Query: "review", Kind: "skill"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "review" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestClientSearchError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"search failed"}`))
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.Search(context.Background(), SearchRequest{})
	if err == nil || !strings.Contains(err.Error(), "search failed") {
		t.Fatalf("Search error = %v, want containing 'search failed'", err)
	}
}

func TestClientSearchTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c, err := NewClient(server.URL, WithTimeout(time.Nanosecond))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = c.Search(ctx, SearchRequest{})
	if err == nil {
		t.Fatal("Search expected timeout error")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "Client.Timeout") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}

func TestClientSearchBadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.Search(context.Background(), SearchRequest{})
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("expected decode error, got: %v", err)
	}
}

func BenchmarkClientSearch(b *testing.B) {
	resp := SearchResponse{
		Items: make([]mpapi.ResourceListItem, 50),
		Total: 50,
	}
	for i := range resp.Items {
		resp.Items[i] = mpapi.ResourceListItem{
			SourceID: fmt.Sprintf("src-%d", i),
			Kind:     "skill",
			Name:     fmt.Sprintf("skill-%d", i),
			Score:    0.5,
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		_ = enc.Encode(resp)
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		b.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := c.Search(ctx, SearchRequest{Query: "review", Kind: "skill"})
		if err != nil {
			b.Fatalf("Search: %v", err)
		}
	}
}
