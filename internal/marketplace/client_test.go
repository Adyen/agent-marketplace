package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func writeTestWellKnown(w http.ResponseWriter, instanceID string) {
	_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "https://example.com"))
}

func writeTestSource(w http.ResponseWriter, src store.Source) {
	_ = json.NewEncoder(w).Encode(mpapi.SourceDTO{
		ID: src.ID, Slug: src.Slug, Aliases: src.Aliases, Origins: src.Origins,
		Ref: src.Ref, Roots: src.Roots, Acquisition: src.Acquisition,
		License: src.License, Listed: src.Listed,
	})
}

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
		BrowserRoute: "/s/{source}/{kind}/{name}",
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

func TestClientRejectsNullAndIncompleteEnvelopes(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "well-known null", path: "/.well-known/agent-marketplace.json", body: "null"},
		{name: "well-known incomplete", path: "/.well-known/agent-marketplace.json", body: `{"instance_id":"mp"}`},
		{name: "search null", path: "/api/v1/search", body: "null"},
		{name: "search incomplete", path: "/api/v1/search", body: `{"items":[],"total":0,"offset":0}`},
		{name: "search null items", path: "/api/v1/search", body: `{"items":null,"total":0,"offset":0,"limit":100}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			c, err := NewClient(server.URL)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if tc.path == "/.well-known/agent-marketplace.json" {
				if _, err := c.WellKnown(context.Background()); err == nil {
					t.Fatal("WellKnown accepted an invalid envelope")
				}
			} else if _, err := c.Search(context.Background(), SearchRequest{}); err == nil {
				t.Fatal("Search accepted an invalid envelope")
			}
		})
	}
}

func TestClientRejectsPrivateAndLegacySourceFields(t *testing.T) {
	for _, field := range []string{"revision_commit", "credentials_ref", "refresh_error", "policy_overrides"} {
		t.Run(field, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				payload := map[string]any{
					"id": "source", "slug": "source", "origins": []string{"https://example.com/repo"},
					"ref": "main", "roots": []map[string]string{{"path": ".agents"}},
					"acquisition": "index-only", "listed": true, field: "must not cross boundary",
				}
				_ = json.NewEncoder(w).Encode(payload)
			}))
			defer server.Close()
			c, err := NewClient(server.URL)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if _, err := c.GetSource(context.Background(), "source"); err == nil {
				t.Fatalf("GetSource accepted private field %q", field)
			}
		})
	}
}

func TestClientSearch(t *testing.T) {
	resp := SearchResponse{
		Items: []mpapi.ResourceListItem{
			{SourceID: "src-test", SourceSlug: "test-source", Kind: "skill", Name: "review", Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: "sha256:" + strings.Repeat("a", 64), Score: 1.0},
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

func TestAPIErrorBoundsAndSanitizesTerminalContent(t *testing.T) {
	body := append([]byte("prefix\x1b[31m\n\u202e"), []byte(strings.Repeat("x", maxAPIErrorBytes*3))...)
	err := apiError(http.StatusBadGateway, body)
	if err == nil {
		t.Fatal("apiError returned nil")
	}
	message := err.Error()
	if strings.ContainsAny(message, "\x00\x1b\n\r\t") {
		t.Fatalf("API error contains raw control character: %q", message)
	}
	if strings.Contains(message, "\u202e") {
		t.Fatalf("API error contains raw bidi marker: %q", message)
	}
	if !strings.Contains(message, `\x1b`) || !strings.Contains(message, `\n`) || !strings.Contains(message, `\u202e`) {
		t.Fatalf("API error did not preserve escaped diagnostics: %q", message)
	}
	if len(message) > maxAPIErrorOutputBytes+64 {
		t.Fatalf("API error was not bounded: %d bytes", len(message))
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

func TestNewClientRejectsUnsafeBaseURL(t *testing.T) {
	for _, baseURL := range []string{
		"http://",
		"http://?query=value",
		"http://example.com/path?query=value",
		"http://example.com/path#fragment",
		"http://user:pass@example.com",
	} {
		t.Run(baseURL, func(t *testing.T) {
			if _, err := NewClient(baseURL); err == nil {
				t.Fatalf("NewClient(%q) succeeded", baseURL)
			}
		})
	}
}

func TestClientEscapesDynamicPathSegments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.EscapedPath(), "/src%2Fwith%20space/skill/name%2Fpart/revisions/") {
			t.Fatalf("request path = %q, want escaped dynamic segments", r.URL.EscapedPath())
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"coordinate": map[string]string{
				"marketplace": "mp",
				"source":      "src/with space",
				"kind":        "skill",
				"name":        "name/part",
			},
			"content_digest": "sha256:" + strings.Repeat("a", 64),
		})
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetResourceRevision(context.Background(), "src/with space", "skill", "name/part", "sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatalf("GetResourceRevision: %v", err)
	}
}

func TestClientBindsResourceResponsesToRequestedCoordinate(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "source", Kind: "skill", Name: "other"},
			ContentDigest: store.RevisionDigest(digest),
		})
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetLatestResourceRevision(context.Background(), "source", "skill", "review"); err == nil || !strings.Contains(err.Error(), "coordinate.name") {
		t.Fatalf("GetLatestResourceRevision error = %v, want coordinate mismatch", err)
	}
	if _, err := c.GetResourceRevision(context.Background(), "source", "skill", "review", digest); err == nil || !strings.Contains(err.Error(), "coordinate.name") {
		t.Fatalf("GetResourceRevision error = %v, want coordinate mismatch", err)
	}
}

func TestClientBindsResourceSourceAfterSourceResolution(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/sources/source-slug":
			writeTestSource(w, store.Source{ID: "source-id", Slug: "source-slug", Origins: []string{"https://example.com/repo"}, Ref: "main", Roots: []store.SourceRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
		default:
			_ = json.NewEncoder(w).Encode(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "other-source", Kind: "skill", Name: "review"},
				ContentDigest: store.RevisionDigest(digest),
			})
		}
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetSourceBySlug(context.Background(), "source-slug"); err != nil {
		t.Fatalf("GetSourceBySlug: %v", err)
	}
	if _, err := c.GetLatestResourceRevision(context.Background(), "source-slug", "skill", "review"); err == nil || !strings.Contains(err.Error(), "coordinate.source") {
		t.Fatalf("GetLatestResourceRevision error = %v, want source mismatch", err)
	}
}

func TestClientBindsSourceResponseToRequestedSourceID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.PublicSource{
			ID: "other-source", Slug: "requested-source", Origins: []string{"https://example.com/repo"},
			Ref: "main", Roots: []store.SourceRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror,
		})
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetSource(context.Background(), "requested-source"); err == nil || !strings.Contains(err.Error(), "source ID") {
		t.Fatalf("GetSource error = %v, want requested source ID mismatch", err)
	}
}

func TestClientBindsSourceOccurrenceToRequestedSourceAndCommit(t *testing.T) {
	commit := strings.Repeat("a", 40)
	manifest := "sha256:" + strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.ExactSourceOccurrence{
			SourceID: "other-source", Ref: "main", Commit: commit,
			SourceRevisionManifestDigest: store.RevisionDigest(manifest),
			Acquisition:                  store.AcquisitionIndexOnly,
			AcquisitionOrigin:            "https://example.com/repo.git",
			OccurredAt:                   time.Now().UTC(),
		})
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetSourceOccurrence(context.Background(), "requested-source", commit); err == nil || !strings.Contains(err.Error(), "source ID") {
		t.Fatalf("GetSourceOccurrence error = %v, want requested source mismatch", err)
	}
}

func TestClientBindsSourceOccurrenceCommitToRequestedCommit(t *testing.T) {
	requestedCommit := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.ExactSourceOccurrence{
			SourceID: "source", Ref: "main", Commit: strings.Repeat("b", 40),
			SourceRevisionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("c", 64)),
			Acquisition:                  store.AcquisitionIndexOnly,
			AcquisitionOrigin:            "https://example.com/repo.git",
			OccurredAt:                   time.Now().UTC(),
		})
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetSourceOccurrence(context.Background(), "source", requestedCommit); err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("GetSourceOccurrence error = %v, want requested commit mismatch", err)
	}
}

func TestValidateAcquisitionOrigin(t *testing.T) {
	for _, origin := range []string{
		"https://example.com/org/repo.git",
		"http://example.com/org/repo.git",
		"ssh://example.com/org/repo.git",
		"git@example.com:org/repo.git",
		"file:///tmp/repo.git",
	} {
		if err := validateAcquisitionOrigin(origin); err != nil {
			t.Errorf("validateAcquisitionOrigin(%q) unexpected error: %v", origin, err)
		}
	}
	for _, test := range []struct {
		origin string
		want   string
	}{
		{origin: "/tmp/repo", want: "bare relative or absolute paths"},
		{origin: "./repo", want: "bare relative or absolute paths"},
		{origin: "https://user:pass@example.com/repo.git", want: "userinfo"},
		{origin: "https://example.com/repo.git?token=secret", want: "query"},
		{origin: "git://example.com/repo.git", want: "git:// protocol"},
		{origin: "ext::sh -c evil", want: "transport helper"},
		{origin: "example.com/repo.git", want: "HTTP(S)"},
	} {
		if err := validateAcquisitionOrigin(test.origin); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("validateAcquisitionOrigin(%q) = %v, want %q", test.origin, err, test.want)
		}
	}
}

func TestClientRejectsNonCanonicalResourceMetadata(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "source", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(digest),
		Provenance:    &store.Provenance{Commit: "abc123", ManifestDigest: store.RevisionDigest(digest)},
	}
	if err := validateAPIResourceRevision(revision); err == nil {
		t.Fatal("resource revision accepted a development provenance commit")
	}
	item := mpapi.ResourceListItem{SourceID: "source", Kind: "skill", Name: "review", Revision: "refs/heads/main", ContentDigest: digest}
	if err := validateAPIResourceListItem(item); err == nil {
		t.Fatal("resource list item accepted a symbolic revision")
	}
}

func TestClientRejectsInvalidArtifactDigest(t *testing.T) {
	c, err := NewClient("https://example.com")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	for _, digest := range []string{"", "sha256:short", "sha256:" + strings.Repeat("A", 64), "sha256:" + strings.Repeat("g", 64), "sha512:" + strings.Repeat("a", 64)} {
		if _, err := c.GetArtifact(context.Background(), digest); err == nil {
			t.Fatalf("GetArtifact(%q) succeeded", digest)
		}
	}
}

func TestClientDetectsOversizedResponse(t *testing.T) {
	resp := &http.Response{
		ContentLength: maxArtifactBytes + 1,
		Body:          io.NopCloser(strings.NewReader("small")),
	}
	if _, err := readResponseBody(resp, maxArtifactBytes); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("readResponseBody error = %v, want oversized response error", err)
	}
}

func TestClientRejectsCrossOriginRedirect(t *testing.T) {
	var targetRequests int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/v1/search", http.StatusFound)
	}))
	defer source.Close()

	c, err := NewClient(source.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.Search(context.Background(), SearchRequest{}); err == nil || !strings.Contains(err.Error(), "redirect origin") {
		t.Fatalf("Search error = %v, want redirect origin error", err)
	}
	if targetRequests != 0 {
		t.Fatalf("cross-origin redirect reached target %d times", targetRequests)
	}
}

func TestClientRejectsRedirectMutationByCheckRedirect(t *testing.T) {
	var targetRequests int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/v1/search", http.StatusFound)
	}))
	defer source.Close()

	hc := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		u := *req.URL
		mutated, err := http.NewRequest(http.MethodGet, target.URL+u.Path, nil)
		if err != nil {
			return err
		}
		req.URL = mutated.URL
		return nil
	}}
	c, err := NewClient(source.URL, WithHTTPClient(hc))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.Search(context.Background(), SearchRequest{}); err == nil || !strings.Contains(err.Error(), "redirect origin") {
		t.Fatalf("Search error = %v, want redirect origin error", err)
	}
	if targetRequests != 0 {
		t.Fatalf("mutated redirect reached target %d times", targetRequests)
	}
}

func BenchmarkClientSearch(b *testing.B) {
	resp := SearchResponse{
		Items: make([]mpapi.ResourceListItem, 50),
		Total: 50,
	}
	for i := range resp.Items {
		resp.Items[i] = canonicalSearchItem(fmt.Sprintf("src-%d", i), "skill", fmt.Sprintf("skill-%d", i), 0.5)
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
